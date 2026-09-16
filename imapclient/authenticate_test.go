package imapclient_test

import (
	"bufio"
	"context"
	"errors"
	"io"
	"net"
	"strings"
	"testing"
	"time"

	"github.com/emersion/go-sasl"

	"github.com/emersion/go-imap/v2"
	"github.com/emersion/go-imap/v2/imapclient"
)

func TestClient_Authenticate(t *testing.T) {
	client, server := newClientServerPair(t, imap.ConnStateNotAuthenticated)
	defer client.Close()
	defer server.Close()

	saslClient := sasl.NewPlainClient("", testUsername, testPassword)
	if err := client.Authenticate(saslClient); err != nil {
		t.Fatalf("Authenticate() = %v", err)
	}

	if state := client.State(); state != imap.ConnStateAuthenticated {
		t.Errorf("State() = %v, want %v", state, imap.ConnStateAuthenticated)
	}
}

// newScriptedServerClient returns a client connected to a server that sends a
// greeting with caps, reads the first command and calls serve with its tag.
// The server's connection has a 2 s deadline, so a client that waits without
// writing fails the test instead of hanging it.
func newScriptedServerClient(caps string, serve func(w io.Writer, br *bufio.Reader, tag string)) *imapclient.Client {
	clientConn, serverConn := net.Pipe()
	go func() {
		defer serverConn.Close()
		serverConn.SetDeadline(time.Now().Add(2 * time.Second))
		io.WriteString(serverConn, "* OK [CAPABILITY "+caps+"] ready\r\n")
		br := bufio.NewReader(serverConn)
		line, _ := br.ReadString('\n')
		tag, _, _ := strings.Cut(line, " ")
		serve(serverConn, br, tag)
	}()
	return imapclient.New(clientConn, nil)
}

func TestClient_Authenticate_cancel(t *testing.T) {
	oauth := sasl.NewOAuthBearerClient(&sasl.OAuthBearerOptions{Username: "user", Token: "bad"})
	// Gmail's reply to a bad token
	gmailChallenge := "+ eyJzdGF0dXMiOiJpbnZhbGlkX3JlcXVlc3QiLCJzY29wZSI6Imh0dHBzOi8vbWFpbC5nb29nbGUuY29tLyJ9"
	gmailNO := "TAG NO [AUTHENTICATIONFAILED] Invalid credentials (Failure)"
	tests := []struct {
		name      string
		sasl      sasl.Client
		challenge string
		reply     string // the server's answer to "*", TAG is the command tag; empty closes the connection
		wantErr   string // prefix
	}{
		{
			name:      "mechanism error",
			sasl:      oauth,
			challenge: gmailChallenge,
			reply:     gmailNO,
			wantErr:   "OAUTHBEARER authentication error (invalid_request): imap: NO [AUTHENTICATIONFAILED]",
		},
		{
			name:      "invalid base64",
			sasl:      oauth,
			challenge: "+ !",
			reply:     gmailNO,
			wantErr:   "illegal base64 data at input byte 0: imap: NO [AUTHENTICATIONFAILED]",
		},
		{
			name:      "no initial response",
			sasl:      fuzzSASLClient{mode: 3},
			challenge: "+ ",
			reply:     gmailNO,
			wantErr:   "imapclient: server requested SASL initial response, but we don't have one: imap: NO [AUTHENTICATIONFAILED]",
		},
		{
			name:      "cancellation accepted",
			sasl:      oauth,
			challenge: gmailChallenge,
			reply:     "TAG OK done",
			wantErr:   "OAUTHBEARER authentication error (invalid_request)",
		},
		{
			name:      "another challenge",
			sasl:      oauth,
			challenge: gmailChallenge,
			reply:     gmailChallenge,
			wantErr:   "OAUTHBEARER authentication error (invalid_request): in continue-req: received unmatched continuation request",
		},
		{
			name:      "connection closed",
			sasl:      oauth,
			challenge: gmailChallenge,
			wantErr:   "OAUTHBEARER authentication error (invalid_request): unexpected EOF",
		},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			cancelLine := make(chan string, 1)
			client := newScriptedServerClient("IMAP4rev1 SASL-IR", func(w io.Writer, br *bufio.Reader, tag string) {
				io.WriteString(w, tc.challenge+"\r\n")
				line, _ := br.ReadString('\n')
				cancelLine <- line
				if line != "*\r\n" || tc.reply == "" {
					return
				}
				io.WriteString(w, strings.Replace(tc.reply, "TAG", tag, 1)+"\r\n")
				line, _ = br.ReadString('\n')
				tag, _, _ = strings.Cut(line, " ")
				io.WriteString(w, tag+" OK done\r\n")
			})
			defer client.Close()

			err := client.Authenticate(tc.sasl)
			state := client.State() // before the server closes the connection
			// Without the cancellation, the server reads NOOP in its place and
			// closes the connection
			noopErr := client.Noop().Wait()
			if line := <-cancelLine; line != "*\r\n" {
				t.Fatalf("sent %q after the challenge, want %q", line, "*\r\n")
			}
			if err == nil || !strings.HasPrefix(err.Error(), tc.wantErr) {
				t.Errorf("Authenticate() = %v, want prefix %q", err, tc.wantErr)
			}
			if tc.reply != gmailNO {
				// The server didn't end the exchange as expected, so the
				// connection can't be trusted
				if state != imap.ConnStateLogout {
					t.Errorf("State() = %v, want %v", state, imap.ConnStateLogout)
				}
				if noopErr == nil {
					t.Errorf("Noop() = nil, want an error on the closed connection")
				}
				return
			}
			var imapErr *imap.Error
			if !errors.As(err, &imapErr) || imapErr.Code != imap.ResponseCodeAuthenticationFailed {
				t.Errorf("Authenticate() = %v, want the server's NO attached", err)
			}
			if state != imap.ConnStateNotAuthenticated {
				t.Errorf("State() = %v, want %v", state, imap.ConnStateNotAuthenticated)
			}
			if noopErr != nil {
				t.Errorf("Noop() = %v", noopErr)
			}
		})
	}
}

// slowClient is a SASL mechanism whose Next takes a while.
type slowClient struct{}

func (slowClient) Start() (string, []byte, error) { return "X-TEST", []byte("ir"), nil }
func (slowClient) Next([]byte) ([]byte, error) {
	time.Sleep(100 * time.Millisecond)
	return []byte("resp"), nil
}

// The server ends AUTHENTICATE right after a challenge, before the client has
// answered it. The client then waits for a challenge on a finished command.
func TestClient_Authenticate_completedDuringNext(t *testing.T) {
	client := newScriptedServerClient("IMAP4rev1 SASL-IR", func(w io.Writer, br *bufio.Reader, tag string) {
		io.WriteString(w, "+ Zm9v\r\n"+tag+" NO [AUTHENTICATIONFAILED] Invalid credentials\r\n")
		io.Copy(io.Discard, br)
	})
	defer client.Close()

	done := make(chan error, 1)
	go func() { done <- client.Authenticate(slowClient{}) }()
	select {
	case err := <-done:
		var imapErr *imap.Error
		if !errors.As(err, &imapErr) || imapErr.Code != imap.ResponseCodeAuthenticationFailed {
			t.Errorf("Authenticate() = %v, want the server's NO", err)
		}
	case <-time.After(3 * time.Second):
		t.Fatal("Authenticate() blocked")
	}
}

// The library has no timeout for a silent server. The caller bounds
// Authenticate by closing the client.
func TestClient_Authenticate_callerTimeout(t *testing.T) {
	client := newScriptedServerClient("IMAP4rev1 SASL-IR", func(w io.Writer, br *bufio.Reader, tag string) {
		io.WriteString(w, "+ Zm9v\r\n")
		io.Copy(io.Discard, br) // never answers the "*"
	})
	defer client.Close()

	ctx, cancel := context.WithTimeout(context.Background(), 100*time.Millisecond)
	defer cancel()
	context.AfterFunc(ctx, func() { client.Close() })

	done := make(chan error, 1)
	go func() { done <- client.Authenticate(fuzzSASLClient{mode: 0}) }()
	select {
	case err := <-done:
		if err == nil {
			t.Errorf("Authenticate() = nil, want an error")
		}
	case <-time.After(time.Second): // before the server's 2 s deadline closes the connection
		t.Fatal("Authenticate() blocked after the client was closed")
	}
}

// fuzzSASLClient is a SASL mechanism whose behavior is picked by mode%4.
type fuzzSASLClient struct{ mode uint8 }

func (c fuzzSASLClient) Start() (string, []byte, error) {
	if c.mode%4 == 3 {
		return "X-TEST", nil, nil
	}
	return "X-TEST", []byte("ir"), nil
}

func (c fuzzSASLClient) Next([]byte) ([]byte, error) {
	switch c.mode % 4 {
	case 0:
		return nil, errors.New("mechanism failed")
	case 1:
		return []byte{}, nil
	default:
		return []byte("resp"), nil
	}
}

// FuzzAuthenticate checks that Authenticate, then NOOP and Close, return
// whatever the server sends before it closes the connection.
//
// mode%4 picks the mechanism's behavior, and mode&4 turns off SASL-IR. In the
// script, lines are separated by "\n" and TAG is the AUTHENTICATE tag.
func FuzzAuthenticate(f *testing.F) {
	gmailChallenge := "+ eyJzdGF0dXMiOiJpbnZhbGlkX3JlcXVlc3QiLCJzY29wZSI6Imh0dHBzOi8vbWFpbC5nb29nbGUuY29tLyJ9"
	f.Add(uint8(0), gmailChallenge+"\nTAG NO [AUTHENTICATIONFAILED] Invalid credentials")
	f.Add(uint8(0), gmailChallenge+"\nTAG OK [CAPABILITY IMAP4rev1] done")
	f.Add(uint8(1), "+ \n+ \nTAG OK done")
	f.Add(uint8(2), "+ Zm9v\n+ Zm9v\nTAG BAD done")
	f.Add(uint8(7), "+ \n+ !\n* BYE bye")
	// The read goroutine looped forever (#39)
	f.Add(uint8(0), "* CAPABILITY (")
	f.Fuzz(func(t *testing.T, mode uint8, script string) {
		caps := "IMAP4rev1 SASL-IR"
		if mode&4 != 0 {
			caps = "IMAP4rev1"
		}
		client := newScriptedServerClient(caps, func(w io.Writer, br *bufio.Reader, tag string) {
			go io.Copy(io.Discard, br)
			for _, l := range strings.Split(script, "\n") {
				if _, err := io.WriteString(w, strings.ReplaceAll(l, "TAG", tag)+"\r\n"); err != nil {
					return
				}
			}
		})

		returnsWithin := func(name string, f func()) {
			done := make(chan struct{})
			go func() {
				f()
				close(done)
			}()
			select {
			case <-done:
			case <-time.After(5 * time.Second):
				t.Fatalf("%v blocked", name)
			}
		}
		returnsWithin("Authenticate()", func() { client.Authenticate(fuzzSASLClient{mode}) })
		returnsWithin("Noop()", func() { client.Noop().Wait() })
		returnsWithin("Close()", func() { client.Close() })
	})
}

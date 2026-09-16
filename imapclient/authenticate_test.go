package imapclient_test

import (
	"bufio"
	"context"
	"errors"
	"io"
	"net"
	"os"
	"strings"
	"testing"
	"testing/synctest"
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
func newScriptedServerClient(caps string, serve func(w io.Writer, br *bufio.Reader, tag string)) *imapclient.Client {
	clientConn, serverConn := net.Pipe()
	go func() {
		defer serverConn.Close()
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
			synctest.Test(t, func(t *testing.T) {
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
		})
	}
}

// slowClient is a SASL mechanism whose Next takes longer than the client's
// 30 s write timeout. Next then returns err, if set.
type slowClient struct{ err error }

func (slowClient) Start() (string, []byte, error) { return "X-TEST", []byte("ir"), nil }
func (c slowClient) Next([]byte) ([]byte, error) {
	time.Sleep(31 * time.Second)
	return []byte("resp"), c.err
}

// The server ends AUTHENTICATE right after a challenge, before the client has
// answered it. The client must not send the answer or the "*", which the server
// would read as a new command.
func TestClient_Authenticate_completedDuringNext(t *testing.T) {
	tests := []struct {
		name string
		sasl sasl.Client
	}{
		{name: "answer", sasl: slowClient{}},
		{name: "cancel", sasl: slowClient{err: errors.New("mechanism failed")}},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			synctest.Test(t, func(t *testing.T) {
				nextLine := make(chan string, 1)
				client := newScriptedServerClient("IMAP4rev1 SASL-IR", func(w io.Writer, br *bufio.Reader, tag string) {
					io.WriteString(w, "+ Zm9v\r\n"+tag+" NO [AUTHENTICATIONFAILED] Invalid credentials\r\n")
					line, _ := br.ReadString('\n')
					nextLine <- line
					tag, _, _ = strings.Cut(line, " ")
					io.WriteString(w, tag+" OK done\r\n")
				})
				defer client.Close()

				err := client.Authenticate(tc.sasl)
				noopErr := client.Noop().Wait()
				if line := <-nextLine; !strings.HasSuffix(line, " NOOP\r\n") {
					t.Errorf("sent %q after the command ended, want NOOP", line)
				}
				var imapErr *imap.Error
				if !errors.As(err, &imapErr) || imapErr.Code != imap.ResponseCodeAuthenticationFailed {
					t.Errorf("Authenticate() = %v, want the server's NO", err)
				}
				if noopErr != nil {
					t.Errorf("Noop() = %v", noopErr)
				}
			})
		})
	}
}

// The answer to a challenge is sent after Next, which outlasts the write
// deadline set when the command started.
func TestClient_Authenticate_slowNext(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		client := newScriptedServerClient("IMAP4rev1 SASL-IR", func(w io.Writer, br *bufio.Reader, tag string) {
			io.WriteString(w, "+ Zm9v\r\n")
			br.ReadString('\n')
			io.WriteString(w, tag+" NO [AUTHENTICATIONFAILED] Invalid credentials\r\n")
		})
		defer client.Close()

		err := client.Authenticate(slowClient{})
		var imapErr *imap.Error
		if !errors.As(err, &imapErr) || imapErr.Code != imap.ResponseCodeAuthenticationFailed {
			t.Errorf("Authenticate() = %v, want the server's NO", err)
		}
	})
}

// The server stops reading, so the answer to a challenge times out. The server
// is left inside the exchange, so the client must be closed.
func TestClient_Authenticate_serverStopsReading(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		stop := make(chan struct{})
		defer close(stop)
		client := newScriptedServerClient("IMAP4rev1 SASL-IR", func(w io.Writer, br *bufio.Reader, tag string) {
			io.WriteString(w, "+ Zm9v\r\n")
			<-stop
		})
		defer client.Close()

		err := client.Authenticate(fuzzSASLClient{mode: 2})
		if !errors.Is(err, os.ErrDeadlineExceeded) {
			t.Errorf("Authenticate() = %v, want a write timeout", err)
		}
		if state := client.State(); state != imap.ConnStateLogout {
			t.Errorf("State() = %v, want %v", state, imap.ConnStateLogout)
		}
	})
}

// The library has no timeout for a silent server. The caller bounds
// Authenticate by closing the client.
func TestClient_Authenticate_callerTimeout(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		client := newScriptedServerClient("IMAP4rev1 SASL-IR", func(w io.Writer, br *bufio.Reader, tag string) {
			io.WriteString(w, "+ Zm9v\r\n")
			io.Copy(io.Discard, br) // never answers the "*"
		})
		defer client.Close()

		ctx, cancel := context.WithTimeout(context.Background(), 100*time.Millisecond)
		defer cancel()
		context.AfterFunc(ctx, func() { client.Close() })

		if err := client.Authenticate(fuzzSASLClient{mode: 0}); err == nil {
			t.Errorf("Authenticate() = nil, want an error")
		}
	})
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
// Unlike the tests above, it runs on the real clock. A goroutine spinning in a
// loop, like the one fixed in #39, stops synctest's fake clock, so a timeout
// would never fire.
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

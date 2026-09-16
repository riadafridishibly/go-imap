package imapclient_test

import (
	"bufio"
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

// noInitialResponseClient is a SASL mechanism without an initial response.
type noInitialResponseClient struct{}

func (noInitialResponseClient) Start() (string, []byte, error) { return "X-TEST", nil, nil }
func (noInitialResponseClient) Next([]byte) ([]byte, error)    { return nil, nil }

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
			sasl:      noInitialResponseClient{},
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
			clientConn, serverConn := net.Pipe()
			cancelLine := make(chan string, 1)
			go func() {
				defer serverConn.Close()
				// Fail instead of hanging if the client waits without writing
				serverConn.SetDeadline(time.Now().Add(5 * time.Second))
				io.WriteString(serverConn, "* OK [CAPABILITY IMAP4rev1 SASL-IR] ready\r\n")
				br := bufio.NewReader(serverConn)
				line, _ := br.ReadString('\n')
				tag, _, _ := strings.Cut(line, " ")
				io.WriteString(serverConn, tc.challenge+"\r\n")
				line, _ = br.ReadString('\n')
				cancelLine <- line
				if line != "*\r\n" || tc.reply == "" {
					return
				}
				io.WriteString(serverConn, strings.Replace(tc.reply, "TAG", tag, 1)+"\r\n")
				line, _ = br.ReadString('\n')
				tag, _, _ = strings.Cut(line, " ")
				io.WriteString(serverConn, tag+" OK done\r\n")
			}()
			client := imapclient.New(clientConn, nil)
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

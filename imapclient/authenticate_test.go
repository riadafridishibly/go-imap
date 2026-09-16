package imapclient_test

import (
	"bufio"
	"errors"
	"io"
	"net"
	"strings"
	"testing"

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
	tests := []struct {
		name      string
		sasl      sasl.Client
		challenge string
		wantErr   string // prefix, followed by the server's NO
	}{
		{
			// Gmail's reply to a bad token
			name:      "mechanism error",
			sasl:      oauth,
			challenge: "+ eyJzdGF0dXMiOiJpbnZhbGlkX3JlcXVlc3QiLCJzY29wZSI6Imh0dHBzOi8vbWFpbC5nb29nbGUuY29tLyJ9",
			wantErr:   "OAUTHBEARER authentication error (invalid_request): ",
		},
		{
			name:      "invalid base64",
			sasl:      oauth,
			challenge: "+ !",
			wantErr:   "illegal base64 data at input byte 0: ",
		},
		{
			name:      "no initial response",
			sasl:      noInitialResponseClient{},
			challenge: "+ ",
			wantErr:   "imapclient: server requested SASL initial response, but we don't have one: ",
		},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			clientConn, serverConn := net.Pipe()
			cancelLine := make(chan string, 1)
			go func() {
				defer serverConn.Close()
				io.WriteString(serverConn, "* OK [CAPABILITY IMAP4rev1 SASL-IR] ready\r\n")
				br := bufio.NewReader(serverConn)
				line, _ := br.ReadString('\n')
				tag, _, _ := strings.Cut(line, " ")
				io.WriteString(serverConn, tc.challenge+"\r\n")
				line, _ = br.ReadString('\n')
				cancelLine <- line
				if line != "*\r\n" {
					return
				}
				io.WriteString(serverConn, tag+" NO [AUTHENTICATIONFAILED] Invalid credentials (Failure)\r\n")
				line, _ = br.ReadString('\n')
				tag, _, _ = strings.Cut(line, " ")
				io.WriteString(serverConn, tag+" OK done\r\n")
			}()
			client := imapclient.New(clientConn, nil)
			defer client.Close()

			err := client.Authenticate(tc.sasl)
			// Without the cancellation, the server reads NOOP in its place and
			// closes the connection
			noopErr := client.Noop().Wait()
			if line := <-cancelLine; line != "*\r\n" {
				t.Fatalf("sent %q after the challenge, want %q", line, "*\r\n")
			}
			var imapErr *imap.Error
			if err == nil || !strings.HasPrefix(err.Error(), tc.wantErr) ||
				!errors.As(err, &imapErr) || imapErr.Code != imap.ResponseCodeAuthenticationFailed {
				t.Errorf("Authenticate() = %v, want %q followed by the server's NO", err, tc.wantErr)
			}
			if noopErr != nil {
				t.Errorf("Noop() = %v", noopErr)
			}
		})
	}
}

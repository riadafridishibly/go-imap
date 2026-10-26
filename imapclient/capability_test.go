package imapclient_test

import (
	"bufio"
	"io"
	"net"
	"strings"
	"testing"
	"time"

	"github.com/emersion/go-sasl"

	"github.com/emersion/go-imap/v2"
	"github.com/emersion/go-imap/v2/imapclient"
)

func TestCapability_spaces(t *testing.T) {
	tests := []struct {
		name string
		resp string
		want imap.Cap // empty means an error is expected
	}{
		// https://github.com/emersion/go-imap/pull/652
		{name: "double SP", resp: "* CAPABILITY IMAP4rev1  IDLE", want: imap.CapIdle},
		// Decoder.SP accepts "(" without reading it, because SP before a list
		// is optional. Skipping repeated SP looped forever on it.
		{name: "list", resp: "* CAPABILITY IMAP4rev1 ("},
		{name: "SP and list", resp: "* CAPABILITY IMAP4rev1  ("},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			client, _ := newRawServerClient(t, tc.resp)
			type result struct {
				caps imap.CapSet
				err  error
			}
			done := make(chan result, 1)
			go func() {
				caps, err := client.Capability().Wait()
				done <- result{caps, err}
			}()
			var res result
			select {
			case res = <-done:
			case <-time.After(5 * time.Second):
				t.Fatal("Capability() blocked")
			}
			if tc.want == "" {
				if res.err == nil {
					t.Errorf("Capability() = %v, want an error", res.caps)
				}
				return
			}
			if res.err != nil || !res.caps.Has(tc.want) {
				t.Errorf("Capability() = %v, %v; want %v", res.caps, res.err, tc.want)
			}
		})
	}
}

func TestWaitGreeting_capabilityList(t *testing.T) {
	client, _ := newRawServerClientCaps(t, "IMAP4rev1 (")
	done := make(chan error, 1)
	go func() { done <- client.WaitGreeting() }()
	select {
	case err := <-done:
		if err == nil {
			t.Errorf("WaitGreeting() = nil, want an error")
		}
	case <-time.After(5 * time.Second):
		t.Fatal("WaitGreeting() blocked")
	}
}

func TestCaps_greetingWithoutCaps(t *testing.T) {
	client, cmds := newRawServerClientCaps(t, "", "", "", "* CAPABILITY IMAP4rev1 MOVE")
	// CAPABILITY is sent when the capabilities are needed, not after the greeting
	for range 2 {
		if err := client.Noop().Wait(); err != nil {
			t.Fatalf("Noop() = %v", err)
		}
		if cmd := <-cmds; cmd != "NOOP" {
			t.Fatalf("sent %q, want NOOP", cmd)
		}
	}
	if !client.Caps().Has(imap.CapMove) {
		t.Errorf("Caps() = %v, want MOVE", client.Caps())
	}
	if cmd := <-cmds; cmd != "CAPABILITY" {
		t.Errorf("sent %q, want CAPABILITY", cmd)
	}
}

// authCommandTests run commands that change the capabilities.
var authCommandTests = []struct {
	name string
	run  func(client *imapclient.Client) error
}{
	{"LOGIN", func(client *imapclient.Client) error {
		return client.Login("user", "pass").Wait()
	}},
	{"AUTHENTICATE", func(client *imapclient.Client) error {
		return client.Authenticate(sasl.NewPlainClient("", "user", "pass"))
	}},
	{"UNAUTHENTICATE", func(client *imapclient.Client) error {
		return client.Unauthenticate().Wait()
	}},
}

func TestCaps_refresh(t *testing.T) {
	for _, tc := range authCommandTests {
		t.Run(tc.name, func(t *testing.T) {
			client, cmds := newRawServerClientCaps(t, "IMAP4rev1 SASL-IR", "", "* CAPABILITY IMAP4rev1 MOVE")
			if err := tc.run(client); err != nil {
				t.Fatalf("%v = %v", tc.name, err)
			}
			if cmd := <-cmds; !strings.HasPrefix(cmd, tc.name) {
				t.Errorf("sent %q, want %v", cmd, tc.name)
			}
			// The server has answered CAPABILITY before the command returns
			select {
			case cmd := <-cmds:
				if cmd != "CAPABILITY" {
					t.Errorf("sent %q, want CAPABILITY", cmd)
				}
			default:
				t.Fatalf("%v returned before CAPABILITY was sent", tc.name)
			}
			if !client.Caps().Has(imap.CapMove) {
				t.Errorf("Caps() = %v, want MOVE", client.Caps())
			}
		})
	}
}

// Gmail sends the new list untagged with the reply, not in the tagged OK.
func TestCaps_untaggedWithAuth(t *testing.T) {
	for _, tc := range authCommandTests {
		t.Run(tc.name, func(t *testing.T) {
			client, cmds := newRawServerClientCaps(t, "IMAP4rev1 SASL-IR", "* CAPABILITY IMAP4rev1 MOVE", "")
			if err := tc.run(client); err != nil {
				t.Fatalf("%v = %v", tc.name, err)
			}
			<-cmds
			select {
			case cmd := <-cmds:
				t.Fatalf("sent %q, want nothing after %v", cmd, tc.name)
			default:
			}
			if !client.Caps().Has(imap.CapMove) {
				t.Errorf("Caps() = %v, want MOVE", client.Caps())
			}
		})
	}
}

// Caps returns the capabilities of its own CAPABILITY command, even if a
// command completed after it resets them.
func TestCaps_resetDuringRequest(t *testing.T) {
	clientConn, serverConn := net.Pipe()
	t.Cleanup(func() { serverConn.Close() })
	capRead := make(chan struct{})
	go func() {
		io.WriteString(serverConn, "* OK ready\r\n")
		br := bufio.NewReader(serverConn)
		br.ReadString('\n') // T1 CAPABILITY
		close(capRead)
		br.ReadString('\n') // T2 LOGIN
		io.WriteString(serverConn, "* CAPABILITY IMAP4rev1 MOVE\r\nT1 OK done\r\nT2 OK done\r\n")
		br.ReadString('\n') // T3 CAPABILITY
		io.WriteString(serverConn, "* CAPABILITY IMAP4rev1 IDLE\r\nT3 OK done\r\n")
	}()
	client := imapclient.New(clientConn, nil)
	t.Cleanup(func() { client.Close() })

	capsCh := make(chan imap.CapSet, 1)
	go func() { capsCh <- client.Caps() }()
	<-capRead
	if err := client.Login("user", "pass").Wait(); err != nil {
		t.Fatalf("Login() = %v", err)
	}
	if caps := <-capsCh; !caps.Has(imap.CapMove) {
		t.Errorf("Caps() before LOGIN = %v, want MOVE", caps)
	}
	if caps := client.Caps(); !caps.Has(imap.CapIdle) {
		t.Errorf("Caps() after LOGIN = %v, want IDLE", caps)
	}
}

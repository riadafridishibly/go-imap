package imapclient_test

import (
	"testing"
	"time"

	"github.com/emersion/go-imap/v2"
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

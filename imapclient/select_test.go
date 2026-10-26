package imapclient_test

import (
	"testing"

	"github.com/emersion/go-imap/v2"
)

func TestSelect(t *testing.T) {
	client, server := newClientServerPair(t, imap.ConnStateAuthenticated)
	defer client.Close()
	defer server.Close()

	data, err := client.Select("INBOX", nil).Wait()
	if err != nil {
		t.Fatalf("Select() = %v", err)
	} else if data.NumMessages != 1 {
		t.Errorf("SelectData.NumMessages = %v, want %v", data.NumMessages, 1)
	} else if data.ReadOnly {
		t.Errorf("SelectData.ReadOnly = true, want false")
	}
}

func TestSelectReadOnly(t *testing.T) {
	client, server := newClientServerPair(t, imap.ConnStateAuthenticated)
	defer client.Close()
	defer server.Close()

	data, err := client.Select("INBOX", &imap.SelectOptions{ReadOnly: true}).Wait()
	if err != nil {
		t.Fatalf("Select() = %v", err)
	} else if !data.ReadOnly {
		t.Errorf("SelectData.ReadOnly = false, want true")
	}
}

// A failed SELECT deselects the mailbox, unless it was rejected with BAD.
func TestSelect_failed(t *testing.T) {
	tests := []struct {
		reply string
		state imap.ConnState
	}{
		{"TAG NO [NONEXISTENT] Unknown Mailbox (now in authenticated state)", imap.ConnStateAuthenticated},
		{"TAG BAD invalid arguments", imap.ConnStateSelected},
	}
	for _, tc := range tests {
		t.Run(tc.reply[4:7], func(t *testing.T) {
			client, _ := newRawServerClientCaps(t, "IMAP4rev1", "TAG OK [READ-ONLY] done", tc.reply)
			if _, err := client.Select("INBOX", &imap.SelectOptions{ReadOnly: true}).Wait(); err != nil {
				t.Fatalf("Select(INBOX) = %v", err)
			}
			if _, err := client.Select("missing", nil).Wait(); err == nil {
				t.Fatalf("Select(missing) = nil, want an error")
			}
			if state := client.State(); state != tc.state {
				t.Errorf("State() = %v, want %v", state, tc.state)
			}
			if mbox := client.Mailbox(); (mbox == nil) != (tc.state != imap.ConnStateSelected) {
				t.Errorf("Mailbox() = %v", mbox)
			}
		})
	}
}

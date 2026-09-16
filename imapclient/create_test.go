package imapclient_test

import (
	"testing"

	"github.com/emersion/go-imap/v2"
	"github.com/emersion/go-imap/v2/imapclient"
)

func testCreate(t *testing.T, name string, utf8Accept bool) {
	client, server := newClientServerPair(t, imap.ConnStateAuthenticated)
	defer client.Close()
	defer server.Close()

	if utf8Accept {
		if !client.Caps().Has(imap.CapUTF8Accept) {
			t.Skipf("missing UTF8=ACCEPT support")
		}
		if data, err := client.Enable(imap.CapUTF8Accept).Wait(); err != nil {
			t.Fatalf("Enable(CapUTF8Accept) = %v", err)
		} else if !data.Caps.Has(imap.CapUTF8Accept) {
			t.Fatalf("server refused to enable UTF8=ACCEPT")
		}
	}

	if err := client.Create(name, nil).Wait(); err != nil {
		t.Fatalf("Create() = %v", err)
	}

	listCmd := client.List("", name, nil)
	mailboxes, err := listCmd.Collect()
	if err != nil {
		t.Errorf("List() = %v", err)
	} else if len(mailboxes) != 1 || mailboxes[0].Mailbox != name {
		t.Errorf("List() = %v, want exactly one entry with correct name", mailboxes)
	}
}

func TestCreate(t *testing.T) {
	t.Run("basic", func(t *testing.T) {
		testCreate(t, "Test mailbox", false)
	})

	t.Run("unicode_utf7", func(t *testing.T) {
		testCreate(t, "Cafè", false)
	})
	t.Run("unicode_utf8", func(t *testing.T) {
		testCreate(t, "Cafè", true)
	})

	// '&' is the UTF-7 escape character
	t.Run("ampersand_utf7", func(t *testing.T) {
		testCreate(t, "Angus & Julia", false)
	})
	t.Run("ampersand_utf8", func(t *testing.T) {
		testCreate(t, "Angus & Julia", true)
	})
}

// A server without IMAP4rev1 is in UTF-8 mode without ENABLE
func TestCreate_imap4rev2Only(t *testing.T) {
	conn, server := newMemClientServerPair(t, imap.CapSet{imap.CapIMAP4rev2: {}})
	defer server.Close()
	client := imapclient.New(conn, nil)
	defer client.Close()
	if err := client.Login(testUsername, testPassword).Wait(); err != nil {
		t.Fatalf("Login() = %v", err)
	}

	for _, name := range []string{"Angus & Julia", "Cafè"} {
		if err := client.Create(name, nil).Wait(); err != nil {
			t.Fatalf("Create(%q) = %v", name, err)
		}
		mailboxes, err := client.List("", name, nil).Collect()
		if err != nil {
			t.Fatalf("List(%q) = %v", name, err)
		}
		if len(mailboxes) != 1 || mailboxes[0].Mailbox != name {
			t.Errorf("List(%q) = %d mailboxes, want exactly one with that name", name, len(mailboxes))
		}
	}
}

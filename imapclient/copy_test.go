package imapclient_test

import (
	"testing"

	"github.com/emersion/go-imap/v2"
)

// COPY and MOVE that match no message succeed without COPYUID.
func TestCopyMove_noMatch(t *testing.T) {
	client, server := newClientServerPair(t, imap.ConnStateSelected)
	defer client.Close()
	defer server.Close()

	if err := client.Create("dest", nil).Wait(); err != nil {
		t.Fatalf("Create() = %v", err)
	}
	if data, err := client.Copy(imap.UIDSetNum(99), "dest").Wait(); err != nil {
		t.Errorf("Copy() = %v", err)
	} else if data.SourceUIDs != nil {
		t.Errorf("Copy() SourceUIDs = %v, want nil", data.SourceUIDs)
	}
	if data, err := client.Move(imap.UIDSetNum(99), "dest").Wait(); err != nil {
		t.Errorf("Move() = %v", err)
	} else if data.SourceUIDs != nil {
		t.Errorf("Move() SourceUIDs = %v, want nil", data.SourceUIDs)
	}
}

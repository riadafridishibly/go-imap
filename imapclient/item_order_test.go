package imapclient

import (
	"bufio"
	"bytes"
	"reflect"
	"testing"

	"github.com/emersion/go-imap/v2"
	"github.com/emersion/go-imap/v2/internal/imapwire"
)

// Items used to be collected from a map, so their order changed between runs.
// Repeat each encoding to catch that.
const itemOrderRuns = 20

func TestWriteFetchItemsOrder(t *testing.T) {
	options := &imap.FetchOptions{
		BodyStructure: &imap.FetchItemBodyStructure{Extended: true},
		Envelope:      true,
		Flags:         true,
		InternalDate:  true,
		RFC822Size:    true,
		ModSeq:        true,
	}
	want := "(UID BODYSTRUCTURE ENVELOPE FLAGS INTERNALDATE RFC822.SIZE MODSEQ)"

	for range itemOrderRuns {
		var buf bytes.Buffer
		bw := bufio.NewWriter(&buf)
		writeFetchItems(imapwire.NewEncoder(bw, imapwire.ConnSideClient), imapwire.NumKindUID, options)
		bw.Flush()
		if got := buf.String(); got != want {
			t.Fatalf("writeFetchItems() = %q, want %q", got, want)
		}
	}
}

func TestStatusItemsOrder(t *testing.T) {
	options := &imap.StatusOptions{
		NumMessages:    true,
		UIDNext:        true,
		UIDValidity:    true,
		NumUnseen:      true,
		NumDeleted:     true,
		Size:           true,
		AppendLimit:    true,
		DeletedStorage: true,
		HighestModSeq:  true,
	}
	want := []string{"MESSAGES", "UIDNEXT", "UIDVALIDITY", "UNSEEN", "DELETED", "SIZE", "APPENDLIMIT", "DELETED-STORAGE", "HIGHESTMODSEQ"}

	for range itemOrderRuns {
		if got := statusItems(options); !reflect.DeepEqual(got, want) {
			t.Fatalf("statusItems() = %v, want %v", got, want)
		}
	}
}

func TestReturnSearchOptionsOrder(t *testing.T) {
	options := &imap.SearchOptions{
		ReturnMin:   true,
		ReturnMax:   true,
		ReturnAll:   true,
		ReturnCount: true,
	}
	want := []string{"MIN", "MAX", "ALL", "COUNT"}

	for range itemOrderRuns {
		if got := returnSearchOptions(options); !reflect.DeepEqual(got, want) {
			t.Fatalf("returnSearchOptions() = %v, want %v", got, want)
		}
	}
}

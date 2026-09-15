package imapserver

import (
	"bufio"
	"strings"
	"testing"

	"github.com/emersion/go-imap/v2"
	"github.com/emersion/go-imap/v2/internal/imapwire"
)

func TestWriteBodyStructure_emptyMultiPart(t *testing.T) {
	// imapclient returns this for a multipart body with no children
	bs := &imap.BodyStructureMultiPart{
		Subtype: "ALTERNATIVE",
		Extended: &imap.BodyStructureMultiPartExt{
			Params: map[string]string{"boundary": "x"},
		},
	}
	tests := []struct {
		extended bool
		want     string
	}{
		{false, `(("text" "plain" ("charset" "us-ascii") NIL NIL "7bit" 0 0) "ALTERNATIVE")`},
		{true, `(("text" "plain" ("charset" "us-ascii") NIL NIL "7bit" 0 0 NIL NIL NIL NIL) "ALTERNATIVE" ("boundary" "x") NIL NIL NIL)`},
	}
	for _, tc := range tests {
		var sb strings.Builder
		bw := bufio.NewWriter(&sb)
		writeBodyStructure(imapwire.NewEncoder(bw, imapwire.ConnSideServer), bs, tc.extended)
		if err := bw.Flush(); err != nil {
			t.Fatal(err)
		}
		if got := sb.String(); got != tc.want {
			t.Errorf("writeBodyStructure(extended=%v) = %v, want %v", tc.extended, got, tc.want)
		}
	}
}

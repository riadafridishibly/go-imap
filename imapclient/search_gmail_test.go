package imapclient

import (
	"bufio"
	"bytes"
	"testing"

	"github.com/emersion/go-imap/v2"
	"github.com/emersion/go-imap/v2/internal/imapwire"
)

func TestWriteSearchKey_gmail(t *testing.T) {
	tests := []struct {
		criteria imap.SearchCriteria
		want     string
	}{
		{imap.SearchCriteria{GmailMsgID: 1853070351386201256}, "X-GM-MSGID 1853070351386201256"},
		{imap.SearchCriteria{GmailThreadID: 18446744073709551615}, "X-GM-THRID 18446744073709551615"},
		{
			imap.SearchCriteria{
				Flag:          []imap.Flag{imap.FlagSeen},
				GmailMsgID:    1,
				GmailThreadID: 2,
				Not:           []imap.SearchCriteria{{GmailMsgID: 3}},
			},
			"SEEN X-GM-MSGID 1 X-GM-THRID 2 NOT (X-GM-MSGID 3)",
		},
	}
	for _, tc := range tests {
		var buf bytes.Buffer
		bw := bufio.NewWriter(&buf)
		writeSearchKey(imapwire.NewEncoder(bw, imapwire.ConnSideClient), &tc.criteria)
		bw.Flush()
		if got := buf.String(); got != tc.want {
			t.Errorf("writeSearchKey(%+v) = %q, want %q", tc.criteria, got, tc.want)
		}
	}
}

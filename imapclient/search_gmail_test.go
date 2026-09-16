package imapclient

import (
	"bufio"
	"bytes"
	"testing"

	"github.com/emersion/go-imap/v2"
	"github.com/emersion/go-imap/v2/internal/imapwire"
)

func TestWriteSearchKey_gmail(t *testing.T) {
	and := func(a, b imap.SearchCriteria) imap.SearchCriteria {
		a.And(&b)
		return a
	}
	tests := []struct {
		criteria imap.SearchCriteria
		want     string
	}{
		{imap.SearchCriteria{GmailMsgID: []uint64{1853070351386201256}}, "X-GM-MSGID 1853070351386201256"},
		{imap.SearchCriteria{GmailThreadID: []uint64{18446744073709551615}}, "X-GM-THRID 18446744073709551615"},
		{
			imap.SearchCriteria{
				Flag:          []imap.Flag{imap.FlagSeen},
				GmailMsgID:    []uint64{1},
				GmailThreadID: []uint64{2},
				Not:           []imap.SearchCriteria{{GmailMsgID: []uint64{3}}},
			},
			"SEEN X-GM-MSGID 1 X-GM-THRID 2 NOT (X-GM-MSGID 3)",
		},
		{
			and(
				imap.SearchCriteria{Flag: []imap.Flag{imap.FlagSeen}, GmailMsgID: []uint64{1}},
				imap.SearchCriteria{GmailMsgID: []uint64{2}, GmailThreadID: []uint64{3}},
			),
			"SEEN X-GM-MSGID 1 X-GM-MSGID 2 X-GM-THRID 3",
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

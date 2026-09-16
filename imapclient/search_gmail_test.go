package imapclient

import (
	"bufio"
	"bytes"
	"reflect"
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

func TestWriteSearchKey_gmailLabels(t *testing.T) {
	tests := []struct {
		name     string
		criteria imap.SearchCriteria
		utf8     bool
		want     string
	}{
		{"user", imap.SearchCriteria{GmailLabels: []string{"work/project x"}}, false, `X-GM-LABELS "work/project x"`},
		{"system", imap.SearchCriteria{GmailLabels: []string{`\Inbox`}}, false, `X-GM-LABELS "\\Inbox"`},
		{"ampersand", imap.SearchCriteria{GmailLabels: []string{"R&D"}}, false, `X-GM-LABELS "R&D"`},
		{"non-ascii", imap.SearchCriteria{GmailLabels: []string{"Übung"}}, false, "X-GM-LABELS {6+}\r\nÜbung"},
		{"utf8", imap.SearchCriteria{GmailLabels: []string{"Übung"}}, true, `X-GM-LABELS "Übung"`},
		{
			"nested",
			imap.SearchCriteria{
				GmailLabels: []string{"a", "b"},
				Not:         []imap.SearchCriteria{{GmailLabels: []string{"c"}}},
			},
			false,
			`X-GM-LABELS "a" X-GM-LABELS "b" NOT (X-GM-LABELS "c")`,
		},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			var buf bytes.Buffer
			bw := bufio.NewWriter(&buf)
			enc := imapwire.NewEncoder(bw, imapwire.ConnSideClient)
			enc.QuotedUTF8 = tc.utf8
			enc.LiteralMinus = true // Gmail advertises LITERAL-
			writeSearchKey(enc, &tc.criteria)
			bw.Flush()
			if got := buf.String(); got != tc.want {
				t.Errorf("writeSearchKey() = %q, want %q", got, tc.want)
			}
		})
	}

	a := imap.SearchCriteria{GmailLabels: []string{"a"}}
	a.And(&imap.SearchCriteria{GmailLabels: []string{"b"}})
	if want := []string{"a", "b"}; !reflect.DeepEqual(a.GmailLabels, want) {
		t.Errorf("And() GmailLabels = %v, want %v", a.GmailLabels, want)
	}
}

// A non-ASCII label makes the client send CHARSET UTF-8 outside UTF-8 mode.
func TestSearchCriteriaIsASCII_gmailLabels(t *testing.T) {
	nonASCII := imap.SearchCriteria{GmailLabels: []string{"Übung"}}
	tests := []struct {
		name     string
		criteria imap.SearchCriteria
		want     bool
	}{
		{"ascii", imap.SearchCriteria{GmailLabels: []string{`\Inbox`, "R&D"}}, true},
		{"non-ascii", nonASCII, false},
		{"not", imap.SearchCriteria{Not: []imap.SearchCriteria{nonASCII}}, false},
		{"or", imap.SearchCriteria{Or: [][2]imap.SearchCriteria{{{}, nonASCII}}}, false},
	}
	for _, tc := range tests {
		if got := searchCriteriaIsASCII(&tc.criteria); got != tc.want {
			t.Errorf("%s: searchCriteriaIsASCII() = %v, want %v", tc.name, got, tc.want)
		}
	}
}

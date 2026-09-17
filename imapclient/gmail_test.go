package imapclient_test

import (
	"bufio"
	"io"
	"net"
	"reflect"
	"strings"
	"testing"

	"github.com/emersion/go-imap/v2"
	"github.com/emersion/go-imap/v2/imapclient"
)

// newRawServerClient returns a client connected to a server that answers the
// nth command with the lines in resps[n], if any, followed by a tagged OK.
// If the last line of resps[n] starts with "TAG ", it replaces the tagged OK,
// with TAG set to the command's tag. Each command, without its tag, is sent on
// the returned channel.
func newRawServerClient(t *testing.T, resps ...string) (*imapclient.Client, <-chan string) {
	return newRawServerClientCaps(t, "IMAP4rev1 X-GM-EXT-1", resps...)
}

// newRawServerClientCaps is newRawServerClient with caps in the greeting.
func newRawServerClientCaps(t *testing.T, caps string, resps ...string) (*imapclient.Client, <-chan string) {
	clientConn, serverConn := net.Pipe()
	cmds := make(chan string, len(resps))
	go func() {
		defer close(cmds)
		// Without CAPABILITY in the greeting, the client sends its own
		// CAPABILITY command, which would race with the test's commands
		io.WriteString(serverConn, "* OK [CAPABILITY "+caps+"] ready\r\n")
		br := bufio.NewReader(serverConn)
		for _, resp := range resps {
			line, err := br.ReadString('\n')
			if err != nil {
				return
			}
			tag, cmd, _ := strings.Cut(strings.TrimSuffix(line, "\r\n"), " ")
			cmds <- cmd
			status := tag + " OK done"
			if i := strings.LastIndex(resp, "\n") + 1; strings.HasPrefix(resp[i:], "TAG ") {
				resp, status = strings.TrimSuffix(resp[:i], "\r\n"), tag+resp[i+len("TAG"):]
			}
			if resp != "" {
				resp += "\r\n"
			}
			io.WriteString(serverConn, resp+status+"\r\n")
		}
	}()
	client := imapclient.New(clientConn, nil)
	t.Cleanup(func() {
		client.Close()
		serverConn.Close()
	})
	return client, cmds
}

// newGmailClient is newRawServerClient answering one command with resp. If
// utf8 is set, the client first enables UTF8=ACCEPT.
func newGmailClient(t *testing.T, utf8 bool, resp string) (*imapclient.Client, <-chan string) {
	if !utf8 {
		return newRawServerClient(t, resp)
	}
	client, cmds := newRawServerClient(t, "* ENABLED UTF8=ACCEPT", resp)
	if _, err := client.Enable(imap.CapUTF8Accept).Wait(); err != nil {
		t.Fatalf("Enable() = %v", err)
	}
	<-cmds
	return client, cmds
}

func TestFetch_gmailIDs(t *testing.T) {
	tests := []struct {
		name string
		resp string
		want *imapclient.FetchMessageBuffer // nil means an error is expected
	}{
		{
			// Gmail sends UID after the X-GM items
			name: "gmail",
			resp: "* 2 FETCH (X-GM-THRID 1873891894758975366 X-GM-MSGID 1873892116380601938 UID 2)",
			want: &imapclient.FetchMessageBuffer{
				SeqNum:        2,
				UID:           2,
				GmailMsgID:    1873892116380601938,
				GmailThreadID: 1873891894758975366,
			},
		},
		{
			name: "above MaxInt64",
			resp: "* 2 FETCH (UID 2 X-GM-MSGID 9223372036854775808 X-GM-THRID 18446744073709551615)",
			want: &imapclient.FetchMessageBuffer{
				SeqNum:        2,
				UID:           2,
				GmailMsgID:    9223372036854775808,
				GmailThreadID: 18446744073709551615,
			},
		},
		{
			name: "overflow",
			resp: "* 2 FETCH (UID 2 X-GM-MSGID 18446744073709551616)",
		},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			client, cmds := newRawServerClient(t, tc.resp)
			msgs, err := client.Fetch(imap.UIDSetNum(2), &imap.FetchOptions{
				GmailMsgID:    true,
				GmailThreadID: true,
			}).Collect()
			if cmd, want := <-cmds, "UID FETCH 2 (UID X-GM-MSGID X-GM-THRID)"; cmd != want {
				t.Errorf("sent %q, want %q", cmd, want)
			}
			if tc.want == nil {
				if err == nil || !strings.Contains(err.Error(), "uint64") {
					t.Fatalf("Collect() = %v, %v; want a uint64 error", msgs, err)
				}
				return
			}
			if err != nil {
				t.Fatalf("Collect() = %v", err)
			}
			if len(msgs) != 1 || !reflect.DeepEqual(msgs[0], tc.want) {
				t.Errorf("Collect() = %+v, want [%+v]", msgs, tc.want)
			}
		})
	}
}

func TestFetch_gmailLabels(t *testing.T) {
	tests := []struct {
		name    string
		utf8    bool // ENABLE UTF8=ACCEPT first
		labels  string
		want    []string
		wantErr string
	}{
		// Gmail transcripts, default mode
		{name: "empty", labels: `()`},
		{name: "system", labels: `("\\Sent")`, want: []string{`\Sent`}},
		{name: "two system", labels: `("\\Important" "\\Inbox")`, want: []string{`\Important`, `\Inbox`}},
		{name: "utf7", labels: `(&ANw-bung)`, want: []string{"Übung"}},
		{
			name:   "mixed",
			labels: `("\\Starred" "\\foo" "work/project x" &ANw-bung ProbeImplicit R&-D)`,
			want:   []string{`\Starred`, `\foo`, "work/project x", "Übung", "ProbeImplicit", "R&D"},
		},
		// Gmail transcripts, UTF-8 mode
		{
			name:   "utf8 mixed",
			utf8:   true,
			labels: `("\\foo" "work/project x" "Übung" ProbeImplicit)`,
			want:   []string{`\foo`, "work/project x", "Übung", "ProbeImplicit"},
		},
		{name: "utf8 ampersand", utf8: true, labels: `(R&D)`, want: []string{"R&D"}},
		// Synthetic
		{name: "NIL", labels: `NIL`},
		{name: "literal", labels: "({3}\r\nfoo \"\\\\Inbox\")", want: []string{"foo", `\Inbox`}},
		{name: "leading space", labels: `( "\\Inbox" foo)`, want: []string{`\Inbox`, "foo"}},
		{name: "bare system", labels: `(\Inbox foo)`, want: []string{`\Inbox`, "foo"}},
		{name: "invalid utf7", labels: `(R&D)`, wantErr: "utf7"},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			client, cmds := newGmailClient(t, tc.utf8, "* 1 FETCH (X-GM-LABELS "+tc.labels+" UID 1)")
			msgs, err := client.Fetch(imap.UIDSetNum(1), &imap.FetchOptions{GmailLabels: true}).Collect()
			if cmd, want := <-cmds, "UID FETCH 1 (UID X-GM-LABELS)"; cmd != want {
				t.Errorf("sent %q, want %q", cmd, want)
			}
			if tc.wantErr != "" {
				if err == nil || !strings.Contains(err.Error(), tc.wantErr) {
					t.Fatalf("Collect() = %v, %v; want a %s error", msgs, err, tc.wantErr)
				}
				return
			}
			if err != nil {
				t.Fatalf("Collect() = %v", err)
			}
			want := &imapclient.FetchMessageBuffer{SeqNum: 1, UID: 1, GmailLabels: tc.want}
			if len(msgs) != 1 || !reflect.DeepEqual(msgs[0], want) {
				t.Errorf("Collect() = %+v, want [%+v]", msgs, want)
			}
		})
	}
}

func TestStoreGmailLabels(t *testing.T) {
	tests := []struct {
		name    string
		utf8    bool // ENABLE UTF8=ACCEPT first
		numSet  imap.NumSet
		store   imap.StoreGmailLabels
		options *imap.StoreOptions
		wantCmd string
		resp    string
		want    *imapclient.FetchMessageBuffer // nil means no message
	}{
		{
			name:    "add",
			numSet:  imap.UIDSetNum(1),
			store:   imap.StoreGmailLabels{Op: imap.StoreFlagsAdd, Labels: []string{`\Inbox`, "work/project x", "Übung", "R&D"}},
			wantCmd: `UID STORE 1 +X-GM-LABELS ("\\Inbox" "work/project x" "&ANw-bung" "R&-D")`,
			resp:    `* 1 FETCH (X-GM-LABELS ("\\Inbox" "work/project x" &ANw-bung R&-D) MODSEQ (20494) UID 1)`,
			want: &imapclient.FetchMessageBuffer{
				SeqNum:      1,
				UID:         1,
				ModSeq:      20494,
				GmailLabels: []string{`\Inbox`, "work/project x", "Übung", "R&D"},
			},
		},
		{
			name:    "utf8",
			utf8:    true,
			numSet:  imap.UIDSetNum(1),
			store:   imap.StoreGmailLabels{Op: imap.StoreFlagsAdd, Labels: []string{"Übung", "R&D"}},
			wantCmd: `UID STORE 1 +X-GM-LABELS ("Übung" "R&D")`,
			resp:    `* 1 FETCH (X-GM-LABELS ("Übung" R&D) UID 1)`,
			want:    &imapclient.FetchMessageBuffer{SeqNum: 1, UID: 1, GmailLabels: []string{"Übung", "R&D"}},
		},
		{
			name:    "silent",
			numSet:  imap.UIDSetNum(1),
			store:   imap.StoreGmailLabels{Op: imap.StoreFlagsDel, Silent: true, Labels: []string{"work/project x"}},
			wantCmd: `UID STORE 1 -X-GM-LABELS.SILENT ("work/project x")`,
		},
		{
			// Gmail transcript with a stale value
			name:    "unchangedsince",
			numSet:  imap.UIDSetNum(1),
			store:   imap.StoreGmailLabels{Op: imap.StoreFlagsSet, Labels: []string{"work/project x"}},
			options: &imap.StoreOptions{UnchangedSince: 1},
			wantCmd: `UID STORE 1 (UNCHANGEDSINCE 1) X-GM-LABELS ("work/project x")`,
			resp:    `* 1 FETCH (X-GM-LABELS ("\\foo" &ANw-bung ProbeImplicit) MODSEQ (20572) UID 1)`,
			want: &imapclient.FetchMessageBuffer{
				SeqNum:      1,
				UID:         1,
				ModSeq:      20572,
				GmailLabels: []string{`\foo`, "Übung", "ProbeImplicit"},
			},
		},
		{
			// Gmail sends a second FETCH for the \Flagged change, which is
			// not part of the command's results
			name:    "starred",
			numSet:  imap.UIDSetNum(1),
			store:   imap.StoreGmailLabels{Op: imap.StoreFlagsAdd, Labels: []string{`\Starred`}},
			wantCmd: `UID STORE 1 +X-GM-LABELS ("\\Starred")`,
			resp: "* 1 FETCH (X-GM-LABELS (\"\\\\Starred\") MODSEQ (20509) UID 1)\r\n" +
				`* 1 FETCH (UID 1 MODSEQ (20509) FLAGS (\Flagged))`,
			want: &imapclient.FetchMessageBuffer{SeqNum: 1, UID: 1, ModSeq: 20509, GmailLabels: []string{`\Starred`}},
		},
		{
			// .SILENT does not suppress the FETCH for \Flagged
			name:    "starred silent",
			numSet:  imap.UIDSetNum(1),
			store:   imap.StoreGmailLabels{Op: imap.StoreFlagsDel, Silent: true, Labels: []string{`\Starred`}},
			wantCmd: `UID STORE 1 -X-GM-LABELS.SILENT ("\\Starred")`,
			resp:    `* 1 FETCH (UID 1 MODSEQ (20510) FLAGS (\Seen))`,
			want:    &imapclient.FetchMessageBuffer{SeqNum: 1, UID: 1, ModSeq: 20510, Flags: []imap.Flag{imap.FlagSeen}},
		},
		{
			name:    "clear by seq",
			numSet:  imap.SeqSetNum(1),
			store:   imap.StoreGmailLabels{Op: imap.StoreFlagsSet},
			wantCmd: `STORE 1 X-GM-LABELS ()`,
			resp:    `* 1 FETCH (X-GM-LABELS ())`,
			want:    &imapclient.FetchMessageBuffer{SeqNum: 1},
		},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			client, cmds := newGmailClient(t, tc.utf8, tc.resp)
			msgs, err := client.StoreGmailLabels(tc.numSet, &tc.store, tc.options).Collect()
			if cmd := <-cmds; cmd != tc.wantCmd {
				t.Errorf("sent %q, want %q", cmd, tc.wantCmd)
			}
			if err != nil {
				t.Fatalf("Collect() = %v", err)
			}
			var want []*imapclient.FetchMessageBuffer
			if tc.want != nil {
				want = append(want, tc.want)
			}
			if !reflect.DeepEqual(msgs, want) {
				t.Errorf("Collect() = %+v, want %+v", msgs, want)
			}
		})
	}
}

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
// nth command with resps[n] followed by a tagged OK. Each command, without its
// tag, is sent on the returned channel.
func newRawServerClient(t *testing.T, resps ...string) (*imapclient.Client, <-chan string) {
	clientConn, serverConn := net.Pipe()
	cmds := make(chan string, len(resps))
	go func() {
		defer close(cmds)
		// Without CAPABILITY in the greeting, the client sends its own
		// CAPABILITY command, which would race with the test's commands
		io.WriteString(serverConn, "* OK [CAPABILITY IMAP4rev1 X-GM-EXT-1] ready\r\n")
		br := bufio.NewReader(serverConn)
		for _, resp := range resps {
			line, err := br.ReadString('\n')
			if err != nil {
				return
			}
			tag, cmd, _ := strings.Cut(strings.TrimSuffix(line, "\r\n"), " ")
			cmds <- cmd
			io.WriteString(serverConn, resp+"\r\n"+tag+" OK done\r\n")
		}
	}()
	client := imapclient.New(clientConn, nil)
	t.Cleanup(func() {
		client.Close()
		serverConn.Close()
	})
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
			resps := []string{"* 1 FETCH (X-GM-LABELS " + tc.labels + " UID 1)"}
			if tc.utf8 {
				resps = append([]string{"* ENABLED UTF8=ACCEPT"}, resps...)
			}
			client, cmds := newRawServerClient(t, resps...)
			if tc.utf8 {
				if _, err := client.Enable(imap.CapUTF8Accept).Wait(); err != nil {
					t.Fatalf("Enable() = %v", err)
				}
				<-cmds
			}
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

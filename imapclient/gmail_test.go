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
// first command with resp followed by a tagged OK.
func newRawServerClient(t *testing.T, resp string) *imapclient.Client {
	clientConn, serverConn := net.Pipe()
	go func() {
		io.WriteString(serverConn, "* OK ready\r\n")
		line, err := bufio.NewReader(serverConn).ReadString('\n')
		if err != nil {
			return
		}
		tag, _, _ := strings.Cut(line, " ")
		io.WriteString(serverConn, resp+"\r\n"+tag+" OK done\r\n")
	}()
	client := imapclient.New(clientConn, nil)
	t.Cleanup(func() {
		client.Close()
		serverConn.Close()
	})
	return client
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
			client := newRawServerClient(t, tc.resp)
			msgs, err := client.Fetch(imap.UIDSetNum(2), &imap.FetchOptions{
				GmailMsgID:    true,
				GmailThreadID: true,
			}).Collect()
			if tc.want == nil {
				if err == nil {
					t.Fatalf("Collect() = %v, want an error", msgs)
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

package imapserver

import (
	"bufio"
	"net"
	"strings"
	"testing"

	"github.com/emersion/go-imap/v2"
)

func TestWriteCopyOK(t *testing.T) {
	tests := []struct {
		name string
		data *imap.CopyData
		want string
	}{
		{"no data", nil, "T1 OK COPY completed\r\n"},
		{"copied", &imap.CopyData{UIDValidity: 5, SourceUIDs: imap.UIDSetNum(1, 2), DestUIDs: imap.UIDSetNum(7, 8)}, "T1 OK [COPYUID 5 1:2 7:8] COPY completed\r\n"},
		{"nothing copied", &imap.CopyData{UIDValidity: 5}, "T1 OK COPY completed\r\n"},
		{"no destination UIDs", &imap.CopyData{UIDValidity: 5, SourceUIDs: imap.UIDSetNum(1)}, "T1 OK COPY completed\r\n"},
		{"no source UIDs", &imap.CopyData{UIDValidity: 5, DestUIDs: imap.UIDSetNum(7)}, "T1 OK COPY completed\r\n"},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			conn, _ := net.Pipe()
			defer conn.Close()
			var sb strings.Builder
			c := &Conn{server: &Server{}, conn: conn, bw: bufio.NewWriter(&sb)}
			if err := c.writeCopyOK("T1", tc.data); err != nil {
				t.Fatalf("writeCopyOK() = %v", err)
			}
			if err := c.bw.Flush(); err != nil {
				t.Fatal(err)
			}
			if got := sb.String(); got != tc.want {
				t.Errorf("writeCopyOK() wrote %q, want %q", got, tc.want)
			}
		})
	}
}

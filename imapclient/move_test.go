package imapclient_test

import (
	"slices"
	"strings"
	"testing"

	"github.com/emersion/go-imap/v2"
)

func TestMove(t *testing.T) {
	uids := imap.UIDSetNum(10, 11)
	copyOK := "TAG OK [COPYUID 7 10:11 20:21] done"
	tests := []struct {
		name     string
		caps     string
		numSet   imap.NumSet
		resps    []string
		want     []string // commands the server receives
		wantErr  string
		wantDest string // MoveData.DestUIDs
	}{
		{
			name:     "MOVE",
			caps:     "IMAP4rev1 MOVE UIDPLUS",
			numSet:   uids,
			resps:    []string{"* OK [COPYUID 7 10:11 20:21] moved\r\n* 1 EXPUNGE\r\n* 1 EXPUNGE"},
			want:     []string{`UID MOVE 10:11 "dest"`},
			wantDest: "20:21",
		},
		{
			name:     "IMAP4rev2",
			caps:     "IMAP4rev2",
			numSet:   uids,
			want:     []string{`UID MOVE 10:11 "dest"`},
			wantDest: "",
		},
		{
			name:     "fallback",
			caps:     "IMAP4rev1 UIDPLUS",
			numSet:   uids,
			resps:    []string{copyOK, "", "* 1 EXPUNGE\r\n* 1 EXPUNGE"},
			want:     []string{`UID COPY 10:11 "dest"`, `UID STORE 10:11 +FLAGS.SILENT (\Deleted)`, "UID EXPUNGE 10:11"},
			wantDest: "20:21",
		},
		{
			name:     "fallback with sequence numbers",
			caps:     "IMAP4rev1 UIDPLUS",
			numSet:   imap.SeqSetNum(1, 2),
			resps:    []string{"* SEARCH 10 11", copyOK, "", "* 1 EXPUNGE\r\n* 1 EXPUNGE"},
			want:     []string{"UID SEARCH 1:2", `UID COPY 10:11 "dest"`, `UID STORE 10:11 +FLAGS.SILENT (\Deleted)`, "UID EXPUNGE 10:11"},
			wantDest: "20:21",
		},
		{
			name:    "fallback, COPY fails",
			caps:    "IMAP4rev1 UIDPLUS",
			numSet:  uids,
			resps:   []string{"TAG NO [TRYCREATE] No folder dest (Failure)"},
			want:    []string{`UID COPY 10:11 "dest"`},
			wantErr: "NO [TRYCREATE]",
		},
		{
			name:    "fallback, STORE fails",
			caps:    "IMAP4rev1 UIDPLUS",
			numSet:  uids,
			resps:   []string{copyOK, "TAG NO [READ-ONLY] mailbox is read-only"},
			want:    []string{`UID COPY 10:11 "dest"`, `UID STORE 10:11 +FLAGS.SILENT (\Deleted)`},
			wantErr: "messages copied, but not flagged",
		},
		{
			name:    "fallback, UID EXPUNGE fails",
			caps:    "IMAP4rev1 UIDPLUS",
			numSet:  uids,
			resps:   []string{copyOK, "", "TAG NO expunge failed"},
			want:    []string{`UID COPY 10:11 "dest"`, `UID STORE 10:11 +FLAGS.SILENT (\Deleted)`, "UID EXPUNGE 10:11"},
			wantErr: "not expunged",
		},
		{
			// Plain EXPUNGE would remove other \Deleted messages
			name:    "neither MOVE nor UIDPLUS",
			caps:    "IMAP4rev1",
			numSet:  uids,
			wantErr: "neither MOVE nor UIDPLUS",
		},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			// A NOOP marks the end of what Move sent; spare replies let
			// unexpected commands through so they show up in the diff
			resps := append(slices.Clone(tc.resps), make([]string, len(tc.want)+4-len(tc.resps))...)
			client, cmds := newRawServerClientCaps(t, tc.caps, resps...)

			data, err := client.Move(tc.numSet, "dest").Wait()
			if tc.wantErr != "" {
				if err == nil || !strings.Contains(err.Error(), tc.wantErr) {
					t.Errorf("Move() = %v, want an error containing %q", err, tc.wantErr)
				}
			} else if err != nil {
				t.Errorf("Move() = %v", err)
			} else if dest := data.DestUIDs; (dest == nil && tc.wantDest != "") || (dest != nil && dest.String() != tc.wantDest) {
				t.Errorf("Move() DestUIDs = %v, want %q", dest, tc.wantDest)
			}

			if err := client.Noop().Wait(); err != nil {
				t.Fatalf("Noop() = %v", err)
			}
			var got []string
			for cmd := range cmds {
				if cmd == "NOOP" {
					break
				}
				got = append(got, cmd)
			}
			if !slices.Equal(got, tc.want) {
				t.Errorf("sent %q, want %q", got, tc.want)
			}
		})
	}
}

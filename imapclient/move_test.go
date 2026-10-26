package imapclient_test

import (
	"fmt"
	"slices"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/emersion/go-imap/v2"
	"github.com/emersion/go-imap/v2/imapclient"
)

func TestMove(t *testing.T) {
	uids := imap.UIDSetNum(10, 11)
	var dynamic imap.UIDSet
	dynamic.AddRange(1, 0) // "1:*"
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
			// COPYUID tells which messages the sequence numbers matched
			name:     "fallback with sequence numbers",
			caps:     "IMAP4rev1 UIDPLUS",
			numSet:   imap.SeqSetNum(1, 2),
			resps:    []string{copyOK, "", "* 1 EXPUNGE\r\n* 1 EXPUNGE"},
			want:     []string{`COPY 1:2 "dest"`, `UID STORE 10:11 +FLAGS.SILENT (\Deleted)`, "UID EXPUNGE 10:11"},
			wantDest: "20:21",
		},
		{
			name:     "fallback with a dynamic set",
			caps:     "IMAP4rev1 UIDPLUS",
			numSet:   dynamic,
			resps:    []string{"TAG OK [COPYUID 7 1:40 100:139] done", "", "* 1 EXPUNGE"},
			want:     []string{`UID COPY 1:* "dest"`, `UID STORE 1:40 +FLAGS.SILENT (\Deleted)`, "UID EXPUNGE 1:40"},
			wantDest: "100:139",
		},
		{
			// Some servers send a dynamic COPYUID after MOVE (#749)
			name:    "fallback with a dynamic set and a dynamic COPYUID",
			caps:    "IMAP4rev1 UIDPLUS",
			numSet:  dynamic,
			resps:   []string{"TAG OK [COPYUID 7 1:* 100:*] done"},
			want:    []string{`UID COPY 1:* "dest"`},
			wantErr: "without COPYUID",
		},
		{
			// 10:11 can match a message added after COPY
			name:    "fallback, COPY without COPYUID",
			caps:    "IMAP4rev1 UIDPLUS",
			numSet:  uids,
			resps:   []string{""},
			want:    []string{`UID COPY 10:11 "dest"`},
			wantErr: "without COPYUID",
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
			name:     "fallback, STORE fails",
			caps:     "IMAP4rev1 UIDPLUS",
			numSet:   uids,
			resps:    []string{copyOK, "TAG NO [READ-ONLY] mailbox is read-only"},
			want:     []string{`UID COPY 10:11 "dest"`, `UID STORE 10:11 +FLAGS.SILENT (\Deleted)`},
			wantErr:  "messages copied, but not flagged",
			wantDest: "20:21",
		},
		{
			name:     "fallback, UID EXPUNGE fails",
			caps:     "IMAP4rev1 UIDPLUS",
			numSet:   uids,
			resps:    []string{copyOK, "", "TAG NO expunge failed"},
			want:     []string{`UID COPY 10:11 "dest"`, `UID STORE 10:11 +FLAGS.SILENT (\Deleted)`, "UID EXPUNGE 10:11"},
			wantErr:  "not expunged",
			wantDest: "20:21",
		},
		{
			// Plain EXPUNGE would remove other \Deleted messages
			name:    "neither MOVE nor UIDPLUS",
			caps:    "IMAP4rev1",
			numSet:  uids,
			wantErr: "neither MOVE nor UIDPLUS",
		},
		{
			name:    "capabilities unknown",
			numSet:  uids,
			resps:   []string{"TAG BAD unknown command"},
			want:    []string{"CAPABILITY"},
			wantErr: "capabilities unknown",
		},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			// Spare replies let unexpected commands through to the diff
			resps := append(slices.Clone(tc.resps), make([]string, max(0, len(tc.want)+4-len(tc.resps)))...)
			client, cmds := newRawServerClientCaps(t, tc.caps, resps...)

			moveCmd := client.Move(tc.numSet, "dest")
			data, err := moveCmd.Wait()
			again := make(chan error, 1)
			go func() {
				_, err := moveCmd.Wait()
				again <- err
			}()
			select {
			case err2 := <-again:
				if (err2 == nil) != (err == nil) {
					t.Errorf("second Wait() = %v, want %v", err2, err)
				}
			case <-time.After(5 * time.Second):
				t.Fatal("second Wait() blocked")
			}
			if tc.wantErr != "" {
				if err == nil || !strings.Contains(err.Error(), tc.wantErr) {
					t.Errorf("Move() = %v, want an error containing %q", err, tc.wantErr)
				}
			} else if err != nil {
				t.Errorf("Move() = %v", err)
			}
			if dest := data.DestUIDs; (dest == nil && tc.wantDest != "") || (dest != nil && dest.String() != tc.wantDest) {
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

// sentUntilNoop sends NOOP and returns the commands the server received
// before it.
func sentUntilNoop(t *testing.T, client *imapclient.Client, cmds <-chan string) []string {
	t.Helper()
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
	return got
}

func TestMove_readOnly(t *testing.T) {
	copyOK := "TAG OK [COPYUID 7 10:11 20:21] done"
	t.Run("EXAMINE", func(t *testing.T) {
		// Spare replies let unexpected commands through to the diff
		client, cmds := newRawServerClientCaps(t, "IMAP4rev1 UIDPLUS", "TAG OK [READ-ONLY] done", "", "", "", "")
		if _, err := client.Select("INBOX", &imap.SelectOptions{ReadOnly: true}).Wait(); err != nil {
			t.Fatalf("Select() = %v", err)
		}
		if _, err := client.Move(imap.UIDSetNum(10, 11), "dest").Wait(); err == nil || !strings.Contains(err.Error(), "read-only") {
			t.Errorf("Move() = %v, want a read-only error", err)
		}
		if got, want := sentUntilNoop(t, client, cmds), []string{"EXAMINE INBOX"}; !slices.Equal(got, want) {
			t.Errorf("sent %q, want %q", got, want)
		}
	})
	// The read-only state may be stale while another SELECT is pending
	t.Run("SELECT pending", func(t *testing.T) {
		client, cmds := newRawServerClientCaps(t, "IMAP4rev1 UIDPLUS", "TAG OK [READ-ONLY] done", "TAG OK [READ-WRITE] done", copyOK, "", "", "")
		if _, err := client.Select("INBOX", &imap.SelectOptions{ReadOnly: true}).Wait(); err != nil {
			t.Fatalf("Select() = %v", err)
		}
		client.Select("Work", nil)
		if _, err := client.Move(imap.UIDSetNum(10, 11), "dest").Wait(); err != nil {
			t.Errorf("Move() = %v", err)
		}
		want := []string{"EXAMINE INBOX", `SELECT "Work"`, `UID COPY 10:11 "dest"`, `UID STORE 10:11 +FLAGS.SILENT (\Deleted)`, "UID EXPUNGE 10:11"}
		if got := sentUntilNoop(t, client, cmds); !slices.Equal(got, want) {
			t.Errorf("sent %q, want %q", got, want)
		}
	})
}

// STORE and EXPUNGE are not sent once another mailbox may be selected.
func TestMove_selectAfterCopy(t *testing.T) {
	var client *imapclient.Client
	options := &imapclient.Options{
		UnilateralDataHandler: &imapclient.UnilateralDataHandler{
			Mailbox: func(*imapclient.UnilateralDataMailbox) {
				client.Select("other", nil)
			},
		},
	}
	// The EXISTS sent with COPY's reply makes the handler send SELECT
	client, cmds := newRawServerClientOptions(t, "IMAP4rev1 UIDPLUS", options,
		"* 3 EXISTS\r\nTAG OK [COPYUID 7 10:11 20:21] done", "", "", "", "")
	_, err := client.Move(imap.UIDSetNum(10, 11), "dest").Wait()
	if err == nil || !strings.Contains(err.Error(), "another mailbox was selected") {
		t.Errorf("Move() = %v, want an error about the selected mailbox", err)
	}
	if got, want := sentUntilNoop(t, client, cmds), []string{`UID COPY 10:11 "dest"`, `SELECT "other"`}; !slices.Equal(got, want) {
		t.Errorf("sent %q, want %q", got, want)
	}
}

// The fallback reports expunges like MOVE does.
func TestMove_expungeHandler(t *testing.T) {
	tests := []struct {
		name  string
		caps  string
		resps []string
	}{
		{"MOVE", "IMAP4rev1 MOVE UIDPLUS", []string{"* 1 EXPUNGE\r\n* 1 EXPUNGE"}},
		{"fallback", "IMAP4rev1 UIDPLUS", []string{"TAG OK [COPYUID 7 10:11 20:21] done", "", "* 1 EXPUNGE\r\n* 1 EXPUNGE"}},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			var (
				mutex   sync.Mutex
				expunge []uint32
			)
			options := &imapclient.Options{
				UnilateralDataHandler: &imapclient.UnilateralDataHandler{
					Expunge: func(seqNum uint32) {
						mutex.Lock()
						expunge = append(expunge, seqNum)
						mutex.Unlock()
					},
				},
			}
			client, _ := newRawServerClientOptions(t, tc.caps, options, tc.resps...)
			if _, err := client.Move(imap.UIDSetNum(10, 11), "dest").Wait(); err != nil {
				t.Fatalf("Move() = %v", err)
			}
			mutex.Lock()
			defer mutex.Unlock()
			if want := []uint32{1, 1}; !slices.Equal(expunge, want) {
				t.Errorf("Expunge handler got %v, want %v", expunge, want)
			}
		})
	}
}

// The fallback against imapserver without MOVE.
func TestMove_fallbackServer(t *testing.T) {
	for _, numSet := range []imap.NumSet{imap.SeqSetNum(1, 2), imap.UIDSetNum(1, 2)} {
		t.Run(fmt.Sprintf("%T", numSet), func(t *testing.T) {
			conn, server := newMemClientServerPair(t, imap.CapSet{imap.CapIMAP4rev1: {}, imap.CapUIDPlus: {}})
			defer server.Close()
			client := imapclient.New(conn, nil)
			defer client.Close()

			if err := client.Login(testUsername, testPassword).Wait(); err != nil {
				t.Fatalf("Login() = %v", err)
			}
			if client.Caps().Has(imap.CapMove) {
				t.Fatalf("server lists MOVE")
			}
			if err := client.Create("dest", nil).Wait(); err != nil {
				t.Fatalf("Create() = %v", err)
			}
			for range 3 {
				appendCmd := client.Append("INBOX", int64(len(simpleRawMessage)), nil)
				appendCmd.Write([]byte(simpleRawMessage))
				appendCmd.Close()
				if _, err := appendCmd.Wait(); err != nil {
					t.Fatalf("Append() = %v", err)
				}
			}
			if _, err := client.Select("INBOX", nil).Wait(); err != nil {
				t.Fatalf("Select() = %v", err)
			}

			data, err := client.Move(numSet, "dest").Wait()
			if err != nil {
				t.Fatalf("Move() = %v", err)
			}
			if got := fmt.Sprint(data.SourceUIDs, " ", data.DestUIDs); got != "1:2 1:2" {
				t.Errorf("Move() source and dest UIDs = %v, want 1:2 1:2", got)
			}
			if n := client.Mailbox().NumMessages; n != 1 {
				t.Errorf("INBOX has %v messages, want 1", n)
			}
			msgs, err := client.Fetch(imap.SeqSetNum(1), &imap.FetchOptions{UID: true}).Collect()
			if err != nil || len(msgs) != 1 || msgs[0].UID != 3 {
				t.Errorf("Fetch() = %v, %v; want UID 3 left in INBOX", msgs, err)
			}
			status, err := client.Status("dest", &imap.StatusOptions{NumMessages: true}).Wait()
			if err != nil || status.NumMessages == nil || *status.NumMessages != 2 {
				t.Errorf("Status(dest) = %v, %v; want 2 messages", status, err)
			}
		})
	}
}

package imapclient_test

import (
	"bufio"
	"io"
	"net"
	"slices"
	"strings"
	"sync"
	"testing"

	"github.com/emersion/go-imap/v2"
	"github.com/emersion/go-imap/v2/imapclient"
)

func TestExpunge(t *testing.T) {
	client, server := newClientServerPair(t, imap.ConnStateSelected)
	defer client.Close()
	defer server.Close()

	seqNums, err := client.Expunge().Collect()
	if err != nil {
		t.Fatalf("Expunge() = %v", err)
	} else if len(seqNums) != 0 {
		t.Errorf("Expunge().Collect() = %v, want []", seqNums)
	}

	seqSet := imap.SeqSetNum(1)
	storeFlags := imap.StoreFlags{
		Op:    imap.StoreFlagsAdd,
		Flags: []imap.Flag{imap.FlagDeleted},
	}
	if err := client.Store(seqSet, &storeFlags, nil).Close(); err != nil {
		t.Fatalf("Store() = %v", err)
	}

	seqNums, err = client.Expunge().Collect()
	if err != nil {
		t.Fatalf("Expunge() = %v", err)
	} else if len(seqNums) != 1 || seqNums[0] != 1 {
		t.Errorf("Expunge().Collect() = %v, want [1]", seqNums)
	}
}

// Expunges sent while an earlier command is answered reach the handler, not a
// pending ExpungeCommand.
func TestExpunge_pipelined(t *testing.T) {
	tests := []struct {
		name        string
		send        func(*imapclient.Client) *imapclient.ExpungeCommand
		resp        string // to T1 and T2
		wantHandler []uint32
		wantCmd     []uint32
	}{
		{
			name: "MOVE then EXPUNGE",
			send: func(c *imapclient.Client) *imapclient.ExpungeCommand {
				c.Move(imap.UIDSetNum(10), "dest")
				return c.Expunge()
			},
			resp:        "* 1 EXPUNGE\r\nT1 OK done\r\n* 3 EXPUNGE\r\nT2 OK done\r\n",
			wantHandler: []uint32{1},
			wantCmd:     []uint32{3},
		},
		{
			// Expunges by another session
			name: "NOOP then EXPUNGE",
			send: func(c *imapclient.Client) *imapclient.ExpungeCommand {
				c.Noop()
				return c.Expunge()
			},
			resp:        "* 2 EXPUNGE\r\nT1 OK done\r\n* 4 EXPUNGE\r\nT2 OK done\r\n",
			wantHandler: []uint32{2},
			wantCmd:     []uint32{4},
		},
		{
			name: "EXPUNGE then NOOP",
			send: func(c *imapclient.Client) *imapclient.ExpungeCommand {
				cmd := c.Expunge()
				c.Noop()
				return cmd
			},
			resp:        "* 4 EXPUNGE\r\nT1 OK done\r\n* 2 EXPUNGE\r\nT2 OK done\r\n",
			wantHandler: []uint32{2},
			wantCmd:     []uint32{4},
		},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			clientConn, serverConn := net.Pipe()
			t.Cleanup(func() { serverConn.Close() })
			go func() {
				io.WriteString(serverConn, "* OK [CAPABILITY IMAP4rev1 MOVE] ready\r\n")
				br := bufio.NewReader(serverConn)
				br.ReadString('\n') // T1
				br.ReadString('\n') // T2
				io.WriteString(serverConn, tc.resp)
				for {
					line, err := br.ReadString('\n')
					if err != nil {
						return
					}
					tag, _, _ := strings.Cut(line, " ")
					io.WriteString(serverConn, tag+" OK done\r\n")
				}
			}()

			var (
				mutex   sync.Mutex
				handler []uint32
			)
			client := imapclient.New(clientConn, &imapclient.Options{
				UnilateralDataHandler: &imapclient.UnilateralDataHandler{
					Expunge: func(seqNum uint32) {
						mutex.Lock()
						handler = append(handler, seqNum)
						mutex.Unlock()
					},
				},
			})
			t.Cleanup(func() { client.Close() })

			seqNums, err := tc.send(client).Collect()
			if err != nil {
				t.Fatalf("Expunge() = %v", err)
			}
			if !slices.Equal(seqNums, tc.wantCmd) {
				t.Errorf("Expunge().Collect() = %v, want %v", seqNums, tc.wantCmd)
			}
			// Replies are read in order, so T2's are done after this
			if err := client.Noop().Wait(); err != nil {
				t.Fatalf("Noop() = %v", err)
			}
			mutex.Lock()
			defer mutex.Unlock()
			if !slices.Equal(handler, tc.wantHandler) {
				t.Errorf("Expunge handler got %v, want %v", handler, tc.wantHandler)
			}
		})
	}
}

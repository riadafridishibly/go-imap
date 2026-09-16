package imapclient_test

import (
	"bytes"
	"crypto/tls"
	"io"
	"strings"
	"testing"

	"github.com/emersion/go-imap/v2"
	"github.com/emersion/go-imap/v2/imapclient"
)

func TestCompress(t *testing.T) {
	client, server := newClientServerPair(t, imap.ConnStateAuthenticated)
	defer client.Close()
	defer server.Close()

	if algos := client.Caps().CompressAlgorithms(); len(algos) == 0 {
		t.Skipf("COMPRESS not supported")
	}

	if err := client.Compress(nil); err != nil {
		t.Fatalf("Compress() = %v", err)
	}

	if err := client.Noop().Wait(); err != nil {
		t.Fatalf("Noop().Wait() = %v", err)
	}
}

func TestCompress_literals(t *testing.T) {
	client, server := newClientServerPair(t, imap.ConnStateAuthenticated)
	defer client.Close()
	defer server.Close()

	if algos := client.Caps().CompressAlgorithms(); len(algos) == 0 {
		t.Skipf("COMPRESS not supported")
	}

	if err := client.Compress(nil); err != nil {
		t.Fatalf("Compress() = %v", err)
	}

	testCompressedSession(t, client)
}

func TestCompress_startTLS(t *testing.T) {
	conn, server := newMemClientServerPair(t)
	defer conn.Close()
	defer server.Close()

	options := imapclient.Options{
		TLSConfig: &tls.Config{InsecureSkipVerify: true},
	}
	client, err := imapclient.NewStartTLS(conn, &options)
	if err != nil {
		t.Fatalf("NewStartTLS() = %v", err)
	}
	defer client.Close()

	if err := client.Login(testUsername, testPassword).Wait(); err != nil {
		t.Fatalf("Login().Wait() = %v", err)
	}
	if err := client.Compress(nil); err != nil {
		t.Fatalf("Compress() = %v", err)
	}

	testCompressedSession(t, client)
}

// testCompressedSession sends a large APPEND, fetches it back and runs IDLE,
// so that literals in both directions and a continuation request go through
// the compressed stream.
func testCompressedSession(t *testing.T, client *imapclient.Client) {
	if _, err := client.Select("INBOX", nil).Wait(); err != nil {
		t.Fatalf("Select().Wait() = %v", err)
	}

	body := "From: a@example.org\r\nSubject: big\r\n\r\n" + strings.Repeat("<p>hello compressed world</p>\r\n", 40000)
	appendCmd := client.Append("INBOX", int64(len(body)), nil)
	if _, err := io.Copy(appendCmd, strings.NewReader(body)); err != nil {
		t.Fatalf("AppendCommand.Write() = %v", err)
	}
	if err := appendCmd.Close(); err != nil {
		t.Fatalf("AppendCommand.Close() = %v", err)
	}
	if _, err := appendCmd.Wait(); err != nil {
		t.Fatalf("AppendCommand.Wait() = %v", err)
	}

	seqSet := imap.SeqSet{imap.SeqRange{Start: 1, Stop: 0}}
	msgs, err := client.Fetch(seqSet, &imap.FetchOptions{
		Envelope:      true,
		BodyStructure: &imap.FetchItemBodyStructure{Extended: true},
		BodySection:   []*imap.FetchItemBodySection{{}},
	}).Collect()
	if err != nil {
		t.Fatalf("Fetch().Collect() = %v", err)
	}
	if len(msgs) == 0 {
		t.Fatalf("Fetch().Collect() returned no messages")
	}
	got := msgs[len(msgs)-1].FindBodySection(&imap.FetchItemBodySection{})
	if !bytes.Equal(got, []byte(body)) {
		t.Fatalf("body section is %d bytes, want %d", len(got), len(body))
	}

	idleCmd, err := client.Idle()
	if err != nil {
		t.Fatalf("Idle() = %v", err)
	}
	if err := idleCmd.Close(); err != nil {
		t.Fatalf("IdleCommand.Close() = %v", err)
	}
	if err := idleCmd.Wait(); err != nil {
		t.Fatalf("IdleCommand.Wait() = %v", err)
	}

	if err := client.Noop().Wait(); err != nil {
		t.Fatalf("Noop().Wait() = %v", err)
	}
}

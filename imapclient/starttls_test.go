package imapclient_test

import (
	"crypto/tls"
	"strings"
	"sync"
	"testing"

	"github.com/emersion/go-imap/v2/imapclient"
)

func TestStartTLS(t *testing.T) {
	conn, server := newMemClientServerPair(t, nil)
	defer conn.Close()
	defer server.Close()

	var wire lockedBuilder
	options := imapclient.Options{
		TLSConfig:   &tls.Config{InsecureSkipVerify: true},
		DebugWriter: &wire,
	}
	client, err := imapclient.NewStartTLS(conn, &options)
	if err != nil {
		t.Fatalf("NewStartTLS() = %v", err)
	}
	defer client.Close()

	// The capabilities from before STARTTLS are discarded and requested again
	if !strings.Contains(wire.String(), "* CAPABILITY ") {
		t.Errorf("NewStartTLS() returned before CAPABILITY was answered")
	}

	if err := client.Noop().Wait(); err != nil {
		t.Fatalf("Noop().Wait() = %v", err)
	}
}

type lockedBuilder struct {
	mutex sync.Mutex
	b     strings.Builder
}

func (lb *lockedBuilder) Write(p []byte) (int, error) {
	lb.mutex.Lock()
	defer lb.mutex.Unlock()
	return lb.b.Write(p)
}

func (lb *lockedBuilder) String() string {
	lb.mutex.Lock()
	defer lb.mutex.Unlock()
	return lb.b.String()
}

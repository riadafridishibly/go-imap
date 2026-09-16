package imapwire

import (
	"bufio"
	"bytes"
	"math"
	"strconv"
	"strings"
	"testing"
)

func TestUint64(t *testing.T) {
	tests := []struct {
		in   string
		want uint64
		ok   bool
	}{
		{"0", 0, true},
		{"1876409439502535953", 1876409439502535953, true}, // Gmail X-GM-MSGID
		{"9223372036854775808", math.MaxInt64 + 1, true},
		{"18446744073709551615", math.MaxUint64, true},
		{"18446744073709551616", 0, false},
		{"", 0, false},
		{"-1", 0, false},
		{"x1", 0, false},
	}
	for _, tc := range tests {
		t.Run(tc.in, func(t *testing.T) {
			dec := NewDecoder(bufio.NewReader(strings.NewReader(tc.in+"\r\n")), ConnSideClient)
			var got uint64
			ok := dec.ExpectUint64(&got)
			if ok != tc.ok {
				t.Fatalf("ExpectUint64() = %v, err %v; want %v", ok, dec.Err(), tc.ok)
			}
			if !ok {
				if err := dec.Err(); err == nil || !strings.Contains(err.Error(), "expected uint64") {
					t.Errorf("Err() = %v, want expected uint64", err)
				}
				return
			}
			if got != tc.want {
				t.Errorf("ExpectUint64() read %v, want %v", got, tc.want)
			}

			var buf bytes.Buffer
			bw := bufio.NewWriter(&buf)
			NewEncoder(bw, ConnSideClient).Uint64(tc.want)
			bw.Flush()
			if buf.String() != strconv.FormatUint(tc.want, 10) {
				t.Errorf("Uint64(%v) wrote %q", tc.want, buf.String())
			}
		})
	}
}

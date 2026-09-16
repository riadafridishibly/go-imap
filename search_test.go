package imap

import (
	"testing"
	"time"
)

func TestSearchCriteriaAnd_size(t *testing.T) {
	tests := []struct {
		name                    string
		a, b                    SearchCriteria
		wantLarger, wantSmaller int64
	}{
		{"smaller then larger", SearchCriteria{Smaller: 100}, SearchCriteria{Larger: 5}, 5, 100},
		{"larger then smaller", SearchCriteria{Larger: 5}, SearchCriteria{Smaller: 100}, 5, 100},
		{"smaller then date", SearchCriteria{Smaller: 100}, SearchCriteria{Since: time.Unix(0, 0)}, 0, 100},
		{"two smaller", SearchCriteria{Smaller: 100}, SearchCriteria{Smaller: 50}, 0, 50},
		{"two larger", SearchCriteria{Larger: 5}, SearchCriteria{Larger: 50}, 50, 0},
	}
	for _, tc := range tests {
		tc.a.And(&tc.b)
		if tc.a.Larger != tc.wantLarger || tc.a.Smaller != tc.wantSmaller {
			t.Errorf("%s: And() = Larger %d, Smaller %d; want %d, %d", tc.name, tc.a.Larger, tc.a.Smaller, tc.wantLarger, tc.wantSmaller)
		}
	}
}

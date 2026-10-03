package retry

import (
	"testing"
	"time"
)

func TestParseRetryAfter(t *testing.T) {
	now := time.Date(2026, 10, 3, 12, 0, 0, 0, time.UTC)
	for _, tc := range []struct {
		value string
		want  time.Duration
	}{
		{"120", 2 * time.Minute},
		{"Sat, 03 Oct 2026 12:03:00 GMT", 3 * time.Minute},
		{"Sat, 03 Oct 2026 11:59:00 GMT", 0},
		{"", 0}, {"invalid", 0}, {"-1", 0},
	} {
		if got := ParseAfter(tc.value, now); got != tc.want {
			t.Errorf("%q: got %v, want %v", tc.value, got, tc.want)
		}
	}
}

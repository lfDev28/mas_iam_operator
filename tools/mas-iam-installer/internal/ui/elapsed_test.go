package ui

import (
	"testing"
	"time"
)

func TestFormatElapsedRoundsAndPicksUnits(t *testing.T) {
	tests := []struct {
		in   time.Duration
		want string
	}{
		{0, "0s"},
		{500 * time.Millisecond, "1s"},
		{12 * time.Second, "12s"},
		{59*time.Second + 600*time.Millisecond, "1m00s"},
		{time.Minute, "1m00s"},
		{9*time.Minute + 13*time.Second, "9m13s"},
		{63*time.Minute + 5*time.Second, "63m05s"},
		{-1 * time.Second, "0s"},
	}
	for _, tt := range tests {
		if got := FormatElapsed(tt.in); got != tt.want {
			t.Fatalf("FormatElapsed(%s) = %q, want %q", tt.in, got, tt.want)
		}
	}
}

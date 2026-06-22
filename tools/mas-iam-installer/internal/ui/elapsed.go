package ui

import (
	"fmt"
	"time"
)

// FormatElapsed prints a duration the way the install summary expects it:
// "12s" under a minute, "9m13s" otherwise. Rounding happens first so a
// duration like 59.6s renders as "1m00s" instead of an ambiguous "60s".
func FormatElapsed(d time.Duration) string {
	if d < 0 {
		d = 0
	}
	totalSeconds := int(d.Round(time.Second).Seconds())
	if totalSeconds < 60 {
		return fmt.Sprintf("%ds", totalSeconds)
	}
	return fmt.Sprintf("%dm%02ds", totalSeconds/60, totalSeconds%60)
}

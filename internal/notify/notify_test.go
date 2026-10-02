package notify

import (
	"testing"
	"time"
)

func TestFmtLength(t *testing.T) {
	for d, want := range map[time.Duration]string{
		6 * time.Second: "6s", 25 * time.Minute: "25m", 50 * time.Minute: "50m",
		time.Hour: "1h", 90 * time.Minute: "1h30m",
	} {
		if got := fmtLength(d); got != want {
			t.Errorf("fmtLength(%v) = %q, want %q", d, got, want)
		}
	}
}

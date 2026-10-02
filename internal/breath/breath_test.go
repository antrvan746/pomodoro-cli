package breath

import (
	"strings"
	"testing"
	"time"
	"unicode/utf8"
)

func TestPhases(t *testing.T) {
	box := ExerciseOf("box")
	cases := []struct {
		at       time.Duration
		label    string
		progress int
	}{
		{0, "Breathe in", 0},
		{4 * time.Second, "Hold", 1000},
		{8 * time.Second, "Breathe out", 1000},
		{12 * time.Second, "Hold", 0},
		{16 * time.Second, "Breathe in", 0}, // the next cycle
	}
	for _, c := range cases {
		p := box.At(c.at)
		if p.Label != c.label || p.Progress != c.progress {
			t.Errorf("box at %v: got %s %d, want %s %d", c.at, p.Label, p.Progress, c.label, c.progress)
		}
	}
	if got := box.At(500 * time.Millisecond).Line(); got != "Breathe in... 4s" {
		t.Errorf("line: got %q", got)
	}

	sigh := ExerciseOf("sigh")
	if p := sigh.At(3999 * time.Millisecond); p.Progress > 850 {
		t.Errorf("sigh's first inhale should stop at 85%%, got %d", p.Progress)
	}
	if p := sigh.At(4500 * time.Millisecond); p.Label != "Sip in" {
		t.Errorf("sigh at 4.5s: got %s, want Sip in", p.Label)
	}
}

func TestResolve(t *testing.T) {
	for word, want := range map[string]string{"HRV": "hrv", "relax": "478", "coherent": "hrv", "nope": ""} {
		if got := Resolve(word); got != want {
			t.Errorf("Resolve(%q) = %q, want %q", word, got, want)
		}
	}
	if ExerciseOf("nope").Key != "hrv" {
		t.Error("unknown exercise should fall back to hrv")
	}
}

func TestFrames(t *testing.T) {
	for _, style := range Styles {
		empty := Frame(style, 0, 80, ArtRows)
		if len(empty) != ArtRows || strings.TrimSpace(strings.Join(empty, "")) != "" {
			t.Errorf("%s: empty breath should draw nothing in %d rows: %q", style, ArtRows, empty)
		}
		full := Frame(style, 1000, 80, ArtRows)
		drawn := false
		for _, line := range full {
			if utf8.RuneCountInString(line) > 80 {
				t.Errorf("%s: line wider than 80: %q", style, line)
			}
			if strings.TrimSpace(line) != "" {
				drawn = true
				if pad := len(line) - len(strings.TrimLeft(line, " ")); pad < 10 {
					t.Errorf("%s: shape not centred: %q", style, line)
				}
			}
		}
		if !drawn {
			t.Errorf("%s: full breath drew nothing", style)
		}
		if n := len(Frame(style, 500, 30, 3)); n != 3 {
			t.Errorf("%s: asked for 3 rows, got %d", style, n)
		}
		if n := len(Frame(style, 500, 30, 11)); n != 11 {
			t.Errorf("%s: asked for 11 rows, got %d", style, n)
		}
	}
}

func TestPickStyle(t *testing.T) {
	if PickStyle("wave", "wave") != "wave" {
		t.Error("a pinned style is kept")
	}
	for i := 0; i < 50; i++ {
		if s := PickStyle("random", "dots"); s == "dots" || !IsStyle(s) {
			t.Fatalf("random picked %q after dots", s)
		}
	}
}

package ui

import (
	"os"
	"strings"
	"testing"
	"time"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
	"github.com/muesli/termenv"

	"github.com/antrvan746/pomodoro-cli/internal/breath"
	"github.com/antrvan746/pomodoro-cli/internal/store"
)

func init() { spawnWatcher = func(string) error { return nil } }

func TestTimerViewRenders(t *testing.T) {
	if os.Getenv("POMO_HOME") == "" {
		t.Setenv("POMO_HOME", t.TempDir())
	}
	if os.Getenv("POMO_DUMP") != "" {
		lipgloss.SetColorProfile(termenv.TrueColor)
		ApplyTheme(os.Getenv("POMO_THEME"))
	}
	m := NewModel(Options{StartKind: store.Focus, Duration: 25 * time.Minute, Label: "demo"})
	defer store.Stop("")
	next, _ := m.Update(tea.WindowSizeMsg{Width: 100, Height: 36})
	v := next.(Model).View()
	if !strings.Contains(v, "█") || !strings.Contains(v, "FOCUS") {
		t.Fatalf("timer view missing clock or phase:\n%s", v)
	}
	if p := os.Getenv("POMO_DUMP"); p != "" {
		_ = os.WriteFile(p, []byte(v), 0o644)
	}
}

func TestBigClockWidth(t *testing.T) {
	for _, s := range []string{"25:00", "1:05:00"} {
		for scale := 1; scale <= 2; scale++ {
			first := strings.Split(BigClock(s, scale, cur.Focus, false), "\n")[0]
			if got := len([]rune(stripANSI(first))); got != ClockWidth(s, scale) {
				t.Errorf("%s@%d: width %d, want %d", s, scale, got, ClockWidth(s, scale))
			}
		}
	}
}

func stripANSI(s string) string {
	var b strings.Builder
	esc := false
	for _, r := range s {
		switch {
		case r == '\x1b':
			esc = true
		case esc && r == 'm':
			esc = false
		case !esc:
			b.WriteRune(r)
		}
	}
	return b.String()
}

func TestBreatheView(t *testing.T) {
	now := time.Now()
	m := breatheModel{ex: breath.ExerciseOf("box"), style: "pulse", start: now, now: now.Add(2 * time.Second)}
	next, _ := m.Update(tea.WindowSizeMsg{Width: 60, Height: 12})
	v := next.View()
	if !strings.Contains(v, "Breathe in") || !strings.Contains(v, "▓") {
		t.Fatalf("breathe view should show the phase and the shape:\n%s", v)
	}
	if _, cmd := m.Update(breatheTick(now)); cmd == nil {
		t.Fatal("a tick should schedule the next frame")
	}
	m.opts.Done = func() bool { return true }
	if _, cmd := m.Update(breatheTick(now)); cmd == nil || cmd() != tea.Quit() {
		t.Fatal("the animation should quit once the turn is done")
	}
}

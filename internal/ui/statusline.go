package ui

import (
	"strings"
	"time"

	"github.com/charmbracelet/lipgloss"

	"github.com/antrvan746/pomodoro-cli/internal/store"
)

var eighths = []rune("▏▎▍▌▋▊▉█")

// SubBar renders a compact progress bar with 1/8-cell resolution, framed like
// ▕████▌░░░░░▏, with the fill coloured along the gradient.
func SubBar(p float64, width int, stops []string) string {
	if p < 0 {
		p = 0
	}
	if p > 1 {
		p = 1
	}
	units := int(p*float64(width*8) + 0.5)
	full, part := units/8, units%8
	cols := Gradient(stops, width)
	track := lipgloss.NewStyle().Foreground(Subtle)

	var b strings.Builder
	b.WriteString(track.Render("▕"))
	for i := 0; i < width; i++ {
		switch {
		case i < full:
			b.WriteString(lipgloss.NewStyle().Foreground(cols[i]).Render("█"))
		case i == full && part > 0:
			b.WriteString(lipgloss.NewStyle().Foreground(cols[i]).Render(string(eighths[part-1])))
		default:
			b.WriteString(track.Render("░"))
		}
	}
	b.WriteString(track.Render("▏"))
	return b.String()
}

// CycleDots renders ●●○○ progress towards the next long break.
func CycleDots(done, every int, kind store.Kind) string {
	if every <= 0 {
		return ""
	}
	pos := done % every
	if kind == store.LongBreak && done > 0 && pos == 0 {
		pos = every // the long break caps a full cycle
	}
	cols := Gradient(LogoStops, every)
	var b strings.Builder
	for i := 0; i < every; i++ {
		if i < pos {
			b.WriteString(lipgloss.NewStyle().Foreground(cols[i]).Render("●"))
		} else {
			b.WriteString(lipgloss.NewStyle().Foreground(Subtle).Render("○"))
		}
	}
	return b.String()
}

// StatusSegment is the one-line timer for status bars (Claude Code, tmux).
// view is minimal, classic or full. It returns "" when idle so the host's
// layout doesn't shift.
func StatusSegment(a *store.Active, cfg store.Config, view string, now time.Time) string {
	if a == nil {
		return ""
	}
	icon := IconFor(a.Kind)
	stops := StopsFor(a.Kind)
	timeStyle := lipgloss.NewStyle().Foreground(AccentFor(a.Kind)).Bold(true)
	if a.Paused() {
		icon = "⏸"
		stops = Fade(stops, 0.5)
		timeStyle = lipgloss.NewStyle().Foreground(Muted)
	}
	parts := []string{icon + " " + timeStyle.Render(FmtClock(a.Remaining(now))), SubBar(a.Progress(now), 10, stops)}
	if view != "minimal" {
		parts = append(parts, CycleDots(a.Cycle, cfg.LongBreakEvery, a.Kind))
	}
	if view == "full" && a.Label != "" {
		label := a.Label
		if r := []rune(label); len(r) > 32 {
			label = string(r[:31]) + "…"
		}
		parts = append(parts, mutedStyle.Italic(true).Render(label))
	}
	return strings.Join(parts, " ")
}

// Package daemon is the background watcher spawned for every session. It
// finalises the session when its time is up (even with every terminal
// closed), announces it, auto-advances to the next phase, and keeps the cmux
// sidebar pill in sync.
package daemon

import (
	"fmt"
	"time"

	"github.com/antrvan746/pomodoro-cli/internal/cmux"
	"github.com/antrvan746/pomodoro-cli/internal/notify"
	"github.com/antrvan746/pomodoro-cli/internal/store"
	"github.com/antrvan746/pomodoro-cli/internal/ui"
)

// Watch blocks until the session (and any phases auto-started after it) ends.
func Watch(id string) {
	sb := newSidebar()
	defer sb.finish()
	for {
		a, err := store.Peek()
		if err != nil || a == nil || a.ID != id {
			return
		}
		now := time.Now()
		sb.show(a, now)
		if a.Paused() {
			time.Sleep(time.Second)
			continue
		}
		if rem := a.Remaining(now); rem > 0 {
			step := 2 * time.Second
			if sb.enabled {
				step = time.Second
			}
			time.Sleep(min(rem, step))
			continue
		}
		rec, _ := store.Finish(id)
		if rec == nil {
			return // someone else (e.g. the TUI) finalised it and owns what's next
		}
		notify.Announce(rec, store.LoadConfig())
		next := notify.AutoAdvance(rec)
		if next == nil {
			return
		}
		id = next.ID
	}
}

// sidebar mirrors the timer into a cmux status pill on every workspace.
type sidebar struct {
	enabled    bool
	workspaces []string
	listedAt   time.Time
	last       string
}

func newSidebar() *sidebar {
	return &sidebar{enabled: store.LoadConfig().Cmux && cmux.Available()}
}

func (s *sidebar) refreshWorkspaces() {
	if time.Since(s.listedAt) < 30*time.Second && s.workspaces != nil {
		return
	}
	if ws := cmux.Workspaces(); ws != nil {
		s.workspaces = ws
		s.last = "" // re-push so new workspaces get the pill
	}
	s.listedAt = time.Now()
}

func pillText(a *store.Active, now time.Time) string {
	icon := ui.IconFor(a.Kind)
	if a.Paused() {
		icon = "⏸"
	}
	text := fmt.Sprintf("%s %s", icon, ui.FmtClock(a.Remaining(now)))
	if a.Label != "" && a.Kind == store.Focus {
		label := a.Label
		if r := []rune(label); len(r) > 18 {
			label = string(r[:17]) + "…"
		}
		text += " · " + label
	}
	return text
}

func (s *sidebar) show(a *store.Active, now time.Time) {
	if !s.enabled {
		return
	}
	s.refreshWorkspaces()
	text := pillText(a, now)
	if text == s.last {
		return
	}
	color := string(ui.AccentFor(a.Kind))
	if a.Paused() {
		color = string(ui.Muted)
	}
	for _, w := range s.workspaces {
		_ = cmux.SetStatus(w, text, color, "")
	}
	s.last = text
}

// finish clears the pill unless another session has taken over (it has its
// own watcher, which will keep the pill).
func (s *sidebar) finish() {
	if !s.enabled {
		return
	}
	if a, _ := store.Peek(); a != nil {
		return
	}
	s.refreshWorkspaces()
	for _, w := range s.workspaces {
		_ = cmux.ClearStatus(w)
	}
}

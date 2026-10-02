// Package notify sends desktop notifications and runs the background watcher
// that finalises sessions when nobody has the TUI open.
package notify

import (
	"fmt"
	"os"
	"os/exec"
	"runtime"
	"strings"
	"syscall"
	"time"

	"github.com/antrvan746/pomodoro-cli/internal/cmux"
	"github.com/antrvan746/pomodoro-cli/internal/store"
)

func Send(title, body string, sound bool) {
	switch runtime.GOOS {
	case "darwin":
		esc := func(s string) string { return strings.ReplaceAll(strings.ReplaceAll(s, `\`, `\\`), `"`, `\"`) }
		script := fmt.Sprintf(`display notification "%s" with title "%s"`, esc(body), esc(title))
		if sound {
			script += ` sound name "Glass"`
		}
		_ = exec.Command("osascript", "-e", script).Run()
	case "linux":
		_ = exec.Command("notify-send", "-a", "pomo", title, body).Run()
		if sound {
			_ = exec.Command("canberra-gtk-play", "-i", "complete").Run()
		}
	}
}

// Announce notifies about a finished session, respecting the config.
func Announce(r *store.Record, cfg store.Config) {
	if r == nil || !cfg.Notify || !r.Completed {
		return
	}
	title, body := Message(r, cfg)
	// Inside cmux, use its notifications: they badge the workspace in the
	// sidebar and avoid a duplicate macOS banner.
	if cfg.Cmux && cmux.Available() && cmux.Notify(title, body) == nil {
		return
	}
	Send(title, body, cfg.Sound)
}

func Message(r *store.Record, cfg store.Config) (title, body string) {
	length := fmtLength(r.Planned.D())
	if r.Kind == store.Focus {
		title = "🍅 Focus complete"
		body = length + " well spent. Take a break!"
		if r.Label != "" {
			body = fmt.Sprintf("%s — %s done. Take a break!", r.Label, length)
		}
		return
	}
	return "☕ Break over", "Ready for the next pomodoro?"
}

// SpawnWatcher starts a detached `pomo __watch <id>` process that will
// finalise and announce the session even if every terminal is closed.
func SpawnWatcher(id string) error {
	exe, err := os.Executable()
	if err != nil {
		return err
	}
	cmd := exec.Command(exe, "__watch", id)
	cmd.SysProcAttr = &syscall.SysProcAttr{Setsid: true}
	if err := cmd.Start(); err != nil {
		return err
	}
	return cmd.Process.Release()
}

// AutoAdvance starts the phase after rec when the config asks for it.
func AutoAdvance(rec *store.Record) *store.Active {
	if rec == nil || !rec.Completed {
		return nil
	}
	cfg := store.LoadConfig()
	if (rec.Kind == store.Focus && !cfg.AutoStartBreak) || (rec.Kind != store.Focus && !cfg.AutoStartFocus) {
		return nil
	}
	kind, cycle := store.Next(rec, rec.Cycle, cfg)
	a, err := store.Start(kind, cfg.Duration(kind), "", rec.Source, cycle, false)
	if err != nil {
		return nil
	}
	return a
}

// Settle finalises an expired session (if the watcher died, say), announces
// it and auto-advances, spawning a watcher for the new phase.
func Settle() (*store.Active, *store.Record, error) {
	a, fin, err := store.Current()
	if err != nil || fin == nil {
		return a, fin, err
	}
	Announce(fin, store.LoadConfig())
	if next := AutoAdvance(fin); next != nil {
		_ = SpawnWatcher(next.ID)
		a = next
	}
	return a, fin, nil
}

// fmtLength renders a session length for notifications: 45s, 25m, 1h30m.
func fmtLength(d time.Duration) string {
	if d < time.Minute {
		return fmt.Sprintf("%ds", int(d.Round(time.Second).Seconds()))
	}
	m := int(d.Round(time.Minute).Minutes())
	switch {
	case m < 60:
		return fmt.Sprintf("%dm", m)
	case m%60 == 0:
		return fmt.Sprintf("%dh", m/60)
	default:
		return fmt.Sprintf("%dh%02dm", m/60, m%60)
	}
}

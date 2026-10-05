// Package launch opens the full-screen clock in a new terminal pane or window,
// next to whatever ran the command (Claude Code, Codex, a script).
package launch

import (
	"errors"
	"fmt"
	"os"
	"os/exec"
	"strings"

	"github.com/antrvan746/pomodoro-cli/internal/wmux"
)

// ErrNoTerminal means no supported terminal could be detected.
var ErrNoTerminal = errors.New("no supported terminal found")

// Open runs command (a shell command line) in a new pane or window and returns
// a short description of where, e.g. "cmux split".
func Open(command string) (string, error) {
	for _, t := range terminals {
		if !t.detect() {
			continue
		}
		if err := t.open(command); err != nil {
			return "", fmt.Errorf("%s: %w", t.name, err)
		}
		return t.name, nil
	}
	return "", ErrNoTerminal
}

// OpenBelow runs command in a new pane under the current one. It only uses
// multiplexers that can split (cmux, tmux, WezTerm, kitty), never a new window.
func OpenBelow(command string) (string, error) {
	for _, t := range terminals {
		if t.below == nil || !t.detect() {
			continue
		}
		if err := t.below(command); err != nil {
			return "", fmt.Errorf("%s: %w", t.name, err)
		}
		return t.name, nil
	}
	return "", ErrNoTerminal
}

type terminal struct {
	name   string
	detect func() bool
	open   func(command string) error
	below  func(command string) error // nil: can't split
}

func env(k string) bool { return os.Getenv(k) != "" }

func run(name string, args ...string) error {
	out, err := exec.Command(name, args...).CombinedOutput()
	if err != nil && len(out) > 0 {
		return fmt.Errorf("%w: %s", err, strings.TrimSpace(string(out)))
	}
	return err
}

func cmuxBin() string {
	if p, err := exec.LookPath("cmux"); err == nil {
		return p
	}
	if p := os.Getenv("CMUX_BUNDLED_CLI_PATH"); p != "" {
		return p
	}
	return "/Applications/cmux.app/Contents/Resources/bin/cmux"
}

func appleScriptString(s string) string {
	return `"` + strings.ReplaceAll(strings.ReplaceAll(s, `\`, `\\`), `"`, `\"`) + `"`
}

// Order matters: multiplexers first (they live inside another terminal), then
// the host terminal app.
var terminals = []terminal{
	// wmux types the command into the pane's shell, which may be cmd,
	// PowerShell or bash, so pomo closes the pane itself when it exits.
	{"wmux split", wmux.Available, func(c string) error {
		return wmux.Split(c+" --close-wmux-pane", false)
	}, func(c string) error {
		return wmux.Split(c+" --close-wmux-pane", true)
	}},
	{"cmux split", func() bool { return env("CMUX_SURFACE_ID") }, func(c string) error {
		// cmux types the command into a new shell; exit closes the pane after.
		return run(cmuxBin(), "new-split", "right", "--surface", os.Getenv("CMUX_SURFACE_ID"),
			"--command", c+"; exit", "--focus", "false")
	}, func(c string) error {
		return run(cmuxBin(), "new-split", "down", "--surface", os.Getenv("CMUX_SURFACE_ID"),
			"--command", c+"; exit", "--focus", "false")
	}},
	{"tmux split", func() bool { return env("TMUX") }, func(c string) error {
		return run("tmux", "split-window", "-h", "-d", c)
	}, func(c string) error {
		return run("tmux", "split-window", "-v", "-d", "-l", "12", c)
	}},
	{"WezTerm split", func() bool { return env("WEZTERM_PANE") }, func(c string) error {
		return run("wezterm", "cli", "split-pane", "--right", "--", "sh", "-c", c)
	}, func(c string) error {
		return run("wezterm", "cli", "split-pane", "--bottom", "--cells", "12", "--", "sh", "-c", c)
	}},
	{"kitty split", func() bool { return env("KITTY_WINDOW_ID") }, func(c string) error {
		return run("kitty", "@", "launch", "--location=vsplit", "--keep-focus", "sh", "-c", c)
	}, func(c string) error {
		return run("kitty", "@", "launch", "--location=hsplit", "--keep-focus", "sh", "-c", c)
	}},
	{"iTerm window", func() bool { return os.Getenv("TERM_PROGRAM") == "iTerm.app" }, func(c string) error {
		return run("osascript", "-e", `tell application "iTerm" to create window with default profile command `+
			appleScriptString("sh -c "+shellQuote(c)))
	}, nil},
	{"Terminal window", func() bool { return os.Getenv("TERM_PROGRAM") == "Apple_Terminal" }, func(c string) error {
		return run("osascript", "-e", `tell application "Terminal" to do script `+appleScriptString(c+"; exit"))
	}, nil},
	{"Ghostty window", func() bool { return os.Getenv("TERM_PROGRAM") == "ghostty" }, func(c string) error {
		return run("open", "-na", "Ghostty", "--args", "-e", "sh", "-c", c)
	}, nil},
}

func shellQuote(s string) string { return "'" + strings.ReplaceAll(s, "'", `'\''`) + "'" }

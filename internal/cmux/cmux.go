// Package cmux talks to the cmux terminal (https://cmux.com) through its CLI:
// sidebar status pills for the running timer and native notifications.
package cmux

import (
	"encoding/json"
	"os"
	"os/exec"
	"path/filepath"
	"time"
)

// StatusKey namespaces pomo's sidebar pill.
const StatusKey = "pomo"

func bin() string {
	if p := os.Getenv("CMUX_BUNDLED_CLI_PATH"); p != "" {
		return p
	}
	if p, err := exec.LookPath("cmux"); err == nil {
		return p
	}
	return "/Applications/cmux.app/Contents/Resources/bin/cmux"
}

func socketPath() string {
	if p := os.Getenv("CMUX_SOCKET_PATH"); p != "" {
		return p
	}
	home, _ := os.UserHomeDir()
	return filepath.Join(home, ".local", "state", "cmux", "cmux.sock")
}

// Available reports whether cmux is installed and running.
func Available() bool {
	if os.Getenv("POMO_NO_CMUX") != "" {
		return false
	}
	if _, err := os.Stat(bin()); err != nil {
		return false
	}
	st, err := os.Stat(socketPath())
	return err == nil && st.Mode()&os.ModeSocket != 0
}

func run(args ...string) ([]byte, error) {
	cmd := exec.Command(bin(), args...)
	cmd.Env = append(os.Environ(), "CMUX_QUIET=1")
	done := make(chan struct{})
	var out []byte
	var err error
	go func() { out, err = cmd.Output(); close(done) }()
	select {
	case <-done:
		return out, err
	case <-time.After(3 * time.Second):
		if cmd.Process != nil {
			_ = cmd.Process.Kill()
		}
		return nil, os.ErrDeadlineExceeded
	}
}

// Workspaces returns the ids of every workspace in the current window.
func Workspaces() []string {
	out, err := run("workspace", "list", "--json")
	if err != nil {
		return nil
	}
	var resp struct {
		Workspaces []struct {
			ID string `json:"id"`
		} `json:"workspaces"`
	}
	if json.Unmarshal(out, &resp) != nil {
		return nil
	}
	ids := make([]string, 0, len(resp.Workspaces))
	for _, w := range resp.Workspaces {
		ids = append(ids, w.ID)
	}
	return ids
}

func SetStatus(workspace, value, color, icon string) error {
	args := []string{"set-status", StatusKey, value, "--workspace", workspace, "--priority", "50"}
	if color != "" {
		args = append(args, "--color", color)
	}
	if icon != "" {
		args = append(args, "--icon", icon)
	}
	_, err := run(args...)
	return err
}

func ClearStatus(workspace string) error {
	_, err := run("clear-status", StatusKey, "--workspace", workspace)
	return err
}

// Notify posts a cmux notification. It targets the pane the session was
// started from when known (so that workspace gets the unread badge), and
// falls back to cmux's default target if that pane is gone.
func Notify(title, body string) error {
	if s := os.Getenv("CMUX_SURFACE_ID"); s != "" {
		if _, err := run("notify", "--title", title, "--body", body, "--surface", s); err == nil {
			return nil
		}
	}
	_, err := run("notify", "--title", title, "--body", body)
	return err
}

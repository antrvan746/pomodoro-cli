// Package wmux talks to the wmux terminal multiplexer for Windows
// (https://github.com/amirlehmam/wmux) the way package cmux talks to cmux:
// native notifications that badge the workspace, and splits for the clock.
//
// It speaks wmux's pipe protocol directly (one JSON or text line per
// connection) instead of shelling out to the Node CLI, which costs a Node
// start-up per call.
package wmux

import (
	"bufio"
	"encoding/json"
	"errors"
	"io"
	"net"
	"os"
	"path/filepath"
	"strings"
	"time"
)

const timeout = 3 * time.Second

func pipePath() string {
	if p := os.Getenv("WMUX_PIPE"); p != "" {
		return p
	}
	return `\\.\pipe\wmux`
}

// token authenticates requests. wmux puts it in the environment of every
// shell it spawns; elsewhere it lives in the instance's APPDATA folder.
func token() string {
	if t := strings.TrimSpace(os.Getenv("WMUX_PIPE_TOKEN")); t != "" {
		return t
	}
	dir := os.Getenv("APPDATA")
	if dir == "" {
		home, _ := os.UserHomeDir()
		dir = filepath.Join(home, "AppData", "Roaming")
	}
	name := "wmux"
	if inst := strings.TrimSpace(os.Getenv("WMUX_INSTANCE")); inst != "" {
		name += "-" + inst
	}
	b, err := os.ReadFile(filepath.Join(dir, name, "pipe-token"))
	if err != nil {
		return ""
	}
	return strings.TrimSpace(string(b))
}

// Surface is the wmux pane this process runs in, if any.
func Surface() string { return os.Getenv("WMUX_SURFACE_ID") }

// Available reports whether this process runs inside a wmux pane.
func Available() bool {
	return os.Getenv("POMO_NO_WMUX") == "" && Surface() != ""
}

func dial() (io.ReadWriteCloser, error) {
	p := pipePath()
	if strings.HasPrefix(p, "/") {
		return net.DialTimeout("unix", p, timeout)
	}
	// A Windows named pipe opens like a file.
	return os.OpenFile(p, os.O_RDWR, 0)
}

// roundTrip writes one line and reads the one-line reply.
func roundTrip(line string) (string, error) {
	type result struct {
		s   string
		err error
	}
	ch := make(chan result, 1)
	go func() {
		c, err := dial()
		if err != nil {
			ch <- result{err: err}
			return
		}
		defer c.Close()
		if _, err := io.WriteString(c, line+"\n"); err != nil {
			ch <- result{err: err}
			return
		}
		s, err := bufio.NewReader(c).ReadString('\n')
		if err != nil && s == "" {
			ch <- result{err: err}
			return
		}
		ch <- result{s: strings.TrimSpace(s)}
	}()
	select {
	case r := <-ch:
		return r.s, r.err
	case <-time.After(timeout):
		return "", os.ErrDeadlineExceeded
	}
}

// call sends a V2 request and decodes its result into out (if non-nil).
func call(method string, params map[string]any, out any) error {
	if params == nil {
		params = map[string]any{}
	}
	if s := Surface(); s != "" {
		// Scopes workspace and pane requests to the caller's window.
		params["caller"] = s
	}
	req, err := json.Marshal(map[string]any{"method": method, "params": params, "id": 1, "token": token()})
	if err != nil {
		return err
	}
	reply, err := roundTrip(string(req))
	if err != nil {
		return err
	}
	var resp struct {
		Result json.RawMessage `json:"result"`
		Error  *struct {
			Message string `json:"message"`
		} `json:"error"`
	}
	if err := json.Unmarshal([]byte(reply), &resp); err != nil {
		return err
	}
	if resp.Error != nil {
		return errors.New(resp.Error.Message)
	}
	if out != nil && len(resp.Result) > 0 {
		return json.Unmarshal(resp.Result, out)
	}
	return nil
}

// Notify posts a wmux notification from this pane: a toast, the configured
// sound, and an unread badge on its workspace.
func Notify(text string) error {
	text = strings.ReplaceAll(text, "\n", " ")
	line := "notify " + Surface() + " " + text
	if t := token(); t != "" {
		line = "auth " + t + " " + line
	}
	reply, err := roundTrip(line)
	if err != nil {
		return err
	}
	if strings.Contains(strings.ToLower(reply), "unauthorized") {
		return errors.New("wmux: unauthorized")
	}
	return nil
}

// CloseSurface closes the pane this process runs in.
func CloseSurface() error {
	s := Surface()
	if s == "" {
		return errors.New("wmux: not in a wmux pane")
	}
	return call("surface.close", map[string]any{"id": s}, nil)
}

// Split opens a terminal pane to the right (or below, with down) of the
// caller's workspace and types command into its shell.
func Split(command string, down bool) error {
	dir := "right"
	if down {
		dir = "down"
	}
	var res struct {
		SurfaceID string `json:"surfaceId"`
	}
	err := call("pane.split", map[string]any{
		"direction":       dir,
		"type":            "terminal",
		"startupCommands": []string{command},
	}, &res)
	if err == nil && res.SurfaceID == "" {
		err = errors.New("wmux: split returned no surface")
	}
	return err
}

// Package store persists pomodoro configuration, the active session and the
// session history. All state lives on disk so that any process — the TUI, a
// background watcher, or an AI agent calling the CLI — sees the same timer.
package store

import (
	"bufio"
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strconv"
	"time"

	"github.com/antrvan746/pomodoro-cli/internal/sysx"
)

type Kind string

const (
	Focus      Kind = "focus"
	ShortBreak Kind = "short_break"
	LongBreak  Kind = "long_break"
)

func (k Kind) Title() string {
	switch k {
	case ShortBreak:
		return "Short Break"
	case LongBreak:
		return "Long Break"
	default:
		return "Focus"
	}
}

func (k Kind) Valid() bool { return k == Focus || k == ShortBreak || k == LongBreak }

// Config holds user preferences. Durations are in minutes.
type Config struct {
	FocusMin       int     `json:"focus_minutes"`
	ShortBreakMin  int     `json:"short_break_minutes"`
	LongBreakMin   int     `json:"long_break_minutes"`
	LongBreakEvery int     `json:"long_break_every"`
	DailyGoal      int     `json:"daily_goal"`
	AutoStartBreak bool    `json:"auto_start_break"`
	AutoStartFocus bool    `json:"auto_start_focus"`
	Notify         bool    `json:"notify"`
	Sound          bool    `json:"sound"`
	Theme          string  `json:"theme"`
	OpenClock      string  `json:"open_clock"`      // auto | always | never
	Cmux           bool    `json:"cmux"`            // sidebar pill + notifications when running in cmux
	Wmux           bool    `json:"wmux"`            // notifications + clock pane when running in wmux
	StatusView     string  `json:"statusline_view"` // minimal | classic | full
	Breathe        Breathe `json:"breathe"`
}

// Breathe configures the breathing pane that opens while an agent works.
type Breathe struct {
	Enabled  bool    `json:"enabled"`
	Exercise string  `json:"exercise"` // hrv | sigh | box | 478
	Style    string  `json:"style"`    // pulse | ripples | dots | wave | random
	Delay    float64 `json:"delay"`    // seconds the agent works before the pane opens
}

func DefaultConfig() Config {
	return Config{
		FocusMin: 25, ShortBreakMin: 5, LongBreakMin: 15, LongBreakEvery: 4,
		DailyGoal: 8, AutoStartBreak: true, AutoStartFocus: false, Notify: true, Sound: true,
		Theme: "catppuccin", StatusView: "classic", OpenClock: "auto", Cmux: true, Wmux: true,
		Breathe: Breathe{Enabled: true, Exercise: "hrv", Style: "random", Delay: 5},
	}
}

func (c Config) Duration(k Kind) time.Duration {
	switch k {
	case ShortBreak:
		return time.Duration(c.ShortBreakMin) * time.Minute
	case LongBreak:
		return time.Duration(c.LongBreakMin) * time.Minute
	default:
		return time.Duration(c.FocusMin) * time.Minute
	}
}

// Active is the currently running (or paused) session.
type Active struct {
	ID          string     `json:"id"`
	Kind        Kind       `json:"kind"`
	Label       string     `json:"label,omitempty"`
	Duration    Seconds    `json:"duration_sec"`
	StartedAt   time.Time  `json:"started_at"`
	PausedAt    *time.Time `json:"paused_at,omitempty"`
	PausedTotal Seconds    `json:"paused_total_sec"`
	Cycle       int        `json:"cycle"` // completed focus sessions in the current cycle
	Source      string     `json:"source"`
}

// Seconds marshals a duration as seconds (millisecond precision).
type Seconds time.Duration

func (s Seconds) MarshalJSON() ([]byte, error) {
	return json.Marshal(float64(time.Duration(s).Round(time.Millisecond)) / float64(time.Second))
}
func (s *Seconds) UnmarshalJSON(b []byte) error {
	var f float64
	if err := json.Unmarshal(b, &f); err != nil {
		return err
	}
	*s = Seconds(time.Duration(f * float64(time.Second)).Round(time.Millisecond))
	return nil
}
func (s Seconds) D() time.Duration { return time.Duration(s) }

func (a *Active) Paused() bool { return a.PausedAt != nil }

func (a *Active) Elapsed(now time.Time) time.Duration {
	end := now
	if a.PausedAt != nil {
		end = *a.PausedAt
	}
	e := end.Sub(a.StartedAt) - a.PausedTotal.D()
	if e < 0 {
		return 0
	}
	return e
}

func (a *Active) Remaining(now time.Time) time.Duration {
	r := a.Duration.D() - a.Elapsed(now)
	if r < 0 {
		return 0
	}
	return r
}

func (a *Active) Progress(now time.Time) float64 {
	if a.Duration <= 0 {
		return 1
	}
	p := float64(a.Elapsed(now)) / float64(a.Duration.D())
	if p > 1 {
		return 1
	}
	return p
}

func (a *Active) Done(now time.Time) bool { return a.Remaining(now) <= 0 }

// EndsAt is the projected wall-clock end, assuming no further pauses.
func (a *Active) EndsAt(now time.Time) time.Time { return now.Add(a.Remaining(now)) }

// Record is one finished (completed or stopped) session in the history log.
type Record struct {
	ID        string    `json:"id"`
	Kind      Kind      `json:"kind"`
	Label     string    `json:"label,omitempty"`
	Planned   Seconds   `json:"planned_sec"`
	Actual    Seconds   `json:"actual_sec"`
	StartedAt time.Time `json:"started_at"`
	EndedAt   time.Time `json:"ended_at"`
	Completed bool      `json:"completed"`
	Source    string    `json:"source"`
	Cycle     int       `json:"cycle"` // completed focus sessions before this one
}

// ---------------------------------------------------------------------------
// Paths

func Dir() string {
	if d := os.Getenv("POMO_HOME"); d != "" {
		return d
	}
	if d := os.Getenv("XDG_DATA_HOME"); d != "" {
		return filepath.Join(d, "pomo")
	}
	home, err := os.UserHomeDir()
	if err != nil {
		return ".pomo"
	}
	return filepath.Join(home, ".local", "share", "pomo")
}

func path(name string) string { return filepath.Join(Dir(), name) }

func ConfigPath() string  { return path("config.json") }
func statePath() string   { return path("active.json") }
func historyPath() string { return path("sessions.jsonl") }

func ensureDir() error { return os.MkdirAll(Dir(), 0o755) }

// withLock serialises read-modify-write cycles across processes.
func withLock(fn func() error) error {
	if err := ensureDir(); err != nil {
		return err
	}
	f, err := os.OpenFile(path(".lock"), os.O_CREATE|os.O_RDWR, 0o644)
	if err != nil {
		return err
	}
	defer f.Close()
	if err := sysx.Lock(f); err != nil {
		return err
	}
	defer sysx.Unlock(f)
	return fn()
}

func writeJSONAtomic(p string, v any) error {
	if err := ensureDir(); err != nil {
		return err
	}
	b, err := json.MarshalIndent(v, "", "  ")
	if err != nil {
		return err
	}
	tmp := p + ".tmp"
	if err := os.WriteFile(tmp, b, 0o644); err != nil {
		return err
	}
	return os.Rename(tmp, p)
}

func NewID() string {
	b := make([]byte, 6)
	_, _ = rand.Read(b)
	return hex.EncodeToString(b)
}

// ---------------------------------------------------------------------------
// Config

func LoadConfig() Config {
	c := DefaultConfig()
	b, err := os.ReadFile(ConfigPath())
	if err == nil {
		_ = json.Unmarshal(b, &c)
	}
	if c.FocusMin <= 0 {
		c.FocusMin = 25
	}
	if c.ShortBreakMin <= 0 {
		c.ShortBreakMin = 5
	}
	if c.LongBreakMin <= 0 {
		c.LongBreakMin = 15
	}
	if c.LongBreakEvery <= 0 {
		c.LongBreakEvery = 4
	}
	return c
}

func SaveConfig(c Config) error { return writeJSONAtomic(ConfigPath(), c) }

// ---------------------------------------------------------------------------
// Active session

var ErrNoSession = errors.New("no active session")
var ErrRunning = errors.New("a session is already running")

func readActive() (*Active, error) {
	b, err := os.ReadFile(statePath())
	if errors.Is(err, os.ErrNotExist) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	var a Active
	if err := json.Unmarshal(b, &a); err != nil {
		return nil, fmt.Errorf("corrupt state file %s: %w", statePath(), err)
	}
	return &a, nil
}

// Current returns the active session, or nil when idle. A session whose time
// has run out is finalised first; finished reports such a record so callers
// can announce it.
func Current() (a *Active, finished *Record, err error) {
	err = withLock(func() error {
		a, err = readActive()
		if err != nil || a == nil {
			return err
		}
		if a.Done(time.Now()) {
			finished, err = finalizeLocked(a, true)
			a = nil
		}
		return err
	})
	return
}

// Peek reads the active session without finalising it.
func Peek() (*Active, error) {
	var a *Active
	err := withLock(func() (err error) { a, err = readActive(); return })
	return a, err
}

func Start(kind Kind, d time.Duration, label, source string, cycle int, force bool) (*Active, error) {
	var out *Active
	err := withLock(func() error {
		cur, err := readActive()
		if err != nil {
			return err
		}
		if cur != nil {
			if cur.Done(time.Now()) {
				if _, err := finalizeLocked(cur, true); err != nil {
					return err
				}
			} else if !force {
				return ErrRunning
			} else if _, err := finalizeLocked(cur, false); err != nil {
				return err
			}
		}
		out = &Active{
			ID: NewID(), Kind: kind, Label: label, Duration: Seconds(d),
			StartedAt: time.Now(), Cycle: cycle, Source: source,
		}
		return writeJSONAtomic(statePath(), out)
	})
	return out, err
}

func mutate(fn func(a *Active) error) (*Active, error) {
	var out *Active
	err := withLock(func() error {
		a, err := readActive()
		if err != nil {
			return err
		}
		if a == nil {
			return ErrNoSession
		}
		if err := fn(a); err != nil {
			return err
		}
		out = a
		return writeJSONAtomic(statePath(), a)
	})
	return out, err
}

func Pause() (*Active, error) {
	return mutate(func(a *Active) error {
		if a.PausedAt == nil {
			now := time.Now()
			a.PausedAt = &now
		}
		return nil
	})
}

func Resume() (*Active, error) {
	return mutate(func(a *Active) error {
		if a.PausedAt != nil {
			a.PausedTotal += Seconds(time.Since(*a.PausedAt))
			a.PausedAt = nil
		}
		return nil
	})
}

func TogglePause() (*Active, error) {
	a, err := Peek()
	if err != nil {
		return nil, err
	}
	if a == nil {
		return nil, ErrNoSession
	}
	if a.Paused() {
		return Resume()
	}
	return Pause()
}

// Stop ends the active session early. If id is non-empty, only a session with
// that id is stopped.
func Stop(id string) (*Record, error) {
	var rec *Record
	err := withLock(func() error {
		a, err := readActive()
		if err != nil {
			return err
		}
		if a == nil || (id != "" && a.ID != id) {
			return ErrNoSession
		}
		rec, err = finalizeLocked(a, a.Done(time.Now()))
		return err
	})
	return rec, err
}

// Finish finalises the session with the given id if its time is up. It returns
// a record only for the caller that actually finalised it, so exactly one
// process announces each completion.
func Finish(id string) (*Record, error) {
	var rec *Record
	err := withLock(func() error {
		a, err := readActive()
		if err != nil || a == nil || a.ID != id || !a.Done(time.Now()) {
			return err
		}
		rec, err = finalizeLocked(a, true)
		return err
	})
	return rec, err
}

func finalizeLocked(a *Active, completed bool) (*Record, error) {
	now := time.Now()
	elapsed := a.Elapsed(now)
	end := now
	if completed {
		elapsed = a.Duration.D()
		// The real finish moment, even if nobody was looking at the timer.
		end = a.StartedAt.Add(a.Duration.D() + a.PausedTotal.D())
		if a.PausedAt != nil {
			end = *a.PausedAt
		}
	}
	rec := &Record{
		ID: a.ID, Kind: a.Kind, Label: a.Label, Planned: a.Duration,
		Actual: Seconds(elapsed.Round(time.Second)), StartedAt: a.StartedAt,
		EndedAt: end, Completed: completed, Source: a.Source, Cycle: a.Cycle,
	}
	if err := appendRecord(rec); err != nil {
		return nil, err
	}
	if err := os.Remove(statePath()); err != nil && !errors.Is(err, os.ErrNotExist) {
		return nil, err
	}
	return rec, nil
}

// ---------------------------------------------------------------------------
// History

func appendRecord(r *Record) error {
	if err := ensureDir(); err != nil {
		return err
	}
	f, err := os.OpenFile(historyPath(), os.O_CREATE|os.O_APPEND|os.O_WRONLY, 0o644)
	if err != nil {
		return err
	}
	defer f.Close()
	b, err := json.Marshal(r)
	if err != nil {
		return err
	}
	_, err = f.Write(append(b, '\n'))
	return err
}

// AddRecord appends a manually logged session (e.g. `pomo log add`).
func AddRecord(r *Record) error { return withLock(func() error { return appendRecord(r) }) }

func History() ([]Record, error) {
	f, err := os.Open(historyPath())
	if errors.Is(err, os.ErrNotExist) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	defer f.Close()
	var out []Record
	sc := bufio.NewScanner(f)
	sc.Buffer(make([]byte, 64*1024), 1024*1024)
	for sc.Scan() {
		var r Record
		if json.Unmarshal(sc.Bytes(), &r) == nil {
			out = append(out, r)
		}
	}
	return out, sc.Err()
}

func FindRecord(id string) (*Record, error) {
	hs, err := History()
	if err != nil {
		return nil, err
	}
	for i := len(hs) - 1; i >= 0; i-- {
		if hs[i].ID == id {
			return &hs[i], nil
		}
	}
	return nil, nil
}

func SetLabel(label string) (*Active, error) {
	return mutate(func(a *Active) error { a.Label = label; return nil })
}

// Extend adds (or with a negative d, removes) time from the active session.
func Extend(d time.Duration) (*Active, error) {
	return mutate(func(a *Active) error {
		nd := a.Duration.D() + d
		if min := a.Elapsed(time.Now()) + time.Second; nd < min {
			nd = min
		}
		a.Duration = Seconds(nd)
		return nil
	})
}

// Next decides the phase after a finished session: a focus session leads to a
// break (long every LongBreakEvery pomodoros), a break leads to focus. cycle is
// the number of completed focus sessions in the current cycle.
func Next(r *Record, cycle int, c Config) (kind Kind, nextCycle int) {
	if r.Kind == Focus {
		if r.Completed {
			cycle++
		}
		if cycle > 0 && cycle%c.LongBreakEvery == 0 {
			return LongBreak, cycle
		}
		return ShortBreak, cycle
	}
	if r.Kind == LongBreak {
		cycle = 0
	}
	return Focus, cycle
}

// CycleNow derives the cycle position from today's history: completed focus
// sessions since the last long break.
func CycleNow() int {
	hs, _ := History()
	y, m, d := time.Now().Date()
	n := 0
	for i := len(hs) - 1; i >= 0; i-- {
		r := hs[i]
		if ry, rm, rd := r.EndedAt.Local().Date(); ry != y || rm != m || rd != d {
			break
		}
		if r.Kind == LongBreak && r.Completed {
			break
		}
		if r.Kind == Focus && r.Completed {
			n++
		}
	}
	return n
}

// ---------------------------------------------------------------------------
// Viewers: open full-screen clocks, so we don't open a second one in the same
// place. Each viewer records where it runs (see Place); a clock in another
// cmux workspace or tmux session doesn't stop one opening here.

func viewersDir() string { return path("viewers") }

// Place identifies where the current process is showing things: the cmux
// workspace or tmux session it runs in, or "" when neither is known.
func Place() string {
	if v := os.Getenv("CMUX_WORKSPACE_ID"); v != "" {
		return "cmux:" + v
	}
	if v := os.Getenv("TMUX"); v != "" {
		// $TMUX is "socket,server-pid,session-index": one per session.
		return "tmux:" + v
	}
	return ""
}

// RegisterViewer marks this process as showing the clock; call the returned
// func on exit.
func RegisterViewer() func() {
	if err := os.MkdirAll(viewersDir(), 0o755); err != nil {
		return func() {}
	}
	p := filepath.Join(viewersDir(), strconv.Itoa(os.Getpid()))
	_ = os.WriteFile(p, []byte(Place()), 0o644)
	return func() { _ = os.Remove(p) }
}

// ViewerOpen reports whether some live process is showing the clock in the
// same place as this one.
func ViewerOpen() bool {
	here := Place()
	entries, _ := os.ReadDir(viewersDir())
	open := false
	for _, e := range entries {
		pid, err := strconv.Atoi(e.Name())
		if err != nil {
			continue
		}
		f := filepath.Join(viewersDir(), e.Name())
		if !sysx.Alive(pid) {
			_ = os.Remove(f) // stale
			continue
		}
		if b, err := os.ReadFile(f); err == nil && string(b) == here {
			open = true
		}
	}
	return open
}

package cli

import (
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
	"syscall"
	"time"

	"github.com/spf13/cobra"

	"github.com/antrvan746/pomodoro-cli/internal/breath"
	"github.com/antrvan746/pomodoro-cli/internal/launch"
	"github.com/antrvan746/pomodoro-cli/internal/store"
	"github.com/antrvan746/pomodoro-cli/internal/ui"
)

// The breathing pane: an agent's hooks call `pomo breathe hook start` when a
// turn begins and `pomo breathe hook stop` when it ends. Start marks the
// place (cmux workspace, tmux session) as working and, after the configured
// delay, opens a small pane below the agent running `pomo breathe pane`. The
// pane quits as soon as the mark is gone.

// breathePaneMaxAge bounds a pane whose stop hook never came.
const breathePaneMaxAge = 30 * time.Minute

func breatheDir() string { return filepath.Join(store.Dir(), "breathe") }

var unsafeKey = regexp.MustCompile(`[^A-Za-z0-9._-]+`)

// breatheKey names this place's files.
func breatheKey() string {
	if p := store.Place(); p != "" {
		return unsafeKey.ReplaceAllString(p, "_")
	}
	return "default"
}

func turnPath(key string) string { return filepath.Join(breatheDir(), key+".turn") }
func panePath(key string) string { return filepath.Join(breatheDir(), key+".pane") }

func alive(pid int) bool {
	err := syscall.Kill(pid, 0)
	return pid > 0 && (err == nil || errors.Is(err, syscall.EPERM))
}

func paneOpen(key string) bool {
	b, err := os.ReadFile(panePath(key))
	if err != nil {
		return false
	}
	pid, _ := strconv.Atoi(strings.TrimSpace(string(b)))
	return alive(pid)
}

func breatheStatus(c store.Breathe) string {
	ex := breath.ExerciseOf(c.Exercise)
	state := "on"
	if !c.Enabled {
		state = "off"
	}
	return fmt.Sprintf("breathe: %s · %s (%s) · style %s · delay %gs", state, ex.Name, ex.Pattern, c.Style, c.Delay)
}

// applyBreathe applies one `pomo breathe <setting>` to c.
func applyBreathe(c *store.Breathe, args []string) error {
	head, arg := strings.ToLower(args[0]), ""
	if len(args) > 1 {
		arg = strings.ToLower(args[1])
	}
	switch {
	case head == "on" || head == "off":
		c.Enabled = head == "on"
	case breath.Resolve(head) != "":
		c.Exercise = breath.Resolve(head)
	case head == "style" && (arg == "random" || breath.IsStyle(arg)):
		c.Style = arg
	case head == "style":
		return fmt.Errorf("style is one of %s, random", strings.Join(breath.Styles, ", "))
	case breath.IsStyle(head) || head == "random":
		c.Style = head
	case head == "delay":
		d, err := strconv.ParseFloat(arg, 64)
		if err != nil || d < 0 {
			return errors.New("delay takes a number of seconds, 0 or more")
		}
		c.Delay = d
	default:
		return fmt.Errorf("no setting called %q (try on, off, hrv, sigh, box, 478, style <name>, delay <s>)", head)
	}
	return nil
}

func breatheCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "breathe [on|off|hrv|sigh|box|478|style <name>|delay <s>]",
		Short: "Guided breathing while an agent works (or right now, in a terminal)",
		Long: "In a terminal with no arguments, shows a breathing exercise until q.\n" +
			"With an argument, changes a setting:\n\n" +
			"  on | off          open the breathing pane while Codex works\n" +
			"  hrv               Coherent Breathing: 5.5s in, 5.5s out\n" +
			"  sigh              Physiological Sigh: double inhale, long exhale\n" +
			"  box               Box Breathing: 4s in, 4s hold, 4s out, 4s hold\n" +
			"  478               4-7-8 Breathing: 4s in, 7s hold, 8s out\n" +
			"  style <name>      pulse, ripples, dots, wave or random\n" +
			"  delay <seconds>   how long Codex works before the pane opens\n\n" +
			"Install the Codex hooks with `pomo setup codex`. In Claude Code the\n" +
			"pomo plugin draws the breath above the prompt instead (/breathe).",
		Args: cobra.ArbitraryArgs,
		RunE: func(cmd *cobra.Command, args []string) error {
			cfg := store.LoadConfig()
			if len(args) == 0 && isTTY() && !jsonOut {
				return ui.RunBreathe(ui.BreatheOptions{Exercise: cfg.Breathe.Exercise, Style: cfg.Breathe.Style})
			}
			if len(args) > 0 && args[0] != "status" {
				if err := applyBreathe(&cfg.Breathe, args); err != nil {
					return err
				}
				if err := store.SaveConfig(cfg); err != nil {
					return err
				}
			}
			if jsonOut {
				return printJSON(cfg.Breathe)
			}
			fmt.Println(accent(ui.Highlight).Render("🫁 ") + breatheStatus(cfg.Breathe))
			return nil
		},
	}
	cmd.AddCommand(breatheHookCmd(), breathePaneCmd(), breatheOpenCmd())
	return cmd
}

// breatheHookCmd is what the agent's hooks run. It prints nothing (a hook's
// output can reach the model) and returns at once.
func breatheHookCmd() *cobra.Command {
	return &cobra.Command{
		Use:       "hook <start|stop>",
		Hidden:    true,
		Args:      cobra.ExactArgs(1),
		ValidArgs: []string{"start", "stop"},
		RunE: func(cmd *cobra.Command, args []string) error {
			_, _ = io.Copy(io.Discard, io.LimitReader(os.Stdin, 1<<20)) // the hook payload
			key := breatheKey()
			switch args[0] {
			case "stop":
				_ = os.Remove(turnPath(key))
				return nil
			case "start":
				cfg := store.LoadConfig().Breathe
				if !cfg.Enabled {
					return nil
				}
				if err := os.MkdirAll(breatheDir(), 0o755); err != nil {
					return nil
				}
				token := strconv.FormatInt(time.Now().UnixNano(), 10)
				if err := os.WriteFile(turnPath(key), []byte(token), 0o644); err != nil {
					return nil
				}
				exe, err := pomoBinary()
				if err != nil {
					return nil
				}
				c := exec.Command(exe, "breathe", "open", key, token)
				c.SysProcAttr = &syscall.SysProcAttr{Setsid: true}
				if c.Start() == nil {
					_ = c.Process.Release()
				}
				return nil
			}
			return fmt.Errorf("unknown hook %q", args[0])
		},
	}
}

// breatheOpenCmd waits out the delay, then opens the pane if the turn is
// still running and no pane is showing here.
func breatheOpenCmd() *cobra.Command {
	return &cobra.Command{
		Use:    "open <key> <token>",
		Hidden: true,
		Args:   cobra.ExactArgs(2),
		RunE: func(cmd *cobra.Command, args []string) error {
			key, token := args[0], args[1]
			cfg := store.LoadConfig().Breathe
			time.Sleep(time.Duration(cfg.Delay * float64(time.Second)))
			if b, err := os.ReadFile(turnPath(key)); err != nil || string(b) != token || paneOpen(key) {
				return nil
			}
			command, err := pomoCommand("breathe pane " + shellQuote(key))
			if err != nil {
				return nil
			}
			_, _ = launch.OpenBelow(command)
			return nil
		},
	}
}

// breathePaneCmd is the pane itself: the animation until the turn ends.
func breathePaneCmd() *cobra.Command {
	return &cobra.Command{
		Use:    "pane <key>",
		Hidden: true,
		Args:   cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			key := args[0]
			if err := os.MkdirAll(breatheDir(), 0o755); err == nil {
				_ = os.WriteFile(panePath(key), []byte(strconv.Itoa(os.Getpid())), 0o644)
				defer os.Remove(panePath(key))
			}
			started := time.Now()
			cfg := store.LoadConfig().Breathe
			return ui.RunBreathe(ui.BreatheOptions{Exercise: cfg.Exercise, Style: cfg.Style, Done: func() bool {
				_, err := os.Stat(turnPath(key))
				return err != nil || time.Since(started) > breathePaneMaxAge
			}})
		},
	}
}

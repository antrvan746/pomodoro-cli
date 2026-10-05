// Package cli wires up the pomo command line.
package cli

import (
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"runtime"
	"strconv"
	"strings"
	"time"

	"github.com/charmbracelet/lipgloss"
	"github.com/muesli/termenv"
	"github.com/spf13/cobra"
	"golang.org/x/term"

	"github.com/antrvan746/pomodoro-cli/internal/daemon"
	"github.com/antrvan746/pomodoro-cli/internal/launch"
	"github.com/antrvan746/pomodoro-cli/internal/notify"
	"github.com/antrvan746/pomodoro-cli/internal/stats"
	"github.com/antrvan746/pomodoro-cli/internal/store"
	"github.com/antrvan746/pomodoro-cli/internal/ui"
	"github.com/antrvan746/pomodoro-cli/internal/wmux"
)

var Version = "dev"

var jsonOut, noOpen bool

func isTTY() bool {
	return term.IsTerminal(int(os.Stdout.Fd())) && term.IsTerminal(int(os.Stdin.Fd()))
}

func termWidth() int {
	if w, _, err := term.GetSize(int(os.Stdout.Fd())); err == nil && w > 0 {
		return w
	}
	if n, err := strconv.Atoi(os.Getenv("COLUMNS")); err == nil && n > 0 {
		return n
	}
	return 100
}

// detectSource labels who started a session, so agent-driven sessions are
// distinguishable in the history.
func detectSource() string {
	if os.Getenv("CLAUDECODE") != "" || os.Getenv("CLAUDE_CODE_ENTRYPOINT") != "" {
		return "claude"
	}
	for _, kv := range os.Environ() {
		if strings.HasPrefix(kv, "CODEX_") {
			return "codex"
		}
	}
	if isTTY() {
		return "cli"
	}
	return "script"
}

func printJSON(v any) error {
	enc := json.NewEncoder(os.Stdout)
	enc.SetIndent("", "  ")
	return enc.Encode(v)
}

var (
	accent = func(c lipgloss.Color) lipgloss.Style { return lipgloss.NewStyle().Foreground(c).Bold(true) }
	subtle = func() lipgloss.Style { return lipgloss.NewStyle().Foreground(ui.Muted) }
	bold   = func() lipgloss.Style { return lipgloss.NewStyle().Foreground(ui.Text).Bold(true) }
)

// ---------------------------------------------------------------------------
// Status payloads

type statusJSON struct {
	Active         bool       `json:"active"`
	AlreadyRunning bool       `json:"already_running,omitempty"`
	ClockOpened    string     `json:"clock_opened,omitempty"` // where the clock was opened, e.g. "cmux split"
	ID             string     `json:"id,omitempty"`
	Kind           store.Kind `json:"kind,omitempty"`
	Label          string     `json:"label,omitempty"`
	State          string     `json:"state"` // running | paused | idle
	DurationSec    int        `json:"duration_sec,omitempty"`
	ElapsedSec     int        `json:"elapsed_sec,omitempty"`
	RemainingSec   int        `json:"remaining_sec,omitempty"`
	Remaining      string     `json:"remaining,omitempty"`
	Progress       float64    `json:"progress,omitempty"`
	EndsAt         *time.Time `json:"ends_at,omitempty"`
	Source         string     `json:"source,omitempty"`
	Today          todayJSON  `json:"today"`
}

type todayJSON struct {
	Pomodoros  int `json:"pomodoros"`
	FocusMins  int `json:"focus_minutes"`
	Goal       int `json:"daily_goal"`
	StreakDays int `json:"streak_days"`
}

func summaryNow(days int) stats.Summary {
	recs, _ := store.History()
	return stats.Compute(recs, time.Now(), days, store.LoadConfig().DailyGoal)
}

func buildStatus(a *store.Active) statusJSON {
	s := summaryNow(1)
	out := statusJSON{State: "idle", Today: todayJSON{s.Today.Pomodoros, s.Today.FocusMins, s.Goal, s.CurrentStreak}}
	if a == nil {
		return out
	}
	now := time.Now()
	ends := a.EndsAt(now).Truncate(time.Second)
	out.Active, out.ID, out.Kind, out.Label, out.Source = true, a.ID, a.Kind, a.Label, a.Source
	out.State = "running"
	if a.Paused() {
		out.State = "paused"
	}
	out.DurationSec = int(a.Duration.D().Seconds())
	out.ElapsedSec = int(a.Elapsed(now).Seconds())
	out.RemainingSec = int(a.Remaining(now).Seconds())
	out.Remaining = ui.FmtClock(a.Remaining(now))
	out.Progress = float64(int(a.Progress(now)*1000)) / 1000
	out.EndsAt = &ends
	return out
}

func printStatusPretty(a *store.Active) {
	st := buildStatus(a)
	if a == nil {
		fmt.Printf("%s  %s\n", ui.Logo(), subtle().Render("idle — run `pomo` or `pomo start` to begin"))
	} else {
		now := time.Now()
		kind := a.Kind
		head := accent((ui.AccentFor(kind))).Render(ui.IconFor(kind) + " " + kind.Title())
		if a.Paused() {
			head = subtle().Render("⏸ Paused · ") + head
		}
		if a.Label != "" {
			head += subtle().Render(" · ") + lipgloss.NewStyle().Italic(true).Render(a.Label)
		}
		fmt.Printf("%s  %s\n", ui.Logo(), head)
		fmt.Printf("      %s %s  %s\n",
			bold().Render(ui.FmtClock(a.Remaining(now))), subtle().Render("left"),
			ui.SubBar(a.Progress(now), 16, ui.StopsFor(kind)))
		if !a.Paused() {
			fmt.Printf("      %s %s\n", subtle().Render("ends at"), bold().Render(a.EndsAt(now).Format("15:04")))
		}
	}
	goal := ""
	if st.Today.Goal > 0 {
		goal = fmt.Sprintf("/%d", st.Today.Goal)
	}
	fmt.Printf("      %s %s  %s %s  %s %s\n",
		subtle().Render("today"), bold().Render(fmt.Sprintf("%d%s 🍅", st.Today.Pomodoros, goal)),
		subtle().Render("focused"), bold().Render(ui.FmtMins(st.Today.FocusMins)),
		subtle().Render("streak"), bold().Render(fmt.Sprintf("%dd", st.Today.StreakDays)))
}

func announceFinished(r *store.Record) {
	if r == nil || jsonOut {
		return
	}
	title, body := notify.Message(r, store.LoadConfig())
	fmt.Fprintf(os.Stderr, "%s %s\n", accent(ui.Highlight).Render(title), subtle().Render(body))
}

// ---------------------------------------------------------------------------
// Commands

func Execute() {
	closePane := takeLaunchArgs()
	done := func() {
		if closePane {
			_ = wmux.CloseSurface()
		}
	}
	theme := os.Getenv("POMO_THEME")
	if theme == "" {
		theme = store.LoadConfig().Theme
	}
	ui.ApplyTheme(theme)
	// termenv only trusts COLORTERM on a TTY; honour it for piped/forced output too.
	if ct := os.Getenv("COLORTERM"); ct == "truecolor" || ct == "24bit" {
		if !isTTY() && os.Getenv("CLICOLOR_FORCE") != "" {
			lipgloss.SetColorProfile(termenv.TrueColor)
		}
	}
	root := newRoot()
	if err := root.Execute(); err != nil {
		if jsonOut {
			_ = printJSON(map[string]string{"error": err.Error()})
		} else {
			fmt.Fprintln(os.Stderr, accent(ui.Danger).Render("✗ ")+err.Error())
		}
		done()
		os.Exit(1)
	}
	done()
}

// takeLaunchArgs strips the flags pomo adds to commands it types into a new
// pane. --pomo-env=K=V sets an environment variable, so the line needs no
// shell syntax and runs the same in cmd, PowerShell and sh; --close-wmux-pane
// closes the wmux pane once the command ends.
func takeLaunchArgs() (closePane bool) {
	args := os.Args[:1]
	for _, a := range os.Args[1:] {
		switch {
		case strings.HasPrefix(a, "--pomo-env="):
			k, v, _ := strings.Cut(strings.TrimPrefix(a, "--pomo-env="), "=")
			_ = os.Setenv(k, v)
		case a == "--close-wmux-pane":
			closePane = true
		default:
			args = append(args, a)
		}
	}
	os.Args = args
	return closePane
}

func newRoot() *cobra.Command {
	root := &cobra.Command{
		Use:   "pomo",
		Short: "A colorful pomodoro timer for your terminal",
		Long: ui.Logo() + " — a colorful pomodoro timer for your terminal.\n\n" +
			"Run `pomo` for the full-screen timer. Every action is also a plain command\n" +
			"with --json output, so AI agents (Claude Code, Codex) can drive it too.",
		Version:       Version,
		SilenceUsage:  true,
		SilenceErrors: true,
		RunE: func(cmd *cobra.Command, args []string) error {
			if !isTTY() {
				return statusCmd().RunE(cmd, args)
			}
			return runTUI(ui.Options{})
		},
	}
	root.PersistentFlags().BoolVar(&jsonOut, "json", false, "machine-readable JSON output")
	root.PersistentFlags().BoolVar(&noOpen, "no-open", false, "don't open the clock in a new pane")
	root.AddCommand(startCmd(), watchCmd(), statusCmd(), pauseCmd(), resumeCmd(), toggleCmd(),
		stopCmd(), extendCmd(), labelCmd(), statsCmd(), logCmd(), configCmd(), agentCmd(), internalWatchCmd(),
		phaseCmd("break", "Start a short break", store.ShortBreak), phaseCmd("long", "Start a long break", store.LongBreak),
		skipCmd(), themeCmd(), statuslineCmd(), setupCmd(), breatheCmd())
	return root
}

func runTUI(opts ui.Options) error {
	if !isTTY() {
		return errors.New("the interactive timer needs a terminal; use `pomo start --detach`")
	}
	m, err := ui.Run(opts)
	if err != nil {
		return err
	}
	if m.Detached() {
		fmt.Println(ui.Logo() + subtle().Render("  timer keeps running in the background — `pomo watch` to reattach"))
	}
	return nil
}

func parseKind(brk, long bool) store.Kind {
	switch {
	case long:
		return store.LongBreak
	case brk:
		return store.ShortBreak
	}
	return store.Focus
}

// startDetached starts a background session and prints its status.
func startDetached(kind store.Kind, d time.Duration, label string, force bool) error {
	_, prev, _ := notify.Settle()
	announceFinished(prev)
	cycle := store.CycleNow()
	a, err := store.Start(kind, d, label, detectSource(), cycle, force)
	if errors.Is(err, store.ErrRunning) {
		// A timer started elsewhere (another terminal, Claude, Codex) is the
		// same timer: join it instead of failing.
		if cur, _ := store.Peek(); cur != nil {
			opened := maybeOpenClock(false)
			if jsonOut {
				st := buildStatus(cur)
				st.AlreadyRunning, st.ClockOpened = true, opened
				return printJSON(st)
			}
			fmt.Println(subtle().Render("already running (started from " + cur.Source + ") — showing it; use --force to start a new one"))
			printStatusPretty(cur)
			printOpened(opened)
			return nil
		}
	}
	if err != nil {
		return err
	}
	if err := notify.SpawnWatcher(a.ID); err != nil {
		fmt.Fprintln(os.Stderr, "warning: could not start background watcher:", err)
	}
	opened := maybeOpenClock(false)
	if jsonOut {
		st := buildStatus(a)
		st.ClockOpened = opened
		return printJSON(st)
	}
	printStatusPretty(a)
	printOpened(opened)
	return nil
}

// maybeOpenClock opens the full-screen clock in a pane next to the caller when
// pomo was run without a terminal (from Claude Code, Codex…), per the
// open_clock setting. It never opens a second clock in the same workspace.
func maybeOpenClock(explicit bool) string {
	mode := store.LoadConfig().OpenClock
	if !explicit && (noOpen || mode == "never" || (mode != "always" && isTTY())) {
		return ""
	}
	if store.ViewerOpen() {
		return ""
	}
	command, err := pomoCommand("watch --auto-close")
	if err != nil {
		return ""
	}
	where, err := launch.Open(command)
	if err != nil {
		if explicit {
			fmt.Fprintln(os.Stderr, "couldn't open the clock:", err)
		}
		return ""
	}
	return where
}

// pomoCommand is a shell command line running this pomo binary with args,
// carrying over the settings that pick its data and theme.
func pomoCommand(args string) (string, error) {
	exe, err := pomoBinary()
	if err != nil {
		return "", err
	}
	var envs []string
	for _, k := range []string{"POMO_HOME", "XDG_DATA_HOME", "POMO_THEME"} {
		if v := os.Getenv(k); v != "" {
			envs = append(envs, k+"="+v)
		}
	}
	if runtime.GOOS == "windows" {
		return windowsCommand(exe, args, envs), nil
	}
	for i, e := range envs {
		k, v, _ := strings.Cut(e, "=")
		envs[i] = k + "=" + shellQuote(v)
	}
	command := shellQuote(exe) + " " + args
	if len(envs) > 0 {
		command = "env " + strings.Join(envs, " ") + " " + command
	}
	return command, nil
}

// windowsCommand builds the line typed into a new wmux pane. Its shell may be
// cmd, PowerShell or Git Bash, so the line is just a program and arguments:
// environment variables travel as --pomo-env flags, and a path with spaces is
// double-quoted, which all three read the same way.
func windowsCommand(exe, args string, envs []string) string {
	parts := []string{winQuote(filepath.ToSlash(exe))}
	for _, e := range envs {
		parts = append(parts, winQuote("--pomo-env="+e))
	}
	return strings.Join(append(parts, args), " ")
}

func winQuote(s string) string {
	if strings.ContainsAny(s, " \t\\&()^|<>'%!") {
		return `"` + s + `"`
	}
	return s
}

// argQuote quotes one argument for the line pomoCommand builds.
func argQuote(s string) string {
	if runtime.GOOS == "windows" {
		return winQuote(s)
	}
	return shellQuote(s)
}

func printOpened(where string) {
	if where != "" {
		fmt.Println(subtle().Render("      clock opened in a " + where + " — q closes it, the timer keeps running"))
	}
}

func startCmd() *cobra.Command {
	var (
		minutes           float64
		label             string
		brk, long, detach bool
		force             bool
	)
	cmd := &cobra.Command{
		Use:   "start [minutes] [label...]",
		Short: "Start a focus session (or a break)",
		Example: `  pomo start                       # 25 min focus in the full-screen timer
  pomo start 50 write the RFC      # 50 min focus with a label
  pomo start "write RFC" -m 50     # same, with flags
  pomo start --break               # short break
  pomo start -d --json             # start in the background, print JSON (for agents)`,
		RunE: func(cmd *cobra.Command, args []string) error {
			cfg := store.LoadConfig()
			kind := parseKind(brk, long)
			if len(args) > 0 {
				var n float64
				if _, err := fmt.Sscanf(args[0], "%g", &n); err == nil && n > 0 {
					minutes, args = n, args[1:]
				}
			}
			if len(args) > 0 {
				label = strings.Join(args, " ")
			}
			d := cfg.Duration(kind)
			if minutes > 0 {
				d = time.Duration(minutes * float64(time.Minute))
			}
			if !detach && isTTY() && !jsonOut {
				if a, _, _ := notify.Settle(); a != nil && !force {
					return runTUI(ui.Options{}) // attach to the running timer
				} else if a != nil {
					if _, err := store.Stop(a.ID); err != nil {
						return err
					}
				}
				return runTUI(ui.Options{StartKind: kind, Duration: d, Label: label})
			}
			return startDetached(kind, d, label, force)
		},
	}
	f := cmd.Flags()
	f.Float64VarP(&minutes, "minutes", "m", 0, "session length in minutes (default from config)")
	f.StringVarP(&label, "label", "l", "", "what you're working on")
	f.BoolVarP(&brk, "break", "b", false, "start a short break")
	f.BoolVarP(&long, "long", "L", false, "start a long break")
	f.BoolVarP(&detach, "detach", "d", false, "run in the background without the full-screen UI")
	f.BoolVarP(&force, "force", "f", false, "replace a running session")
	return cmd
}

func watchCmd() *cobra.Command {
	var autoClose bool
	cmd := &cobra.Command{
		Use:     "watch",
		Aliases: []string{"ui", "attach", "open"},
		Short:   "Open the full-screen timer (attaches to a running session)",
		Long: "Opens the full-screen timer. Run without a terminal (e.g. /pomo watch in\n" +
			"Claude Code) it opens the clock in a new pane or window instead.",
		RunE: func(cmd *cobra.Command, args []string) error {
			if !isTTY() {
				if store.ViewerOpen() {
					fmt.Println(subtle().Render("the clock is already open"))
					return nil
				}
				where := maybeOpenClock(true)
				if where == "" {
					return errors.New("couldn't find a terminal to open the clock in; run `pomo watch` in a terminal")
				}
				fmt.Println(accent(ui.Highlight).Render("🖥 clock opened") + subtle().Render(" in a "+where))
				return nil
			}
			return runTUI(ui.Options{AutoClose: autoClose})
		},
	}
	cmd.Flags().BoolVar(&autoClose, "auto-close", false, "quit when the timer is stopped")
	return cmd
}

func statusCmd() *cobra.Command {
	var short, context bool
	cmd := &cobra.Command{
		Use:     "status",
		Aliases: []string{"st"},
		Short:   "Show the current session",
		Long:    "Show the current session. --short prints a compact line for tmux/shell prompts.",
		RunE: func(cmd *cobra.Command, args []string) error {
			a, fin, err := notify.Settle()
			if err != nil {
				return err
			}
			if jsonOut {
				return printJSON(buildStatus(a))
			}
			if context {
				// One sentence for an agent's context (Claude Code SessionStart hook).
				if a != nil {
					st := buildStatus(a)
					label := ""
					if a.Label != "" {
						label = fmt.Sprintf(" (%q)", a.Label)
					}
					fmt.Printf("pomo: a %s session%s is %s with %s left, started from %s. Today: %d/%d pomodoros. Use /pomo to control it.\n",
						strings.ToLower(a.Kind.Title()), label, st.State, st.Remaining, a.Source, st.Today.Pomodoros, st.Today.Goal)
				}
				return nil
			}
			if short {
				if a != nil {
					icon := ui.IconFor(a.Kind)
					if a.Paused() {
						icon = "⏸"
					}
					fmt.Printf("%s %s\n", icon, ui.FmtClock(a.Remaining(time.Now())))
				}
				return nil
			}
			announceFinished(fin)
			printStatusPretty(a)
			return nil
		},
	}
	cmd.Flags().BoolVarP(&short, "short", "s", false, "one-line output, e.g. for a status bar")
	cmd.Flags().BoolVar(&context, "context", false, "one sentence for an AI agent's context; empty when idle")
	return cmd
}

func simpleMutation(use, short string, fn func() (*store.Active, error), verb string) *cobra.Command {
	return &cobra.Command{
		Use:   use,
		Short: short,
		RunE: func(cmd *cobra.Command, args []string) error {
			if _, fin, err := notify.Settle(); err != nil {
				return err
			} else {
				announceFinished(fin)
			}
			a, err := fn()
			if err != nil {
				return err
			}
			if jsonOut {
				return printJSON(buildStatus(a))
			}
			fmt.Println(accent(ui.Key).Render(verb) + subtle().Render(" · "+a.Kind.Title()+" · "+ui.FmtClock(a.Remaining(time.Now()))+" left"))
			return nil
		},
	}
}

func pauseCmd() *cobra.Command {
	return simpleMutation("pause", "Pause the current session", store.Pause, "⏸ Paused")
}
func resumeCmd() *cobra.Command {
	return simpleMutation("resume", "Resume a paused session", store.Resume, "▶ Resumed")
}
func toggleCmd() *cobra.Command {
	return simpleMutation("toggle", "Pause or resume", store.TogglePause, "⏯ Toggled")
}

func extendCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "extend [minutes]",
		Short: "Add minutes to the current session (negative to shorten)",
		Args:  cobra.MaximumNArgs(1),
	}
	cmd.RunE = func(c *cobra.Command, args []string) error {
		m := 5.0
		if len(args) == 1 {
			if _, err := fmt.Sscanf(args[0], "%g", &m); err != nil {
				return fmt.Errorf("invalid minutes %q", args[0])
			}
		}
		return simpleMutation("", "", func() (*store.Active, error) {
			return store.Extend(time.Duration(m * float64(time.Minute)))
		}, fmt.Sprintf("⏱ Extended by %gm", m)).RunE(c, args)
	}
	return cmd
}

func labelCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "label <text>",
		Short: "Set the label of the current session",
		Args:  cobra.ExactArgs(1),
		RunE: func(c *cobra.Command, args []string) error {
			return simpleMutation("", "", func() (*store.Active, error) { return store.SetLabel(args[0]) },
				"🏷 Labeled “"+args[0]+"”").RunE(c, args)
		},
	}
}

func stopCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "stop",
		Short: "Stop the current session early (time so far is still recorded)",
		RunE: func(cmd *cobra.Command, args []string) error {
			if _, fin, err := notify.Settle(); err != nil {
				return err
			} else if fin != nil {
				announceFinished(fin)
				if jsonOut {
					return printJSON(fin)
				}
				return nil
			}
			rec, err := store.Stop("")
			if err != nil {
				return err
			}
			if jsonOut {
				return printJSON(rec)
			}
			fmt.Println(accent(ui.Danger).Render("■ Stopped") +
				subtle().Render(fmt.Sprintf(" · %s after %s", rec.Kind.Title(), ui.FmtDur(rec.Actual.D()))))
			return nil
		},
	}
}

func statsCmd() *cobra.Command {
	var weeks int
	var interactive bool
	cmd := &cobra.Command{
		Use:   "stats",
		Short: "Show statistics and the activity heatmap",
		RunE: func(cmd *cobra.Command, args []string) error {
			if interactive {
				return runTUI(ui.Options{Stats: true})
			}
			if _, fin, err := notify.Settle(); err == nil {
				announceFinished(fin)
			}
			s := summaryNow(54 * 7)
			if jsonOut {
				if weeks > 0 && weeks*7 < len(s.Days) {
					s.Days = s.Days[len(s.Days)-weeks*7:]
				}
				return printJSON(s)
			}
			w := termWidth() - 2
			if weeks > 0 {
				if fit := 4 + 6 + 2*weeks; fit > w {
					w = fit
				}
			}
			fmt.Println()
			fmt.Println(lipgloss.NewStyle().PaddingLeft(1).Render(ui.StatsView(s, w, time.Now())))
			fmt.Println()
			return nil
		},
	}
	cmd.Flags().IntVarP(&weeks, "weeks", "w", 0, "limit JSON days to the last N weeks")
	cmd.Flags().BoolVarP(&interactive, "interactive", "i", false, "open stats in the full-screen UI")
	return cmd
}

func logCmd() *cobra.Command {
	var n int
	var all bool
	cmd := &cobra.Command{
		Use:     "log",
		Aliases: []string{"history"},
		Short:   "List recent sessions",
		RunE: func(cmd *cobra.Command, args []string) error {
			recs, err := store.History()
			if err != nil {
				return err
			}
			if !all {
				var f []store.Record
				for _, r := range recs {
					if r.Kind == store.Focus {
						f = append(f, r)
					}
				}
				recs = f
			}
			if n > 0 && len(recs) > n {
				recs = recs[len(recs)-n:]
			}
			if jsonOut {
				if recs == nil {
					recs = []store.Record{}
				}
				return printJSON(recs)
			}
			if len(recs) == 0 {
				fmt.Println(subtle().Render("No sessions yet. Start one with `pomo`."))
				return nil
			}
			lastDay := ""
			for i := len(recs) - 1; i >= 0; i-- {
				r := recs[i]
				day := r.StartedAt.Local().Format("Mon 02 Jan")
				if day != lastDay {
					fmt.Println("\n" + ui.GradientText(day, ui.LogoStops, true))
					lastDay = day
				}
				mark := accent((ui.AccentFor(r.Kind))).Render("●")
				if !r.Completed {
					mark = subtle().Render("○")
				}
				label := r.Label
				if label == "" {
					label = subtle().Render(r.Kind.Title())
				}
				fmt.Printf("  %s %s  %s  %s %s\n", mark,
					subtle().Render(r.StartedAt.Local().Format("15:04")+"–"+r.EndedAt.Local().Format("15:04")),
					bold().Render(fmt.Sprintf("%6s", ui.FmtDur(r.Actual.D()))), label,
					subtle().Render("· "+r.Source))
			}
			fmt.Println()
			return nil
		},
	}
	cmd.Flags().IntVarP(&n, "limit", "n", 20, "number of sessions to show (0 = all)")
	cmd.Flags().BoolVarP(&all, "all", "a", false, "include breaks")

	var minutes float64
	var label, at string
	add := &cobra.Command{
		Use:   "add",
		Short: "Record a completed focus session after the fact",
		RunE: func(cmd *cobra.Command, args []string) error {
			end := time.Now()
			if at != "" {
				t, err := time.ParseInLocation("2006-01-02 15:04", at, time.Local)
				if err != nil {
					return fmt.Errorf("--at must look like \"2026-10-02 14:30\": %w", err)
				}
				end = t
			}
			d := time.Duration(minutes * float64(time.Minute))
			r := &store.Record{
				ID: store.NewID(), Kind: store.Focus, Label: label, Planned: store.Seconds(d),
				Actual: store.Seconds(d), StartedAt: end.Add(-d), EndedAt: end, Completed: true, Source: "manual",
			}
			if err := store.AddRecord(r); err != nil {
				return err
			}
			if jsonOut {
				return printJSON(r)
			}
			fmt.Println(accent(ui.Highlight).Render("🍅 Logged") + subtle().Render(fmt.Sprintf(" · %s ending %s", ui.FmtDur(d), end.Format("Mon 15:04"))))
			return nil
		},
	}
	add.Flags().Float64VarP(&minutes, "minutes", "m", 25, "length in minutes")
	add.Flags().StringVarP(&label, "label", "l", "", "label")
	add.Flags().StringVar(&at, "at", "", `end time, "YYYY-MM-DD HH:MM" (default now)`)
	cmd.AddCommand(add)
	return cmd
}

func internalWatchCmd() *cobra.Command {
	return &cobra.Command{
		Use:    "__watch <id>",
		Hidden: true,
		Args:   cobra.ExactArgs(1),
		RunE:   func(cmd *cobra.Command, args []string) error { daemon.Watch(args[0]); return nil },
	}
}

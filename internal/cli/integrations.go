package cli

import (
	"bytes"
	_ "embed"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"time"

	"github.com/charmbracelet/lipgloss"
	"github.com/muesli/termenv"
	"github.com/spf13/cobra"

	"github.com/antrvan746/pomodoro-cli/internal/notify"
	"github.com/antrvan746/pomodoro-cli/internal/store"
	"github.com/antrvan746/pomodoro-cli/internal/ui"
)

// ---------------------------------------------------------------------------
// Status line

func statuslineCmd() *cobra.Command {
	var wrap, view string
	cmd := &cobra.Command{
		Use:   "statusline",
		Short: "Print a one-line timer segment for Claude Code / tmux status bars",
		Long: "Prints e.g. `🍅 22:47 ▕████████░░▏ ●●○○`, or nothing when idle.\n\n" +
			"--wrap runs your existing status-line command with the same stdin and\n" +
			"appends its output, so pomo never replaces what you already have.",
		RunE: func(cmd *cobra.Command, args []string) error {
			in := readStdinQuick()
			if os.Getenv("NO_COLOR") == "" {
				lipgloss.SetColorProfile(termenv.TrueColor)
			}
			seg := ""
			if os.Getenv("POMO_HIDE") == "" {
				if a, _, err := notify.Settle(); err == nil {
					cfg := store.LoadConfig()
					if view == "" {
						view = cfg.StatusView
					}
					seg = ui.StatusSegment(a, cfg, view, time.Now())
				}
			}
			rest := ""
			if wrap != "" {
				rest = runWrapped(wrap, in)
			}
			first, more, _ := strings.Cut(rest, "\n")
			line := first
			switch {
			case seg != "" && first != "":
				line = seg + "  " + first
			case seg != "":
				line = seg
			}
			if line == "" && more == "" {
				return nil
			}
			// Leading reset stops colour bleeding in from the host.
			fmt.Print("\x1b[0m" + line + "\x1b[0m")
			if more != "" {
				fmt.Print("\n" + more)
			}
			fmt.Println()
			return nil
		},
	}
	cmd.Flags().StringVar(&wrap, "wrap", "", "existing status-line command to run and append")
	cmd.Flags().StringVar(&view, "view", "", "minimal | classic | full (default from config)")
	return cmd
}

func readStdinQuick() []byte {
	if isTTYFd(os.Stdin) {
		return nil
	}
	ch := make(chan []byte, 1)
	go func() { b, _ := io.ReadAll(os.Stdin); ch <- b }()
	select {
	case b := <-ch:
		return b
	case <-time.After(300 * time.Millisecond):
		return nil
	}
}

func isTTYFd(f *os.File) bool {
	st, err := f.Stat()
	return err == nil && st.Mode()&os.ModeCharDevice != 0
}

func runWrapped(command string, stdin []byte) string {
	c := exec.Command("sh", "-c", command)
	c.Stdin = bytes.NewReader(stdin)
	c.Stderr = io.Discard
	out, _ := c.Output()
	return strings.TrimRight(string(out), "\n")
}

// ---------------------------------------------------------------------------
// Setup

//go:embed codex_prompt.md
var codexPromptMD string

func setupCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "setup",
		Short: "Integrate pomo with Claude Code or Codex",
	}
	var uninstall bool
	claude := &cobra.Command{
		Use:   "claude",
		Short: "Show the timer in Claude Code's status line (wraps your existing one)",
		Long: "Adds pomo to the statusLine in ~/.claude/settings.json. An existing status\n" +
			"line command is kept and wrapped. A backup is written first, and\n" +
			"--uninstall restores exactly what was there.\n\n" +
			"For the /pomo slash command, install the plugin (see README).",
		RunE: func(cmd *cobra.Command, args []string) error {
			if uninstall {
				return uninstallClaude()
			}
			return setupClaude()
		},
	}
	claude.Flags().BoolVar(&uninstall, "uninstall", false, "restore the previous status line")

	var codexUninstall bool
	codex := &cobra.Command{
		Use:   "codex",
		Short: "Install the /prompts:pomo command, the pomo skill and the breathing hooks for Codex",
		RunE: func(cmd *cobra.Command, args []string) error {
			home, err := os.UserHomeDir()
			if err != nil {
				return err
			}
			dir := os.Getenv("CODEX_HOME")
			if dir == "" {
				dir = filepath.Join(home, ".codex")
			}
			files := map[string]string{
				filepath.Join(dir, "prompts", "pomo.md"):         codexPromptMD,
				filepath.Join(dir, "skills", "pomo", "SKILL.md"): skillMD,
			}
			for p, body := range files {
				if codexUninstall {
					if err := os.Remove(p); err == nil {
						fmt.Println(accent(ui.Highlight).Render("− ") + "removed " + p)
					}
					continue
				}
				if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
					return err
				}
				if err := os.WriteFile(p, []byte(body), 0o644); err != nil {
					return err
				}
				fmt.Println(accent(ui.Highlight).Render("✓ ") + "wrote " + p)
			}
			hooksPath := filepath.Join(dir, "hooks.json")
			changed, err := setCodexBreatheHooks(hooksPath, !codexUninstall)
			if err != nil {
				return fmt.Errorf("%s: %w", hooksPath, err)
			}
			if changed && codexUninstall {
				fmt.Println(accent(ui.Highlight).Render("− ") + "removed the breathing hooks from " + hooksPath)
			} else if changed {
				fmt.Println(accent(ui.Highlight).Render("✓ ") + "added the breathing hooks to " + hooksPath)
			}
			if !codexUninstall {
				fmt.Println(subtle().Render("\n  Restart Codex (and trust the new hooks when it asks), then try:\n" +
					"  /prompts:pomo start 25 \"write tests\"   ·   pomo breathe box"))
			}
			return nil
		},
	}
	codex.Flags().BoolVar(&codexUninstall, "uninstall", false, "remove the files again")
	cmd.AddCommand(claude, codex)
	return cmd
}

// codexBreatheEvents maps Codex hook events to `pomo breathe hook` actions.
var codexBreatheEvents = [][2]string{{"UserPromptSubmit", "start"}, {"Stop", "stop"}}

// isBreatheHook reports whether a hooks.json matcher group is one of ours.
func isBreatheHook(group any) bool {
	g, _ := group.(map[string]any)
	hooks, _ := g["hooks"].([]any)
	for _, h := range hooks {
		hm, _ := h.(map[string]any)
		if c, _ := hm["command"].(string); strings.Contains(c, " breathe hook ") {
			return true
		}
	}
	return false
}

// setCodexBreatheHooks adds (or removes) pomo's breathing hooks in Codex's
// hooks.json, leaving every other hook alone. It reports whether it changed
// the file.
func setCodexBreatheHooks(path string, install bool) (bool, error) {
	obj, err := readOrderedJSON(path)
	if err != nil {
		return false, err
	}
	// Other hooks are kept byte for byte: Codex trusts each by its hash.
	hooks := &orderedJSON{vals: map[string]json.RawMessage{}}
	if raw := obj.get("hooks"); raw != nil {
		if hooks, err = parseOrderedJSON(raw); err != nil {
			return false, err
		}
	}
	exe, err := pomoBinary()
	if err != nil {
		return false, err
	}
	changed := false
	for _, ev := range codexBreatheEvents {
		var groups []json.RawMessage
		if raw := hooks.get(ev[0]); raw != nil {
			if err := json.Unmarshal(raw, &groups); err != nil {
				return false, err
			}
		}
		kept := groups[:0:0]
		for _, g := range groups {
			var v any
			_ = json.Unmarshal(g, &v)
			if !isBreatheHook(v) {
				kept = append(kept, g)
			}
		}
		if install {
			ours, _ := json.Marshal(map[string]any{"hooks": []any{map[string]any{
				"type": "command", "command": shellQuote(exe) + " breathe hook " + ev[1], "timeout": 5,
			}}})
			kept = append(kept, ours)
		}
		next, _ := json.Marshal(kept)
		if len(kept) == 0 {
			next = nil
		}
		if prev := hooks.get(ev[0]); !jsonEqual(prev, next) {
			changed = true
			if next == nil {
				hooks.del(ev[0])
			} else {
				hooks.set(ev[0], next)
			}
		}
	}
	if !changed {
		return false, nil
	}
	if len(hooks.keys) == 0 {
		obj.del("hooks")
	} else {
		obj.set("hooks", hooks.marshal())
	}
	return true, writeOrderedJSON(path, obj)
}

// jsonEqual compares two JSON documents by value; nil is no document.
func jsonEqual(a, b json.RawMessage) bool {
	if a == nil || b == nil {
		return a == nil && b == nil
	}
	var x, y any
	if json.Unmarshal(a, &x) != nil || json.Unmarshal(b, &y) != nil {
		return false
	}
	xb, _ := json.Marshal(x)
	yb, _ := json.Marshal(y)
	return bytes.Equal(xb, yb)
}

func claudeDir() string {
	if d := os.Getenv("CLAUDE_CONFIG_DIR"); d != "" {
		return d
	}
	home, _ := os.UserHomeDir()
	return filepath.Join(home, ".claude")
}

type setupRecord struct {
	Settings string          `json:"settings"`
	Previous json.RawMessage `json:"previous"` // null when there was none
	Backup   string          `json:"backup"`
}

func setupRecordPath() string { return filepath.Join(store.Dir(), "claude-setup.json") }

func pomoBinary() (string, error) {
	exe, err := os.Executable()
	if err != nil {
		return "", err
	}
	if r, err := filepath.EvalSymlinks(exe); err == nil {
		exe = r
	}
	return exe, nil
}

func shellQuote(s string) string { return "'" + strings.ReplaceAll(s, "'", `'\''`) + "'" }

func setupClaude() error {
	settingsPath := filepath.Join(claudeDir(), "settings.json")
	obj, err := readOrderedJSON(settingsPath)
	if err != nil {
		return fmt.Errorf("can't parse %s (fix it, then re-run): %w", settingsPath, err)
	}
	exe, err := pomoBinary()
	if err != nil {
		return err
	}

	prev := obj.get("statusLine")
	var prevLine struct {
		Command string `json:"command"`
		Padding *int   `json:"padding"`
	}
	if prev != nil {
		_ = json.Unmarshal(prev, &prevLine)
	}
	if strings.HasPrefix(prevLine.Command, shellQuote(exe)+" statusline") {
		fmt.Println(accent(ui.Highlight).Render("✓ ") + "pomo is already in your Claude Code status line")
		return nil
	}

	command := shellQuote(exe) + " statusline"
	if prevLine.Command != "" {
		command += " --wrap " + shellQuote(prevLine.Command)
	}
	// Spawning processes is slow on Windows (Claude Code runs this through Git
	// Bash, then sh for --wrap): at 1 s, runs pile up faster than they finish.
	refresh := 1
	if runtime.GOOS == "windows" {
		refresh = 10
	}
	line := map[string]any{"type": "command", "command": command, "refreshInterval": refresh}
	if prevLine.Padding != nil {
		line["padding"] = *prevLine.Padding
	}

	backup := ""
	if b, err := os.ReadFile(settingsPath); err == nil {
		backup = settingsPath + ".pomo-backup-" + time.Now().Format("20060102-150405")
		if err := os.WriteFile(backup, b, 0o644); err != nil {
			return err
		}
	}
	raw, _ := json.Marshal(line)
	obj.set("statusLine", raw)
	if err := writeOrderedJSON(settingsPath, obj); err != nil {
		return err
	}
	if prev == nil {
		prev = json.RawMessage("null")
	}
	// Keep the original "previous" from an earlier setup, so re-running setup
	// can never make uninstall restore pomo's own line.
	if old, err := os.ReadFile(setupRecordPath()); err == nil {
		var r setupRecord
		if json.Unmarshal(old, &r) == nil && r.Settings == settingsPath && len(r.Previous) > 0 {
			prev = r.Previous
		}
	}
	rec, _ := json.MarshalIndent(setupRecord{Settings: settingsPath, Previous: prev, Backup: backup}, "", "  ")
	_ = os.MkdirAll(store.Dir(), 0o755)
	if err := os.WriteFile(setupRecordPath(), rec, 0o644); err != nil {
		return err
	}

	fmt.Println(accent(ui.Highlight).Render("✓ ") + "status line set in " + settingsPath)
	if prevLine.Command != "" {
		fmt.Println(subtle().Render("  kept your existing status line: " + prevLine.Command))
	}
	if backup != "" {
		fmt.Println(subtle().Render("  backup: " + backup))
	}
	fmt.Println(subtle().Render("  undo with: pomo setup claude --uninstall"))
	return nil
}

func uninstallClaude() error {
	b, err := os.ReadFile(setupRecordPath())
	if errors.Is(err, os.ErrNotExist) {
		return errors.New("pomo isn't set up in Claude Code (nothing to undo)")
	}
	if err != nil {
		return err
	}
	var rec setupRecord
	if err := json.Unmarshal(b, &rec); err != nil {
		return err
	}
	obj, err := readOrderedJSON(rec.Settings)
	if err != nil {
		return err
	}
	if string(rec.Previous) == "null" || len(rec.Previous) == 0 {
		obj.del("statusLine")
	} else {
		obj.set("statusLine", rec.Previous)
	}
	if err := writeOrderedJSON(rec.Settings, obj); err != nil {
		return err
	}
	_ = os.Remove(setupRecordPath())
	fmt.Println(accent(ui.Highlight).Render("✓ ") + "restored your previous status line in " + rec.Settings)
	return nil
}

// orderedJSON is a top-level JSON object that keeps its key order, so we don't
// reshuffle the user's settings file.
type orderedJSON struct {
	keys []string
	vals map[string]json.RawMessage
}

func (o *orderedJSON) get(k string) json.RawMessage { return o.vals[k] }
func (o *orderedJSON) set(k string, v json.RawMessage) {
	if _, ok := o.vals[k]; !ok {
		o.keys = append(o.keys, k)
	}
	o.vals[k] = v
}
func (o *orderedJSON) del(k string) {
	delete(o.vals, k)
	for i, key := range o.keys {
		if key == k {
			o.keys = append(o.keys[:i], o.keys[i+1:]...)
			break
		}
	}
}

func readOrderedJSON(path string) (*orderedJSON, error) {
	o := &orderedJSON{vals: map[string]json.RawMessage{}}
	b, err := os.ReadFile(path)
	if errors.Is(err, os.ErrNotExist) || (err == nil && len(bytes.TrimSpace(b)) == 0) {
		return o, nil
	}
	if err != nil {
		return nil, err
	}
	return parseOrderedJSON(b)
}

func parseOrderedJSON(b []byte) (*orderedJSON, error) {
	o := &orderedJSON{vals: map[string]json.RawMessage{}}
	dec := json.NewDecoder(bytes.NewReader(b))
	if t, err := dec.Token(); err != nil || t != json.Delim('{') {
		return nil, errors.New("expected a JSON object")
	}
	for dec.More() {
		t, err := dec.Token()
		if err != nil {
			return nil, err
		}
		var v json.RawMessage
		if err := dec.Decode(&v); err != nil {
			return nil, err
		}
		o.set(t.(string), v)
	}
	return o, nil
}

// marshal encodes o compactly, keys in their original order.
func (o *orderedJSON) marshal() json.RawMessage {
	var b bytes.Buffer
	b.WriteString("{")
	for i, k := range o.keys {
		if i > 0 {
			b.WriteString(",")
		}
		kb, _ := json.Marshal(k)
		b.Write(kb)
		b.WriteString(":")
		b.Write(o.vals[k])
	}
	b.WriteString("}")
	return b.Bytes()
}

func writeOrderedJSON(path string, o *orderedJSON) error {
	var b bytes.Buffer
	b.WriteString("{\n")
	for i, k := range o.keys {
		kb, _ := json.Marshal(k)
		var v bytes.Buffer
		if err := json.Indent(&v, o.vals[k], "  ", "  "); err != nil {
			return err
		}
		fmt.Fprintf(&b, "  %s: %s", kb, v.String())
		if i < len(o.keys)-1 {
			b.WriteString(",")
		}
		b.WriteString("\n")
	}
	b.WriteString("}\n")
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return err
	}
	tmp := path + ".pomo-tmp"
	if err := os.WriteFile(tmp, b.Bytes(), 0o644); err != nil {
		return err
	}
	return os.Rename(tmp, path)
}

// ---------------------------------------------------------------------------
// Theme

func themeCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "theme [name]",
		Short: "List colour themes, or switch to one",
		Args:  cobra.MaximumNArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			cfg := store.LoadConfig()
			if len(args) == 1 {
				name := strings.ToLower(args[0])
				valid := false
				for _, n := range ui.ThemeNames() {
					valid = valid || n == name
				}
				if !valid {
					return fmt.Errorf("unknown theme %q (choose from: %s)", name, strings.Join(ui.ThemeNames(), ", "))
				}
				cfg.Theme = name
				if err := store.SaveConfig(cfg); err != nil {
					return err
				}
				ui.ApplyTheme(name)
				if jsonOut {
					return printJSON(map[string]string{"theme": name})
				}
				fmt.Println(ui.Logo() + subtle().Render("  theme → ") + bold().Render(name))
				return nil
			}
			if jsonOut {
				return printJSON(map[string]any{"theme": cfg.Theme, "available": ui.ThemeNames()})
			}
			fmt.Println(ui.Logo() + subtle().Render("  themes"))
			fmt.Println()
			for _, n := range ui.ThemeNames() {
				t := ui.ApplyTheme(n)
				mark := "  "
				if n == cfg.Theme {
					mark = accent(ui.Highlight).Render("● ")
				}
				desc := t.Desc
				if n == "catppuccin" {
					desc = "Catppuccin — Mocha on dark terminals, Latte on light (now: " + t.Name + ")"
				}
				fmt.Printf("%s%s %s\n    %s\n\n", mark, lipgloss.NewStyle().Foreground(ui.Text).Bold(true).Width(11).Render(n),
					lipgloss.NewStyle().Foreground(ui.Muted).Render(desc), ui.Swatch(t))
			}
			ui.ApplyTheme(cfg.Theme)
			fmt.Println(subtle().Render("  switch with: pomo theme <name>   (or press c in the timer)"))
			return nil
		},
	}
}

// ---------------------------------------------------------------------------
// Phase shortcuts: break, long, skip

func phaseCmd(use, short string, kind store.Kind) *cobra.Command {
	var minutes float64
	var force bool
	cmd := &cobra.Command{
		Use:   use + " [minutes]",
		Short: short,
		Args:  cobra.MaximumNArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			cfg := store.LoadConfig()
			d := cfg.Duration(kind)
			if len(args) == 1 {
				if _, err := fmt.Sscanf(args[0], "%g", &minutes); err != nil {
					return fmt.Errorf("invalid minutes %q", args[0])
				}
			}
			if minutes > 0 {
				d = time.Duration(minutes * float64(time.Minute))
			}
			return startDetached(kind, d, "", force)
		},
	}
	cmd.Flags().BoolVarP(&force, "force", "f", false, "replace a running session")
	return cmd
}

func skipCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "skip",
		Short: "End the current phase now and start the next one",
		RunE: func(cmd *cobra.Command, args []string) error {
			a, _, err := notify.Settle()
			if err != nil {
				return err
			}
			if a == nil {
				return store.ErrNoSession
			}
			rec, err := store.Stop(a.ID)
			if err != nil {
				return err
			}
			cfg := store.LoadConfig()
			kind, cycle := store.Next(rec, rec.Cycle, cfg)
			next, err := store.Start(kind, cfg.Duration(kind), "", rec.Source, cycle, false)
			if err != nil {
				return err
			}
			_ = notify.SpawnWatcher(next.ID)
			opened := maybeOpenClock(false)
			if jsonOut {
				st := buildStatus(next)
				st.ClockOpened = opened
				return printJSON(st)
			}
			fmt.Println(accent(ui.Key).Render("⏭ Skipped "+strings.ToLower(rec.Kind.Title())) + subtle().Render(" → "+kind.Title()))
			printStatusPretty(next)
			printOpened(opened)
			return nil
		},
	}
}

package ui

import (
	"fmt"
	"strings"
	"time"

	"github.com/charmbracelet/bubbles/textinput"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"

	"github.com/antrvan746/pomodoro-cli/internal/notify"
	"github.com/antrvan746/pomodoro-cli/internal/stats"
	"github.com/antrvan746/pomodoro-cli/internal/store"
)

type view int

const (
	viewTimer view = iota
	viewStats
)

type tickMsg time.Time

// spawnWatcher is swapped out in tests, where os.Executable is the test binary.
var spawnWatcher = notify.SpawnWatcher

type Model struct {
	cfg     store.Config
	active  *store.Active
	next    store.Kind // what enter starts while idle
	cycle   int        // completed focus sessions in the current cycle
	label   string
	summary stats.Summary

	view      view
	scroll    int
	help      bool
	editing   bool
	input     textinput.Model
	flash     string
	flashTill time.Time
	err       error
	blink     bool

	width, height int
	detached      bool
	autoClose     bool
}

// Options configure the TUI at launch.
type Options struct {
	StartKind store.Kind // empty: don't auto-start
	Duration  time.Duration
	Label     string
	Stats     bool
	AutoClose bool // quit when the timer is stopped (for auto-opened panes)
}

func NewModel(opts Options) Model {
	ti := textinput.New()
	ti.Placeholder = "what are you working on?"
	ti.CharLimit = 60
	ti.Prompt = "› "
	ti.PromptStyle = lipgloss.NewStyle().Foreground(Key)
	ti.TextStyle = textStyle

	m := Model{cfg: store.LoadConfig(), next: store.Focus, label: opts.Label, input: ti, cycle: store.CycleNow()}
	if opts.Stats {
		m.view = viewStats
	}
	m.autoClose = opts.AutoClose
	m.refreshSummary()

	if a, _ := store.Peek(); a != nil {
		m.adopt(a)
	} else if opts.StartKind != "" {
		d := opts.Duration
		if d <= 0 {
			d = m.cfg.Duration(opts.StartKind)
		}
		m.start(opts.StartKind, d)
	}
	return m
}

// Detached reports whether the user left the TUI with a session still running.
func (m Model) Detached() bool { return m.detached }

func tick() tea.Cmd {
	return tea.Tick(250*time.Millisecond, func(t time.Time) tea.Msg { return tickMsg(t) })
}

func (m Model) Init() tea.Cmd { return tea.Batch(tick(), tea.SetWindowTitle("pomo")) }

func (m *Model) refreshSummary() {
	recs, _ := store.History()
	m.summary = stats.Compute(recs, time.Now(), 54*7, m.cfg.DailyGoal)
}

func (m *Model) say(s string) {
	m.flash = s
	m.flashTill = time.Now().Add(4 * time.Second)
}

func (m *Model) adopt(a *store.Active) {
	m.active = a
	m.cycle = a.Cycle
	if a.Label != "" && a.Kind == store.Focus {
		m.label = a.Label
	}
}

func (m *Model) start(k store.Kind, d time.Duration) {
	label := ""
	if k == store.Focus {
		label = m.label
	}
	a, err := store.Start(k, d, label, "tui", m.cycle, false)
	if err != nil {
		m.err = err
		return
	}
	m.active = a
	m.err = nil
	_ = spawnWatcher(a.ID)
}

// advance moves to the next phase after a session ends.
func (m *Model) advance(rec *store.Record) {
	m.active = nil
	m.refreshSummary()
	next, cycle := store.Next(rec, rec.Cycle, m.cfg)
	m.cycle = cycle
	m.next = next
	if rec.Completed {
		if rec.Kind == store.Focus {
			m.say(fmt.Sprintf("🍅 Pomodoro done! %d today. Time for a %s.", m.summary.Today.Pomodoros, strings.ToLower(next.Title())))
		} else {
			m.say("☕ Break's over — press enter to focus.")
		}
	}
	if (rec.Kind == store.Focus && m.cfg.AutoStartBreak) || (rec.Kind != store.Focus && m.cfg.AutoStartFocus) {
		m.start(next, m.cfg.Duration(next))
	}
}

// sync reconciles the model with the on-disk state, which other processes
// (agents, `pomo pause`, the background watcher) may have changed.
func (m *Model) sync() tea.Cmd {
	disk, err := store.Peek()
	if err != nil {
		m.err = err
		return nil
	}
	if m.active == nil {
		if disk != nil {
			m.adopt(disk)
			m.say(IconFor(disk.Kind) + " Session started from another process")
		}
		return nil
	}
	id := m.active.ID
	if disk != nil && disk.ID == id {
		m.active = disk
		if disk.Done(time.Now()) {
			if rec, _ := store.Finish(id); rec != nil {
				notify.Announce(rec, m.cfg)
				m.advance(rec)
				return bell
			}
		}
		return nil
	}
	// Our session ended elsewhere.
	rec, _ := store.FindRecord(id)
	switch {
	case rec != nil && rec.Completed && disk == nil:
		m.advance(rec)
		return bell
	case disk != nil:
		// e.g. the background watcher finished our session and auto-started the next.
		m.refreshSummary()
		m.adopt(disk)
		if rec != nil && rec.Completed {
			m.say(IconFor(disk.Kind) + " " + disk.Kind.Title() + " started")
			return bell
		}
	default:
		m.active = nil
		m.say("■ Session stopped")
		if m.autoClose {
			return tea.Quit
		}
	}
	return nil
}

func bell() tea.Msg {
	fmt.Print("\a")
	return nil
}

func (m Model) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	switch msg := msg.(type) {
	case tea.WindowSizeMsg:
		m.width, m.height = msg.Width, msg.Height
		return m, nil

	case tickMsg:
		m.blink = time.Time(msg).UnixMilli()/600%2 == 0
		if !m.flashTill.IsZero() && time.Now().After(m.flashTill) {
			m.flash = ""
		}
		return m, tea.Batch(m.sync(), tick())

	case tea.KeyMsg:
		if m.editing {
			return m.updateEditing(msg)
		}
		return m.updateKeys(msg)
	}
	return m, nil
}

func (m Model) updateEditing(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	switch msg.String() {
	case "enter":
		m.label = strings.TrimSpace(m.input.Value())
		m.editing = false
		m.input.Blur()
		if m.active != nil && m.active.Kind == store.Focus {
			if a, err := store.SetLabel(m.label); err == nil {
				m.active = a
			}
		}
		return m, nil
	case "esc", "ctrl+c":
		m.editing = false
		m.input.Blur()
		return m, nil
	}
	var cmd tea.Cmd
	m.input, cmd = m.input.Update(msg)
	return m, cmd
}

func (m Model) updateKeys(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	key := msg.String()
	switch key {
	case "q", "ctrl+c":
		m.detached = m.active != nil
		return m, tea.Quit
	case "?":
		m.help = !m.help
		return m, nil
	case "t", "tab":
		if m.view == viewStats {
			m.view = viewTimer
		} else {
			m.refreshSummary()
			m.view, m.scroll = viewStats, 0
		}
		return m, nil
	case "esc":
		m.view, m.help = viewTimer, false
		return m, nil
	case "up", "k", "down", "j", "pgup", "pgdown", "home", "end":
		if m.view == viewStats {
			switch key {
			case "up", "k":
				m.scroll--
			case "down", "j":
				m.scroll++
			case "pgup":
				m.scroll -= m.height / 2
			case "pgdown":
				m.scroll += m.height / 2
			case "home":
				m.scroll = 0
			case "end":
				m.scroll = m.maxScroll()
			}
			m.scroll = max(0, min(m.scroll, m.maxScroll()))
			return m, nil
		}
	case "c":
		names := ThemeNames()
		i := 0
		for j, n := range names {
			if n == m.cfg.Theme {
				i = j
			}
		}
		m.cfg.Theme = names[(i+1)%len(names)]
		t := ApplyTheme(m.cfg.Theme)
		m.input.PromptStyle = lipgloss.NewStyle().Foreground(Key)
		m.input.TextStyle = textStyle
		saved := store.LoadConfig()
		saved.Theme = m.cfg.Theme
		_ = store.SaveConfig(saved)
		label := m.cfg.Theme
		if label == "catppuccin" {
			label += " (" + t.Name + ")"
		}
		m.say("🎨 theme: " + label)
		return m, nil
	case "l":
		m.editing = true
		m.input.SetValue(m.label)
		m.input.CursorEnd()
		return m, m.input.Focus()
	}

	if m.active == nil {
		switch key {
		case "enter", " ", "s":
			m.start(m.next, m.cfg.Duration(m.next))
		case "f":
			m.start(store.Focus, m.cfg.Duration(store.Focus))
		case "b":
			m.start(store.ShortBreak, m.cfg.Duration(store.ShortBreak))
		case "B":
			m.start(store.LongBreak, m.cfg.Duration(store.LongBreak))
		}
		if m.active != nil {
			m.view = viewTimer
		}
		return m, nil
	}

	switch key {
	case " ", "p":
		if a, err := store.TogglePause(); err == nil {
			m.active = a
		}
	case "+", "=":
		if a, err := store.Extend(time.Minute); err == nil {
			m.active = a
			m.say("+1 minute")
		}
	case "-", "_":
		if a, err := store.Extend(-time.Minute); err == nil {
			m.active = a
			m.say("−1 minute")
		}
	case "s":
		if rec, err := store.Stop(m.active.ID); err == nil {
			m.say("⏭ Skipped " + strings.ToLower(rec.Kind.Title()))
			m.advance(rec)
		}
	case "x":
		if rec, err := store.Stop(m.active.ID); err == nil {
			m.active = nil
			m.next = rec.Kind
			m.refreshSummary()
			m.say(fmt.Sprintf("■ Stopped after %s", FmtDur(rec.Actual.D())))
			if m.autoClose {
				return m, tea.Quit
			}
		}
	}
	return m, nil
}

// ---------------------------------------------------------------------------
// View

func (m Model) View() string {
	if m.width == 0 {
		return ""
	}
	var body string
	if m.view == viewStats {
		return m.statsScreen()
	} else {
		body = m.timerView()
	}
	if m.help {
		body = m.helpView()
	}
	return lipgloss.Place(m.width, m.height, lipgloss.Center, lipgloss.Center, body)
}

// statsLines renders the dashboard as lines; it may be taller than the screen.
func (m Model) statsLines() []string {
	return strings.Split(StatsView(m.summary, m.width-4, time.Now()), "\n")
}

func (m Model) maxScroll() int {
	n := len(m.statsLines()) - (m.height - 2)
	if n < 0 {
		return 0
	}
	return n
}

func (m Model) statsScreen() string {
	lines := m.statsLines()
	h := m.height - 2 // leave room for the hint bar
	hints := [][2]string{{"t", "timer"}, {"q", "quit"}}
	if len(lines) > h {
		off := min(m.scroll, len(lines)-h)
		lines = lines[off : off+h]
		hints = append([][2]string{{"↑/↓", "scroll"}}, hints...)
	}
	body := lipgloss.JoinVertical(lipgloss.Left, strings.Join(lines, "\n"), "", m.keyHints(hints))
	return lipgloss.Place(m.width, m.height, lipgloss.Center, lipgloss.Center, body)
}

func (m Model) keyHints(keys [][2]string) string {
	var parts []string
	k := lipgloss.NewStyle().Foreground(Key).Bold(true)
	for _, kv := range keys {
		parts = append(parts, k.Render(kv[0])+" "+mutedStyle.Render(kv[1]))
	}
	return strings.Join(parts, subtleStyle.Render("  ·  "))
}

func (m Model) timerView() string {
	now := time.Now()
	kind := m.next
	remaining := m.cfg.Duration(kind)
	progress := 0.0
	paused, idle := false, m.active == nil
	label := m.label
	if !idle {
		kind = m.active.Kind
		remaining = m.active.Remaining(now)
		progress = m.active.Progress(now)
		paused = m.active.Paused()
		label = m.active.Label
	}
	stops := StopsFor(kind)

	// Header: phase pill + label.
	pill := Pill(IconFor(kind)+" "+strings.ToUpper(kind.Title()), AccentFor(kind))
	switch {
	case paused:
		pill = Pill("⏸ PAUSED", Muted)
	case idle:
		pill = Pill("● READY", Muted)
	}
	header := Logo() + "   " + pill
	if label != "" && kind == store.Focus {
		header += "  " + textStyle.Italic(true).Render(label)
	}

	// Clock.
	text := FmtClock(remaining)
	scale := 1
	if m.width >= ClockWidth(text, 2)+8 && m.height >= ClockHeight(2)+16 {
		scale = 2
	}
	clock := BigClock(text, scale, stops, paused)
	cw := ClockWidth(text, scale)

	// Progress.
	barW := cw
	pct := lipgloss.NewStyle().Foreground(AccentFor(kind)).Bold(true).Render(fmt.Sprintf(" %3.0f%%", progress*100))
	// Pad the left by the percentage width so the bar stays aligned with the clock.
	bar := strings.Repeat(" ", lipgloss.Width(pct)) + Bar(progress, barW, stops, paused) + pct

	// Cycle dots + end time.
	var dots strings.Builder
	for i := 0; i < m.cfg.LongBreakEvery; i++ {
		if i < m.cycle%m.cfg.LongBreakEvery || (m.cycle > 0 && m.cycle%m.cfg.LongBreakEvery == 0 && kind == store.LongBreak) {
			dots.WriteString(lipgloss.NewStyle().Foreground(HeatLevels[i%len(HeatLevels)]).Render("●") + " ")
		} else if i == m.cycle%m.cfg.LongBreakEvery && kind == store.Focus && !idle {
			dots.WriteString(lipgloss.NewStyle().Foreground(Highlight).Render("◉") + " ")
		} else {
			dots.WriteString(lipgloss.NewStyle().Foreground(Subtle).Render("○") + " ")
		}
	}
	meta := dots.String() + subtleStyle.Render("·  ")
	switch {
	case idle:
		meta += mutedStyle.Render(fmt.Sprintf("press enter to start %s", strings.ToLower(kind.Title())))
	case paused:
		if m.blink {
			meta += mutedStyle.Render("paused — space to resume")
		} else {
			meta += subtleStyle.Render("paused — space to resume")
		}
	default:
		meta += mutedStyle.Render("ends at ") + boldStyle.Render(m.active.EndsAt(now).Format("15:04"))
	}

	// Today line.
	s := m.summary
	goal := ""
	if s.Goal > 0 {
		goal = fmt.Sprintf("/%d", s.Goal)
	}
	today := fmt.Sprintf("%s %s   %s %s   %s %s",
		subtleStyle.Render("today"), boldStyle.Render(fmt.Sprintf("%d%s 🍅", s.Today.Pomodoros, goal)),
		subtleStyle.Render("focused"), boldStyle.Render(FmtMins(s.Today.FocusMins)),
		subtleStyle.Render("streak"), boldStyle.Render(fmt.Sprintf("%dd 🔥", s.CurrentStreak)))

	// Mini heatmap strip: last 14 days.
	var strip strings.Builder
	days := s.Days
	if len(days) > 14 {
		days = days[len(days)-14:]
	}
	max := 0
	for _, d := range s.Days {
		if d.Pomodoros > max {
			max = d.Pomodoros
		}
	}
	for _, d := range days {
		strip.WriteString(heatCell(heatLevel(d.Pomodoros, max)) + " ")
	}

	// Footer.
	var footer string
	switch {
	case m.editing:
		footer = m.input.View()
	case idle:
		footer = m.keyHints([][2]string{{"enter", "start"}, {"f", "focus"}, {"b", "break"}, {"l", "label"}, {"t", "stats"}, {"c", "theme"}, {"q", "quit"}})
	default:
		footer = m.keyHints([][2]string{{"space", "pause"}, {"s", "skip"}, {"x", "stop"}, {"+/-", "1 min"}, {"t", "stats"}, {"q", "detach"}})
	}

	flash := " "
	if m.flash != "" {
		flash = lipgloss.NewStyle().Foreground(Highlight).Bold(true).Render(m.flash)
	}
	if m.err != nil {
		flash = lipgloss.NewStyle().Foreground(Danger).Render("error: " + m.err.Error())
	}

	return lipgloss.JoinVertical(lipgloss.Center,
		header, "", "",
		clock, "",
		bar,
		meta, "", "",
		today,
		strip.String()+subtleStyle.Render(" last 14 days"), "",
		flash, "",
		footer,
	)
}

func (m Model) helpView() string {
	rows := [][2]string{
		{"enter", "start next phase (when idle)"},
		{"f / b / B", "start focus / short break / long break"},
		{"space · p", "pause / resume"},
		{"s", "skip to next phase"},
		{"x", "stop the current session"},
		{"+ / -", "add / remove one minute"},
		{"l", "set label for focus sessions"},
		{"t · tab", "toggle statistics"},
		{"c", "cycle colour theme"},
		{"q", "quit (a running timer keeps going in the background)"},
		{"?", "toggle this help"},
	}
	k := lipgloss.NewStyle().Foreground(Key).Bold(true).Width(12)
	var lines []string
	for _, r := range rows {
		lines = append(lines, k.Render(r[0])+textStyle.Render(r[1]))
	}
	return panel("Keyboard shortcuts", strings.Join(lines, "\n"), 66)
}

// Run launches the full-screen TUI.
func Run(opts Options) (Model, error) {
	defer store.RegisterViewer()()
	p := tea.NewProgram(NewModel(opts), tea.WithAltScreen())
	final, err := p.Run()
	if err != nil {
		return Model{}, err
	}
	return final.(Model), nil
}

package ui

import (
	"strings"
	"time"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"

	"github.com/antrvan746/pomodoro-cli/internal/breath"
)

// BreatheOptions configure the breathing animation.
type BreatheOptions struct {
	Exercise string
	Style    string
	// Done, when set, is polled every frame; the animation quits once it
	// reports true (the agent finished its turn).
	Done func() bool
}

type breatheTick time.Time

type breatheModel struct {
	opts          BreatheOptions
	ex            breath.Exercise
	style         string
	start         time.Time
	now           time.Time
	width, height int
}

const breatheFrame = 100 * time.Millisecond

func breatheTickCmd() tea.Cmd {
	return tea.Tick(breatheFrame, func(t time.Time) tea.Msg { return breatheTick(t) })
}

func (m breatheModel) Init() tea.Cmd {
	return tea.Batch(breatheTickCmd(), tea.SetWindowTitle("pomo · breathe"))
}

func (m breatheModel) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	switch msg := msg.(type) {
	case tea.WindowSizeMsg:
		m.width, m.height = msg.Width, msg.Height
	case tea.KeyMsg:
		switch msg.String() {
		case "q", "esc", "ctrl+c":
			return m, tea.Quit
		case "n", "tab": // next exercise
			for i, e := range breath.Exercises {
				if e.Key == m.ex.Key {
					m.ex = breath.Exercises[(i+1)%len(breath.Exercises)]
					break
				}
			}
			m.start = m.now
		case "s": // next style
			m.style = breath.PickStyle("random", m.style)
		}
	case breatheTick:
		m.now = time.Time(msg)
		if m.opts.Done != nil && m.opts.Done() {
			return m, tea.Quit
		}
		return m, breatheTickCmd()
	}
	return m, nil
}

func (m breatheModel) View() string {
	if m.width == 0 {
		return ""
	}
	phase := m.ex.At(m.now.Sub(m.start))
	// a short pane keeps the phase line and drops the rest
	art := breath.Frame(m.style, phase.Progress, m.width, min(breath.ArtRows, max(0, m.height-3)))
	stops := Gradient(cur.Short, max(1, len(art)))
	lines := make([]string, 0, len(art)+3)
	for i, l := range art {
		lines = append(lines, lipgloss.NewStyle().Foreground(stops[i]).Render(l))
	}
	centre := func(s string) string { return lipgloss.PlaceHorizontal(m.width, lipgloss.Center, s) }
	lines = append(lines, "", centre(boldStyle.Render(phase.Line())))
	if m.height >= len(art)+4 {
		lines = append(lines, centre(subtleStyle.Render(m.ex.Name+" · q close · n next exercise")))
	}
	return lipgloss.Place(m.width, m.height, lipgloss.Left, lipgloss.Center, strings.Join(lines, "\n"))
}

// RunBreathe shows the breathing animation full screen until q, or until
// opts.Done reports true.
func RunBreathe(opts BreatheOptions) error {
	now := time.Now()
	m := breatheModel{opts: opts, ex: breath.ExerciseOf(opts.Exercise),
		style: breath.PickStyle(opts.Style, ""), start: now, now: now}
	_, err := tea.NewProgram(m, tea.WithAltScreen()).Run()
	return err
}

package ui

import (
	"fmt"
	"sort"
	"strings"
	"time"

	"github.com/charmbracelet/lipgloss"
	"github.com/lucasb-eyer/go-colorful"

	"github.com/antrvan746/pomodoro-cli/internal/store"
)

// Theme defines every colour role used by the UI. Themes are explicit rather
// than adaptive so text contrast is designed together with the accents.
type Theme struct {
	Name, Desc string

	Text   string // primary text, numbers
	Muted  string // secondary text, key-hint descriptions
	Subtle string // separators, axis labels
	Track  string // empty progress, paused clock base
	Border string // card borders
	Empty  string // heatmap cell with no activity

	Focus, Short, Long []string // clock / bar gradients per phase
	Heat               [4]string
	Logo               []string

	FocusAccent, ShortAccent, LongAccent string
	Highlight, Key, Danger               string
	PillText                             string
}

var themes = map[string]Theme{
	"mocha": {
		Name: "mocha", Desc: "Catppuccin Mocha — soothing pastels on dark",
		Text: "#cdd6f4", Muted: "#bac2de", Subtle: "#9399b2", Track: "#45475a", Border: "#585b70", Empty: "#313244",
		Focus:       []string{"#f9e2af", "#fab387", "#eba0ac", "#f38ba8"},
		Short:       []string{"#f5c2e7", "#cba6f7"},
		Long:        []string{"#cba6f7", "#b4befe"},
		Heat:        [4]string{"#f9e2af", "#f5c2e7", "#f38ba8", "#cba6f7"},
		Logo:        []string{"#f9e2af", "#fab387", "#f38ba8", "#cba6f7"},
		FocusAccent: "#f38ba8", ShortAccent: "#f5c2e7", LongAccent: "#cba6f7",
		Highlight: "#f9e2af", Key: "#f5c2e7", Danger: "#f38ba8", PillText: "#1e1e2e",
	},
	"latte": {
		Name: "latte", Desc: "Catppuccin Latte — for light terminals",
		Text: "#4c4f69", Muted: "#5c5f77", Subtle: "#7c7f93", Track: "#bcc0cc", Border: "#acb0be", Empty: "#ccd0da",
		Focus:       []string{"#df8e1d", "#fe640b", "#e64553", "#d20f39"},
		Short:       []string{"#ea76cb", "#8839ef"},
		Long:        []string{"#8839ef", "#7287fd"},
		Heat:        [4]string{"#df8e1d", "#ea76cb", "#d20f39", "#8839ef"},
		Logo:        []string{"#df8e1d", "#fe640b", "#d20f39", "#8839ef"},
		FocusAccent: "#d20f39", ShortAccent: "#ea76cb", LongAccent: "#8839ef",
		Highlight: "#df8e1d", Key: "#8839ef", Danger: "#d20f39", PillText: "#eff1f5",
	},
	"ember": {
		Name: "ember", Desc: "Glowing coals — peach, coral and brick red",
		Text: "#fbe6d8", Muted: "#e3c2b3", Subtle: "#b8908a", Track: "#4d3335", Border: "#6a4446", Empty: "#3b2829",
		Focus:       []string{"#faab75", "#e77e5d", "#d45a52", "#b8464a"},
		Short:       []string{"#faab75", "#e77e5d"},
		Long:        []string{"#e77e5d", "#d45a52", "#973b3f"},
		Heat:        [4]string{"#973b3f", "#d45a52", "#e77e5d", "#faab75"},
		Logo:        []string{"#faab75", "#e77e5d", "#d45a52"},
		FocusAccent: "#e77e5d", ShortAccent: "#faab75", LongAccent: "#d45a52",
		Highlight: "#faab75", Key: "#e77e5d", Danger: "#d45a52", PillText: "#2a1718",
	},
	"sand": {
		Name: "sand", Desc: "Desert dusk — cream, mustard and olive",
		Text: "#fbebd8", Muted: "#e2d3bd", Subtle: "#b3a48f", Track: "#4f4440", Border: "#6b5d55", Empty: "#463b37",
		Focus:       []string{"#fbebd8", "#f0b86c", "#a9a071"},
		Short:       []string{"#a9a071", "#f0b86c"},
		Long:        []string{"#f0b86c", "#a9a071", "#7f7a52"},
		Heat:        [4]string{"#a9a071", "#cdae6e", "#f0b86c", "#fbebd8"},
		Logo:        []string{"#fbebd8", "#f0b86c", "#a9a071"},
		FocusAccent: "#f0b86c", ShortAccent: "#a9a071", LongAccent: "#cdae6e",
		Highlight: "#f0b86c", Key: "#f0b86c", Danger: "#e07a5f", PillText: "#2e2623",
	},
}

// ThemeNames lists selectable names; "catppuccin" picks mocha or latte from
// the terminal background.
func ThemeNames() []string {
	names := []string{"catppuccin"}
	var rest []string
	for n := range themes {
		rest = append(rest, n)
	}
	sort.Strings(rest)
	return append(names, rest...)
}

func resolveTheme(name string) (Theme, bool) {
	if name == "" || name == "catppuccin" || name == "auto" {
		if lipgloss.HasDarkBackground() {
			return themes["mocha"], true
		}
		return themes["latte"], true
	}
	t, ok := themes[strings.ToLower(name)]
	return t, ok
}

// Current colour roles. They are reassigned by ApplyTheme.
var (
	cur Theme

	Text, Muted, Subtle, Track, Border, Empty lipgloss.Color
	Highlight, Key, Danger                    lipgloss.Color
	HeatLevels                                []lipgloss.Color
	LogoStops                                 []string

	textStyle, mutedStyle, subtleStyle, boldStyle lipgloss.Style
)

func init() { ApplyTheme("mocha") }

// ApplyTheme switches the active theme. Unknown names fall back to catppuccin.
func ApplyTheme(name string) Theme {
	t, ok := resolveTheme(name)
	if !ok {
		t, _ = resolveTheme("catppuccin")
	}
	cur = t
	c := func(s string) lipgloss.Color { return lipgloss.Color(s) }
	Text, Muted, Subtle, Track, Border, Empty = c(t.Text), c(t.Muted), c(t.Subtle), c(t.Track), c(t.Border), c(t.Empty)
	Highlight, Key, Danger = c(t.Highlight), c(t.Key), c(t.Danger)
	HeatLevels = []lipgloss.Color{c(t.Heat[0]), c(t.Heat[1]), c(t.Heat[2]), c(t.Heat[3])}
	LogoStops = t.Logo
	textStyle = lipgloss.NewStyle().Foreground(Text)
	mutedStyle = lipgloss.NewStyle().Foreground(Muted)
	subtleStyle = lipgloss.NewStyle().Foreground(Subtle)
	boldStyle = lipgloss.NewStyle().Foreground(Text).Bold(true)
	return t
}

func CurrentTheme() Theme { return cur }

func StopsFor(k store.Kind) []string {
	switch k {
	case store.ShortBreak:
		return cur.Short
	case store.LongBreak:
		return cur.Long
	default:
		return cur.Focus
	}
}

func AccentFor(k store.Kind) lipgloss.Color {
	switch k {
	case store.ShortBreak:
		return lipgloss.Color(cur.ShortAccent)
	case store.LongBreak:
		return lipgloss.Color(cur.LongAccent)
	default:
		return lipgloss.Color(cur.FocusAccent)
	}
}

func IconFor(k store.Kind) string {
	switch k {
	case store.ShortBreak:
		return "☕"
	case store.LongBreak:
		return "🌙"
	default:
		return "🍅"
	}
}

func hexes(stops []string) []colorful.Color {
	cs := make([]colorful.Color, len(stops))
	for i, s := range stops {
		cs[i], _ = colorful.Hex(s)
	}
	return cs
}

// Gradient returns n colours evenly spread across the stops.
func Gradient(stops []string, n int) []lipgloss.Color {
	if n <= 0 {
		return nil
	}
	cs := hexes(stops)
	out := make([]lipgloss.Color, n)
	for i := 0; i < n; i++ {
		if len(cs) == 1 || n == 1 {
			out[i] = lipgloss.Color(stops[0])
			continue
		}
		t := float64(i) / float64(n-1) * float64(len(cs)-1)
		seg := int(t)
		if seg >= len(cs)-1 {
			seg = len(cs) - 2
		}
		out[i] = lipgloss.Color(cs[seg].BlendLab(cs[seg+1], t-float64(seg)).Clamped().Hex())
	}
	return out
}

// Fade blends every stop towards the track colour; used for the paused clock
// so it keeps a hint of colour instead of going flat grey.
func Fade(stops []string, amount float64) []string {
	track, _ := colorful.Hex(cur.Track)
	out := make([]string, len(stops))
	for i, c := range hexes(stops) {
		out[i] = c.BlendLab(track, amount).Clamped().Hex()
	}
	return out
}

// GradientText colours each rune of s along the gradient.
func GradientText(s string, stops []string, bold bool) string {
	rs := []rune(s)
	cols := Gradient(stops, len(rs))
	var b strings.Builder
	for i, r := range rs {
		b.WriteString(lipgloss.NewStyle().Foreground(cols[i]).Bold(bold).Render(string(r)))
	}
	return b.String()
}

// Bar renders a gradient progress bar of the given width.
func Bar(p float64, width int, stops []string, dim bool) string {
	if width < 1 {
		return ""
	}
	filled := int(p*float64(width) + 0.5)
	if filled > width {
		filled = width
	}
	if dim {
		stops = Fade(stops, 0.5)
	}
	cols := Gradient(stops, width)
	var b strings.Builder
	for i := 0; i < width; i++ {
		var c lipgloss.TerminalColor = Track
		if i < filled {
			c = cols[i]
		}
		b.WriteString(lipgloss.NewStyle().Foreground(c).Render("━"))
	}
	return b.String()
}

func Pill(label string, bg lipgloss.TerminalColor) string {
	return lipgloss.NewStyle().Background(bg).Foreground(lipgloss.Color(cur.PillText)).
		Bold(true).Padding(0, 1).Render(label)
}

func Logo() string { return GradientText("pomo", LogoStops, true) }

// Swatch renders a row of colour blocks, used by `pomo theme`.
func Swatch(t Theme) string {
	var b strings.Builder
	for _, c := range Gradient(t.Focus, 12) {
		b.WriteString(lipgloss.NewStyle().Foreground(c).Render("█"))
	}
	b.WriteString("  ")
	b.WriteString(lipgloss.NewStyle().Foreground(lipgloss.Color(t.Empty)).Render("■ "))
	for _, h := range t.Heat {
		b.WriteString(lipgloss.NewStyle().Foreground(lipgloss.Color(h)).Render("■ "))
	}
	b.WriteString(" ")
	b.WriteString(lipgloss.NewStyle().Foreground(lipgloss.Color(t.Text)).Render("Text "))
	b.WriteString(lipgloss.NewStyle().Foreground(lipgloss.Color(t.Muted)).Render("muted "))
	b.WriteString(lipgloss.NewStyle().Foreground(lipgloss.Color(t.Subtle)).Render("subtle"))
	return b.String()
}

// FmtDur renders a duration compactly: 45m, 2h05m.
func FmtDur(d time.Duration) string {
	m := int(d.Round(time.Minute).Minutes())
	if m < 60 {
		return fmt.Sprintf("%dm", m)
	}
	return fmt.Sprintf("%dh%02dm", m/60, m%60)
}

func FmtMins(m int) string { return FmtDur(time.Duration(m) * time.Minute) }

// FmtClock renders a countdown: 24:59 or 1:05:00.
func FmtClock(d time.Duration) string {
	s := int((d + time.Second - 1) / time.Second) // round up so 0:00 means done
	if s >= 3600 {
		return fmt.Sprintf("%d:%02d:%02d", s/3600, (s/60)%60, s%60)
	}
	return fmt.Sprintf("%02d:%02d", s/60, s%60)
}

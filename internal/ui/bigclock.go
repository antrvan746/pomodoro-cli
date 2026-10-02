package ui

import (
	"strings"

	"github.com/charmbracelet/lipgloss"
)

// 3×5 bitmap font; each "pixel" becomes a block of terminal cells.
var glyphs = map[rune][]string{
	'0': {"###", "#.#", "#.#", "#.#", "###"},
	'1': {"##.", ".#.", ".#.", ".#.", "###"},
	'2': {"###", "..#", "###", "#..", "###"},
	'3': {"###", "..#", "###", "..#", "###"},
	'4': {"#.#", "#.#", "###", "..#", "..#"},
	'5': {"###", "#..", "###", "..#", "###"},
	'6': {"###", "#..", "###", "#.#", "###"},
	'7': {"###", "..#", "..#", "..#", "..#"},
	'8': {"###", "#.#", "###", "#.#", "###"},
	'9': {"###", "#.#", "###", "..#", "###"},
	':': {".", "#", ".", "#", "."},
}

// ClockWidth is the rendered width in cells of text at the given scale.
func ClockWidth(text string, scale int) int {
	w := 0
	for i, r := range text {
		if i > 0 {
			w += scale // gap
		}
		w += len(glyphs[r][0]) * 2 * scale
	}
	return w
}

func ClockHeight(scale int) int { return 5 * scale }

// BigClock renders text in the block font with a horizontal gradient. When
// dim is true the clock is drawn in a muted colour (used while paused).
func BigClock(text string, scale int, stops []string, dim bool) string {
	if scale < 1 {
		scale = 1
	}
	width := ClockWidth(text, scale)
	if dim {
		stops = Fade(stops, 0.6)
	}
	cols := Gradient(stops, width)
	cell := strings.Repeat("█", 2*scale)
	gap := strings.Repeat(" ", scale)

	var lines []string
	for row := 0; row < 5; row++ {
		var b strings.Builder
		x := 0
		for i, r := range text {
			if i > 0 {
				b.WriteString(gap)
				x += scale
			}
			for _, px := range glyphs[r][row] {
				if px == '#' {
					b.WriteString(lipgloss.NewStyle().Foreground(cols[x]).Render(cell))
				} else {
					b.WriteString(strings.Repeat(" ", 2*scale))
				}
				x += 2 * scale
			}
		}
		line := b.String()
		for i := 0; i < scale; i++ {
			lines = append(lines, line)
		}
	}
	return strings.Join(lines, "\n")
}

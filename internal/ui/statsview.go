package ui

import (
	"fmt"
	"strings"
	"time"

	"github.com/charmbracelet/lipgloss"

	"github.com/antrvan746/pomodoro-cli/internal/stats"
)

// HeatWeeks picks how many weeks of heatmap fit in the given width.
func HeatWeeks(width int) int {
	// 4 cells of weekday labels + 2 per week + card padding/border (6).
	w := (width - 4 - 6) / 2
	if w > 53 {
		w = 53
	}
	if w < 8 {
		w = 8
	}
	return w
}

func heatLevel(n, max int) int {
	if n <= 0 {
		return 0
	}
	if max < 4 {
		max = 4
	}
	l := (n*4 + max - 1) / max
	if l > 4 {
		l = 4
	}
	if l < 1 {
		l = 1
	}
	return l
}

func heatCell(level int) string {
	if level == 0 {
		return lipgloss.NewStyle().Foreground(Empty).Render("■")
	}
	return lipgloss.NewStyle().Foreground(HeatLevels[level-1]).Render("■")
}

// Heatmap renders a GitHub-style contribution grid of completed pomodoros.
func Heatmap(s stats.Summary, weeks int, now time.Time) string {
	byKey := map[string]stats.Day{}
	max := 0
	for _, d := range s.Days {
		byKey[d.Key] = d
		if d.Pomodoros > max {
			max = d.Pomodoros
		}
	}
	today := time.Date(now.Year(), now.Month(), now.Day(), 0, 0, 0, 0, now.Location())
	// First column starts on the Sunday (weeks-1) weeks before this week.
	start := today.AddDate(0, 0, -int(today.Weekday())-(weeks-1)*7)

	// Month labels.
	label := make([]rune, 4+weeks*2+3)
	for i := range label {
		label[i] = ' '
	}
	lastMonth := time.Month(0)
	nextFree := 0
	for w := 0; w < weeks; w++ {
		d := start.AddDate(0, 0, w*7)
		// Label a month on the first column that contains its 1st..7th day.
		if d.Month() != lastMonth && (w > 0 || d.Day() <= 7) {
			pos := 4 + w*2
			if pos >= nextFree && pos+3 <= len(label) {
				copy(label[pos:], []rune(d.Format("Jan")))
				nextFree = pos + 4
			}
		}
		if d.Month() != lastMonth {
			lastMonth = d.Month()
		}
	}
	var rows []string
	rows = append(rows, subtleStyle.Render(strings.TrimRight(string(label), " ")))

	dayNames := []string{"", "Mon", "", "Wed", "", "Fri", ""}
	for wd := 0; wd < 7; wd++ {
		var b strings.Builder
		b.WriteString(subtleStyle.Render(fmt.Sprintf("%-3s ", dayNames[wd])))
		for w := 0; w < weeks; w++ {
			d := start.AddDate(0, 0, w*7+wd)
			if d.After(today) {
				b.WriteString("  ")
				continue
			}
			b.WriteString(heatCell(heatLevel(byKey[stats.Key(d)].Pomodoros, max)))
			b.WriteString(" ")
		}
		rows = append(rows, strings.TrimRight(b.String(), " "))
	}

	var legend strings.Builder
	legend.WriteString(subtleStyle.Render("Less "))
	for l := 0; l <= 4; l++ {
		legend.WriteString(heatCell(l) + " ")
	}
	legend.WriteString(subtleStyle.Render("More"))
	rows = append(rows, "", legend.String())
	return strings.Join(rows, "\n")
}

func card(title, value, sub string, accent lipgloss.Color, width int) string {
	t := lipgloss.NewStyle().Foreground(accent).Bold(true).Render(strings.ToUpper(title))
	v := lipgloss.NewStyle().Foreground(Text).Bold(true).Render(value)
	body := lipgloss.JoinVertical(lipgloss.Left, t, v, mutedStyle.Render(sub))
	return lipgloss.NewStyle().Border(lipgloss.RoundedBorder()).BorderForeground(Border).
		Padding(0, 1).Width(width).Render(body)
}

func panel(title, body string, width int) string {
	t := GradientText(title, LogoStops, true)
	return lipgloss.NewStyle().Border(lipgloss.RoundedBorder()).BorderForeground(Border).
		Padding(0, 2).Width(width).Render(t + "\n\n" + body)
}

var sparkRunes = []rune("▁▂▃▄▅▆▇█")

func sparkline(vals []int, stops []string) string {
	max := 0
	for _, v := range vals {
		if v > max {
			max = v
		}
	}
	cols := Gradient(stops, len(vals))
	var b strings.Builder
	for i, v := range vals {
		if v == 0 {
			b.WriteString(lipgloss.NewStyle().Foreground(Track).Render("▁"))
			continue
		}
		idx := v * (len(sparkRunes) - 1) / max
		b.WriteString(lipgloss.NewStyle().Foreground(cols[i]).Render(string(sparkRunes[idx])))
	}
	return b.String()
}

// StatsView renders the full statistics dashboard at the given width.
func StatsView(s stats.Summary, width int, now time.Time) string {
	if width > 120 {
		width = 120
	}
	if width < 44 {
		width = 44
	}
	header := Logo() + subtleStyle.Render("  ·  statistics  ·  "+now.Format("Mon, 02 Jan 2006"))

	// Summary cards.
	cols := 4
	if width < 76 {
		cols = 2
	}
	cw := (width-cols)/cols - 2
	goal := ""
	if s.Goal > 0 {
		goal = fmt.Sprintf("/%d", s.Goal)
	}
	todaySub := FmtMins(s.Today.FocusMins) + " focused"
	if s.Goal > 0 {
		p := float64(s.Today.Pomodoros) / float64(s.Goal)
		if p > 1 {
			p = 1
		}
		todaySub = Bar(p, cw-2, cur.Focus, false)
	}
	streakSub := fmt.Sprintf("best %d days", s.BestStreak)
	ca := Gradient(LogoStops, 4)
	cards := []string{
		card("Today", fmt.Sprintf("%d%s 🍅", s.Today.Pomodoros, goal), todaySub, ca[0], cw),
		card("This week", fmt.Sprintf("%d 🍅", s.WeekPomodoros), FmtMins(s.WeekFocusMins)+" focused", ca[1], cw),
		card("Streak", fmt.Sprintf("%d days 🔥", s.CurrentStreak), streakSub, ca[2], cw),
		card("All time", fmt.Sprintf("%d 🍅", s.TotalPomodoro), FmtMins(s.TotalFocusMin)+" focused", ca[3], cw),
	}
	var cardRows []string
	for i := 0; i < len(cards); i += cols {
		end := i + cols
		if end > len(cards) {
			end = len(cards)
		}
		row := []string{}
		for j, c := range cards[i:end] {
			if j > 0 {
				row = append(row, " ")
			}
			row = append(row, c)
		}
		cardRows = append(cardRows, lipgloss.JoinHorizontal(lipgloss.Top, row...))
	}

	// Heatmap.
	weeks := HeatWeeks(width)
	yearTotal := 0
	for _, d := range s.Days {
		yearTotal += d.Pomodoros
	}
	heatTitle := fmt.Sprintf("%d pomodoros in the last %d weeks", yearTotal, weeks)
	heat := panel(heatTitle, Heatmap(s, weeks, now), width-2)

	// Last 7 days bar chart.
	last := s.Days
	if len(last) > 7 {
		last = last[len(last)-7:]
	}
	maxP := 1
	for _, d := range last {
		if d.Pomodoros > maxP {
			maxP = d.Pomodoros
		}
	}
	half := (width - 3) / 2
	if width < 76 {
		half = width - 2
	}
	barW := half - 22
	if barW < 6 {
		barW = 6
	}
	var weekLines []string
	for _, d := range last {
		p := float64(d.Pomodoros) / float64(maxP)
		name := mutedStyle.Render(d.Date.Format("Mon"))
		if stats.Key(d.Date) == stats.Key(now) {
			name = lipgloss.NewStyle().Foreground(Highlight).Bold(true).Render(d.Date.Format("Mon"))
		}
		bar := Bar(p, barW, cur.Focus, false)
		if d.Pomodoros == 0 {
			bar = lipgloss.NewStyle().Foreground(Track).Render(strings.Repeat("·", barW))
		}
		weekLines = append(weekLines, fmt.Sprintf("%s %s %s",
			name, bar, boldStyle.Render(fmt.Sprintf("%2d", d.Pomodoros))+
				subtleStyle.Render(fmt.Sprintf(" %6s", FmtMins(d.FocusMins)))))
	}
	weekPanel := panel("Last 7 days", strings.Join(weekLines, "\n"), half)

	// Top labels.
	var labelLines []string
	if len(s.Labels) == 0 {
		labelLines = append(labelLines, subtleStyle.Render("Use --label to tag your sessions"))
	}
	labelW := half - 18
	for i, l := range s.Labels {
		name := l.Name
		if lipgloss.Width(name) > labelW {
			name = string([]rune(name)[:labelW-1]) + "…"
		}
		dot := lipgloss.NewStyle().Foreground(HeatLevels[i%len(HeatLevels)]).Render("●")
		labelLines = append(labelLines, fmt.Sprintf("%s %s %s",
			dot, textStyle.Render(fmt.Sprintf("%-*s", labelW, name)),
			boldStyle.Render(fmt.Sprintf("%3d", l.Pomodoros))+subtleStyle.Render(fmt.Sprintf(" %6s", FmtMins(l.FocusMins)))))
	}
	for len(labelLines) < len(weekLines) {
		labelLines = append(labelLines, "")
	}
	labelPanel := panel("Top labels", strings.Join(labelLines, "\n"), half)

	var lower string
	if width < 76 {
		lower = lipgloss.JoinVertical(lipgloss.Left, weekPanel, labelPanel)
	} else {
		lower = lipgloss.JoinHorizontal(lipgloss.Top, weekPanel, " ", labelPanel)
	}

	// Rhythm: hour-of-day and weekday distribution.
	hours := sparkline(s.Hours[:], LogoStops)
	axis := subtleStyle.Render("0     6     12    18   23")
	order := []int{1, 2, 3, 4, 5, 6, 0}
	wdVals := make([]int, 7)
	for i, d := range order {
		wdVals[i] = s.Weekdays[d]
	}
	wd := sparkline(wdVals, LogoStops)
	peak, peakN := 0, 0
	for h, n := range s.Hours {
		if n > peakN {
			peak, peakN = h, n
		}
	}
	rhythm := fmt.Sprintf("%s  %s\n%s  %s\n\n%s  %s   %s  %s",
		mutedStyle.Render("by hour   "), hours,
		"          ", axis,
		mutedStyle.Render("by weekday"), wd+subtleStyle.Render("  mon → sun"),
		mutedStyle.Render("completion"), boldStyle.Render(fmt.Sprintf("%.0f%%", s.Completion*100)))
	if peakN > 0 {
		rhythm += subtleStyle.Render(fmt.Sprintf("   ·   peak hour %02d:00", peak))
	}
	rhythmPanel := panel("Rhythm", rhythm, width-2)

	parts := []string{header, ""}
	parts = append(parts, cardRows...)
	parts = append(parts, heat, lower, rhythmPanel)
	return lipgloss.JoinVertical(lipgloss.Left, parts...)
}

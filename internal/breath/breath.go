// Package breath holds the breathing exercises and the shapes that draw a
// breath, ported from Mindful-Claude (MIT, github.com/halluton/Mindful-Claude).
// Pure functions: no terminal, no clock.
package breath

import (
	"fmt"
	"math/rand"
	"strings"
	"time"
)

// Exercise is one breathing pattern.
type Exercise struct {
	Key, Name, Pattern           string
	Inhale, Hold1, Exhale, Hold2 time.Duration
	Sip                          bool // physiological sigh: the first hold is a second, smaller inhale
}

var Exercises = []Exercise{
	{Key: "hrv", Name: "Coherent Breathing", Pattern: "5.5s in, 5.5s out", Inhale: 5500 * time.Millisecond, Exhale: 5500 * time.Millisecond},
	{Key: "sigh", Name: "Physiological Sigh", Pattern: "double inhale, long exhale", Inhale: 4 * time.Second, Hold1: time.Second, Exhale: 10 * time.Second, Sip: true},
	{Key: "box", Name: "Box Breathing", Pattern: "4s in, 4s hold, 4s out, 4s hold", Inhale: 4 * time.Second, Hold1: 4 * time.Second, Exhale: 4 * time.Second, Hold2: 4 * time.Second},
	{Key: "478", Name: "4-7-8 Breathing", Pattern: "4s in, 7s hold, 8s out", Inhale: 4 * time.Second, Hold1: 7 * time.Second, Exhale: 8 * time.Second},
}

var aliases = map[string]string{
	"hrv": "hrv", "coherent": "hrv", "coherence": "hrv",
	"sigh": "sigh", "physiological": "sigh",
	"box": "box",
	"478": "478", "relax": "478",
}

// Resolve returns the exercise key a word names (hrv, coherent, sigh, box,
// 478, relax), or "".
func Resolve(word string) string { return aliases[strings.ToLower(strings.TrimSpace(word))] }

// ExerciseOf returns the exercise for key, or Coherent Breathing.
func ExerciseOf(key string) Exercise {
	for _, e := range Exercises {
		if e.Key == key {
			return e
		}
	}
	return Exercises[0]
}

func (e Exercise) Cycle() time.Duration { return e.Inhale + e.Hold1 + e.Exhale + e.Hold2 }

// Phase is where in a breath a moment lands.
type Phase struct {
	Label     string        // Breathe in, Sip in, Hold, Breathe out
	Remaining time.Duration // left in this phase
	Progress  int           // how full the lungs are, 0..1000
}

// ease is a quadratic ease-out over 0..1000.
func ease(x int) int { return x * (2000 - x) / 1000 }

func frac(t, d time.Duration) int { return int(t * 1000 / d) }

// At returns the phase elapsed since the exercise began.
func (e Exercise) At(elapsed time.Duration) Phase {
	cycle := e.Cycle()
	t := ((elapsed % cycle) + cycle) % cycle
	inhaleEnd := e.Inhale
	hold1End := inhaleEnd + e.Hold1
	exhaleEnd := hold1End + e.Exhale

	switch {
	case t < inhaleEnd:
		p := ease(frac(t, e.Inhale))
		if e.Sip { // a sigh's first inhale fills 85%; the sip takes the rest
			p = p * 850 / 1000
		}
		return Phase{"Breathe in", inhaleEnd - t, p}
	case t < hold1End:
		if e.Sip {
			return Phase{"Sip in", hold1End - t, 850 + ease(frac(t-inhaleEnd, e.Hold1))*150/1000}
		}
		return Phase{"Hold", hold1End - t, 1000}
	case t < exhaleEnd:
		return Phase{"Breathe out", exhaleEnd - t, 1000 - ease(frac(t-hold1End, e.Exhale))}
	}
	return Phase{"Hold", cycle - t, 0}
}

// Line is the countdown under the picture: "Breathe in... 4s".
func (p Phase) Line() string {
	secs := int((p.Remaining + time.Second - 1) / time.Second)
	return fmt.Sprintf("%s... %ds", p.Label, secs)
}

// ---------------------------------------------------------------------------
// Shapes

var Styles = []string{"pulse", "ripples", "dots", "wave"}

func IsStyle(s string) bool {
	for _, x := range Styles {
		if x == s {
			return true
		}
	}
	return false
}

// PickStyle returns setting, or a random style other than last when setting
// is "random".
func PickStyle(setting, last string) string {
	if IsStyle(setting) {
		return setting
	}
	var pool []string
	for _, s := range Styles {
		if s != last {
			pool = append(pool, s)
		}
	}
	return pool[rand.Intn(len(pool))]
}

// ArtRows is the height of a frame; the middle row is 3.
const (
	ArtRows = 7
	mid     = 3
)

var (
	hblk        = []rune(" ▁▂▃▄▅▆▇█")
	ripple      = []string{"━", "─", "╌", "┈"}
	pulseRatio  = []int{200, 600, 1000, 600, 200}
	rippleRatio = []int{1000, 750, 500, 250}
	// [row, column factor]: the column is -1000..1000 from the centre
	dotsAt = [][2]int{
		{3, 0}, {3, 70}, {3, -80},
		{2, 150}, {4, -160}, {2, -220}, {4, 230}, {3, 300}, {3, -310},
		{1, 250}, {5, -260}, {1, -400}, {5, 410}, {2, 450}, {4, -460},
		{2, -530}, {4, 540}, {3, 600}, {3, -620},
		{0, 400}, {6, -420}, {0, -600}, {6, 620}, {1, 700}, {5, -710},
		{1, -800}, {5, 810}, {2, 850}, {4, -860},
		{0, 900}, {6, -910}, {0, -950}, {6, 960},
	}
)

// boxWidth is the shape's widest extent: the width less a margin, at most 60.
func boxWidth(width int) int { return min(60, max(4, width-4)) }

// scaledHalf is half the shape's width in cells; never 0 while any breath is in.
func scaledHalf(progress, width int) int {
	half := boxWidth(width) / 2 * progress / 1000
	if half < 1 && progress > 0 {
		return 1
	}
	return half
}

type grid [][]rune

func blank(width int) grid {
	g := make(grid, ArtRows)
	for i := range g {
		g[i] = []rune(strings.Repeat(" ", width))
	}
	return g
}

func (g grid) centred(row int, text string) {
	if row < 0 || row >= len(g) {
		return
	}
	line, chars := g[row], []rune(text)
	start := (len(line) - len(chars)) / 2
	for i, ch := range chars {
		if col := start + i; col >= 0 && col < len(line) {
			line[col] = ch
		}
	}
}

func pulseBar(h int) string {
	fill := strings.Repeat("█", max(0, h-3))
	l, r := "", ""
	if h >= 3 {
		l += "░"
	}
	if h >= 2 {
		l += "▒"
		r = "▒"
	}
	if h >= 3 {
		r += "░"
	}
	return l + "▓" + fill + fill + "▓" + r
}

// Frame draws one frame of style at progress (0..1000): rows lines, each at
// most width cells, the shape centred. Fewer than ArtRows rows shows the
// middle of the picture; more pads it top and bottom.
func Frame(style string, progress, width, rows int) []string {
	width = max(1, width)
	g := blank(width)
	half := scaledHalf(progress, width)
	switch style {
	case "pulse":
		for i, ratio := range pulseRatio {
			if h := min(32, half*ratio/1000); h > 0 {
				g.centred(mid-2+i, pulseBar(h))
			}
		}
	case "ripples":
		for dist, ratio := range rippleRatio {
			rw := min(64, half*ratio/1000)
			if rw <= 0 {
				continue
			}
			line := strings.Repeat(ripple[dist], rw*2)
			g.centred(mid-dist, line)
			if dist > 0 {
				g.centred(mid+dist, line)
			}
		}
	case "dots":
		if progress > 0 {
			centre, maxHalf := width/2, boxWidth(width)/2
			for _, d := range dotsAt {
				row, dcol := d[0], d[1]
				abs := max(dcol, -dcol)
				if abs > progress {
					continue
				}
				col := centre + dcol*maxHalf/1000
				ch := '·'
				if abs < 150 {
					ch = '✦'
				} else if abs < 500 {
					ch = '•'
				}
				if col >= 0 && col < width {
					g[row][col] = ch
				}
			}
		}
	case "wave":
		if half > 0 {
			sh2 := half * half
			for rowIdx := 0; rowIdx < 3; rowIdx++ {
				var b strings.Builder
				any := false
				for x := -half; x <= half; x++ {
					h := min(8, max(0, 24*(sh2-x*x)/sh2-rowIdx*8))
					b.WriteRune(hblk[h])
					any = any || h > 0
				}
				if any {
					g.centred(mid+2-rowIdx, b.String())
				}
			}
		}
	}
	lines := make([]string, len(g))
	for i, l := range g {
		lines[i] = strings.TrimRight(string(l), " ")
	}
	if rows >= ArtRows {
		above := (rows - ArtRows) / 2
		out := make([]string, 0, rows)
		out = append(out, make([]string, above)...)
		out = append(out, lines...)
		return append(out, make([]string, rows-ArtRows-above)...)
	}
	skip := (ArtRows - max(0, rows)) / 2
	return lines[skip : skip+max(0, rows)]
}

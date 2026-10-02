// Package stats aggregates the session history into daily totals, streaks
// and other summaries.
package stats

import (
	"sort"
	"time"

	"github.com/antrvan746/pomodoro-cli/internal/store"
)

type Day struct {
	Date      time.Time     `json:"-"`
	Key       string        `json:"date"`
	Pomodoros int           `json:"pomodoros"`
	Focus     time.Duration `json:"-"`
	FocusMins int           `json:"focus_minutes"`
}

type Label struct {
	Name      string `json:"label"`
	Pomodoros int    `json:"pomodoros"`
	FocusMins int    `json:"focus_minutes"`
}

type Summary struct {
	Today         Day     `json:"today"`
	WeekPomodoros int     `json:"week_pomodoros"`
	WeekFocusMins int     `json:"week_focus_minutes"`
	TotalPomodoro int     `json:"total_pomodoros"`
	TotalFocusMin int     `json:"total_focus_minutes"`
	CurrentStreak int     `json:"current_streak_days"`
	BestStreak    int     `json:"best_streak_days"`
	BestDay       Day     `json:"best_day"`
	ActiveDays    int     `json:"active_days"`
	Completion    float64 `json:"completion_rate"` // completed / started focus sessions
	Hours         [24]int `json:"pomodoros_by_hour"`
	Weekdays      [7]int  `json:"pomodoros_by_weekday"` // Sunday first
	Labels        []Label `json:"top_labels"`
	Days          []Day   `json:"days"` // oldest → newest, contiguous, ending today
	Goal          int     `json:"daily_goal"`
}

func Key(t time.Time) string { return t.Format("2006-01-02") }

func dayStart(t time.Time) time.Time {
	y, m, d := t.Date()
	return time.Date(y, m, d, 0, 0, 0, 0, t.Location())
}

// Compute builds a summary. days controls how many calendar days (ending today)
// are included in Days.
func Compute(recs []store.Record, now time.Time, days int, goal int) Summary {
	now = now.Local()
	byDay := map[string]*Day{}
	labels := map[string]*Label{}
	s := Summary{Goal: goal}
	started, completed := 0, 0

	for _, r := range recs {
		if r.Kind != store.Focus {
			continue
		}
		started++
		end := r.EndedAt.Local()
		k := Key(end)
		d := byDay[k]
		if d == nil {
			d = &Day{Date: dayStart(end), Key: k}
			byDay[k] = d
		}
		d.Focus += r.Actual.D()
		s.TotalFocusMin += int(r.Actual.D().Minutes())
		name := r.Label
		if name == "" {
			name = "(unlabeled)"
		}
		l := labels[name]
		if l == nil {
			l = &Label{Name: name}
			labels[name] = l
		}
		l.FocusMins += int(r.Actual.D().Minutes())
		if r.Completed {
			completed++
			d.Pomodoros++
			l.Pomodoros++
			s.TotalPomodoro++
			s.Hours[r.StartedAt.Local().Hour()]++
			s.Weekdays[end.Weekday()]++
		}
	}
	if started > 0 {
		s.Completion = float64(completed) / float64(started)
	}
	for _, d := range byDay {
		d.FocusMins = int(d.Focus.Minutes())
		if d.Pomodoros > 0 {
			s.ActiveDays++
		}
		if d.Pomodoros > s.BestDay.Pomodoros {
			s.BestDay = *d
		}
	}

	today := dayStart(now)
	get := func(t time.Time) Day {
		if d, ok := byDay[Key(t)]; ok {
			return *d
		}
		return Day{Date: t, Key: Key(t)}
	}
	s.Today = get(today)

	// Week starts on Monday.
	wd := (int(today.Weekday()) + 6) % 7
	for i := 0; i <= wd; i++ {
		d := get(today.AddDate(0, 0, -i))
		s.WeekPomodoros += d.Pomodoros
		s.WeekFocusMins += d.FocusMins
	}

	// Current streak: consecutive active days ending today (or yesterday, so
	// the streak isn't "lost" before you've had a chance to work today).
	cur := today
	if get(cur).Pomodoros == 0 {
		cur = cur.AddDate(0, 0, -1)
	}
	for get(cur).Pomodoros > 0 {
		s.CurrentStreak++
		cur = cur.AddDate(0, 0, -1)
	}

	// Best streak over all history.
	keys := make([]string, 0, len(byDay))
	for k, d := range byDay {
		if d.Pomodoros > 0 {
			keys = append(keys, k)
		}
	}
	sort.Strings(keys)
	run := 0
	var prev time.Time
	for _, k := range keys {
		t := byDay[k].Date
		if run > 0 && dayStart(prev.AddDate(0, 0, 1)).Equal(t) {
			run++
		} else {
			run = 1
		}
		if run > s.BestStreak {
			s.BestStreak = run
		}
		prev = t
	}

	for i := days - 1; i >= 0; i-- {
		s.Days = append(s.Days, get(today.AddDate(0, 0, -i)))
	}

	for _, l := range labels {
		s.Labels = append(s.Labels, *l)
	}
	sort.Slice(s.Labels, func(i, j int) bool {
		if s.Labels[i].Pomodoros != s.Labels[j].Pomodoros {
			return s.Labels[i].Pomodoros > s.Labels[j].Pomodoros
		}
		return s.Labels[i].FocusMins > s.Labels[j].FocusMins
	})
	if len(s.Labels) > 5 {
		s.Labels = s.Labels[:5]
	}
	return s
}

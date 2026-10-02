package stats

import (
	"testing"
	"time"

	"github.com/antrvan746/pomodoro-cli/internal/store"
)

func rec(end time.Time, completed bool, label string) store.Record {
	d := 25 * time.Minute
	return store.Record{
		Kind: store.Focus, Label: label, Planned: store.Seconds(d), Actual: store.Seconds(d),
		StartedAt: end.Add(-d), EndedAt: end, Completed: completed,
	}
}

func TestCompute(t *testing.T) {
	now := time.Date(2026, 10, 2, 18, 0, 0, 0, time.Local) // Friday
	day := func(back int) time.Time { return now.AddDate(0, 0, -back).Add(-6 * time.Hour) }
	recs := []store.Record{
		rec(day(0), true, "a"), rec(day(0), true, "a"), rec(day(0), false, "a"),
		rec(day(1), true, "b"),
		rec(day(2), true, "b"),
		// gap on day 3
		rec(day(4), true, "c"), rec(day(5), true, "c"), rec(day(6), true, "c"), rec(day(7), true, "c"),
		{Kind: store.ShortBreak, Planned: 300, Actual: 300, EndedAt: day(0), Completed: true},
	}
	s := Compute(recs, now, 14, 8)

	if s.Today.Pomodoros != 2 {
		t.Errorf("today = %d, want 2", s.Today.Pomodoros)
	}
	if s.CurrentStreak != 3 {
		t.Errorf("current streak = %d, want 3", s.CurrentStreak)
	}
	if s.BestStreak != 4 {
		t.Errorf("best streak = %d, want 4", s.BestStreak)
	}
	if s.TotalPomodoro != 8 {
		t.Errorf("total = %d, want 8", s.TotalPomodoro)
	}
	// Week starts Monday 28 Sep: days 0..4 back → 2+1+1+0+1.
	if s.WeekPomodoros != 5 {
		t.Errorf("week = %d, want 5", s.WeekPomodoros)
	}
	if len(s.Days) != 14 || s.Days[13].Key != "2026-10-02" {
		t.Errorf("days window wrong: len=%d last=%s", len(s.Days), s.Days[len(s.Days)-1].Key)
	}
	if s.Labels[0].Name != "c" || s.Labels[0].Pomodoros != 4 {
		t.Errorf("top label = %+v", s.Labels[0])
	}
	if got := s.Completion; got < 0.88 || got > 0.9 {
		t.Errorf("completion = %v, want 8/9", got)
	}
}

func TestStreakSurvivesUntilYouWorkToday(t *testing.T) {
	now := time.Date(2026, 10, 2, 8, 0, 0, 0, time.Local)
	recs := []store.Record{rec(now.AddDate(0, 0, -1), true, ""), rec(now.AddDate(0, 0, -2), true, "")}
	if s := Compute(recs, now, 7, 0); s.CurrentStreak != 2 {
		t.Errorf("streak = %d, want 2", s.CurrentStreak)
	}
}

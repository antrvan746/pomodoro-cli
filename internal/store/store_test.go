package store

import (
	"errors"
	"testing"
	"time"
)

func TestLifecycle(t *testing.T) {
	t.Setenv("POMO_HOME", t.TempDir())

	a, err := Start(Focus, 50*time.Millisecond, "write", "test", 0, false)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := Start(Focus, time.Minute, "", "test", 0, false); !errors.Is(err, ErrRunning) {
		t.Fatalf("second start: got %v, want ErrRunning", err)
	}

	// Pausing freezes the clock.
	if _, err := Pause(); err != nil {
		t.Fatal(err)
	}
	time.Sleep(80 * time.Millisecond)
	if cur, fin, _ := Current(); cur == nil || fin != nil {
		t.Fatal("paused session should not finish")
	}
	if _, err := Resume(); err != nil {
		t.Fatal(err)
	}

	time.Sleep(80 * time.Millisecond)
	// Only one caller finalises.
	r1, _ := Finish(a.ID)
	r2, _ := Finish(a.ID)
	if r1 == nil || r2 != nil {
		t.Fatalf("finish: r1=%v r2=%v", r1, r2)
	}
	if !r1.Completed || r1.Label != "write" {
		t.Errorf("record = %+v", r1)
	}
	if cur, _ := Peek(); cur != nil {
		t.Error("state should be cleared")
	}
	hs, _ := History()
	if len(hs) != 1 {
		t.Fatalf("history len = %d", len(hs))
	}
}

func TestStopRecordsPartial(t *testing.T) {
	t.Setenv("POMO_HOME", t.TempDir())
	if _, err := Start(Focus, time.Hour, "", "test", 0, false); err != nil {
		t.Fatal(err)
	}
	r, err := Stop("")
	if err != nil {
		t.Fatal(err)
	}
	if r.Completed {
		t.Error("stopped session must not count as completed")
	}
	if _, err := Stop(""); !errors.Is(err, ErrNoSession) {
		t.Errorf("stop when idle: %v", err)
	}
}

func TestExtend(t *testing.T) {
	t.Setenv("POMO_HOME", t.TempDir())
	if _, err := Start(Focus, 10*time.Minute, "", "test", 0, false); err != nil {
		t.Fatal(err)
	}
	a, _ := Extend(5 * time.Minute)
	if a.Duration.D() != 15*time.Minute {
		t.Errorf("duration = %v", a.Duration.D())
	}
	a, _ = Extend(-time.Hour)
	if a.Duration.D() > 2*time.Second {
		t.Errorf("shrink should clamp to elapsed, got %v", a.Duration.D())
	}
}

func TestViewerPerPlace(t *testing.T) {
	t.Setenv("POMO_HOME", t.TempDir())
	t.Setenv("TMUX", "")
	t.Setenv("CMUX_WORKSPACE_ID", "ws-a")

	if ViewerOpen() {
		t.Fatal("no viewer registered yet")
	}
	done := RegisterViewer()
	if !ViewerOpen() {
		t.Fatal("viewer in the same workspace should count")
	}

	t.Setenv("CMUX_WORKSPACE_ID", "ws-b")
	if ViewerOpen() {
		t.Fatal("viewer in another workspace should not count")
	}

	done()
	t.Setenv("CMUX_WORKSPACE_ID", "ws-a")
	if ViewerOpen() {
		t.Fatal("closed viewer should not count")
	}
}

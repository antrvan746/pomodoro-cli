package cli

import (
	"os"
	"testing"
)

func TestWindowsCommand(t *testing.T) {
	cases := []struct {
		exe, args string
		envs      []string
		want      string
	}{
		{`C:\Users\me\go\bin\pomo.exe`, "watch --auto-close", nil,
			`C:/Users/me/go/bin/pomo.exe watch --auto-close`},
		{`C:\Program Files\pomo\pomo.exe`, "watch", nil,
			`"C:/Program Files/pomo/pomo.exe" watch`},
		{`C:\pomo.exe`, "watch", []string{`POMO_HOME=D:\my data`, "POMO_THEME=sand"},
			`C:/pomo.exe "--pomo-env=POMO_HOME=D:\my data" --pomo-env=POMO_THEME=sand watch`},
		{`C:\pomo.exe`, "watch", []string{`POMO_HOME=D:\p`},
			`C:/pomo.exe "--pomo-env=POMO_HOME=D:\p" watch`},
	}
	for _, c := range cases {
		if got := windowsCommand(c.exe, c.args, c.envs); got != c.want {
			t.Errorf("windowsCommand(%q) = %q, want %q", c.exe, got, c.want)
		}
	}
}

func TestTakeLaunchArgs(t *testing.T) {
	old := os.Args
	defer func() { os.Args = old }()
	t.Setenv("POMO_TEST_ENV", "")
	os.Args = []string{"pomo", "--pomo-env=POMO_TEST_ENV=a=b", "watch", "--close-wmux-pane", "--auto-close"}
	if !takeLaunchArgs() {
		t.Error("closePane = false, want true")
	}
	if got := os.Getenv("POMO_TEST_ENV"); got != "a=b" {
		t.Errorf("POMO_TEST_ENV = %q, want a=b", got)
	}
	if want := []string{"pomo", "watch", "--auto-close"}; len(os.Args) != 3 || os.Args[1] != want[1] || os.Args[2] != want[2] {
		t.Errorf("os.Args = %q, want %q", os.Args, want)
	}
}

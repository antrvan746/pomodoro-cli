package cli

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/antrvan746/pomodoro-cli/internal/store"
	"github.com/antrvan746/pomodoro-cli/internal/ui"
)

func TestPluginSkillInSync(t *testing.T) {
	b, err := os.ReadFile("../../plugin/skills/pomo/SKILL.md")
	if err != nil {
		t.Fatal(err)
	}
	if string(b) != skillMD {
		t.Fatal("plugin/skills/pomo/SKILL.md differs from internal/cli/SKILL.md; copy it over")
	}
}

func TestOrderedJSONRoundTrip(t *testing.T) {
	p := filepath.Join(t.TempDir(), "settings.json")
	in := "{\n  \"zeta\": 1,\n  \"alpha\": {\"b\": [1, 2]},\n  \"statusLine\": {\"type\": \"command\", \"command\": \"x\"}\n}\n"
	if err := os.WriteFile(p, []byte(in), 0o644); err != nil {
		t.Fatal(err)
	}
	o, err := readOrderedJSON(p)
	if err != nil {
		t.Fatal(err)
	}
	o.set("statusLine", []byte(`{"type":"command","command":"y"}`))
	o.set("new", []byte(`true`))
	if err := writeOrderedJSON(p, o); err != nil {
		t.Fatal(err)
	}
	out, _ := os.ReadFile(p)
	s := string(out)
	if !(strings.Index(s, "zeta") < strings.Index(s, "alpha") && strings.Index(s, "alpha") < strings.Index(s, "statusLine") &&
		strings.Index(s, "statusLine") < strings.Index(s, "new")) {
		t.Errorf("key order not preserved:\n%s", s)
	}
	if !strings.Contains(s, `"command": "y"`) {
		t.Errorf("value not replaced:\n%s", s)
	}
}

func TestSetupClaudeWrapsAndRestores(t *testing.T) {
	dir := t.TempDir()
	t.Setenv("CLAUDE_CONFIG_DIR", dir)
	t.Setenv("POMO_HOME", t.TempDir())
	settings := filepath.Join(dir, "settings.json")
	orig := `{"model": "opus", "statusLine": {"type": "command", "command": "~/.claude/statusline.sh", "padding": 0}}`
	_ = os.WriteFile(settings, []byte(orig), 0o644)

	if err := setupClaude(); err != nil {
		t.Fatal(err)
	}
	b, _ := os.ReadFile(settings)
	s := string(b)
	for _, want := range []string{"statusline --wrap '~/.claude/statusline.sh'", `"refreshInterval": 1`, `"padding": 0`, `"model": "opus"`} {
		if !strings.Contains(s, want) {
			t.Errorf("settings missing %q:\n%s", want, s)
		}
	}
	if err := setupClaude(); err != nil { // idempotent
		t.Fatal(err)
	}
	if err := uninstallClaude(); err != nil {
		t.Fatal(err)
	}
	b, _ = os.ReadFile(settings)
	if !strings.Contains(string(b), `"command": "~/.claude/statusline.sh"`) || strings.Contains(string(b), "pomo") {
		t.Errorf("not restored:\n%s", b)
	}
}

func TestStatusSegment(t *testing.T) {
	cfg := store.DefaultConfig()
	if ui.StatusSegment(nil, cfg, "classic", time.Now()) != "" {
		t.Error("idle segment should be empty")
	}
	a := &store.Active{Kind: store.Focus, Label: "tests", Duration: store.Seconds(25 * time.Minute),
		StartedAt: time.Now().Add(-5 * time.Minute), Cycle: 2}
	seg := ui.StatusSegment(a, cfg, "full", time.Now())
	for _, want := range []string{"🍅", "20:00", "▕", "●●○○", "tests"} {
		if !strings.Contains(seg, want) {
			t.Errorf("segment %q missing %q", seg, want)
		}
	}
}

func TestCodexBreatheHooks(t *testing.T) {
	p := filepath.Join(t.TempDir(), "hooks.json")
	orig := `{"hooks":{"Stop":[{"hooks":[{"type":"command","command":"other"}]}]},"x":1}`
	if err := os.WriteFile(p, []byte(orig), 0o644); err != nil {
		t.Fatal(err)
	}
	if changed, err := setCodexBreatheHooks(p, true); err != nil || !changed {
		t.Fatalf("install: changed=%v err=%v", changed, err)
	}
	if changed, _ := setCodexBreatheHooks(p, true); changed {
		t.Fatal("installing twice should change nothing")
	}
	var got struct{ Hooks map[string][]any }
	b, _ := os.ReadFile(p)
	_ = json.Unmarshal(b, &got)
	if len(got.Hooks["Stop"]) != 2 || len(got.Hooks["UserPromptSubmit"]) != 1 {
		t.Fatalf("want the other Stop hook kept beside ours: %s", b)
	}
	if _, err := setCodexBreatheHooks(p, false); err != nil {
		t.Fatal(err)
	}
	b, _ = os.ReadFile(p)
	if !jsonEqual(b, []byte(orig)) {
		t.Fatalf("uninstall should restore the original: %s", b)
	}
}

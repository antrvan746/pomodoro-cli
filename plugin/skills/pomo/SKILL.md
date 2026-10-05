---
name: pomo
description: Start, check, pause, stop and report on pomodoro focus sessions with the `pomo` CLI. Use when the user asks to start a pomodoro / focus timer / break, asks how much time is left, or wants their focus statistics.
---

# pomo — pomodoro timer

`pomo` is a terminal pomodoro timer. All state is on disk, so sessions you
start here also appear in the user's full-screen timer (`pomo watch`), and a
background process sends a desktop notification when the timer ends.

Always pass `--json` and read the result; never run bare `pomo`. Starting a
session from here also opens the big clock in a pane next to the user's
terminal (cmux, wmux, tmux, WezTerm, kitty, iTerm, Terminal, Ghostty); pass
`--no-open` if they don't want it. `pomo watch` opens the clock on demand.

## Commands

| Goal | Command |
| --- | --- |
| Start focus (default length) | `pomo start --detach --json` |
| Start focus with length / label | `pomo start 50 write tests --detach --json` |
| Short / long break | `pomo break --json` / `pomo long --json` |
| Skip to the next phase | `pomo skip --json` |
| Replace a running session | add `--force` |
| Time left | `pomo status --json` |
| Pause / resume | `pomo pause --json` / `pomo resume --json` |
| Add or remove minutes | `pomo extend 5 --json` / `pomo extend -- -5 --json` |
| Rename the session | `pomo label "new label" --json` |
| Stop early | `pomo stop --json` |
| Statistics | `pomo stats --json` (add `--weeks 4` to trim `days`) |
| History | `pomo log --json -n 10` |
| Record a past session | `pomo log add -m 25 -l "label" --at "2026-10-02 14:30" --json` |
| Settings | `pomo config --json`, `pomo config set focus 50` |
| Colour theme | `pomo theme --json`, `pomo theme ember` (catppuccin, mocha, latte, ember, sand) |
| Breathing exercise settings | `pomo breathe --json`, `pomo breathe box` (hrv, sigh, box, 478, `style wave`, `delay 5`, on, off) |

## Output

`status`, `start`, `pause`, `resume`, `extend`, `label` return:

```json
{ "active": true, "kind": "focus", "label": "write tests", "state": "running",
  "remaining": "18:42", "remaining_sec": 1122, "progress": 0.252,
  "ends_at": "2026-10-02T15:04:00+02:00",
  "today": { "pomodoros": 3, "focus_minutes": 75, "daily_goal": 8, "streak_days": 4 } }
```

`kind` is `focus`, `short_break` or `long_break`; `state` is `running`,
`paused` or `idle`. There is one timer per machine: starting while one runs
returns it with `"already_running": true` instead of a new session; ask the
user before replacing it with `--force`. Errors print `{"error": "..."}` and
exit 1.

## Tips

- If the user is mid-task, label the session with what you're working on.
- When reporting stats, summarise: today vs. goal, streak, week total, top labels.
- Tell the user they can watch the big clock with `pomo watch` in a terminal.

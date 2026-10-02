package cli

import (
	_ "embed"
	"fmt"
	"strconv"
	"strings"

	"github.com/spf13/cobra"

	"github.com/antrvan746/pomodoro-cli/internal/store"
	"github.com/antrvan746/pomodoro-cli/internal/ui"
)

type configKey struct {
	name string
	get  func(c *store.Config) any
	set  func(c *store.Config, v string) error
}

func intKey(name string, p func(c *store.Config) *int) configKey {
	return configKey{name, func(c *store.Config) any { return *p(c) }, func(c *store.Config, v string) error {
		n, err := strconv.Atoi(v)
		if err != nil || n < 0 {
			return fmt.Errorf("%s must be a non-negative integer", name)
		}
		*p(c) = n
		return nil
	}}
}

func boolKey(name string, p func(c *store.Config) *bool) configKey {
	return configKey{name, func(c *store.Config) any { return *p(c) }, func(c *store.Config, v string) error {
		b, err := strconv.ParseBool(v)
		if err != nil {
			return fmt.Errorf("%s must be true or false", name)
		}
		*p(c) = b
		return nil
	}}
}

var configKeys = []configKey{
	intKey("focus", func(c *store.Config) *int { return &c.FocusMin }),
	intKey("short_break", func(c *store.Config) *int { return &c.ShortBreakMin }),
	intKey("long_break", func(c *store.Config) *int { return &c.LongBreakMin }),
	intKey("long_break_every", func(c *store.Config) *int { return &c.LongBreakEvery }),
	intKey("daily_goal", func(c *store.Config) *int { return &c.DailyGoal }),
	boolKey("auto_start_break", func(c *store.Config) *bool { return &c.AutoStartBreak }),
	boolKey("auto_start_focus", func(c *store.Config) *bool { return &c.AutoStartFocus }),
	boolKey("notify", func(c *store.Config) *bool { return &c.Notify }),
	boolKey("sound", func(c *store.Config) *bool { return &c.Sound }),
	boolKey("cmux", func(c *store.Config) *bool { return &c.Cmux }),
	enumKey("theme", func(c *store.Config) *string { return &c.Theme }, ui.ThemeNames()),
	enumKey("open_clock", func(c *store.Config) *string { return &c.OpenClock }, []string{"auto", "always", "never"}),
	enumKey("statusline_view", func(c *store.Config) *string { return &c.StatusView }, []string{"minimal", "classic", "full"}),
}

func enumKey(name string, p func(c *store.Config) *string, allowed []string) configKey {
	return configKey{name, func(c *store.Config) any { return *p(c) }, func(c *store.Config, v string) error {
		for _, a := range allowed {
			if a == v {
				*p(c) = v
				return nil
			}
		}
		return fmt.Errorf("%s must be one of: %s", name, strings.Join(allowed, ", "))
	}}
}

func configCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "config",
		Short: "Show or change settings",
		RunE: func(cmd *cobra.Command, args []string) error {
			c := store.LoadConfig()
			if jsonOut {
				return printJSON(c)
			}
			fmt.Println(ui.Logo() + subtle().Render("  config · "+store.ConfigPath()))
			for _, k := range configKeys {
				fmt.Printf("  %-18s %v\n", accent(ui.Key).Render(k.name), k.get(&c))
			}
			fmt.Println(subtle().Render("\n  change with: pomo config set <key> <value>"))
			return nil
		},
	}
	set := &cobra.Command{
		Use:   "set <key> <value>",
		Short: "Change a setting (durations in minutes)",
		Args:  cobra.ExactArgs(2),
		RunE: func(cmd *cobra.Command, args []string) error {
			c := store.LoadConfig()
			for _, k := range configKeys {
				if k.name == args[0] {
					if err := k.set(&c, args[1]); err != nil {
						return err
					}
					if err := store.SaveConfig(c); err != nil {
						return err
					}
					if jsonOut {
						return printJSON(c)
					}
					fmt.Printf("%s %s = %v\n", accent(ui.Highlight).Render("✓"), args[0], k.get(&c))
					return nil
				}
			}
			return fmt.Errorf("unknown key %q", args[0])
		},
	}
	cmd.AddCommand(set)
	return cmd
}

//go:embed SKILL.md
var skillMD string

func agentCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "agent",
		Short: "Print the SKILL.md that teaches AI agents to drive pomo",
		Long:  "Prints a SKILL.md describing pomo's agent-friendly commands.\nInstall it with `pomo setup codex` or the Claude Code plugin.",
		RunE: func(cmd *cobra.Command, args []string) error {
			fmt.Print(skillMD)
			return nil
		},
	}
}

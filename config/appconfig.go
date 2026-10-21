// Package config loads the user's customization from one or more TOML files via viper,
// merged in order so a later file wins. It covers which CLI agents run and the flags it
// takes, the tags an agent can be given, the panel keybindings, and which database, address
// and tmux server this komrad is.
package config

import (
	"fmt"
	"strings"
)

// Config is the user's customization.
type Config struct {
	// Which agent CLI a new agent runs when neither the agent nor its workflow
	// names one. It has to be a harness this binary implements, so it is one of
	// the names under [harness] below.
	DefaultHarness string `mapstructure:"default_harness"`
	// Whether `komrad open` sets up a workflow for the git checkout it is run in, with a
	// worktree for each agent and an orchestrator standing by, when no workflow covers that
	// directory yet.
	AutoSetup bool      `mapstructure:"auto_setup"`
	Harness   Harnesses `mapstructure:"harness"`
	// Limits how much agents may run and how large their conversations may grow.
	Usage Usage `mapstructure:"usage"`
	// Sets how much of the window the panel's sidebar, the stage beside it, and the info pane under the stage each take, and which side of the stage the info pane opens on.
	Layout Layout `mapstructure:"layout"`
	// Panel keybindings, scoped to the screen each binding acts in.
	Keybindings Keybindings `mapstructure:"keybindings"`
	// Edit the panel's text fields the way vim does. A form's field opens in insert mode, esc goes
	// to normal mode, and a second esc closes it. The filter, search and rename lines close on esc,
	// and ctrl+f puts one in normal mode, after which esc works as it does in a form.
	VimMode bool `mapstructure:"vim_mode"`
	// Sets how the panel draws a diff.
	Diff Diff `mapstructure:"diff"`
	// The icons the panel draws, grouped by where each one is drawn. Every icon is one character
	// from the approved set, the Nerd Fonts octicons and a few named characters. The chrome the
	// layout is drawn with, such as the status bars, dividers and rules, is fixed and not
	// configured here.
	Glyphs Glyphs `mapstructure:"glyphs"`
	// The colours the panel draws in, one for each part a colour plays. Each is a terminal colour
	// number from 0 to 255, which your terminal's theme decides the look of, or a hex code like
	// "#1e3a5f".
	Palette Palette `mapstructure:"palette"`
	// Sets where komrad finds the scripts a workflow hangs on a moment in its agents' lives, and how long
	// each may run.
	Hooks Hooks `mapstructure:"hooks"`
	// Sets where a workflow's generated worktree provisioner creates its worktrees.
	Worktrees Worktrees `mapstructure:"worktrees"`
	// Controls how komrad reads the git checkouts its agents work in.
	Git Git `mapstructure:"git"`
	// Controls how komrad reads the review threads on the pull requests its agents open.
	Forge Forge `mapstructure:"forge"`
	// The GitHub App an engine fetches and pushes as.
	GitHubApp GitHubApp `mapstructure:"github_app"`
	// The standing text every agent is started with, whatever workflow it is in.
	Context Context `mapstructure:"context"`
	// Controls how komrad delivers mail between agents.
	Mail Mail `mapstructure:"mail"`
	// The log the server writes, which the panel's log key pages.
	Log Log `mapstructure:"log"`
	// Puts komrad into debug mode which records additional information used to help diagnose issues.
	Debug Debug `mapstructure:"debug"`
	// The identity provider people sign in to this engine with, and the listener their requests
	// reach it on.
	SignIn SignIn `mapstructure:"sign_in"`
	// Which komrad this is, by its database, its address, its tmux socket and its cache.
	Connection Connection `mapstructure:"connection"`
}

// Defaults is the configuration a user who has written nothing runs on, and what the
// generated config.toml spells out, so the two agree by construction.
func Defaults() *Config {
	return &Config{
		DefaultHarness: DefaultHarness,
		Usage:          newUsage(),
		Layout:         DefaultLayout(),
		Keybindings:    DefaultKeybindings(),
		Glyphs:         defaultGlyphs(),
		Palette:        defaultPalette(),
		Forge:          Forge{MaxCheckLogBytes: defaultMaxCheckLogBytes},
		Mail:           Mail{DeliveryDeadline: defaultDeliveryDeadline, MaxExchange: DefaultMaxExchange},
		Hooks:          Hooks{Timeout: defaultHookTimeout},
		Log:            Log{Level: defaultLogLevel},
		Debug: Debug{
			FrameHistory: DefaultFrameHistory,
			Pprof: Pprof{
				Server: Profiler{Address: defaultPprofServerAddress},
				Panel:  Profiler{Address: defaultPprofPanelAddress},
			},
		},
		Diff:       Diff{Wrap: true},
		Connection: defaultConnection(),
		SignIn:     SignIn{Addr: defaultSignInAddr},
	}
}

// validate rejects the settings the panel could not run on, so a config that cannot work
// says so when it is read rather than when something tries to draw with it.
func (c Config) validate() error {
	sections := []func() error{
		c.Log.validate,
		c.Usage.validate,
		c.Keybindings.validate,
		c.Mail.validate,
		c.Hooks.validate,
		c.Git.validate,
		c.Glyphs.validate,
		c.Palette.validate,
		c.Connection.validate,
		c.SignIn.validate,
		c.Layout.validate,
	}
	for _, validate := range sections {
		if err := validate(); err != nil {
			return err
		}
	}

	return nil
}

func nonNegative(key string, value int, unit string) error {
	if value < 0 {
		return fmt.Errorf("%s: %d is not %s", key, value, unit)
	}

	return nil
}

// normalize keeps the settings whose zero has historically meant "unset" on
// their built-in values. Collections are deliberately excluded: an explicit empty list
// clears a list supplied by a default or lower layer.
func (c *Config) normalize() {
	if strings.TrimSpace(c.DefaultHarness) == "" {
		c.DefaultHarness = DefaultHarness
	}

	c.Usage.normalize()
	c.Layout.normalize()
	c.Connection.normalize()
	c.SignIn.normalize()
	c.Log.normalize()
	c.Debug.normalize()
}

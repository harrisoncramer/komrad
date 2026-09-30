// Package cli builds the komrad command tree, which opens the panel when run with no subcommand
// and otherwise starts the service, attaches to an agent, runs the doctor, or calls one RPC.
//
// The rpc subcommands are not written here. They are generated from the same proto descriptors
// the handlers and the MCP tools come from, so every endpoint is reachable from a shell without
// anybody adding a command for it.
package cli

import (
	"errors"
	"fmt"
	"log/slog"

	"komrad/config"
	"komrad/internal/agents"
	"komrad/internal/glyph"
	"komrad/internal/harness"
	"komrad/internal/harness/claude"
	"komrad/internal/harness/codex"
	"komrad/internal/loader"
	"komrad/internal/logging"
	"komrad/internal/tmux"
	"komrad/internal/ui"

	"github.com/spf13/cobra"
)

// IsQuiet reports whether an error leaves main nothing to print.
func IsQuiet(err error) bool {
	return errors.Is(err, ErrLogged) || errors.Is(err, ErrReported)
}

// NewRootCommand builds the komrad command tree, which opens the panel when run with no
// subcommand.
func NewRootCommand() *cobra.Command {
	app := &app{}

	var (
		configPaths []string
		engineName  string
	)

	root := &cobra.Command{
		Use:   "komrad",
		Short: "tmux panel and CLI for background coding agents",
		Long: `Komrad runs coding agents in tmux panes and keeps track of them.

There are two processes. ` + "`komrad serve`" + ` is the long-lived service that owns the
database, answers the API and the MCP endpoint, and watches every pane. The panel
is a client of it, which is why it can be closed and reopened without stopping
anything. Every other command here is a client of the same service.

` + "`komrad open`" + ` brings the panel up in this terminal, and the other commands are
for the times something has to be done by hand: check the machine, start or stop
the service, or call an endpoint from a script.`,
		Args:          cobra.NoArgs,
		SilenceUsage:  true,
		SilenceErrors: true,
		PersistentPreRunE: func(cmd *cobra.Command, _ []string) error {
			stack, err := resolveConfigStack(configPaths, engineName)
			if err != nil {
				return err
			}

			cfg, report, err := config.LoadWithReport(stack)
			if err != nil {
				if isConfigFatal(cmd) {
					return err
				}

				app.configErr = err
				cfg = config.Defaults()
				report = &config.Report{Sources: map[string]config.Source{}}
			}

			env, err := loadEnvironment(cfg)
			if err != nil {
				return err
			}

			codexHarness := codex.NewHarness(codex.NewHarnessParams{
				Home: codex.NamedHome(),
			})
			claudeHarness := claude.NewHarness(claude.NewHarnessParams{
				ConfigDir: claude.NamedConfigDir(),
			})

			harnessRegistry, err := harness.NewRegistry(claudeHarness, codexHarness)
			if err != nil {
				return fmt.Errorf("failed to create registry: %w", err)
			}
			if _, err := harnessRegistry.GetByID(cfg.DefaultHarness); err != nil && app.configErr == nil {
				app.configErr = fmt.Errorf("failed to find the default_harness: %w", err)
			}
			if err := ui.ValidateKeybindings(cfg.Keybindings); err != nil && app.configErr == nil {
				app.configErr = err
			}
			app.harnesses = harnessRegistry

			app.layout = cfg.Layout
			app.githubApp = cfg.GitHubApp
			app.logLevel = cfg.Log.Level
			app.configStack = stack
			app.env = env
			app.configSources = report.Sources
			app.keys = cfg.Keybindings
			app.loader, err = loader.Parse(cfg.Glyphs.Loader)
			if err != nil && app.configErr == nil {
				app.configErr = fmt.Errorf("failed to read glyphs.loader: %w", err)
			}
			app.glyphs, err = cfg.Glyphs.Set()
			if err != nil {
				if app.configErr == nil {
					app.configErr = err
				}
				app.glyphs = glyph.Defaults()
			}
			app.defaultHarness = cfg.DefaultHarness
			app.usage = cfg.Usage

			app.tmux = tmux.NewClient(env.TmuxSocket)

			app.session = ui.NewSession(app.tmux, agents.ManagerSession, tmux.LeaderKey, tmux.LeaderTable)
			contextPath, err := cfg.GetContextFile()
			if err != nil {
				ctx := cmd.Context()

				logging.From(ctx).LogAttrs(ctx, slog.LevelDebug, "failed to resolve the context file", logging.Err(err)...)
			}

			app.contextPath = contextPath
			app.defaultFlags = map[string]string{
				claudeHarness.ID(): cfg.Harness.Claude.Flags,
				codexHarness.ID():  cfg.Harness.Codex.Flags,
			}
			app.trustDirs = map[string]bool{
				claudeHarness.ID(): cfg.Harness.Claude.TrustLaunchDirs,
				codexHarness.ID():  cfg.Harness.Codex.TrustLaunchDirs,
			}

			return nil
		},
	}

	root.PersistentFlags().StringArrayVar(&configPaths, "config", nil, "path to a config TOML, repeatable, later files win over earlier ones")
	root.PersistentFlags().StringVar(&engineName, "engine", "", "engine to reach: local for this machine, or a file in the config's engines directory, layered over the config")

	if err := root.RegisterFlagCompletionFunc("engine", completeEngines); err != nil {
		panic(err)
	}

	root.AddCommand(
		doctorCmd(app),
		setupCmd(app),
		panelCmd(app),
		openCmd(app),
		attachCmd(app),
		githubCmd(app),
		forgeCmd(app),
		prCmd(app),
		serveCmd(app),
		startCmd(app),
		stopCmd(app),
		versionCmd(app),
		migrateCmd(app),
		engineCmd(app),
		rpcCmd(app),
		hookCmd(app),
		logCmd(app),
		shellCmd(app),
		terminalCmd(app),
	)

	return root
}

var commandsSurvivingBadConfig = map[string]bool{
	"doctor":  true,
	"setup":   true,
	"version": true,
	"panel":   true,
}

func resolveConfigStack(flags []string, engine string) (config.Stack, error) {
	paths := flags
	if len(paths) == 0 {
		paths = config.LayerPaths()
	}

	if engine == config.LocalEngine {
		engine = ""
	}

	if engine != "" {
		if _, err := config.EnginePath(engine); err != nil {
			return config.Stack{}, err
		}
	}

	return config.Stack{Paths: paths, Engine: engine}, nil
}

func completeEngines(*cobra.Command, []string, string) ([]string, cobra.ShellCompDirective) {
	names, err := config.EngineNames()
	if err != nil {
		return nil, cobra.ShellCompDirectiveError
	}

	return append([]string{config.LocalEngine}, names...), cobra.ShellCompDirectiveNoFileComp
}

// configArgs is the flags that put a child process on the same config stack.
func configArgs(stack config.Stack) []string {
	args := make([]string, 0, len(stack.Paths)*2+2)
	for _, path := range stack.Paths {
		args = append(args, "--config", path)
	}

	if stack.Engine != "" {
		args = append(args, "--engine", stack.Engine)
	}

	return args
}

func isConfigFatal(cmd *cobra.Command) bool {
	return !commandsSurvivingBadConfig[cmd.Name()]
}

func loadEnvironment(cfg *config.Config) (*config.Environment, error) {
	return config.LoadEnvironment(config.Environment{
		DatabaseURL:         cfg.Connection.DatabaseURL,
		ReadOnlyDatabaseURL: cfg.Connection.ReadOnlyDatabaseURL,
		OwnerDatabaseURL:    cfg.Connection.OwnerDatabaseURL,
		Addr:                cfg.Connection.Addr,
		TmuxSocket:          cfg.Connection.TmuxSocket,
		CacheDir:            cfg.Connection.CacheDir,
		SSH:                 cfg.Connection.SSH,
		Forward:             cfg.Connection.Forward,
	})
}

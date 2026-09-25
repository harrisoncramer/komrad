// Package cli builds the komrade command tree, which opens the panel when run with no subcommand
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

	"komrade/config"
	"komrade/internal/agents"
	"komrade/internal/harness"
	"komrade/internal/harness/claude"
	"komrade/internal/harness/codex"
	"komrade/internal/logging"
	"komrade/internal/tmux"
	"komrade/internal/ui"

	"github.com/spf13/cobra"
)

// IsQuiet reports whether an error leaves main nothing to print, either because the command has
// already had its say or because a non-zero exit is the answer it was asked for.
func IsQuiet(err error) bool {
	return errors.Is(err, ErrLogged) || errors.Is(err, ErrReported) || errors.Is(err, ErrPanelHidden)
}

// NewRootCommand builds the komrade command tree, which opens the panel when run with no
// subcommand.
func NewRootCommand() *cobra.Command {
	app := &app{}

	var (
		configPaths []string
		engineName  string
	)

	root := &cobra.Command{
		Use:   "komrade",
		Short: "tmux panel and CLI for background coding agents",
		Long: `Komrade runs coding agents in tmux panes and keeps track of them.

There are two processes. ` + "`komrade serve`" + ` is the long-lived service that owns the
database, answers the API and the MCP endpoint, and watches every pane. The panel
is a client of it, which is why it can be closed and reopened without stopping
anything. Every other command here is a client of the same service.

The panel is normally reached through the tmux key binding rather than by name,
and the commands below are for the times something has to be done by hand: check
the machine, start or stop the service, or call an endpoint from a script.`,
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
				Home:            codex.NamedHome(),
				TrustLaunchDirs: cfg.Harness.Codex.TrustLaunchDirs,
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
			app.logViewer = cfg.Log.Viewer
			app.processViewer = cfg.Log.ProcessViewer
			app.configStack = stack
			app.env = env
			app.configSources = report.Sources
			app.keys = cfg.Keybindings
			app.defaultHarness = cfg.DefaultHarness
			app.maxAgents = cfg.Usage.MaxAgents
			app.maxChildren = cfg.Usage.MaxChildren
			app.autoCompaction = cfg.Usage.AutoCompactionByRole()

			app.tmux = tmux.NewClient(env.TmuxSocket)

			app.popup = ui.NewPopupWindow(app.tmux, agents.ManagerSession)
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

			return nil
		},
	}

	root.PersistentFlags().StringArrayVar(&configPaths, "config", nil, "path to a config TOML, repeatable, later files win over earlier ones")
	root.PersistentFlags().StringVar(&engineName, "engine", "", "engine to reach, named after a file in the config's engines directory, layered over the config")

	root.AddCommand(
		doctorCmd(app),
		panelCmd(app),
		openCmd(app),
		attachCmd(app),
		hideCmd(app),
		shownCmd(app),
		tmuxCmd(app),
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
	)

	return root
}

var commandsSurvivingBadConfig = map[string]bool{
	"doctor":  true,
	"version": true,
	"panel":   true,
}

func resolveConfigStack(flags []string, engine string) (config.Stack, error) {
	paths := flags
	if len(paths) == 0 {
		paths = config.LayerPaths()
	}

	if engine != "" {
		if _, err := config.EnginePath(engine); err != nil {
			return config.Stack{}, err
		}
	}

	return config.Stack{Paths: paths, Engine: engine}, nil
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
		EngineTmuxSocket:    cfg.Connection.EngineTmuxSocket,
		PanelKey:            cfg.Connection.PanelKey,
		Exec:                cfg.Connection.Exec,
		Forward:             cfg.Connection.Forward,
		EngineUser:          cfg.Connection.EngineUser,
	})
}

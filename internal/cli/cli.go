package cli

import (
	"fmt"
	"log/slog"

	"weave/config"
	"weave/internal/agents"
	"weave/internal/harness"
	"weave/internal/harness/claude"
	"weave/internal/harness/codex"
	"weave/internal/logging"
	"weave/internal/tmux"
	"weave/internal/ui"

	"github.com/spf13/cobra"
)

// NewRootCommand builds the weave command tree. Running it with no
// subcommand opens the panel.
func NewRootCommand() *cobra.Command {
	app := &app{}

	var configPaths []string

	root := &cobra.Command{
		Use:   "weave",
		Short: "tmux panel and CLI for background coding agents",
		Long: "Weave runs coding agents in tmux panes and keeps track of them.\n\n" +
			"There are two processes. `weave serve` is the long-lived service that owns the\n" +
			"database, answers the API and the MCP endpoint, and watches every pane. The panel\n" +
			"is a client of it, which is why it can be closed and reopened without stopping\n" +
			"anything. Every other command here is a client of the same service.\n\n" +
			"The panel is normally reached through the tmux key binding rather than by name,\n" +
			"and the commands below are for the times something has to be done by hand: check\n" +
			"the machine, start or stop the service, or call an endpoint from a script.",
		Args:          cobra.NoArgs,
		SilenceUsage:  true,
		SilenceErrors: true,
		PersistentPreRunE: func(cmd *cobra.Command, _ []string) error {
			configPaths = resolveConfigPaths(configPaths)

			cfg, report, err := config.LoadWithReport(configPaths)
			if err != nil {
				if isConfigFatal(cmd) {
					return err
				}

				app.configErr = err
				cfg = config.Defaults()
				report = &config.Report{Sources: map[string]config.Source{}}
			}

			env := config.LoadEnvironment(config.Environment{
				DatabaseURL:         cfg.Connection.DatabaseURL,
				ReadOnlyDatabaseURL: cfg.Connection.ReadOnlyDatabaseURL,
				Addr:                cfg.Connection.Addr,
				TmuxSocket:          cfg.Connection.TmuxSocket,
				CacheDir:            cfg.Connection.CacheDir,
			})

			codexHarness := codex.NewHarness(codex.NewHarnessParams{
				TrustLaunchDirs: cfg.Harness.Codex.TrustLaunchDirs,
			})
			claudeHarness := claude.NewHarness(claude.NewHarnessParams{
				ProjectsDir: claude.DefaultProjectsDir(),
			})

			harnessRegistry, err := harness.NewRegistry(claudeHarness, codexHarness)
			if err != nil {
				return fmt.Errorf("failed to create registry: %w", err)
			}
			if _, err := harnessRegistry.GetByID(cfg.DefaultHarness); err != nil && app.configErr == nil {
				app.configErr = fmt.Errorf("default_harness: %w", err)
			}
			app.harnesses = harnessRegistry

			app.tags = cfg.Tags
			app.layout = cfg.Layout
			app.logLevel = cfg.Log.Level
			app.logViewer = cfg.Log.Viewer
			app.processViewer = cfg.Log.ProcessViewer
			app.configPaths = configPaths
			app.env = env
			app.configSources = report.Sources
			app.keys = cfg.Keybindings
			app.defaultHarness = cfg.DefaultHarness
			app.maxAgents = cfg.MaxAgents

			manager := tmux.NewClient(env.TmuxSocket)
			app.host = tmux.NewHost(manager, env.Tmux)
			app.tmux = tmux.NewServers(manager, app.host)

			app.popup = ui.NewPopupWindow(app.tmux, agents.ManagerSession)
			contextPath, err := cfg.GetContextFile()
			if err != nil {
				ctx := cmd.Context()

				logging.From(ctx).LogAttrs(ctx, slog.LevelDebug, "failed to resolve the context file", logging.Err(err)...)
			}

			app.contextPath = contextPath
			app.defaultFlags = map[string]string{
				claudeHarness.GetID(): cfg.Harness.Claude.Flags,
				codexHarness.GetID():  cfg.Harness.Codex.Flags,
			}

			return nil
		},
	}

	root.PersistentFlags().StringArrayVar(&configPaths, "config", nil, "path to a config TOML, repeatable, later files win over earlier ones")

	root.AddCommand(
		doctorCmd(app),
		panelCmd(app),
		openCmd(app),
		attachCmd(app),
		hideCmd(app),
		tmuxCmd(app),
		serveCmd(app),
		startCmd(app),
		stopCmd(app),
		versionCmd(app),
		migrateCmd(app),
		readonlyRoleCmd(app),
		rpcCmd(app),
		hookCmd(app),
		traceCmd(app),
		paletteCmd(app),
		stageCmd(app),
	)

	return root
}

var commandsSurvivingBadConfig = map[string]bool{
	"doctor":  true,
	"version": true,
	"panel":   true,
}

func resolveConfigPaths(flags []string) []string {
	if len(flags) > 0 {
		return flags
	}

	return config.LayerPaths()
}

func isConfigFatal(cmd *cobra.Command) bool {
	return !commandsSurvivingBadConfig[cmd.Name()]
}

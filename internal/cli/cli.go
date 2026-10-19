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
	"komrad/internal/ui/paint"
	"komrad/internal/version"

	"github.com/spf13/cobra"
)

// IsQuiet reports whether an error leaves main nothing to print.
func IsQuiet(err error) bool {
	return errors.Is(err, errLogged) || errors.Is(err, errReported)
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
		Short: "Run, search and schedule Claude Code and Codex agents from tmux",
		Long: `Komrad runs coding agents in tmux panes and keeps track of them.

There are two processes. ` + "`komrad serve`" + ` is the long-lived server that owns the
database, answers the API and the MCP endpoint, and watches every pane. The panel
is a client of it, which is why it can be closed and reopened without stopping
anything. Every other command here is a client of the same service.

` + "`komrad open`" + ` brings the panel up in this terminal, and the other commands are
for the times something has to be done by hand: check the machine, start or stop
the server, or call an endpoint from a script.`,
		Version:       version.Describe(version.Version(), version.Commit()),
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
				cfg, report = fallbackConfig()
			}

			env, err := loadEnvironment(cfg)
			if err != nil {
				return err
			}

			if err := requireEmbeddedPostgres(cmd.Context(), cmd, env); err != nil {
				return err
			}

			codexHarness := codex.NewHarness(codex.NewHarnessParams{
				Home:     codex.NamedHome(),
				CacheDir: env.CacheDir,
			})
			claudeHarness := claude.NewHarness(claude.NewHarnessParams{
				ConfigDir: claude.NamedConfigDir(),
				CacheDir:  env.CacheDir,
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
			app.gitConfig = cfg.Git
			app.logLevel = cfg.Log.Level
			app.configStack = stack
			app.env = env
			app.configSources = report.Sources
			app.keys = cfg.Keybindings
			app.vimMode = cfg.VimMode
			app.diffWrap = cfg.Diff.Wrap
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
			palette, err := paint.NewPalette(cfg.Palette.Colors())
			if err != nil {
				if app.configErr == nil {
					app.configErr = err
				}
				palette = &paint.Panel
			}
			paint.Use(*palette)
			app.defaultHarness = cfg.DefaultHarness
			app.autoSetup = cfg.AutoSetup
			app.usage = cfg.Usage
			app.signIn = cfg.SignIn
			app.forgeProxy = cfg.ForgeProxy
			app.controlPlane = cfg.ControlPlane

			app.tmux = tmux.NewClient(env.TmuxSocket)

			app.session = ui.NewSession(ui.NewSessionParams{
				Tmux:        app.tmux,
				Session:     agents.ManagerSession,
				LeaderKey:   tmux.LeaderKey,
				LeaderTable: tmux.LeaderTable,
			})
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
			app.debug = cfg.Debug

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
		gitlabCmd(app),
		forgeCmd(app),
		prCmd(app),
		serveCmd(app),
		startCmd(app),
		stopCmd(app),
		versionCmd(app),
		migrateCmd(app),
		engineCmd(app),
		controlPlaneCmd(app),
		loginCmd(app),
		logoutCmd(app),
		tokenCmd(app),
		rpcCmd(app),
		hookCmd(app),
		logCmd(app),
		shellCmd(app),
		sshProxyCmd(app),
		terminalCmd(app),
		visualCmd(app),
	)

	return root
}

const survivesBadConfig = "komrad.survives-bad-config"

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
	for current := cmd; current != nil; current = current.Parent() {
		if _, ok := current.Annotations[survivesBadConfig]; ok {
			return false
		}
	}

	return true
}

func fallbackConfig() (*config.Config, *config.Report) {
	cfg, report, err := config.LoadDefaults()
	if err != nil {
		return config.Defaults(), &config.Report{Sources: map[string]config.Source{}}
	}

	return cfg, report
}

func loadEnvironment(cfg *config.Config) (*config.Environment, error) {
	return config.LoadEnvironment(config.Environment{
		DatabaseURL:           cfg.Connection.DatabaseURL,
		ReadOnlyDatabaseURL:   cfg.Connection.ReadOnlyDatabaseURL,
		OwnerDatabaseURL:      cfg.Connection.OwnerDatabaseURL,
		Addr:                  cfg.Connection.Addr,
		TmuxSocket:            cfg.Connection.TmuxSocket,
		CacheDir:              cfg.Connection.CacheDir,
		StateDir:              cfg.Connection.StateDir,
		EmbeddedPostgres:      cfg.Connection.UsesEmbeddedPostgres(),
		OwnerEmbeddedPostgres: cfg.Connection.OwnerUsesEmbeddedPostgres(),
		URL:                   cfg.Connection.URL,
	})
}

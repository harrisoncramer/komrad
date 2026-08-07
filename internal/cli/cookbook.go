package cli

import (
	"context"
	"encoding/json"
	"fmt"
	"path/filepath"

	"weave/config"
	"weave/internal/caller"
	"weave/internal/cookbooks"
	"weave/internal/rpcapi"

	"github.com/spf13/cobra"
)

// builtinCookbooksSubdir is where InstallBuiltins writes the cookbooks this build ships,
// under the cache directory, named for what puts it there the way the built-in actions
// directory is.
const builtinCookbooksSubdir = "weave-builtin-cookbooks"

// cookbookCmd is the parent of the cookbook subcommands.
func cookbookCmd(state *app) *cobra.Command {
	cmd := &cobra.Command{
		Use:   "cookbook",
		Short: "Install a workflow, its actions, and its schedule from a packaged recipe",
		Long: "A cookbook is a directory holding a cookbook.toml manifest and the action scripts\n" +
			"it names. Installing one writes those scripts into your actions directory and calls\n" +
			"the same create_workflow endpoint the panel's w key and the MCP tools already call,\n" +
			"and create_schedule too when the cookbook arms one, so installing a cookbook is not a\n" +
			"new code path.\n\n" +
			"weave ships a few built in; list shows those plus anything in your own directory,\n" +
			"configured under [cookbooks] in your config.",
		Args:          cobra.NoArgs,
		SilenceUsage:  true,
		SilenceErrors: true,
	}

	cmd.AddCommand(cookbookListCmd(state), cookbookInstallCmd(state))

	return cmd
}

// cookbookListCmd lists every cookbook this build ships plus whatever the user's own
// directory holds.
func cookbookListCmd(state *app) *cobra.Command {
	cmd := &cobra.Command{
		Use:           "list",
		Short:         "List the available cookbooks, built in and your own",
		Args:          cobra.NoArgs,
		SilenceUsage:  true,
		SilenceErrors: true,
		RunE: func(cmd *cobra.Command, _ []string) error {
			found, err := listCookbooks(state)
			if err != nil {
				return err
			}

			if len(found) == 0 {
				_, err := fmt.Fprintln(cmd.OutOrStdout(), "no cookbooks found")

				return err
			}

			for _, cookbook := range found {
				if _, err := fmt.Fprintf(cmd.OutOrStdout(), "%-20s %s\n", cookbook.Name, cookbook.Manifest.Description); err != nil {
					return err
				}
			}

			return nil
		},
	}

	return cmd
}

// cookbookInstallCmd installs one cookbook's actions, workflow, and schedule.
func cookbookInstallCmd(state *app) *cobra.Command {
	var cwd string

	cmd := &cobra.Command{
		Use:           "install <name>",
		Short:         "Install a cookbook's actions, workflow, and schedule",
		Args:          cobra.ExactArgs(1),
		SilenceUsage:  true,
		SilenceErrors: true,
		RunE: func(cmd *cobra.Command, args []string) error {
			if err := requireServer(state.env.Addr); err != nil {
				return err
			}

			installed, err := installCookbook(cmd.Context(), state, args[0], cwd)
			if err != nil {
				return err
			}

			return installed.print(cmd)
		},
	}

	cmd.Flags().StringVar(&cwd, "cwd", "", "override the cookbook's default working directory")

	return cmd
}

// installedCookbook is what installing one produced, kept apart from the install itself so
// the command's RunE reads as plan-then-report rather than interleaving the two.
type installedCookbook struct {
	workflowName string
	workflowID   string
	actions      []string
	scheduleName string
}

// print reports what installing a cookbook produced.
func (i installedCookbook) print(cmd *cobra.Command) error {
	out := cmd.OutOrStdout()

	if _, err := fmt.Fprintf(out, "installed workflow %q (%s)\n", i.workflowName, i.workflowID); err != nil {
		return err
	}

	for _, name := range i.actions {
		if _, err := fmt.Fprintf(out, "installed action %q\n", name); err != nil {
			return err
		}
	}

	if i.scheduleName == "" {
		return nil
	}

	_, err := fmt.Fprintf(out, "installed schedule %q\n", i.scheduleName)

	return err
}

// installCookbook writes a cookbook's action scripts into the actions directory and creates
// its workflow and, if it arms one, its schedule over the same endpoints the panel and the
// MCP tools use.
func installCookbook(ctx context.Context, state *app, name, cwdOverride string) (installedCookbook, error) {
	cfg, err := config.Load(state.configPaths)
	if err != nil {
		return installedCookbook{}, err
	}

	actionsDir, err := cfg.GetActionsDir()
	if err != nil {
		return installedCookbook{}, err
	}

	userDir, builtinDir, err := cookbookDirs(state, cfg)
	if err != nil {
		return installedCookbook{}, err
	}

	found, err := cookbooks.Find(name, userDir, builtinDir)
	if err != nil {
		return installedCookbook{}, err
	}

	installedActions, err := found.InstallActions(actionsDir)
	if err != nil {
		return installedCookbook{}, err
	}

	ctx, cancel := context.WithTimeout(ctx, rpcTimeout)
	defer cancel()

	from := caller.Caller{AgentID: state.env.AgentID, SessionID: state.env.SessionID}

	workflowID, err := createCookbookWorkflow(ctx, state.env.Addr, from, found.Manifest.Workflow, cwdOverride)
	if err != nil {
		return installedCookbook{}, err
	}

	result := installedCookbook{
		workflowName: found.Manifest.Workflow.Name,
		workflowID:   workflowID,
		actions:      installedActions,
	}

	if found.Manifest.Schedule != nil {
		if err := createCookbookSchedule(ctx, state.env.Addr, from, *found.Manifest.Schedule, workflowID); err != nil {
			return installedCookbook{}, err
		}

		result.scheduleName = found.Manifest.Schedule.Name
	}

	return result, nil
}

// listCookbooks is every cookbook this build ships plus whatever the user's own directory
// holds, the user's own winning on a name collision.
func listCookbooks(state *app) ([]cookbooks.Cookbook, error) {
	cfg, err := config.Load(state.configPaths)
	if err != nil {
		return nil, err
	}

	userDir, builtinDir, err := cookbookDirs(state, cfg)
	if err != nil {
		return nil, err
	}

	return cookbooks.List(userDir, builtinDir)
}

// cookbookDirs is the two directories a cookbook may be found in: the user's configured one
// and this build's own, extracted fresh so an older build's cookbook does not stay on offer.
func cookbookDirs(state *app, cfg *config.Config) (userDir, builtinDir string, err error) {
	userDir, err = cfg.GetCookbooksDir()
	if err != nil {
		return "", "", err
	}

	builtinDir = filepath.Join(state.env.CacheDir, builtinCookbooksSubdir)
	if err = cookbooks.InstallBuiltins(builtinDir); err != nil {
		return "", "", err
	}

	return userDir, builtinDir, nil
}

// createCookbookWorkflow calls create_workflow the way rpc create-workflow would, and
// returns the id of what was created.
func createCookbookWorkflow(ctx context.Context, addr string, from caller.Caller, spec cookbooks.WorkflowSpec, cwdOverride string) (string, error) {
	method, err := findRPCMethod("create_workflow")
	if err != nil {
		return "", err
	}

	cwd := spec.Cwd
	if cwdOverride != "" {
		cwd = cwdOverride
	}

	request := map[string]any{
		"name":          spec.Name,
		"context":       spec.Context,
		"cwd":           cwd,
		"flags":         spec.Flags,
		"harness":       spec.Harness,
		"setupAction":   spec.SetupAction,
		"cleanupAction": spec.CleanupAction,
	}

	payload, err := callRPC(ctx, callRPCParams{Addr: addr, Path: method.GetPath(), From: from, Request: request})
	if err != nil {
		return "", err
	}

	var response struct {
		Workflow struct {
			ID string `json:"id"`
		} `json:"workflow"`
	}
	if err := json.Unmarshal(payload, &response); err != nil {
		return "", fmt.Errorf("failed to read the created workflow: %w", err)
	}

	return response.Workflow.ID, nil
}

// createCookbookSchedule calls create_schedule the way rpc create-schedule would, arming the
// schedule a cookbook's manifest describes against the workflow it just created.
func createCookbookSchedule(ctx context.Context, addr string, from caller.Caller, spec cookbooks.ScheduleSpec, workflowID string) error {
	method, err := findRPCMethod("create_schedule")
	if err != nil {
		return err
	}

	request := map[string]any{
		"name":       spec.Name,
		"cron":       spec.Cron,
		"timezone":   spec.Timezone,
		"prompt":     spec.Prompt,
		"enabled":    spec.Enabled,
		"hideAgents": spec.HideAgents,
		"workflowId": workflowID,
	}

	_, err = callRPC(ctx, callRPCParams{Addr: addr, Path: method.GetPath(), From: from, Request: request})

	return err
}

// findRPCMethod is the exposed RPC named toolName, the same lookup rpc.go builds one
// subcommand per method from.
func findRPCMethod(toolName string) (rpcapi.Method, error) {
	for method := range rpcapi.GetMethods() {
		if method.Annotation.GetToolName() == toolName {
			return method, nil
		}
	}

	return rpcapi.Method{}, fmt.Errorf("no exposed rpc named %q", toolName)
}

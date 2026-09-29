package scriptrun

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"

	"komrad/config"
	"komrad/internal/scripts"
	"komrad/internal/store"
	"komrad/lib/xstr"

	agentstore "komrad/internal/store/agent"
	workflowstore "komrad/internal/store/workflow"
)

const maxScriptPrompt = 16 * 1024

type configLoader func() (*config.Config, error)

type scriptPayload struct {
	ID              string `json:"id"`
	Name            string `json:"name"`
	State           string `json:"state"`
	Alive           bool   `json:"alive"`
	Cwd             string `json:"cwd"`
	Harness         string `json:"harness"`
	SessionID       string `json:"session_id"`
	Flags           string `json:"flags"`
	Prompt          string `json:"prompt"`
	PromptTruncated bool   `json:"prompt_truncated"`
	Pane            string `json:"pane"`
	Workflow        string `json:"workflow"`
	Hidden          bool   `json:"hidden"`
	CreatedAt       string `json:"created_at"`
}

type scriptEnvParams struct {
	agent   agentstore.Agent
	dir     string
	trigger string
	script  string
}

// ListHooks returns the available hooks in name order, the order they run in on a moment that
// holds several.
func (s *ScriptService) ListHooks() ([]scripts.Script, error) {
	dir, err := s.hooksDir()
	if err != nil {
		return nil, err
	}

	return scripts.List(dir)
}

func (s *ScriptService) find(name string) (scripts.Script, error) {
	return scripts.Find(name, s.builtinDir)
}

// FindHook is the hook named name in the hooks directory or among the hooks komrad ships.
func (s *ScriptService) FindHook(name string) (scripts.Script, error) {
	dir, err := s.hooksDir()
	if err != nil {
		return scripts.Script{}, err
	}

	written, err := scripts.Find(name, dir)
	if err != nil && !errors.Is(err, scripts.ErrScriptNotFound) {
		return scripts.Script{}, err
	}

	shipped, ships, shippedErr := s.findShippedHook(name)
	if shippedErr != nil {
		return scripts.Script{}, shippedErr
	}

	switch {
	case err == nil && ships && written.Path != shipped.Path:
		return scripts.Script{}, fmt.Errorf("%w: %q", ErrHookNameTaken, name)
	case err == nil:
		return written, nil
	case ships:
		return shipped, nil
	}

	return scripts.Script{}, err
}

func (s *ScriptService) findShippedHook(name string) (scripts.Script, bool, error) {
	if strings.HasPrefix(name, "_") {
		return scripts.Script{}, false, nil
	}

	shipped, err := scripts.Find(name, s.builtinDir)
	if errors.Is(err, scripts.ErrScriptNotFound) {
		return scripts.Script{}, false, nil
	}
	if err != nil {
		return scripts.Script{}, false, err
	}

	return shipped, true, nil
}

func (s *ScriptService) hooksDir() (string, error) {
	cfg, err := s.load()
	if err != nil {
		return "", fmt.Errorf("failed to read the config: %w", err)
	}

	return cfg.GetHooksDir()
}

func (s *ScriptService) env(ctx context.Context, params scriptEnvParams) ([]string, error) {
	agent, dir, trigger := params.agent, params.dir, params.trigger

	workflow, err := s.workflows.GetIncludingArchived(ctx, agent.WorkflowID)
	if err != nil && !errors.Is(err, store.ErrNotFound) {
		return nil, fmt.Errorf("failed to read workflow %q: %w", agent.WorkflowID, err)
	}

	payload := scriptPayload{
		ID:        agent.ID,
		Name:      agent.Name,
		State:     string(agent.State),
		Alive:     isPaneOwnedBy(s.tmux.GetPaneOwner(agent.Pane), agent.ID),
		Cwd:       agent.Cwd,
		Harness:   agent.Harness,
		SessionID: agent.SessionID,
		Flags:     agent.Flags,
		Pane:      agent.Pane,
		Workflow:  workflowName(workflow),
		Hidden:    agent.Hidden,
	}
	payload.Prompt, payload.PromptTruncated = xstr.Head(agent.Prompt, maxScriptPrompt)
	if !agent.CreatedAt.IsZero() {
		payload.CreatedAt = agent.CreatedAt.Format(time.RFC3339)
	}

	encoded, err := json.Marshal(payload)
	if err != nil {
		return nil, fmt.Errorf("failed to encode agent %q: %w", agent.ID, err)
	}

	env := []string{
		"KOMRAD_AGENT=" + string(encoded),
		"KOMRAD_AGENT_ID=" + agent.ID,
		"KOMRAD_AGENT_CWD=" + agent.Cwd,
		"KOMRAD_ADDR=" + s.addr,
		"KOMRAD_TMUX_SOCKET=" + s.socket,
		"KOMRAD_SCRIPT_DIR=" + dir,
		"KOMRAD_TRIGGER=" + trigger,
		"KOMRAD_SCRIPT=" + params.script,
		"KOMRAD_INFO_PANE=" + s.tmux.GetInfoPane(),
		"KOMRAD_HOST_TMUX_SOCKET=" + s.tmux.GetHostSocket(),
		"EDITOR=" + s.editor,
		"KOMRAD_SHELL=" + s.tmux.GetDefaultShell(),
	}

	if s.binPath != "" {
		existing, _ := config.LookupEnv("PATH")
		env = append(env, "PATH="+filepath.Dir(s.binPath)+string(os.PathListSeparator)+existing)
	}

	env = append(env, gitPromptGuardEnv()...)

	// An agent that provisions nothing is not handed KOMRAD_WORKTREE_REPO.
	if workflow != nil && workflow.Worktree.Enabled && !agent.IsProvisionerDisabled() {
		worktreeEnv, err := s.worktreeEnv(*workflow, agent)
		if err != nil {
			return nil, err
		}

		env = append(env, worktreeEnv...)
	}

	return env, nil
}

func gitPromptGuardEnv() []string {
	guard := []string{
		"GIT_TERMINAL_PROMPT=0",
		"GCM_INTERACTIVE=never",
	}

	if _, set := config.LookupEnv("GIT_ASKPASS"); !set {
		guard = append(guard, "GIT_ASKPASS=")
	}
	if _, set := config.LookupEnv("SSH_ASKPASS"); !set {
		guard = append(guard, "SSH_ASKPASS=")
	}

	return guard
}

func workflowName(workflow *workflowstore.Workflow) string {
	if workflow == nil {
		return ""
	}

	return workflow.Name
}

func (s *ScriptService) worktreeEnv(workflow workflowstore.Workflow, agent agentstore.Agent) ([]string, error) {
	loaded, err := s.load()
	if err != nil {
		return nil, fmt.Errorf("failed to read the config: %w", err)
	}

	cfg := workflow.Worktree

	// The configured directory is shared by every workflow, so a repository that expects its
	// worktrees in a particular place is served by the workflow naming that place itself.
	worktreesDir := config.ExpandHome(cfg.Dir)
	if worktreesDir == "" {
		worktreesDir, err = loaded.GetWorktreesDir()
		if err != nil {
			return nil, err
		}
	}

	return []string{
		"KOMRAD_WORKTREE_REPO=" + config.ExpandHome(strings.TrimSpace(workflow.Cwd)),
		"KOMRAD_WORKTREES_DIR=" + worktreesDir,
		"KOMRAD_WORKTREE_BASE_BRANCH=" + cfg.BaseBranch,
		"KOMRAD_WORKTREE_COPY_PATHS=" + strings.Join(cfg.CopyPaths, "\n"),
		"KOMRAD_WORKTREE_SYMLINK_PATHS=" + strings.Join(cfg.SymlinkPaths, "\n"),
		"KOMRAD_WORKTREE_ADOPT_BRANCH=" + agent.AdoptedBranch,
	}, nil
}

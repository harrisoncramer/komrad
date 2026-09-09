package agents

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"

	"weave/config"
	"weave/internal/scripts"
	"weave/internal/store"
	"weave/lib/xstr"

	agentstore "weave/internal/store/agent"
	workflowstore "weave/internal/store/workflow"
)

const maxScriptPrompt = 16 * 1024

type configLoader func() (*config.Config, error)

type scriptPayload struct {
	ID              string            `json:"id"`
	Name            string            `json:"name"`
	State           string            `json:"state"`
	Alive           bool              `json:"alive"`
	Cwd             string            `json:"cwd"`
	Harness         string            `json:"harness"`
	SessionID       string            `json:"session_id"`
	Flags           string            `json:"flags"`
	Prompt          string            `json:"prompt"`
	PromptTruncated bool              `json:"prompt_truncated"`
	Pane            string            `json:"pane"`
	Workflow        string            `json:"workflow"`
	Hidden          bool              `json:"hidden"`
	CreatedAt       string            `json:"created_at"`
	Metadata        map[string]string `json:"metadata"`
}

type scriptEnvParams struct {
	agent   agentstore.Agent
	dir     string
	trigger string
	script  string
}

// ListHooks returns the available hooks in name order, which is the order they run in on a
// moment that holds several.
func (s *ScriptService) ListHooks() ([]scripts.Script, error) {
	dirs, err := s.hookDirs()
	if err != nil {
		return nil, err
	}

	return scripts.List(dirs...)
}

func (s *ScriptService) find(name string) (scripts.Script, error) {
	return scripts.Find(name, s.builtinDirs()...)
}

func (s *ScriptService) findHook(name string) (scripts.Script, error) {
	dirs, err := s.hookDirs()
	if err != nil {
		return scripts.Script{}, err
	}

	return scripts.Find(name, dirs...)
}

func (s *ScriptService) hookDirs() ([]string, error) {
	cfg, err := s.load()
	if err != nil {
		return nil, fmt.Errorf("failed to read the config: %w", err)
	}

	dir, err := cfg.GetHooksDir()
	if err != nil {
		return nil, err
	}

	return []string{dir}, nil
}

// builtinDirs is where the scripts weave ships live, which is the only place a script that is
// not a hook comes from.
func (s *ScriptService) builtinDirs() []string {
	return []string{s.builtinDir}
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
		Metadata:  make(map[string]string, len(agent.Metadata)),
	}
	payload.Prompt, payload.PromptTruncated = xstr.Head(agent.Prompt, maxScriptPrompt)
	if !agent.CreatedAt.IsZero() {
		payload.CreatedAt = agent.CreatedAt.Format(time.RFC3339)
	}
	for _, entry := range agent.Metadata {
		payload.Metadata[entry.Key] = entry.Value
	}

	encoded, err := json.Marshal(payload)
	if err != nil {
		return nil, fmt.Errorf("failed to encode agent %q: %w", agent.ID, err)
	}

	env := []string{
		"WEAVE_AGENT=" + string(encoded),
		"WEAVE_AGENT_ID=" + agent.ID,
		"WEAVE_AGENT_CWD=" + agent.Cwd,
		"WEAVE_ADDR=" + s.addr,
		"WEAVE_TMUX_SOCKET=" + s.socket,
		"WEAVE_SCRIPT_DIR=" + dir,
		"WEAVE_TRIGGER=" + trigger,
		"WEAVE_SCRIPT=" + params.script,
		"WEAVE_INFO_PANE=" + s.tmux.GetInfoPane(),
		"WEAVE_HOST_TMUX_SOCKET=" + s.tmux.GetHostSocket(),
		"EDITOR=" + s.editor,
		"WEAVE_SHELL=" + s.tmux.GetDefaultShell(),
	}

	if s.binPath != "" {
		existing, _ := config.LookupEnv("PATH")
		env = append(env, "PATH="+filepath.Dir(s.binPath)+string(os.PathListSeparator)+existing)
	}

	env = append(env, gitPromptGuardEnv()...)

	// An agent that provisions nothing keeps the directory it was given, so its hooks must not
	// be handed WEAVE_WORKTREE_REPO pointing at a worktree nobody made for it.
	if workflow != nil && workflow.Worktree.Enabled && !isProvisionerDisabled(agent.ProvisionEnabled) {
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
		"WEAVE_WORKTREE_REPO=" + config.ExpandHome(strings.TrimSpace(workflow.Cwd)),
		"WEAVE_WORKTREES_DIR=" + worktreesDir,
		"WEAVE_WORKTREE_BASE_BRANCH=" + cfg.BaseBranch,
		"WEAVE_WORKTREE_COPY_PATHS=" + strings.Join(cfg.CopyPaths, "\n"),
		"WEAVE_WORKTREE_SYMLINK_PATHS=" + strings.Join(cfg.SymlinkPaths, "\n"),
		"WEAVE_WORKTREE_ADOPT_BRANCH=" + agent.AdoptedBranch,
	}, nil
}

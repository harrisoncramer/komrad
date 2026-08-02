package agents

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"os"
	"time"
	"unicode/utf8"

	"weave/internal/actions"
	"weave/internal/appconfig"
	"weave/internal/logging"

	agentstore "weave/internal/store/agent"
)

// actionReader is the part of the agent store this service reads. An action changes
// nothing about the agent it fires on, so this never writes.
type actionReader interface {
	GetByID(ctx context.Context, id string) (*agentstore.Agent, error)
}

// actionRunner finds actions and starts them. RunAndWait is for a caller with something
// downstream of the action, and Run for one with nothing waiting.
type actionRunner interface {
	Discover(dir string) ([]actions.Action, error)
	Run(p actions.RunParams) error
	RunAndWait(ctx context.Context, p actions.RunParams) (actions.Result, error)
}

// actionTmux is the one thing this asks of tmux, which agent a pane belongs to, which is
// what says whether the agent an action is fired on is running.
type actionTmux interface {
	PaneOwner(pane string) string
}

// configLoader reads the user's config. The service holds the loader rather than a
// parsed config so a directory changed while the server runs takes effect without a
// restart.
type configLoader func() (*appconfig.Config, error)

// ErrActionNotFound reports that the actions directory holds no file by that name.
var ErrActionNotFound = errors.New("action not found")

// What set an action off, passed to it in WEAVE_TRIGGER, which is how a script tells
// apart the moments it can be reached at and refuses the ones it does not handle.
//
// The contract differs under TriggerSetup: a launch is waiting, so the exit status decides
// whether the agent starts and stdout is read as the directory it runs in. Under the other
// two nothing is waiting and neither is read.
const (
	// TriggerManual is a person pressing the action key or a model calling run_action.
	TriggerManual = "manual"
	// TriggerSetup is a workflow's setup action, before one of its agents starts.
	TriggerSetup = "setup"
	// TriggerCleanup is a workflow's cleanup action, once one of its agents is deleted.
	TriggerCleanup = "cleanup"
)

// ActionService lists and fires the executables the user keeps in their actions
// directory. The WEAVE_AGENT payload is built here and nowhere else, so the panel firing
// an action and a model firing one hand the script the same thing.
type ActionService struct {
	agents actionReader
	// workflows is read for the name of the workflow an agent belongs to, which a script
	// can use where the id it is stored under says nothing.
	workflows storedWorkflowReader
	tmux      actionTmux
	runner    actionRunner
	load      configLoader
	// addr and socket are what an action reaches back through, the address of this server
	// and the tmux server every agent's pane lives on.
	addr   string
	socket string
}

// ActionParams carries the action service's dependencies.
type ActionParams struct {
	Agents    actionReader
	Workflows storedWorkflowReader
	Tmux      actionTmux
	Runner    actionRunner
	Load      configLoader
	Addr      string
	Socket    string
}

// NewActionService builds the service from its dependencies.
func NewActionService(p ActionParams) *ActionService {
	return &ActionService{
		agents:    p.Agents,
		workflows: p.Workflows,
		tmux:      p.Tmux,
		runner:    p.Runner,
		load:      p.Load,
		addr:      p.Addr,
		socket:    p.Socket,
	}
}

// maxActionPreview bounds how much of an action is read for the preview, since an action
// can be a compiled program of any size and a caller renders whatever it is given.
const maxActionPreview = 8 * 1024

// maxActionPrompt bounds how much of an agent's prompt the payload carries. Everything an
// exec is handed shares one kernel limit, and the prompt is the only field here with no
// bound of its own, so a long enough one would fail the run at exec.
const maxActionPrompt = 16 * 1024

// ListedAction is an action with the start of its own text, which is what a caller
// showing one needs.
type ListedAction struct {
	actions.Action
	// Preview is the head of the file, empty for one that could not be read.
	Preview []byte
	// Truncated says the file is longer than Preview carries.
	Truncated bool
}

// ListActions is every file in the actions directory, in name order, each with the start
// of its contents. A file that cannot be read is listed with an empty preview rather than
// dropped, since it is still there and still nameable.
func (s *ActionService) ListActions() ([]ListedAction, error) {
	_, found, err := s.discover()
	if err != nil {
		return nil, err
	}

	listed := make([]ListedAction, 0, len(found))
	for _, action := range found {
		head, truncated, err := readHead(action.Path, maxActionPreview)
		if err != nil {
			listed = append(listed, ListedAction{Action: action})

			continue
		}

		listed = append(listed, ListedAction{Action: action, Preview: head, Truncated: truncated})
	}

	return listed, nil
}

// RunAction fires an action on an agent and returns once the process exists. Nothing
// waits for it, so the only failures reported are the ones before it starts: a missing
// agent or action, and a file that will not execute. What it did afterwards reaches the log.
//
// It runs in the actions directory rather than the agent's own, since an action that
// removes its agent's directory cannot be standing in it, and an agent whose directory is
// already gone is exactly the one still wanting an action. The agent's directory is in
// the payload for a script that wants to cd there itself.
func (s *ActionService) RunAction(ctx context.Context, agentID, name string) error {
	agent, err := s.agents.GetByID(ctx, agentID)
	if err != nil {
		return fmt.Errorf("failed to read agent %q: %w", agentID, err)
	}
	if agent == nil {
		return ErrAgentNotFound
	}

	action, dir, err := s.find(name)
	if err != nil {
		return err
	}

	env, err := s.env(ctx, *agent, dir, TriggerManual)
	if err != nil {
		return err
	}

	return s.start(ctx, startParams{
		action:  action,
		dir:     dir,
		env:     env,
		agent:   *agent,
		trigger: TriggerManual,
	})
}

// ErrSetupFailed reports that a workflow's setup action refused, so the agent was never
// launched. It is the one action failure something else is downstream of.
var ErrSetupFailed = errors.New("the setup action failed")

// setupTimeout bounds how long a setup action may take, since a launch waits on it. It is
// generous because the ordinary case is installing dependencies over a network.
const setupTimeout = 10 * time.Minute

// maxSetupDir bounds what a setup action may name as the directory to run in, matching
// the limit the same field carries on the wire.
const maxSetupDir = 4096

// RunSetupAction runs a workflow's setup action and does not return until it has
// finished. The directory the agent should run in is whatever the action trims to on
// stdout, ordinarily nothing. Progress belongs on stderr, so a script that chats on
// stdout is refused rather than taken at its word.
func (s *ActionService) RunSetupAction(ctx context.Context, agent agentstore.Agent, name string) (string, error) {
	action, actionsDir, err := s.find(name)
	if err != nil {
		return "", fmt.Errorf("%w: %w", ErrSetupFailed, err)
	}

	env, err := s.env(ctx, agent, actionsDir, TriggerSetup)
	if err != nil {
		return "", err
	}

	ctx, cancel := context.WithTimeout(ctx, setupTimeout)
	defer cancel()

	result, err := s.runner.RunAndWait(ctx, actions.RunParams{Action: action, Dir: actionsDir, Env: env})
	if err != nil {
		return "", fmt.Errorf("%w: %w%s", ErrSetupFailed, err, stderrDetail(result.Stderr))
	}

	answered := string(bytes.TrimSpace(result.Stdout))
	if len(answered) > maxSetupDir {
		return "", fmt.Errorf("%w: %s wrote %d bytes to stdout, where a directory to run in was expected. Progress belongs on stderr", ErrSetupFailed, name, len(answered))
	}

	return answered, nil
}

// RunCleanupAction fires a workflow's cleanup action and returns once the process exists,
// since nothing is downstream of a cleanup. The agent is taken as a value rather than
// looked up, so a cleanup can fire for an agent whose row has already gone.
func (s *ActionService) RunCleanupAction(ctx context.Context, agent agentstore.Agent, name string) error {
	action, dir, err := s.find(name)
	if err != nil {
		return err
	}

	env, err := s.env(ctx, agent, dir, TriggerCleanup)
	if err != nil {
		return err
	}

	return s.start(ctx, startParams{
		action:  action,
		dir:     dir,
		env:     env,
		agent:   agent,
		trigger: TriggerCleanup,
	})
}

// startParams is one action about to be fired, with the agent and the trigger its record
// is written against.
type startParams struct {
	action  actions.Action
	dir     string
	env     []string
	agent   agentstore.Agent
	trigger string
}

// start fires an action and arranges for what it did to reach the log.
func (s *ActionService) start(ctx context.Context, p startParams) error {
	logger := logging.From(ctx).With(logging.Attrs([]slog.Attr{
		slog.String(logging.ActionName, p.action.Name),
		slog.String(logging.ActionTrigger, p.trigger),
		slog.String(logging.ActionAgentID, p.agent.ID),
		slog.String(logging.ActionAgentCwd, p.agent.Cwd),
	})...)
	ctx = context.WithoutCancel(ctx)

	err := s.runner.Run(actions.RunParams{
		Action: p.action,
		Dir:    p.dir,
		Env:    p.env,
		Done: func(result actions.Result, err error) {
			if err != nil {
				logger.LogAttrs(ctx, slog.LevelError, "action failed", failedAction(result, err)...)

				return
			}

			logger.LogAttrs(ctx, slog.LevelDebug, "action finished", slog.Int(logging.ExitCode, result.ExitCode))
		},
	})
	if err != nil {
		return err
	}

	// Written once the process exists, so it never claims a run that never happened.
	logger.LogAttrs(ctx, slog.LevelDebug, "action started")

	return nil
}

// failedAction is what a failed run adds to its record, with the exit status pulled out of
// the error beside it, since one inside a wrapped sentence cannot be filtered on.
func failedAction(result actions.Result, err error) []slog.Attr {
	attrs := []slog.Attr{slog.Int(logging.ExitCode, result.ExitCode)}
	attrs = append(attrs, logging.Err(err)...)

	if said := bytes.TrimSpace(result.Stderr); len(said) > 0 {
		attrs = append(attrs, slog.String(logging.ActionStderr, string(said)))
	}

	return attrs
}

// stderrDetail renders an action's stderr for the end of a refusal, so what the script
// said about its own failure travels with the error rather than only reaching a log.
func stderrDetail(stderr []byte) string {
	trimmed := bytes.TrimSpace(stderr)
	if len(trimmed) == 0 {
		return ""
	}

	return ": " + string(trimmed)
}

// find is the action by name, with the directory it was found in. The name is matched
// against what the directory holds rather than joined onto its path, so a name that
// climbs out finds nothing instead of some executable elsewhere on the machine.
func (s *ActionService) find(name string) (actions.Action, string, error) {
	dir, found, err := s.discover()
	if err != nil {
		return actions.Action{}, "", err
	}

	for _, action := range found {
		if action.Name == name {
			return action, dir, nil
		}
	}

	return actions.Action{}, "", fmt.Errorf("%w: %q is not in %s", ErrActionNotFound, name, dir)
}

// discover is the actions directory and what it holds. The config is read here rather than
// held, so a directory changed while the server runs takes effect without a restart.
func (s *ActionService) discover() (string, []actions.Action, error) {
	cfg, err := s.load()
	if err != nil {
		return "", nil, fmt.Errorf("failed to read the config: %w", err)
	}

	dir, err := cfg.ActionsDir()
	if err != nil {
		return "", nil, err
	}

	found, err := s.runner.Discover(dir)
	if err != nil {
		return "", nil, err
	}

	return dir, found, nil
}

// actionPayload is what an action is told about the agent it was fired on, encoded into
// WEAVE_AGENT. It is a shape of its own rather than the wire format, so what a script
// parses is written down in one place and metadata arrives as an object it can index by
// key. The transcript is left out, since it is far past what an environment variable
// holds and a script that wants it has the agent's id and the server's address.
type actionPayload struct {
	ID        string `json:"id"`
	Name      string `json:"name"`
	State     string `json:"state"`
	Alive     bool   `json:"alive"`
	Cwd       string `json:"cwd"`
	Harness   string `json:"harness"`
	SessionID string `json:"session_id"`
	Flags     string `json:"flags"`
	Prompt    string `json:"prompt"`
	// PromptTruncated says the prompt is longer than what Prompt carries, so a script
	// showing it can say so rather than presenting a cut one as the whole.
	PromptTruncated bool              `json:"prompt_truncated"`
	Pane            string            `json:"pane"`
	Workflow        string            `json:"workflow"`
	Mark            string            `json:"mark"`
	Hidden          bool              `json:"hidden"`
	CreatedAt       string            `json:"created_at"`
	Metadata        map[string]string `json:"metadata"`
}

// env is the environment an action runs with, over the server's own. The agent goes in
// whole as JSON, beside the values a script reaches back through: this server's address,
// the socket of the tmux server the agent's pane lives on, and the actions directory.
//
// The socket matters because a script sending keys to the pane in the payload would
// otherwise address the user's own server, where that pane id means something else. The
// directory is how a script that cds elsewhere finds its siblings again, since its own
// path is only reliable when it was not reached through a symlink.
func (s *ActionService) env(ctx context.Context, agent agentstore.Agent, dir, trigger string) ([]string, error) {
	payload := actionPayload{
		ID:        agent.ID,
		Name:      agent.Name,
		State:     agent.State,
		Alive:     isPaneOwnedBy(s.tmux.PaneOwner(agent.Pane), agent.ID),
		Cwd:       agent.Cwd,
		Harness:   agent.Harness,
		SessionID: agent.SessionID,
		Flags:     agent.Flags,
		Pane:      agent.Pane,
		Workflow:  s.workflowName(ctx, agent.WorkflowID),
		Hidden:    agent.Hidden,
		Metadata:  make(map[string]string, len(agent.Metadata)),
	}
	payload.Prompt, payload.PromptTruncated = head(agent.Prompt, maxActionPrompt)
	if agent.Mark != nil {
		payload.Mark = *agent.Mark
	}
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

	return []string{
		"WEAVE_AGENT=" + string(encoded),
		"WEAVE_AGENT_ID=" + agent.ID,
		"WEAVE_ADDR=" + s.addr,
		"WEAVE_TMUX_SOCKET=" + s.socket,
		"WEAVE_ACTIONS_DIR=" + dir,
		"WEAVE_TRIGGER=" + trigger,
	}, nil
}

// workflowName is the name of the workflow an agent belongs to. A payload is not worth
// refusing over one label, so a lookup that fails leaves the field blank. The archive is
// read through, since an agent in an archived workflow still belongs to it by name.
func (s *ActionService) workflowName(ctx context.Context, id string) string {
	found, err := s.workflows.GetIncludingArchived(ctx, id)
	if err != nil || found == nil {
		return ""
	}

	return found.Name
}

// head is the first limit bytes of s and whether anything was dropped. The cut is pulled
// back off a partial character, so a script is always handed text it can decode.
func head(s string, limit int) (string, bool) {
	if len(s) <= limit {
		return s, false
	}

	cut := s[:limit]
	for len(cut) > 0 && !utf8.ValidString(cut) {
		cut = cut[:len(cut)-1]
	}

	return cut, true
}

// readHead reads up to limit bytes of a file and reports whether there was more. A
// truncated read is cut back to its last complete line, which keeps a preview from ending
// mid-character and reading as a file that is not text at all.
func readHead(path string, limit int) ([]byte, bool, error) {
	file, err := os.Open(path)
	if err != nil {
		return nil, false, fmt.Errorf("failed to open %s: %w", path, err)
	}
	defer func() { _ = file.Close() }()

	head := make([]byte, limit+1)
	read, err := io.ReadFull(file, head)
	if err != nil && !errors.Is(err, io.EOF) && !errors.Is(err, io.ErrUnexpectedEOF) {
		return nil, false, fmt.Errorf("failed to read %s: %w", path, err)
	}

	if read <= limit {
		return head[:read], false, nil
	}

	head = head[:limit]
	if lastLine := bytes.LastIndexByte(head, '\n'); lastLine > 0 {
		head = head[:lastLine]
	}

	return head, true, nil
}

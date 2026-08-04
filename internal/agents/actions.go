package agents

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"slices"
	"strings"
	"time"
	"unicode/utf8"

	"weave/config"
	"weave/internal/actions"
	"weave/internal/logging"
	"weave/internal/tmux"
	"weave/lib/xslice"

	agentstore "weave/internal/store/agent"
)

// agentGetter is the part of the agent store this service reads. An action changes
// nothing about the agent it fires on, so this never writes.
type agentGetter interface {
	GetByID(ctx context.Context, id string) (*agentstore.Agent, error)
}

// tmuxReader is what an action is told about tmux: who owns the agent's pane, the socket
// of the user's own server, and the options the panel records for a script to lay itself
// out against.
type tmuxReader interface {
	GetPaneOwner(pane string) string
	GetHostSocket() string
	GetPanelGlobal(name string) string
	ShowGlobal(name string) string
	DefaultShell() string
}

// actionRunner finds actions and starts them. RunAndWait is for a caller with something
// downstream of the action, and Run for one with nothing waiting.
type actionRunner interface {
	Discover(dir string) ([]actions.Action, error)
	Run(p actions.RunParams) error
	RunAndWait(ctx context.Context, p actions.RunParams) (actions.Result, error)
}

// configLoader reads the user's config. The service holds the loader rather than a
// parsed config so a directory changed while the server runs takes effect without a
// restart.
type configLoader func() (*config.Config, error)

// ErrActionNotFound reports that the actions directory holds no file by that name.
var ErrActionNotFound = errors.New("action not found")

// What set an action off, passed to it in WEAVE_TRIGGER, which is how a script tells
// apart the moments it can be reached at and refuses the ones it does not handle.
//
// The contract differs under TriggerSetup and TriggerResume: the agent is waiting on the
// action, so the exit status decides whether it gets a pane and stdout is read as the
// directory it runs in. Under the other two nothing is waiting and neither is read.
const (
	// TriggerManual is a person pressing the action key or a model calling run_action.
	TriggerManual = "manual"
	// TriggerSetup is a workflow's setup action, before one of its agents starts.
	TriggerSetup = "setup"
	// TriggerResume is the same setup action again, before a dormant agent gets its pane
	// back. A script reached this way is running a second time against a workspace it
	// built once already, so what it does about one that is still there is its own to
	// decide.
	TriggerResume = "resume"
	// TriggerCleanup is a workflow's cleanup action, once one of its agents is deleted.
	TriggerCleanup = "cleanup"
)

// ActionService lists and fires the executables behind the panel's actions, the ones the
// user has written and the ones weave ships. The WEAVE_AGENT payload is built here and
// nowhere else, so the panel firing an action and a model firing one hand the script the
// same thing.
type ActionService struct {
	agents agentGetter
	// workflows is read for the name of the workflow an agent belongs to, which a script
	// can use where the id it is stored under says nothing.
	workflows storedWorkflowReader
	tmux      tmuxReader
	runner    actionRunner
	load      configLoader
	// addr and socket are what an action reaches back through, the address of this server
	// and the tmux server every agent's pane lives on.
	addr   string
	socket string
	// builtinDir holds the actions shipped with this build, which the user's own directory
	// is read over.
	builtinDir string
	// editor is what an action opening one runs, since the service outlives the shell it
	// was spawned from and cannot trust the environment it inherited.
	editor string
}

// ActionParams carries the action service's dependencies.
type ActionParams struct {
	Agents     agentGetter
	Workflows  storedWorkflowReader
	Tmux       tmuxReader
	Runner     actionRunner
	Load       configLoader
	Addr       string
	Socket     string
	BuiltinDir string
	Editor     string
}

// NewActionService builds the service from its dependencies.
func NewActionService(p ActionParams) *ActionService {
	return &ActionService{
		agents:     p.Agents,
		workflows:  p.Workflows,
		tmux:       p.Tmux,
		runner:     p.Runner,
		load:       p.Load,
		addr:       p.Addr,
		socket:     p.Socket,
		builtinDir: p.BuiltinDir,
		editor:     p.Editor,
	}
}

// maxActionPrompt bounds how much of an agent's prompt the payload carries. Everything an
// exec is handed shares one kernel limit, and the prompt is the only field here with no
// bound of its own, so a long enough one would fail the run at exec.
const maxActionPrompt = 16 * 1024

// ListActions is every action on offer, in name order, each with what its own header
// declares about it.
func (s *ActionService) ListActions() ([]actions.Action, error) {
	_, found, err := s.discover()
	if err != nil {
		return nil, err
	}

	return xslice.Map(found, func(action foundAction) actions.Action { return action.Action }), nil
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

	action, err := s.find(name)
	if err != nil {
		return err
	}

	env, err := s.env(ctx, actionEnvParams{agent: *agent, dir: action.dir, trigger: TriggerManual})
	if err != nil {
		return err
	}

	return s.start(ctx, startParams{
		action:  action,
		env:     env,
		agent:   *agent,
		trigger: TriggerManual,
	})
}

// ErrNotEditable reports that an action's file is a compiled program, so there is nothing
// in it to open in an editor.
var ErrNotEditable = errors.New("the file is not text, so there is nothing to edit")

// editActionName is the action that opens another action's file. It is fired like any
// other, so a user who wants their editor opened differently writes their own.
const editActionName = "_edit_action"

// editPathVar is how the file to open reaches the editing action, which takes a path
// rather than a name because the panel names an action and the server is what knows which
// directory that name was found in.
const editPathVar = "WEAVE_EDIT_PATH"

// EditAction opens an action's own file in the user's editor, by firing the editing action
// on the agent the panel's list was opened on. It returns once that process exists, the way
// RunAction does, since the editor it opens belongs to whoever is reading the pane.
//
// A compiled program is refused rather than opened, since an editor handed one shows a
// screen of bytes and the file it would write back is not a program any more.
func (s *ActionService) EditAction(ctx context.Context, agentID, name string) error {
	agent, err := s.agents.GetByID(ctx, agentID)
	if err != nil {
		return fmt.Errorf("failed to read agent %q: %w", agentID, err)
	}
	if agent == nil {
		return ErrAgentNotFound
	}

	target, err := s.find(name)
	if err != nil {
		return err
	}
	if !target.Text {
		return fmt.Errorf("failed to edit action %s: %w", name, ErrNotEditable)
	}

	editor, err := s.find(editActionName)
	if err != nil {
		return err
	}

	env, err := s.env(ctx, actionEnvParams{agent: *agent, dir: editor.dir, trigger: TriggerManual})
	if err != nil {
		return err
	}

	return s.start(ctx, startParams{
		action:  editor,
		env:     append(env, editPathVar+"="+target.Path),
		agent:   *agent,
		trigger: TriggerManual,
	})
}

// ErrSetupFailed reports that a workflow's setup action refused, so the agent was never
// launched. It is the one action failure something else is downstream of.
var ErrSetupFailed = errors.New("the setup action failed")

// setupTimeout bounds how long a setup action that declares no timeout of its own may
// take, since a launch waits on it. It is generous because the ordinary case is installing
// dependencies over a network.
const setupTimeout = 10 * time.Minute

// maxSetupDir bounds what a setup action may name as the directory to run in, matching
// the limit the same field carries on the wire.
const maxSetupDir = 4096

// RunSetupAction runs a workflow's setup action for an agent being created and does not
// return until it has finished.
func (s *ActionService) RunSetupAction(ctx context.Context, agent agentstore.Agent, name string) (string, error) {
	return s.runAwaitedAction(ctx, awaitedActionParams{agent: agent, name: name, trigger: TriggerSetup})
}

// RunResumeAction runs the same setup action for a dormant agent about to get its pane
// back. Resume is where a workspace has to exist again, since archive leaves whatever setup
// built alone and a restore only takes the row out of the archive.
func (s *ActionService) RunResumeAction(ctx context.Context, agent agentstore.Agent, name string) (string, error) {
	return s.runAwaitedAction(ctx, awaitedActionParams{agent: agent, name: name, trigger: TriggerResume})
}

// awaitedActionParams is one run of the setup action, with what set it off, since a create
// and a resume reach the same script and only the trigger tells them apart.
type awaitedActionParams struct {
	agent   agentstore.Agent
	name    string
	trigger string
}

// runAwaitedAction runs the setup action to completion. The directory the agent should run
// in is whatever the action trims to on stdout, ordinarily nothing. Progress belongs on
// stderr, so a script that chats on stdout is refused rather than taken at its word.
//
// Both ends reach the log at info, and a failure is recorded as the end of the run rather
// than reported as one, since the refusal travels back to the caller and is theirs to tell
// somebody about. The record is what says a run happened at all.
func (s *ActionService) runAwaitedAction(ctx context.Context, p awaitedActionParams) (string, error) {
	action, err := s.find(p.name)
	if err != nil {
		return "", fmt.Errorf("%w: %w", ErrSetupFailed, err)
	}

	env, err := s.env(ctx, actionEnvParams{agent: p.agent, dir: action.dir, trigger: p.trigger})
	if err != nil {
		return "", err
	}

	logger := s.actionLogger(ctx, actionRecord{name: p.name, trigger: p.trigger, agent: p.agent})
	logger.LogAttrs(ctx, slog.LevelInfo, "action started")
	began := time.Now()

	result, err := s.runner.RunAndWait(ctx, actions.RunParams{
		Action:  action.Action,
		Dir:     action.dir,
		Env:     env,
		Timeout: timeoutFor(action, setupTimeout),
	})

	finished := []slog.Attr{
		slog.Int(logging.ExitCode, result.ExitCode),
		slog.Int64(logging.DurationMS, elapsedMS(began)),
	}
	if said := bytes.TrimSpace(result.Stderr); len(said) > 0 {
		finished = append(finished, slog.String(logging.ActionStderr, string(said)))
	}

	logger.LogAttrs(ctx, slog.LevelInfo, "action finished", finished...)

	if err != nil {
		return "", fmt.Errorf("%w: %w%s", ErrSetupFailed, err, stderrDetail(result.Stderr))
	}

	answered := string(bytes.TrimSpace(result.Stdout))
	if len(answered) > maxSetupDir {
		return "", fmt.Errorf("%w: %s wrote %d bytes to stdout, where a directory to run in was expected. Progress belongs on stderr", ErrSetupFailed, p.name, len(answered))
	}

	return answered, nil
}

// RunCleanupAction fires a workflow's cleanup action and returns once the process exists,
// since nothing is downstream of a cleanup. The agent is taken as a value rather than
// looked up, so a cleanup can fire for an agent whose row has already gone.
func (s *ActionService) RunCleanupAction(ctx context.Context, agent agentstore.Agent, name string) error {
	action, err := s.find(name)
	if err != nil {
		return err
	}

	env, err := s.env(ctx, actionEnvParams{agent: agent, dir: action.dir, trigger: TriggerCleanup})
	if err != nil {
		return err
	}

	return s.start(ctx, startParams{
		action:  action,
		env:     env,
		agent:   agent,
		trigger: TriggerCleanup,
	})
}

// startParams is one action about to be fired, with the agent and the trigger its record
// is written against.
type startParams struct {
	action  foundAction
	env     []string
	agent   agentstore.Agent
	trigger string
}

// start fires an action and arranges for what it did to reach the log.
func (s *ActionService) start(ctx context.Context, p startParams) error {
	logger := s.actionLogger(ctx, actionRecord{name: p.action.Name, trigger: p.trigger, agent: p.agent})
	ctx = context.WithoutCancel(ctx)
	began := time.Now()

	err := s.runner.Run(actions.RunParams{
		Action: p.action.Action,
		Dir:    p.action.dir,
		Env:    p.env,
		// Nothing is waiting, so an action that declared no timeout runs as long as it likes.
		Timeout: timeoutFor(p.action, 0),
		Done: func(result actions.Result, err error) {
			// Reported here rather than returned, since this is the path with nothing
			// downstream: the caller answered the moment the process existed, so a
			// failure that is not written here is never told to anybody.
			if err != nil {
				logger.LogAttrs(ctx, slog.LevelError, "action failed",
					append(failedAction(result, err), slog.Int64(logging.DurationMS, elapsedMS(began)))...)

				return
			}

			logger.LogAttrs(ctx, slog.LevelInfo, "action finished",
				slog.Int(logging.ExitCode, result.ExitCode),
				slog.Int64(logging.DurationMS, elapsedMS(began)))
		},
	})
	if err != nil {
		return err
	}

	// Written once the process exists, so it never claims a run that never happened.
	logger.LogAttrs(ctx, slog.LevelInfo, "action started")

	return nil
}

// actionRecord names the run a logger is being built for.
type actionRecord struct {
	name    string
	trigger string
	agent   agentstore.Agent
}

// actionLogger carries the fields every record about one run of an action shares, so a run
// is followed by filtering on the action, the trigger, or the agent it fired on rather than
// by reading the messages.
func (s *ActionService) actionLogger(ctx context.Context, r actionRecord) *slog.Logger {
	return logging.From(ctx).With(logging.Attrs([]slog.Attr{
		slog.String(logging.ActionName, r.name),
		slog.String(logging.ActionTrigger, r.trigger),
		slog.String(logging.ActionAgentID, r.agent.ID),
		slog.String(logging.ActionAgentCwd, r.agent.Cwd),
	})...)
}

// elapsedMS is how long a run took, for a field that sorts and thresholds as a number.
func elapsedMS(began time.Time) int64 {
	return time.Since(began).Milliseconds()
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

// foundAction is an action with the directory it came out of, since an action runs among
// its own siblings and those are the user's for one file and this build's for another.
type foundAction struct {
	actions.Action
	dir string
}

// find is the action by name. The name is matched against what the directories hold rather
// than joined onto a path, so a name that climbs out finds nothing instead of some
// executable elsewhere on the machine.
func (s *ActionService) find(name string) (foundAction, error) {
	dir, found, err := s.discover()
	if err != nil {
		return foundAction{}, err
	}

	for _, action := range found {
		if action.Name == name {
			return action, nil
		}
	}

	return foundAction{}, fmt.Errorf("%w: %q is not in %s", ErrActionNotFound, name, dir)
}

// timeoutFor is how long one run of an action gets. What the file declares wins, and
// fallback is what an action that declared nothing gets.
func timeoutFor(action foundAction, fallback time.Duration) time.Duration {
	if action.Header.Timeout > 0 {
		return action.Header.Timeout
	}

	return fallback
}

// discover is the user's actions directory and every action on offer, theirs read over the
// ones shipped with this build so a file they write of the same name is what runs. The
// config is read here rather than held, so a directory changed while the server runs takes
// effect without a restart.
func (s *ActionService) discover() (string, []foundAction, error) {
	cfg, err := s.load()
	if err != nil {
		return "", nil, fmt.Errorf("failed to read the config: %w", err)
	}

	userDir, err := cfg.GetActionsDir()
	if err != nil {
		return "", nil, err
	}

	var found []foundAction
	claimed := make(map[string]bool)

	for _, dir := range []string{userDir, s.builtinDir} {
		if dir == "" {
			continue
		}

		listed, err := s.runner.Discover(dir)
		if err != nil {
			return "", nil, err
		}

		for _, action := range listed {
			if claimed[action.Name] {
				continue
			}

			claimed[action.Name] = true
			found = append(found, foundAction{Action: action, dir: dir})
		}
	}

	slices.SortFunc(found, func(a, b foundAction) int { return strings.Compare(a.Name, b.Name) })

	return userDir, found, nil
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

// actionEnvParams is the one run an environment is being built for.
type actionEnvParams struct {
	agent   agentstore.Agent
	dir     string
	trigger string
}

// env is the environment an action runs with, over the server's own. The agent goes in
// whole as JSON, beside the values a script reaches back through: this server's address,
// the socket of the tmux server the agent's pane lives on, and the actions directory.
//
// The socket matters because a script sending keys to the pane in the payload would
// otherwise address the user's own server, where that pane id means something else. The
// directory is how a script that cds elsewhere finds its siblings again, since its own
// path is only reliable when it was not reached through a symlink.
//
// The rest is what a script drawing something on screen needs without parsing the payload:
// the agent's directory, the pane under the panel's stage, the editor and shell to open in
// it, and the user's own tmux server for a script that would rather open a window there.
func (s *ActionService) env(ctx context.Context, p actionEnvParams) ([]string, error) {
	agent, dir, trigger := p.agent, p.dir, p.trigger

	payload := actionPayload{
		ID:        agent.ID,
		Name:      agent.Name,
		State:     agent.State,
		Alive:     isPaneOwnedBy(s.tmux.GetPaneOwner(agent.Pane), agent.ID),
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
		"WEAVE_AGENT_CWD=" + agent.Cwd,
		"WEAVE_ADDR=" + s.addr,
		"WEAVE_TMUX_SOCKET=" + s.socket,
		"WEAVE_ACTIONS_DIR=" + dir,
		"WEAVE_TRIGGER=" + trigger,
		"WEAVE_SECONDARY_PANE=" + s.tmux.GetPanelGlobal(tmux.SecondaryPaneOption),
		"WEAVE_HOST_TMUX_SOCKET=" + s.tmux.GetHostSocket(),
		"WEAVE_HOST_CLIENT=" + s.tmux.ShowGlobal(tmux.InvokingClientOption),
		"EDITOR=" + s.editor,
		"WEAVE_SHELL=" + s.tmux.DefaultShell(),
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

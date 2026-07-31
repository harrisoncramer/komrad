package agents

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"time"
	"unicode/utf8"

	"weave/internal/actions"
	"weave/internal/appconfig"

	agentstore "weave/internal/store/agent"
)

// actionReader is the part of the agent store this service reads. An action is fired on an
// agent and changes nothing about it, so this never writes.
type actionReader interface {
	GetByID(ctx context.Context, id string) (*agentstore.Agent, error)
}

// actionRunner starts an action. The service takes it as an interface so its tests drive the
// whole path without spawning processes.
type actionRunner interface {
	Run(action actions.Action, dir string, env []string) error
}

// actionTmux is the one thing this asks of tmux, which agent a pane currently belongs to. It is
// what says whether the agent an action is fired on is running.
type actionTmux interface {
	PaneOwner(pane string) string
}

// configLoader reads the user's config. The service holds the loader rather than a parsed
// config so the file is read per call, which is what lets an actions directory changed while
// the server runs take effect without a restart.
type configLoader func() (*appconfig.Config, error)

// ErrActionNotFound reports that the actions directory holds no file by that name.
var ErrActionNotFound = errors.New("action not found")

// ActionService lists and fires the executables the user keeps in their actions directory.
//
// It owns the whole of what an action sees. The payload a script parses out of WEAVE_AGENT is
// built here and nowhere else, so the panel firing an action and a model firing one hand the
// script the same thing.
type ActionService struct {
	agents actionReader
	// workflows is read for the name of the workflow an agent belongs to, which is what a
	// script can use where the id it is stored under says nothing.
	workflows workflowReader
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
	Workflows workflowReader
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

// maxActionPreview bounds how much of an action is read for the preview. A caller renders
// whatever it is given and an action can be a compiled program of any size, so this is a head
// of the file rather than all of it.
const maxActionPreview = 8 * 1024

// maxActionPrompt bounds how much of an agent's prompt the payload carries. Everything an exec
// is handed shares one kernel limit, and the prompt is the only field in here with no bound of
// its own, so a long enough one would fail the run at exec with a message naming the script. The
// transcript is left out of the payload for the same reason; this is the same call made for a
// field small enough to be worth keeping a head of.
const maxActionPrompt = 16 * 1024

// ListedAction is an action with the start of its own text, which is what a caller showing one
// needs and what the file itself is the only source of.
type ListedAction struct {
	actions.Action
	// Preview is the head of the file, empty for one that could not be read.
	Preview []byte
	// Truncated says the file is longer than Preview carries.
	Truncated bool
}

// ListActions is every file in the actions directory, in name order, each with the start of its
// contents. A file that cannot be read is listed with an empty preview rather than dropped,
// since it is still there and still nameable.
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

// RunAction fires an action on an agent and returns once the process exists. Nothing waits for
// it and nothing reads what it did, so the only failures are the ones that happen before it
// starts: an agent or an action that is not there, and a file that will not execute.
//
// It runs in the actions directory rather than in the agent's own. An action that removes the
// directory its agent worked in cannot be standing in it, and an agent whose directory is
// already gone would otherwise have no action that could run on it at all, which is exactly the
// row still wanting one. The agent's directory is in the payload for a script that wants it, and
// a script that needs to work there says so with a cd of its own.
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

	env, err := s.env(ctx, *agent, dir)
	if err != nil {
		return err
	}

	return s.runner.Run(action, dir, env)
}

// find is the action by name, with the directory it was found in. The name is matched against
// what the directory holds rather than joined onto its path, so a name carrying enough parent
// directories to climb out finds nothing instead of resolving to some executable elsewhere on
// the machine.
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

	found, err := actions.Discover(dir)
	if err != nil {
		return "", nil, err
	}

	return dir, found, nil
}

// actionPayload is what an action is told about the agent it was fired on, encoded into
// WEAVE_AGENT. It is a shape of its own rather than the wire format, so what a script parses is
// written down in one place, and so metadata arrives as an object a script can index by key
// rather than as the store's list of pairs.
//
// The transcript is not in here. It is far past what an environment variable holds, and a
// script that wants it has the agent's id and the address of the server.
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

// env is the environment an action runs with, over the server's own.
//
// The agent goes in whole as JSON under one name. The values beside it are what a script reaches
// back through: the address of this server, which is where everything about the agent too large
// to pass in an environment variable is, the socket of the tmux server the agent's pane lives on,
// and the directory the action was found in. Without that socket a script sending keys to the
// pane in the payload would be addressing the user's own server, where that pane id means
// something else. The directory is what a script that cds to the agent's own gets back to its
// siblings by, since its own path is only reliable when it was not reached through a symlink.
func (s *ActionService) env(ctx context.Context, agent agentstore.Agent, dir string) ([]string, error) {
	payload := actionPayload{
		ID:        agent.ID,
		Name:      agent.Name,
		State:     agent.State,
		Alive:     paneOwnedBy(s.tmux.PaneOwner(agent.Pane), agent.ID),
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
	}, nil
}

// workflowName is the name of the workflow an agent belongs to, empty when it cannot be read.
// A payload is not worth refusing over the label on one field, so a lookup that fails leaves the
// field blank rather than failing the run.
func (s *ActionService) workflowName(ctx context.Context, id string) string {
	found, err := s.workflows.Get(ctx, id)
	if err != nil || found == nil {
		return ""
	}

	return found.Name
}

// head is the first limit bytes of s and whether anything was dropped. The cut is pulled back
// off a partial character, so what a script is handed is always text it can decode.
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

// readHead reads up to limit bytes of a file and reports whether there was more. A truncated
// read is cut back to its last complete line, which keeps a preview from ending mid-character
// and reading as a file that is not text at all.
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

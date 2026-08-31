package agents

import (
	"context"
	"errors"
	"fmt"
	"strings"

	"weave/config"
	"weave/internal/harness"
	"weave/internal/store"
	agentstore "weave/internal/store/agent"
	"weave/lib/xptr"
	"weave/lib/xstr"
)

var ErrSessionNotFound = errors.New("no conversation on disk holds that session id")

var ErrSessionIsWeaves = errors.New("that conversation is one of this panel's own agents")

var ErrSessionDirInUse = errors.New("the directory that conversation was held in belongs to another agent")

var ErrSessionDirGone = errors.New("the directory that conversation was held in is gone")

// adoptSessionAgentStore is the slice of the agent store adopting a conversation reads, which is
// every agent the panel holds, since the question is which of them already has the conversation
// or the directory it was held in.
type adoptSessionAgentStore interface {
	List(ctx context.Context) ([]agentstore.Agent, error)
	ListAll(ctx context.Context) ([]agentstore.Agent, error)
}

// harnessLister is the registry as adoption reads it, which needs every harness rather than one,
// since the conversation's own log is what says which CLI held it.
type harnessLister interface {
	harnessGetter
	IDs() []string
}

// AdoptAgentService puts an agent on a conversation somebody else started, working out which CLI
// held it and where before handing the launch itself over.
type AdoptAgentService struct {
	agents    adoptSessionAgentStore
	workflows workflowReader
	launch    *LaunchService
	harnesses harnessLister
}

type AdoptAgentServiceParams struct {
	Agents    adoptSessionAgentStore
	Workflows workflowReader
	Launch    *LaunchService
	Harnesses harnessLister
}

// NewAdoptAgentService builds the service from its dependencies.
func NewAdoptAgentService(p AdoptAgentServiceParams) *AdoptAgentService {
	return &AdoptAgentService{
		agents:    p.Agents,
		workflows: p.Workflows,
		launch:    p.Launch,
		harnesses: p.Harnesses,
	}
}

type AdoptAgentParams struct {
	Client         string
	SessionID      string
	Title          string
	Flags          string
	Prompt         string
	Harness        string
	WorkflowID     string
	Hidden         bool
	IdempotencyKey string
	SetupPane      string
}

// AdoptAgent puts an agent on a conversation weave did not start, forking it where the CLI can
// so the person keeps their own, and taking it over where it cannot.
func (s *AdoptAgentService) AdoptAgent(ctx context.Context, p AdoptAgentParams) (*agentstore.Agent, error) {
	sessionID := strings.TrimSpace(p.SessionID)
	if sessionID == "" {
		return nil, fmt.Errorf("%w: nothing to adopt", ErrSessionNotFound)
	}

	if _, err := s.workflows.Get(ctx, p.WorkflowID); errors.Is(err, store.ErrNotFound) {
		return nil, fmt.Errorf("%w: %q", ErrWorkflowNotFound, p.WorkflowID)
	} else if err != nil {
		return nil, fmt.Errorf("failed to look up workflow %q: %w", p.WorkflowID, err)
	}

	existing, err := s.agentOnSession(ctx, sessionID)
	if err != nil && !errors.Is(err, store.ErrNotFound) {
		return nil, err
	}
	if err == nil {
		return existing, nil
	}

	harnessID, hn, err := s.findHarness(sessionID, p.Harness)
	if err != nil {
		return nil, err
	}

	session, err := hn.DescribeSession(sessionID)
	if errors.Is(err, harness.ErrSessionNotFound) {
		return nil, fmt.Errorf("%w: %q", ErrSessionNotFound, sessionID)
	}
	if err != nil {
		return nil, fmt.Errorf("failed to read the conversation %q: %w", sessionID, err)
	}

	dir := config.ExpandHome(session.Dir)
	if err := requireUsableDir(dir); err != nil {
		return nil, fmt.Errorf("%w: %s was held in %s: %w", ErrSessionDirGone, sessionID, dir, err)
	}

	if owner, err := s.agentIn(ctx, dir); err == nil {
		return nil, fmt.Errorf("%w: %s is where agent %s works", ErrSessionDirInUse, dir, owner.Name)
	} else if !errors.Is(err, store.ErrNotFound) {
		return nil, err
	}

	params := NewAgentParams{
		Client:     p.Client,
		Title:      adoptSessionTitle(p.Title, sessionID),
		Dir:        dir,
		Flags:      p.Flags,
		Prompt:     adoptSessionPrompt(sessionID, session, hn.ForksOnResume(), p.Prompt),
		Harness:    harnessID,
		WorkflowID: p.WorkflowID,
		Hidden:     p.Hidden,
		// The conversation was held in somebody's own checkout, so both actions are blanked to
		// stop the workflow making a worktree the agent will not use and, on delete, removing a
		// directory weave never created.
		SetupAction:    xptr.Ptr(""),
		CleanupAction:  xptr.Ptr(""),
		IdempotencyKey: p.IdempotencyKey,
		SetupPane:      p.SetupPane,
		AdoptedBranch:  session.Branch,
		AdoptedSession: sessionID,
		ResumeFrom:     sessionID,
	}

	// A CLI that cannot fork writes on into the person's own conversation, so the agent holds
	// their session id rather than one of weave's and the row says as much.
	if !hn.ForksOnResume() {
		params.SessionID = sessionID
	}

	created, err := s.launch.NewAgent(ctx, params)
	if err != nil && !errors.Is(err, ErrAlreadyLaunched) {
		return nil, err
	}

	return created, nil
}

// findHarness works out which CLI held a conversation by asking each of them for its log, since
// a session id is all the caller has and the log's location is what names its CLI.
func (s *AdoptAgentService) findHarness(sessionID, named string) (string, harness.Harness, error) {
	if id := strings.TrimSpace(named); id != "" {
		hn, err := s.harnesses.GetByID(id)
		if err != nil {
			return "", nil, err
		}

		return id, hn, nil
	}

	for _, id := range s.harnesses.IDs() {
		hn, err := s.harnesses.GetByID(id)
		if err != nil {
			continue
		}
		if hn.GetLogPath(sessionID) != "" {
			return id, hn, nil
		}
	}

	return "", nil, fmt.Errorf("%w: %q", ErrSessionNotFound, sessionID)
}

// agentOnSession returns the live agent already adopted onto a conversation, so asking twice
// hands back the agent doing the work rather than starting a second CLI on one history.
func (s *AdoptAgentService) agentOnSession(ctx context.Context, sessionID string) (*agentstore.Agent, error) {
	agents, err := s.agents.List(ctx)
	if err != nil {
		return nil, fmt.Errorf("failed to list agents: %w", err)
	}

	for _, agent := range agents {
		if agent.ArchivedAt != nil {
			continue
		}
		if agent.AdoptedSession == sessionID {
			return &agent, nil
		}
		// A conversation the panel started is already an agent's, and resuming it beside that
		// agent would put two CLIs on one log.
		if agent.SessionID == sessionID {
			return nil, fmt.Errorf("%w: %s is the conversation of agent %s", ErrSessionIsWeaves, sessionID, agent.Name)
		}
	}

	return nil, fmt.Errorf("%w: no agent is on conversation %q", store.ErrNotFound, sessionID)
}

// agentIn returns the agent whose cwd is a directory, archived ones included, since an archived
// agent's cleanup may still be the thing that removes it.
func (s *AdoptAgentService) agentIn(ctx context.Context, dir string) (*agentstore.Agent, error) {
	agents, err := s.agents.ListAll(ctx)
	if err != nil {
		return nil, fmt.Errorf("failed to list agents: %w", err)
	}

	for _, agent := range agents {
		if agent.Cwd != "" && sameDir(agent.Cwd, dir) {
			return &agent, nil
		}
	}

	return nil, fmt.Errorf("%w: no agent is working in %q", store.ErrNotFound, dir)
}

func adoptSessionTitle(title, sessionID string) string {
	if strings.TrimSpace(title) != "" {
		return title
	}

	return "adopted-" + xstr.Slug(sessionID[:min(len(sessionID), 8)], 12)
}

// adoptSessionPrompt puts what the agent is walking into in front of whatever it was asked to do,
// since an agent that does not know the history is somebody else's will answer as though every
// turn in it were its own.
func adoptSessionPrompt(sessionID string, session harness.Session, forked bool, prompt string) string {
	var b strings.Builder

	b.WriteString("You are picking up a conversation that already exists. Every turn before this one was a person working with another agent, and you are reading it rather than remembering it, so read back over it before you answer anything.\n")

	switch {
	case forked:
		fmt.Fprintf(&b, "\nThis is a copy. The conversation you were forked from is %s and it is untouched, so the person can carry on in their own terminal and nothing you do here reaches them.\n", sessionID)
	default:
		fmt.Fprintf(&b, "\nThis is not a copy. You are writing into conversation %s itself, which is the person's own, so anything you say lands in the history they will read when they come back to it.\n", sessionID)
	}

	fmt.Fprintf(&b, "\nYou are working in %s", session.Dir)
	if session.Branch != "" {
		fmt.Fprintf(&b, ", on branch %s", session.Branch)
	}
	b.WriteString(". The work in it is real and it is not yours: read the log and the diff before you change anything, and do not force-push, rebase, or delete a branch you did not make.\n")

	if strings.TrimSpace(prompt) == "" {
		b.WriteString("\nNothing has been asked of you yet. Say where the conversation had got to and wait.")

		return b.String()
	}

	b.WriteString("\n")
	b.WriteString(prompt)

	return b.String()
}

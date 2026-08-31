package agents

import (
	"context"
	"errors"
	"fmt"
	"path/filepath"
	"strings"

	"weave/internal/store"
	agentstore "weave/internal/store/agent"
	"weave/lib/xptr"
)

var ErrAdoptDirInUse = errors.New("that directory is another agent's to work in")

// adoptionAgentStore is the slice of the agent store both adoptions read, which is every agent
// the panel holds, since the question is always which of them already has what is being adopted.
type adoptionAgentStore interface {
	List(ctx context.Context) ([]agentstore.Agent, error)
	ListAll(ctx context.Context) ([]agentstore.Agent, error)
	SetMetadata(ctx context.Context, params agentstore.SetMetadataParams) (bool, error)
}

// placement is where an adopted agent runs and who made that directory.
type placement struct {
	// Dir is where the agent runs, empty when the workflow's own setup action is to make a
	// checkout for it.
	Dir string

	// Theirs marks a directory somebody else made, whose provisioning and teardown are none of
	// weave's business.
	Theirs bool
}

// adoption is one resolved adoption, which is what a subject and a placement come to once the
// question of what was named has been answered.
type adoption struct {
	Client         string
	Title          string
	Flags          string
	Prompt         string
	Harness        string
	WorkflowID     string
	Hidden         bool
	IdempotencyKey string
	SetupPane      string

	Placement placement

	// Branch is the branch the agent works on rather than one weave made, which is what pull
	// request detection and archive-on-close key off.
	Branch string

	// Session is the conversation the agent was put on, blank when a branch was adopted.
	Session string

	// SessionID is the conversation the agent holds, which is set only when the CLI cannot carry
	// a conversation into an id weave minted and the agent therefore holds somebody else's.
	SessionID string

	// ResumeFrom is the conversation the CLI is pointed at on the way in.
	ResumeFrom string

	// PullRequest is recorded on the agent as it starts, so the panel can open it straight away.
	PullRequest string
}

// adopter is the middle of both adoptions: it decides nothing about what is being adopted and
// everything about how an agent is started on it.
type adopter struct {
	agents adoptionAgentStore
	launch *LaunchService
}

// start launches the agent an adoption resolved to and records what it was put on.
func (a *adopter) start(ctx context.Context, plan adoption) (*agentstore.Agent, error) {
	params := NewAgentParams{
		Client:         plan.Client,
		Title:          plan.Title,
		Dir:            plan.Placement.Dir,
		Flags:          plan.Flags,
		Prompt:         plan.Prompt,
		Harness:        plan.Harness,
		WorkflowID:     plan.WorkflowID,
		Hidden:         plan.Hidden,
		IdempotencyKey: plan.IdempotencyKey,
		SetupPane:      plan.SetupPane,
		AdoptedBranch:  plan.Branch,
		AdoptedSession: plan.Session,
		SessionID:      plan.SessionID,
		ResumeFrom:     plan.ResumeFrom,
	}

	// Both actions are blanked rather than left to the workflow, which would otherwise make a
	// second checkout nobody uses and, on delete, remove a directory weave never created.
	if plan.Placement.Theirs {
		params.SetupAction = xptr.Ptr("")
		params.CleanupAction = xptr.Ptr("")
	}

	created, err := a.launch.NewAgent(ctx, params)
	if err != nil && !errors.Is(err, ErrAlreadyLaunched) {
		return nil, err
	}

	if plan.PullRequest != "" {
		if _, err := a.agents.SetMetadata(ctx, agentstore.SetMetadataParams{ID: created.ID, Key: prMetadataKey, Value: &plan.PullRequest}); err != nil {
			return nil, fmt.Errorf("failed to record the pull request %s was adopted from: %w", created.Name, err)
		}
	}

	return created, nil
}

// requireDirFree refuses a directory that is already an agent's to work in, since two agents in
// one checkout means either one's cleanup removes it out from under the other.
func (a *adopter) requireDirFree(ctx context.Context, dir string) error {
	owner, err := a.agentIn(ctx, dir)
	if errors.Is(err, store.ErrNotFound) {
		return nil
	}
	if err != nil {
		return err
	}

	return fmt.Errorf("%w: %s is where agent %s works", ErrAdoptDirInUse, dir, owner.Name)
}

// agentIn returns the agent whose cwd is a directory, archived ones included, since an archived
// agent's cleanup may still be the thing that removes it.
func (a *adopter) agentIn(ctx context.Context, dir string) (*agentstore.Agent, error) {
	agents, err := a.agents.ListAll(ctx)
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

// sameDir compares two paths as the filesystem sees them, since a worktree listing reports
// resolved paths and a configured cwd is whatever somebody typed.
func sameDir(a, b string) bool {
	resolvedA, err := filepath.EvalSymlinks(a)
	if err != nil {
		resolvedA = a
	}

	resolvedB, err := filepath.EvalSymlinks(b)
	if err != nil {
		resolvedB = b
	}

	return filepath.Clean(resolvedA) == filepath.Clean(resolvedB)
}

// closeAdoptionPrompt ends an adoption's opening prompt with what was asked of the agent, or with
// the standing instruction for an adoption nothing has been asked of yet.
func closeAdoptionPrompt(b *strings.Builder, prompt, waiting string) string {
	if strings.TrimSpace(prompt) == "" {
		b.WriteString("\n")
		b.WriteString(waiting)

		return b.String()
	}

	b.WriteString("\n")
	b.WriteString(prompt)

	return b.String()
}

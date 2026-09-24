// Package panel is what the service has told the panel, in the panel's own shape.
package panel

import (
	"time"

	"weave/internal/git"
	"weave/lib/xslice"
	agentv1 "weave/proto/gen/agent/v1"
)

// State is what an agent is doing.
type State string

const (
	StateThinking        State = "thinking"
	StateWaiting         State = "waiting"
	StateIdle            State = "idle"
	StateSettingUp       State = "setting_up"
	StateFailedToCreate  State = "failed_to_create"
	StateFailedToCleanup State = "failed_to_cleanup"
	StateUncertain       State = "uncertain"
)

// DoesPaneDecide reports whether reading the agent's terminal is what decides this state.
func (s State) DoesPaneDecide() bool {
	switch s {
	case StateIdle, StateThinking, StateWaiting, StateUncertain:
		return true
	case StateSettingUp, StateFailedToCreate, StateFailedToCleanup:
		return false
	}

	return false
}

// IsFailed reports whether a state is one of the two an agent reaches by failing.
func (s State) IsFailed() bool {
	return s == StateFailedToCreate || s == StateFailedToCleanup
}

// Role is what an agent was launched to do.
type Role string

const (
	RoleImplementer  Role = "implementer"
	RoleReviewer     Role = "reviewer"
	RoleOrchestrator Role = "orchestrator"
)

// Metadata is one fact an agent recorded about its own work.
type Metadata struct {
	Key   string
	Value string
}

// Agent is one agent as the service listed it.
type Agent struct {
	ID             string
	Name           string
	Pane           string
	Cwd            string
	Flags          string
	Prompt         string
	Role           Role
	State          State
	StateChangedAt time.Time
	ReportedReason string
	Harness        string
	SessionID      string
	WorkflowID     string
	ScheduleID     *string
	CreatedAt      time.Time
	ArchivedAt     *time.Time
	Hidden         bool
	Metadata       []Metadata
	Branch         *string
	// ParentAgentID is the agent this one is attached to, whose archive, delete and restore it
	// follows.
	ParentAgentID   *string
	WorktreeRemoved bool
	// SequenceInProgress is whether a merge, a rebase or a cherry-pick is part-way through the
	// agent's checkout and waiting on somebody.
	SequenceInProgress bool
	// HeldAt is when a person stopped this agent and took its worktree, nil while nobody holds it.
	HeldAt *time.Time
}

// AgentRow is an agent as the listing draws it: the agent, whether its pane is alive, the mail
// waiting for it, its unread failure, its local review and the pull requests on its branch.
type AgentRow struct {
	Agent      Agent
	Alive      bool
	UnreadMail bool
	FailedRun  bool
	// FailedRunLog is the run its unread failure was written to.
	FailedRunLog *int32
	// LocalVerdict is what the last released local review concluded about this agent's work,
	// UNSPECIFIED when none has concluded anything.
	LocalVerdict agentv1.ReviewVerdict
	// LocalTree is where the worktree stands against the round that verdict was given on.
	LocalTree git.Standing
	// MovedSinceRound is the newest round's snapshot while the worktree no longer hashes to it.
	MovedSinceRound string
	// PullRequest is where the pull request opened from its branch stands.
	PullRequest PullRequestStanding
	// PullRequests is every pull request opened from its branch, the current one first.
	PullRequests []PullRequest
}

// IDs is the id of each agent, in the order they were given.
func IDs(agents []Agent) []string {
	return xslice.Map(agents, func(agent Agent) string { return agent.ID })
}

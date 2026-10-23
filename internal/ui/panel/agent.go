// Package panel is what the server has told the panel, in the panel's own shape.
package panel

import (
	"time"

	"komrad/internal/view"
	"komrad/lib/xslice"
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
	StateErrored         State = "errored"
)

// DoesPaneDecide reports whether reading the agent's terminal is what decides this state.
func (s State) DoesPaneDecide() bool {
	switch s {
	case StateIdle, StateThinking, StateWaiting, StateUncertain, StateErrored:
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

// role is what komrad launched an agent to do.
type role string

const (
	RoleWriter       role = "writer"
	RoleReviewer     role = "reviewer"
	RoleOrchestrator role = "orchestrator"
)

// Sequence is what git has part-way through an agent's checkout, named as git's own command is.
type Sequence string

const (
	sequenceNone       Sequence = ""
	SequenceRebase     Sequence = "rebase"
	sequenceApply      Sequence = "am"
	sequenceMerge      Sequence = "merge"
	sequenceCherryPick Sequence = "cherry-pick"
	sequenceRevert     Sequence = "revert"
	sequenceUnknown    Sequence = "git operation"
)

// IsInProgress reports whether anything is part-way through.
func (s Sequence) IsInProgress() bool {
	return s != sequenceNone
}

// Agent is one agent as the server listed it.
type Agent struct {
	ID             string
	Name           string
	Pane           string
	Cwd            string
	Flags          string
	Prompt         string
	Role           role
	State          State
	StateChangedAt time.Time
	FailureReason  string
	TurnError      string
	Harness        string
	SessionID      string
	WorkflowID     string
	ScheduleID     *string
	CreatedAt      time.Time
	ArchivedAt     *time.Time
	// TaskID is the task the agent works on, empty for an orchestrator.
	TaskID string
	// ProvisionDisabled is whether it was told to keep the directory it was given rather than be provisioned a worktree.
	ProvisionDisabled bool
	// Tool is the tool its CLI last reported running, empty when it reported none.
	Tool string
	// Hooks is the hooks komrad installs in its CLI.
	Hooks []string
}

// isCheckoutOwner reports whether the agent is the one its task's checkout was provisioned for.
func (a Agent) isCheckoutOwner() bool {
	return a.Role == RoleWriter && !a.ProvisionDisabled
}

// AgentRow is an agent as the listing renders it: the agent, whether its pane is alive, the mail
// waiting for it, and its unread failure.
type AgentRow struct {
	Agent      Agent
	Alive      bool
	UnreadMail bool
	FailedRun  bool
	// FailedRunLog is the run its unread failure was written to.
	FailedRunLog *int32
	// Documents is how many documents the agent has written.
	Documents int32
	// DocumentsUpdatedAt is when the newest of them was last written, nil when there are none.
	DocumentsUpdatedAt *time.Time
}

// DisplayState is the state a row wears, idle once the pane has gone even though the store
// still holds whatever the agent stopped in.
func (r AgentRow) DisplayState() State {
	state := r.Agent.State
	if !r.Alive && state.DoesPaneDecide() && state != StateErrored {
		return StateIdle
	}

	return state
}

// Status is what an agent is doing this second, read off the state the row wears rather than
// the one the store holds, so an agent whose pane has gone reads as idle.
func (r AgentRow) Status() view.Status {
	switch r.DisplayState() {
	case StateThinking, StateSettingUp:
		return view.StatusThinking
	case StateWaiting, StateUncertain:
		return view.StatusWaiting
	case StateFailedToCreate, StateFailedToCleanup, StateErrored:
		return view.StatusBroken
	case StateIdle:
		return view.StatusIdle
	}

	return view.StatusIdle
}

// Process reports whether anything is running behind a row, which the state it wears cannot
// say since an agent whose pane has gone wears the same idle as one sitting quiet.
func (r AgentRow) Process() view.Process {
	if !r.Alive && r.Agent.State.DoesPaneDecide() {
		return view.ProcessStopped
	}

	return view.ProcessAlive
}

// Facts fills in everything a list row says about the agent standing behind it.
func (r AgentRow) Facts(row view.Row) view.Row {
	row.Status = r.Status()
	row.Process = r.Process()
	row.Archived = r.Agent.ArchivedAt != nil
	row.UnreadMail = r.UnreadMail
	row.FailedRun = r.FailedRun
	row.Documents = r.Documents > 0
	row.Scheduled = r.Agent.ScheduleID != nil
	row.Tool = r.Agent.Tool

	return row
}

// IDs is the id of each agent, in the order the caller gave them.
func IDs(agents []Agent) []string {
	return xslice.Map(agents, func(agent Agent) string { return agent.ID })
}

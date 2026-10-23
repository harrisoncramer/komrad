// Package panel is what the server has told the panel, in the panel's own shape.
package panel

import (
	"time"

	"komrad/internal/view"
	"komrad/lib/xslice"
	agentv1 "komrad/proto/gen/agent/v1"
	taskv1 "komrad/proto/gen/task/v1"
)

// DoesPaneDecide reports whether reading the agent's terminal is what decides the state.
func DoesPaneDecide(state agentv1.AgentState) bool {
	switch state {
	case agentv1.AgentState_AGENT_STATE_IDLE, agentv1.AgentState_AGENT_STATE_THINKING, agentv1.AgentState_AGENT_STATE_WAITING, agentv1.AgentState_AGENT_STATE_UNCERTAIN, agentv1.AgentState_AGENT_STATE_ERRORED:
		return true
	case agentv1.AgentState_AGENT_STATE_SETTING_UP, agentv1.AgentState_AGENT_STATE_FAILED_TO_CREATE, agentv1.AgentState_AGENT_STATE_FAILED_TO_CLEANUP:
		return false
	case agentv1.AgentState_AGENT_STATE_UNSPECIFIED:
	}

	return false
}

// IsFailed reports whether a state is one of the two an agent reaches by failing.
func IsFailed(state agentv1.AgentState) bool {
	return state == agentv1.AgentState_AGENT_STATE_FAILED_TO_CREATE || state == agentv1.AgentState_AGENT_STATE_FAILED_TO_CLEANUP
}

// IsInProgress reports whether anything is part-way through.
func IsInProgress(sequence taskv1.Sequence) bool {
	return sequence != taskv1.Sequence_SEQUENCE_NONE && sequence != taskv1.Sequence_SEQUENCE_UNSPECIFIED
}

// DescribeSequence names a sequence as git's own command is, empty when nothing is part-way through.
func DescribeSequence(sequence taskv1.Sequence) string {
	switch sequence {
	case taskv1.Sequence_SEQUENCE_REBASE:
		return "rebase"
	case taskv1.Sequence_SEQUENCE_AM:
		return "am"
	case taskv1.Sequence_SEQUENCE_MERGE:
		return "merge"
	case taskv1.Sequence_SEQUENCE_CHERRY_PICK:
		return "cherry-pick"
	case taskv1.Sequence_SEQUENCE_REVERT:
		return "revert"
	case taskv1.Sequence_SEQUENCE_UNSPECIFIED, taskv1.Sequence_SEQUENCE_NONE:
		return ""
	}

	return "git operation"
}

// Agent is one agent as the server listed it.
type Agent struct {
	ID             string
	Name           string
	Pane           string
	Cwd            string
	Flags          string
	Prompt         string
	Role           agentv1.AgentRole
	State          agentv1.AgentState
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
	return a.Role == agentv1.AgentRole_AGENT_ROLE_WRITER && !a.ProvisionDisabled
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
func (r AgentRow) DisplayState() agentv1.AgentState {
	state := r.Agent.State
	if !r.Alive && DoesPaneDecide(state) && state != agentv1.AgentState_AGENT_STATE_ERRORED {
		return agentv1.AgentState_AGENT_STATE_IDLE
	}

	return state
}

// Status is what an agent is doing this second, read off the state the row wears rather than
// the one the store holds, so an agent whose pane has gone reads as idle.
func (r AgentRow) Status() view.Status {
	switch r.DisplayState() {
	case agentv1.AgentState_AGENT_STATE_THINKING, agentv1.AgentState_AGENT_STATE_SETTING_UP:
		return view.StatusThinking
	case agentv1.AgentState_AGENT_STATE_WAITING, agentv1.AgentState_AGENT_STATE_UNCERTAIN:
		return view.StatusWaiting
	case agentv1.AgentState_AGENT_STATE_FAILED_TO_CREATE, agentv1.AgentState_AGENT_STATE_FAILED_TO_CLEANUP, agentv1.AgentState_AGENT_STATE_ERRORED:
		return view.StatusBroken
	case agentv1.AgentState_AGENT_STATE_IDLE, agentv1.AgentState_AGENT_STATE_UNSPECIFIED:
		return view.StatusIdle
	}

	return view.StatusIdle
}

// Process reports whether anything is running behind a row, which the state it wears cannot
// say since an agent whose pane has gone wears the same idle as one sitting quiet.
func (r AgentRow) Process() view.Process {
	if !r.Alive && DoesPaneDecide(r.Agent.State) {
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

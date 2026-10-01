package ui

import (
	"testing"
	"time"

	"komrad/internal/ui/panel"
	agentv1 "komrad/proto/gen/agent/v1"
	workflowv1 "komrad/proto/gen/workflow/v1"
	worktreev1 "komrad/proto/gen/worktree/v1"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// TestProtoToAgentCarriesTheSyncedColumns pins the branch and the worktree removal flag
// surviving the conversion.
func TestProtoToAgentCarriesTheSyncedColumns(t *testing.T) {
	branch := "fix/thing"
	converted := agentFrom(&agentv1.Agent{
		Id:              "a1",
		Branch:          &branch,
		WorktreeRemoved: true,
	})

	require.NotNil(t, converted.Branch)
	assert.Equal(t, branch, *converted.Branch)
	assert.True(t, converted.WorktreeRemoved)
}

// TestProtoToAgentCarriesAFailedTurn pins the text a turn an API error ended is drawn with.
func TestProtoToAgentCarriesAFailedTurn(t *testing.T) {
	assert.Equal(t, "Overloaded", agentFrom(&agentv1.Agent{Id: "a1", TurnError: "Overloaded"}).TurnError)
}

// TestProtoToAgentCarriesTheStateChange pins the timestamp the row's state is dated by.
func TestProtoToAgentCarriesTheStateChange(t *testing.T) {
	changed := time.Date(2026, time.September, 1, 9, 9, 0, 0, time.UTC)
	converted := agentFrom(&agentv1.Agent{
		Id:             "a1",
		StateChangedAt: changed.Format(time.RFC3339),
	})

	assert.True(t, converted.StateChangedAt.Equal(changed))
}

// TestProtoToAgentWithoutAStateChange covers a server that sent no timestamp, which leaves the
// zero time rather than an instant nothing dated it from.
func TestProtoToAgentWithoutAStateChange(t *testing.T) {
	assert.True(t, agentFrom(&agentv1.Agent{Id: "a1"}).StateChangedAt.IsZero())
}

// TestWorkflowsFromCarriesTheWholeWorkflow covers the listing conversion field by field.
func TestWorkflowsFromCarriesTheWholeWorkflow(t *testing.T) {
	got := workflowsFrom([]*workflowv1.Workflow{{
		Id:              "w1",
		Name:            "api",
		Context:         "shared notes",
		Harness:         "claude",
		Cwd:             "/work/api",
		Flags:           "--model opus",
		ReviewerHarness: "codex",
		ReviewerFlags:   "--model gpt-5",
		Worktree: &workflowv1.WorktreeConfig{
			Enabled:      true,
			BaseBranch:   "develop",
			CopyPaths:    []string{".env"},
			SymlinkPaths: []string{"node_modules"},
			DiffExclude:  []string{"proto/gen"},
		},
		Hooks: []*workflowv1.WorkflowHook{
			{Event: "pre-launch", Script: "install-deps"},
			{Event: "post-launch", Script: "announce"},
		},
		Prompts: []*workflowv1.WorkflowPrompt{{Notice: "review.approved", Prompt: "open the pull request"}},
	}})

	require.Len(t, got, 1)
	assert.Equal(t, panel.Workflow{
		ID:              "w1",
		Name:            "api",
		Context:         "shared notes",
		Harness:         "claude",
		Cwd:             "/work/api",
		Flags:           "--model opus",
		ReviewerHarness: "codex",
		ReviewerFlags:   "--model gpt-5",
		Hooks: []panel.Hook{
			{Event: "pre-launch", Script: "install-deps"},
			{Event: "post-launch", Script: "announce"},
		},
		Prompts: []panel.Prompt{{Notice: "review.approved", Prompt: "open the pull request"}},
		Worktree: panel.WorktreeConfig{
			Enabled:      true,
			BaseBranch:   "develop",
			CopyPaths:    []string{".env"},
			SymlinkPaths: []string{"node_modules"},
			DiffExclude:  []string{"proto/gen"},
		},
	}, got[0])
}

// TestStateFromReadsEveryWireState pins each wire state to the one the panel keeps, and every
// value the wire can carry to one of them.
func TestStateFromReadsEveryWireState(t *testing.T) {
	t.Parallel()

	states := map[agentv1.AgentState]panel.State{
		agentv1.AgentState_AGENT_STATE_UNSPECIFIED:       panel.StateUncertain,
		agentv1.AgentState_AGENT_STATE_IDLE:              panel.StateIdle,
		agentv1.AgentState_AGENT_STATE_THINKING:          panel.StateThinking,
		agentv1.AgentState_AGENT_STATE_WAITING:           panel.StateWaiting,
		agentv1.AgentState_AGENT_STATE_SETTING_UP:        panel.StateSettingUp,
		agentv1.AgentState_AGENT_STATE_FAILED_TO_CREATE:  panel.StateFailedToCreate,
		agentv1.AgentState_AGENT_STATE_FAILED_TO_CLEANUP: panel.StateFailedToCleanup,
		agentv1.AgentState_AGENT_STATE_UNCERTAIN:         panel.StateUncertain,
		agentv1.AgentState_AGENT_STATE_ERRORED:           panel.StateErrored,
	}
	assert.Len(t, states, len(agentv1.AgentState_name), "every wire state is answered for")
	for wire, want := range states {
		assert.Equal(t, want, stateFrom(wire), wire.String())
	}
}

// TestRoleFromReadsEveryWireRole pins each wire role to the one the panel keeps.
func TestRoleFromReadsEveryWireRole(t *testing.T) {
	t.Parallel()

	roles := map[agentv1.AgentRole]panel.Role{
		agentv1.AgentRole_AGENT_ROLE_UNSPECIFIED:  "",
		agentv1.AgentRole_AGENT_ROLE_IMPLEMENTER:  panel.RoleImplementer,
		agentv1.AgentRole_AGENT_ROLE_REVIEWER:     panel.RoleReviewer,
		agentv1.AgentRole_AGENT_ROLE_ORCHESTRATOR: panel.RoleOrchestrator,
	}
	assert.Len(t, roles, len(agentv1.AgentRole_name), "every wire role is answered for")
	for wire, want := range roles {
		assert.Equal(t, want, roleFrom(wire), wire.String())
	}
}

// TestWorktreeFateIsReadOffTheWire covers each fate the server names landing on the item, and
// one it does not name reading as unowned.
func TestWorktreeFateIsReadOffTheWire(t *testing.T) {
	for wire, want := range map[worktreev1.WorktreeFate]worktreeFate{
		worktreev1.WorktreeFate_WORKTREE_FATE_LIVE:        fateLive,
		worktreev1.WorktreeFate_WORKTREE_FATE_ARCHIVED:    fateArchived,
		worktreev1.WorktreeFate_WORKTREE_FATE_UNOWNED:     fateUnowned,
		worktreev1.WorktreeFate_WORKTREE_FATE_UNSPECIFIED: fateUnowned,
	} {
		item := worktreeItemFrom(&worktreev1.Worktree{Dir: "/trees/a", Fate: wire})
		assert.Equal(t, want, item.fate, wire.String())
	}
}

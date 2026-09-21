package ui

import (
	"testing"
	"time"

	"weave/internal/rpc/render"
	agentstore "weave/internal/store/agent"
	workflowstore "weave/internal/store/workflow"
	agentv1 "weave/proto/gen/agent/v1"
	workflowv1 "weave/proto/gen/workflow/v1"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// TestProtoToAgentCarriesTheSyncedColumns pins the branch and the worktree removal flag
// surviving the conversion.
func TestProtoToAgentCarriesTheSyncedColumns(t *testing.T) {
	branch := "fix/thing"
	converted := protoToAgent(&agentv1.Agent{
		Id:              "a1",
		Branch:          &branch,
		WorktreeRemoved: true,
	})

	require.NotNil(t, converted.Branch)
	assert.Equal(t, branch, *converted.Branch)
	assert.True(t, converted.WorktreeRemoved)
}

// TestProtoToAgentCarriesTheStateChange pins the timestamp the row's state is dated by.
func TestProtoToAgentCarriesTheStateChange(t *testing.T) {
	changed := time.Date(2026, time.September, 1, 9, 9, 0, 0, time.UTC)
	converted := protoToAgent(&agentv1.Agent{
		Id:             "a1",
		StateChangedAt: changed.Format(time.RFC3339),
	})

	assert.True(t, converted.StateChangedAt.Equal(changed))
}

// TestProtoToAgentWithoutAStateChange covers a server that sent no timestamp, which leaves the
// zero time rather than an instant nothing dated it from.
func TestProtoToAgentWithoutAStateChange(t *testing.T) {
	assert.True(t, protoToAgent(&agentv1.Agent{Id: "a1"}).StateChangedAt.IsZero())
}

// TestWorkflowsFromCarriesTheWholeWorkflow covers the listing conversion field by field.
func TestWorkflowsFromCarriesTheWholeWorkflow(t *testing.T) {
	got := workflowsFrom([]*workflowv1.Workflow{{
		Id:      "w1",
		Name:    "api",
		Context: "shared notes",
		Harness: "claude",
		Cwd:     "/work/api",
		Flags:   "--model opus",
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
	}})

	require.Len(t, got, 1)
	assert.Equal(t, workflowstore.Workflow{
		ID:      "w1",
		Name:    "api",
		Context: "shared notes",
		Harness: "claude",
		Cwd:     "/work/api",
		Flags:   "--model opus",
		Hooks: []workflowstore.Hook{
			{Event: "pre-launch", Script: "install-deps"},
			{Event: "post-launch", Script: "announce"},
		},
		Worktree: workflowstore.WorktreeConfig{
			Enabled:      true,
			BaseBranch:   "develop",
			CopyPaths:    []string{".env"},
			SymlinkPaths: []string{"node_modules"},
			DiffExclude:  []string{"proto/gen"},
		},
	}, got[0])
}

// TestStateFromRoundTripsEveryStoredState pins the panel's reading of a state to the service's
// rendering of it.
func TestStateFromRoundTripsEveryStoredState(t *testing.T) {
	t.Parallel()

	states := []agentstore.State{
		agentstore.StateIdle,
		agentstore.StateThinking,
		agentstore.StateWaiting,
		agentstore.StateSettingUp,
		agentstore.StateFailedToCreate,
		agentstore.StateFailedToCleanup,
		agentstore.StateUncertain,
	}
	for _, state := range states {
		assert.Equal(t, state, stateFrom(render.AgentStateOf(state)), string(state))
	}
}

// TestRoleFromRoundTripsEveryStoredRole pins the panel's reading of a role to the service's
// rendering of it.
func TestRoleFromRoundTripsEveryStoredRole(t *testing.T) {
	t.Parallel()

	roles := []agentstore.Role{
		agentstore.RoleImplementer,
		agentstore.RoleReviewer,
		agentstore.RoleOrchestrator,
	}
	for _, role := range roles {
		assert.Equal(t, role, roleFrom(render.AgentRoleOf(role)), string(role))
	}
}

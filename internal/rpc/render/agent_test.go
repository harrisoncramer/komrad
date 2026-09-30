package render

import (
	"strings"
	"testing"
	"time"

	"github.com/google/go-cmp/cmp"
	"github.com/stretchr/testify/assert"
	"google.golang.org/protobuf/testing/protocmp"

	agentstore "komrad/internal/store/agent"
	rowstore "komrad/internal/store/row"
	"komrad/lib/xptr"
	agentv1 "komrad/proto/gen/agent/v1"
)

// TestAgent pins the fields a client cannot draw unless the response carries them.
func TestAgent(t *testing.T) {
	t.Parallel()

	changed := time.Date(2026, time.September, 1, 9, 9, 0, 0, time.UTC)

	written := time.Date(2026, time.September, 1, 9, 9, 0, 250_000_000, time.UTC)

	tests := []struct {
		name   string
		stored agentstore.Agent
		beside rowstore.Row
		want   *agentv1.Agent
	}{
		{
			name: "an agent nothing has written to",
			stored: agentstore.Agent{
				ID:   "a2",
				Name: "reviewer",
			},
			want: &agentv1.Agent{
				Id:    "a2",
				Name:  "reviewer",
				Alive: xptr.Ptr(false),
			},
		},
		{
			name: "an agent the syncers have written to",
			stored: agentstore.Agent{
				ID:              "a1",
				Name:            "worker",
				State:           agentstore.StateWaiting,
				Role:            agentstore.RoleReviewer,
				StateChangedAt:  changed,
				Branch:          xptr.Ptr("fix/thing"),
				WorktreeRemoved: true,
			},
			want: &agentv1.Agent{
				Id:              "a1",
				Name:            "worker",
				State:           agentv1.AgentState_AGENT_STATE_WAITING,
				Role:            agentv1.AgentRole_AGENT_ROLE_REVIEWER,
				StateChangedAt:  changed.Format(time.RFC3339),
				Branch:          xptr.Ptr("fix/thing"),
				WorktreeRemoved: true,
				Alive:           xptr.Ptr(false),
			},
		},
		{
			name: "an agent whose last turn ended after one that failed",
			stored: agentstore.Agent{
				ID:           "a4",
				Name:         "recovered",
				TurnEndedAt:  xptr.Ptr(changed.Add(time.Minute)),
				TurnFailedAt: xptr.Ptr(changed),
				TurnError:    "Overloaded",
			},
			want: &agentv1.Agent{
				Id:    "a4",
				Name:  "recovered",
				Alive: xptr.Ptr(false),
			},
		},
		{
			name: "an agent whose last turn failed",
			stored: agentstore.Agent{
				ID:           "a5",
				Name:         "failed",
				State:        agentstore.StateErrored,
				TurnEndedAt:  xptr.Ptr(changed),
				TurnFailedAt: xptr.Ptr(changed),
				TurnError:    "Overloaded",
			},
			want: &agentv1.Agent{
				Id:        "a5",
				Name:      "failed",
				State:     agentv1.AgentState_AGENT_STATE_ERRORED,
				TurnError: "Overloaded",
				Alive:     xptr.Ptr(false),
			},
		},
		{
			name: "an agent that has written documents, stamped to within the second",
			stored: agentstore.Agent{
				ID:   "a3",
				Name: "author",
			},
			beside: rowstore.Row{
				Documents:          2,
				DocumentsUpdatedAt: &written,
			},
			want: &agentv1.Agent{
				Id:                 "a3",
				Name:               "author",
				Alive:              xptr.Ptr(false),
				Documents:          2,
				DocumentsUpdatedAt: xptr.Ptr("2026-09-01T09:09:00.25Z"),
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			row := tt.beside
			row.Agent = tt.stored
			assert.Empty(t, cmp.Diff(tt.want, Agent(row, AgentOptions{}), protocmp.Transform()))
		})
	}
}

// TestWatchedAgent pins that the watch cuts an archived agent's prompt to its opening and leaves a live one whole.
func TestWatchedAgent(t *testing.T) {
	t.Parallel()

	prompt := strings.Repeat("p", SummaryHead*2)

	tests := []struct {
		name          string
		archivedAt    *time.Time
		wantPrompt    string
		wantTruncated bool
	}{
		{
			name:          "an archived agent",
			archivedAt:    xptr.Ptr(time.Date(2026, 9, 21, 12, 0, 0, 0, time.UTC)),
			wantPrompt:    prompt[:SummaryHead],
			wantTruncated: true,
		},
		{
			name:       "a live agent",
			wantPrompt: prompt,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			rendered := WatchedAgent(rowOf(agentstore.Agent{
				ID:         "a1",
				Prompt:     prompt,
				ArchivedAt: tt.archivedAt,
			}))

			assert.Equal(t, tt.wantPrompt, rendered.GetPrompt())
			assert.Equal(t, tt.wantTruncated, rendered.GetPromptTruncated())
		})
	}
}

// rowOf is an agent's row with nothing beside it.
func rowOf(stored agentstore.Agent) rowstore.Row {
	return rowstore.Row{Agent: stored}
}

// TestAgentStateOf checks every stored state lands on its own wire value.
func TestAgentStateOf(t *testing.T) {
	t.Parallel()

	states := map[agentstore.State]agentv1.AgentState{
		agentstore.StateIdle:            agentv1.AgentState_AGENT_STATE_IDLE,
		agentstore.StateThinking:        agentv1.AgentState_AGENT_STATE_THINKING,
		agentstore.StateWaiting:         agentv1.AgentState_AGENT_STATE_WAITING,
		agentstore.StateSettingUp:       agentv1.AgentState_AGENT_STATE_SETTING_UP,
		agentstore.StateFailedToCreate:  agentv1.AgentState_AGENT_STATE_FAILED_TO_CREATE,
		agentstore.StateFailedToCleanup: agentv1.AgentState_AGENT_STATE_FAILED_TO_CLEANUP,
		agentstore.StateUncertain:       agentv1.AgentState_AGENT_STATE_UNCERTAIN,
		agentstore.StateErrored:         agentv1.AgentState_AGENT_STATE_ERRORED,
	}
	for stored, wire := range states {
		assert.Equal(t, wire, AgentStateOf(stored), string(stored))
	}
	assert.Equal(t, agentv1.AgentState_AGENT_STATE_UNSPECIFIED, AgentStateOf(""))
}

// TestAgentRoleOf checks every stored role lands on its own wire value.
func TestAgentRoleOf(t *testing.T) {
	t.Parallel()

	roles := map[agentstore.Role]agentv1.AgentRole{
		agentstore.RoleImplementer:  agentv1.AgentRole_AGENT_ROLE_IMPLEMENTER,
		agentstore.RoleReviewer:     agentv1.AgentRole_AGENT_ROLE_REVIEWER,
		agentstore.RoleOrchestrator: agentv1.AgentRole_AGENT_ROLE_ORCHESTRATOR,
	}
	for stored, wire := range roles {
		assert.Equal(t, wire, AgentRoleOf(stored), string(stored))
	}
	assert.Equal(t, agentv1.AgentRole_AGENT_ROLE_UNSPECIFIED, AgentRoleOf(""))
}

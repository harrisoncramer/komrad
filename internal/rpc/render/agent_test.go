package render

import (
	"strings"
	"testing"
	"time"

	agentstore "komrad/internal/store/agent"
	rowstore "komrad/internal/store/row"
	"komrad/lib/xptr"
	agentv1 "komrad/proto/gen/agent/v1"

	"github.com/google/go-cmp/cmp"
	"github.com/google/uuid"
	"github.com/stretchr/testify/assert"
	"google.golang.org/protobuf/testing/protocmp"
	"google.golang.org/protobuf/types/known/timestamppb"
)

// TestAgent pins the fields a client cannot draw unless the response carries them.
func TestAgent(t *testing.T) {
	t.Parallel()

	changed := time.Date(2026, time.September, 1, 9, 9, 0, 0, time.UTC)

	written := time.Date(2026, time.September, 1, 9, 9, 0, 250_000_000, time.UTC)

	tests := []struct {
		name    string
		stored  agentstore.Agent
		beside  rowstore.Row
		options AgentOptions
		want    *agentv1.Agent
	}{
		{
			name: "an agent nothing has written to",
			stored: agentstore.Agent{
				ID:         secondAgentID,
				WorkflowID: workflowID,
				Name:       "reviewer",
			},
			want: &agentv1.Agent{
				Id:         secondAgentID.String(),
				WorkflowId: workflowID.String(),
				Name:       "reviewer",
				Alive:      xptr.Ptr(false),
			},
		},
		{
			name: "an agent whose state was written",
			stored: agentstore.Agent{
				ID:             firstAgentID,
				WorkflowID:     workflowID,
				Name:           "worker",
				State:          agentv1.AgentState_AGENT_STATE_WAITING,
				Role:           agentv1.AgentRole_AGENT_ROLE_REVIEWER,
				StateChangedAt: changed,
			},
			want: &agentv1.Agent{
				Id:             firstAgentID.String(),
				WorkflowId:     workflowID.String(),
				Name:           "worker",
				State:          agentv1.AgentState_AGENT_STATE_WAITING,
				Role:           agentv1.AgentRole_AGENT_ROLE_REVIEWER,
				StateChangedAt: timestamppb.New(changed),
				Alive:          xptr.Ptr(false),
			},
		},
		{
			name: "an agent whose last turn ended after one that failed",
			stored: agentstore.Agent{
				ID:         fourthAgentID,
				WorkflowID: workflowID,
				Name:       "recovered",
				Turn: agentstore.Turn{
					EndedAt:  xptr.Ptr(changed.Add(time.Minute)),
					FailedAt: xptr.Ptr(changed),
					Error:    "Overloaded",
				},
			},
			want: &agentv1.Agent{
				Id:         fourthAgentID.String(),
				WorkflowId: workflowID.String(),
				Name:       "recovered",
				Alive:      xptr.Ptr(false),
			},
		},
		{
			name: "an agent whose last turn failed",
			stored: agentstore.Agent{
				ID:         fifthAgentID,
				WorkflowID: workflowID,
				Name:       "failed",
				State:      agentv1.AgentState_AGENT_STATE_ERRORED,
				Turn: agentstore.Turn{
					EndedAt:  xptr.Ptr(changed),
					FailedAt: xptr.Ptr(changed),
					Error:    "Overloaded",
				},
			},
			want: &agentv1.Agent{
				Id:         fifthAgentID.String(),
				WorkflowId: workflowID.String(),
				Name:       "failed",
				State:      agentv1.AgentState_AGENT_STATE_ERRORED,
				TurnError:  "Overloaded",
				Alive:      xptr.Ptr(false),
			},
		},
		{
			name: "an agent that has written documents, stamped to within the second",
			stored: agentstore.Agent{
				ID:         thirdAgentID,
				WorkflowID: workflowID,
				Name:       "author",
			},
			beside: rowstore.Row{
				Documents:          2,
				DocumentsUpdatedAt: &written,
			},
			want: &agentv1.Agent{
				Id:                 thirdAgentID.String(),
				WorkflowId:         workflowID.String(),
				Name:               "author",
				Alive:              xptr.Ptr(false),
				Documents:          2,
				DocumentsUpdatedAt: timestamppb.New(time.Date(2026, 9, 1, 9, 9, 0, 250_000_000, time.UTC)),
			},
		},
		{
			name: "an agent whose launch installed hooks",
			stored: agentstore.Agent{
				ID:         sixthAgentID,
				WorkflowID: workflowID,
				Name:       "hooked",
				Hooks:      []string{"PreToolUse", "Stop"},
			},
			want: &agentv1.Agent{
				Id:         sixthAgentID.String(),
				WorkflowId: workflowID.String(),
				Name:       "hooked",
				Alive:      xptr.Ptr(false),
				Hooks:      []string{"PreToolUse", "Stop"},
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			row := tt.beside
			row.Agent = tt.stored
			assert.Empty(t, cmp.Diff(tt.want, Agent(row, tt.options), protocmp.Transform()))
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
				ID:         firstAgentID,
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

var (
	workflowID    = uuid.New()
	fifthAgentID  = uuid.New()
	firstAgentID  = uuid.New()
	fourthAgentID = uuid.New()
	secondAgentID = uuid.New()
	sixthAgentID  = uuid.New()
	thirdAgentID  = uuid.New()
)

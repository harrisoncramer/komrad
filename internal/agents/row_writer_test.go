package agents

import (
	"context"
	"testing"

	"weave/internal/store"
	"weave/lib/xptr"

	agentstore "weave/internal/store/agent"

	"github.com/google/uuid"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// recordingReconciler records what the row writer tells the reconcile loop.
type recordingReconciler struct {
	boundaries []string
	reasons    []string
}

func (r *recordingReconciler) RecordTurnBoundary(agentID string) {
	r.boundaries = append(r.boundaries, agentID)
}

func (r *recordingReconciler) RecordReportedReason(_, reason string) {
	r.reasons = append(r.reasons, reason)
}

// TestRowWriterRefusals pins each write the row writer turns away.
func TestRowWriterRefusals(t *testing.T) {
	tests := []struct {
		name    string
		write   func(ctx context.Context, writer *RowWriter, agent agentstore.Agent) error
		wantErr error
	}{
		{
			name: "hiding an id nobody holds",
			write: func(ctx context.Context, writer *RowWriter, _ agentstore.Agent) error {
				return writer.SetHidden(ctx, uuid.NewString(), true)
			},
			wantErr: ErrAgentNotFound,
		},
		{
			name: "recording a fact on an id nobody holds",
			write: func(ctx context.Context, writer *RowWriter, _ agentstore.Agent) error {
				return writer.SetMetadata(ctx, SetMetadataParams{
					AgentID: uuid.NewString(),
					Key:     "linear_issue",
					Value:   xptr.Ptr("https://linear.app/x/issue/ENG-1"),
				})
			},
			wantErr: ErrAgentNotFound,
		},
		{
			name: "recording the pull request key the forge fills",
			write: func(ctx context.Context, writer *RowWriter, agent agentstore.Agent) error {
				return writer.SetMetadata(ctx, SetMetadataParams{
					AgentID: agent.ID,
					Key:     pullRequestMetadataKey,
					Value:   xptr.Ptr("https://github.com/o/r/pull/1"),
				})
			},
			wantErr: ErrPullRequestKey,
		},
		{
			name: "clearing the pull request key",
			write: func(ctx context.Context, writer *RowWriter, agent agentstore.Agent) error {
				return writer.SetMetadata(ctx, SetMetadataParams{
					AgentID: agent.ID,
					Key:     pullRequestMetadataKey,
				})
			},
			wantErr: ErrPullRequestKey,
		},
		{
			name: "renaming an id nobody holds",
			write: func(ctx context.Context, writer *RowWriter, _ agentstore.Agent) error {
				return writer.Rename(ctx, uuid.NewString(), "renamed")
			},
			wantErr: ErrAgentNotFound,
		},
		{
			name: "renaming onto a name a live agent in the workflow holds",
			write: func(ctx context.Context, writer *RowWriter, agent agentstore.Agent) error {
				return writer.Rename(ctx, agent.ID, "beta")
			},
			wantErr: store.ErrNameTaken,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			st := newStore(t)
			workflowID := seedWorkflow(t, st)
			alpha := seedInWorkflow(t, st, workflowID, "alpha", "", agentstore.StateIdle)
			seedInWorkflow(t, st, workflowID, "beta", "", agentstore.StateIdle)
			writer := newRowWriter(st, &recordingReconciler{})

			require.ErrorIs(t, tt.write(t.Context(), writer, alpha), tt.wantErr)
		})
	}
}

// TestRowWriterWrites pins what each accepted write leaves on the row.
func TestRowWriterWrites(t *testing.T) {
	tests := []struct {
		name  string
		write func(ctx context.Context, writer *RowWriter, agent agentstore.Agent) error
		check func(t *testing.T, row agentstore.Agent)
	}{
		{
			name: "clearing a key nothing held is served",
			write: func(ctx context.Context, writer *RowWriter, agent agentstore.Agent) error {
				return writer.SetMetadata(ctx, SetMetadataParams{
					AgentID: agent.ID,
					Key:     "linear_issue",
				})
			},
			check: func(t *testing.T, row agentstore.Agent) {
				assert.Empty(t, row.Metadata)
			},
		},
		{
			name: "hiding",
			write: func(ctx context.Context, writer *RowWriter, agent agentstore.Agent) error {
				return writer.SetHidden(ctx, agent.ID, true)
			},
			check: func(t *testing.T, row agentstore.Agent) {
				assert.True(t, row.Hidden)
			},
		},
		{
			name: "recording a fact",
			write: func(ctx context.Context, writer *RowWriter, agent agentstore.Agent) error {
				return writer.SetMetadata(ctx, SetMetadataParams{
					AgentID: agent.ID,
					Key:     "linear_issue",
					Value:   xptr.Ptr("https://linear.app/x/issue/ENG-1"),
				})
			},
			check: func(t *testing.T, row agentstore.Agent) {
				assert.Equal(t, []agentstore.Metadata{
					{
						Key:   "linear_issue",
						Value: "https://linear.app/x/issue/ENG-1",
					},
				}, row.Metadata)
			},
		},
		{
			name: "renaming keeps the id",
			write: func(ctx context.Context, writer *RowWriter, agent agentstore.Agent) error {
				return writer.Rename(ctx, agent.ID, "renamed")
			},
			check: func(t *testing.T, row agentstore.Agent) {
				assert.Equal(t, "renamed", row.Name)
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			st := newStore(t)
			alpha := seed(t, st, "alpha", "", agentstore.StateIdle)
			writer := newRowWriter(st, &recordingReconciler{})

			require.NoError(t, tt.write(t.Context(), writer, alpha))
			tt.check(t, agentByID(t, st, alpha.ID))
		})
	}
}

// TestRecordReport pins that a report reaches the reconciler and the row, and that only a turn
// end stamps the turn.
func TestRecordReport(t *testing.T) {
	tests := []struct {
		name           string
		params         RecordReportParams
		wantBoundaries int
		wantReason     string
		wantTurnEnded  bool
	}{
		{
			name: "a reason alone claims nothing about the turn",
			params: RecordReportParams{
				Reason: "Bash",
			},
			wantReason: "Bash",
		},
		{
			name: "a turn end stamps the turn and leaves no reason",
			params: RecordReportParams{
				TurnEnded: true,
			},
			wantBoundaries: 1,
			wantTurnEnded:  true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			st := newStore(t)
			alpha := seed(t, st, "alpha", "", agentstore.StateThinking)
			require.NoError(t, st.Agents.SetReportedReason(t.Context(), alpha.ID, "Read"))
			reconciler := &recordingReconciler{}

			params := tt.params
			params.AgentID = alpha.ID
			require.NoError(t, newRowWriter(st, reconciler).RecordReport(t.Context(), params))

			assert.Len(t, reconciler.boundaries, tt.wantBoundaries)
			assert.Equal(t, []string{tt.wantReason}, reconciler.reasons)

			row := agentByID(t, st, alpha.ID)
			assert.Equal(t, tt.wantReason, row.ReportedReason)
			assert.Equal(t, tt.wantTurnEnded, row.TurnEndedAt != nil)
		})
	}
}

func newRowWriter(st *testStore, reconciler *recordingReconciler) *RowWriter {
	return NewRowWriter(NewRowWriterParams{
		Agents:     st.Agents,
		Runs:       st.Runs,
		Reconciler: reconciler,
	})
}

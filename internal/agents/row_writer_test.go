package agents

import (
	"context"
	"testing"

	"komrad/internal/agenterr"
	"komrad/internal/harness"
	"komrad/internal/store"
	agentstore "komrad/internal/store/agent"
	rowstore "komrad/internal/store/row"
	runstore "komrad/internal/store/run"
	"komrad/internal/waketest"

	"github.com/google/uuid"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// rowWriterFixture is a row writer and the fakes it was built over.
type rowWriterFixture struct {
	writer         *RowWriter
	cwdSync        *waketest.Counter
	transcriptSync *waketest.Counter
}

// TestRowWriterRefusals pins each write the row writer turns away.
func TestRowWriterRefusals(t *testing.T) {
	tests := []struct {
		name    string
		write   func(ctx context.Context, writer *RowWriter, agent agentstore.Agent) error
		wantErr error
	}{
		{
			name: "renaming an id nobody holds",
			write: func(ctx context.Context, writer *RowWriter, _ agentstore.Agent) error {
				return writer.Rename(ctx, uuid.NewString(), "renamed")
			},
			wantErr: agenterr.ErrAgentNotFound,
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
			writer := newRowWriter(st).writer

			require.ErrorIs(t, tt.write(t.Context(), writer, alpha), tt.wantErr)
		})
	}
}

// TestRowWriterWrites pins what each accepted write leaves on the row.
func TestRowWriterWrites(t *testing.T) {
	tests := []struct {
		name  string
		write func(ctx context.Context, writer *RowWriter, agent agentstore.Agent) error
		check func(t *testing.T, row rowstore.Row)
	}{
		{
			name: "claiming the failed runs",
			write: func(ctx context.Context, writer *RowWriter, agent agentstore.Agent) error {
				return writer.MarkRunsRead(ctx, agent.ID)
			},
			check: func(t *testing.T, row rowstore.Row) {
				assert.False(t, row.FailedRun)
			},
		},
		{
			name: "renaming keeps the id",
			write: func(ctx context.Context, writer *RowWriter, agent agentstore.Agent) error {
				return writer.Rename(ctx, agent.ID, "renamed")
			},
			check: func(t *testing.T, row rowstore.Row) {
				assert.Equal(t, "renamed", row.Name)
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			st := newStore(t)
			alpha := seed(t, st, "alpha", "", agentstore.StateIdle)
			failed, err := st.Runs.Begin(t.Context(), runstore.BeginParams{
				AgentID: alpha.ID,
				Trigger: "agent.post-launch",
				Script:  "install-deps",
			})
			require.NoError(t, err)
			require.NoError(t, st.Runs.Finish(t.Context(), runstore.FinishParams{
				ID:      failed,
				Failure: "a hook failed",
			}))
			writer := newRowWriter(st).writer

			require.NoError(t, tt.write(t.Context(), writer, alpha))

			row, err := st.Rows.Get(t.Context(), alpha.ID)
			require.NoError(t, err)
			tt.check(t, *row)
		})
	}
}

// TestRecordReport pins that a report reaches the row, that only a tool starting or waiting puts
// its tool there, and that only a turn end stamps the turn and wakes the loops that read what it
// left behind.
func TestRecordReport(t *testing.T) {
	tests := []struct {
		name           string
		report         harness.Report
		wantReason     string
		wantTurnEnded  bool
		wantTurnFailed bool
		wantWakes      int
	}{
		{
			name:       "a tool starting claims nothing about the turn",
			report:     harness.Report{Event: harness.ReportToolStarted, Tool: "Bash"},
			wantReason: "Bash",
		},
		{
			name:       "a tool waiting on a person is drawn the same way",
			report:     harness.Report{Event: harness.ReportApprovalWanted, Tool: "Edit"},
			wantReason: "Edit",
		},
		{
			name:   "a tool finishing takes the tool off the row",
			report: harness.Report{Event: harness.ReportToolFinished, Tool: "Read"},
		},
		{
			name:       "a compaction leaves the row as it was",
			report:     harness.Report{Event: harness.ReportCompacted},
			wantReason: "Read",
		},
		{
			name:          "a turn end stamps the turn and leaves no tool",
			report:        harness.Report{Event: harness.ReportTurnEnded},
			wantTurnEnded: true,
			wantWakes:     1,
		},
		{
			name:           "a failed turn stamps the turn as failed",
			report:         harness.Report{Event: harness.ReportTurnFailed, TurnError: "Overloaded"},
			wantTurnEnded:  true,
			wantTurnFailed: true,
			wantWakes:      1,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			st := newStore(t)
			alpha := seed(t, st, "alpha", "", agentstore.StateThinking)
			require.NoError(t, st.Agents.SetReportedReason(t.Context(), alpha.ID, "Read"))
			fixture := newRowWriter(st)

			require.NoError(t, fixture.writer.RecordReport(t.Context(), RecordReportParams{AgentID: alpha.ID, Report: tt.report}))

			row := agentByID(t, st, alpha.ID)
			assert.Equal(t, tt.wantReason, row.ReportedReason)
			assert.Equal(t, tt.wantTurnEnded, row.Turn.EndedAt != nil)
			assert.Equal(t, tt.wantTurnFailed, row.Turn.IsFailed())
			assert.Equal(t, tt.wantWakes, fixture.cwdSync.Wakes)
			assert.Equal(t, tt.wantWakes, fixture.transcriptSync.Wakes)
		})
	}
}

func newRowWriter(st *testStore) rowWriterFixture {
	fixture := rowWriterFixture{
		cwdSync:        &waketest.Counter{},
		transcriptSync: &waketest.Counter{},
	}
	fixture.writer = NewRowWriter(NewRowWriterParams{
		Agents:         st.Agents,
		Runs:           st.Runs,
		CwdSync:        fixture.cwdSync,
		TranscriptSync: fixture.transcriptSync,
	})

	return fixture
}

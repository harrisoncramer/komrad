package interceptors

import (
	"context"
	"testing"

	agentv1 "komrad/proto/gen/agent/v1"
	"komrad/proto/gen/agent/v1/agentv1connect"
	annotationv1 "komrad/proto/gen/annotation/v1"
	"komrad/proto/gen/schedule/v1/schedulev1connect"

	"komrad/internal/caller"

	"connectrpc.com/connect"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// TestRoles checks which agents are refused a method by the roles it names.
func TestRoles(t *testing.T) {
	t.Parallel()

	roles := map[string]annotationv1.Role{
		"implementer":  annotationv1.Role_ROLE_IMPLEMENTER,
		"reviewer":     annotationv1.Role_ROLE_REVIEWER,
		"orchestrator": annotationv1.Role_ROLE_ORCHESTRATOR,
	}

	tests := []struct {
		name       string
		agentID    string
		procedure  string
		wantErr    bool
		wantLookup bool
	}{
		{
			name:       "a reviewer is refused a document",
			agentID:    "reviewer",
			procedure:  agentv1connect.AgentServiceWriteDocumentProcedure,
			wantErr:    true,
			wantLookup: true,
		},
		{
			name:       "an implementer writes a document",
			agentID:    "implementer",
			procedure:  agentv1connect.AgentServiceWriteDocumentProcedure,
			wantLookup: true,
		},
		{
			name:       "an orchestrator writes a document",
			agentID:    "orchestrator",
			procedure:  agentv1connect.AgentServiceWriteDocumentProcedure,
			wantLookup: true,
		},
		{
			name:      "a reviewer reads a document without its role being looked up",
			agentID:   "reviewer",
			procedure: agentv1connect.AgentServiceGetDocumentProcedure,
		},
		{
			name:       "an implementer is refused a schedule",
			agentID:    "implementer",
			procedure:  schedulev1connect.ScheduleServiceCreateScheduleProcedure,
			wantErr:    true,
			wantLookup: true,
		},
		{
			name:      "a caller naming no agent is served everything",
			procedure: schedulev1connect.ScheduleServiceCreateScheduleProcedure,
		},
		{
			name:       "an agent with no role is served everything",
			agentID:    "unknown",
			procedure:  agentv1connect.AgentServiceWriteDocumentProcedure,
			wantLookup: true,
		},
		{
			name:      "a method naming no roles is open to every agent",
			agentID:   "reviewer",
			procedure: agentv1connect.AgentServiceRecordReportProcedure,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			lookedUp := false
			interceptor := NewRoles(func(_ context.Context, agentID string) annotationv1.Role {
				lookedUp = true

				return roles[agentID]
			})

			ctx := caller.With(t.Context(), caller.Caller{AgentID: tt.agentID})
			served := false
			handler := interceptor.WrapUnary(func(context.Context, connect.AnyRequest) (connect.AnyResponse, error) {
				served = true

				return connect.NewResponse(&agentv1.WriteDocumentResponse{}), nil
			})

			_, err := handler(ctx, &procedureRequest{
				AnyRequest: connect.NewRequest(&agentv1.WriteDocumentRequest{}),
				procedure:  tt.procedure,
			})
			assert.Equal(t, tt.wantLookup, lookedUp)

			if tt.wantErr {
				require.Error(t, err)
				assert.Equal(t, connect.CodePermissionDenied, connect.CodeOf(err))
				assert.False(t, served)

				return
			}

			require.NoError(t, err)
			assert.True(t, served)
		})
	}
}

type procedureRequest struct {
	connect.AnyRequest
	procedure string
}

func (r *procedureRequest) Spec() connect.Spec {
	return connect.Spec{Procedure: r.procedure}
}

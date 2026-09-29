package interceptors

import (
	"context"
	"errors"
	"testing"

	agentv1 "komrad/proto/gen/agent/v1"
	"komrad/proto/gen/agent/v1/agentv1connect"
	annotationv1 "komrad/proto/gen/annotation/v1"
	"komrad/proto/gen/schedule/v1/schedulev1connect"

	"komrad/internal/caller"
	"komrad/internal/rpcapi"

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
		wantCode   connect.Code
		wantLookup bool
	}{
		{
			name:       "a reviewer is refused a document",
			agentID:    "reviewer",
			procedure:  agentv1connect.AgentServiceWriteDocumentProcedure,
			wantCode:   connect.CodePermissionDenied,
			wantLookup: true,
		},
		{
			name:       "an implementer is refused a schedule",
			agentID:    "implementer",
			procedure:  schedulev1connect.ScheduleServiceCreateScheduleProcedure,
			wantCode:   connect.CodePermissionDenied,
			wantLookup: true,
		},
		{
			name:       "an agent komrad does not know is refused",
			agentID:    "unknown",
			procedure:  agentv1connect.AgentServiceWriteDocumentProcedure,
			wantCode:   connect.CodePermissionDenied,
			wantLookup: true,
		},
		{
			name:       "an agent whose role cannot be read is refused until it can",
			agentID:    "unreadable",
			procedure:  agentv1connect.AgentServiceWriteDocumentProcedure,
			wantCode:   connect.CodeUnavailable,
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
			name:      "a caller naming no agent is served everything",
			procedure: schedulev1connect.ScheduleServiceCreateScheduleProcedure,
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
			interceptor := NewRoles(func(_ context.Context, agentID string) (annotationv1.Role, error) {
				lookedUp = true

				switch agentID {
				case "unknown":
					return annotationv1.Role_ROLE_UNSPECIFIED, rpcapi.ErrUnknownCaller
				case "unreadable":
					return annotationv1.Role_ROLE_UNSPECIFIED, errors.New("connection refused")
				default:
					return roles[agentID], nil
				}
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

			if tt.wantCode != 0 {
				require.Error(t, err)
				assert.Equal(t, tt.wantCode, connect.CodeOf(err))
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

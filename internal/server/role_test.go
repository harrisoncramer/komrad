package server

import (
	"testing"

	annotationv1 "komrad/proto/gen/annotation/v1"

	"komrad/internal/rpcapi"
	agentstore "komrad/internal/store/agent"
	"komrad/internal/storetest"

	"github.com/google/uuid"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// TestEveryRoleIsMapped fails when a role reaches the server with nothing to place it by.
func TestEveryRoleIsMapped(t *testing.T) {
	assert.Len(t, toolRoles, len(agentstore.Roles))

	for _, role := range agentstore.Roles {
		assert.NotEqual(t, annotationv1.Role_ROLE_UNSPECIFIED, toolRoles[role], "role %q is not mapped", role)
	}
}

// TestRoleOf pins which callers komrad places by their row and which it refuses as unknown.
func TestRoleOf(t *testing.T) {
	stores := storetest.Open(t)
	roleOf := newRoleOf(stores.Agents)

	reviewer := stores.SeedAgent(t, agentstore.Agent{ID: uuid.NewString(), Name: "reviewer", Cwd: "/repo", Role: agentstore.RoleReviewer})
	archived := stores.SeedAgent(t, agentstore.Agent{ID: uuid.NewString(), Name: "archived", Cwd: "/repo", Role: agentstore.RoleOrchestrator})
	_, err := stores.Agents.Archive(t.Context(), []string{archived.ID})
	require.NoError(t, err)

	tests := []struct {
		name    string
		agentID string
		want    annotationv1.Role
		wantErr error
	}{
		{
			name:    "an id no row holds is unknown",
			agentID: uuid.NewString(),
			wantErr: rpcapi.ErrUnknownCaller,
		},
		{
			name:    "an id that is not an id is unknown",
			agentID: "not-an-id",
			wantErr: rpcapi.ErrUnknownCaller,
		},
		{
			name:    "a live agent is placed by its role",
			agentID: reviewer.ID,
			want:    annotationv1.Role_ROLE_REVIEWER,
		},
		{
			name:    "an archived agent still running keeps its role",
			agentID: archived.ID,
			want:    annotationv1.Role_ROLE_ORCHESTRATOR,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			role, err := roleOf(t.Context(), tt.agentID)
			if tt.wantErr != nil {
				require.ErrorIs(t, err, tt.wantErr)
				return
			}

			require.NoError(t, err)
			assert.Equal(t, tt.want, role)
		})
	}
}

package mcp

import (
	"encoding/json"
	"testing"

	annotationv1 "weave/proto/gen/annotation/v1"

	"weave/internal/rpcapi"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// toolsFor is the tools a caller holding the role is served, by name.
func toolsFor(t *testing.T, role annotationv1.Role) map[string]bool {
	t.Helper()

	served := map[string]bool{}
	for exposed := range rpcapi.GetMethods() {
		if exposed.Surfaces.GetMcp() && rpcapi.ServesRole(exposed.Surfaces, role) {
			served[exposed.Surfaces.GetName()] = true
		}
	}

	return served
}

// TestUnspecifiedIsServedEverything checks that a caller weave cannot place, such as a person
// who registered the server by hand, loses nothing.
func TestUnspecifiedIsServedEverything(t *testing.T) {
	every := map[string]bool{}
	for exposed := range rpcapi.GetMethods() {
		if exposed.Surfaces.GetMcp() {
			every[exposed.Surfaces.GetName()] = true
		}
	}

	assert.Equal(t, every, toolsFor(t, annotationv1.Role_ROLE_UNSPECIFIED))
}

// TestRolesAreServedTheToolsTheirBriefingNames holds each role to the calls its briefing names.
func TestRolesAreServedTheToolsTheirBriefingNames(t *testing.T) {
	tests := []struct {
		name    string
		role    annotationv1.Role
		served  []string
		refused []string
	}{
		{
			name:    "implementer",
			role:    annotationv1.Role_ROLE_IMPLEMENTER,
			served:  []string{"request_review", "add_review_message", "get_review", "list_review_threads", "create_agent", "set_agent_metadata", "send_mail", "read_mail"},
			refused: []string{"create_review_thread", "release_review", "open_review_round", "resolve_review_thread", "create_workflow", "delete_agents", "rebase_worktree"},
		},
		{
			name:    "reviewer",
			role:    annotationv1.Role_ROLE_REVIEWER,
			served:  []string{"open_review_round", "create_review_thread", "release_review", "resolve_review_thread", "list_review_threads", "send_mail", "read_mail", "create_agent"},
			refused: []string{"request_review", "create_workflow", "delete_agents"},
		},
		{
			name:    "orchestrator",
			role:    annotationv1.Role_ROLE_ORCHESTRATOR,
			served:  []string{"create_agent", "create_workflow", "create_schedule", "archive_agents", "rebase_worktree", "start_orchestrator", "run_doctor"},
			refused: []string{"request_review", "create_review_thread", "release_review"},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			served := toolsFor(t, tt.role)
			require.NotEmpty(t, served)

			for _, name := range tt.served {
				assert.True(t, served[name], "%s is not served %s", tt.name, name)
			}
			for _, name := range tt.refused {
				assert.False(t, served[name], "%s is still served %s", tt.name, name)
			}
		})
	}
}

// TestRoleScopingShrinksTheToolSet is the point of scoping at all: what a role is served costs
// it fewer tokens than the whole surface would.
func TestRoleScopingShrinksTheToolSet(t *testing.T) {
	size := func(role annotationv1.Role) int {
		total := 0
		for exposed := range rpcapi.GetMethods() {
			if !exposed.Surfaces.GetMcp() || !rpcapi.ServesRole(exposed.Surfaces, role) {
				continue
			}

			schema, err := inputSchema(exposed.Method.Input())
			require.NoError(t, err)
			encoded, err := json.Marshal(schema)
			require.NoError(t, err)

			total += len(exposed.Surfaces.GetDescription()) + len(encoded)
		}

		return total
	}

	whole := size(annotationv1.Role_ROLE_UNSPECIFIED)
	for _, role := range []annotationv1.Role{annotationv1.Role_ROLE_IMPLEMENTER, annotationv1.Role_ROLE_REVIEWER} {
		assert.Less(t, size(role), whole/2, "%s carries more than half the whole surface", role)
	}
}

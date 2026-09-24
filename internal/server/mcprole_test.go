package server

import (
	"testing"

	annotationv1 "komrade/proto/gen/annotation/v1"

	agentstore "komrade/internal/store/agent"

	"github.com/stretchr/testify/assert"
)

// TestEveryRoleIsMapped fails when a role reaches the MCP server with nothing to place it by.
func TestEveryRoleIsMapped(t *testing.T) {
	assert.Len(t, toolRoles, len(agentstore.Roles))

	for _, role := range agentstore.Roles {
		assert.NotEqual(t, annotationv1.Role_ROLE_UNSPECIFIED, toolRoles[role], "role %q is not mapped", role)
	}
}

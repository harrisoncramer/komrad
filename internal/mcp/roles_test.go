package mcp

import (
	"testing"

	annotationv1 "weave/proto/gen/annotation/v1"

	"weave/internal/rpcapi"

	"github.com/stretchr/testify/assert"
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

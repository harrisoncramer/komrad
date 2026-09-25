package rpcapi

import (
	"testing"

	annotationv1 "komrad/proto/gen/annotation/v1"

	"github.com/stretchr/testify/assert"
)

// TestUnspecifiedIsServedEverything checks that a caller komrad cannot place loses nothing.
func TestUnspecifiedIsServedEverything(t *testing.T) {
	every := map[string]bool{}
	served := map[string]bool{}
	for exposed := range GetMethods() {
		if !exposed.Surfaces.GetMcp() {
			continue
		}

		every[exposed.Surfaces.GetName()] = true
		if ServesRole(exposed.Surfaces, annotationv1.Role_ROLE_UNSPECIFIED) {
			served[exposed.Surfaces.GetName()] = true
		}
	}

	assert.Equal(t, every, served)
}

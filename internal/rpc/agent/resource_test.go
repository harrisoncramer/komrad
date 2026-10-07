package agent

import (
	"testing"

	textsearchstore "komrad/internal/store/textsearch"
	"komrad/internal/ui/paint"

	"github.com/stretchr/testify/assert"
)

// TestPaintMarkersMatchTheStore pins the markers paint reads to the ones the store writes.
func TestPaintMarkersMatchTheStore(t *testing.T) {
	t.Parallel()

	assert.Equal(t, textsearchstore.Open, paint.MarkOpen)
	assert.Equal(t, textsearchstore.Close, paint.MarkClose)
}

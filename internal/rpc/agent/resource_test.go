package agent

import (
	"testing"

	transcriptstore "komrad/internal/store/transcript"
	"komrad/internal/ui/paint"

	"github.com/stretchr/testify/assert"
)

// TestPaintMarkersMatchTheStore pins the markers paint reads to the ones the store writes.
func TestPaintMarkersMatchTheStore(t *testing.T) {
	t.Parallel()

	assert.Equal(t, transcriptstore.SnippetOpen, paint.MarkOpen)
	assert.Equal(t, transcriptstore.SnippetClose, paint.MarkClose)
}

package ui

import (
	"testing"

	transcriptstore "weave/internal/store/transcript"
	"weave/internal/ui/paint"

	"github.com/stretchr/testify/assert"
)

// TestPaintMarkersMatchTheStore pins the markers paint reads to the ones the store writes, which
// is the contract paint declares for itself rather than imports.
func TestPaintMarkersMatchTheStore(t *testing.T) {
	assert.Equal(t, transcriptstore.SnippetOpen, paint.MarkOpen)
	assert.Equal(t, transcriptstore.SnippetClose, paint.MarkClose)
}

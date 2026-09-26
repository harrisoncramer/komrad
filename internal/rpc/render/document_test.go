package render

import (
	"testing"
	"time"

	"github.com/stretchr/testify/assert"

	documentstore "komrad/internal/store/document"
)

// TestDocument pins that a rewrite stamps to within the second, so a client sees one that lands
// in the same second as the write before it.
func TestDocument(t *testing.T) {
	t.Parallel()

	rendered := Document(documentstore.Document{
		ID:        "d1",
		CreatedAt: time.Date(2026, time.September, 1, 9, 9, 0, 0, time.UTC),
		UpdatedAt: time.Date(2026, time.September, 1, 9, 9, 0, 250_000_000, time.UTC),
	})

	assert.Equal(t, "2026-09-01T09:09:00.25Z", rendered.GetUpdatedAt())
}

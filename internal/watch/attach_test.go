package watch

import (
	"os"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// migrationPath is the migration the generator writes.
const migrationPath = "../../db/migrations/20260922050000_watch_attachments.sql"

// TestTheAttachmentsAreCommitted pins the generated migration to the resources: what the
// generator would write now is what the repository holds.
func TestTheAttachmentsAreCommitted(t *testing.T) {
	t.Parallel()

	committed, err := os.ReadFile(migrationPath)
	require.NoError(t, err)
	assert.Equal(t, string(RenderAttachments()), string(committed), "run 'mise run gen:triggers'")
}

// TestEveryResourceAndProjectionIsAttached pins that each resource's table and each table
// projected onto one gets a trigger, with the lookup's parent columns and its narrowing.
func TestEveryResourceAndProjectionIsAttached(t *testing.T) {
	t.Parallel()

	rendered := string(RenderAttachments())

	for _, r := range Resources() {
		assert.Contains(t, rendered, "attach_watch_trigger('"+r.Table+"', '"+r.Key+"');")

		for _, p := range r.Projects {
			if p.Column != "" {
				assert.Contains(t, rendered, "attach_projection_trigger('"+p.Table+"', '"+r.Table+"', '"+p.Column+"');")

				continue
			}

			assert.Contains(t, rendered, "attach_projection_lookup_trigger('"+p.Table+"', '"+r.Table+"', '"+r.Key+"', '"+p.Lookup+"', '"+p.Via+"', "+narrowing(p.Where)+");")
		}
	}

	assert.Equal(t, len(Resources()), strings.Count(rendered, "attach_watch_trigger("))
}

package watch

import (
	"testing"

	"weave/internal/resource"
	watchv1 "weave/proto/gen/watch/v1"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// TestResourcesAreTheAnnotatedTables pins the table map to the resource option: one entry per
// annotated table, carrying the enum value the wire sends it under, and nothing else.
func TestResourcesAreTheAnnotatedTables(t *testing.T) {
	t.Parallel()

	all := resource.All()
	require.NotEmpty(t, all)

	assert.Len(t, resources, len(all))
	for _, r := range all {
		res, ok := resources[r.Option.GetTable()]
		require.True(t, ok, "the table %s is left out, so watch.v1.Resource has no %s", r.Option.GetTable(), r.EnumValue())
		assert.Equal(t, r.EnumValue(), res.String(), "the table %s is sent under the wrong value", r.Option.GetTable())
	}
	assert.Len(t, watchv1.Resource_value, len(all)+1, "the enum names the resources and RESOURCE_UNSPECIFIED")
}

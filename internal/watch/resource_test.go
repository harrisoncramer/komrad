package watch

import (
	"testing"

	"weave/internal/resource"
	watchv1 "weave/proto/gen/watch/v1"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// TestKindsAreTheResources pins the table map and the wire's enum to the resource option: one
// entry per annotated table, numbered the way the enum numbers it, and nothing else.
func TestKindsAreTheResources(t *testing.T) {
	t.Parallel()

	resources := resource.All()
	require.NotEmpty(t, resources)

	want := map[string]Kind{}
	for _, r := range resources {
		number, ok := watchv1.Resource_value[r.EnumValue()]
		require.True(t, ok, "watch.v1.Resource has no %s for the table %s", r.EnumValue(), r.Option.GetTable())

		want[r.Option.GetTable()] = Kind(number)
	}

	assert.Equal(t, want, kinds)
	assert.Len(t, watchv1.Resource_value, len(resources)+1, "the enum names the resources and RESOURCE_UNSPECIFIED")
}

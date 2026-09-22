package watch

import (
	"testing"

	watchv1 "weave/proto/gen/watch/v1"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// TestAllResourcesReadsEveryAnnotatedMessage pins that the option is read off the descriptors
// and that the spellings derived from it are the ones the code uses.
func TestAllResourcesReadsEveryAnnotatedMessage(t *testing.T) {
	t.Parallel()

	all := AllResources()
	require.NotEmpty(t, all)

	byTable := map[string]Resource{}
	for _, r := range all {
		byTable[r.Option.GetTable()] = r
	}

	agent, ok := byTable["agents"]
	require.True(t, ok, "agent.v1.Agent carries the option")
	assert.Equal(t, "agent.v1.Agent", string(agent.Message.FullName()))
	assert.Equal(t, "Agent", agent.Name())
	assert.Equal(t, "Agents", agent.Plural())
	assert.Equal(t, "RESOURCE_AGENT", agent.EnumValue())
	assert.Equal(t, "id", agent.Key(), "the key is id when the option leaves it out")

	mark, ok := byTable["marks"]
	require.True(t, ok, "agent.v1.Mark carries the option")
	assert.Equal(t, "mark", mark.Key())
}

// TestResourcesAreTheAnnotatedTables pins the table map to the resource option: one entry per
// annotated table, carrying the enum value the wire sends it under, and nothing else.
func TestResourcesAreTheAnnotatedTables(t *testing.T) {
	t.Parallel()

	all := AllResources()
	require.NotEmpty(t, all)

	assert.Len(t, resources, len(all))
	for _, r := range all {
		res, ok := resources[r.Option.GetTable()]
		require.True(t, ok, "the table %s is left out, so watch.v1.Resource has no %s", r.Option.GetTable(), r.EnumValue())
		assert.Equal(t, r.EnumValue(), res.String(), "the table %s is sent under the wrong value", r.Option.GetTable())
	}
	assert.Len(t, watchv1.Resource_value, len(all)+1, "the enum names the resources and RESOURCE_UNSPECIFIED")
}

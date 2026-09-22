package resource_test

import (
	"testing"

	"weave/internal/resource"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// TestAllReadsEveryAnnotatedMessage pins that the option is read off the descriptors and
// that the spellings the checks derive from it are the ones the code uses.
func TestAllReadsEveryAnnotatedMessage(t *testing.T) {
	t.Parallel()

	all := resource.All()
	require.NotEmpty(t, all)

	byTable := map[string]resource.Resource{}
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
	assert.Equal(t, []string{"updated_at", "state_seen_at", "nudged_at", "nudge_sent_at"}, agent.Option.GetIgnores())
	assert.Len(t, agent.Option.GetProjects(), 4)
}

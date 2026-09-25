package harness_test

import (
	"testing"

	"komrad/internal/harness"
	"komrad/internal/harnesstest"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// TestNewRegistry covers what a registry refuses to be built from, both cases being ones that
// would otherwise decide an agent's harness by argument order.
func TestNewRegistry(t *testing.T) {
	tests := []struct {
		name      string
		harnesses []harness.Harness
		wantErr   string
	}{
		{
			name:      "two harnesses claiming one id",
			harnesses: []harness.Harness{&harnesstest.Fake{Name: "claude"}, &harnesstest.Fake{Name: "claude"}},
			wantErr:   `two harnesses answer to "claude"`,
		},
		{
			name:      "a harness with no id, which nothing could name",
			harnesses: []harness.Harness{&harnesstest.Fake{Anonymous: true}},
			wantErr:   "answers to no id",
		},
		{
			name:      "distinct ids",
			harnesses: []harness.Harness{&harnesstest.Fake{Name: "claude"}, &harnesstest.Fake{Name: "codex"}},
		},
		{
			name: "no harnesses at all, which is a binding that runs nothing",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			registry, err := harness.NewRegistry(tt.harnesses...)

			if tt.wantErr != "" {
				require.ErrorContains(t, err, tt.wantErr)

				return
			}

			require.NoError(t, err)
			assert.Len(t, registry.IDs(), len(tt.harnesses))
		})
	}
}

// TestRegistryGetByID covers an id nothing implements returning an error naming what is
// available.
func TestRegistryGetByID(t *testing.T) {
	registry, err := harness.NewRegistry(&harnesstest.Fake{Name: "codex"}, &harnesstest.Fake{Name: "claude"})
	require.NoError(t, err)

	assert.Equal(t, []string{"claude", "codex"}, registry.IDs(), "sorted, so a message reads the same every time")

	found, err := registry.GetByID("codex")
	require.NoError(t, err)
	assert.Equal(t, "codex", found.ID())

	_, err = registry.GetByID("gemini")
	require.ErrorIs(t, err, harness.ErrNotRegistered)
	require.ErrorContains(t, err, "claude, codex", "the error names what it does hold")

	_, err = registry.GetByID("")
	require.ErrorIs(t, err, harness.ErrNotRegistered, "resolution happens before the lookup, so nothing here guesses a default")
}

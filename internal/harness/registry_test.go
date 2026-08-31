package harness

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

type stub struct {
	id string
}

func (s stub) GetID() string                       { return s.id }
func (s stub) GetBinary() string                   { return s.id }
func (s stub) BuildCommand(Launch) (string, error) { return "", nil }
func (s stub) MintSessionID() string               { return "" }

func (s stub) FindSession(string) (string, error)     { return "", nil }
func (s stub) ReadActivity(string) Activity           { return ActivityUnknown }
func (s stub) GetInterruptKeys() []string             { return nil }
func (s stub) GetLogPath(string) string               { return "" }
func (s stub) ReadLog(string, int64) (LogRead, error) { return LogRead{}, nil }

// TestNewRegistry covers what a registry refuses to be built from, both cases being ones that
// would otherwise decide an agent's harness by argument order.
func TestNewRegistry(t *testing.T) {
	tests := []struct {
		name      string
		harnesses []Harness
		wantErr   string
	}{
		{
			name:      "two harnesses claiming one id",
			harnesses: []Harness{stub{id: "claude"}, stub{id: "claude"}},
			wantErr:   `two harnesses answer to "claude"`,
		},
		{
			name:      "a harness with no id, which nothing could name",
			harnesses: []Harness{stub{}},
			wantErr:   "answers to no id",
		},
		{
			name:      "distinct ids",
			harnesses: []Harness{stub{id: "claude"}, stub{id: "codex"}},
		},
		{
			name: "no harnesses at all, which is a binding that runs nothing",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			registry, err := NewRegistry(tt.harnesses...)

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
// available, since the callers are loops that would dereference a nil.
func TestRegistryGetByID(t *testing.T) {
	registry, err := NewRegistry(stub{id: "codex"}, stub{id: "claude"})
	require.NoError(t, err)

	assert.Equal(t, []string{"claude", "codex"}, registry.IDs(), "sorted, so a message reads the same every time")

	found, err := registry.GetByID("codex")
	require.NoError(t, err)
	assert.Equal(t, "codex", found.GetID())

	_, err = registry.GetByID("gemini")
	require.ErrorIs(t, err, ErrNotRegistered)
	assert.ErrorContains(t, err, "claude, codex", "the error names what it does hold")

	_, err = registry.GetByID("")
	require.ErrorIs(t, err, ErrNotRegistered, "resolution happens before the lookup, so nothing here guesses a default")
}

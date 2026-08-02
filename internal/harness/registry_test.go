package harness

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// stub is a harness that answers to one id and nothing else. The registry only ever
// asks for that, so the rest of the interface is unreachable here.
type stub struct {
	id string
}

func (s stub) GetID() string                       { return s.id }
func (s stub) BuildCommand(Launch) (string, error) { return "", nil }
func (s stub) MintSessionID() string               { return "" }

func (s stub) FindSession(string) (string, error)     { return "", nil }
func (s stub) ReadActivity(string) Activity           { return ActivityUnknown }
func (s stub) GetLogPath(string) string               { return "" }
func (s stub) ReadLog(string, int64) (LogRead, error) { return LogRead{}, nil }
func (s stub) Relocate(string, string) error          { return nil }

// TestNewRegistry covers what a registry refuses to be built from. Both cases would
// otherwise decide which harness an agent got by argument order.
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
			assert.Len(t, registry.ids(), len(tt.harnesses))
		})
	}
}

// TestRegistryResolve covers resolving an id. An id nothing implements is an error
// naming what is available rather than a nil the caller has to notice, since the
// callers are loops that would otherwise dereference it.
func TestRegistryResolve(t *testing.T) {
	registry, err := NewRegistry(stub{id: "codex"}, stub{id: "claude"})
	require.NoError(t, err)

	assert.Equal(t, []string{"claude", "codex"}, registry.ids(), "sorted, so a message reads the same every time")

	found, err := registry.GetByID("codex")
	require.NoError(t, err)
	assert.Equal(t, "codex", found.GetID())

	_, err = registry.GetByID("gemini")
	require.ErrorIs(t, err, ErrNotRegistered)
	assert.ErrorContains(t, err, "claude, codex", "the error names what it does hold")

	_, err = registry.GetByID("")
	require.ErrorIs(t, err, ErrNotRegistered, "resolution happens before the lookup, so nothing here guesses a default")
}

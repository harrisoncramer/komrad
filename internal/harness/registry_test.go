package harness

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

type fakeHarness struct {
	id string
}

func (s fakeHarness) ID() string                          { return s.id }
func (s fakeHarness) Binary() string                      { return s.id }
func (s fakeHarness) BuildCommand(Launch) (string, error) { return "", nil }
func (s fakeHarness) MintSessionID() string               { return "" }

func (s fakeHarness) FindSession(string) (string, error) { return "", nil }
func (s fakeHarness) ReadActivity(string) Activity       { return ActivityUnknown }
func (s fakeHarness) ReadReport(Event, []byte) (Report, error) {
	return Report{}, nil
}
func (s fakeHarness) InterruptKeys() []string                { return nil }
func (s fakeHarness) LogPath(string) string                  { return "" }
func (s fakeHarness) ReadLog(string, int64) (LogRead, error) { return LogRead{}, nil }

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
			harnesses: []Harness{fakeHarness{id: "claude"}, fakeHarness{id: "claude"}},
			wantErr:   `two harnesses answer to "claude"`,
		},
		{
			name:      "a harness with no id, which nothing could name",
			harnesses: []Harness{fakeHarness{}},
			wantErr:   "answers to no id",
		},
		{
			name:      "distinct ids",
			harnesses: []Harness{fakeHarness{id: "claude"}, fakeHarness{id: "codex"}},
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
// available.
func TestRegistryGetByID(t *testing.T) {
	registry, err := NewRegistry(fakeHarness{id: "codex"}, fakeHarness{id: "claude"})
	require.NoError(t, err)

	assert.Equal(t, []string{"claude", "codex"}, registry.IDs(), "sorted, so a message reads the same every time")

	found, err := registry.GetByID("codex")
	require.NoError(t, err)
	assert.Equal(t, "codex", found.ID())

	_, err = registry.GetByID("gemini")
	require.ErrorIs(t, err, ErrNotRegistered)
	require.ErrorContains(t, err, "claude, codex", "the error names what it does hold")

	_, err = registry.GetByID("")
	require.ErrorIs(t, err, ErrNotRegistered, "resolution happens before the lookup, so nothing here guesses a default")
}

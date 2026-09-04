package claude

import (
	"os"
	"path/filepath"
	"testing"

	"weave/internal/harness"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// TestReadReport covers what one hook payload is read as, the event deciding it since that is
// what weave pointed a subcommand at.
func TestReadReport(t *testing.T) {
	tests := []struct {
		name    string
		event   string
		payload string
		want    harness.Report
		wantErr bool
	}{
		{
			name:    "a turn ending",
			event:   EventStop,
			payload: `{"stop_hook_active":false}`,
			want:    harness.Report{TurnEnded: true},
		},
		{
			name:    "a turn weave's own last answer caused",
			event:   EventStop,
			payload: `{"stop_hook_active":true}`,
			want:    harness.Report{TurnEnded: true},
		},
		{
			name:  "a turn ending whose payload never arrived",
			event: EventStop,
			want:  harness.Report{TurnEnded: true},
		},
		{
			name:    "an event no reading is taken from",
			event:   "PreToolUse",
			payload: `{"tool_name":"Bash"}`,
			wantErr: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			report, err := NewHarness(NewHarnessParams{ProjectsDir: t.TempDir()}).ReadReport(tt.event, []byte(tt.payload))
			if tt.wantErr {
				require.Error(t, err, "an event with no case here would otherwise clear the reason on the row")
				assert.Equal(t, harness.Report{}, report)

				return
			}

			require.NoError(t, err)
			assert.Equal(t, tt.want, report)
		})
	}
}

// TestReadReportOnACapturedPayload runs the derive over a Stop payload taken off a real headless
// run, which is what pins the reading against the shape Claude actually sends rather than the
// one its documentation describes.
func TestReadReportOnACapturedPayload(t *testing.T) {
	payload, err := os.ReadFile(filepath.Join("testdata", "stop_hook_payload.json"))
	require.NoError(t, err)

	report, err := NewHarness(NewHarnessParams{ProjectsDir: t.TempDir()}).ReadReport(EventStop, payload)
	require.NoError(t, err)
	assert.Equal(t, harness.Report{TurnEnded: true}, report)
}

// TestEveryInstalledHookIsRead pins the direction the subcommand test cannot cover: an event
// weave installs and takes no reading from would fire on every occurrence and record nothing.
func TestEveryInstalledHookIsRead(t *testing.T) {
	hn := NewHarness(NewHarnessParams{ProjectsDir: t.TempDir()})

	for event := range InstalledHooks {
		report, err := hn.ReadReport(event, nil)
		require.NoError(t, err, "the %s hook is installed, so a reading is taken from it", event)
		assert.NotEqual(t, harness.Report{}, report, "the %s hook reports something", event)
	}
}

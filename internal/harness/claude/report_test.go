package claude

import (
	"os"
	"path/filepath"
	"testing"

	"komrad/internal/harness"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// TestReadReport covers what one hook payload is read as, the event deciding it since that is
// what komrad pointed a subcommand at.
func TestReadReport(t *testing.T) {
	tests := []struct {
		name    string
		event   harness.Event
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
			name:    "a turn komrad's own last answer caused",
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
			name:    "a tool call starting",
			event:   EventPreToolUse,
			payload: `{"tool_name":"Bash","tool_input":{"command":"echo hi"}}`,
			want:    harness.Report{Reason: "Bash"},
		},
		{
			name:    "a tool call returning",
			event:   EventPostToolUse,
			payload: `{"tool_name":"Bash","tool_response":{"stdout":"hi"}}`,
			want:    harness.Report{},
		},
		{
			name:    "a tool call waiting on a person to allow it",
			event:   EventPermissionRequest,
			payload: `{"tool_name":"Bash","tool_input":{"command":"echo hi"}}`,
			want:    harness.Report{Reason: "Bash"},
		},
		{
			name:    "a tool call komrad could not read",
			event:   EventPreToolUse,
			payload: `{`,
			wantErr: true,
		},
		{
			name:    "an event no reading is taken from",
			event:   "SessionEnd",
			payload: `{"reason":"clear"}`,
			wantErr: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			report, err := NewHarness(NewHarnessParams{ConfigDir: t.TempDir()}).ReadReport(tt.event, []byte(tt.payload))
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

// TestReadReportOnCapturedPayloads runs the derive over payloads taken off a real headless run,
// which is what pins each reading against the shape Claude actually sends rather than the one its
// documentation describes.
func TestReadReportOnCapturedPayloads(t *testing.T) {
	tests := []struct {
		file  string
		event harness.Event
		want  harness.Report
	}{
		{
			file:  "stop_hook_payload.json",
			event: EventStop,
			want:  harness.Report{TurnEnded: true},
		},
		{
			file:  "pre_tool_use_hook_payload.json",
			event: EventPreToolUse,
			want:  harness.Report{Reason: "Bash"},
		},
		{
			file:  "permission_request_hook_payload.json",
			event: EventPermissionRequest,
			want:  harness.Report{Reason: "Bash"},
		},
		// Claude 2.1.278 fires PermissionRequest for a question under bypassed permissions too, naming the tool.
		{
			file:  "permission_request_hook_payload_for_a_question.json",
			event: EventPermissionRequest,
			want:  harness.Report{Reason: "AskUserQuestion"},
		},
		// Claude 2.1.278 fires PermissionRequest for the plan picker too, naming ExitPlanMode.
		{
			file:  "permission_request_hook_payload_for_a_plan.json",
			event: EventPermissionRequest,
			want:  harness.Report{Reason: "ExitPlanMode"},
		},
		{
			file:  "post_tool_use_hook_payload.json",
			event: EventPostToolUse,
			want:  harness.Report{},
		},
	}

	for _, tt := range tests {
		t.Run(tt.file, func(t *testing.T) {
			payload, err := os.ReadFile(filepath.Join("testdata", tt.file))
			require.NoError(t, err)

			report, err := NewHarness(NewHarnessParams{ConfigDir: t.TempDir()}).ReadReport(tt.event, payload)
			require.NoError(t, err)
			assert.Equal(t, tt.want, report)
		})
	}
}

// TestEveryInstalledHookIsRead pins the direction the subcommand test cannot cover: an event
// komrad installs but takes no reading from would fire on every occurrence and be refused.
func TestEveryInstalledHookIsRead(t *testing.T) {
	hn := NewHarness(NewHarnessParams{ConfigDir: t.TempDir()})

	for event, install := range InstalledHooks {
		if install.TakesNoReading {
			_, err := hn.ReadReport(event, []byte(`{}`))
			require.Error(t, err, "the %s hook records nothing, so no reading may clear the row's reason", event)

			continue
		}

		_, err := hn.ReadReport(event, []byte(`{}`))
		assert.NoError(t, err, "the %s hook is installed, so a reading is taken from it", event)
	}
}

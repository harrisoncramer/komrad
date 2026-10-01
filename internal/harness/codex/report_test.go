package codex

import (
	"testing"

	"komrad/internal/harness"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// TestReadReport covers what a notify payload is read as.
func TestReadReport(t *testing.T) {
	type readReportCase struct {
		name    string
		event   harness.Event
		payload string
		want    harness.Report
		wantErr error
		failed  bool
	}

	tests := []readReportCase{
		{
			name:    "an event codex does not fire",
			event:   "Stop",
			payload: `{"type":"agent-turn-complete"}`,
			failed:  true,
		},
		{
			name:    "a payload that will not parse",
			event:   EventNotify,
			payload: `{`,
			failed:  true,
		},
		{
			name:    "anything else codex notifies about",
			event:   EventNotify,
			payload: `{"type":"agent-something-else"}`,
			wantErr: harness.ErrNoReport,
			failed:  true,
		},
		{
			name:  "a bare notify with no payload",
			event: EventNotify,
			want:  harness.Report{TurnEnded: true},
		},
		{
			name:    "a completed turn",
			event:   EventNotify,
			payload: `{"type":"agent-turn-complete","turn-id":"t1"}`,
			want:    harness.Report{TurnEnded: true},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			report, err := NewHarness(NewHarnessParams{Home: t.TempDir()}).ReadReport(tt.event, []byte(tt.payload))
			if tt.failed {
				require.Error(t, err)
				if tt.wantErr != nil {
					require.ErrorIs(t, err, tt.wantErr)
				}

				return
			}

			require.NoError(t, err)
			assert.Equal(t, tt.want, report)
		})
	}
}

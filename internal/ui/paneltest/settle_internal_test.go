package paneltest

import (
	"testing"
	"time"

	"charm.land/bubbles/v2/cursor"
	tea "charm.land/bubbletea/v2"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// TestSettleDropsTimers covers settle telling a timer from a command it can run at once.
func TestSettleDropsTimers(t *testing.T) {
	blinker := cursor.New()

	tests := []struct {
		name string
		cmd  tea.Cmd
		want bool
	}{
		{
			name: "a command that answers at once is run",
			cmd: func() tea.Msg {
				return nil
			},
			want: false,
		},
		{
			name: "a tick is dropped",
			cmd:  tea.Tick(time.Hour, nil),
			want: true,
		},
		{
			name: "an every is dropped",
			cmd:  tea.Every(time.Hour, nil),
			want: true,
		},
		{
			name: "a cursor blink is dropped",
			cmd:  blinker.Blink(),
			want: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			require.NotNil(t, tt.cmd)

			assert.Equal(t, tt.want, isDropped(tt.cmd))
		})
	}
}

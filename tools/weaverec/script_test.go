package main

import (
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// TestParseScript covers every step kind and the lines a script refuses.
func TestParseScript(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name    string
		script  string
		want    []step
		wantErr string
	}{
		{
			name:    "empty script",
			script:  "# nothing here\n\n",
			wantErr: "no steps",
		},
		{
			name:    "unknown step",
			script:  "press j\n",
			wantErr: `unknown step "press", expected key, type, sleep or wait at line 1`,
		},
		{
			name:    "key without a name",
			script:  "key\n",
			wantErr: "key needs a tmux key name at line 1",
		},
		{
			name:    "sleep without a duration",
			script:  "key j\nsleep soon\n",
			wantErr: "sleep needs a duration such as 500ms at line 2",
		},
		{
			name:    "sleep of zero",
			script:  "sleep 0s\n",
			wantErr: "above zero",
		},
		{
			name:   "every step kind",
			script: "# open\nkey j\n  key C-c Enter\ntype hello world \nwait Claude\nsleep 500ms\n",
			want: []step{
				{
					kind: stepKey,
					arg:  "j",
					line: 2,
					text: "key j",
				},
				{
					kind: stepKey,
					arg:  "C-c Enter",
					line: 3,
					text: "key C-c Enter",
				},
				{
					kind: stepType,
					arg:  "hello world",
					line: 4,
					text: "type hello world",
				},
				{
					kind: stepWait,
					arg:  "Claude",
					line: 5,
					text: "wait Claude",
				},
				{
					kind:  stepSleep,
					pause: 500 * time.Millisecond,
					line:  6,
					text:  "sleep 500ms",
				},
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			got, err := parseScript(strings.NewReader(tt.script))
			if tt.wantErr != "" {
				require.ErrorContains(t, err, tt.wantErr)

				return
			}

			require.NoError(t, err)
			assert.Equal(t, tt.want, got)
		})
	}
}

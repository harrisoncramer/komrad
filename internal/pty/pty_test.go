package pty_test

import (
	"bytes"
	"testing"

	"komrad/internal/pty"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// TestStart covers a program run on a terminal of its own.
func TestStart(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name       string
		argv       []string
		term       string
		resize     *pty.Size
		input      string
		wantErr    bool
		wantCode   int
		wantOutput string
	}{
		{
			name:    "no program is refused",
			argv:    nil,
			wantErr: true,
		},
		{
			name:     "a signal is reported as 128 plus the signal",
			argv:     []string{"sh", "-c", "kill -TERM $$"},
			wantCode: 143,
		},
		{
			name:     "a non-zero exit is reported",
			argv:     []string{"sh", "-c", "exit 3"},
			wantCode: 3,
		},
		{
			name:       "a resize reaches the program",
			argv:       []string{"sh", "-c", "read line; stty size"},
			resize:     &pty.Size{Rows: 30, Cols: 100},
			input:      "\n",
			wantOutput: "30 100",
		},
		{
			name:       "the terminal type is passed on",
			argv:       []string{"sh", "-c", "echo $TERM"},
			term:       "screen-256color",
			wantOutput: "screen-256color",
		},
		{
			name:       "the terminal type defaults when none is named",
			argv:       []string{"sh", "-c", "echo $TERM"},
			wantOutput: "xterm-256color",
		},
		{
			name:       "what is typed reaches the program",
			argv:       []string{"sh", "-c", "read line; echo got-$line"},
			input:      "hello\n",
			wantOutput: "got-hello",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			session, err := pty.Start(t.Context(), pty.StartParams{
				Argv: tt.argv,
				Dir:  t.TempDir(),
				Size: pty.Size{Rows: 24, Cols: 80},
				Term: tt.term,
			})
			if tt.wantErr {
				require.Error(t, err)

				return
			}

			require.NoError(t, err)

			if tt.resize != nil {
				require.NoError(t, session.Resize(*tt.resize))
			}

			if tt.input != "" {
				_, err := session.Write([]byte(tt.input))
				require.NoError(t, err)
			}

			var output bytes.Buffer
			buffer := make([]byte, 1024)
			for {
				n, err := session.Read(buffer)
				output.Write(buffer[:n])
				if err != nil {
					break
				}
			}

			code, err := session.Wait()
			require.NoError(t, err)
			require.NoError(t, session.Close())

			assert.Equal(t, tt.wantCode, code)
			assert.Contains(t, output.String(), tt.wantOutput)
		})
	}
}

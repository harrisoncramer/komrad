package claude

import (
	"bufio"
	"net"
	"testing"

	"komrad/internal/harness"
	"komrad/internal/harnesstest"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// TestChannelSend covers what the channel writes to an agent's messaging socket, and the false it
// returns when nothing there takes the line.
func TestChannelSend(t *testing.T) {
	const agentID = "agent-1"

	tests := []struct {
		name     string
		socket   string
		want     bool
		wantLine string
	}{
		{
			name: "no socket at the path",
		},
		{
			name:   "a socket left by a CLI that exited",
			socket: "stale",
		},
		{
			name:     "a CLI listening",
			socket:   "listening",
			want:     true,
			wantLine: `{"type":"user","message":{"role":"user","content":"read your mail"},"from":"komrad"}`,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			hn := NewHarness(NewHarnessParams{ConfigDir: t.TempDir(), CacheDir: harnesstest.SocketDir(t)})
			path, err := harness.PrepareSocket(hn.cacheDir, HarnessID, agentID)
			require.NoError(t, err)

			lines := make(chan string, 1)
			if tt.socket != "" {
				listener, err := net.ListenUnix("unix", &net.UnixAddr{Name: path, Net: "unix"})
				require.NoError(t, err)
				t.Cleanup(func() { _ = listener.Close() })

				if tt.socket == "stale" {
					listener.SetUnlinkOnClose(false)
					require.NoError(t, listener.Close())
				} else {
					go func() {
						conn, err := listener.Accept()
						if err != nil {
							return
						}
						defer func() { _ = conn.Close() }()

						line, _ := bufio.NewReader(conn).ReadString('\n')
						lines <- line
					}()
				}
			}

			sent, err := hn.Channel().Send(t.Context(), harness.Target{AgentID: agentID}, "read your mail")
			require.NoError(t, err)
			assert.Equal(t, tt.want, sent)

			if tt.wantLine != "" {
				assert.JSONEq(t, tt.wantLine, <-lines)
			}
		})
	}
}

package codex

import (
	"os"
	"path/filepath"
	"testing"

	"weave/internal/harness"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestDescribeSession(t *testing.T) {
	const sessionID = "019fb3e7-2cb6-7ab3-86a0-51a670a33140"

	tests := []struct {
		name    string
		rollout string

		wantDir   string
		wantErrIs error
	}{
		{
			name:    "the directory the opening record names",
			rollout: metaRecord + "\n" + promptRecord,
			wantDir: "/repo/worktree",
		},
		{
			name: "a rollout whose opening record names no directory",
			rollout: `{"type":"session_meta","payload":{"id":"` + sessionID + `"}}` + "\n" +
				promptRecord,
			wantErrIs: harness.ErrSessionNotFound,
		},
		{
			name:      "a rollout that does not open with its own metadata",
			rollout:   promptRecord,
			wantErrIs: nil,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			sessions := t.TempDir()
			day := filepath.Join(sessions, "2026", "07", "30")
			require.NoError(t, os.MkdirAll(day, 0o755))

			path := filepath.Join(day, "rollout-2026-07-30T12-41-39-"+sessionID+".jsonl")
			require.NoError(t, os.WriteFile(path, []byte(tt.rollout), 0o600))

			session, err := NewHarness(NewHarnessParams{SessionsDir: sessions}).DescribeSession(sessionID)

			if tt.wantDir == "" {
				require.Error(t, err)
				if tt.wantErrIs != nil {
					require.ErrorIs(t, err, tt.wantErrIs)
				}

				return
			}

			require.NoError(t, err)
			assert.Equal(t, tt.wantDir, session.Dir)
			assert.Empty(t, session.Branch, "codex records no branch, so an adopted agent takes whatever its checkout is on")
		})
	}
}

func TestDescribeSessionWithoutARollout(t *testing.T) {
	_, err := NewHarness(NewHarnessParams{SessionsDir: t.TempDir()}).DescribeSession("nothing-here")
	require.ErrorIs(t, err, harness.ErrSessionNotFound)
}

func TestForksOnResume(t *testing.T) {
	assert.False(t, NewHarness(NewHarnessParams{}).ForksOnResume(),
		"codex takes no session id, so adopting a conversation writes into the person's own rollout")
}

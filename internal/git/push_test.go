//go:build integration

package git_test

import (
	"os"
	"path/filepath"
	"testing"

	"komrad/internal/git"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// TestIntegrationPush sends a branch to origin and checks what origin holds afterwards.
func TestIntegrationPush(t *testing.T) {
	t.Parallel()

	type testCase struct {
		name string
		// build leaves a clone on branch topic and answers the commit origin's topic should hold
		// once the push is over.
		build    func(t *testing.T, origin, clone string) string
		force    bool
		wantErr  error
		wantSaid string
	}

	tests := []testCase{
		{
			name: "a forced push is refused when origin holds a commit the branch fetched but never took in",
			build: func(t *testing.T, origin, clone string) string {
				t.Helper()

				pushTopic(t, clone)
				run(t, origin, "checkout", "--quiet", "topic")
				commitFile(t, origin, "forge.txt", "forge\n", "forge")
				forged := out(t, origin, "rev-parse", "HEAD")
				run(t, origin, "checkout", "--quiet", "main")
				run(t, clone, "fetch", "--quiet", "origin")
				commitFile(t, clone, "tracked.txt", "rewritten\n", "rewritten")

				return forged
			},
			force:   true,
			wantErr: git.ErrPushRejected,
		},
		{
			name: "a plain push is refused when origin moved on",
			build: func(t *testing.T, origin, clone string) string {
				t.Helper()

				pushTopic(t, clone)
				run(t, origin, "checkout", "--quiet", "topic")
				commitFile(t, origin, "forge.txt", "forge\n", "forge")
				forged := out(t, origin, "rev-parse", "HEAD")
				run(t, origin, "checkout", "--quiet", "main")
				commitFile(t, clone, "tracked.txt", "more\n", "more")

				return forged
			},
			wantErr: git.ErrPushRejected,
		},
		{
			name: "a failing pre-push hook is refused with what the hook said",
			build: func(t *testing.T, _, clone string) string {
				t.Helper()

				hook := filepath.Join(out(t, clone, "rev-parse", "--absolute-git-dir"), "hooks", "pre-push")
				require.NoError(t, os.WriteFile(hook, []byte("#!/bin/sh\necho lint found three problems\nexit 1\n"), 0o755))
				run(t, clone, "checkout", "--quiet", "-b", "topic")
				commitFile(t, clone, "tracked.txt", "topic\n", "topic")

				return ""
			},
			wantSaid: "lint found three problems",
		},
		{
			name: "a rebased branch goes up forced",
			build: func(t *testing.T, origin, clone string) string {
				t.Helper()

				pushTopic(t, clone)
				commitFile(t, origin, "other.txt", "moved on\n", "moved on")
				run(t, clone, "fetch", "--quiet", "origin")
				run(t, clone, "rebase", "--quiet", "origin/main")

				return out(t, clone, "rev-parse", "HEAD")
			},
			force: true,
		},
		{
			name: "a branch origin never had goes up",
			build: func(t *testing.T, _, clone string) string {
				t.Helper()

				run(t, clone, "checkout", "--quiet", "-b", "topic")
				commitFile(t, clone, "tracked.txt", "topic\n", "topic")

				return out(t, clone, "rev-parse", "HEAD")
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			origin, clone := newClonePair(t)
			want := tt.build(t, origin, clone)

			sha, err := client.Push(t.Context(), git.PushParams{
				Dir:    clone,
				Branch: "topic",
				Force:  tt.force,
			})

			if tt.wantSaid != "" {
				require.Error(t, err)
				assert.Contains(t, err.Error(), tt.wantSaid)

				return
			}
			if tt.wantErr != nil {
				require.ErrorIs(t, err, tt.wantErr)
				assert.Equal(t, want, out(t, origin, "rev-parse", "topic"), "origin keeps what it had")

				return
			}

			require.NoError(t, err)
			assert.Equal(t, want, sha)
			assert.Equal(t, want, out(t, origin, "rev-parse", "topic"))
		})
	}
}

// pushTopic puts a branch topic with one commit of its own on origin.
func pushTopic(t *testing.T, clone string) {
	t.Helper()

	run(t, clone, "checkout", "--quiet", "-b", "topic")
	commitFile(t, clone, "tracked.txt", "topic\n", "topic")
	run(t, clone, "push", "--quiet", "origin", "topic")
}

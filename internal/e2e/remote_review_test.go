//go:build integration

package e2e

import (
	"os"
	"path/filepath"
	"testing"

	"komrad/internal/git"
	"komrad/internal/komradtest"
	agentv1 "komrad/proto/gen/agent/v1"

	"connectrpc.com/connect"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// TestIntegrationRemoteReviewReadsThroughTheEngine covers a panel on another machine reading an
// agent's change through the engine's api, getting what the engine's own git would give it, and
// being refused a repository no agent works in.
func TestIntegrationRemoteReviewReadsThroughTheEngine(t *testing.T) {
	isolateGit(t)

	instance := komradtest.Start(t, komradtest.Params{WorktreesDir: t.TempDir()})
	ctx := t.Context()

	request := buildWorktreeWorkflow(t)
	workflow, err := instance.Workflows.CreateWorkflow(ctx, connect.NewRequest(request))
	require.NoError(t, err)

	created, err := instance.Agents.CreateAgent(ctx, connect.NewRequest(&agentv1.CreateAgentRequest{
		Prompt:     "greet people",
		Title:      "greet",
		WorkflowId: workflow.Msg.GetWorkflow().GetId(),
	}))
	require.NoError(t, err)

	tree := created.Msg.GetAgent().GetCwd()
	base := readGitOutput(t, tree, "rev-parse", "HEAD")
	require.NoError(t, os.MkdirAll(filepath.Join(tree, "proto", "gen"), 0o755))
	writeIn(t, tree, "it's here.go", "package main\n\nfunc main() {}\n")
	writeIn(t, tree, "proto/gen/api.pb.go", "package gen\n")
	writeIn(t, tree, "README.md", "hello\nworld\n")
	runGitIn(t, tree, "add", "-A")
	runGitIn(t, tree, "commit", "--quiet", "-m", "greeting")
	head := readGitOutput(t, tree, "rev-parse", "HEAD")

	engine := git.NewEngineClient(instance.Git)
	local := git.NewClient()

	t.Run("the panel reads the agent's change as the engine's own git does", func(t *testing.T) {
		root, err := engine.GetRoot(ctx, filepath.Join(tree, "proto"))
		require.NoError(t, err)
		wantRoot, err := local.GetRoot(ctx, tree)
		require.NoError(t, err)
		assert.Equal(t, wantRoot, root)

		diffParams := git.DiffParams{
			Dir:     tree,
			FromSHA: base,
			ToSHA:   head,
			Exclude: []string{"proto/gen"},
		}
		patch, err := engine.Diff(ctx, diffParams)
		require.NoError(t, err)
		wantPatch, err := local.Diff(ctx, diffParams)
		require.NoError(t, err)
		assert.Equal(t, string(wantPatch), string(patch))
		assert.NotContains(t, string(patch), "api.pb.go")

		changed, err := engine.CountChangedFiles(ctx, git.CountChangedFilesParams{
			Dir:     tree,
			FromSHA: base,
			ToSHA:   head,
			Exclude: []string{"proto/gen"},
		})
		require.NoError(t, err)
		assert.Equal(t, 2, changed)

		files, err := engine.ReadFiles(ctx, tree, []git.FileRevision{
			{SHA: head, Path: "it's here.go"},
			{SHA: base, Path: "it's here.go"},
		})
		require.NoError(t, err)
		assert.Equal(t, map[git.FileRevision][]byte{
			{SHA: head, Path: "it's here.go"}: []byte("package main\n\nfunc main() {}\n"),
		}, files)

		mapParams := git.MapRangesParams{
			Dir:     tree,
			FromSHA: base,
			ToSHA:   head,
		}
		mappings, err := engine.MapRanges(ctx, mapParams)
		require.NoError(t, err)
		wantMappings, err := local.MapRanges(ctx, mapParams)
		require.NoError(t, err)
		assert.Equal(t, wantMappings, mappings)

		replayParams := git.ReplayParams{
			Dir:         tree,
			FromBase:    base,
			OntoBase:    base,
			SnapshotSHA: head,
		}
		replayed, err := engine.Replay(ctx, replayParams)
		require.NoError(t, err)
		wantReplayed, err := local.Replay(ctx, replayParams)
		require.NoError(t, err)
		assert.Equal(t, wantReplayed, replayed)

		isAncestor, err := engine.IsAncestor(ctx, git.IsAncestorParams{Dir: tree, MaybeAncestor: base, Of: head})
		require.NoError(t, err)
		assert.True(t, isAncestor)

		isAncestor, err = engine.IsAncestor(ctx, git.IsAncestorParams{Dir: tree, MaybeAncestor: head, Of: base})
		require.NoError(t, err)
		assert.False(t, isAncestor)
	})

	t.Run("a repository no agent works in is refused", func(t *testing.T) {
		_, err := engine.Diff(ctx, git.DiffParams{
			Dir:     request.GetCwd(),
			FromSHA: base,
			ToSHA:   head,
		})

		require.Error(t, err)
		assert.Equal(t, connect.CodePermissionDenied, connect.CodeOf(err))
	})
}

//go:build integration

package git

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"weave/internal/engine"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// shellEngine is a transport that runs the command here through a shell, which is what the far
// side of ssh does with it, with each command written to a log.
func shellEngine(t *testing.T) (engine.Remote, string) {
	t.Helper()

	log := filepath.Join(t.TempDir(), "commands")

	return engine.Remote{
		Exec:   []string{"sh", "-c", `printf '%s\n' "$1" >> ` + log + ` && exec sh -c "$1"`, "sh", "{{command}}"},
		Socket: "weave",
	}, log
}

func commandsRun(t *testing.T, log string) []string {
	t.Helper()

	out, err := os.ReadFile(log)
	if os.IsNotExist(err) {
		return nil
	}

	require.NoError(t, err)

	return strings.Split(strings.TrimSpace(string(out)), "\n")
}

// TestIntegrationEngineClientRefusesADirectoryTheEngineDoesNotHave covers the failure the panel used to show, now from the far side.
func TestIntegrationEngineClientRefusesADirectoryTheEngineDoesNotHave(t *testing.T) {
	t.Parallel()

	remote, _ := shellEngine(t)

	_, err := NewEngineClient(remote).GetRoot(t.Context(), filepath.Join(t.TempDir(), "missing"))

	require.Error(t, err)
}

// TestIntegrationEngineClientRunsGitThroughTheTransport covers the command reaching the transport quoted, and not this machine.
func TestIntegrationEngineClientRunsGitThroughTheTransport(t *testing.T) {
	t.Parallel()

	root := repo(t)
	remote, log := shellEngine(t)

	got, err := NewEngineClient(remote).GetRoot(t.Context(), root)
	require.NoError(t, err)

	want, err := client.GetRoot(t.Context(), root)
	require.NoError(t, err)
	assert.Equal(t, want, got)
	assert.Equal(t, []string{"git -C '" + root + "' 'rev-parse' '--show-toplevel'"}, commandsRun(t, log))
}

// TestIntegrationEngineClientReadsWhatTheLocalClientReads covers every read the review screen makes answering as the local client does.
func TestIntegrationEngineClientReadsWhatTheLocalClientReads(t *testing.T) {
	t.Parallel()

	root := repo(t)
	write(t, root, "it's here.go", "package main\n\nfunc main() {}\n")
	write(t, root, "proto/gen/api.pb.go", "package gen\n\nvar moved = 1\n")
	run(t, root, "add", "-A")
	run(t, root, "commit", "-m", "second")

	base, err := Run(t.Context(), root, "rev-parse", "HEAD~1")
	require.NoError(t, err)

	head, err := Run(t.Context(), root, "rev-parse", "HEAD")
	require.NoError(t, err)

	remote, _ := shellEngine(t)
	engineClient := NewEngineClient(remote)

	patch, err := engineClient.Diff(t.Context(), DiffParams{
		Dir:     root,
		FromSHA: base,
		ToSHA:   head,
		Exclude: []string{"proto/gen"},
	})
	require.NoError(t, err)

	local, err := client.Diff(t.Context(), DiffParams{
		Dir:     root,
		FromSHA: base,
		ToSHA:   head,
		Exclude: []string{"proto/gen"},
	})
	require.NoError(t, err)

	assert.Equal(t, string(local), string(patch))
	assert.Contains(t, string(patch), "it's here.go")
	assert.NotContains(t, string(patch), "api.pb.go")

	files, err := engineClient.ReadFiles(t.Context(), root, []FileRevision{
		{SHA: head, Path: "it's here.go"},
		{SHA: base, Path: "it's here.go"},
		{SHA: head, Path: "proto/gen/api.pb.go"},
	})
	require.NoError(t, err)
	assert.Equal(t, map[FileRevision][]byte{
		{SHA: head, Path: "it's here.go"}:        []byte("package main\n\nfunc main() {}\n"),
		{SHA: head, Path: "proto/gen/api.pb.go"}: []byte("package gen\n\nvar moved = 1\n"),
	}, files)

	changed, err := engineClient.CountChangedFiles(t.Context(), CountChangedFilesParams{
		Dir:     root,
		FromSHA: base,
		ToSHA:   head,
	})
	require.NoError(t, err)
	assert.Equal(t, 2, changed)

	reachable, err := engineClient.IsAncestor(t.Context(), root, base, head)
	require.NoError(t, err)
	assert.True(t, reachable)

	reachable, err = engineClient.IsAncestor(t.Context(), root, head, base)
	require.NoError(t, err)
	assert.False(t, reachable)

	want, err := client.GetTree(t.Context(), root, head)
	require.NoError(t, err)

	mappings, err := engineClient.MapRanges(t.Context(), MapRangesParams{
		Dir:     root,
		FromSHA: base,
		ToSHA:   head,
	})
	require.NoError(t, err)
	assert.Len(t, mappings, 2)

	replayed, err := engineClient.Replay(t.Context(), ReplayParams{
		Dir:         root,
		FromBase:    base,
		OntoBase:    base,
		SnapshotSHA: head,
	})
	require.NoError(t, err)
	assert.Equal(t, want, replayed.Tree)
	assert.Empty(t, replayed.Conflicts)
}

// TestIntegrationShellCommandQuotesEveryWord covers a path and an argument the far side's shell would otherwise split.
func TestIntegrationShellCommandQuotesEveryWord(t *testing.T) {
	t.Parallel()

	got := shellCommand("/var/lib/it's", []string{"show", "abc:a b.go"})

	assert.Equal(t, `git -C '/var/lib/it'\''s' 'show' 'abc:a b.go'`, got)
}

// TestIntegrationEngineRunnerRefusesAnEnvironment covers a snapshot writer's environment never being carried
// onto the engine.
func TestIntegrationEngineRunnerRefusesAnEnvironment(t *testing.T) {
	t.Parallel()

	remote, log := shellEngine(t)

	_, err := runInput(t.Context(), engineRunner{remote: remote}, t.TempDir(), []string{"GIT_INDEX_FILE=/tmp/x"}, nil, "write-tree")

	require.Error(t, err)
	assert.Empty(t, commandsRun(t, log))
}

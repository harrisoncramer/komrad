package main

import (
	"bytes"
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// TestRunAgainstTheRepository pins that the checked-in queries and renderers satisfy every
// resource.
func TestRunAgainstTheRepository(t *testing.T) {
	t.Parallel()

	var out bytes.Buffer
	require.NoError(t, run("../..", &out))
	assert.Contains(t, out.String(), "every resource has its queries and its renderer")
}

// TestRunNamesWhatIsMissing covers a tree holding none of what the resources call for.
func TestRunNamesWhatIsMissing(t *testing.T) {
	t.Parallel()

	root := t.TempDir()
	require.NoError(t, os.MkdirAll(filepath.Join(root, queriesDir), 0o755))
	require.NoError(t, os.MkdirAll(filepath.Join(root, renderDir), 0o755))
	require.NoError(t, os.WriteFile(filepath.Join(root, queriesDir, "agent.sql"), []byte("-- name: ListAgentsByKey :many\nSELECT 1;\n"), 0o644))
	require.NoError(t, os.WriteFile(filepath.Join(root, renderDir, "render.go"), []byte("package render\n"), 0o644))

	err := run(root, &bytes.Buffer{})
	require.Error(t, err)

	assert.NotContains(t, err.Error(), "agents has no ListAgentsByKey")
	require.ErrorContains(t, err, "agents has no SnapshotAgents under db/queries")
	require.ErrorContains(t, err, "agents has no Agent under internal/rpc/render")
	require.ErrorContains(t, err, "schedules has no ListSchedulesByKey under db/queries")
	require.ErrorContains(t, err, "pull_requests has no PullRequest under internal/rpc/render")
}

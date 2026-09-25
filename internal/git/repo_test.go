//go:build integration

package git

import (
	"os"
	"os/exec"
	"path/filepath"
	"testing"

	"komrad/lib/xptr"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// client is the git these tests drive.
var client = NewClient()

// TestMain keeps background gc and maintenance out of every repository these tests build.
func TestMain(m *testing.M) {
	for key, value := range map[string]string{
		"GIT_CONFIG_COUNT":   "2",
		"GIT_CONFIG_KEY_0":   "gc.auto",
		"GIT_CONFIG_VALUE_0": "0",
		"GIT_CONFIG_KEY_1":   "maintenance.auto",
		"GIT_CONFIG_VALUE_1": "false",
	} {
		if err := os.Setenv(key, value); err != nil {
			panic(err)
		}
	}

	os.Exit(m.Run())
}

func write(t *testing.T, root, path, content string) {
	t.Helper()

	full := filepath.Join(root, filepath.FromSlash(path))
	require.NoError(t, os.MkdirAll(filepath.Dir(full), 0o755))
	require.NoError(t, os.WriteFile(full, []byte(content), 0o644))
}

func run(t *testing.T, root string, args ...string) {
	t.Helper()

	cmd := exec.Command("git", append([]string{"-C", root}, args...)...)
	out, err := cmd.CombinedOutput()
	require.NoError(t, err, string(out))
}

// repo builds a repository with one commit behind it, so a snapshot has somewhere to measure from.
func repo(t *testing.T) string {
	t.Helper()

	root := t.TempDir()
	run(t, root, "init", "--initial-branch=main")
	run(t, root, "config", "user.email", "test@example.com")
	run(t, root, "config", "user.name", "test")

	write(t, root, "kept.go", "package main\n")
	write(t, root, "proto/gen/api.pb.go", "package gen\n")
	run(t, root, "add", "-A")
	run(t, root, "commit", "-m", "init")

	return root
}

// clonedRepo is a clone of a repository, so it has an origin.
func clonedRepo(t *testing.T) string {
	t.Helper()

	clone := filepath.Join(t.TempDir(), "clone")
	run(t, filepath.Dir(clone), "clone", "--quiet", repo(t), clone)

	return clone
}

// TestIntegrationGetCurrentBranch covers what the panel reads off an agent's directory, where anything git
// will not name a branch for has to come back as no branch rather than as a name.
func TestIntegrationGetCurrentBranch(t *testing.T) {
	t.Parallel()

	onMain := repo(t)

	onFeature := repo(t)
	run(t, onFeature, "checkout", "--quiet", "-b", "feature/x")

	detached := repo(t)
	head, err := Run(t.Context(), detached, "rev-parse", "HEAD")
	require.NoError(t, err)
	run(t, detached, "checkout", "--quiet", head)

	tests := []struct {
		name string
		dir  string
		want *string
	}{
		{
			name: "a directory that is not a checkout names nothing",
			dir:  t.TempDir(),
			want: nil,
		},
		{
			name: "a detached head is not a named branch",
			dir:  detached,
			want: nil,
		},
		{
			name: "a checkout on main reads main",
			dir:  onMain,
			want: xptr.Ptr("main"),
		},
		{
			name: "a branch holding a slash keeps it whole",
			dir:  onFeature,
			want: xptr.Ptr("feature/x"),
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			got := client.GetCurrentBranch(t.Context(), tt.dir)

			if tt.want == nil {
				assert.Nil(t, got)

				return
			}

			require.NotNil(t, got)
			assert.Equal(t, *tt.want, *got)
		})
	}
}

// TestIntegrationReadFilesReadsEverySideInOneCommand covers the sources of a whole patch coming back
// keyed by commit and path, with a path the commit does not hold left out rather than failing.
func TestIntegrationReadFilesReadsEverySideInOneCommand(t *testing.T) {
	t.Parallel()

	root := repo(t)
	write(t, root, "kept.go", "package main\n\nvar moved = 1\n")
	write(t, root, "added.go", "package main\n")
	run(t, root, "add", "-A")
	run(t, root, "commit", "-m", "second")

	base, err := Run(t.Context(), root, "rev-parse", "HEAD~1")
	require.NoError(t, err)

	head, err := Run(t.Context(), root, "rev-parse", "HEAD")
	require.NoError(t, err)

	files, err := client.ReadFiles(t.Context(), root, []FileRevision{
		{SHA: base, Path: "kept.go"},
		{SHA: head, Path: "kept.go"},
		{SHA: base, Path: "added.go"},
		{SHA: head, Path: "added.go"},
		{SHA: head, Path: "proto"},
	})
	require.NoError(t, err)

	assert.Equal(t, map[FileRevision][]byte{
		{SHA: base, Path: "kept.go"}:  []byte("package main\n"),
		{SHA: head, Path: "kept.go"}:  []byte("package main\n\nvar moved = 1\n"),
		{SHA: head, Path: "added.go"}: []byte("package main\n"),
	}, files)
}

// TestIntegrationReadFilesOfNothingRunsNothing covers a patch with no file to colour.
func TestIntegrationReadFilesOfNothingRunsNothing(t *testing.T) {
	t.Parallel()

	files, err := client.ReadFiles(t.Context(), t.TempDir(), nil)
	require.NoError(t, err)
	assert.Empty(t, files)
}

// TestIntegrationReadFilesRefusesADirectoryGitDoesNotHave covers the one failure that is git's rather than
// a file's.
func TestIntegrationReadFilesRefusesADirectoryGitDoesNotHave(t *testing.T) {
	t.Parallel()

	_, err := client.ReadFiles(t.Context(), t.TempDir(), []FileRevision{{SHA: "HEAD", Path: "x"}})
	require.Error(t, err)
}

// TestIntegrationDefaultBaseReadsTheCloneOnce covers what a workflow left without a base is given: what
// origin/HEAD names, else the branch the checkout is on, else main.
func TestIntegrationDefaultBaseReadsTheCloneOnce(t *testing.T) {
	t.Parallel()

	clone := clonedRepo(t)
	run(t, clone, "checkout", "--quiet", "-b", "wip")
	assert.Equal(t, "main", DefaultBase(t.Context(), clone), "origin/HEAD wins over the branch checked out")

	run(t, clone, "symbolic-ref", "-d", "refs/remotes/origin/HEAD")
	assert.Equal(t, "wip", DefaultBase(t.Context(), clone), "without origin/HEAD, the branch checked out")

	trunk := t.TempDir()
	run(t, trunk, "init", "--quiet", "-b", "trunk", ".")
	assert.Equal(t, "trunk", DefaultBase(t.Context(), trunk), "an unborn branch still has a name")

	assert.Equal(t, "main", DefaultBase(t.Context(), filepath.Join(t.TempDir(), "nowhere")), "a directory that is no repository")
	assert.Equal(t, "main", DefaultBase(t.Context(), ""), "no directory at all")
}

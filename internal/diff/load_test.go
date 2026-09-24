//go:build integration

package diff

import (
	"os"
	"os/exec"
	"path/filepath"
	"slices"
	"strings"
	"testing"

	"weave/internal/git"
	"weave/lib/xslice"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// gitClient is the git these tests drive.
var gitClient = git.NewClient()

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

// repo builds a repository with one commit behind it, so a diff has somewhere to measure from.
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

// loadRound freezes the worktree the way a round does and reads the patch back out of it, which
// is the only way the loader is ever called.
func loadRound(t *testing.T, root string, exclude ...string) *Patch {
	t.Helper()

	snapshot, err := gitClient.TakeSnapshot(t.Context(), git.TakeSnapshotParams{Dir: root, Base: "main"})
	require.NoError(t, err)

	patch, err := Load(t.Context(), gitClient, LoadParams{
		Dir:         root,
		BaseSHA:     snapshot.BaseSHA,
		SnapshotSHA: snapshot.SHA,
		Exclude:     exclude,
	})
	require.NoError(t, err)

	return patch
}

func paths(patch *Patch) []string {
	found := xslice.Map(patch.Files, func(file File) string {
		return file.Path()
	})

	slices.Sort(found)

	return found
}

// TestIntegrationLoadHonoursExcludes is the point of the setting: a workflow that names its generated
// tree stops seeing it, and everything else is untouched.
func TestIntegrationLoadHonoursExcludes(t *testing.T) {
	tests := []struct {
		name    string
		exclude []string
		want    []string
	}{
		{
			name: "nothing excluded shows every change",
			want: []string{"kept.go", "proto/gen/api.pb.go", "proto/gen/new.pb.go", "untracked.go"},
		},
		{
			name:    "a directory drops the tracked and untracked files under it",
			exclude: []string{"proto/gen"},
			want:    []string{"kept.go", "untracked.go"},
		},
		{
			name:    "a glob crosses directories, since git pathspecs match that way",
			exclude: []string{"*.pb.go"},
			want:    []string{"kept.go", "untracked.go"},
		},
		{
			name:    "a blank entry is ignored rather than excluding everything",
			exclude: []string{"", "   "},
			want:    []string{"kept.go", "proto/gen/api.pb.go", "proto/gen/new.pb.go", "untracked.go"},
		},
		{
			name:    "a path that matches nothing leaves the diff alone",
			exclude: []string{"vendor"},
			want:    []string{"kept.go", "proto/gen/api.pb.go", "proto/gen/new.pb.go", "untracked.go"},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			root := repo(t)

			write(t, root, "kept.go", "package main\n\nfunc main() {}\n")
			write(t, root, "proto/gen/api.pb.go", "package gen\n\nvar Generated = true\n")
			write(t, root, "proto/gen/new.pb.go", "package gen\n")
			write(t, root, "untracked.go", "package main\n")

			assert.Equal(t, tt.want, paths(loadRound(t, root, tt.exclude...)))
		})
	}
}

// TestIntegrationAPathSurvivesTheUserSDiffPrefixConfig keeps true the parser's one assumption, that the
// first segment of the name on the header line is a prefix to drop, which a global
// diff.noprefix would otherwise turn into a real directory eaten off every path.
func TestIntegrationAPathSurvivesTheUserSDiffPrefixConfig(t *testing.T) {
	for _, settings := range [][]string{
		{"diff.noprefix", "true"},
		{"diff.mnemonicPrefix", "true"},
		{"diff.srcPrefix", "src/"},
		{"diff.dstPrefix", "dst/"},
	} {
		t.Run(settings[0], func(t *testing.T) {
			root := repo(t)
			run(t, root, "checkout", "-b", "work")
			run(t, root, "config", settings[0], settings[1])

			write(t, root, "proto/gen/api.pb.go", "package gen\n\nvar Generated = true\n")

			assert.Equal(t, []string{"proto/gen/api.pb.go"}, paths(loadRound(t, root)))
		})
	}
}

// TestIntegrationLoadReadsBothSidesOfEveryFile covers the sources the colouring reads arriving for a
// changed file, a new one and a deleted one alike.
func TestIntegrationLoadReadsBothSidesOfEveryFile(t *testing.T) {
	t.Parallel()

	root := repo(t)

	write(t, root, "kept.go", "package main\n\nfunc main() {}\n")
	write(t, root, "untracked.go", "package main\n")
	require.NoError(t, os.Remove(filepath.Join(root, "proto/gen/api.pb.go")))

	sources := map[string][2]string{}
	for _, file := range loadRound(t, root).Files {
		sources[file.Path()] = [2]string{file.OldSource, file.NewSource}
	}

	assert.Equal(t, map[string][2]string{
		"kept.go":             {"package main\n", "package main\n\nfunc main() {}\n"},
		"untracked.go":        {"", "package main\n"},
		"proto/gen/api.pb.go": {"package gen\n", ""},
	}, sources)
}

func capture(t *testing.T, root string, args ...string) string {
	t.Helper()

	cmd := exec.Command("git", append([]string{"-C", root}, args...)...)
	out, err := cmd.CombinedOutput()
	require.NoError(t, err, string(out))

	return strings.TrimSpace(string(out))
}

// TestIntegrationTakeHoldsCommittedAndUncommittedWorkAlike pins what a round covers.
func TestIntegrationTakeHoldsCommittedAndUncommittedWorkAlike(t *testing.T) {
	root := repo(t)
	run(t, root, "checkout", "-b", "agent/one")

	write(t, root, "committed.go", "package main\n\nfunc A() {}\n")
	run(t, root, "add", "-A")
	run(t, root, "commit", "-m", "work")

	write(t, root, "kept.go", "package main\n\nfunc B() {}\n")
	write(t, root, "untracked.go", "package main\n")

	snapshot, err := gitClient.TakeSnapshot(t.Context(), git.TakeSnapshotParams{
		Dir:  root,
		Base: "main",
		Ref:  "refs/weave/rounds/one",
	})
	require.NoError(t, err)

	patch, err := Load(t.Context(), gitClient, LoadParams{
		Dir:         root,
		BaseSHA:     snapshot.BaseSHA,
		SnapshotSHA: snapshot.SHA,
	})
	require.NoError(t, err)

	assert.ElementsMatch(t, []string{"committed.go", "kept.go", "untracked.go"}, changed(patch))
}

func changed(patch *Patch) []string {
	found := make([]string, 0, len(patch.Files))
	for _, file := range patch.Files {
		if len(file.Hunks) > 0 || file.Content != "" {
			found = append(found, file.Path())
		}
	}

	return found
}

// TestIntegrationTakeIsHeldByItsRefAndReadableAfterTheWorktreeMovesOn is what a round is for: the patch reads
// the same however far the agent has moved on since.
func TestIntegrationTakeIsHeldByItsRefAndReadableAfterTheWorktreeMovesOn(t *testing.T) {
	root := repo(t)
	write(t, root, "kept.go", "package main\n\nfunc First() {}\n")

	snapshot, err := gitClient.TakeSnapshot(t.Context(), git.TakeSnapshotParams{
		Dir:  root,
		Base: "main",
		Ref:  "refs/weave/rounds/three",
	})
	require.NoError(t, err)

	write(t, root, "kept.go", "package main\n\nfunc Second() {}\n")
	require.NoError(t, os.Remove(filepath.Join(root, "proto", "gen", "api.pb.go")))

	patch, err := Load(t.Context(), gitClient, LoadParams{
		Dir:         root,
		BaseSHA:     snapshot.BaseSHA,
		SnapshotSHA: snapshot.SHA,
	})
	require.NoError(t, err)

	require.Len(t, patch.Files, 1)
	assert.Equal(t, "kept.go", patch.Files[0].Path())
	assert.Contains(t, patch.Files[0].NewSource, "func First()")
	assert.Equal(t, snapshot.SHA, capture(t, root, "rev-parse", "refs/weave/rounds/three"))
}

// TestIntegrationTakeIgnoresWhatGitIgnores pins that a snapshot obeys the repository's own ignores rather than
// staging everything on disk.
func TestIntegrationTakeIgnoresWhatGitIgnores(t *testing.T) {
	root := repo(t)
	write(t, root, ".gitignore", ".weave/\n")
	write(t, root, ".weave/reviews/round-1.md", "a review\n")

	snapshot, err := gitClient.TakeSnapshot(t.Context(), git.TakeSnapshotParams{
		Dir:  root,
		Base: "main",
		Ref:  "refs/weave/rounds/four",
	})
	require.NoError(t, err)

	patch, err := Load(t.Context(), gitClient, LoadParams{
		Dir:         root,
		BaseSHA:     snapshot.BaseSHA,
		SnapshotSHA: snapshot.SHA,
	})
	require.NoError(t, err)

	assert.NotContains(t, changed(patch), ".weave/reviews/round-1.md")
}

// TestIntegrationTakeExcludesNothingItself pins where the workflow's excludes are applied.
func TestIntegrationTakeExcludesNothingItself(t *testing.T) {
	root := repo(t)
	write(t, root, "proto/gen/api.pb.go", "package gen\n\nfunc Gen() {}\n")

	snapshot, err := gitClient.TakeSnapshot(t.Context(), git.TakeSnapshotParams{
		Dir:  root,
		Base: "main",
		Ref:  "refs/weave/rounds/six",
	})
	require.NoError(t, err)

	// The round holds the excluded file whole.
	lines, err := gitClient.ReadFileLines(t.Context(), root, snapshot.SHA, "proto/gen/api.pb.go")
	require.NoError(t, err)
	assert.Contains(t, lines, "func Gen() {}")

	// The reader is shown none of it, and is told that rather than told the agent did nothing.
	_, err = Load(t.Context(), gitClient, LoadParams{
		Dir:         root,
		BaseSHA:     snapshot.BaseSHA,
		SnapshotSHA: snapshot.SHA,
		Exclude:     []string{"proto/gen"},
	})
	require.ErrorIs(t, err, ErrAllExcluded)
	require.NotErrorIs(t, err, ErrNoChanges)
}

// TestIntegrationLoadSaysNothingChangedOnlyWhenNothingDid pins the other half of that: a worktree that really
// has not moved reads as nothing to review whether or not the workflow excludes anything.
func TestIntegrationLoadSaysNothingChangedOnlyWhenNothingDid(t *testing.T) {
	root := repo(t)

	snapshot, err := gitClient.TakeSnapshot(t.Context(), git.TakeSnapshotParams{Dir: root, Base: "main"})
	require.NoError(t, err)

	for _, exclude := range [][]string{nil, {"proto/gen"}} {
		_, err = Load(t.Context(), gitClient, LoadParams{
			Dir:         root,
			BaseSHA:     snapshot.BaseSHA,
			SnapshotSHA: snapshot.SHA,
			Exclude:     exclude,
		})
		require.ErrorIs(t, err, ErrNoChanges)
	}
}

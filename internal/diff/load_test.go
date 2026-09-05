package diff

import (
	"os"
	"os/exec"
	"path/filepath"
	"slices"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

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

	snapshot, err := Take(t.Context(), SnapshotParams{Dir: root, Base: "main"})
	require.NoError(t, err)

	patch, err := Load(t.Context(), LoadParams{
		Dir:         root,
		BaseSHA:     snapshot.BaseSHA,
		SnapshotSHA: snapshot.SHA,
		Exclude:     exclude,
	})
	require.NoError(t, err)

	return patch
}

func paths(patch *Patch) []string {
	found := make([]string, 0, len(patch.Files))
	for _, file := range patch.Files {
		found = append(found, file.Path())
	}

	slices.Sort(found)

	return found
}

// TestLoadHonoursExcludes is the point of the setting: a workflow that names its generated
// tree stops seeing it, and everything else is untouched.
func TestLoadHonoursExcludes(t *testing.T) {
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

// TestAPathSurvivesTheUserSDiffPrefixConfig keeps true the parser's one assumption, that the
// first segment of the name on the header line is a prefix to drop, which a global
// diff.noprefix would otherwise turn into a real directory eaten off every path.
func TestAPathSurvivesTheUserSDiffPrefixConfig(t *testing.T) {
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

package diff

import (
	"context"
	"os"
	"os/exec"
	"path/filepath"
	"slices"
	"strings"
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

			patch, err := Load(context.Background(), LoadParams{
				Dir:     root,
				Base:    "main",
				Exclude: tt.exclude,
			})
			require.NoError(t, err)

			assert.Equal(t, tt.want, paths(patch))
		})
	}
}

func fileIn(t *testing.T, root, path string) File {
	t.Helper()

	patch, err := Load(context.Background(), LoadParams{Dir: root, Base: "main"})
	require.NoError(t, err)

	for _, file := range patch.Files {
		if file.Path() == path {
			return file
		}
	}

	t.Fatalf("no file %q in the patch, which held %v", path, paths(patch))

	return File{}
}

// writeBinary writes logo.png with a NUL in it, which both the before and the after need, since
// a diff from a blob holding one to a blob that does not is a text diff.
func writeBinary(t *testing.T, root, tail string) {
	t.Helper()

	write(t, root, "logo.png", "\x89PNG\r\n\x1a\n\x00"+tail)
}

// TestFingerprintMovesForAFileTheDiffDrawsNothingOf is what makes the viewed mark come off a
// binary, since hunks are the reader's evidence everywhere else and a binary has none, leaving
// the mark held against the path alone and never lifting.
func TestFingerprintMovesForAFileTheDiffDrawsNothingOf(t *testing.T) {
	tests := []struct {
		name    string
		before  func(t *testing.T, root string)
		rewrite func(t *testing.T, root string)
		path    string
	}{
		{
			name: "a committed binary replaced by another commit",
			before: func(t *testing.T, root string) {
				writeBinary(t, root, "AAAA")
				run(t, root, "add", "logo.png")
				run(t, root, "commit", "-m", "add a binary")
			},
			rewrite: func(t *testing.T, root string) {
				writeBinary(t, root, "ZZZZZZZZ")
				run(t, root, "add", "logo.png")
				run(t, root, "commit", "-m", "replace the binary")
			},
			path: "logo.png",
		},
		{
			name: "a committed binary rewritten in the working tree, which is what an agent mid-turn leaves",
			before: func(t *testing.T, root string) {
				writeBinary(t, root, "AAAA")
				run(t, root, "add", "logo.png")
				run(t, root, "commit", "-m", "add a binary")
			},
			rewrite: func(t *testing.T, root string) {
				writeBinary(t, root, "ZZZZZZZZ")
			},
			path: "logo.png",
		},
		{
			name: "an untracked binary, which is in no diff git will produce",
			before: func(t *testing.T, root string) {
				writeBinary(t, root, "AAAA")
			},
			rewrite: func(t *testing.T, root string) {
				writeBinary(t, root, "ZZZZZZZZ")
			},
			path: "logo.png",
		},
		{
			name: "an untracked file too big to draw",
			before: func(t *testing.T, root string) {
				write(t, root, "dump.sql", strings.Repeat("a", maxFileBytes+1))
			},
			rewrite: func(t *testing.T, root string) {
				write(t, root, "dump.sql", strings.Repeat("b", maxFileBytes+1))
			},
			path: "dump.sql",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			root := repo(t)
			run(t, root, "checkout", "-b", "work")

			tt.before(t, root)
			read := fileIn(t, root, tt.path)
			require.Empty(t, read.Hunks, "the case only bites where the diff draws no hunks")

			tt.rewrite(t, root)
			rewritten := fileIn(t, root, tt.path)

			assert.NotEqual(t, read.Fingerprint(), rewritten.Fingerprint(),
				"rewriting the file has to drop the mark saying it was read")
		})
	}
}

// TestModeOnlyChangeSaysSo covers the one file git shows with neither hunks nor content: its
// permissions moved and nothing else did, so the mode is the whole of what the row has to say.
func TestModeOnlyChangeSaysSo(t *testing.T) {
	root := repo(t)
	run(t, root, "checkout", "-b", "work")
	require.NoError(t, os.Chmod(filepath.Join(root, "kept.go"), 0o755))

	changed := fileIn(t, root, "kept.go")
	assert.Equal(t, []string{"mode 0644 to 0755"}, changed.Metadata)
	assert.Empty(t, changed.Hunks)
}

// TestANewFileIsNotAModeChange keeps the mode line off a file that has only ever had one mode,
// since git names a mode for both sides of a file arriving and a file leaving.
func TestANewFileIsNotAModeChange(t *testing.T) {
	root := repo(t)
	run(t, root, "checkout", "-b", "work")

	write(t, root, "added.go", "package added\n\nfunc Added() {}\n")
	run(t, root, "add", "added.go")
	run(t, root, "rm", "-q", "kept.go")

	assert.Equal(t, []string{"new file"}, fileIn(t, root, "added.go").Metadata)
	assert.Equal(t, []string{"deleted file"}, fileIn(t, root, "kept.go").Metadata)
}

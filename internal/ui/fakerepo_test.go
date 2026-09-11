package ui

import (
	"context"
	"strings"

	"weave/internal/git"
)

// fakePatch is what git writes between the two commits fakeRepo stands for, taken from a real
// repository so the parser meets the bytes it would meet in one.
const fakePatch = `diff --git a/greet.go b/greet.go
index 0a90f17972dec66c378df67739faaf48b6235df9..23cef252a97433f151c14725c88e0c2dafce63ef 100644
--- a/greet.go
+++ b/greet.go
@@ -1,5 +1,5 @@
 package greet
 
 func Hello() string {
-	return "hello"
+	return "goodbye"
 }
`

const (
	fakeBaseSHA     = "39d09b1e629b114cf0fb9651479f2225948c20ea"
	fakeSnapshotSHA = "ad9807a52747d7475b923c65fcd6d8f3b7ce74d6"
	fakeBaseSource  = "package greet\n\nfunc Hello() string {\n\treturn \"hello\"\n}\n"
	fakeHeadSource  = "package greet\n\nfunc Hello() string {\n\treturn \"goodbye\"\n}\n"
)

// fakeRepo is the git a review screen reads, answering from what a test set rather than from a
// repository on disk.
type fakeRepo struct {
	// fakeGit refuses everything a review screen does not read from this fixture.
	fakeGit
	// root is the directory every read is answered against.
	root string
	// patch is what a diff of the round answers with.
	patch string
	// tree is what the worktree hashes to now, and held what the round froze.
	tree string
	held string
}

// newFakeRepo is a repository holding one changed file, which is what most review tests draw.
func newFakeRepo(root string) *fakeRepo {
	return &fakeRepo{
		root:  root,
		patch: fakePatch,
		tree:  "tree-unmoved",
		held:  "tree-unmoved",
	}
}

func (f *fakeRepo) GetRoot(_ context.Context, dir string) (string, error) {
	if f.root != "" {
		return f.root, nil
	}

	return dir, nil
}

func (f *fakeRepo) Diff(_ context.Context, _ git.DiffParams) ([]byte, error) {
	return []byte(f.patch), nil
}

func (f *fakeRepo) ReadFile(_ context.Context, _, sha, _ string) ([]byte, error) {
	if sha == fakeSnapshotSHA {
		return []byte(fakeHeadSource), nil
	}

	return []byte(fakeBaseSource), nil
}

func (f *fakeRepo) ReadFileLines(ctx context.Context, dir, sha, path string) ([]string, error) {
	out, err := f.ReadFile(ctx, dir, sha, path)
	if err != nil {
		return nil, err
	}

	return strings.Split(strings.TrimSuffix(string(out), "\n"), "\n"), nil
}

func (f *fakeRepo) ProbeTree(context.Context, string) (string, error) {
	return f.tree, nil
}

func (f *fakeRepo) GetTree(context.Context, string, string) (string, error) {
	return f.held, nil
}

// WorktreeHead answers as a round nothing has checked out yet, which is what a screen opening
// on one finds.
func (f *fakeRepo) WorktreeHead(context.Context, string) (string, bool) {
	return "", false
}

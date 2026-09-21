package ui

import (
	"context"
	"fmt"
	"strings"

	"weave/internal/git"
)

// fakePatch is what git writes between the two commits fakeRepo stands for, taken from a real
// repository.
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

// fakePatchWrittenOver is the same file written over a second time.
const fakePatchWrittenOver = `diff --git a/greet.go b/greet.go
index 0a90f17972dec66c378df67739faaf48b6235df9..5586caad9270132f074ee5374d4652b83d0acc98 100644
--- a/greet.go
+++ b/greet.go
@@ -1,5 +1,5 @@
 package greet
 
 func Hello() string {
-	return "hello"
+	return "written by the agent"
 }
`

// fakePatchOfAnAddedFile is a worktree whose only work is a file git had never seen.
const fakePatchOfAnAddedFile = `diff --git a/new.go b/new.go
new file mode 100644
index 0000000000000000000000000000000000000000..eb3519d6032ff1d467fca3ee4bca7670dd93c393
--- /dev/null
+++ b/new.go
@@ -0,0 +1 @@
+package greet
`

// fakePatchWithADeletion changes one file and removes another.
const fakePatchWithADeletion = `diff --git a/gone.go b/gone.go
deleted file mode 100644
index 2e28ecec343b169b8c4b27117080bd08dc427c5d..0000000000000000000000000000000000000000
--- a/gone.go
+++ /dev/null
@@ -1,3 +0,0 @@
-package greet
-
-func Gone() {}
diff --git a/greet.go b/greet.go
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

// fakePatchOfBothFiles is the whole change of a worktree the agent wrote a second file into.
const fakePatchOfBothFiles = fakePatch + `diff --git a/second.go b/second.go
new file mode 100644
index 0000000000000000000000000000000000000000..6f3be6f46b20679ed1c38500fd37b317742c260e
--- /dev/null
+++ b/second.go
@@ -0,0 +1,3 @@
+package greet
+
+func Second() {}
`

// fakePatchOfTheSecondFile is what git reports between those two rounds.
const fakePatchOfTheSecondFile = `diff --git a/second.go b/second.go
new file mode 100644
index 0000000000000000000000000000000000000000..6f3be6f46b20679ed1c38500fd37b317742c260e
--- /dev/null
+++ b/second.go
@@ -0,0 +1,3 @@
+package greet
+
+func Second() {}
`

// fakePatchOfAnotherFile is a second file holding enough lines for a jump to land in.
const fakePatchOfAnotherFile = `diff --git a/second.go b/second.go
new file mode 100644
index 0000000000000000000000000000000000000000..a3c7f6e558464c8b1b0eab6d0a6c53687c253ae3
--- /dev/null
+++ b/second.go
@@ -0,0 +1,5 @@
+package greet
+
+func Second() string {
+	return "second"
+}
`

// fakePatchOfATallFile changes one line in the middle of a file long enough that three lines of
// context leave most of it off the screen.
const fakePatchOfATallFile = `diff --git a/tall.go b/tall.go
index 0a90f17972dec66c378df67739faaf48b6235df9..23cef252a97433f151c14725c88e0c2dafce63ef 100644
--- a/tall.go
+++ b/tall.go
@@ -5,7 +5,7 @@ package tall
 func One() {}
 
 func Two() {
-	fmt.Println("one")
+	fmt.Println("two")
 }
 
 func Three() {}
`

// fakePatchInAnExcludedPath is a worktree whose every changed line sits inside an excluded path.
const fakePatchInAnExcludedPath = `diff --git a/proto/gen/api.pb.go b/proto/gen/api.pb.go
new file mode 100644
index 0000000000000000000000000000000000000000..15c192a3393746be504c4f3ffeb5bcf24a264988
--- /dev/null
+++ b/proto/gen/api.pb.go
@@ -0,0 +1 @@
+package gen
`

const (
	// snapshotPrefix is what a round's commit is named after the tree it froze.
	snapshotPrefix  = "snapshot-"
	fakeBaseSHA     = "39d09b1e629b114cf0fb9651479f2225948c20ea"
	fakeTree        = "tree-unmoved"
	fakeFile        = "greet.go"
	fakeSnapshotSHA = snapshotPrefix + fakeTree
	fakeBaseSource  = "package greet\n\nfunc Hello() string {\n\treturn \"hello\"\n}\n"
	fakeHeadSource  = "package greet\n\nfunc Hello() string {\n\treturn \"goodbye\"\n}\n"
	fakeTallFile    = "tall.go"
	// The two sources of the tall file differ only on line eight, so the lines the patch leaves
	// out are lines one to four and twelve to thirteen.
	fakeTallBase = "package tall\n\nimport \"fmt\"\n\nfunc One() {}\n\nfunc Two() {\n\tfmt.Println(\"one\")\n}" +
		"\n\nfunc Three() {}\n\nfunc Four() {}\n"
	fakeTallHead = "package tall\n\nimport \"fmt\"\n\nfunc One() {}\n\nfunc Two() {\n\tfmt.Println(\"two\")\n}" +
		"\n\nfunc Three() {}\n\nfunc Four() {}\n"
)

// replayOf names one replay: the base a snapshot was measured from, the base it is carried
// onto, and the snapshot.
func replayOf(fromBase, ontoBase, snapshot string) string {
	return fromBase + ">" + ontoBase + ":" + snapshot
}

// sourceOf names one file's whole source: the snapshot holding it and its path.
func sourceOf(sha, path string) string {
	return sha + ":" + path
}

// snapshotOf is the round a tree is frozen as.
func snapshotOf(tree string) string {
	return snapshotPrefix + tree
}

// pairOf names one diff: the two snapshots it is read between.
func pairOf(from, to string) string {
	return from + ".." + to
}

// fakeRepo is the git a review screen reads, answering for the snapshots a test says the agent
// took rather than from a repository on disk.
type fakeRepo struct {
	// fakeGit refuses everything a review screen does not read from this fixture.
	fakeGit
	// root is the directory every read is answered against.
	root string
	// rootErr is a directory no repository can be found for.
	rootErr error
	// tree is what the worktree hashes to now.
	tree string
	// patches is what git reports between a pair of snapshots, and a pair nobody named is a pair
	// nothing changed between.
	patches map[string]string
	// hidden is the pairs whose every changed path is one the workflow keeps out of the diff.
	hidden map[string]bool
	// sources is the file's whole source in each snapshot, which the colouring reads.
	sources map[string]string
	// replays is what carrying a snapshot onto another base answers, keyed by the three commits,
	// and a triple nobody stated is a repository that cannot replay.
	replays map[string]*git.Replayed
	// rewritten is the base pairs whose older base the newer one cannot reach.
	rewritten map[string]bool
}

// newFakeRepo is a repository holding one changed file.
func newFakeRepo(root string) *fakeRepo {
	return &fakeRepo{
		root:    root,
		tree:    fakeTree,
		patches: map[string]string{pairOf(fakeBaseSHA, fakeSnapshotSHA): fakePatch},
		hidden:  map[string]bool{},
		sources: map[string]string{
			sourceOf(fakeBaseSHA, fakeFile):     fakeBaseSource,
			sourceOf(fakeSnapshotSHA, fakeFile): fakeHeadSource,
		},
	}
}

// newTallFakeRepo is a repository whose one changed file is long enough to have lines the patch
// does not draw.
func newTallFakeRepo(root string) *fakeRepo {
	return &fakeRepo{
		root:    root,
		tree:    fakeTree,
		patches: map[string]string{pairOf(fakeBaseSHA, fakeSnapshotSHA): fakePatchOfATallFile},
		hidden:  map[string]bool{},
		sources: map[string]string{
			sourceOf(fakeBaseSHA, fakeTallFile):     fakeTallBase,
			sourceOf(fakeSnapshotSHA, fakeTallFile): fakeTallHead,
		},
	}
}

// wroteParams is one turn of an agent's work: the tree the worktree now hashes to, and what git
// reports for it.
type wroteParams struct {
	// Tree is what the worktree hashes to afterwards, which names the round taken of it.
	Tree string
	// Whole is what git reports between the base and this snapshot.
	Whole string
	// Since is what git reports between the snapshot before it and this one.
	Since string
	// Sources is the whole source each path holds in this snapshot, keyed by path.
	Sources map[string]string
	// Hidden says every changed path is one the workflow keeps out of the diff.
	Hidden bool
}

// wrote is the agent writing again, which moves the worktree onto a tree of its own.
func (f *fakeRepo) wrote(params wroteParams) {
	previous := snapshotOf(f.tree)
	f.tree = params.Tree

	sha := snapshotOf(params.Tree)
	f.patches[pairOf(fakeBaseSHA, sha)] = params.Whole

	if params.Since != "" {
		f.patches[pairOf(previous, sha)] = params.Since
	}

	for path, source := range params.Sources {
		f.sources[sourceOf(sha, path)] = source
	}

	if params.Hidden {
		f.hidden[pairOf(fakeBaseSHA, sha)] = true
	}
}

func (f *fakeRepo) Replay(_ context.Context, params git.ReplayParams) (*git.Replayed, error) {
	replayed, stated := f.replays[replayOf(params.FromBase, params.OntoBase, params.SnapshotSHA)]
	if !stated {
		return nil, fmt.Errorf("this test stated no replay of %s from %s onto %s: %w",
			params.SnapshotSHA, params.FromBase, params.OntoBase, errNoGit)
	}

	return replayed, nil
}

func (f *fakeRepo) IsAncestor(_ context.Context, _, maybeAncestor, of string) (bool, error) {
	return !f.rewritten[pairOf(maybeAncestor, of)], nil
}

func (f *fakeRepo) GetRoot(_ context.Context, dir string) (string, error) {
	if f.rootErr != nil {
		return "", f.rootErr
	}

	if f.root != "" {
		return f.root, nil
	}

	return dir, nil
}

func (f *fakeRepo) Diff(_ context.Context, params git.DiffParams) ([]byte, error) {
	patch, err := f.patchBetween(patchBetweenParams{
		FromSHA: params.FromSHA,
		ToSHA:   params.ToSHA,
		Exclude: params.Exclude,
	})
	if err != nil {
		return nil, err
	}

	return []byte(patch), nil
}

func (f *fakeRepo) CountChangedFiles(_ context.Context, params git.CountChangedFilesParams) (int, error) {
	patch, err := f.patchBetween(patchBetweenParams{
		FromSHA: params.FromSHA,
		ToSHA:   params.ToSHA,
		Exclude: params.Exclude,
	})
	if err != nil {
		return 0, err
	}

	return strings.Count(patch, "diff --git "), nil
}

// patchBetweenParams is one read of a diff: the two snapshots, and the paths left out of it.
type patchBetweenParams struct {
	FromSHA string
	ToSHA   string
	Exclude []string
}

// patchBetween is what git reports between two snapshots, refusing a pair the test never stated
// rather than answering as a pair nothing changed between.
func (f *fakeRepo) patchBetween(params patchBetweenParams) (string, error) {
	// A snapshot against itself is nothing changed.
	if params.FromSHA == params.ToSHA {
		return "", nil
	}

	pair := pairOf(params.FromSHA, params.ToSHA)

	patch, stated := f.patches[pair]
	if !stated {
		return "", fmt.Errorf("this test stated no diff between %s and %s: %w",
			params.FromSHA, params.ToSHA, errNoGit)
	}

	if len(params.Exclude) > 0 && f.hidden[pair] {
		return "", nil
	}

	return patch, nil
}

func (f *fakeRepo) ReadFile(_ context.Context, _, sha, path string) ([]byte, error) {
	source, stated := f.sources[sourceOf(sha, path)]
	if !stated {
		return nil, fmt.Errorf("the snapshot %s holds no %s: %w", sha, path, errNoGit)
	}

	return []byte(source), nil
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

// GetTree answers with the tree a round froze, which its own name carries.
func (f *fakeRepo) GetTree(_ context.Context, _, sha string) (string, error) {
	return strings.TrimPrefix(sha, snapshotPrefix), nil
}

// WorktreeHead answers as a round nothing has checked out yet.
func (f *fakeRepo) WorktreeHead(context.Context, string) (string, bool) {
	return "", false
}

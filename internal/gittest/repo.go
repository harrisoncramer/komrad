package gittest

import (
	"context"
	"fmt"
	"strings"
	"sync"

	"weave/internal/git"
	"weave/internal/position"
)

// Patch is what git writes between the two commits Repo stands for, taken from a real
// repository.
const Patch = `diff --git a/greet.go b/greet.go
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

// PatchWrittenOver is the same file written over a second time.
const PatchWrittenOver = `diff --git a/greet.go b/greet.go
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

// PatchOfAnAddedFile is a worktree whose only work is a file git had never seen.
const PatchOfAnAddedFile = `diff --git a/new.go b/new.go
new file mode 100644
index 0000000000000000000000000000000000000000..eb3519d6032ff1d467fca3ee4bca7670dd93c393
--- /dev/null
+++ b/new.go
@@ -0,0 +1 @@
+package greet
`

// PatchWithADeletion changes one file and removes another.
const PatchWithADeletion = `diff --git a/gone.go b/gone.go
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

// PatchOfBothFiles is the whole change of a worktree the agent wrote a second file into.
const PatchOfBothFiles = Patch + `diff --git a/second.go b/second.go
new file mode 100644
index 0000000000000000000000000000000000000000..6f3be6f46b20679ed1c38500fd37b317742c260e
--- /dev/null
+++ b/second.go
@@ -0,0 +1,3 @@
+package greet
+
+func Second() {}
`

// PatchOfTheSecondFile is what git reports between those two rounds.
const PatchOfTheSecondFile = `diff --git a/second.go b/second.go
new file mode 100644
index 0000000000000000000000000000000000000000..6f3be6f46b20679ed1c38500fd37b317742c260e
--- /dev/null
+++ b/second.go
@@ -0,0 +1,3 @@
+package greet
+
+func Second() {}
`

// PatchOfAnotherFile is a second file holding enough lines for a jump to land in.
const PatchOfAnotherFile = `diff --git a/second.go b/second.go
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

// PatchOfATallFile changes one line in the middle of a file long enough that three lines of
// context leave most of it off the screen.
const PatchOfATallFile = `diff --git a/tall.go b/tall.go
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

// PatchInAnExcludedPath is a worktree whose every changed line sits inside an excluded path.
const PatchInAnExcludedPath = `diff --git a/proto/gen/api.pb.go b/proto/gen/api.pb.go
new file mode 100644
index 0000000000000000000000000000000000000000..15c192a3393746be504c4f3ffeb5bcf24a264988
--- /dev/null
+++ b/proto/gen/api.pb.go
@@ -0,0 +1 @@
+package gen
`

const (
	// SnapshotPrefix is what a round's commit is named after the tree it froze.
	SnapshotPrefix = "snapshot-"
	BaseSHA        = "39d09b1e629b114cf0fb9651479f2225948c20ea"
	UnmovedTree    = "tree-unmoved"
	File           = "greet.go"
	SnapshotSHA    = SnapshotPrefix + UnmovedTree
	BaseSource     = "package greet\n\nfunc Hello() string {\n\treturn \"hello\"\n}\n"
	HeadSource     = "package greet\n\nfunc Hello() string {\n\treturn \"goodbye\"\n}\n"
	TallFile       = "tall.go"
	// The two sources of the tall file differ only on line eight, so the lines the patch leaves
	// out are lines one to four and twelve to thirteen.
	TallBase = "package tall\n\nimport \"fmt\"\n\nfunc One() {}\n\nfunc Two() {\n\tfmt.Println(\"one\")\n}" +
		"\n\nfunc Three() {}\n\nfunc Four() {}\n"
	TallHead = "package tall\n\nimport \"fmt\"\n\nfunc One() {}\n\nfunc Two() {\n\tfmt.Println(\"two\")\n}" +
		"\n\nfunc Three() {}\n\nfunc Four() {}\n"
)

// Repo is the git a review screen reads, answering for the snapshots a test says the agent
// took rather than from a repository on disk.
type Repo struct {
	// NoRepo refuses everything a review screen does not read from this fixture.
	NoRepo
	// Root is the directory every read is answered against.
	Root string
	// RootErr is a directory no repository can be found for.
	RootErr error
	// Tree is what the worktree hashes to now.
	Tree string
	// Patches is what git reports between a pair of snapshots, and a pair nobody named is a pair
	// nothing changed between.
	Patches map[string]string
	// Hidden is the pairs whose every changed path is one the workflow keeps out of the diff.
	Hidden map[string]bool
	// Sources is the file's whole source in each snapshot, which the colouring reads.
	Sources map[string]string
	// Replays is what carrying a snapshot onto another base answers, keyed by the three commits,
	// and a triple nobody stated is a repository that cannot replay.
	Replays map[string]*git.Replayed
	// Rewritten is the base pairs whose older base the newer one cannot reach.
	Rewritten map[string]bool
	// Mappings is where each line of one snapshot sits in another, keyed by the pair.
	Mappings map[string][]position.Mapping
	// Before runs ahead of every read, when set, with the read's name such as git.Diff.
	Before func(ctx context.Context, name string)

	mu            sync.Mutex
	countsAsked   []git.CountChangedFilesParams
	mappingsAsked []git.MapRangesParams
}

// NewRepo is a repository holding one changed file.
func NewRepo(root string) *Repo {
	return &Repo{
		Root:    root,
		Tree:    UnmovedTree,
		Patches: map[string]string{NamePair(BaseSHA, SnapshotSHA): Patch},
		Hidden:  map[string]bool{},
		Sources: map[string]string{
			NameSource(BaseSHA, File):     BaseSource,
			NameSource(SnapshotSHA, File): HeadSource,
		},
	}
}

// NewTallRepo is a repository whose one changed file is long enough to have lines the patch
// does not draw.
func NewTallRepo(root string) *Repo {
	return &Repo{
		Root:    root,
		Tree:    UnmovedTree,
		Patches: map[string]string{NamePair(BaseSHA, SnapshotSHA): PatchOfATallFile},
		Hidden:  map[string]bool{},
		Sources: map[string]string{
			NameSource(BaseSHA, TallFile):     TallBase,
			NameSource(SnapshotSHA, TallFile): TallHead,
		},
	}
}

// WroteParams is one turn of an agent's work: the tree the worktree now hashes to, and what git
// reports for it.
type WroteParams struct {
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

// Wrote is the agent writing again, which moves the worktree onto a tree of its own.
func (f *Repo) Wrote(params WroteParams) {
	previous := NameSnapshot(f.Tree)
	f.Tree = params.Tree

	sha := NameSnapshot(params.Tree)
	f.Patches[NamePair(BaseSHA, sha)] = params.Whole

	if params.Since != "" {
		f.Patches[NamePair(previous, sha)] = params.Since
	}

	for path, source := range params.Sources {
		f.Sources[NameSource(sha, path)] = source
	}

	if params.Hidden {
		f.Hidden[NamePair(BaseSHA, sha)] = true
	}
}

// Replay answers the replay the test stated for these three commits.
func (f *Repo) Replay(ctx context.Context, params git.ReplayParams) (*git.Replayed, error) {
	f.before(ctx, "git.Replay")

	replayed, stated := f.Replays[NameReplay(params.FromBase, params.OntoBase, params.SnapshotSHA)]
	if !stated {
		return nil, fmt.Errorf("%w: this test stated no replay of %s from %s onto %s",
			ErrNoGit, params.SnapshotSHA, params.FromBase, params.OntoBase)
	}

	return replayed, nil
}

// IsAncestor is true unless the pair is in Rewritten.
func (f *Repo) IsAncestor(ctx context.Context, _, maybeAncestor, of string) (bool, error) {
	f.before(ctx, "git.IsAncestor")

	return !f.Rewritten[NamePair(maybeAncestor, of)], nil
}

// GetRoot answers Root, or the directory itself when Root is blank.
func (f *Repo) GetRoot(ctx context.Context, dir string) (string, error) {
	f.before(ctx, "git.GetRoot")

	if f.RootErr != nil {
		return "", f.RootErr
	}

	if f.Root != "" {
		return f.Root, nil
	}

	return dir, nil
}

// Diff answers the patch the test stated between two snapshots.
func (f *Repo) Diff(ctx context.Context, params git.DiffParams) ([]byte, error) {
	f.before(ctx, "git.Diff")

	patch, err := f.readPatchBetween(readPatchBetweenParams{
		FromSHA: params.FromSHA,
		ToSHA:   params.ToSHA,
		Exclude: params.Exclude,
	})
	if err != nil {
		return nil, err
	}

	return []byte(patch), nil
}

// CountChangedFiles counts the files in the patch between two snapshots.
func (f *Repo) CountChangedFiles(ctx context.Context, params git.CountChangedFilesParams) (int, error) {
	f.before(ctx, "git.CountChangedFiles")

	f.mu.Lock()
	f.countsAsked = append(f.countsAsked, params)
	f.mu.Unlock()

	patch, err := f.readPatchBetween(readPatchBetweenParams{
		FromSHA: params.FromSHA,
		ToSHA:   params.ToSHA,
		Exclude: params.Exclude,
	})
	if err != nil {
		return 0, err
	}

	return strings.Count(patch, "diff --git "), nil
}

// MapRanges answers the mapping the test stated between two snapshots.
func (f *Repo) MapRanges(ctx context.Context, params git.MapRangesParams) ([]position.Mapping, error) {
	f.before(ctx, "git.MapRanges")

	f.mu.Lock()
	f.mappingsAsked = append(f.mappingsAsked, params)
	f.mu.Unlock()

	mappings, stated := f.Mappings[NamePair(params.FromSHA, params.ToSHA)]
	if !stated {
		return nil, fmt.Errorf("%w: this test stated no mapping between %s and %s",
			ErrNoGit, params.FromSHA, params.ToSHA)
	}

	return mappings, nil
}

// CountsAsked is every count of changed files the repository was asked for.
func (f *Repo) CountsAsked() []git.CountChangedFilesParams {
	f.mu.Lock()
	defer f.mu.Unlock()

	return append([]git.CountChangedFilesParams(nil), f.countsAsked...)
}

// MappingsAsked is every mapping between snapshots the repository was asked for.
func (f *Repo) MappingsAsked() []git.MapRangesParams {
	f.mu.Lock()
	defer f.mu.Unlock()

	return append([]git.MapRangesParams(nil), f.mappingsAsked...)
}

// ReadFiles answers the sources the test stated for each revision.
func (f *Repo) ReadFiles(ctx context.Context, _ string, revisions []git.FileRevision) (map[git.FileRevision][]byte, error) {
	f.before(ctx, "git.ReadFiles")

	files := make(map[git.FileRevision][]byte, len(revisions))
	for _, revision := range revisions {
		if source, stated := f.Sources[NameSource(revision.SHA, revision.Path)]; stated {
			files[revision] = []byte(source)
		}
	}

	return files, nil
}

// WorktreeHead answers as a round nothing has checked out yet.
func (f *Repo) WorktreeHead(context.Context, string) (string, bool) {
	return "", false
}

func (f *Repo) before(ctx context.Context, name string) {
	if f.Before != nil {
		f.Before(ctx, name)
	}
}

// NameReplay names one replay: the base a snapshot was measured from, the base it is carried
// onto, and the snapshot.
func NameReplay(fromBase, ontoBase, snapshot string) string {
	return fromBase + ">" + ontoBase + ":" + snapshot
}

// NameSource names one file's whole source: the snapshot holding it and its path.
func NameSource(sha, path string) string {
	return sha + ":" + path
}

// NameSnapshot is the round a tree is frozen as.
func NameSnapshot(tree string) string {
	return SnapshotPrefix + tree
}

// NamePair names one diff: the two snapshots it is read between.
func NamePair(from, to string) string {
	return from + ".." + to
}

// readPatchBetweenParams is one read of a diff: the two snapshots, and the paths left out of it.
type readPatchBetweenParams struct {
	FromSHA string
	ToSHA   string
	Exclude []string
}

// readPatchBetween is what git reports between two snapshots, refusing a pair the test never stated
// rather than answering as a pair nothing changed between.
func (f *Repo) readPatchBetween(params readPatchBetweenParams) (string, error) {
	// A snapshot against itself is nothing changed.
	if params.FromSHA == params.ToSHA {
		return "", nil
	}

	pair := NamePair(params.FromSHA, params.ToSHA)

	patch, stated := f.Patches[pair]
	if !stated {
		return "", fmt.Errorf("%w: this test stated no diff between %s and %s",
			ErrNoGit, params.FromSHA, params.ToSHA)
	}

	if len(params.Exclude) > 0 && f.Hidden[pair] {
		return "", nil
	}

	return patch, nil
}

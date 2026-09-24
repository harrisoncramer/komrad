package git

import (
	"context"
	"os/exec"
	"strings"

	"weave/internal/engine"
	"weave/internal/position"
	"weave/lib/xslice"
)

// EngineClient is git run on the engine an agent's worktree is on, reached through its transport.
type EngineClient struct {
	remote engine.Remote
	on     runner
}

// NewEngineClient returns a client that runs each command on the engine rather than here.
func NewEngineClient(remote engine.Remote) *EngineClient {
	return &EngineClient{remote: remote, on: engineRunner{remote: remote}}
}

// GetRoot is the top of the working tree any path inside it belongs to.
func (c *EngineClient) GetRoot(ctx context.Context, dir string) (string, error) {
	return getRoot(ctx, c.on, dir)
}

// ReadFiles is every named file as its commit holds it, read in one command, with a file the
// commit does not hold left out.
func (c *EngineClient) ReadFiles(ctx context.Context, dir string, revisions []FileRevision) (map[FileRevision][]byte, error) {
	return readFiles(ctx, c.on, dir, revisions)
}

// CountChangedFiles is how many files differ between two commits.
func (c *EngineClient) CountChangedFiles(ctx context.Context, params CountChangedFilesParams) (int, error) {
	return countChangedFiles(ctx, c.on, params)
}

// Diff is the patch between two commits, written with the prefixes a parser of it expects.
func (c *EngineClient) Diff(ctx context.Context, params DiffParams) ([]byte, error) {
	return diff(ctx, c.on, params)
}

// MapRanges is what the later commit did to each file, in the form the position package moves a
// place through.
func (c *EngineClient) MapRanges(ctx context.Context, params MapRangesParams) ([]position.Mapping, error) {
	return mapRanges(ctx, c.on, params)
}

// Replay carries a snapshot's own change onto another base in memory, answering with the tree
// that results and the paths that would not settle.
func (c *EngineClient) Replay(ctx context.Context, params ReplayParams) (*Replayed, error) {
	return replay(ctx, c.on, params)
}

// IsAncestor reports whether one commit is reachable from another.
func (c *EngineClient) IsAncestor(ctx context.Context, dir, maybeAncestor, of string) (bool, error) {
	return isAncestor(ctx, c.on, dir, maybeAncestor, of)
}

type engineRunner struct {
	remote engine.Remote
}

func (r engineRunner) command(ctx context.Context, dir string, env []string, args ...string) *exec.Cmd {
	if len(env) > 0 {
		return exec.CommandContext(ctx, "")
	}

	return engineCommand(ctx, r.remote, shellCommand(dir, args))
}

func engineCommand(ctx context.Context, remote engine.Remote, command string) *exec.Cmd {
	argv := remote.CommandArgv(command)
	if len(argv) == 0 {
		return exec.CommandContext(ctx, "")
	}

	return exec.CommandContext(ctx, argv[0], argv[1:]...)
}

func shellCommand(dir string, args []string) string {
	words := append([]string{"git", "-C", engine.Quote(dir)}, xslice.Map(args, engine.Quote)...)

	return strings.Join(words, " ")
}

package git

import (
	"context"
	"fmt"

	"komrad/internal/position"
	"komrad/lib/xslice"
	gitv1 "komrad/proto/gen/git/v1"
	"komrad/proto/gen/git/v1/gitv1connect"

	"connectrpc.com/connect"
)

// EngineClient is git run on the engine an agent's worktree is on, reached through its api.
type EngineClient struct {
	client gitv1connect.GitServiceClient
}

// NewEngineClient returns a client that asks the engine behind client to run each command.
func NewEngineClient(client gitv1connect.GitServiceClient) *EngineClient {
	return &EngineClient{client: client}
}

// GetRoot is the top of the working tree any path inside it belongs to.
func (c *EngineClient) GetRoot(ctx context.Context, dir string) (string, error) {
	response, err := c.client.GetRoot(ctx, connect.NewRequest(&gitv1.GetRootRequest{Dir: dir}))
	if err != nil {
		return "", fmt.Errorf("failed to read the top of %q on the engine: %w", dir, err)
	}

	return response.Msg.GetRoot(), nil
}

// ReadFiles is every named file as its commit holds it, read in one command, with a file the
// commit does not hold left out.
func (c *EngineClient) ReadFiles(ctx context.Context, dir string, revisions []FileRevision) (map[FileRevision][]byte, error) {
	files := make(map[FileRevision][]byte, len(revisions))
	if len(revisions) == 0 {
		return files, nil
	}

	response, err := c.client.ReadFiles(ctx, connect.NewRequest(&gitv1.ReadFilesRequest{
		Dir: dir,
		Revisions: xslice.Map(revisions, func(revision FileRevision) *gitv1.FileRevision {
			return &gitv1.FileRevision{Sha: revision.SHA, Path: revision.Path}
		}),
	}))
	if err != nil {
		return nil, fmt.Errorf("failed to read %d files of %q on the engine: %w", len(revisions), dir, err)
	}

	for _, file := range response.Msg.GetFiles() {
		revision := FileRevision{SHA: file.GetRevision().GetSha(), Path: file.GetRevision().GetPath()}
		files[revision] = append([]byte{}, file.GetContent()...)
	}

	return files, nil
}

// CountChangedFiles is how many files differ between two commits.
func (c *EngineClient) CountChangedFiles(ctx context.Context, params CountChangedFilesParams) (int, error) {
	response, err := c.client.CountChangedFiles(ctx, connect.NewRequest(&gitv1.CountChangedFilesRequest{
		Dir:     params.Dir,
		FromSha: params.FromSHA,
		ToSha:   params.ToSHA,
		Exclude: params.Exclude,
	}))
	if err != nil {
		return 0, fmt.Errorf("failed to compare %q against %q on the engine: %w", params.FromSHA, params.ToSHA, err)
	}

	return int(response.Msg.GetCount()), nil
}

// Diff is the patch between two commits, written with the prefixes a parser of it expects.
func (c *EngineClient) Diff(ctx context.Context, params DiffParams) ([]byte, error) {
	response, err := c.client.Diff(ctx, connect.NewRequest(&gitv1.DiffRequest{
		Dir:     params.Dir,
		FromSha: params.FromSHA,
		ToSha:   params.ToSHA,
		Exclude: params.Exclude,
	}))
	if err != nil {
		return nil, fmt.Errorf("failed to diff %q against %q on the engine: %w", params.FromSHA, params.ToSHA, err)
	}

	return response.Msg.GetPatch(), nil
}

// MapRanges is what the later commit did to each file, in the form the position package moves a
// place through.
func (c *EngineClient) MapRanges(ctx context.Context, params MapRangesParams) ([]position.Mapping, error) {
	response, err := c.client.MapRanges(ctx, connect.NewRequest(&gitv1.MapRangesRequest{
		Dir:     params.Dir,
		FromSha: params.FromSHA,
		ToSha:   params.ToSHA,
	}))
	if err != nil {
		return nil, fmt.Errorf("failed to map %s onto %s on the engine: %w", params.FromSHA, params.ToSHA, err)
	}

	return xslice.Map(response.Msg.GetFiles(), fromFileMapping), nil
}

// Replay carries a snapshot's own change onto another base in memory, answering with the tree
// that results and the paths that would not settle.
func (c *EngineClient) Replay(ctx context.Context, params ReplayParams) (*Replayed, error) {
	response, err := c.client.Replay(ctx, connect.NewRequest(&gitv1.ReplayRequest{
		Dir:         params.Dir,
		FromBase:    params.FromBase,
		OntoBase:    params.OntoBase,
		SnapshotSha: params.SnapshotSHA,
	}))
	if err != nil {
		return nil, fmt.Errorf("failed to replay %s from %s onto %s on the engine: %w",
			params.SnapshotSHA, params.FromBase, params.OntoBase, err)
	}

	return &Replayed{Tree: response.Msg.GetTree(), Conflicts: response.Msg.GetConflicts()}, nil
}

// IsAncestor reports whether one commit is reachable from another.
func (c *EngineClient) IsAncestor(ctx context.Context, params IsAncestorParams) (bool, error) {
	response, err := c.client.IsAncestor(ctx, connect.NewRequest(&gitv1.IsAncestorRequest{
		Dir:           params.Dir,
		MaybeAncestor: params.MaybeAncestor,
		Of:            params.Of,
	}))
	if err != nil {
		return false, fmt.Errorf("failed to ask whether %s is an ancestor of %s on the engine: %w", params.MaybeAncestor, params.Of, err)
	}

	return response.Msg.GetIsAncestor(), nil
}

func fromFileMapping(file *gitv1.FileMapping) position.Mapping {
	return position.Mapping{
		File: position.FileMapping{OldPath: file.GetOldPath(), NewPath: file.GetNewPath()},
		Ranges: xslice.Map(file.GetRanges(), func(ranges *gitv1.RangeMapping) position.RangeMapping {
			return position.RangeMapping{
				Old: fromLineRange(ranges.GetOldLines()),
				New: fromLineRange(ranges.GetNewLines()),
			}
		}),
	}
}

func fromLineRange(lines *gitv1.LineRange) position.Range {
	return position.Range{Start: int(lines.GetStart()), End: int(lines.GetEnd())}
}

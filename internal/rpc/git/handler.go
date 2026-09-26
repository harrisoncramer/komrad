// Package git is the ConnectRPC transport over the git a panel somewhere else reads a review
// through.
package git

import (
	"context"
	"math"

	"komrad/internal/fail"
	gitrepo "komrad/internal/git"
	"komrad/internal/position"
	"komrad/internal/rpcerr"
	"komrad/lib/xslice"
	gitv1 "komrad/proto/gen/git/v1"
)

type gitRunner interface {
	GetRoot(ctx context.Context, dir string) (string, error)
	ReadFiles(ctx context.Context, dir string, revisions []gitrepo.FileRevision) (map[gitrepo.FileRevision][]byte, error)
	CountChangedFiles(ctx context.Context, params gitrepo.CountChangedFilesParams) (int, error)
	Diff(ctx context.Context, params gitrepo.DiffParams) ([]byte, error)
	MapRanges(ctx context.Context, params gitrepo.MapRangesParams) ([]position.Mapping, error)
	Replay(ctx context.Context, params gitrepo.ReplayParams) (*gitrepo.Replayed, error)
	IsAncestor(ctx context.Context, dir, maybeAncestor, of string) (bool, error)
}

// NewHandlerParams is what the handler is built over.
type NewHandlerParams struct {
	Git gitRunner
}

// Handler serves the engine's git.
type Handler struct {
	git gitRunner
}

// NewHandler builds the handler.
func NewHandler(params NewHandlerParams) *Handler {
	return &Handler{git: params.Git}
}

// GetRoot answers with the top of the working tree a directory belongs to.
func (h *Handler) GetRoot(ctx context.Context, req *gitv1.GetRootRequest) (*gitv1.GetRootResponse, *fail.Error) {
	root, err := h.git.GetRoot(ctx, req.GetDir())
	if err != nil {
		return nil, rpcerr.From(err, "failed to read the top of the repository")
	}

	return &gitv1.GetRootResponse{Root: root}, nil
}

// ReadFiles answers with each named file its commit holds, in the order they were asked for.
func (h *Handler) ReadFiles(ctx context.Context, req *gitv1.ReadFilesRequest) (*gitv1.ReadFilesResponse, *fail.Error) {
	revisions := xslice.Map(req.GetRevisions(), func(revision *gitv1.FileRevision) gitrepo.FileRevision {
		return gitrepo.FileRevision{SHA: revision.GetSha(), Path: revision.GetPath()}
	})

	files, err := h.git.ReadFiles(ctx, req.GetDir(), revisions)
	if err != nil {
		return nil, rpcerr.From(err, "failed to read the files")
	}

	response := &gitv1.ReadFilesResponse{}
	for _, revision := range revisions {
		content, ok := files[revision]
		if !ok {
			continue
		}

		response.Files = append(response.Files, &gitv1.File{
			Revision: &gitv1.FileRevision{Sha: revision.SHA, Path: revision.Path},
			Content:  content,
		})
	}

	return response, nil
}

// CountChangedFiles answers with how many files differ between two commits.
func (h *Handler) CountChangedFiles(ctx context.Context, req *gitv1.CountChangedFilesRequest) (*gitv1.CountChangedFilesResponse, *fail.Error) {
	count, err := h.git.CountChangedFiles(ctx, gitrepo.CountChangedFilesParams{
		Dir:     req.GetDir(),
		FromSHA: req.GetFromSha(),
		ToSHA:   req.GetToSha(),
		Exclude: req.GetExclude(),
	})
	if err != nil {
		return nil, rpcerr.From(err, "failed to count the changed files")
	}

	return &gitv1.CountChangedFilesResponse{Count: toInt32(count)}, nil
}

// Diff answers with the patch between two commits.
func (h *Handler) Diff(ctx context.Context, req *gitv1.DiffRequest) (*gitv1.DiffResponse, *fail.Error) {
	patch, err := h.git.Diff(ctx, gitrepo.DiffParams{
		Dir:     req.GetDir(),
		FromSHA: req.GetFromSha(),
		ToSHA:   req.GetToSha(),
		Exclude: req.GetExclude(),
	})
	if err != nil {
		return nil, rpcerr.From(err, "failed to read the patch")
	}

	return &gitv1.DiffResponse{Patch: patch}, nil
}

// MapRanges answers with what the later of two commits did to each file.
func (h *Handler) MapRanges(ctx context.Context, req *gitv1.MapRangesRequest) (*gitv1.MapRangesResponse, *fail.Error) {
	mappings, err := h.git.MapRanges(ctx, gitrepo.MapRangesParams{
		Dir:     req.GetDir(),
		FromSHA: req.GetFromSha(),
		ToSHA:   req.GetToSha(),
	})
	if err != nil {
		return nil, rpcerr.From(err, "failed to map the ranges")
	}

	return &gitv1.MapRangesResponse{Files: xslice.Map(mappings, renderFileMapping)}, nil
}

// Replay answers with the tree a snapshot's own change makes on another base.
func (h *Handler) Replay(ctx context.Context, req *gitv1.ReplayRequest) (*gitv1.ReplayResponse, *fail.Error) {
	replayed, err := h.git.Replay(ctx, gitrepo.ReplayParams{
		Dir:         req.GetDir(),
		FromBase:    req.GetFromBase(),
		OntoBase:    req.GetOntoBase(),
		SnapshotSHA: req.GetSnapshotSha(),
	})
	if err != nil {
		return nil, rpcerr.From(err, "failed to replay the snapshot")
	}

	return &gitv1.ReplayResponse{Tree: replayed.Tree, Conflicts: replayed.Conflicts}, nil
}

// IsAncestor answers whether one commit is reachable from another.
func (h *Handler) IsAncestor(ctx context.Context, req *gitv1.IsAncestorRequest) (*gitv1.IsAncestorResponse, *fail.Error) {
	isAncestor, err := h.git.IsAncestor(ctx, req.GetDir(), req.GetMaybeAncestor(), req.GetOf())
	if err != nil {
		return nil, rpcerr.From(err, "failed to compare the commits")
	}

	return &gitv1.IsAncestorResponse{IsAncestor: isAncestor}, nil
}

func renderFileMapping(mapping position.Mapping) *gitv1.FileMapping {
	return &gitv1.FileMapping{
		OldPath: mapping.File.OldPath,
		NewPath: mapping.File.NewPath,
		Ranges: xslice.Map(mapping.Ranges, func(ranges position.RangeMapping) *gitv1.RangeMapping {
			return &gitv1.RangeMapping{
				OldLines: renderLineRange(ranges.Old),
				NewLines: renderLineRange(ranges.New),
			}
		}),
	}
}

func renderLineRange(lines position.Range) *gitv1.LineRange {
	return &gitv1.LineRange{Start: toInt32(lines.Start), End: toInt32(lines.End)}
}

func toInt32(n int) int32 {
	return int32(max(min(n, math.MaxInt32), math.MinInt32)) //nolint:gosec // bounded above
}

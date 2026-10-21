package pullrequests

import (
	"context"
	"errors"
	"log/slog"

	"komrad/internal/fail"
	"komrad/internal/forge"
	"komrad/internal/logging"
	"komrad/internal/store"
	pullrequeststore "komrad/internal/store/pullrequest"
	"komrad/internal/wake"
)

var (
	// errMergeHasParent says the pull request comes from a child, which merges only after its parent merges.
	errMergeHasParent = fail.Precondition("this task depends on another task, so merge that task first and komrad re-targets this one onto the base branch")
	// errMergeOntoFinished says the pull request targets a task's branch whose own pull request has merged or closed.
	errMergeOntoFinished = fail.Precondition("this pull request targets a task's branch whose own pull request has merged or closed, so re-target it onto the base branch first")
)

// cascader moves the children of a pull request that has just merged onto the base branch.
type cascader interface {
	CascadeMerged(ctx context.Context, address string) error
}

// NewWriterParams is what a writer is built over.
type NewWriterParams struct {
	Forges       *forge.Service
	Sync         wake.HostReader
	PullRequests *pullrequeststore.PullRequestStore
	Cascade      cascader
}

// Writer changes pull requests through the connection for their host.
type Writer struct {
	forges       *forge.Service
	sync         wake.HostReader
	pullRequests *pullrequeststore.PullRequestStore
	cascade      cascader
}

// NewWriter builds a writer over the forge service and the pull request sync.
func NewWriter(params NewWriterParams) *Writer {
	return &Writer{
		forges:       params.Forges,
		sync:         params.Sync,
		pullRequests: params.PullRequests,
		cascade:      params.Cascade,
	}
}

// PlanPullRequestMerge is the ways the pull request at the address could merge.
func (w *Writer) PlanPullRequestMerge(ctx context.Context, address string) (*forge.MergePlan, error) {
	_, f, err := w.forges.ForAddress(ctx, address)
	if err != nil {
		return nil, err
	}

	return f.PlanMerge(ctx, address)
}

// MergePullRequest merges the pull request, has the pull request sync read its host often, and moves its
// children onto the base branch.
func (w *Writer) MergePullRequest(ctx context.Context, params forge.MergeParams) error {
	if err := w.refuseBlockedMerge(ctx, params.URL); err != nil {
		return err
	}

	host, f, err := w.forges.ForAddress(ctx, params.URL)
	if err != nil {
		return err
	}

	if err := f.Merge(ctx, params); err != nil {
		return err
	}

	w.sync.ReadOften(host)

	if err := w.cascade.CascadeMerged(context.WithoutCancel(ctx), params.URL); err != nil {
		logging.From(ctx).LogAttrs(ctx, slog.LevelWarn, "failed to move the children of a merged pull request",
			append([]slog.Attr{slog.String(logging.PullRequestURL, params.URL)}, logging.Err(err)...)...)
	}

	return nil
}

// SetPullRequestDraft marks the pull request at the address a draft or ready for review and
// has the pull request sync read its host often.
func (w *Writer) SetPullRequestDraft(ctx context.Context, address string, draft bool) error {
	host, f, err := w.forges.ForAddress(ctx, address)
	if err != nil {
		return err
	}

	if err := f.SetDraft(ctx, address, draft); err != nil {
		return err
	}

	w.sync.ReadOften(host)

	return nil
}

// SetPullRequestAutoMerge asks the forge to merge the pull request once its checks and reviews
// pass, or stops it, and has the pull request sync read its host often.
func (w *Writer) SetPullRequestAutoMerge(ctx context.Context, params forge.SetAutoMergeParams) error {
	if params.On {
		if err := w.refuseBlockedMerge(ctx, params.URL); err != nil {
			return err
		}
	}

	host, f, err := w.forges.ForAddress(ctx, params.URL)
	if err != nil {
		return err
	}

	if err := f.SetAutoMerge(ctx, params); err != nil {
		return err
	}

	w.sync.ReadOften(host)

	return nil
}

// RequestPullRequestRereview asks everyone who requested changes on the pull request at the
// address to review it again and has the pull request sync read its host often.
func (w *Writer) RequestPullRequestRereview(ctx context.Context, address string) error {
	host, f, err := w.forges.ForAddress(ctx, address)
	if err != nil {
		return err
	}

	if err := f.RequestRereview(ctx, address); err != nil {
		return err
	}

	w.sync.ReadOften(host)

	return nil
}

// refuseBlockedMerge turns down a merge from a child, or onto a finished task's branch.
func (w *Writer) refuseBlockedMerge(ctx context.Context, address string) error {
	blockers, err := w.pullRequests.GetMergeBlockers(ctx, address)
	if errors.Is(err, store.ErrNotFound) {
		return nil
	}
	if err != nil {
		return err
	}

	switch {
	case blockers.HeadHasParent:
		return errMergeHasParent
	case blockers.BaseFinished:
		return errMergeOntoFinished
	}

	return nil
}

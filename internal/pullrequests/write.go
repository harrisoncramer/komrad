package pullrequests

import (
	"context"
	"errors"
	"net/url"

	"komrad/internal/fail"
	"komrad/internal/forge"
	"komrad/internal/store"
	pullrequeststore "komrad/internal/store/pullrequest"
	"komrad/internal/wake"
)

var (
	// errMergeStanding says the pull request comes from a task that stands on another, which merges only once it stands on the base branch.
	errMergeStanding = fail.Precondition("this task stands on another task, so it merges only once komrad has moved it onto the base branch")

	// errMergeOntoFinished says the pull request targets a task's branch whose own pull request has merged or closed.
	errMergeOntoFinished = fail.Precondition("this pull request targets a task's branch whose own pull request has merged or closed, so point it at the base branch first")
)

// NewWriterParams is what a writer is built over.
type NewWriterParams struct {
	Forges       *forge.Service
	Sync         wake.Heater
	PullRequests *pullrequeststore.PullRequestStore
}

// Writer changes pull requests through the connection for their host.
type Writer struct {
	forges       *forge.Service
	sync         wake.Heater
	pullRequests *pullrequeststore.PullRequestStore
}

// NewWriter builds a writer over the forge service and the pull request sync.
func NewWriter(params NewWriterParams) *Writer {
	return &Writer{
		forges:       params.Forges,
		sync:         params.Sync,
		pullRequests: params.PullRequests,
	}
}

// PlanPullRequestMerge is the ways the pull request at the address could merge.
func (w *Writer) PlanPullRequestMerge(ctx context.Context, address string) (*forge.MergePlan, error) {
	_, f, err := w.forgeFor(ctx, address)
	if err != nil {
		return nil, err
	}

	return f.PlanMerge(ctx, address)
}

// MergePullRequest merges the pull request and heats its host in the pull request sync.
func (w *Writer) MergePullRequest(ctx context.Context, params forge.MergeParams) error {
	if err := w.refuseStandMerge(ctx, params.URL); err != nil {
		return err
	}

	host, f, err := w.forgeFor(ctx, params.URL)
	if err != nil {
		return err
	}

	if err := f.Merge(ctx, params); err != nil {
		return err
	}

	w.sync.Heat(host)

	return nil
}

// SetPullRequestDraft marks the pull request at the address a draft or ready for review and
// heats its host in the pull request sync.
func (w *Writer) SetPullRequestDraft(ctx context.Context, address string, draft bool) error {
	host, f, err := w.forgeFor(ctx, address)
	if err != nil {
		return err
	}

	if err := f.SetDraft(ctx, address, draft); err != nil {
		return err
	}

	w.sync.Heat(host)

	return nil
}

// SetPullRequestAutoMerge asks the forge to merge the pull request once its checks and reviews
// pass, or stops it, and heats its host in the pull request sync.
func (w *Writer) SetPullRequestAutoMerge(ctx context.Context, params forge.SetAutoMergeParams) error {
	if params.On {
		if err := w.refuseStandMerge(ctx, params.URL); err != nil {
			return err
		}
	}

	host, f, err := w.forgeFor(ctx, params.URL)
	if err != nil {
		return err
	}

	if err := f.SetAutoMerge(ctx, params); err != nil {
		return err
	}

	w.sync.Heat(host)

	return nil
}

// RequestPullRequestRereview asks everyone who requested changes on the pull request at the
// address to review it again and heats its host in the pull request sync.
func (w *Writer) RequestPullRequestRereview(ctx context.Context, address string) error {
	host, f, err := w.forgeFor(ctx, address)
	if err != nil {
		return err
	}

	if err := f.RequestRereview(ctx, address); err != nil {
		return err
	}

	w.sync.Heat(host)

	return nil
}

// forgeFor is the host of the pull request address and the forge connected for it.
func (w *Writer) forgeFor(ctx context.Context, address string) (string, forge.Forge, error) {
	parsed, err := url.Parse(address)
	if err != nil || parsed.Hostname() == "" {
		return "", nil, forge.ErrNotPullRequest.Saying("%q is not a pull request's address", address)
	}

	host := parsed.Hostname()
	f, err := w.forges.ForHost(ctx, host)
	if err != nil {
		return "", nil, err
	}

	return host, f, nil
}

// refuseStandMerge turns down a merge from a task standing on another, or into a finished task's branch.
func (w *Writer) refuseStandMerge(ctx context.Context, address string) error {
	stand, err := w.pullRequests.GetMergeStand(ctx, address)
	if errors.Is(err, store.ErrNotFound) {
		return nil
	}
	if err != nil {
		return err
	}

	switch {
	case stand.HeadStands:
		return errMergeStanding
	case stand.BaseFinished:
		return errMergeOntoFinished
	}

	return nil
}

package pullrequests

import (
	"context"
	"net/url"

	"komrad/internal/forge"
	"komrad/internal/wake"
)

// NewWriterParams is what a writer is built over.
type NewWriterParams struct {
	Forges *forge.Service
	Sync   wake.Heater
}

// Writer changes pull requests through the connection for their host.
type Writer struct {
	forges *forge.Service
	sync   wake.Heater
}

// NewWriter builds a writer over the forge service and the pull request sync.
func NewWriter(params NewWriterParams) *Writer {
	return &Writer{
		forges: params.Forges,
		sync:   params.Sync,
	}
}

// MergePullRequest merges the pull request at the address and heats its host in the pull
// request sync.
func (w *Writer) MergePullRequest(ctx context.Context, address string) error {
	host, f, err := w.forgeFor(ctx, address)
	if err != nil {
		return err
	}

	if err := f.Merge(ctx, address); err != nil {
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

package pullrequests

import (
	"context"
	"fmt"
	"net/url"

	"komrad/internal/forge"
	forgestore "komrad/internal/store/forge"
)

// writeForgeFinder is what a write asks of the forge service.
type writeForgeFinder interface {
	ConnectionFor(ctx context.Context, host string) (*forgestore.Connection, error)
	ForConnection(connection forgestore.Connection) (forge.Forge, error)
}

// heater keeps a host's repositories in the pull request sync's fast lane for a while.
type heater interface {
	Heat(host string)
}

// NewWriterParams is what a writer is built over.
type NewWriterParams struct {
	Forges writeForgeFinder
	Sync   heater
}

// Writer changes pull requests through the connection for their host.
type Writer struct {
	forges writeForgeFinder
	sync   heater
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

// forgeFor is the host of the pull request address and the forge connected for it.
func (w *Writer) forgeFor(ctx context.Context, address string) (string, forge.Forge, error) {
	parsed, err := url.Parse(address)
	if err != nil || parsed.Hostname() == "" {
		return "", nil, forge.ErrNotPullRequest.Saying("%q is not a pull request's address", address)
	}

	host := parsed.Hostname()
	connection, err := w.forges.ConnectionFor(ctx, host)
	if err != nil {
		return "", nil, fmt.Errorf("failed to find the forge for %s: %w", host, err)
	}

	f, err := w.forges.ForConnection(*connection)
	if err != nil {
		return "", nil, fmt.Errorf("failed to build the forge for %s: %w", host, err)
	}

	return host, f, nil
}

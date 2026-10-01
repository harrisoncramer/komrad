package pullrequests

import (
	"context"
	"fmt"
	"net/url"
	"time"

	"komrad/internal/forge"
	forgestore "komrad/internal/store/forge"
)

// writeForgeFinder is what a write asks of the forge service.
type writeForgeFinder interface {
	ConnectionFor(ctx context.Context, host string) (*forgestore.Connection, error)
	ForConnection(connection forgestore.Connection) (forge.Forge, error)
}

// loopWaker asks a background loop to run before its next tick.
type loopWaker interface {
	Wake()
	WakeAfter(wait time.Duration)
}

const (
	writeSettleAfter  = 10 * time.Second
	writeSettleWithin = time.Minute
)

// NewWriterParams is what a writer is built over.
type NewWriterParams struct {
	Forges writeForgeFinder
	Sync   loopWaker
}

// Writer changes pull requests through the connection for their host.
type Writer struct {
	forges writeForgeFinder
	sync   loopWaker
}

// NewWriter builds a writer over the forge service and the waker of the pull request sync.
func NewWriter(params NewWriterParams) *Writer {
	return &Writer{
		forges: params.Forges,
		sync:   params.Sync,
	}
}

// MergePullRequest merges the pull request at the address and wakes the pull request sync.
func (w *Writer) MergePullRequest(ctx context.Context, address string) error {
	f, err := w.forgeFor(ctx, address)
	if err != nil {
		return err
	}

	if err := f.Merge(ctx, address); err != nil {
		return err
	}

	w.wakeUntilSettled()

	return nil
}

// SetPullRequestDraft marks the pull request at the address a draft or ready for review and
// wakes the pull request sync.
func (w *Writer) SetPullRequestDraft(ctx context.Context, address string, draft bool) error {
	f, err := w.forgeFor(ctx, address)
	if err != nil {
		return err
	}

	if err := f.SetDraft(ctx, address, draft); err != nil {
		return err
	}

	w.wakeUntilSettled()

	return nil
}

// SetPullRequestAutoMerge asks the forge to merge the pull request once its checks and reviews
// pass, or stops it, and wakes the pull request sync.
func (w *Writer) SetPullRequestAutoMerge(ctx context.Context, params forge.SetAutoMergeParams) error {
	f, err := w.forgeFor(ctx, params.URL)
	if err != nil {
		return err
	}

	if err := f.SetAutoMerge(ctx, params); err != nil {
		return err
	}

	w.wakeUntilSettled()

	return nil
}

// forgeFor is the forge connected for the pull request address's host.
func (w *Writer) forgeFor(ctx context.Context, address string) (forge.Forge, error) {
	parsed, err := url.Parse(address)
	if err != nil || parsed.Hostname() == "" {
		return nil, forge.ErrNotPullRequest.Saying("%q is not a pull request's address", address)
	}

	connection, err := w.forges.ConnectionFor(ctx, parsed.Hostname())
	if err != nil {
		return nil, fmt.Errorf("failed to find the forge for %s: %w", parsed.Hostname(), err)
	}

	f, err := w.forges.ForConnection(*connection)
	if err != nil {
		return nil, fmt.Errorf("failed to build the forge for %s: %w", parsed.Hostname(), err)
	}

	return f, nil
}

func (w *Writer) wakeUntilSettled() {
	w.sync.Wake()
	for wait := writeSettleAfter; wait < writeSettleWithin; wait += writeSettleAfter {
		w.sync.WakeAfter(wait)
	}
}

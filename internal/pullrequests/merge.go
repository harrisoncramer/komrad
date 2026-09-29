package pullrequests

import (
	"context"
	"fmt"
	"net/url"

	"komrad/internal/forge"
	forgestore "komrad/internal/store/forge"
)

// mergeForgeFinder is what a merge asks of the forge service.
type mergeForgeFinder interface {
	ConnectionFor(ctx context.Context, host string) (*forgestore.Connection, error)
	ForConnection(connection forgestore.Connection) (forge.Forge, error)
}

// loopWaker asks a background loop to run before its next tick.
type loopWaker interface {
	Wake()
}

// NewMergerParams is what a merger is built over.
type NewMergerParams struct {
	Forges mergeForgeFinder
	Sync   loopWaker
}

// Merger merges pull requests through the connection for their host.
type Merger struct {
	forges mergeForgeFinder
	sync   loopWaker
}

// NewMerger builds a merger over the forge service and the waker of the pull request sync.
func NewMerger(params NewMergerParams) *Merger {
	return &Merger{
		forges: params.Forges,
		sync:   params.Sync,
	}
}

// MergePullRequest merges the pull request at the address and wakes the pull request sync.
func (m *Merger) MergePullRequest(ctx context.Context, address string) error {
	parsed, err := url.Parse(address)
	if err != nil || parsed.Hostname() == "" {
		return forge.ErrNotPullRequest.Saying("%q is not a pull request's address", address)
	}

	connection, err := m.forges.ConnectionFor(ctx, parsed.Hostname())
	if err != nil {
		return fmt.Errorf("failed to find the forge for %s: %w", parsed.Hostname(), err)
	}

	f, err := m.forges.ForConnection(*connection)
	if err != nil {
		return fmt.Errorf("failed to build the forge for %s: %w", parsed.Hostname(), err)
	}

	if err := f.Merge(ctx, address); err != nil {
		return err
	}

	m.sync.Wake()

	return nil
}

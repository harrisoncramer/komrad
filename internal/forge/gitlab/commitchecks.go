package gitlab

import (
	"context"

	"komrad/internal/forge"
)

// ReadCommitChecks refuses, since komrad reads GitLab's checks only through a merge request's head
// pipeline.
func (f *Forge) ReadCommitChecks(context.Context, forge.ReadCommitChecksParams) (*forge.Reading, error) {
	return nil, forge.ErrUnsupported
}

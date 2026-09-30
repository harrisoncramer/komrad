package gitlab

import (
	"context"
	"errors"
	"fmt"

	"komrad/internal/forge"
	"komrad/internal/git"
)

// ReviewThreads refuses, since komrad reads no review comments from GitLab.
func (f *Forge) ReviewThreads(context.Context, git.RemoteURL, int) (*forge.Threads, error) {
	return nil, fmt.Errorf("%w: komrad does not read review comments from gitlab", errors.ErrUnsupported)
}

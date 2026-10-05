package github

import (
	"bytes"
	"context"
	"fmt"

	"komrad/internal/forge"
	"komrad/internal/git"
)

// maxJobLogBytes is the most of one job's log komrad keeps.
const maxJobLogBytes = 64 << 20

var _ forge.JobLogReader = (*Forge)(nil)

// JobLog reads the log of a finished Actions job, which GitHub hands over as a redirect to a signed
// address that must be fetched without the credential.
func (f *Forge) JobLog(ctx context.Context, repo git.RemoteURL, jobID int64) (*forge.JobLog, error) {
	owner, name, err := ownerAndName(repo.Path)
	if err != nil {
		return nil, err
	}

	signed, err := f.client.Redirect(ctx, fmt.Sprintf("%s/repos/%s/%s/actions/jobs/%d/logs", f.client.API(), owner, name, jobID))
	if err != nil {
		return nil, fmt.Errorf("failed to find the log of job %d: %w", jobID, err)
	}

	body, truncated, err := f.client.Download(ctx, signed, maxJobLogBytes)
	if err != nil {
		return nil, fmt.Errorf("failed to download the log of job %d: %w", jobID, err)
	}
	if truncated {
		body = body[:bytes.LastIndexByte(body, '\n')+1]
	}

	return &forge.JobLog{Text: string(body), Truncated: truncated}, nil
}

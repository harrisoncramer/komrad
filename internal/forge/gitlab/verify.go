package gitlab

import (
	"context"
	"fmt"

	"komrad/internal/forge"
)

const verifyQuery = `query Verify { currentUser { username } }`

// Verify asks GitLab who the credential belongs to.
func (f *Forge) Verify(ctx context.Context) (string, error) {
	var data struct {
		CurrentUser *struct {
			Username string `json:"username"`
		} `json:"currentUser"`
	}
	answer, err := f.client.Query(ctx, verifyQuery, nil, &data)
	if err != nil {
		return "", err
	}
	if err := failureOf(answer); err != nil {
		return "", err
	}
	if data.CurrentUser == nil || data.CurrentUser.Username == "" {
		return "", fmt.Errorf("%w: gitlab answered currentUser with nobody", forge.ErrUnauthorized)
	}

	return data.CurrentUser.Username, nil
}

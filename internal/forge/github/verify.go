package github

import (
	"context"
	_ "embed"
	"fmt"
	"net/http"

	"komrad/internal/forge"
	"komrad/lib/xhttp"
)

//go:embed verify.gql
var verifyQuery string

// Verify asks GitHub who the credential belongs to.
func (f *Forge) Verify(ctx context.Context) (string, error) {
	viewer, err := f.ReadViewer(ctx)
	if err != nil {
		return "", err
	}

	return viewer.Login, nil
}

// ReadViewer asks GitHub who the credential belongs to, with the name and email they commit as.
func (f *Forge) ReadViewer(ctx context.Context) (*forge.Viewer, error) {
	var data struct {
		Viewer struct {
			Login      string `json:"login"`
			Name       string `json:"name"`
			Email      string `json:"email"`
			DatabaseID int64  `json:"databaseId"`
		} `json:"viewer"`
	}
	answer, err := f.client.Query(ctx, verifyQuery, nil, &data)
	if err != nil {
		return nil, err
	}
	if err := failureOf(answer); err != nil {
		return nil, err
	}
	if data.Viewer.Login == "" {
		return nil, fmt.Errorf("github answered viewer with no login")
	}

	return &forge.Viewer{
		Login: data.Viewer.Login,
		Name:  data.Viewer.Name,
		Email: data.Viewer.Email,
		ID:    data.Viewer.DatabaseID,
	}, nil
}

// VerifyInstallation is how many repositories the installation the credential was minted for
// can reach, over REST, the one call GitHub has no GraphQL field for.
func (f *Forge) VerifyInstallation(ctx context.Context) (int, error) {
	var installation struct {
		TotalCount int `json:"total_count"`
	}
	answer, err := f.client.Send(ctx, xhttp.JSONParams{
		Method: http.MethodGet,
		URL:    f.client.API() + "/installation/repositories?per_page=1",
	}, &installation)
	if err != nil {
		return 0, err
	}
	if answer.Status == http.StatusNotFound {
		return 0, fmt.Errorf("%w: github answered 404 for /installation/repositories, which is also its answer for a repository the credential cannot see", forge.ErrNotFound)
	}

	return installation.TotalCount, nil
}

package controlplane

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"net/http"

	"komrad/internal/forge"
	"komrad/internal/logging"
)

// GitHubAPIURL is where GitHub's api lives.
const GitHubAPIURL = "https://api.github.com"

// GitHubPath answers whether the engine whose path it follows holds a GitHub login GitHub takes.
const GitHubPath = "/github"

// GitHubStatus is whether an engine's GitHub login works.
type GitHubStatus struct {
	Authorized bool `json:"authorized"`
}

func (h *handler) answerGitHub(w http.ResponseWriter, r *http.Request) {
	engine, ok := h.admit(w, r)
	if !ok {
		return
	}

	authorized, err := h.isGitHubAuthorized(r.Context(), engine)
	if err != nil {
		h.logger.LogAttrs(r.Context(), slog.LevelWarn, "failed to ask GitHub whether an engine's login works",
			append(logging.Err(err), slog.String(logging.EngineName, engine))...)
		writeMessage(w, http.StatusBadGateway, "the control plane failed to ask GitHub whether the engine's login works")

		return
	}

	writeJSON(w, http.StatusOK, GitHubStatus{Authorized: authorized})
}

// isGitHubAuthorized reports whether the engine holds a login GitHub still takes.
func (h *handler) isGitHubAuthorized(ctx context.Context, engine string) (bool, error) {
	login, err := h.logins.Get(ctx, engine)
	if errors.Is(err, forge.ErrNoCredential) {
		return false, nil
	}
	if err != nil {
		return false, fmt.Errorf("failed to read the engine's GitHub login: %w", err)
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, h.githubAPI+"/user", nil)
	if err != nil {
		return false, fmt.Errorf("failed to build the request for GitHub's /user: %w", err)
	}
	req.Header.Set("Authorization", "Bearer "+login.Expose())
	req.Header.Set("Accept", "application/vnd.github+json")

	resp, err := h.client.Do(req)
	if err != nil {
		return false, fmt.Errorf("failed to reach GitHub's /user: %w", err)
	}
	_ = resp.Body.Close() //nolint:errcheck // only the status is read

	switch resp.StatusCode {
	case http.StatusOK:
		return true, nil
	case http.StatusUnauthorized:
		return false, nil
	default:
		return false, fmt.Errorf("GitHub answered /user with %d", resp.StatusCode)
	}
}

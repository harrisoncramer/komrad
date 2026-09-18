// Package github is the forge behind github.com and GitHub Enterprise.
package github

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"time"

	"weave/internal/fail"
	"weave/internal/forge"
	"weave/internal/git"
	"weave/lib/xhttp"
)

// DefaultAPI is where github.com answers REST calls.
const DefaultAPI = "https://api.github.com"

// DefaultTimeout bounds one call to GitHub.
const DefaultTimeout = 30 * time.Second

// NewParams says how the forge reaches the API and as whom.
type NewParams struct {
	Auth forge.Authenticator
	// API is the REST base, DefaultAPI when unset.
	API string
	// Next is the round tripper under the credential transport, the default when nil.
	Next http.RoundTripper
	// Timeout bounds one call, DefaultTimeout when unset.
	Timeout time.Duration
}

// Forge is GitHub, spoken to over its REST API.
type Forge struct {
	api    string
	client *http.Client
}

// New builds the forge.
func New(params NewParams) *Forge {
	api := params.API
	if api == "" {
		api = DefaultAPI
	}

	timeout := params.Timeout
	if timeout == 0 {
		timeout = DefaultTimeout
	}

	return &Forge{
		api:    strings.TrimRight(api, "/"),
		client: xhttp.NewClientOver(forge.NewTransport(params.Auth, params.Next), timeout),
	}
}

// Kind is what the forge answers to.
func (f *Forge) Kind() string { return forge.KindGitHub }

// Verify asks GitHub who the credential belongs to.
func (f *Forge) Verify(ctx context.Context) (string, error) {
	var user struct {
		Login string `json:"login"`
	}
	if err := f.get(ctx, "/user", &user); err != nil {
		return "", err
	}
	if user.Login == "" {
		return "", fmt.Errorf("github answered /user with no login")
	}

	return user.Login, nil
}

// VerifyInstallation is how many repositories the installation the credential was minted for
// can reach.
func (f *Forge) VerifyInstallation(ctx context.Context) (int, error) {
	var installation struct {
		TotalCount int `json:"total_count"`
	}
	if err := f.get(ctx, "/installation/repositories?per_page=1", &installation); err != nil {
		return 0, err
	}

	return installation.TotalCount, nil
}

// ListOnBranch is every pull request opened from the named branch of the repository.
func (f *Forge) ListOnBranch(ctx context.Context, repo git.RemoteURL, branch string) ([]forge.PullRequest, error) {
	owner, name, err := ownerAndName(repo.Path)
	if err != nil {
		return nil, err
	}

	query := url.Values{
		"state":    {"all"},
		"head":     {owner + ":" + branch},
		"per_page": {"100"},
	}
	var raws []pull
	if err := f.get(ctx, fmt.Sprintf("/repos/%s/%s/pulls?%s", owner, name, query.Encode()), &raws); err != nil {
		return nil, err
	}

	pulls := make([]forge.PullRequest, 0, len(raws))
	for _, raw := range raws {
		pulls = append(pulls, raw.toPullRequest())
	}

	return pulls, nil
}

// Get is one pull request by its web address.
func (f *Forge) Get(ctx context.Context, address string) (forge.PullRequest, error) {
	owner, name, number, err := parsePullURL(address)
	if err != nil {
		return forge.PullRequest{}, err
	}

	var raw pull
	if err := f.get(ctx, fmt.Sprintf("/repos/%s/%s/pulls/%d", owner, name, number), &raw); err != nil {
		return forge.PullRequest{}, err
	}

	return raw.toPullRequest(), nil
}

func (f *Forge) get(ctx context.Context, path string, into any) error {
	answer, err := xhttp.JSON(ctx, xhttp.JSONParams{
		Client: f.client,
		Method: http.MethodGet,
		URL:    f.api + path,
		Header: http.Header{
			"Accept":               {"application/vnd.github+json"},
			"X-GitHub-Api-Version": {"2022-11-28"},
		},
	}, into)
	if err != nil {
		if failure := fail.Of(err); failure != nil {
			return failure
		}

		return err
	}

	switch {
	case answer.Status == http.StatusUnauthorized:
		return fmt.Errorf("%w: %s", forge.ErrUnauthorized, messageOf(answer.Body))
	case answer.Status == http.StatusNotFound:
		return fmt.Errorf("%w: github answered 404 for %s, which is also its answer for a repository the credential cannot see", forge.ErrNotFound, path)
	case !answer.IsSuccess():
		return fmt.Errorf("github answered %d for %s: %s", answer.Status, path, messageOf(answer.Body))
	}

	return nil
}

// messageOf is what GitHub said went wrong, which its error bodies carry as message.
func messageOf(body []byte) string {
	var failure struct {
		Message string `json:"message"`
	}
	if err := json.Unmarshal(body, &failure); err == nil && failure.Message != "" {
		return failure.Message
	}

	return strings.TrimSpace(string(body))
}

// ownerAndName splits a repository path, which on GitHub is always two segments.
func ownerAndName(path string) (owner, name string, err error) {
	owner, name, ok := strings.Cut(path, "/")
	if !ok || owner == "" || name == "" || strings.Contains(name, "/") {
		return "", "", fmt.Errorf("%w: %q is not owner/name", forge.ErrNotRepository, path)
	}

	return owner, name, nil
}

// parsePullURL reads the owner, name and number out of https://host/owner/name/pull/N.
func parsePullURL(address string) (owner, name string, number int, err error) {
	parsed, err := url.Parse(address)
	if err != nil || parsed.Hostname() == "" {
		return "", "", 0, fmt.Errorf("%w: %q is not a github pull request", forge.ErrNotPullRequest, address)
	}

	segments := strings.Split(strings.Trim(parsed.Path, "/"), "/")
	if len(segments) < 4 || segments[2] != "pull" {
		return "", "", 0, fmt.Errorf("%w: %q is not a github pull request", forge.ErrNotPullRequest, address)
	}

	number, err = strconv.Atoi(segments[3])
	if err != nil || number <= 0 {
		return "", "", 0, fmt.Errorf("%w: %q is not a github pull request", forge.ErrNotPullRequest, address)
	}

	return segments[0], segments[1], number, nil
}

// pull is the slice of GitHub's pull request payload the record is built from.
type pull struct {
	NodeID    string     `json:"node_id"`
	HTMLURL   string     `json:"html_url"`
	Number    int        `json:"number"`
	Title     string     `json:"title"`
	State     string     `json:"state"`
	Draft     bool       `json:"draft"`
	CreatedAt time.Time  `json:"created_at"`
	UpdatedAt time.Time  `json:"updated_at"`
	MergedAt  *time.Time `json:"merged_at"`
	ClosedAt  *time.Time `json:"closed_at"`
	Head      ref        `json:"head"`
	Base      ref        `json:"base"`
}

type ref struct {
	Ref  string `json:"ref"`
	SHA  string `json:"sha"`
	Repo *repo  `json:"repo"`
}

type repo struct {
	HTMLURL string `json:"html_url"`
}

func (p pull) toPullRequest() forge.PullRequest {
	return forge.PullRequest{
		Forge:      forge.KindGitHub,
		ProviderID: p.NodeID,
		URL:        p.HTMLURL,
		Repository: remoteOf(p.Base.Repo),
		Head:       remoteOf(p.Head.Repo),
		Number:     p.Number,
		Title:      p.Title,
		HeadBranch: p.Head.Ref,
		HeadSHA:    p.Head.SHA,
		BaseBranch: p.Base.Ref,
		BaseSHA:    p.Base.SHA,
		Draft:      p.Draft,
		State:      p.State,
		CreatedAt:  p.CreatedAt,
		UpdatedAt:  p.UpdatedAt,
		MergedAt:   p.MergedAt,
		ClosedAt:   p.ClosedAt,
	}
}

// remoteOf is the repository a payload names, zero for one GitHub no longer has.
func remoteOf(r *repo) git.RemoteURL {
	if r == nil {
		return git.RemoteURL{}
	}

	remote, err := git.ParseRemoteURL(r.HTMLURL)
	if err != nil {
		return git.RemoteURL{}
	}

	return remote
}

// Build is the Builder for GitHub.
func Build(api string, auth forge.Authenticator) forge.Forge {
	return New(NewParams{Auth: auth, API: api})
}

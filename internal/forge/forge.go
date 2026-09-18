package forge

import (
	"context"
	"fmt"
	"net/http"
	"time"

	"weave/internal/fail"
	"weave/internal/git"
	"weave/lib/xhttp"
)

// ErrNotFound says the forge has nothing at the address asked for, which for a repository the
// credential cannot see is what it answers too.
var ErrNotFound = fail.NotFound("the forge has nothing there")

// ErrNotRepository says a repository path is not one the forge can address.
var ErrNotRepository = fail.InvalidArgument("not a repository on this forge")

// ErrNotPullRequest says an address does not name a pull request on the forge.
var ErrNotPullRequest = fail.InvalidArgument("not a pull request on this forge")

// ErrUnauthorized says the forge refused the credential.
var ErrUnauthorized = fail.Denied("the forge refused the credential")

// KindGitHub is the kind a GitHub host records.
const KindGitHub = "github"

// KindGitLab is the kind a GitLab host records.
const KindGitLab = "gitlab"

// MethodToken is the method for a credential a person supplied.
const MethodToken = "token"

// PullRequest is one pull request as every forge describes it.
type PullRequest struct {
	// Forge is the kind of forge the pull request lives on, such as github.
	Forge string
	// ProviderID is the forge's own stable identifier, such as GitHub's node_id.
	ProviderID string
	// URL is the web address a person opens.
	URL string
	// Repository is the repository the pull request targets.
	Repository git.RemoteURL
	// Head is the repository the branch lives in, zero when the forge no longer has it.
	Head       git.RemoteURL
	Number     int
	Title      string
	HeadBranch string
	HeadSHA    string
	BaseBranch string
	BaseSHA    string
	Draft      bool
	// State is the forge's own word for where the pull request stands, kept verbatim.
	State     string
	CreatedAt time.Time
	UpdatedAt time.Time
	MergedAt  *time.Time
	ClosedAt  *time.Time
}

// IsFinished says whether the pull request has merged or closed.
func (pr PullRequest) IsFinished() bool {
	return pr.MergedAt != nil || pr.ClosedAt != nil
}

// IsCrossRepository says whether the branch lives somewhere other than the repository it targets.
func (pr PullRequest) IsCrossRepository() bool {
	return pr.Head.IsZero() || pr.Head.Canonical() != pr.Repository.Canonical()
}

// Forge is one service pull requests live on, spoken to as one connection.
type Forge interface {
	Kind() string
	// Verify asks the forge who the credential belongs to.
	Verify(ctx context.Context) (login string, err error)
	// ListOnBranch is every pull request whose branch is the named one in the repository.
	ListOnBranch(ctx context.Context, repo git.RemoteURL, branch string) ([]PullRequest, error)
	// Get is one pull request by its web address.
	Get(ctx context.Context, url string) (PullRequest, error)
}

// InstallationVerifier is a forge that can say what an installation token reaches.
type InstallationVerifier interface {
	// VerifyInstallation is how many repositories the credential's installation covers.
	VerifyInstallation(ctx context.Context) (int, error)
}

// Transport sends every request with the connection's credential and tells the
// Authenticator when the forge refuses it.
type Transport struct {
	auth Authenticator
	next http.RoundTripper
}

// NewTransport wraps a round tripper, xhttp's when nil.
func NewTransport(auth Authenticator, next http.RoundTripper) *Transport {
	if next == nil {
		next = xhttp.NewTransport()
	}

	return &Transport{auth: auth, next: next}
}

// RoundTrip sets the credential header and reports a 401 back to the Authenticator.
func (t *Transport) RoundTrip(req *http.Request) (*http.Response, error) {
	credential, err := t.auth.Credential(req.Context())
	if err != nil {
		return nil, err
	}

	cloned := req.Clone(req.Context())
	cloned.Header.Set(credential.Header, credential.HeaderValue())

	resp, err := t.next.RoundTrip(cloned)
	if err != nil {
		return nil, err
	}
	if resp.StatusCode == http.StatusUnauthorized {
		if err := t.auth.Invalidate(req.Context()); err != nil {
			resp.Body.Close()

			return nil, fmt.Errorf("failed to record the refusal: %w", err)
		}
	}

	return resp, nil
}

// Package gitlab is the forge behind gitlab.com and self-hosted GitLab, where a pull request is
// a merge request.
package gitlab

import (
	"net/http"
	"time"

	"komrad/internal/forge"
	"komrad/internal/forge/graphql"
	forgev1 "komrad/proto/gen/forge/v1"
)

// defaultAPI is where gitlab.com answers REST calls, with GraphQL beside it.
const defaultAPI = "https://gitlab.com/api/v4"

// dialect is what GitLab does differently on the wire.
var dialect = graphql.Dialect{
	Name:            "gitlab",
	RateLimitHeader: "RateLimit",
	Version:         "/v4",
}

// NewForgeParams says how the forge reaches the API and as whom.
type NewForgeParams struct {
	Auth forge.Authenticator
	// API is the REST base, https://gitlab.com/api/v4 when unset, beside which GraphQL is answered.
	API string
	// Next is the round tripper under the credential transport, the default when nil.
	Next http.RoundTripper
	// Timeout bounds one call, graphql.DefaultTimeout when unset.
	Timeout time.Duration
	// Now is the clock a Retry-After is counted from, time.Now when unset.
	Now func() time.Time
}

// Forge is GitLab, spoken to over its GraphQL API.
type Forge struct {
	client *graphql.Client
}

// NewForge builds the forge.
func NewForge(params NewForgeParams) *Forge {
	api := params.API
	if api == "" {
		api = defaultAPI
	}

	return &Forge{client: graphql.NewClient(graphql.NewClientParams{
		Dialect: dialect,
		Auth:    params.Auth,
		API:     api,
		Next:    params.Next,
		Timeout: params.Timeout,
		Now:     params.Now,
	})}
}

// Kind is what the forge answers to.
func (f *Forge) Kind() forgev1.Kind {
	return forgev1.Kind_KIND_GITLAB
}

// Build is the Builder for GitLab.
func Build(api string, auth forge.Authenticator) forge.Forge {
	return NewForge(NewForgeParams{Auth: auth, API: api})
}

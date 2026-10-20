// Package github is the forge behind github.com and GitHub Enterprise.
package github

import (
	"net/http"
	"time"

	"komrad/internal/forge"
	"komrad/internal/forge/graphql"
)

// defaultAPI is where github.com answers REST calls, with GraphQL beside it.
const defaultAPI = "https://api.github.com"

// dialect is what GitHub does differently on the wire.
var dialect = graphql.Dialect{
	Name: "github",
	Header: http.Header{
		"Accept":               {"application/vnd.github+json"},
		"X-Github-Api-Version": {"2022-11-28"},
	},
	RateLimitHeader: "X-RateLimit",
	Version:         "/v3",
}

// NewForgeParams says how the forge reaches the API and as whom.
type NewForgeParams struct {
	Auth forge.Authenticator
	// API is the REST base, https://api.github.com when unset, beside which GraphQL is answered.
	API string
	// Next is the round tripper under the credential transport, the default when nil.
	Next http.RoundTripper
	// Timeout bounds one call, graphql.DefaultTimeout when unset.
	Timeout time.Duration
	// Now is the clock a Retry-After is counted from, time.Now when unset.
	Now func() time.Time
	// Stacks says the host has GitHub stacks, which github.com has and GitHub Enterprise lacks.
	Stacks bool
}

// Forge is GitHub, spoken to over its GraphQL API.
type Forge struct {
	client *graphql.Client
	// stacks says the host has GitHub stacks.
	stacks bool
	// mergePoll is how long a merge waits between reads of how it stands.
	mergePoll time.Duration
}

// NewForge builds the forge.
func NewForge(params NewForgeParams) *Forge {
	api := params.API
	if api == "" {
		api = defaultAPI
	}

	return &Forge{
		client: graphql.NewClient(graphql.NewClientParams{
			Dialect: dialect,
			Auth:    params.Auth,
			API:     api,
			Next:    params.Next,
			Timeout: params.Timeout,
			Now:     params.Now,
		}),
		stacks:    params.Stacks,
		mergePoll: mergePoll,
	}
}

// Kind is what the forge answers to.
func (f *Forge) Kind() string {
	return forge.KindGitHub
}

// Build is the Builder for GitHub.
func Build(api string, auth forge.Authenticator) forge.Forge {
	return NewForge(NewForgeParams{Auth: auth, API: api, Stacks: api == "" || api == defaultAPI})
}

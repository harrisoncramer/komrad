// Package graphql is the client every forge speaks its GraphQL API through.
package graphql

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"time"

	"komrad/internal/fail"
	"komrad/internal/forge"
	"komrad/internal/git"
	"komrad/lib/xhttp"
	"komrad/lib/xptr"
	"komrad/lib/xslice"
)

// defaultTimeout bounds one call to a forge.
const defaultTimeout = 30 * time.Second

// downloadTimeout bounds one download of a file the forge redirected to, which can run to tens of
// megabytes.
const downloadTimeout = 5 * time.Minute

// Dialect is what one forge does differently from the others on the wire.
type Dialect struct {
	// Name is the forge as its error messages call it, such as github.
	Name string
	// Header is what every call carries besides the credential.
	Header http.Header
	// RateLimitHeader is what the forge's rate limit headers start with, such as X-RateLimit.
	RateLimitHeader string
	// Version is the REST base's last segment, which GraphQL is answered beside, such as /v3.
	Version string
}

// NewClientParams says how the client reaches the API and as whom.
type NewClientParams struct {
	Dialect Dialect
	Auth    forge.Authenticator
	// API is the REST base, beside which GraphQL is answered.
	API string
	// Next is the round tripper under the credential transport, the default when nil.
	Next http.RoundTripper
	// Timeout bounds one call, DefaultTimeout when unset.
	Timeout time.Duration
	// Now is the clock a Retry-After is counted from, time.Now when unset.
	Now func() time.Time
}

// Client speaks to one forge's API as one credential.
type Client struct {
	dialect Dialect
	api     string
	http    *http.Client
	// redirects is http that answers with a redirect rather than following it.
	redirects *http.Client
	// downloads fetches what a redirect names without the credential.
	downloads *http.Client
	now       func() time.Time
}

// NewClient builds the client.
func NewClient(params NewClientParams) *Client {
	timeout := params.Timeout
	if timeout == 0 {
		timeout = defaultTimeout
	}

	now := params.Now
	if now == nil {
		now = time.Now
	}

	next := params.Next
	if next == nil {
		next = xhttp.NewTransport()
	}

	credentialed := forge.NewTransport(params.Auth, next)
	redirects := xhttp.NewClientOver(credentialed, timeout)
	redirects.CheckRedirect = func(*http.Request, []*http.Request) error {
		return http.ErrUseLastResponse
	}

	return &Client{
		dialect:   params.Dialect,
		api:       strings.TrimRight(params.API, "/"),
		http:      xhttp.NewClientOver(credentialed, timeout),
		redirects: redirects,
		downloads: xhttp.NewClientOver(next, downloadTimeout),
		now:       now,
	}
}

// API is the REST base.
func (c *Client) API() string {
	return c.api
}

// URL is where the host answers GraphQL.
func (c *Client) URL() string {
	return strings.TrimSuffix(c.api, c.dialect.Version) + "/graphql"
}

// Error is one entry of the errors a GraphQL answer carries beside its data.
type Error struct {
	Type    string `json:"type"`
	Message string `json:"message"`
	Path    []any  `json:"path"`
}

// Answer is what one query came back with besides its data.
type Answer struct {
	Errors    []Error
	RateLimit forge.RateLimit
}

// Query sends one named operation to the GraphQL endpoint, decodes its data, and turns the
// forge's refusals into komrad's.
func (c *Client) Query(ctx context.Context, operation string, variables map[string]any, into any) (*Answer, error) {
	var envelope struct {
		Data   json.RawMessage `json:"data"`
		Errors []Error         `json:"errors"`
	}
	answer, err := c.Send(ctx, xhttp.JSONParams{
		Method: http.MethodPost,
		URL:    c.URL(),
		Body:   map[string]any{"query": operation, "variables": variables},
	}, &envelope)
	if err != nil {
		return nil, err
	}
	if answer.Status == http.StatusNotFound {
		return nil, fmt.Errorf("%s answered 404 at %s, which is not where its GraphQL lives", c.dialect.Name, c.URL())
	}

	limit := c.readRateLimit(answer.Header)
	for _, failure := range envelope.Errors {
		if failure.Type == "RATE_LIMITED" {
			return nil, &forge.ThrottledError{RetryAt: limit.ResetAt}
		}
	}

	if len(envelope.Data) > 0 && string(envelope.Data) != "null" {
		if err := json.Unmarshal(envelope.Data, into); err != nil {
			return nil, fmt.Errorf("failed to decode %s's answer to %s: %w", c.dialect.Name, OperationOf(operation), err)
		}
	}

	return &Answer{Errors: envelope.Errors, RateLimit: limit}, nil
}

// Send makes one call with the headers every call carries and turns the forge's refusals into
// komrad's, a 404 aside.
func (c *Client) Send(ctx context.Context, params xhttp.JSONParams, into any) (xhttp.Answer, error) {
	params.Client = c.http
	answer, err := c.send(ctx, params, into)
	if err != nil {
		return answer, err
	}

	if answer.Status == http.StatusNotFound {
		return answer, nil
	}
	if !answer.IsSuccess() {
		return answer, fmt.Errorf("%s answered %d for %s: %s", c.dialect.Name, answer.Status, pathOf(params.URL), MessageOf(answer.Body))
	}

	return answer, nil
}

// redirection is where the forge redirected a call, and the credential's budget after it.
type redirection struct {
	Location  string
	RateLimit forge.RateLimit
}

// Redirect is where the forge redirects a GET of address to without following it, ErrNotFound on a
// 404 and ErrExpired on a 410.
func (c *Client) Redirect(ctx context.Context, address string) (*redirection, error) {
	answer, err := c.send(ctx, xhttp.JSONParams{
		Client: c.redirects,
		Method: http.MethodGet,
		URL:    address,
	}, nil)
	if err != nil {
		return nil, err
	}

	path := pathOf(address)
	switch {
	case answer.Status == http.StatusNotFound:
		return nil, fmt.Errorf("%w: %s answered 404 for %s", forge.ErrNotFound, c.dialect.Name, path)
	case answer.Status == http.StatusGone:
		return nil, fmt.Errorf("%w: %s answered 410 for %s", forge.ErrExpired, c.dialect.Name, path)
	case answer.Status < 300 || answer.Status >= 400:
		return nil, fmt.Errorf("%s answered %d rather than a redirect for %s: %s", c.dialect.Name, answer.Status, path, MessageOf(answer.Body))
	}

	location, err := url.Parse(answer.Header.Get("Location"))
	if err != nil || location.String() == "" {
		return nil, fmt.Errorf("%s redirected %s to no address it could be read at", c.dialect.Name, path)
	}
	base, err := url.Parse(address)
	if err != nil {
		return nil, fmt.Errorf("failed to read the address %s: %w", path, err)
	}

	return &redirection{Location: base.ResolveReference(location).String(), RateLimit: c.readRateLimit(answer.Header)}, nil
}

// Download is the body of a GET of address made without the credential and read up to maxBytes,
// with whether the body ran past them.
func (c *Client) Download(ctx context.Context, address string, maxBytes int64) ([]byte, bool, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, address, nil)
	if err != nil {
		return nil, false, fmt.Errorf("failed to build the download of %s: %w", pathOf(address), err)
	}

	resp, err := c.downloads.Do(req)
	if err != nil {
		if failure := fail.Of(err); failure != nil {
			return nil, false, failure
		}

		return nil, false, fmt.Errorf("failed to download %s: %w", pathOf(address), err)
	}
	defer func() { _ = resp.Body.Close() }()

	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return nil, false, fmt.Errorf("download of %s answered %d", pathOf(address), resp.StatusCode)
	}

	body, err := io.ReadAll(io.LimitReader(resp.Body, maxBytes+1))
	if err != nil {
		return nil, false, fmt.Errorf("failed to read the download of %s: %w", pathOf(address), err)
	}
	if int64(len(body)) > maxBytes {
		return body[:maxBytes], true, nil
	}

	return body, false, nil
}

// send makes one call with the headers every call carries and turns a refusal for the credential
// or the rate limit into komrad's, leaving every other status to the caller.
func (c *Client) send(ctx context.Context, params xhttp.JSONParams, into any) (xhttp.Answer, error) {
	if params.Header == nil {
		params.Header = http.Header{}
	}
	for name, values := range c.dialect.Header {
		params.Header[http.CanonicalHeaderKey(name)] = values
	}

	answer, err := xhttp.JSON(ctx, params, into)
	if err != nil {
		if failure := fail.Of(err); failure != nil {
			return xhttp.Answer{}, failure
		}

		return xhttp.Answer{}, err
	}

	switch answer.Status {
	case http.StatusUnauthorized:
		return answer, fmt.Errorf("%w: %s", forge.ErrUnauthorized, MessageOf(answer.Body))
	case http.StatusForbidden, http.StatusTooManyRequests:
		if throttled := c.throttleOf(answer); throttled != nil {
			return answer, throttled
		}
	}

	return answer, nil
}

// readRateLimit is what the forge's headers said about the credential's budget.
func (c *Client) readRateLimit(header http.Header) forge.RateLimit {
	remaining, err := strconv.Atoi(header.Get(c.dialect.RateLimitHeader + "-Remaining"))
	if err != nil {
		return forge.RateLimit{}
	}

	limit := forge.RateLimit{Known: true, Remaining: remaining}
	if budget, err := strconv.Atoi(header.Get(c.dialect.RateLimitHeader + "-Limit")); err == nil {
		limit.Limit = budget
	}
	if reset, err := strconv.ParseInt(header.Get(c.dialect.RateLimitHeader+"-Reset"), 10, 64); err == nil {
		limit.ResetAt = time.Unix(reset, 0)
	}

	return limit
}

// throttleOf is the refusal a 403 or 429 is when it is the forge's rate limit rather than a
// permission answer, nil otherwise.
func (c *Client) throttleOf(answer xhttp.Answer) *forge.ThrottledError {
	retryAfter, hasRetryAfter := readRetryAfter(answer.Header)
	limit := c.readRateLimit(answer.Header)
	throttled := answer.Status == http.StatusTooManyRequests ||
		hasRetryAfter ||
		limit.IsExhausted() ||
		isSecondaryLimitMessage(MessageOf(answer.Body))
	if !throttled {
		return nil
	}

	switch {
	case hasRetryAfter:
		return &forge.ThrottledError{RetryAt: c.now().Add(retryAfter)}
	case limit.IsExhausted() && !limit.ResetAt.IsZero():
		return &forge.ThrottledError{RetryAt: limit.ResetAt}
	default:
		return &forge.ThrottledError{}
	}
}

func readRetryAfter(header http.Header) (time.Duration, bool) {
	seconds, err := strconv.Atoi(header.Get("Retry-After"))
	if err != nil || seconds < 0 {
		return 0, false
	}

	return time.Duration(seconds) * time.Second, true
}

// isSecondaryLimitMessage recognises the body GitHub sends for its secondary limit, which can
// come with neither rate limit header.
func isSecondaryLimitMessage(message string) bool {
	lowered := strings.ToLower(message)

	return strings.Contains(lowered, "secondary rate limit") || strings.Contains(lowered, "abuse detection")
}

// MessageOf is what the forge said went wrong, which its error bodies carry as message or error.
func MessageOf(body []byte) string {
	var failure struct {
		Message json.RawMessage `json:"message"`
		Error   string          `json:"error"`
	}
	if err := json.Unmarshal(body, &failure); err == nil {
		if failure.Error != "" {
			return failure.Error
		}

		var message string
		if err := json.Unmarshal(failure.Message, &message); err == nil && message != "" {
			return message
		}
		if len(failure.Message) > 0 {
			return strings.Trim(string(failure.Message), `"`)
		}
	}

	return strings.TrimSpace(string(body))
}

// pathOf is the path of an address, or the address itself when it cannot be read.
func pathOf(address string) string {
	parsed, err := url.Parse(address)
	if err != nil {
		return address
	}

	return parsed.RequestURI()
}

// OperationOf is the name a query or mutation was written with.
func OperationOf(operation string) string {
	name, _, _ := strings.Cut(strings.TrimPrefix(strings.TrimPrefix(operation, "query "), "mutation "), "(")
	name, _, _ = strings.Cut(name, " ")

	return name
}

// CursorOf is a cursor as the query variable takes it, null for the first page.
func CursorOf(cursor string) *string {
	if cursor == "" {
		return nil
	}

	return xptr.Ptr(cursor)
}

// RemoteOf is the repository a web address names, zero for one the forge no longer has.
func RemoteOf(address string) git.RemoteURL {
	if address == "" {
		return git.RemoteURL{}
	}

	remote, err := git.ParseRemoteURL(address)
	if err != nil {
		return git.RemoteURL{}
	}

	return remote
}

// Connection is one page of a connection as a forge answers it.
type Connection[N any] struct {
	PageInfo struct {
		HasNextPage bool   `json:"hasNextPage"`
		EndCursor   string `json:"endCursor"`
	} `json:"pageInfo"`
	Nodes []N `json:"nodes"`
}

// PageOf is a connection as one page of pull requests, each node read by toPullRequest.
func PageOf[N any](c *Connection[N], limit forge.RateLimit, toPullRequest func(N) forge.PullRequest) *forge.Page {
	pullRequests := xslice.Map(c.Nodes, toPullRequest)

	return &forge.Page{
		PullRequests: pullRequests,
		HasMore:      c.PageInfo.HasNextPage,
		Cursor:       c.PageInfo.EndCursor,
		RateLimit:    limit,
	}
}

// Nodes is the nodes one alias of a batched query answered.
type Nodes[N any] struct {
	Nodes []N `json:"nodes"`
}

// OwnPullRequestsOf is the pull requests under aliases b0 to b(count-1) whose branch lives in the repository they target.
func OwnPullRequestsOf[N any](aliased map[string]*Nodes[N], count int, toPullRequest func(N) forge.PullRequest) []forge.PullRequest {
	var pullRequests []forge.PullRequest
	for i := range count {
		answered := aliased["b"+strconv.Itoa(i)]
		if answered == nil {
			continue
		}

		for _, node := range answered.Nodes {
			pullRequest := toPullRequest(node)
			if !pullRequest.IsFromItsOwnRepository() {
				continue
			}

			pullRequests = append(pullRequests, pullRequest)
		}
	}

	return pullRequests
}

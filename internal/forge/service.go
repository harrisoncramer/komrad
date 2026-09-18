package forge

import (
	"context"
	"errors"
	"fmt"
	"net/url"
	"strings"
	"time"

	"weave/internal/fail"
	"weave/internal/store"
	forgestore "weave/internal/store/forge"
)

// ErrUnknownKind says no forge of that kind is compiled in.
var ErrUnknownKind = fail.InvalidArgument("no forge of that kind")

// ErrInsecureAPI says an API address would send the credential without TLS.
var ErrInsecureAPI = fail.InvalidArgument("the API address has to be https")

// ErrNoApp says the App method was asked for on a build with no App configured.
var ErrNoApp = fail.Precondition("no github app is configured")

// ErrAppReachesNothing says the App is installed on no repository.
var ErrAppReachesNothing = fail.Precondition("the github app is installed on no repository")

// ErrKindUnknownForHost says a host names no public forge, so the kind has to be given.
var ErrKindUnknownForHost = fail.InvalidArgument("the host does not say which forge it runs")

// Builder makes the forge for one kind, reaching the API at api as auth.
type Builder func(api string, auth Authenticator) Forge

// ConnectParams is what a person supplies to connect a host.
type ConnectParams struct {
	// Host is what agents' remotes name.
	Host string
	// Kind is which forge it runs, worked out from the host when empty.
	Kind string
	// APIURL is where an enterprise host answers, empty for the public forge.
	APIURL string
	// Method is how the credential comes, MethodToken when empty.
	Method string
	// Token is the credential for MethodToken, and ignored for MethodGitHubApp.
	Token Secret
}

// NewServiceParams says what the service writes to and which forges it can build.
type NewServiceParams struct {
	Connections *forgestore.Store
	Credentials CredentialStore
	Builders    map[string]Builder
	// Verified is what a refusal is recorded on, the connections themselves when unset and
	// NoRecord for a caller that only reports.
	Verified verifiedSetter
	// App is the GitHub App this machine can connect as, unconfigured when it has none.
	App App
	// Now is the clock, time.Now when unset.
	Now func() time.Time
}

// Service connects and disconnects forges, and builds one to speak to for a connection.
type Service struct {
	connections *forgestore.Store
	credentials CredentialStore
	builders    map[string]Builder
	verified    verifiedSetter
	app         App
	now         func() time.Time
}

// NewService builds the service.
func NewService(params NewServiceParams) *Service {
	now := params.Now
	if now == nil {
		now = time.Now
	}
	verified := params.Verified
	if verified == nil {
		verified = params.Connections
	}

	return &Service{
		connections: params.Connections,
		credentials: params.Credentials,
		builders:    params.Builders,
		verified:    verified,
		app:         params.App,
		now:         now,
	}
}

// KindForHost is the forge a public host runs, or ErrKindUnknownForHost.
func KindForHost(host string) (string, error) {
	switch strings.ToLower(host) {
	case "github.com":
		return KindGitHub, nil
	case "gitlab.com":
		return KindGitLab, nil
	default:
		return "", ErrKindUnknownForHost.Saying("%s is not a public forge, so say which kind it runs", host)
	}
}

// APIURLFor is where a host's API answers: the given address checked for https, or for a host
// that is not the public forge, the place that forge's self-hosted builds put it.
func APIURLFor(kind, host, given string) (string, error) {
	given = strings.TrimRight(strings.TrimSpace(given), "/")
	if given != "" {
		parsed, err := url.Parse(given)
		if err != nil || parsed.Scheme != "https" || parsed.Host == "" {
			return "", ErrInsecureAPI.Saying("%q is not an https address", given)
		}

		return given, nil
	}

	if _, err := KindForHost(host); err == nil {
		return "", nil
	}

	switch kind {
	case KindGitHub:
		return "https://" + host + "/api/v3", nil
	case KindGitLab:
		return "https://" + host + "/api/v4", nil
	default:
		return "", ErrUnknownKind.Saying("%q is not a forge this build knows", kind)
	}
}

// HeaderFor is the header a credential obtained one way is sent in on one kind of forge.
func HeaderFor(kind, method string) string {
	if kind == KindGitLab && method == MethodToken {
		return HeaderPrivateToken
	}

	return HeaderBearer
}

// Connect proves a credential against the forge and records the connection, refusing a host
// that is already connected and a credential the forge does not accept.
func (s *Service) Connect(ctx context.Context, params ConnectParams) (*forgestore.Connection, error) {
	host := strings.ToLower(strings.TrimSpace(params.Host))
	if host == "" {
		return nil, fail.InvalidArgument("a host is needed")
	}

	kind := params.Kind
	if kind == "" {
		found, err := KindForHost(host)
		if err != nil {
			return nil, err
		}

		kind = found
	}

	build, ok := s.builders[kind]
	if !ok {
		return nil, ErrUnknownKind.Saying("%q is not a forge this build knows", kind)
	}

	method := params.Method
	if method == "" {
		method = MethodToken
	}

	apiURL, err := APIURLFor(kind, host, params.APIURL)
	if err != nil {
		return nil, err
	}

	if _, err := s.connections.GetByHost(ctx, host); err == nil {
		return nil, forgestore.ErrHostTaken.Saying("%s is already connected; disconnect it first", host)
	} else if !errors.Is(err, store.ErrNotFound) {
		return nil, err
	}

	var login string
	switch method {
	case MethodToken:
		login, err = s.proveToken(ctx, build, kind, host, apiURL, params.Token)
	case MethodGitHubApp:
		login, err = s.proveApp(ctx, build, kind, apiURL)
	default:
		return nil, fail.InvalidArgument("not a way to connect").Saying("%q is not a way to connect", method)
	}
	if err != nil {
		return nil, err
	}

	connection, err := s.connections.Create(ctx, forgestore.CreateParams{
		Kind:       kind,
		Host:       host,
		APIURL:     apiURL,
		Method:     method,
		Login:      login,
		VerifiedAt: s.now(),
	})
	if err != nil {
		return nil, err
	}

	if method == MethodToken {
		if err := s.credentials.Set(ctx, connection.ID, params.Token); err != nil {
			return nil, errors.Join(err, s.connections.Delete(ctx, connection.ID))
		}
	}

	return connection, nil
}

// proveToken asks the forge who a pasted token belongs to.
func (s *Service) proveToken(ctx context.Context, build Builder, kind, host, apiURL string, token Secret) (string, error) {
	if token.IsZero() {
		return "", fail.InvalidArgument("a token is needed")
	}

	probe := build(apiURL, staticCredential{Header: HeaderFor(kind, MethodToken), Token: token})
	login, err := probe.Verify(ctx)
	if err != nil {
		return "", fmt.Errorf("%s did not accept the token: %w", host, err)
	}

	return login, nil
}

// proveApp mints from the App, checks its installation reaches a repository, and answers with
// the App's own name.
func (s *Service) proveApp(ctx context.Context, build Builder, kind, apiURL string) (string, error) {
	if kind != KindGitHub {
		return "", fail.InvalidArgument("a github app connects only to github")
	}
	if !s.app.IsConfigured() {
		return "", ErrNoApp.Saying("set app_id, installation_id and private_key_path under [github_app]")
	}

	probe, ok := build(apiURL, NewAppCredential(NewAppCredentialParams{App: s.app, Connections: NoRecord{}})).(InstallationVerifier)
	if !ok {
		return "", fail.InvalidArgument("this forge cannot check an installation")
	}

	reached, err := probe.VerifyInstallation(ctx)
	if err != nil {
		return "", fmt.Errorf("github did not accept the app's token: %w", err)
	}
	if reached == 0 {
		return "", ErrAppReachesNothing.Saying("install the app on the repositories your agents work in")
	}

	return s.app.Slug(ctx)
}

// List is every connection, by host.
func (s *Service) List(ctx context.Context) ([]forgestore.Connection, error) {
	return s.connections.List(ctx)
}

// Disconnect removes a connection and the credential beside it.
func (s *Service) Disconnect(ctx context.Context, host string) error {
	connection, err := s.connections.GetByHost(ctx, strings.ToLower(strings.TrimSpace(host)))
	if err != nil {
		return err
	}

	if err := s.credentials.Delete(ctx, connection.ID); err != nil {
		return err
	}

	return s.connections.Delete(ctx, connection.ID)
}

// HasCredential says whether a connection still has what it needs to speak, which for an App
// row is always so.
func (s *Service) HasCredential(ctx context.Context, connection forgestore.Connection) (bool, error) {
	if connection.Method == MethodGitHubApp {
		return true, nil
	}

	_, err := s.credentials.Get(ctx, connection.ID)
	if errors.Is(err, ErrNoCredential) {
		return false, nil
	}
	if err != nil {
		return false, err
	}

	return true, nil
}

// ForConnection is the forge to speak to as a connection.
func (s *Service) ForConnection(connection forgestore.Connection) (Forge, error) {
	build, ok := s.builders[connection.Kind]
	if !ok {
		return nil, ErrUnknownKind.Saying("%q is not a forge this build knows", connection.Kind)
	}

	var auth Authenticator = NewStoredCredential(NewStoredCredentialParams{
		Store:        s.credentials,
		ConnectionID: connection.ID,
		Header:       HeaderFor(connection.Kind, connection.Method),
		Connections:  s.verified,
	})
	if connection.Method == MethodGitHubApp {
		auth = NewAppCredential(NewAppCredentialParams{
			App:          s.app,
			ConnectionID: connection.ID,
			Connections:  s.verified,
		})
	}

	return build(connection.APIURL, auth), nil
}

// staticCredential is a token held in memory for the one call that proves it.
type staticCredential Credential

func (c staticCredential) Credential(context.Context) (Credential, error) {
	return Credential(c), nil
}

func (staticCredential) Invalidate(context.Context) error { return nil }

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

// ErrBadApp says the [github_app] section in config cannot be read as an App.
var ErrBadApp = fail.Precondition("the github app in config is unusable")

// ErrAppReachesNothing says the App is installed on no repository.
var ErrAppReachesNothing = fail.Precondition("the github app is installed on no repository")

// ErrKindUnknownForHost says a host names no public forge, so the kind has to be given.
var ErrKindUnknownForHost = fail.InvalidArgument("the host does not say which forge it runs")

// ErrNoHost says a connection was asked for with no host.
var ErrNoHost = fail.InvalidArgument("a host is needed")

// ErrUnknownMethod says a connection was asked for by a method that is not one.
var ErrUnknownMethod = fail.InvalidArgument("not a way to connect")

// ErrNoToken says the token method was asked for with no token.
var ErrNoToken = fail.InvalidArgument("a token is needed")

// ErrAppNotGitHub says the App method was asked for on a forge that is not GitHub.
var ErrAppNotGitHub = fail.InvalidArgument("a github app connects only to github")

// ErrNoInstallationCheck says the forge has no way to check an installation.
var ErrNoInstallationCheck = fail.InvalidArgument("this forge cannot check an installation")

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

// NewForgeServiceParams says what the service writes to and which forges it can build.
type NewForgeServiceParams struct {
	Connections *forgestore.Store
	Credentials CredentialStore
	Builders    map[string]Builder
	// Verified is what a refusal is recorded on, the connections themselves when unset and
	// NoRecord for a caller that only reports.
	Verified verifiedSetter
	// LoadApp reads the GitHub App this machine can connect as, on every call that needs it,
	// and is unset or answers an unconfigured App when it has none.
	LoadApp func() (App, error)
	// Now is the clock, time.Now when unset.
	Now func() time.Time
}

// ForgeService connects and disconnects forges, and builds one to speak to for a connection.
type ForgeService struct {
	connections *forgestore.Store
	credentials CredentialStore
	builders    map[string]Builder
	verified    verifiedSetter
	loadApp     func() (App, error)
	now         func() time.Time
}

// NewForgeService builds the service.
func NewForgeService(params NewForgeServiceParams) *ForgeService {
	now := params.Now
	if now == nil {
		now = time.Now
	}
	verified := params.Verified
	if verified == nil {
		verified = params.Connections
	}
	loadApp := params.LoadApp
	if loadApp == nil {
		loadApp = func() (App, error) {
			return App{}, nil
		}
	}

	return &ForgeService{
		connections: params.Connections,
		credentials: params.Credentials,
		builders:    params.Builders,
		verified:    verified,
		loadApp:     loadApp,
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

// ResolveAPIURL is where the host's API answers: APIURL checked for https, or for a host that
// is not the public forge, the place that forge's self-hosted builds put it.
func (p ConnectParams) ResolveAPIURL() (string, error) {
	given := strings.TrimRight(strings.TrimSpace(p.APIURL), "/")
	if given != "" {
		parsed, err := url.Parse(given)
		if err != nil || parsed.Scheme != "https" || parsed.Host == "" {
			return "", ErrInsecureAPI.Saying("%q is not an https address", given)
		}

		return given, nil
	}

	if _, err := KindForHost(p.Host); err == nil {
		return "", nil
	}

	switch p.Kind {
	case KindGitHub:
		return "https://" + p.Host + "/api/v3", nil
	case KindGitLab:
		return "https://" + p.Host + "/api/v4", nil
	default:
		return "", ErrUnknownKind.Saying("%q is not a forge this build knows", p.Kind)
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
func (s *ForgeService) Connect(ctx context.Context, params ConnectParams) (*forgestore.Connection, error) {
	params.Host = strings.ToLower(strings.TrimSpace(params.Host))
	if params.Host == "" {
		return nil, ErrNoHost
	}

	if params.Kind == "" {
		found, err := KindForHost(params.Host)
		if err != nil {
			return nil, err
		}

		params.Kind = found
	}

	build, ok := s.builders[params.Kind]
	if !ok {
		return nil, ErrUnknownKind.Saying("%q is not a forge this build knows", params.Kind)
	}

	if params.Method == "" {
		params.Method = MethodToken
	}

	apiURL, err := params.ResolveAPIURL()
	if err != nil {
		return nil, err
	}
	params.APIURL = apiURL

	if _, err := s.connections.GetByHost(ctx, params.Host); err == nil {
		return nil, forgestore.ErrHostTaken.Saying("%s is already connected; disconnect it first", params.Host)
	} else if !errors.Is(err, store.ErrNotFound) {
		return nil, err
	}

	var login string
	switch params.Method {
	case MethodToken:
		login, err = s.proveToken(ctx, build, params)
	case MethodGitHubApp:
		login, err = s.proveApp(ctx, build, params)
	default:
		return nil, ErrUnknownMethod.Saying("%q is not a way to connect", params.Method)
	}
	if err != nil {
		return nil, err
	}

	connection, err := s.connections.Create(ctx, forgestore.CreateParams{
		Kind:       params.Kind,
		Host:       params.Host,
		APIURL:     params.APIURL,
		Method:     params.Method,
		Login:      login,
		VerifiedAt: s.now(),
	})
	if err != nil {
		return nil, err
	}

	if params.Method == MethodToken {
		if err := s.credentials.Set(ctx, connection.ID, params.Token); err != nil {
			return nil, errors.Join(err, s.connections.Delete(ctx, connection.ID))
		}
	}

	return connection, nil
}

// proveToken asks the forge who a pasted token belongs to.
func (s *ForgeService) proveToken(ctx context.Context, build Builder, params ConnectParams) (string, error) {
	if params.Token.IsZero() {
		return "", ErrNoToken
	}

	probe := build(params.APIURL, staticCredential{Header: HeaderFor(params.Kind, MethodToken), Token: params.Token})
	login, err := probe.Verify(ctx)
	if errors.Is(err, ErrUnauthorized) {
		return "", fmt.Errorf("failed to verify the token with %s: %w", params.Host, err)
	}
	if err != nil {
		return "", err
	}

	return login, nil
}

// proveApp mints from the App, checks its installation reaches a repository, and answers with
// the App's own name.
func (s *ForgeService) proveApp(ctx context.Context, build Builder, params ConnectParams) (string, error) {
	if params.Kind != KindGitHub {
		return "", ErrAppNotGitHub
	}
	app, err := s.loadApp()
	if err != nil {
		return "", ErrBadApp.Saying("%v", err)
	}
	if !app.IsConfigured() {
		return "", ErrNoApp.Saying("set app_id, installation_id and private_key_path under [github_app]")
	}

	app = app.At(params.APIURL)
	probe := build(params.APIURL, NewAppCredential(NewAppCredentialParams{App: app, Connections: NoRecord{}}))
	err = requireInstallationReach(ctx, probe)
	if errors.Is(err, ErrUnauthorized) {
		return "", fmt.Errorf("failed to verify the app's token with github: %w", err)
	}
	if err != nil {
		return "", err
	}

	return app.Slug(ctx)
}

// requireInstallationReach asks the forge whether the credential's installation reaches a
// repository, refusing a forge that cannot say and an installation that reaches none.
func requireInstallationReach(ctx context.Context, f Forge) error {
	installation, ok := f.(InstallationVerifier)
	if !ok {
		return ErrNoInstallationCheck
	}

	reached, err := installation.VerifyInstallation(ctx)
	if err != nil {
		return err
	}
	if reached == 0 {
		return ErrAppReachesNothing.Saying("install the app on the repositories your agents work in")
	}

	return nil
}

// List is every connection, by host.
func (s *ForgeService) List(ctx context.Context) ([]forgestore.Connection, error) {
	return s.connections.List(ctx)
}

// Disconnect removes a connection and the credential beside it.
func (s *ForgeService) Disconnect(ctx context.Context, host string) error {
	connection, err := s.connections.GetByHost(ctx, strings.ToLower(strings.TrimSpace(host)))
	if err != nil {
		return err
	}

	if err := s.credentials.Delete(ctx, connection.ID); err != nil {
		return err
	}

	return s.connections.Delete(ctx, connection.ID)
}

// Check asks the forge whether a connection still works, the way its method allows: who the
// token belongs to for a pasted token, and whether the installation still reaches a repository
// for an App, whose login is the row's own.
func (s *ForgeService) Check(ctx context.Context, connection forgestore.Connection) (string, error) {
	held, err := s.HasCredential(ctx, connection)
	if err != nil {
		return "", err
	}
	if !held {
		return "", ErrNoCredential.Saying("no credential file for %s; connect it again", connection.Host)
	}

	f, err := s.ForConnection(connection)
	if err != nil {
		return "", err
	}

	if connection.Method != MethodGitHubApp {
		return f.Verify(ctx)
	}

	if err := requireInstallationReach(ctx, f); err != nil {
		return "", err
	}

	return connection.Login, nil
}

// ConnectionFor is the connection a host has, or ErrNotFound.
func (s *ForgeService) ConnectionFor(ctx context.Context, host string) (*forgestore.Connection, error) {
	return s.connections.GetByHost(ctx, strings.ToLower(host))
}

// SetVerifiedAt records when a forge last confirmed a connection, or clears it.
func (s *ForgeService) SetVerifiedAt(ctx context.Context, id string, at *time.Time) error {
	return s.connections.SetVerifiedAt(ctx, id, at)
}

// HasCredential says whether a connection still has what it needs to speak, which for an App
// row is always so.
func (s *ForgeService) HasCredential(ctx context.Context, connection forgestore.Connection) (bool, error) {
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

// SetThrottledUntil records when a connection's forge said its rate limit resets, or clears it.
func (s *ForgeService) SetThrottledUntil(ctx context.Context, id string, until *time.Time) error {
	return s.connections.SetThrottledUntil(ctx, id, until)
}

// ForConnection is the forge to speak to as a connection.
func (s *ForgeService) ForConnection(connection forgestore.Connection) (Forge, error) {
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
		app, err := s.loadApp()
		if err != nil {
			return nil, ErrBadApp.Saying("%v", err)
		}

		auth = NewAppCredential(NewAppCredentialParams{
			App:          app.At(connection.APIURL),
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

func (staticCredential) Invalidate(context.Context) error {
	return nil
}

package forge

import (
	"context"
	"errors"
	"fmt"
	"net/url"
	"strings"
	"sync"
	"time"

	"komrad/internal/fail"
	"komrad/internal/store"
	forgestore "komrad/internal/store/forge"
)

// The kinds of trouble a check reports.
const (
	ProblemRefused      = "refused"
	problemNoCredential = "no_credential"
	problemUnreachable  = "unreachable"
)

// errUnknownKind says no forge of that kind is compiled in.
var errUnknownKind = fail.InvalidArgument("no forge of that kind")

// errInsecureAPI says an API address would send the credential without TLS.
var errInsecureAPI = fail.InvalidArgument("the API address has to be https")

// errNoApp says the App method was asked for on a build with no App configured.
var errNoApp = fail.Precondition("no github app is configured")

// errBadApp says the [github_app] section in config cannot be read as an App.
var errBadApp = fail.Precondition("the github app in config is unusable")

// errAppReachesNothing says the App is installed on no repository.
var errAppReachesNothing = fail.Precondition("the github app is installed on no repository")

// errKindUnknownForHost says a host names no public forge, so the kind has to be given.
var errKindUnknownForHost = fail.InvalidArgument("the host does not say which forge it runs")

// errNoHost says a connection was asked for with no host.
var errNoHost = fail.InvalidArgument("a host is needed")

// errUnknownMethod says a connection was asked for by a method that is not one.
var errUnknownMethod = fail.InvalidArgument("not a way to connect")

// errNoToken says the token method was asked for with no token.
var errNoToken = fail.InvalidArgument("a token is needed")

// errAppNotGitHub says the App method was asked for on a forge that is not GitHub.
var errAppNotGitHub = fail.InvalidArgument("a github app connects only to github")

// errNoInstallationCheck says the forge has no way to check an installation.
var errNoInstallationCheck = fail.InvalidArgument("this forge cannot check an installation")

// errRotatingNotGitLab says the rotating token method was asked for on a forge that is not GitLab.
var errRotatingNotGitLab = fail.InvalidArgument("a rotating token connects only to gitlab")

// errNoRotation says the forge has no way to rotate a token.
var errNoRotation = fail.InvalidArgument("this forge cannot rotate a token")

// errTokenCannotRotate says a token lacks the scope it needs to rotate itself.
var errTokenCannotRotate = fail.InvalidArgument("the token lacks the scope to rotate itself")

// Builder makes the forge for one kind, reaching the API at api as auth.
type Builder func(api string, auth Authenticator) Forge

// CheckResult is what a check found of one connection, and the connection as the check left it.
type CheckResult struct {
	Connection  forgestore.Connection
	Problem     error
	ProblemKind string
}

// NewServiceParams says what the service writes to and which forges it can build.
type NewServiceParams struct {
	Connections *forgestore.ForgeStore
	Credentials credentialStore
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

// Service connects and disconnects forges, and builds one to speak to for a connection.
type Service struct {
	connections *forgestore.ForgeStore
	credentials credentialStore
	builders    map[string]Builder
	verified    verifiedSetter
	loadApp     func() (App, error)
	now         func() time.Time
	rotation    sync.Mutex
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
	loadApp := params.LoadApp
	if loadApp == nil {
		loadApp = func() (App, error) {
			return App{}, nil
		}
	}

	return &Service{
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
		return "", errKindUnknownForHost.Saying("%s is not a public forge, so say which kind it runs", host)
	}
}

// resolveAPIURL is where the host's API answers: APIURL checked for https, or for a host that
// is not the public forge, the place that forge's self-hosted builds put it.
func (p ConnectParams) resolveAPIURL() (string, error) {
	given := strings.TrimRight(strings.TrimSpace(p.APIURL), "/")
	if given != "" {
		parsed, err := url.Parse(given)
		if err != nil || parsed.Scheme != "https" || parsed.Host == "" {
			return "", errInsecureAPI.Saying("%q is not an https address", given)
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
		return "", errUnknownKind.Saying("%q is not a forge this build knows", p.Kind)
	}
}

// headerFor is the header a credential obtained one way is sent in on one kind of forge.
func headerFor(kind, method string) string {
	if kind == KindGitLab && method != MethodGitHubApp {
		return HeaderPrivateToken
	}

	return HeaderBearer
}

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
	// Token is the credential for MethodToken and MethodRotatingToken, and ignored for MethodGitHubApp.
	Token Secret
}

// Connect proves a credential against the forge and records the connection, refusing a host
// that is already connected and a credential the forge does not accept.
func (s *Service) Connect(ctx context.Context, params ConnectParams) (*forgestore.Connection, error) {
	params.Host = strings.ToLower(strings.TrimSpace(params.Host))
	if params.Host == "" {
		return nil, errNoHost
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
		return nil, errUnknownKind.Saying("%q is not a forge this build knows", params.Kind)
	}

	if params.Method == "" {
		params.Method = MethodToken
	}

	apiURL, err := params.resolveAPIURL()
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
	case MethodRotatingToken:
		login, err = s.proveRotatingToken(ctx, build, params)
	default:
		return nil, errUnknownMethod.Saying("%q is not a way to connect", params.Method)
	}
	if err != nil {
		return nil, err
	}

	if params.Method == MethodRotatingToken {
		s.rotation.Lock()
		defer s.rotation.Unlock()
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

	if err := s.keepCredential(ctx, build, params, connection.ID); err != nil {
		return nil, errors.Join(err, s.connections.Delete(ctx, connection.ID))
	}

	return connection, nil
}

// keepCredential stores what a new connection sends the forge, which an App mints rather than keeps.
func (s *Service) keepCredential(ctx context.Context, build Builder, params ConnectParams, connectionID string) error {
	switch params.Method {
	case MethodToken:
		return s.credentials.Set(ctx, connectionID, params.Token)
	case MethodRotatingToken:
		return s.keepRotatedToken(ctx, build, params, connectionID)
	default:
		return nil
	}
}

// proveToken asks the forge who a pasted token belongs to.
func (s *Service) proveToken(ctx context.Context, build Builder, params ConnectParams) (string, error) {
	if params.Token.IsZero() {
		return "", errNoToken
	}

	probe := build(params.APIURL, staticCredential{Header: headerFor(params.Kind, MethodToken), Token: params.Token})
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
func (s *Service) proveApp(ctx context.Context, build Builder, params ConnectParams) (string, error) {
	if params.Kind != KindGitHub {
		return "", errAppNotGitHub
	}
	app, err := s.loadApp()
	if err != nil {
		return "", errBadApp.Saying("%v", err)
	}
	if !app.IsConfigured() {
		return "", errNoApp.Saying("set app_id, installation_id and private_key_path under [github_app]")
	}

	app = app.At(params.APIURL)
	probe := build(params.APIURL, newAppCredential(newAppCredentialParams{App: app, Connections: noRecord{}}))
	err = requireInstallationReach(ctx, probe)
	if errors.Is(err, ErrUnauthorized) {
		return "", fmt.Errorf("failed to verify the app's token with github: %w", err)
	}
	if err != nil {
		return "", err
	}

	return app.slug(ctx)
}

// requireInstallationReach asks the forge whether the credential's installation reaches a
// repository, refusing a forge that cannot say and an installation that reaches none.
func requireInstallationReach(ctx context.Context, f Forge) error {
	installation, ok := f.(installationVerifier)
	if !ok {
		return errNoInstallationCheck
	}

	reached, err := installation.VerifyInstallation(ctx)
	if err != nil {
		return err
	}
	if reached == 0 {
		return errAppReachesNothing.Saying("install the app on the repositories your agents work in")
	}

	return nil
}

// ListConnections is every connection, by host.
func (s *Service) ListConnections(ctx context.Context) ([]forgestore.Connection, error) {
	return s.connections.List(ctx)
}

// Disconnect removes a connection and the credential beside it.
func (s *Service) Disconnect(ctx context.Context, host string) error {
	connection, err := s.connections.GetByHost(ctx, strings.ToLower(strings.TrimSpace(host)))
	if err != nil {
		return err
	}

	var revoked error
	if connection.Method == MethodRotatingToken {
		revoked = s.revokeRotatingToken(ctx, *connection)
	}

	if err := s.credentials.Delete(ctx, connection.ID); err != nil {
		return errors.Join(revoked, err)
	}

	return errors.Join(revoked, s.connections.Delete(ctx, connection.ID))
}

// Check asks the forge whether a connection still works, the way its method allows: who the
// token belongs to for a pasted token, which a rotating one also rotates when due, and whether
// the installation still reaches a repository for an App, whose login is the row's own.
func (s *Service) Check(ctx context.Context, connection forgestore.Connection) (string, error) {
	if connection.Method == MethodRotatingToken {
		return s.checkRotating(ctx, connection)
	}

	f, err := s.heldForge(ctx, connection)
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

// heldForge is the forge for a connection whose credential komrad still holds.
func (s *Service) heldForge(ctx context.Context, connection forgestore.Connection) (Forge, error) {
	held, err := s.HasCredential(ctx, connection)
	if err != nil {
		return nil, err
	}
	if !held {
		return nil, ErrNoCredential.Saying("no credential file for %s; connect it again", connection.Host)
	}

	return s.ForConnection(connection)
}

// RecheckConnection checks a connection and records the forge's answer on its verified stamp.
func (s *Service) RecheckConnection(ctx context.Context, connection forgestore.Connection) (*CheckResult, error) {
	if _, err := s.Check(ctx, connection); err != nil {
		kind := problemKind(err)
		if kind != problemUnreachable {
			if err := s.connections.SetVerifiedAt(ctx, connection.ID, nil); err != nil {
				return nil, err
			}

			connection.VerifiedAt = nil
		}

		return &CheckResult{
			Connection:  connection,
			Problem:     err,
			ProblemKind: kind,
		}, nil
	}

	at := s.now()
	if err := s.connections.SetVerifiedAt(ctx, connection.ID, &at); err != nil {
		return nil, err
	}

	connection.VerifiedAt = &at

	return &CheckResult{Connection: connection}, nil
}

// ConnectionFor is the connection a host has, or ErrNotFound.
func (s *Service) ConnectionFor(ctx context.Context, host string) (*forgestore.Connection, error) {
	return s.connections.GetByHost(ctx, strings.ToLower(host))
}

// ForHost is the forge for a host's connection.
func (s *Service) ForHost(ctx context.Context, host string) (Forge, error) {
	connection, err := s.ConnectionFor(ctx, host)
	if err != nil {
		return nil, fmt.Errorf("failed to find the forge for %s: %w", host, err)
	}

	hostForge, err := s.ForConnection(*connection)
	if err != nil {
		return nil, fmt.Errorf("failed to build the forge for %s: %w", host, err)
	}

	return hostForge, nil
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

// SetThrottledUntil records when a connection's forge said its rate limit resets, or clears it.
func (s *Service) SetThrottledUntil(ctx context.Context, id string, until *time.Time) error {
	return s.connections.SetThrottledUntil(ctx, id, until)
}

// ForConnection is the forge to speak to as a connection.
func (s *Service) ForConnection(connection forgestore.Connection) (Forge, error) {
	build, ok := s.builders[connection.Kind]
	if !ok {
		return nil, errUnknownKind.Saying("%q is not a forge this build knows", connection.Kind)
	}

	var auth Authenticator = newStoredCredential(newStoredCredentialParams{
		Store:        s.credentials,
		ConnectionID: connection.ID,
		Header:       headerFor(connection.Kind, connection.Method),
		Connections:  s.verified,
	})
	if connection.Method == MethodGitHubApp {
		app, err := s.loadApp()
		if err != nil {
			return nil, errBadApp.Saying("%v", err)
		}

		auth = newAppCredential(newAppCredentialParams{
			App:          app.At(connection.APIURL),
			ConnectionID: connection.ID,
			Connections:  s.verified,
		})
	}

	return build(connection.APIURL, auth), nil
}

// problemKind says which kind of trouble a connection is in.
func problemKind(err error) string {
	switch {
	case errors.Is(err, ErrNoCredential):
		return problemNoCredential
	case errors.Is(err, ErrUnauthorized), errors.Is(err, errAppReachesNothing):
		return ProblemRefused
	default:
		return problemUnreachable
	}
}

// staticCredential is a token held in memory for the one call that proves it.
type staticCredential Credential

func (c staticCredential) Credential(context.Context) (Credential, error) {
	return Credential(c), nil
}

func (staticCredential) Invalidate(context.Context) error {
	return nil
}

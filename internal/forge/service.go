package forge

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"time"

	"weave/internal/fail"
	"weave/internal/store"
	forgestore "weave/internal/store/forge"
)

// ErrUnknownKind says no forge of that kind is compiled in.
var ErrUnknownKind = fail.InvalidArgument("no forge of that kind")

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
	Token  Secret
}

// NewServiceParams says what the service writes to and which forges it can build.
type NewServiceParams struct {
	Connections *forgestore.Store
	Credentials CredentialStore
	Builders    map[string]Builder
	// Verified is what a refusal is recorded on, the connections themselves when unset and
	// NoRecord for a caller that only reports.
	Verified verifiedSetter
	// Now is the clock, time.Now when unset.
	Now func() time.Time
}

// Service connects and disconnects forges, and builds one to speak to for a connection.
type Service struct {
	connections *forgestore.Store
	credentials CredentialStore
	builders    map[string]Builder
	verified    verifiedSetter
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

// HeaderFor is the header a credential obtained one way is sent in on one kind of forge.
func HeaderFor(kind, method string) string {
	if kind == KindGitLab && method == MethodToken {
		return HeaderPrivateToken
	}

	return HeaderBearer
}

// Connect proves a token against the forge and records the connection, refusing a host that
// is already connected and a token the forge does not accept.
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

	if params.Token.IsZero() {
		return nil, fail.InvalidArgument("a token is needed")
	}

	if _, err := s.connections.GetByHost(ctx, host); err == nil {
		return nil, forgestore.ErrHostTaken.Saying("%s is already connected; disconnect it first", host)
	} else if !errors.Is(err, store.ErrNotFound) {
		return nil, err
	}

	probe := build(params.APIURL, staticCredential{Header: HeaderFor(kind, MethodToken), Token: params.Token})
	login, err := probe.Verify(ctx)
	if err != nil {
		return nil, fmt.Errorf("%s did not accept the token: %w", host, err)
	}

	connection, err := s.connections.Create(ctx, forgestore.CreateParams{
		Kind:       kind,
		Host:       host,
		APIURL:     params.APIURL,
		Method:     MethodToken,
		Login:      login,
		VerifiedAt: s.now(),
	})
	if err != nil {
		return nil, err
	}

	if err := s.credentials.Set(ctx, connection.ID, params.Token); err != nil {
		return nil, errors.Join(err, s.connections.Delete(ctx, connection.ID))
	}

	return connection, nil
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

// HasCredential says whether the store still holds a connection's token.
func (s *Service) HasCredential(ctx context.Context, connection forgestore.Connection) (bool, error) {
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

	auth := NewStoredCredential(NewStoredCredentialParams{
		Store:        s.credentials,
		ConnectionID: connection.ID,
		Header:       HeaderFor(connection.Kind, connection.Method),
		Connections:  s.verified,
	})

	return build(connection.APIURL, auth), nil
}

// staticCredential is a token held in memory for the one call that proves it.
type staticCredential Credential

func (c staticCredential) Credential(context.Context) (Credential, error) {
	return Credential(c), nil
}

func (staticCredential) Invalidate(context.Context) error { return nil }

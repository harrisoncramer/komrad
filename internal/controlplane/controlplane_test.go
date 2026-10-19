package controlplane_test

import (
	"context"
	"errors"
	"log/slog"
	"net/http/httptest"
	"sync"
	"testing"
	"time"

	"komrad/internal/caller"
	"komrad/internal/controlplane"
	"komrad/internal/controlplanetest"
	"komrad/internal/dbtest"
	"komrad/internal/forge"
	"komrad/internal/interceptors"
	"komrad/internal/rpcerr"
	"komrad/internal/store"

	"github.com/stretchr/testify/require"
)

// engine is one engine the control plane serves, with its forge proxy credential.
type engine struct {
	name       string
	credential string
}

// plane is a control plane serving harry's, ada's and grace's engines in front of a GitHub,
// with harry's and ada's people already through the device flow and harry granted all three.
type plane struct {
	github    *controlplanetest.GitHub
	ca        *controlplanetest.CA
	logins    *forge.FileStore
	clock     *clock
	engines   map[string]engine
	proxy     *httptest.Server
	endpoints *httptest.Server
}

func startPlane(t *testing.T) *plane {
	t.Helper()

	database := dbtest.New(t)
	engines := make(map[string]engine)
	credentials := make(map[string]string)
	for _, person := range []string{"harry", "ada", "grace"} {
		name := dbtest.NewEngine(t, database).Name
		engines[person] = engine{name: name, credential: "credential-" + person}
		credentials[name] = "credential-" + person

		require.NoError(t, store.GrantAccess(t.Context(), store.GrantAccessParams{
			OwnerURL: database.OwnerURL,
			Engine:   name,
			Email:    "harry@example.com",
		}))
	}

	grants, err := store.OpenGrantStore(t.Context(), database.OwnerURL)
	require.NoError(t, err)
	t.Cleanup(grants.Close)

	ca := controlplanetest.NewCA(t)
	github := controlplanetest.StartGitHub(t, ca)
	logins := forge.NewFileStore(t.TempDir())
	now := &clock{at: time.Date(2026, 10, 18, 9, 0, 0, 0, time.UTC)}

	require.NoError(t, logins.Set(t.Context(), engines["harry"].name, forge.NewSecret("gho_harry")))
	require.NoError(t, logins.Set(t.Context(), engines["ada"].name, forge.NewSecret("gho_ada")))

	served := controlplane.New(controlplane.NewParams{
		Logger:         slog.New(slog.DiscardHandler),
		Engines:        credentials,
		CA:             ca.Cert,
		Logins:         logins,
		Grants:         grants,
		Verifier:       verifier{},
		GitHubClientID: "Iv1.komrad",
		GitHub:         github.URL,
		Client:         github.Client(),
		Upstream:       github.Upstream(),
		Now:            now.Now,
	})

	proxy := httptest.NewServer(served.ForgeProxy())
	endpoints := httptest.NewServer(served.Handler())
	t.Cleanup(func() {
		endpoints.Close()
		proxy.Close()
		require.NoError(t, served.Close())
	})

	return &plane{github: github, ca: ca, logins: logins, clock: now, engines: engines, proxy: proxy, endpoints: endpoints}
}

// engine is the engine a person's name stands for, or an engine by that name the control plane
// does not serve.
func (p *plane) engine(person string) engine {
	served, ok := p.engines[person]
	if !ok {
		return engine{name: person}
	}

	return served
}

type clock struct {
	mu sync.Mutex
	at time.Time
}

func (c *clock) Now() time.Time {
	c.mu.Lock()
	defer c.mu.Unlock()

	return c.at
}

func (c *clock) advance(by time.Duration) {
	c.mu.Lock()
	defer c.mu.Unlock()

	c.at = c.at.Add(by)
}

// Tokens the verifier knows, each naming a person or failing the way a provider can.
const (
	harryToken    = "id-token-harry"
	strangerToken = "id-token-stranger"
	downToken     = "id-token-down"
)

type verifier struct{}

func (verifier) VerifyToken(_ context.Context, raw string) (*interceptors.SignedIn, error) {
	switch raw {
	case harryToken:
		return &interceptors.SignedIn{Caller: caller.Caller{Email: "harry@example.com", Issuer: "https://idp.example.com", Subject: "harry"}, EmailVerified: true}, nil
	case strangerToken:
		return &interceptors.SignedIn{Caller: caller.Caller{Email: "stranger@example.com", Issuer: "https://idp.example.com", Subject: "stranger"}, EmailVerified: true}, nil
	case downToken:
		return nil, rpcerr.Unavailable(errors.New("the provider is down"))
	default:
		return nil, rpcerr.Unauthenticated(errors.New("the token is not the provider's"))
	}
}

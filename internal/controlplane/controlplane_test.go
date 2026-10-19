package controlplane_test

import (
	"context"
	"encoding/json"
	"errors"
	"log/slog"
	"net"
	"net/http"
	"net/http/httptest"
	"sync"
	"testing"
	"time"

	"komrad/config"
	"komrad/internal/caller"
	"komrad/internal/controlplane"
	"komrad/internal/controlplanetest"
	"komrad/internal/dbtest"
	"komrad/internal/forge"
	"komrad/internal/interceptors"
	"komrad/internal/rpcerr"
	"komrad/internal/store"
	"komrad/lib/xhttp"

	"github.com/stretchr/testify/require"
)

// engine is one engine the control plane serves, with its forge proxy credential.
type engine struct {
	name       string
	credential string
}

// signIn is the provider the control plane answers each engine's sign-in path with.
var signIn = config.SignIn{Issuer: "https://idp.example.com", ClientID: "komrad"}

// plane is a control plane serving harry's, ada's and grace's engines in front of a GitHub,
// with harry's and ada's people already through the device flow and harry granted all three.
// Harry's engine answers each request with what reached it, and ada's and grace's answer
// nothing.
type plane struct {
	github    *controlplanetest.GitHub
	ca        *controlplanetest.CA
	logins    *forge.FileStore
	clock     *clock
	runner    *runner
	engines   map[string]engine
	proxy     *httptest.Server
	endpoints *httptest.Server
}

func startPlane(t *testing.T) *plane {
	t.Helper()

	database := dbtest.New(t)
	engines := make(map[string]engine)
	behind := make(map[string]config.ControlPlaneEngine)
	for _, person := range []string{"harry", "ada", "grace"} {
		name := dbtest.NewEngine(t, database).Name
		engines[person] = engine{name: name, credential: "credential-" + person}
		behind[name] = config.ControlPlaneEngine{Credential: "credential-" + person, Addr: closedAddr(t)}

		require.NoError(t, store.GrantAccess(t.Context(), store.GrantAccessParams{
			OwnerURL: database.OwnerURL,
			Engine:   name,
			Email:    "harry@example.com",
		}))
	}
	behind[engines["harry"].name] = config.ControlPlaneEngine{Credential: "credential-harry", Addr: startEngine(t)}

	grants, err := store.OpenGrantStore(t.Context(), database.OwnerURL)
	require.NoError(t, err)
	t.Cleanup(grants.Close)

	ca := controlplanetest.NewCA(t)
	github := controlplanetest.StartGitHub(t, ca)
	logins := forge.NewFileStore(t.TempDir())
	now := &clock{at: time.Date(2026, 10, 18, 9, 0, 0, 0, time.UTC)}
	engineRunner := &runner{states: make(map[string]controlplane.EngineState)}

	require.NoError(t, logins.Set(t.Context(), engines["harry"].name, forge.NewSecret("gho_harry")))
	require.NoError(t, logins.Set(t.Context(), engines["ada"].name, forge.NewSecret("gho_ada")))

	served := controlplane.New(controlplane.NewParams{
		Logger:         slog.New(slog.DiscardHandler),
		Engines:        behind,
		SignIn:         signIn,
		CA:             ca.Cert,
		Logins:         logins,
		Grants:         grants,
		Verifier:       verifier{},
		Runner:         engineRunner,
		GitHubClientID: "Iv1.komrad",
		GitHub:         github.URL,
		GitHubAPI:      github.URL,
		Client:         github.Client(),
		Upstream:       github.Upstream(),
		Engine:         controlplane.NewEngineTransport(),
		Now:            now.Now,
	})

	proxy := httptest.NewServer(served.ForgeProxy())
	endpoints := httptest.NewUnstartedServer(served.Handler())
	endpoints.Config.Protocols = xhttp.NewServerProtocols()
	endpoints.Start()
	t.Cleanup(func() {
		endpoints.Close()
		proxy.Close()
		require.NoError(t, served.Close())
	})

	return &plane{github: github, ca: ca, logins: logins, clock: now, runner: engineRunner, engines: engines, proxy: proxy, endpoints: endpoints}
}

// reached is what harry's engine answers with: the request as it reached the engine.
type reached struct {
	Path          string `json:"path"`
	Authorization string `json:"authorization"`
	Proto         string `json:"proto"`
}

// startEngine serves an engine that answers each request with what reached it, over HTTP/2
// without TLS the way an engine's public listener does.
func startEngine(t *testing.T) string {
	t.Helper()

	engine := httptest.NewUnstartedServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(reached{ //nolint:errchkjson // the test hears a cut answer as a failed request
			Path:          r.URL.Path,
			Authorization: r.Header.Get("Authorization"),
			Proto:         r.Proto,
		})
	}))
	engine.Config.Protocols = xhttp.NewServerProtocols()
	engine.Start()
	t.Cleanup(engine.Close)

	return engine.Listener.Addr().String()
}

// closedAddr is a loopback address nothing listens on.
func closedAddr(t *testing.T) string {
	t.Helper()

	listener, err := (&net.ListenConfig{}).Listen(t.Context(), "tcp", "127.0.0.1:0")
	require.NoError(t, err)
	addr := listener.Addr().String()
	require.NoError(t, listener.Close())

	return addr
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

// runner keeps each engine's state as the test sets it, and the engines it was asked to start
// and stop.
type runner struct {
	mu      sync.Mutex
	states  map[string]controlplane.EngineState
	err     error
	started []string
	stopped []string
}

func (r *runner) Start(_ context.Context, engine string) error {
	r.mu.Lock()
	defer r.mu.Unlock()

	r.started = append(r.started, engine)

	return r.err
}

func (r *runner) Stop(_ context.Context, engine string) error {
	r.mu.Lock()
	defer r.mu.Unlock()

	r.stopped = append(r.stopped, engine)

	return r.err
}

func (r *runner) State(_ context.Context, engine string) (controlplane.EngineState, error) {
	r.mu.Lock()
	defer r.mu.Unlock()

	return r.states[engine], r.err
}

func (r *runner) set(engine string, state controlplane.EngineState, err error) {
	r.mu.Lock()
	defer r.mu.Unlock()

	r.states[engine] = state
	r.err = err
}

func (r *runner) calls() (started, stopped []string) {
	r.mu.Lock()
	defer r.mu.Unlock()

	return append([]string(nil), r.started...), append([]string(nil), r.stopped...)
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
	harryToken = "id-token-harry"
	adaToken   = "id-token-ada"
	downToken  = "id-token-down"
)

type verifier struct{}

func (verifier) VerifyToken(_ context.Context, raw string) (*interceptors.SignedIn, error) {
	switch raw {
	case harryToken:
		return &interceptors.SignedIn{Caller: caller.Caller{Email: "harry@example.com", Issuer: "https://idp.example.com", Subject: "harry"}, EmailVerified: true}, nil
	case adaToken:
		return &interceptors.SignedIn{Caller: caller.Caller{Email: "ada@example.com", Issuer: "https://idp.example.com", Subject: "ada"}, EmailVerified: true}, nil
	case downToken:
		return nil, rpcerr.Unavailable(errors.New("the provider is down"))
	default:
		return nil, rpcerr.Unauthenticated(errors.New("the token is not the provider's"))
	}
}

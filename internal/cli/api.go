package cli

import (
	"context"
	"net/http"
	"time"

	"komrad/internal/caller"
	"komrad/internal/engine"
	"komrad/lib/xhttp"
)

// apiBase is the base url this machine reaches the engine's api on.
func (a *app) apiBase() string {
	if a.env.URL != "" {
		return a.env.URL
	}

	return formatBase(a.env.Addr)
}

func formatBase(addr string) string {
	return "http://" + addr
}

func (a *app) nameServer() string {
	if a.env.URL != "" {
		return a.env.URL
	}

	return a.env.Addr
}

// apiCallTimeout bounds one call a command makes to the server.
const apiCallTimeout = 60 * time.Second

// caller is who a call from this process says it is from: the agent this pane belongs to, or
// nobody when the call goes to an engine that has no such agent.
func (a *app) caller() caller.Caller {
	if a.engine().IsRemote() {
		return caller.Caller{}
	}

	return caller.Caller{AgentID: a.env.AgentID, SessionID: a.env.SessionID}
}

// withAPI reaches the server this komrad is pointed at, on this machine or at the engine's url,
// and hands the base url to run for the length of one command.
func (a *app) withAPI(ctx context.Context, run func(ctx context.Context, base string) error) error {
	ctx, cancel := context.WithTimeout(ctx, apiCallTimeout)
	defer cancel()

	base := a.apiBase()

	if err := requireServer(ctx, requireServerParams{
		Transport: a.signTransport(xhttp.NewTransport()),
		Base:      base,
		Named:     a.nameServer(),
	}); err != nil {
		return err
	}

	return run(ctx, base)
}

// signTransport sends every request with the signed-in person's token when the engine is reached
// at its url, and leaves next as it is otherwise.
func (a *app) signTransport(next http.RoundTripper) http.RoundTripper {
	if a.env.URL == "" {
		return next
	}

	return engine.NewBearerTransport(a.getTokenRefresher(), next)
}

// getTokenRefresher is the one refresher this process renews the engine's token through.
func (a *app) getTokenRefresher() *engine.TokenRefresher {
	if a.refresher == nil {
		a.refresher = engine.NewTokenRefresher(engine.NewSignInKeeper(a.env.StateDir), a.env.URL)
	}

	return a.refresher
}

// getTokenSource hands out a fresh token for the engine reached at its url, and is nil for an
// engine reached any other way.
func (a *app) getTokenSource() func(ctx context.Context) (string, error) {
	if a.env.URL == "" {
		return nil
	}

	return a.getTokenRefresher().GetToken
}

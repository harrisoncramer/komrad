package forge

import (
	"context"
	"errors"
	"net/http"
	"time"

	githubapp "weave/internal/github/app"
	"weave/lib/xhttp"
)

// MethodGitHubApp is the method for a credential minted from the engine's GitHub App.
const MethodGitHubApp = "github_app"

// App is the GitHub App a service can connect as, and where its minted tokens are kept.
type App struct {
	Cache githubapp.Cache
	// Client makes the mint and describe calls, xhttp's with appCallTimeout when nil.
	Client *http.Client
	// Now is the clock, time.Now when unset.
	Now func() time.Time
}

// appCallTimeout bounds a mint or a describe.
const appCallTimeout = 20 * time.Second

// IsConfigured says whether the App has everything it needs to mint.
func (a App) IsConfigured() bool {
	return a.Cache.Identity.IsConfigured()
}

// At is the same App speaking to an enterprise host's API, or unchanged when api is empty.
func (a App) At(api string) App {
	if api != "" {
		a.Cache.Identity.API = api
	}

	return a
}

func (a App) client() *http.Client {
	if a.Client != nil {
		return a.Client
	}

	return xhttp.NewClient(appCallTimeout)
}

func (a App) now() time.Time {
	if a.Now != nil {
		return a.Now()
	}

	return time.Now()
}

// Slug is the name GitHub knows the App by.
func (a App) Slug(ctx context.Context) (string, error) {
	return a.Cache.Identity.Slug(ctx, a.client(), a.now())
}

// NewAppCredentialParams says which App to mint from and which row a refusal is recorded on.
type NewAppCredentialParams struct {
	App          App
	ConnectionID string
	Connections  verifiedSetter
}

// AppCredential is an installation token minted on demand and held until it nears expiry.
type AppCredential struct {
	app          App
	connectionID string
	connections  verifiedSetter
}

// NewAppCredential builds an Authenticator over the App.
func NewAppCredential(params NewAppCredentialParams) *AppCredential {
	return &AppCredential{
		app:          params.App,
		connectionID: params.ConnectionID,
		connections:  params.Connections,
	}
}

// Credential is the installation token, minted when the cached one is spent.
func (c *AppCredential) Credential(ctx context.Context) (Credential, error) {
	token, err := c.app.Cache.Fetch(ctx, c.app.client(), c.app.now())
	if errors.Is(err, githubapp.ErrRefused) {
		if invalidateErr := c.Invalidate(ctx); invalidateErr != nil {
			return Credential{}, ErrUnauthorized.Saying("%v; failed to record the refusal: %v", err, invalidateErr)
		}

		return Credential{}, ErrUnauthorized.Saying("%v", err)
	}
	if err != nil {
		return Credential{}, err
	}

	return Credential{
		Header:    HeaderBearer,
		Token:     NewSecret(token.Value),
		ExpiresAt: token.ExpiresAt,
	}, nil
}

// Invalidate drops the cached token so the next call mints, and clears the row's verified_at.
func (c *AppCredential) Invalidate(ctx context.Context) error {
	return errors.Join(c.app.Cache.Forget(), c.connections.SetVerifiedAt(ctx, c.connectionID, nil))
}

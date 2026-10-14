package engine

import (
	"context"
	"crypto/rand"
	"encoding/base64"
	"fmt"
	"net"
	"net/http"
	"slices"
	"time"

	"komrad/internal/fail"
	"komrad/lib/xhttp"

	"github.com/coreos/go-oidc/v3/oidc"
	"golang.org/x/oauth2"
)

// CallbackAddr is where the provider sends the browser back to once the person signs in, which
// the owner registers with the provider as http://127.0.0.1:8085.
const CallbackAddr = "127.0.0.1:8085"

// loginTimeout bounds how long `komrad login` waits for the person to finish in the browser.
const loginTimeout = 5 * time.Minute

// offlineScope asks a provider that lists it for a refresh token.
const offlineScope = "offline_access"

var (
	errLoginRefused   = fail.Denied("the provider refused the sign-in")
	errLoginMismatch  = fail.InvalidArgument("the browser came back from some other sign-in")
	errLoginNoCode    = fail.InvalidArgument("the browser came back without a code from the provider")
	errNoProvider     = fail.Precondition("the engine names no provider to sign in with")
	errLoginAbandoned = fail.Precondition("nobody finished signing in within five minutes")
)

// urlOpener shows the person a page.
type urlOpener interface {
	OpenURL(ctx context.Context, target string) error
}

// LoginParams is the engine to sign in to and how to reach the person.
type LoginParams struct {
	URL    string
	Keeper *SignInKeeper
	Opener urlOpener
	// Listen is where the provider sends the browser back to, CallbackAddr unless a test names another.
	Listen string
	// Prompt tells the person where to sign in, for when the browser does not open.
	Prompt func(authURL string)
}

// Login signs the person in to the engine at url through its provider in their browser, and
// keeps the sign-in.
func Login(ctx context.Context, params LoginParams) (*signIn, error) {
	ctx, cancel := context.WithTimeoutCause(ctx, loginTimeout, errLoginAbandoned)
	defer cancel()

	client := xhttp.NewClient(providerTimeout)

	var named SignInProvider
	answer, err := xhttp.JSON(ctx, xhttp.JSONParams{
		Client: client,
		Method: http.MethodGet,
		URL:    params.URL + SignInPath,
	}, &named)
	if err != nil {
		return nil, err
	}
	if !answer.IsSuccess() || named.Issuer == "" || named.ClientID == "" {
		return nil, fmt.Errorf("%w at %s, answering %d", errNoProvider, params.URL, answer.Status)
	}

	ctx = oidc.ClientContext(ctx, client)

	provider, err := oidc.NewProvider(ctx, named.Issuer)
	if err != nil {
		return nil, fmt.Errorf("failed to reach the identity provider at %s: %w", named.Issuer, err)
	}

	listener, err := (&net.ListenConfig{}).Listen(ctx, "tcp", params.Listen)
	if err != nil {
		return nil, fmt.Errorf("failed to listen for the provider's answer on %s, which another komrad login may hold: %w", params.Listen, err)
	}
	defer func() { _ = listener.Close() }() //nolint:errcheck // nothing is listened for once the login returns

	config := oauth2.Config{
		ClientID:     named.ClientID,
		ClientSecret: named.ClientSecret,
		Endpoint:     provider.Endpoint(),
		RedirectURL:  "http://" + listener.Addr().String(),
		Scopes:       []string{oidc.ScopeOpenID, "email", "profile"},
	}

	state, err := generateNonce()
	if err != nil {
		return nil, err
	}
	nonce, err := generateNonce()
	if err != nil {
		return nil, err
	}
	verifier := oauth2.GenerateVerifier()

	options := []oauth2.AuthCodeOption{oauth2.S256ChallengeOption(verifier), oidc.Nonce(nonce)}
	if isOfflineScopeSupported(provider) {
		config.Scopes = append(config.Scopes, offlineScope)
	} else {
		options = append(options, oauth2.AccessTypeOffline, oauth2.ApprovalForce)
	}

	authURL := config.AuthCodeURL(state, options...)

	code, err := awaitCode(ctx, awaitCodeParams{
		Listener: listener,
		State:    state,
		Ready: func() {
			params.Prompt(authURL)
			_ = params.Opener.OpenURL(ctx, authURL) //nolint:errcheck // the prompt printed the url for a browser that does not open
		},
	})
	if err != nil {
		return nil, err
	}

	token, err := config.Exchange(ctx, code, oauth2.VerifierOption(verifier))
	if err != nil {
		return nil, fmt.Errorf("failed to trade the provider's code for a token: %w", err)
	}

	idToken, err := verifyIDToken(ctx, verifyIDTokenParams{
		Provider: provider,
		ClientID: named.ClientID,
		Token:    token,
		Nonce:    nonce,
	})
	if err != nil {
		return nil, err
	}

	kept := signIn{
		Provider:     named,
		Email:        idToken.email,
		Subject:      idToken.subject,
		IDToken:      idToken.raw,
		RefreshToken: token.RefreshToken,
		Expiry:       idToken.expiry,
	}
	if err := params.Keeper.SaveSignIn(params.URL, kept); err != nil {
		return nil, err
	}

	return &kept, nil
}

type awaitCodeParams struct {
	Listener net.Listener
	State    string
	Ready    func()
}

// awaitCode serves the page the provider sends the browser back to, and answers with the code
// it carries.
func awaitCode(ctx context.Context, params awaitCodeParams) (string, error) {
	type result struct {
		code string
		err  error
	}

	results := make(chan result, 1)
	server := &http.Server{
		ReadHeaderTimeout: 5 * time.Second,
		Handler: http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			if r.URL.Path != "/" {
				http.NotFound(w, r)

				return
			}

			query := r.URL.Query()

			var outcome result
			switch {
			case query.Get("state") != params.State:
				outcome.err = errLoginMismatch
			case query.Get("error") != "":
				outcome.err = fmt.Errorf("%w: %s %s", errLoginRefused, query.Get("error"), query.Get("error_description"))
			case query.Get("code") == "":
				outcome.err = errLoginNoCode
			default:
				outcome.code = query.Get("code")
			}

			if outcome.err != nil {
				http.Error(w, outcome.err.Error(), http.StatusBadRequest)
			} else {
				_, _ = fmt.Fprintln(w, "Signed in to komrad. You can close this tab.") //nolint:errcheck // a browser that left has nothing to be told
			}

			select {
			case results <- outcome:
			default:
			}
		}),
	}

	go func() { _ = server.Serve(params.Listener) }() //nolint:errcheck // serving ends when the login returns and closes it
	defer func() { _ = server.Close() }()             //nolint:errcheck // the code is in hand or the wait is over

	params.Ready()

	select {
	case outcome := <-results:
		return outcome.code, outcome.err
	case <-ctx.Done():
		return "", context.Cause(ctx)
	}
}

func isOfflineScopeSupported(provider *oidc.Provider) bool {
	var discovery struct {
		ScopesSupported []string `json:"scopes_supported"`
	}
	if err := provider.Claims(&discovery); err != nil {
		return false
	}

	return slices.Contains(discovery.ScopesSupported, offlineScope)
}

func generateNonce() (string, error) {
	random := make([]byte, 24)
	if _, err := rand.Read(random); err != nil {
		return "", fmt.Errorf("failed to draw a random value for the sign-in: %w", err)
	}

	return base64.RawURLEncoding.EncodeToString(random), nil
}

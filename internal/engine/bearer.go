package engine

import (
	"context"
	"fmt"
	"net/http"
	"sync"
	"time"

	"komrad/internal/fail"
	"komrad/lib/xhttp"

	"github.com/coreos/go-oidc/v3/oidc"
	"golang.org/x/oauth2"
)

// refreshAhead is how long before an ID token runs out komrad trades the refresh token for a new
// one, so a request never leaves with a token about to lapse.
const refreshAhead = 5 * time.Minute

// providerTimeout bounds one request to the identity provider.
const providerTimeout = 30 * time.Second

var (
	errSignInLapsed        = ErrSignInNeeded.Saying("the sign-in ran out and the provider gave no way to renew it, so run komrad login")
	errNoIDToken           = fail.Internal("the provider answered without an ID token")
	errIDTokenMismatch     = fail.InvalidArgument("the ID token the provider gave answers some other sign-in")
	errProviderUnreachable = fail.Precondition("failed to reach the identity provider")
)

// NewTokenRefresher hands out a live ID token for the engine at url, renewing the sign-in keeper
// holds for it when the token is near its end.
func NewTokenRefresher(keeper *SignInKeeper, url string) *TokenRefresher {
	return &TokenRefresher{
		keeper: keeper,
		url:    url,
		client: xhttp.NewClient(providerTimeout),
	}
}

// TokenRefresher hands out a live ID token for one engine.
type TokenRefresher struct {
	keeper *SignInKeeper
	url    string
	client *http.Client
	mu     sync.Mutex
	signIn *signIn
}

// GetToken is an ID token for the engine that lasts at least refreshAhead longer, renewing the
// sign-in when the one held is nearer its end than that.
func (r *TokenRefresher) GetToken(ctx context.Context) (string, error) {
	r.mu.Lock()
	defer r.mu.Unlock()

	if r.signIn != nil && isFresh(r.signIn) {
		return r.signIn.IDToken, nil
	}

	kept, err := r.keeper.GetSignIn(r.url)
	if err != nil {
		return "", err
	}

	if !isFresh(kept) {
		kept, err = r.renew(ctx, *kept)
		if err != nil {
			return "", err
		}

		if err := r.keeper.SaveSignIn(r.url, *kept); err != nil {
			return "", err
		}
	}

	r.signIn = kept

	return kept.IDToken, nil
}

// Forget drops the sign-in held in memory, so the next token is read from the keeper again.
func (r *TokenRefresher) Forget() {
	r.mu.Lock()
	defer r.mu.Unlock()

	r.signIn = nil
}

// renew trades the sign-in's refresh token for a new ID token from the same provider.
func (r *TokenRefresher) renew(ctx context.Context, kept signIn) (*signIn, error) {
	if kept.RefreshToken == "" {
		return nil, errSignInLapsed.Saying("the sign-in to %s ran out and the provider gave no way to renew it, so run komrad login", r.url)
	}

	ctx = oidc.ClientContext(ctx, r.client)

	provider, err := oidc.NewProvider(ctx, kept.Provider.Issuer)
	if err != nil {
		return nil, errProviderUnreachable.Saying("failed to reach the identity provider at %s: %v", kept.Provider.Issuer, err)
	}

	config := oauth2.Config{
		ClientID:     kept.Provider.ClientID,
		ClientSecret: kept.Provider.ClientSecret,
		Endpoint:     provider.Endpoint(),
	}

	token, err := config.TokenSource(ctx, &oauth2.Token{RefreshToken: kept.RefreshToken}).Token()
	if err != nil {
		return nil, fmt.Errorf("%w: %w", errSignInLapsed.Saying("the provider would not renew the sign-in to %s, so run komrad login", r.url), err)
	}

	idToken, err := verifyIDToken(ctx, verifyIDTokenParams{
		Provider: provider,
		ClientID: kept.Provider.ClientID,
		Token:    token,
	})
	if err != nil {
		return nil, err
	}

	kept.IDToken = idToken.raw
	kept.Expiry = idToken.expiry
	if token.RefreshToken != "" {
		kept.RefreshToken = token.RefreshToken
	}

	return &kept, nil
}

type verifiedIDToken struct {
	raw     string
	expiry  time.Time
	email   string
	subject string
}

type verifyIDTokenParams struct {
	Provider *oidc.Provider
	ClientID string
	Token    *oauth2.Token
	Nonce    string
}

// verifyIDToken reads the ID token out of the provider's answer and checks it was signed for komrad.
func verifyIDToken(ctx context.Context, params verifyIDTokenParams) (*verifiedIDToken, error) {
	raw, ok := params.Token.Extra("id_token").(string)
	if !ok || raw == "" {
		return nil, errNoIDToken
	}

	verified, err := params.Provider.Verifier(&oidc.Config{ClientID: params.ClientID}).Verify(ctx, raw)
	if err != nil {
		return nil, fmt.Errorf("failed to verify the ID token the provider gave: %w", err)
	}

	if params.Nonce != "" && verified.Nonce != params.Nonce {
		return nil, errIDTokenMismatch
	}

	var claims struct {
		Email string `json:"email"`
	}
	if err := verified.Claims(&claims); err != nil {
		return nil, fmt.Errorf("failed to read the ID token the provider gave: %w", err)
	}

	return &verifiedIDToken{
		raw:     raw,
		expiry:  verified.Expiry,
		email:   claims.Email,
		subject: verified.Subject,
	}, nil
}

func isFresh(kept *signIn) bool {
	return time.Until(kept.Expiry) > refreshAhead
}

// NewBearerTransport sends every request with a live ID token from refresher, over next.
func NewBearerTransport(refresher *TokenRefresher, next http.RoundTripper) http.RoundTripper {
	return bearerTransport{refresher: refresher, next: next}
}

type bearerTransport struct {
	refresher *TokenRefresher
	next      http.RoundTripper
}

// RoundTrip sends a copy of the request carrying the token.
func (t bearerTransport) RoundTrip(req *http.Request) (*http.Response, error) {
	token, err := t.refresher.GetToken(req.Context())
	if err != nil {
		if req.Body != nil {
			_ = req.Body.Close()
		}

		return nil, err
	}

	signed := req.Clone(req.Context())
	signed.Header.Set("Authorization", "Bearer "+token)

	return t.next.RoundTrip(signed)
}

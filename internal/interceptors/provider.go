package interceptors

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"sync"
	"time"

	"komrad/internal/caller"
	"komrad/internal/rpcerr"
	"komrad/lib/xhttp"

	"github.com/coreos/go-oidc/v3/oidc"
)

// providerTimeout bounds one request to the identity provider for its discovery document or keys.
const providerTimeout = 10 * time.Second

var (
	errProviderUnreachable = errors.New("failed to reach the identity provider")
	errTokenRejected       = errors.New("the token is not one the identity provider signed for komrad")
	errNoSubject           = errors.New("the token names nobody")
)

// NewProviderVerifierParams names the provider an engine trusts and komrad's id there.
type NewProviderVerifierParams struct {
	Issuer   string
	ClientID string
}

// ProviderVerifier is a TokenVerifier over one OIDC provider.
type ProviderVerifier struct {
	client   *http.Client
	issuer   string
	clientID string
	mu       sync.Mutex
	verifier *oidc.IDTokenVerifier
}

// NewProviderVerifier checks ID tokens against the provider's published keys, looking the provider
// up the first time a token arrives so the engine starts while the provider is down.
func NewProviderVerifier(params NewProviderVerifierParams) *ProviderVerifier {
	return &ProviderVerifier{
		client:   xhttp.NewClient(providerTimeout),
		issuer:   params.Issuer,
		clientID: params.ClientID,
	}
}

// VerifyToken checks the token's signature, issuer, audience and expiry, and reads who it names.
func (p *ProviderVerifier) VerifyToken(ctx context.Context, raw string) (*signedIn, error) {
	verifier, err := p.getVerifier(ctx)
	if err != nil {
		return nil, rpcerr.Unavailable(err)
	}

	token, err := verifier.Verify(ctx, raw)
	if err != nil {
		return nil, rpcerr.Unauthenticated(fmt.Errorf("%w: %w", errTokenRejected, err))
	}

	var claims struct {
		Email         string        `json:"email"`
		EmailVerified emailVerified `json:"email_verified"`
	}
	if err := token.Claims(&claims); err != nil {
		return nil, rpcerr.Unauthenticated(fmt.Errorf("%w: %w", errTokenRejected, err))
	}

	if token.Subject == "" {
		return nil, rpcerr.Unauthenticated(errNoSubject)
	}

	return &signedIn{
		Caller: caller.Caller{
			Email:   claims.Email,
			Issuer:  token.Issuer,
			Subject: token.Subject,
		},
		EmailVerified: bool(claims.EmailVerified),
		Expiry:        token.Expiry,
	}, nil
}

// getVerifier looks the provider up once it answers, and again on every token until it does.
func (p *ProviderVerifier) getVerifier(ctx context.Context) (*oidc.IDTokenVerifier, error) {
	p.mu.Lock()
	defer p.mu.Unlock()

	if p.verifier != nil {
		return p.verifier, nil
	}

	provider, err := oidc.NewProvider(oidc.ClientContext(ctx, p.client), p.issuer)
	if err != nil {
		return nil, fmt.Errorf("%w at %s: %w", errProviderUnreachable, p.issuer, err)
	}

	p.verifier = provider.Verifier(&oidc.Config{
		ClientID:             p.clientID,
		SupportedSigningAlgs: []string{oidc.RS256, oidc.ES256},
	})

	return p.verifier, nil
}

// emailVerified reads the claim as a boolean, or as the string some providers send instead.
type emailVerified bool

// UnmarshalJSON takes true, false, "true" or "false".
func (e *emailVerified) UnmarshalJSON(data []byte) error {
	var flag bool
	if err := json.Unmarshal(data, &flag); err == nil {
		*e = emailVerified(flag)

		return nil
	}

	var text string
	if err := json.Unmarshal(data, &text); err != nil {
		return fmt.Errorf("failed to read email_verified: %w", err)
	}

	*e = emailVerified(text == "true")

	return nil
}

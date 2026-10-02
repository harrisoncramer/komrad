package gitlab

import (
	"context"
	"fmt"
	"net/http"
	"time"

	"komrad/internal/forge"
	"komrad/lib/xhttp"
)

// tokenDateLayout is how GitLab writes a token's expiry, a day that ends at midnight UTC.
const tokenDateLayout = "2006-01-02"

// personalToken is GitLab's account of a personal access token.
type personalToken struct {
	Scopes    []string `json:"scopes"`
	ExpiresAt string   `json:"expires_at"`
	Token     string   `json:"token"`
}

// ReadToken is what GitLab says of the token it was shown.
func (f *Forge) ReadToken(ctx context.Context) (*forge.TokenInfo, error) {
	var token personalToken
	answer, err := f.client.Send(ctx, xhttp.JSONParams{
		Method: http.MethodGet,
		URL:    f.selfTokenURL(),
	}, &token)
	if err != nil {
		return nil, err
	}
	if answer.Status == http.StatusNotFound {
		return nil, noSelfToken()
	}

	return token.info()
}

// RotateToken trades the token GitLab was shown for a new one that expires on the day given,
// which revokes the old one at once.
func (f *Forge) RotateToken(ctx context.Context, expiresAt time.Time) (forge.Secret, error) {
	var token personalToken
	answer, err := f.client.Send(ctx, xhttp.JSONParams{
		Method: http.MethodPost,
		URL:    f.selfTokenURL() + "/rotate",
		Body:   map[string]string{"expires_at": expiresAt.UTC().Format(tokenDateLayout)},
	}, &token)
	if err != nil {
		return forge.Secret{}, err
	}
	if answer.Status == http.StatusNotFound {
		return forge.Secret{}, noSelfToken()
	}

	secret := forge.NewSecret(token.Token)
	if secret.IsZero() {
		return forge.Secret{}, fmt.Errorf("gitlab rotated the token but answered no new one")
	}

	return secret, nil
}

// RevokeToken ends the token GitLab was shown.
func (f *Forge) RevokeToken(ctx context.Context) error {
	answer, err := f.client.Send(ctx, xhttp.JSONParams{
		Method: http.MethodDelete,
		URL:    f.selfTokenURL(),
	}, nil)
	if err != nil {
		return err
	}
	if answer.Status == http.StatusNotFound {
		return noSelfToken()
	}

	return nil
}

// selfTokenURL is where GitLab answers for the token a call is made with.
func (f *Forge) selfTokenURL() string {
	return f.client.API() + "/personal_access_tokens/self"
}

// info is the token in komrad's terms.
func (t personalToken) info() (*forge.TokenInfo, error) {
	info := &forge.TokenInfo{Scopes: t.Scopes}
	if t.ExpiresAt == "" {
		return info, nil
	}

	expiresAt, err := time.Parse(tokenDateLayout, t.ExpiresAt)
	if err != nil {
		return nil, fmt.Errorf("failed to read the token's expiry %q: %w", t.ExpiresAt, err)
	}
	info.ExpiresAt = expiresAt

	return info, nil
}

// noSelfToken says GitLab has no personal token behind the credential, which is also its answer
// for an instance too old to describe one.
func noSelfToken() error {
	return fmt.Errorf("%w: gitlab has no personal access token behind the credential", forge.ErrNotFound)
}

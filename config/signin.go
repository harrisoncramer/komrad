package config

import (
	"errors"
	"fmt"
	"net/url"
	"strings"
)

// defaultSignInAddr is the public listener's address beside the default api address.
const defaultSignInAddr = "127.0.0.1:8788"

// SignIn is the identity provider people sign in to an engine with, and the listener their
// requests reach it on.
type SignIn struct {
	// The provider's issuer, like https://accounts.google.com, which every ID token it signs
	// names. Left empty, along with client_id, the engine doesn't take signed-in requests.
	Issuer string `mapstructure:"issuer"`
	// The id the owner registered komrad under at the provider, which every ID token for komrad
	// names as its audience.
	ClientID string `mapstructure:"client_id"`
	// The secret some providers, such as Google, hand a desktop app beside its id. Every copy of
	// komrad carries it, so it protects nothing, and `komrad login` reads it from the engine.
	ClientSecret string `mapstructure:"client_secret"`
	// The address the public listener serves signed-in requests on. It has to be a loopback
	// host, since a web server on the same machine carries the network's requests to it.
	Addr string `mapstructure:"addr"`
}

// IsConfigured reports whether the engine names a provider to check signed-in requests against.
func (s SignIn) IsConfigured() bool {
	return s.Issuer != "" && s.ClientID != ""
}

func (s *SignIn) normalize() {
	s.Issuer = strings.TrimSpace(s.Issuer)
	s.ClientID = strings.TrimSpace(s.ClientID)
	if strings.TrimSpace(s.Addr) == "" {
		s.Addr = defaultSignInAddr
	}
}

// validate rejects a provider named by half, an issuer komrad could not fetch keys from safely,
// and a listener the network could reach directly.
func (s SignIn) validate() error {
	if (s.Issuer == "") != (s.ClientID == "") {
		return errors.New("sign_in: issuer and client_id are set together, and one of them is empty")
	}

	if err := ValidateAddr(s.Addr); err != nil {
		return fmt.Errorf("sign_in.addr: %w", err)
	}

	if s.Issuer == "" {
		return nil
	}

	if err := requireSecureURL(s.Issuer); err != nil {
		return fmt.Errorf("sign_in.issuer: %w", err)
	}

	return nil
}

// requireSecureURL rejects a url that is not https, unless its host is this machine.
func requireSecureURL(raw string) error {
	parsed, err := url.Parse(raw)
	if err != nil {
		return fmt.Errorf("%q is not a url: %w", raw, err)
	}
	if parsed.Host == "" {
		return fmt.Errorf("%q names no host", raw)
	}
	if parsed.Scheme == "https" {
		return nil
	}
	if parsed.Scheme == "http" && isLoopbackHost(parsed.Hostname()) {
		return nil
	}

	return fmt.Errorf("%q is not https, and only this machine is reached over plain http", raw)
}

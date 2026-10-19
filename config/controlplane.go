package config

import (
	"errors"
	"fmt"
	"net"
	"net/url"
	"strconv"
	"strings"
)

const (
	defaultControlPlaneAddr = "127.0.0.1:8790"
	defaultForgeProxyAddr   = "127.0.0.1:8789"
)

// ControlPlane is the process in front of a team's engines, which holds people's GitHub logins
// and runs the forge proxy.
type ControlPlane struct {
	// The address the control plane's own endpoints listen on. It has to be a loopback host,
	// since a web server on the same machine carries the network's requests to it.
	Addr string `mapstructure:"addr"`
	// The address engines reach the forge proxy on, a loopback or private address. The forge
	// proxy speaks plain HTTP to engines and checks each one's forge proxy credential.
	ForgeProxyAddr string `mapstructure:"forge_proxy_addr"`
	// The PEM file holding the CA certificate the forge proxy signs GitHub's names with. Each
	// engine trusts it through forge_proxy.ca_file.
	CACertFile string `mapstructure:"ca_cert_file"`
	// The PEM file holding the CA's private key.
	CAKeyFile string `mapstructure:"ca_key_file"`
	// The client id of the GitHub OAuth app people authorize GitHub through.
	GitHubClientID string `mapstructure:"github_client_id"`
	// Each engine the forge proxy takes requests from, written as "name=credential", such as
	// "harry=6f1c...". The engine's forge_proxy.url carries the same name and credential.
	Engines []string `mapstructure:"engines"`
}

// GetEngines is each engine's forge proxy credential keyed by the engine's name.
func (c ControlPlane) GetEngines() (map[string]string, error) {
	engines := make(map[string]string, len(c.Engines))
	for _, entry := range c.Engines {
		name, credential, ok := strings.Cut(entry, "=")
		name, credential = strings.TrimSpace(name), strings.TrimSpace(credential)
		if !ok || name == "" || credential == "" || strings.ContainsAny(name, ":/ \t") {
			return nil, fmt.Errorf("an entry is not written as name=credential with a bare engine name")
		}
		if _, seen := engines[name]; seen {
			return nil, fmt.Errorf("%q is named a second time", name)
		}

		engines[name] = credential
	}

	return engines, nil
}

func (c *ControlPlane) normalize() {
	if strings.TrimSpace(c.Addr) == "" {
		c.Addr = defaultControlPlaneAddr
	}
	if strings.TrimSpace(c.ForgeProxyAddr) == "" {
		c.ForgeProxyAddr = defaultForgeProxyAddr
	}
}

func (c ControlPlane) validate() error {
	if err := ValidateAddr(c.Addr); err != nil {
		return fmt.Errorf("control_plane.addr: %w", err)
	}
	if err := validatePrivateAddr(c.ForgeProxyAddr); err != nil {
		return fmt.Errorf("control_plane.forge_proxy_addr: %w", err)
	}
	if (c.CACertFile == "") != (c.CAKeyFile == "") {
		return errors.New("control_plane: ca_cert_file and ca_key_file are set together, and one of them is empty")
	}
	if _, err := c.GetEngines(); err != nil {
		return fmt.Errorf("control_plane.engines: %w", err)
	}

	return nil
}

// validatePrivateAddr rejects an address the internet could reach.
func validatePrivateAddr(addr string) error {
	host, port, err := net.SplitHostPort(strings.TrimSpace(addr))
	if err != nil {
		return fmt.Errorf("%q is not a host and port: %w", addr, err)
	}
	if number, err := strconv.Atoi(port); err != nil || number < 1 || number > 65535 {
		return fmt.Errorf("%q is not a port", port)
	}
	if isLoopbackHost(host) {
		return nil
	}

	ip := net.ParseIP(host)
	if ip == nil || !ip.IsPrivate() {
		return fmt.Errorf("%q is not a loopback or private address", host)
	}

	return nil
}

// ForgeProxy is the forge proxy a remote engine sends its GitHub requests through.
type ForgeProxy struct {
	// The forge proxy's address, with this engine's name and forge proxy credential as its user
	// and password, such as http://harry:6f1c...@10.0.1.5:8789. Left empty, the engine reaches
	// GitHub directly with its own credentials.
	URL string `mapstructure:"url"`
	// The PEM file holding the control plane's CA certificate, which the forge proxy's
	// certificate for GitHub's names is signed with.
	CAFile string `mapstructure:"ca_file"`
}

// IsConfigured reports whether the engine sends its GitHub requests through a forge proxy.
func (f ForgeProxy) IsConfigured() bool {
	return f.URL != ""
}

func (f *ForgeProxy) normalize() {
	f.URL = strings.TrimSpace(f.URL)
	f.CAFile = strings.TrimSpace(f.CAFile)
}

func (f ForgeProxy) validate() error {
	if f.URL == "" {
		if f.CAFile != "" {
			return errors.New("forge_proxy: ca_file is set and url is empty")
		}

		return nil
	}

	parsed, err := url.Parse(f.URL)
	if err != nil {
		return errors.New("forge_proxy.url: is not a url")
	}
	if parsed.Scheme != "http" || parsed.Host == "" {
		return errors.New("forge_proxy.url: is not an http address with a host")
	}
	if parsed.User == nil || parsed.User.Username() == "" {
		return errors.New("forge_proxy.url: names no engine as its user")
	}
	if password, _ := parsed.User.Password(); password == "" {
		return errors.New("forge_proxy.url: carries no forge proxy credential as its password")
	}
	if f.CAFile == "" {
		return errors.New("forge_proxy: url is set and ca_file is empty")
	}

	return nil
}

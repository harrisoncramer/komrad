package config

import (
	"fmt"
	"strings"
)

// defaultMaxCheckLogBytes is the largest check log komrad keeps when the config names no size, set
// at more than three times the largest log sampled across real repositories.
const defaultMaxCheckLogBytes = 2 << 20

// Git is how komrad reads the checkouts its agents work in.
type Git struct {
	// SSH hosts that stand in for a forge's web host, each written as "ssh_host=web_host", such
	// as "ssh.gitlab.example.com=gitlab.example.com". A remote on the SSH host is then matched to
	// the forge connected for the web host. Write host names only, with no port, scheme or
	// user. Public aliases such as ssh.github.com and altssh.gitlab.com are already known.
	SSHHosts []string `mapstructure:"ssh_hosts"`
}

// GetSSHHosts is the SSH hosts keyed to the web host each stands in for, lowercased.
func (g Git) GetSSHHosts() (map[string]string, error) {
	hosts := make(map[string]string, len(g.SSHHosts))
	for _, entry := range g.SSHHosts {
		ssh, web, ok := strings.Cut(entry, "=")
		ssh, web = strings.ToLower(strings.TrimSpace(ssh)), strings.ToLower(strings.TrimSpace(web))
		if !ok || !isBareHost(ssh) || !isBareHost(web) {
			return nil, fmt.Errorf("%q is not written as ssh_host=web_host, with host names only", entry)
		}
		if _, seen := hosts[ssh]; seen {
			return nil, fmt.Errorf("%q names %s a second time", entry, ssh)
		}

		hosts[ssh] = web
	}

	return hosts, nil
}

func (g Git) validate() error {
	if _, err := g.GetSSHHosts(); err != nil {
		return fmt.Errorf("git.ssh_hosts: %w", err)
	}

	return nil
}

// Forge is how komrad reads the review threads on the pull requests its agents open.
type Forge struct {
	// Logins whose review comments are left out of the threads mirrored onto an agent, such as a
	// CI bot or a code review App. A thread whose every comment is from one of them is not mirrored
	// at all. Names ignore case.
	BotNames []string `mapstructure:"bot_names"`
	// The largest check log komrad keeps, in bytes. Komrad drops a log past it, and the panel links to it
	// on the forge instead. Komrad refuses a negative size.
	MaxCheckLogBytes int64 `mapstructure:"max_check_log_bytes"`
}

// GetMaxCheckLogBytes is the largest check log komrad keeps. An unset size takes the default, and a
// negative one is an error rather than the default.
func (f Forge) GetMaxCheckLogBytes() (int64, error) {
	if f.MaxCheckLogBytes == 0 {
		f.MaxCheckLogBytes = defaultMaxCheckLogBytes
	}
	if f.MaxCheckLogBytes < 0 {
		return 0, fmt.Errorf("the check log size limit must be positive, not %d", f.MaxCheckLogBytes)
	}

	return f.MaxCheckLogBytes, nil
}

// isBareHost says whether a host carries no port, scheme, user or path.
func isBareHost(host string) bool {
	return host != "" && !strings.ContainsAny(host, ":/@= \t")
}

// GitHubApp is the App an engine fetches and pushes as.
type GitHubApp struct {
	// The App's own id, from its settings page. Left empty the engine has no GitHub identity
	// and git is left to whatever credentials the account already has, which on a machine
	// nobody logs into is none.
	AppID string `mapstructure:"app_id"`
	// The installation the token is for, which scopes it to the repositories the App was
	// installed on rather than to everything it could reach. GitHub writes it as a number, and
	// it appears in the URL of the installation's own settings page.
	InstallationID string `mapstructure:"installation_id"`
	// The PEM the App was created with. On a deployed engine, komrad fetches this into a tmpfs at
	// boot rather than writing it to disk, so the key does not outlive the machine.
	PrivateKeyPath string `mapstructure:"private_key_path"`
}

package cli

import (
	"bytes"
	"context"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"komrad/config"
	"komrad/lib/xhttp"
	"komrad/proto/gen/terminal/v1/terminalv1connect"

	"connectrpc.com/connect"
	"google.golang.org/protobuf/types/known/emptypb"
)

// sshIncludeLine is the line ~/.ssh/config needs for programs besides the panel to reach engines
// by the hosts komrad login writes.
const sshIncludeLine = "Include ~/.ssh/komrad/*.conf"

// sshHost is the ssh host that reaches one engine, and the files that describe it to ssh.
type sshHost struct {
	Name       string
	Config     string
	KnownHosts string
}

func sshHostOf(stack config.Stack) (*sshHost, error) {
	home, err := os.UserHomeDir()
	if err != nil {
		return nil, fmt.Errorf("failed to find the home directory: %w", err)
	}

	name := "komrad"
	if stack.Engine != "" {
		name += "-" + stack.Engine
	}

	dir := filepath.Join(home, ".ssh", "komrad")

	return &sshHost{
		Name:       name,
		Config:     filepath.Join(dir, name+".conf"),
		KnownHosts: filepath.Join(dir, name+".known_hosts"),
	}, nil
}

type writeSSHHostParams struct {
	Client terminalv1connect.TerminalServiceClient
	Host   sshHost
	Proxy  []string
}

// writeSSHHost writes the host that reaches the engine through komrad ssh-proxy, trusting the host
// key the engine gives over its signed-in api.
func writeSSHHost(ctx context.Context, params writeSSHHostParams) error {
	res, err := params.Client.GetSSHHostKey(ctx, connect.NewRequest(&emptypb.Empty{}))
	if err != nil {
		return fmt.Errorf("failed to read the engine's SSH host key: %w", err)
	}

	host := params.Host
	if err := os.MkdirAll(filepath.Dir(host.Config), 0o700); err != nil {
		return fmt.Errorf("failed to make the directory for komrad's ssh hosts: %w", err)
	}

	knownHosts := host.Name + " " + res.Msg.GetPublicKey() + "\n"
	if err := os.WriteFile(host.KnownHosts, []byte(knownHosts), 0o600); err != nil {
		return fmt.Errorf("failed to write the engine's SSH host key: %w", err)
	}

	quoted := make([]string, len(params.Proxy))
	for i, word := range params.Proxy {
		quoted[i] = shellQuote(word)
	}

	entry := fmt.Sprintf("Host %s\n  ProxyCommand %s ssh-proxy\n  HostKeyAlias %s\n  UserKnownHostsFile \"%s\"\n  StrictHostKeyChecking yes\n",
		host.Name, strings.Join(quoted, " "), host.Name, host.KnownHosts)
	if err := os.WriteFile(host.Config, []byte(entry), 0o600); err != nil {
		return fmt.Errorf("failed to write the engine's ssh host: %w", err)
	}

	return nil
}

// isSSHIncluded reports whether ~/.ssh/config already reads the hosts komrad login writes.
func isSSHIncluded() bool {
	home, err := os.UserHomeDir()
	if err != nil {
		return false
	}

	raw, err := os.ReadFile(filepath.Join(home, ".ssh", "config")) //nolint:gosec // the person's own ssh config
	if err != nil {
		return false
	}

	return bytes.Contains(raw, []byte(sshIncludeLine))
}

func shellQuote(value string) string {
	return "'" + strings.ReplaceAll(value, "'", `'\''`) + "'"
}

// setUpSSH writes the ssh host for the engine this process signs in to.
func (a *app) setUpSSH(ctx context.Context) error {
	host, err := sshHostOf(a.configStack)
	if err != nil {
		return err
	}

	return writeSSHHost(ctx, writeSSHHostParams{
		Client: terminalv1connect.NewTerminalServiceClient(xhttp.NewClientOver(a.signTransport(xhttp.NewTransport()), 0), a.getAPIBase()),
		Host:   *host,
		Proxy:  append([]string{a.selfPath()}, configArgs(a.configStack)...),
	})
}

package git

import (
	"fmt"
	"net/url"
	"strings"

	"weave/internal/fail"
)

// ErrNotForgeRemote says a remote names no repository on a forge.
var ErrNotForgeRemote = fail.InvalidArgument("remote is not a forge repository")

// RemoteURL is a git remote taken apart into the pieces a forge repository is named by.
type RemoteURL struct {
	// Raw is the remote exactly as git had it.
	Raw string
	// Scheme is the transport the remote fetches over: ssh, git, http or https.
	Scheme string
	// Host is the lowercased host with no port.
	Host string
	// Port is the port as spelled, or empty when the remote named none.
	Port string
	// Path is the repository path with its .git suffix and surrounding slashes removed.
	Path string
}

// Canonical is the one web address every spelling of the repository shares.
func (r RemoteURL) Canonical() string {
	host := r.Host
	if strings.Contains(host, ":") {
		host = "[" + host + "]"
	}
	if r.Port != "" && (r.Scheme == "http" || r.Scheme == "https") {
		host += ":" + r.Port
	}

	return "https://" + host + "/" + r.Path
}

// ParseRemoteURL takes a git remote apart, refusing one that names no forge repository.
func ParseRemoteURL(raw string) (RemoteURL, error) {
	remote := RemoteURL{Raw: raw}

	trimmed := strings.TrimSpace(raw)
	if trimmed == "" || strings.ContainsRune(trimmed, '\\') {
		return RemoteURL{}, ErrNotForgeRemote
	}

	if strings.Contains(trimmed, "://") {
		parsed, err := url.Parse(trimmed)
		if err != nil {
			return RemoteURL{}, fmt.Errorf("%w: %w", ErrNotForgeRemote, err)
		}

		switch strings.ToLower(parsed.Scheme) {
		case "http", "https", "ssh", "git":
			remote.Scheme = strings.ToLower(parsed.Scheme)
		case "git+https":
			remote.Scheme = "https"
		case "git+ssh":
			remote.Scheme = "ssh"
		default:
			return RemoteURL{}, fmt.Errorf("%w: scheme %q", ErrNotForgeRemote, parsed.Scheme)
		}

		remote.Host = parsed.Hostname()
		remote.Port = parsed.Port()
		remote.Path = parsed.EscapedPath()
	} else {
		before, after, ok := strings.Cut(trimmed, ":")
		if !ok || strings.Contains(before, "/") {
			return RemoteURL{}, ErrNotForgeRemote
		}
		if at := strings.LastIndex(before, "@"); at >= 0 {
			before = before[at+1:]
		}
		if len(before) < 2 || strings.ContainsAny(before, "[]") {
			return RemoteURL{}, ErrNotForgeRemote
		}

		remote.Scheme = "ssh"
		remote.Host = before
		remote.Path = after
	}

	if remote.Host == "" {
		return RemoteURL{}, fmt.Errorf("%w: no host", ErrNotForgeRemote)
	}

	remote.Host = strings.ToLower(remote.Host)
	if remote.Host == "www.github.com" {
		remote.Host = "github.com"
	}

	path := strings.Trim(remote.Path, "/")
	path = strings.TrimSuffix(path, ".git")
	path = strings.TrimRight(path, "/")
	if strings.HasPrefix(path, "~") {
		return RemoteURL{}, fmt.Errorf("%w: home-relative path", ErrNotForgeRemote)
	}

	segments := strings.Split(path, "/")
	if len(segments) < 2 {
		return RemoteURL{}, fmt.Errorf("%w: path %q has fewer than two segments", ErrNotForgeRemote, path)
	}
	for _, segment := range segments {
		if segment == "" {
			return RemoteURL{}, fmt.Errorf("%w: path %q has an empty segment", ErrNotForgeRemote, path)
		}
	}
	remote.Path = path

	return remote, nil
}

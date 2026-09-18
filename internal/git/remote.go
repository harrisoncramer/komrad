package git

import (
	"net/url"
	"strings"
)

// CanonicalRemoteURL is the one web address every spelling of a forge repository's remote
// shares, and false for a remote that names no forge repository.
func CanonicalRemoteURL(raw string) (string, bool) {
	raw = strings.TrimSpace(raw)
	if raw == "" || strings.ContainsRune(raw, '\\') {
		return "", false
	}

	var host, port, path string
	var keepPort bool
	if strings.Contains(raw, "://") {
		parsed, err := url.Parse(raw)
		if err != nil {
			return "", false
		}

		switch strings.ToLower(parsed.Scheme) {
		case "http", "https", "git+https":
			keepPort = true
		case "ssh", "git", "git+ssh":
		default:
			return "", false
		}

		host = parsed.Hostname()
		port = parsed.Port()
		path = parsed.EscapedPath()
	} else {
		before, after, ok := strings.Cut(raw, ":")
		if !ok || strings.Contains(before, "/") {
			return "", false
		}
		if at := strings.LastIndex(before, "@"); at >= 0 {
			before = before[at+1:]
		}
		if len(before) < 2 || strings.ContainsAny(before, "[]") {
			return "", false
		}

		host = before
		path = after
	}

	if host == "" {
		return "", false
	}

	host = strings.ToLower(host)
	if host == "www.github.com" {
		host = "github.com"
	}
	if strings.Contains(host, ":") {
		host = "[" + host + "]"
	}
	if keepPort && port != "" {
		host += ":" + port
	}

	path = strings.Trim(path, "/")
	path = strings.TrimSuffix(path, ".git")
	path = strings.TrimRight(path, "/")
	if strings.HasPrefix(path, "~") {
		return "", false
	}

	segments := strings.Split(path, "/")
	if len(segments) < 2 {
		return "", false
	}
	for _, segment := range segments {
		if segment == "" {
			return "", false
		}
	}

	return "https://" + host + "/" + path, true
}

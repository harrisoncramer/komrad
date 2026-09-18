package git

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// TestParseRemoteURL covers every spelling of a remote that names one repository, and
// every shape of remote that names none.
func TestParseRemoteURL(t *testing.T) {
	t.Parallel()

	const weave = "https://github.com/harrisoncramer/weave"

	tests := []struct {
		name      string
		raw       string
		canonical string
		refused   bool
	}{
		{name: "scp-like with a .git suffix", raw: "git@github.com:harrisoncramer/weave.git", canonical: weave},
		{name: "scp-like without a .git suffix", raw: "git@github.com:harrisoncramer/weave", canonical: weave},
		{name: "ssh scheme becomes https", raw: "ssh://git@github.com/harrisoncramer/weave.git", canonical: weave},
		{name: "ssh drops its port", raw: "ssh://git@github.com:22/harrisoncramer/weave.git", canonical: weave},
		{name: "ssh without a user", raw: "ssh://github.com/harrisoncramer/weave.git", canonical: weave},
		{name: "git+ssh unwraps to https", raw: "git+ssh://git@github.com/harrisoncramer/weave.git", canonical: weave},
		{name: "git scheme becomes https", raw: "git://github.com/harrisoncramer/weave.git", canonical: weave},
		{name: "git drops its port", raw: "git://github.com:9418/harrisoncramer/weave.git", canonical: weave},
		{name: "https is already canonical", raw: "https://github.com/harrisoncramer/weave", canonical: weave},
		{name: "https drops a .git suffix", raw: "https://github.com/harrisoncramer/weave.git", canonical: weave},
		{name: "https drops a trailing slash", raw: "https://github.com/harrisoncramer/weave/", canonical: weave},
		{name: "host is lowercased", raw: "https://GitHub.com/harrisoncramer/weave", canonical: weave},
		{name: "www.github.com is github.com", raw: "https://www.github.com/harrisoncramer/weave.git", canonical: weave},
		{name: "a user is dropped", raw: "https://harrisoncramer@github.com/harrisoncramer/weave.git", canonical: weave},
		{name: "a user and token are dropped", raw: "https://user:token@github.com/harrisoncramer/weave.git", canonical: weave},
		{name: "query and fragment are dropped", raw: "https://github.com/harrisoncramer/weave.git?foo=bar#frag", canonical: weave},
		{name: "git+https unwraps to https", raw: "git+https://github.com/harrisoncramer/weave.git", canonical: weave},
		{name: "http becomes https", raw: "http://github.com/harrisoncramer/weave.git", canonical: weave},
		{name: "surrounding whitespace is trimmed", raw: "  git@github.com:harrisoncramer/weave.git\n", canonical: weave},
		{name: "scheme case does not matter", raw: "HTTPS://github.com/harrisoncramer/weave", canonical: weave},
		{name: "scp-like user may be anything", raw: "deploy@github.com:harrisoncramer/weave.git", canonical: weave},
		{name: "scp-like without a user", raw: "github.com:harrisoncramer/weave.git", canonical: weave},
		{name: "path case is kept", raw: "https://github.com/HarrisonCramer/Weave", canonical: "https://github.com/HarrisonCramer/Weave"},
		{name: "a nested path is kept whole", raw: "https://gitlab.com/group/subgroup/repo.git", canonical: "https://gitlab.com/group/subgroup/repo"},
		{name: "an https port is kept", raw: "https://ghe.example.com:8443/owner/repo", canonical: "https://ghe.example.com:8443/owner/repo"},
		{name: "an http port is kept", raw: "http://ghe.example.com:8080/owner/repo.git", canonical: "https://ghe.example.com:8080/owner/repo"},
		{name: "an ssh port on another host is dropped", raw: "ssh://git@ghe.example.com:2222/owner/repo.git", canonical: "https://ghe.example.com/owner/repo"},
		{name: "www is kept on any other host", raw: "https://www.ghe.example.com/owner/repo", canonical: "https://www.ghe.example.com/owner/repo"},
		{name: "scp-like has no port so the number is a path segment", raw: "git@github.com:9999/user/repo.git", canonical: "https://github.com/9999/user/repo"},
		{name: "an ipv6 host keeps its brackets and http port", raw: "http://[::1]:8080/owner/repo.git", canonical: "https://[::1]:8080/owner/repo"},
		{name: "an encoded slash stays encoded", raw: "https://github.com/owner/repo%2Fsub", canonical: "https://github.com/owner/repo%2Fsub"},
		{name: "an encoded space stays encoded", raw: "https://github.com/owner/re%20po", canonical: "https://github.com/owner/re%20po"},
		{name: ".git is removed only once", raw: "https://github.com/owner/repo.git.git", canonical: "https://github.com/owner/repo.git"},
		{name: "empty is not a repository", raw: "", refused: true},
		{name: "whitespace is not a repository", raw: "   ", refused: true},
		{name: "an absolute local path is not a repository", raw: "/Users/harrisoncramer/weave", refused: true},
		{name: "a relative local path is not a repository", raw: "./repo", refused: true},
		{name: "a local path with a colon after a slash is not scp-like", raw: "./foo:bar", refused: true},
		{name: "file scheme is not a repository", raw: "file:///path/to/repo.git", refused: true},
		{name: "a windows drive path is not scp-like", raw: `C:\repos\weave`, refused: true},
		{name: "a forward-slash windows drive path is not scp-like", raw: "C:/repos/weave", refused: true},
		{name: "a bracketed ipv6 scp-like host is refused", raw: "git@[::1]:owner/repo.git", refused: true},
		{name: "a backslash anywhere is refused", raw: `https://github.com/owner\repo`, refused: true},
		{name: "a tilde path is not a repository", raw: "git@host:~user/repo.git", refused: true},
		{name: "a tilde path over ssh is not a repository", raw: "ssh://git@host/~user/repo.git", refused: true},
		{name: "a host with no path is not a repository", raw: "https://github.com", refused: true},
		{name: "a host with only a slash is not a repository", raw: "https://github.com/", refused: true},
		{name: "a one-segment path is not a repository", raw: "https://github.com/harrisoncramer", refused: true},
		{name: "a one-segment scp-like path is not a repository", raw: "git@github.com:harrisoncramer.git", refused: true},
		{name: "an empty segment is not a repository", raw: "https://github.com/owner//repo", refused: true},
		{name: "scp-like with no host is not a repository", raw: "git@:owner/repo", refused: true},
		{name: "ftp is not a scheme git uses", raw: "ftp://github.com/harrisoncramer/weave.git", refused: true},
		{name: "a scheme with no host is not a repository", raw: "https:///harrisoncramer/weave", refused: true},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			got, err := ParseRemoteURL(tt.raw)
			if tt.refused {
				require.ErrorIs(t, err, ErrNotForgeRemote)
				assert.Equal(t, RemoteURL{}, got)

				return
			}

			require.NoError(t, err)
			assert.Equal(t, tt.raw, got.Raw)
			assert.Equal(t, tt.canonical, got.Canonical())
		})
	}
}

// TestParseRemoteURLPieces covers what a remote is taken apart into, which is what a caller reads
// instead of parsing the string again.
func TestParseRemoteURLPieces(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name string
		raw  string
		want RemoteURL
	}{
		{
			name: "scp-like is ssh with no port",
			raw:  "git@github.com:harrisoncramer/weave.git",
			want: RemoteURL{Raw: "git@github.com:harrisoncramer/weave.git", Scheme: "ssh", Host: "github.com", Path: "harrisoncramer/weave"},
		},
		{
			name: "an ssh port is kept on the pieces even though canonical drops it",
			raw:  "ssh://git@ghe.example.com:2222/owner/repo.git",
			want: RemoteURL{Raw: "ssh://git@ghe.example.com:2222/owner/repo.git", Scheme: "ssh", Host: "ghe.example.com", Port: "2222", Path: "owner/repo"},
		},
		{
			name: "git+https unwraps to https",
			raw:  "git+https://GitHub.com/owner/repo",
			want: RemoteURL{Raw: "git+https://GitHub.com/owner/repo", Scheme: "https", Host: "github.com", Path: "owner/repo"},
		},
		{
			name: "an ipv6 host is stored without its brackets",
			raw:  "http://[::1]:8080/owner/repo.git",
			want: RemoteURL{Raw: "http://[::1]:8080/owner/repo.git", Scheme: "http", Host: "::1", Port: "8080", Path: "owner/repo"},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			got, err := ParseRemoteURL(tt.raw)
			require.NoError(t, err)
			assert.Equal(t, tt.want, got)
		})
	}
}

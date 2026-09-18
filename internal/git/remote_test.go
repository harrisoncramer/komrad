package git

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// TestParseRemoteURL covers every spelling of a remote that names one repository, the pieces each
// is taken apart into, and every shape of remote that names none.
func TestParseRemoteURL(t *testing.T) {
	t.Parallel()

	const weave = "https://github.com/harrisoncramer/weave"

	tests := []struct {
		name      string
		raw       string
		want      RemoteURL
		canonical string
		refused   bool
	}{
		{
			name:      "scp-like with a .git suffix",
			raw:       "git@github.com:harrisoncramer/weave.git",
			want:      RemoteURL{Scheme: "ssh", Host: "github.com", Path: "harrisoncramer/weave"},
			canonical: weave,
		},
		{
			name:      "scp-like without a .git suffix",
			raw:       "git@github.com:harrisoncramer/weave",
			want:      RemoteURL{Scheme: "ssh", Host: "github.com", Path: "harrisoncramer/weave"},
			canonical: weave,
		},
		{
			name:      "scp-like user may be anything",
			raw:       "deploy@github.com:harrisoncramer/weave.git",
			want:      RemoteURL{Scheme: "ssh", Host: "github.com", Path: "harrisoncramer/weave"},
			canonical: weave,
		},
		{
			name:      "scp-like without a user",
			raw:       "github.com:harrisoncramer/weave.git",
			want:      RemoteURL{Scheme: "ssh", Host: "github.com", Path: "harrisoncramer/weave"},
			canonical: weave,
		},
		{
			name:      "scp-like host is lowercased",
			raw:       "git@GitHub.com:harrisoncramer/weave.git",
			want:      RemoteURL{Scheme: "ssh", Host: "github.com", Path: "harrisoncramer/weave"},
			canonical: weave,
		},
		{
			name:      "scp-like has no port so the number is a path segment",
			raw:       "git@github.com:9999/user/repo.git",
			want:      RemoteURL{Scheme: "ssh", Host: "github.com", Path: "9999/user/repo"},
			canonical: "https://github.com/9999/user/repo",
		},
		{
			name:      "scp-like keeps a nested path whole",
			raw:       "git@gitlab.com:group/subgroup/repo.git",
			want:      RemoteURL{Scheme: "ssh", Host: "gitlab.com", Path: "group/subgroup/repo"},
			canonical: "https://gitlab.com/group/subgroup/repo",
		},
		{
			name:      "scp-like with a leading slash on the path",
			raw:       "git@github.com:/harrisoncramer/weave.git",
			want:      RemoteURL{Scheme: "ssh", Host: "github.com", Path: "harrisoncramer/weave"},
			canonical: weave,
		},
		{
			name:      "ssh scheme becomes https",
			raw:       "ssh://git@github.com/harrisoncramer/weave.git",
			want:      RemoteURL{Scheme: "ssh", Host: "github.com", Path: "harrisoncramer/weave"},
			canonical: weave,
		},
		{
			name:      "ssh keeps its port on the pieces and drops it from canonical",
			raw:       "ssh://git@github.com:22/harrisoncramer/weave.git",
			want:      RemoteURL{Scheme: "ssh", Host: "github.com", Port: "22", Path: "harrisoncramer/weave"},
			canonical: weave,
		},
		{
			name:      "ssh without a user",
			raw:       "ssh://github.com/harrisoncramer/weave.git",
			want:      RemoteURL{Scheme: "ssh", Host: "github.com", Path: "harrisoncramer/weave"},
			canonical: weave,
		},
		{
			name:      "ssh drops a user and password",
			raw:       "ssh://user:secret@github.com/harrisoncramer/weave.git",
			want:      RemoteURL{Scheme: "ssh", Host: "github.com", Path: "harrisoncramer/weave"},
			canonical: weave,
		},
		{
			name:      "ssh port on another host is dropped from canonical",
			raw:       "ssh://git@ghe.example.com:2222/owner/repo.git",
			want:      RemoteURL{Scheme: "ssh", Host: "ghe.example.com", Port: "2222", Path: "owner/repo"},
			canonical: "https://ghe.example.com/owner/repo",
		},
		{
			name:      "ssh host is lowercased",
			raw:       "ssh://git@GitHub.com/harrisoncramer/weave.git",
			want:      RemoteURL{Scheme: "ssh", Host: "github.com", Path: "harrisoncramer/weave"},
			canonical: weave,
		},
		{
			name:      "git+ssh unwraps to ssh",
			raw:       "git+ssh://git@github.com/harrisoncramer/weave.git",
			want:      RemoteURL{Scheme: "ssh", Host: "github.com", Path: "harrisoncramer/weave"},
			canonical: weave,
		},
		{
			name:      "git+ssh keeps its port on the pieces and drops it from canonical",
			raw:       "git+ssh://git@ghe.example.com:2222/owner/repo.git",
			want:      RemoteURL{Scheme: "ssh", Host: "ghe.example.com", Port: "2222", Path: "owner/repo"},
			canonical: "https://ghe.example.com/owner/repo",
		},
		{
			name:      "git scheme becomes https",
			raw:       "git://github.com/harrisoncramer/weave.git",
			want:      RemoteURL{Scheme: "git", Host: "github.com", Path: "harrisoncramer/weave"},
			canonical: weave,
		},
		{
			name:      "git keeps its port on the pieces and drops it from canonical",
			raw:       "git://github.com:9418/harrisoncramer/weave.git",
			want:      RemoteURL{Scheme: "git", Host: "github.com", Port: "9418", Path: "harrisoncramer/weave"},
			canonical: weave,
		},
		{
			name:      "git host is lowercased",
			raw:       "git://GitHub.com/harrisoncramer/weave.git",
			want:      RemoteURL{Scheme: "git", Host: "github.com", Path: "harrisoncramer/weave"},
			canonical: weave,
		},
		{
			name:      "https is already canonical",
			raw:       "https://github.com/harrisoncramer/weave",
			want:      RemoteURL{Scheme: "https", Host: "github.com", Path: "harrisoncramer/weave"},
			canonical: weave,
		},
		{
			name:      "https drops a .git suffix",
			raw:       "https://github.com/harrisoncramer/weave.git",
			want:      RemoteURL{Scheme: "https", Host: "github.com", Path: "harrisoncramer/weave"},
			canonical: weave,
		},
		{
			name:      "https drops a trailing slash",
			raw:       "https://github.com/harrisoncramer/weave/",
			want:      RemoteURL{Scheme: "https", Host: "github.com", Path: "harrisoncramer/weave"},
			canonical: weave,
		},
		{
			name:      "https drops a .git suffix and a trailing slash together",
			raw:       "https://github.com/harrisoncramer/weave.git/",
			want:      RemoteURL{Scheme: "https", Host: "github.com", Path: "harrisoncramer/weave"},
			canonical: weave,
		},
		{
			name:      "https host is lowercased",
			raw:       "https://GitHub.com/harrisoncramer/weave",
			want:      RemoteURL{Scheme: "https", Host: "github.com", Path: "harrisoncramer/weave"},
			canonical: weave,
		},
		{
			name:      "www.github.com is github.com",
			raw:       "https://www.github.com/harrisoncramer/weave.git",
			want:      RemoteURL{Scheme: "https", Host: "github.com", Path: "harrisoncramer/weave"},
			canonical: weave,
		},
		{
			name:      "www is kept on any other host",
			raw:       "https://www.ghe.example.com/owner/repo",
			want:      RemoteURL{Scheme: "https", Host: "www.ghe.example.com", Path: "owner/repo"},
			canonical: "https://www.ghe.example.com/owner/repo",
		},
		{
			name:      "https drops a user",
			raw:       "https://harrisoncramer@github.com/harrisoncramer/weave.git",
			want:      RemoteURL{Scheme: "https", Host: "github.com", Path: "harrisoncramer/weave"},
			canonical: weave,
		},
		{
			name:      "https drops a user and token",
			raw:       "https://user:token@github.com/harrisoncramer/weave.git",
			want:      RemoteURL{Scheme: "https", Host: "github.com", Path: "harrisoncramer/weave"},
			canonical: weave,
		},
		{
			name:      "https drops a query and fragment",
			raw:       "https://github.com/harrisoncramer/weave.git?foo=bar#frag",
			want:      RemoteURL{Scheme: "https", Host: "github.com", Path: "harrisoncramer/weave"},
			canonical: weave,
		},
		{
			name:      "https keeps its port",
			raw:       "https://ghe.example.com:8443/owner/repo",
			want:      RemoteURL{Scheme: "https", Host: "ghe.example.com", Port: "8443", Path: "owner/repo"},
			canonical: "https://ghe.example.com:8443/owner/repo",
		},
		{
			name:      "https keeps path case",
			raw:       "https://github.com/HarrisonCramer/Weave",
			want:      RemoteURL{Scheme: "https", Host: "github.com", Path: "HarrisonCramer/Weave"},
			canonical: "https://github.com/HarrisonCramer/Weave",
		},
		{
			name:      "https keeps a nested path whole",
			raw:       "https://gitlab.com/group/subgroup/repo.git",
			want:      RemoteURL{Scheme: "https", Host: "gitlab.com", Path: "group/subgroup/repo"},
			canonical: "https://gitlab.com/group/subgroup/repo",
		},
		{
			name:      "https keeps an encoded slash encoded",
			raw:       "https://github.com/owner/repo%2Fsub",
			want:      RemoteURL{Scheme: "https", Host: "github.com", Path: "owner/repo%2Fsub"},
			canonical: "https://github.com/owner/repo%2Fsub",
		},
		{
			name:      "https keeps an encoded space encoded",
			raw:       "https://github.com/owner/re%20po",
			want:      RemoteURL{Scheme: "https", Host: "github.com", Path: "owner/re%20po"},
			canonical: "https://github.com/owner/re%20po",
		},
		{
			name:      "https removes .git only once",
			raw:       "https://github.com/owner/repo.git.git",
			want:      RemoteURL{Scheme: "https", Host: "github.com", Path: "owner/repo.git"},
			canonical: "https://github.com/owner/repo.git",
		},
		{
			name:      "git+https unwraps to https",
			raw:       "git+https://github.com/harrisoncramer/weave.git",
			want:      RemoteURL{Scheme: "https", Host: "github.com", Path: "harrisoncramer/weave"},
			canonical: weave,
		},
		{
			name:      "git+https keeps its port",
			raw:       "git+https://ghe.example.com:8443/owner/repo.git",
			want:      RemoteURL{Scheme: "https", Host: "ghe.example.com", Port: "8443", Path: "owner/repo"},
			canonical: "https://ghe.example.com:8443/owner/repo",
		},
		{
			name:      "http becomes https",
			raw:       "http://github.com/harrisoncramer/weave.git",
			want:      RemoteURL{Scheme: "http", Host: "github.com", Path: "harrisoncramer/weave"},
			canonical: weave,
		},
		{
			name:      "http keeps its port",
			raw:       "http://ghe.example.com:8080/owner/repo.git",
			want:      RemoteURL{Scheme: "http", Host: "ghe.example.com", Port: "8080", Path: "owner/repo"},
			canonical: "https://ghe.example.com:8080/owner/repo",
		},
		{
			name:      "http drops a user and password",
			raw:       "http://user:secret@ghe.example.com/owner/repo.git",
			want:      RemoteURL{Scheme: "http", Host: "ghe.example.com", Path: "owner/repo"},
			canonical: "https://ghe.example.com/owner/repo",
		},
		{
			name:      "an ipv6 host is stored bare and canonical brackets it with its port",
			raw:       "http://[::1]:8080/owner/repo.git",
			want:      RemoteURL{Scheme: "http", Host: "::1", Port: "8080", Path: "owner/repo"},
			canonical: "https://[::1]:8080/owner/repo",
		},
		{
			name:      "an ipv6 host over ssh drops its port from canonical",
			raw:       "ssh://git@[::1]:2222/owner/repo.git",
			want:      RemoteURL{Scheme: "ssh", Host: "::1", Port: "2222", Path: "owner/repo"},
			canonical: "https://[::1]/owner/repo",
		},
		{
			name:      "scheme case does not matter",
			raw:       "HTTPS://github.com/harrisoncramer/weave",
			want:      RemoteURL{Scheme: "https", Host: "github.com", Path: "harrisoncramer/weave"},
			canonical: weave,
		},
		{
			name:      "surrounding whitespace is trimmed and raw is kept as given",
			raw:       "  git@github.com:harrisoncramer/weave.git\n",
			want:      RemoteURL{Scheme: "ssh", Host: "github.com", Path: "harrisoncramer/weave"},
			canonical: weave,
		},
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
		{name: "scp-like with no path is not a repository", raw: "git@github.com:", refused: true},
		{name: "ftp is not a scheme git uses", raw: "ftp://github.com/harrisoncramer/weave.git", refused: true},
		{name: "a scheme with no host is not a repository", raw: "https:///harrisoncramer/weave", refused: true},
		{name: "a port with no path is not a repository", raw: "https://github.com:443", refused: true},
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
			tt.want.Raw = tt.raw
			assert.Equal(t, tt.want, got)
			assert.Equal(t, tt.canonical, got.Canonical())
		})
	}
}

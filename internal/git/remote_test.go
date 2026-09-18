package git

import (
	"testing"

	"github.com/stretchr/testify/assert"
)

// TestCanonicalRemoteURL covers every spelling of a remote that names one repository, and
// every shape of remote that names none.
func TestCanonicalRemoteURL(t *testing.T) {
	t.Parallel()

	const weave = "https://github.com/harrisoncramer/weave"

	tests := []struct {
		name string
		raw  string
		want string
		ok   bool
	}{
		{name: "scp-like with a .git suffix", raw: "git@github.com:harrisoncramer/weave.git", want: weave, ok: true},
		{name: "scp-like without a .git suffix", raw: "git@github.com:harrisoncramer/weave", want: weave, ok: true},
		{name: "ssh scheme becomes https", raw: "ssh://git@github.com/harrisoncramer/weave.git", want: weave, ok: true},
		{name: "ssh drops its port", raw: "ssh://git@github.com:22/harrisoncramer/weave.git", want: weave, ok: true},
		{name: "ssh without a user", raw: "ssh://github.com/harrisoncramer/weave.git", want: weave, ok: true},
		{name: "git+ssh unwraps to https", raw: "git+ssh://git@github.com/harrisoncramer/weave.git", want: weave, ok: true},
		{name: "git scheme becomes https", raw: "git://github.com/harrisoncramer/weave.git", want: weave, ok: true},
		{name: "git drops its port", raw: "git://github.com:9418/harrisoncramer/weave.git", want: weave, ok: true},
		{name: "https is already canonical", raw: "https://github.com/harrisoncramer/weave", want: weave, ok: true},
		{name: "https drops a .git suffix", raw: "https://github.com/harrisoncramer/weave.git", want: weave, ok: true},
		{name: "https drops a trailing slash", raw: "https://github.com/harrisoncramer/weave/", want: weave, ok: true},
		{name: "host is lowercased", raw: "https://GitHub.com/harrisoncramer/weave", want: weave, ok: true},
		{name: "www.github.com is github.com", raw: "https://www.github.com/harrisoncramer/weave.git", want: weave, ok: true},
		{name: "a user is dropped", raw: "https://harrisoncramer@github.com/harrisoncramer/weave.git", want: weave, ok: true},
		{name: "a user and token are dropped", raw: "https://user:token@github.com/harrisoncramer/weave.git", want: weave, ok: true},
		{name: "query and fragment are dropped", raw: "https://github.com/harrisoncramer/weave.git?foo=bar#frag", want: weave, ok: true},
		{name: "git+https unwraps to https", raw: "git+https://github.com/harrisoncramer/weave.git", want: weave, ok: true},
		{name: "http becomes https", raw: "http://github.com/harrisoncramer/weave.git", want: weave, ok: true},
		{name: "surrounding whitespace is trimmed", raw: "  git@github.com:harrisoncramer/weave.git\n", want: weave, ok: true},
		{name: "scheme case does not matter", raw: "HTTPS://github.com/harrisoncramer/weave", want: weave, ok: true},
		{name: "scp-like user may be anything", raw: "deploy@github.com:harrisoncramer/weave.git", want: weave, ok: true},
		{name: "scp-like without a user", raw: "github.com:harrisoncramer/weave.git", want: weave, ok: true},
		{name: "path case is kept", raw: "https://github.com/HarrisonCramer/Weave", want: "https://github.com/HarrisonCramer/Weave", ok: true},
		{name: "a nested path is kept whole", raw: "https://gitlab.com/group/subgroup/repo.git", want: "https://gitlab.com/group/subgroup/repo", ok: true},
		{name: "an https port is kept", raw: "https://ghe.example.com:8443/owner/repo", want: "https://ghe.example.com:8443/owner/repo", ok: true},
		{name: "an http port is kept", raw: "http://ghe.example.com:8080/owner/repo.git", want: "https://ghe.example.com:8080/owner/repo", ok: true},
		{name: "an ssh port on another host is dropped", raw: "ssh://git@ghe.example.com:2222/owner/repo.git", want: "https://ghe.example.com/owner/repo", ok: true},
		{name: "www is kept on any other host", raw: "https://www.ghe.example.com/owner/repo", want: "https://www.ghe.example.com/owner/repo", ok: true},
		{name: "scp-like has no port so the number is a path segment", raw: "git@github.com:9999/user/repo.git", want: "https://github.com/9999/user/repo", ok: true},
		{name: "an ipv6 host keeps its brackets and http port", raw: "http://[::1]:8080/owner/repo.git", want: "https://[::1]:8080/owner/repo", ok: true},
		{name: ".git is removed only once", raw: "https://github.com/owner/repo.git.git", want: "https://github.com/owner/repo.git", ok: true},
		{name: "empty is not a repository", raw: "", want: "", ok: false},
		{name: "whitespace is not a repository", raw: "   ", want: "", ok: false},
		{name: "an absolute local path is not a repository", raw: "/Users/harrisoncramer/weave", want: "", ok: false},
		{name: "a relative local path is not a repository", raw: "./repo", want: "", ok: false},
		{name: "a local path with a colon after a slash is not scp-like", raw: "./foo:bar", want: "", ok: false},
		{name: "file scheme is not a repository", raw: "file:///path/to/repo.git", want: "", ok: false},
		{name: "a windows drive path is not scp-like", raw: `C:\repos\weave`, want: "", ok: false},
		{name: "a backslash anywhere is refused", raw: `https://github.com/owner\repo`, want: "", ok: false},
		{name: "a tilde path is not a repository", raw: "git@host:~user/repo.git", want: "", ok: false},
		{name: "a tilde path over ssh is not a repository", raw: "ssh://git@host/~user/repo.git", want: "", ok: false},
		{name: "a host with no path is not a repository", raw: "https://github.com", want: "", ok: false},
		{name: "a host with only a slash is not a repository", raw: "https://github.com/", want: "", ok: false},
		{name: "a one-segment path is not a repository", raw: "https://github.com/harrisoncramer", want: "", ok: false},
		{name: "a one-segment scp-like path is not a repository", raw: "git@github.com:harrisoncramer.git", want: "", ok: false},
		{name: "an empty segment is not a repository", raw: "https://github.com/owner//repo", want: "", ok: false},
		{name: "scp-like with no host is not a repository", raw: "git@:owner/repo", want: "", ok: false},
		{name: "ftp is not a scheme git uses", raw: "ftp://github.com/harrisoncramer/weave.git", want: "", ok: false},
		{name: "a scheme with no host is not a repository", raw: "https:///harrisoncramer/weave", want: "", ok: false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			got, ok := CanonicalRemoteURL(tt.raw)
			assert.Equal(t, tt.want, got)
			assert.Equal(t, tt.ok, ok)
		})
	}
}

package cli

import (
	"testing"

	"komrad/internal/forge"
	forgev1 "komrad/proto/gen/forge/v1"

	"github.com/stretchr/testify/assert"
)

// TestGitLabConnection covers which connection a host's token is read from, a host holding no
// GitLab token getting none so glab and git fall back to what the machine has.
func TestGitLabConnection(t *testing.T) {
	gitlab := &forgev1.Forge{Id: "gl", Kind: forge.KindGitLab, Host: "gitlab.com", Method: forge.MethodToken, HasCredential: true}
	selfHosted := &forgev1.Forge{Id: "self", Kind: forge.KindGitLab, Host: "git.example.com", Method: forge.MethodToken, HasCredential: true}
	github := &forgev1.Forge{Id: "gh", Kind: forge.KindGitHub, Host: "github.com", Method: forge.MethodToken, HasCredential: true}
	tokenless := &forgev1.Forge{Id: "gone", Kind: forge.KindGitLab, Host: "gitlab.com", Method: forge.MethodToken}

	tests := []struct {
		name   string
		forges []*forgev1.Forge
		host   string
		want   *forgev1.Forge
	}{
		{name: "gitlab.com", forges: []*forgev1.Forge{github, gitlab}, host: "gitlab.com", want: gitlab},
		{name: "a self-hosted gitlab", forges: []*forgev1.Forge{gitlab, selfHosted}, host: "git.example.com", want: selfHosted},
		{name: "a host spelled in another case", forges: []*forgev1.Forge{gitlab}, host: "GitLab.com", want: gitlab},
		{name: "a github host", forges: []*forgev1.Forge{github, gitlab}, host: "github.com"},
		{name: "a connection whose token is gone", forges: []*forgev1.Forge{tokenless}, host: "gitlab.com"},
		{name: "nothing connected", host: "gitlab.com"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			assert.Equal(t, tt.want, gitlabConnection(tt.forges, tt.host))
		})
	}
}

// TestWithoutPort covers the host a git credential query is matched on.
func TestWithoutPort(t *testing.T) {
	tests := []struct {
		name string
		host string
		want string
	}{
		{name: "a host alone", host: "gitlab.com", want: "gitlab.com"},
		{name: "a self-hosted gitlab on a port of its own", host: "git.example.com:8443", want: "git.example.com"},
		{name: "an ipv6 address on a port", host: "[::1]:8443", want: "::1"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			assert.Equal(t, tt.want, withoutPort(tt.host))
		})
	}
}

// TestGitLabCredentialReply covers the answer git is given, under the username GitLab's docs
// use for a token.
func TestGitLabCredentialReply(t *testing.T) {
	assert.Equal(t, "username=oauth2\npassword=glpat-fake\n", gitlabCredentialReply(forge.NewSecret("glpat-fake")))
}

// TestGlabEnv covers what glab is handed, naming the API host only where it is not the
// connection's own.
func TestGlabEnv(t *testing.T) {
	token := forge.NewSecret("glpat-fake")

	tests := []struct {
		name       string
		connection *forgev1.Forge
		want       string
	}{
		{
			name:       "gitlab.com",
			connection: &forgev1.Forge{Host: "gitlab.com"},
			want:       "GITLAB_TOKEN=glpat-fake\nGITLAB_HOST=gitlab.com\n",
		},
		{
			name:       "a self-hosted gitlab answering on its own host",
			connection: &forgev1.Forge{Host: "git.example.com", ApiUrl: "https://git.example.com/api/v4"},
			want:       "GITLAB_TOKEN=glpat-fake\nGITLAB_HOST=git.example.com\n",
		},
		{
			name:       "a self-hosted gitlab whose api answers on a host of its own",
			connection: &forgev1.Forge{Host: "git.example.com", ApiUrl: "https://api.git.example.com/api/v4"},
			want:       "GITLAB_TOKEN=glpat-fake\nGITLAB_HOST=git.example.com\nGITLAB_API_HOST=api.git.example.com\n",
		},
		{
			name:       "an api host spelled in another case",
			connection: &forgev1.Forge{Host: "git.example.com", ApiUrl: "https://Git.Example.com/api/v4"},
			want:       "GITLAB_TOKEN=glpat-fake\nGITLAB_HOST=git.example.com\n",
		},
		{
			name:       "a self-hosted gitlab whose api answers over plain http",
			connection: &forgev1.Forge{Host: "git.example.com", ApiUrl: "http://git.example.com/api/v4"},
			want:       "GITLAB_TOKEN=glpat-fake\nGITLAB_HOST=git.example.com\nGLAB_API_PROTOCOL=http\n",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			assert.Equal(t, tt.want, glabEnv(tt.connection, token))
		})
	}
}

// TestHandable covers which tokens komrad hands out on a line of their own.
func TestHandable(t *testing.T) {
	tests := []struct {
		name  string
		token string
		want  error
	}{
		{name: "a token on one line", token: "glpat-fake"},
		{name: "a token with a newline in it", token: "glpat-fake\nGITLAB_API_HOST=evil.example.com", want: errTokenSpansLines},
		{name: "a token with a carriage return in it", token: "glpat\r-fake", want: errTokenSpansLines},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			assert.ErrorIs(t, handable(forge.NewSecret(tt.token)), tt.want)
		})
	}
}

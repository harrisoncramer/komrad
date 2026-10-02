//go:build integration

package harness

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// writeFakeBinary puts an executable shell script on disk and answers its path.
func writeFakeBinary(t *testing.T, path, body string) string {
	t.Helper()

	require.NoError(t, os.MkdirAll(filepath.Dir(path), 0o755))
	require.NoError(t, os.WriteFile(path, []byte("#!/bin/sh\n"+body), 0o755)) //nolint:gosec // a script a test runs

	return path
}

// shimCase is one run of a stand-in: the environment it is given, what komrad answers, what
// the tool beyond it does, and what all of that should come to.
type shimCase struct {
	name string
	env  []string
	// komrad is the body of the komrad command the stand-in asks for a credential.
	komrad string
	// beyond is the body of the binary beyond the stand-in, an empty one writing down what it was
	// handed.
	beyond string
	// notInstalled leaves the machine with no such tool but the stand-in itself.
	notInstalled bool
	args         []string
	wantExit     int
	// wantSaw is the token and arguments the tool beyond wrote down, empty for one never run.
	wantSaw string
	// wantAsked is what komrad was asked for, {dir} standing for the checkout, and empty for a
	// komrad never asked.
	wantAsked  string
	wantStdout string
	wantStderr string
}

func runCase(t *testing.T, tool string, tt shimCase) {
	t.Helper()

	shim := shimFor(t, tool)

	root, err := filepath.EvalSymlinks(t.TempDir())
	require.NoError(t, err)

	cache := filepath.Join(root, "cache")
	asked := filepath.Join(root, "asked")
	saw := filepath.Join(root, "saw")
	scoped := filepath.Join(root, "scoped")

	komrad := writeFakeBinary(t, filepath.Join(root, "komrad"), "echo \"$@\" >"+asked+"\n"+tt.komrad)

	beyond := filepath.Join(root, "beyond")
	require.NoError(t, os.MkdirAll(beyond, 0o755))
	if !tt.notInstalled {
		body := tt.beyond
		if body == "" {
			body = "echo \"" + handedOver[shim.tool] + " $*\" >" + saw + "\n"
		}
		writeFakeBinary(t, filepath.Join(beyond, shim.tool), "echo \"${GLAB_CONFIG_DIR:-}\" >"+scoped+"\n"+body)
	}

	dir, err := WriteToolShims(cache, komrad)
	require.NoError(t, err)
	require.Equal(t, filepath.Join(cache, ToolsDirName), dir)

	work := filepath.Join(root, "work")
	require.NoError(t, os.MkdirAll(work, 0o755))

	cmd := exec.Command(filepath.Join(dir, shim.tool), tt.args...) //nolint:gosec // the stand-in this test wrote
	cmd.Dir = work
	cmd.Env = append([]string{"PATH=" + dir + ":" + beyond + ":/usr/bin:/bin", "HOME=" + root}, tt.env...)

	var stdout, stderr strings.Builder
	cmd.Stdout, cmd.Stderr = &stdout, &stderr

	exitCode := 0

	var exit *exec.ExitError
	if err := cmd.Run(); err != nil {
		require.ErrorAs(t, err, &exit)
		exitCode = exit.ExitCode()
	}

	assert.Equal(t, tt.wantExit, exitCode)
	assert.Equal(t, tt.wantSaw, readIfThere(t, saw))
	assert.Equal(t, strings.ReplaceAll(tt.wantAsked, "{dir}", work), readIfThere(t, asked))
	assert.Equal(t, tt.wantStdout, stdout.String())
	assert.Equal(t, tt.wantStderr, stderr.String())

	if config := readIfThere(t, scoped); config != "" {
		assert.NoDirExists(t, config)
	}
}

// handedOver is what each tool beyond writes down of the credential it was handed.
var handedOver = map[string]string{
	"gh":   "$GH_TOKEN",
	"glab": `${GLAB_CONFIG_DIR:+$(ls -l "$GLAB_CONFIG_DIR/config.yml" | cut -c1-10) $(cat "$GLAB_CONFIG_DIR/config.yml")}`,
}

func shimFor(t *testing.T, tool string) toolShim {
	t.Helper()

	for _, shim := range toolShims {
		if shim.tool == tool {
			return shim
		}
	}

	t.Fatalf("komrad writes no stand-in for %s", tool)

	return toolShim{}
}

func readIfThere(t *testing.T, path string) string {
	t.Helper()

	raw, err := os.ReadFile(path)
	if os.IsNotExist(err) {
		return ""
	}
	require.NoError(t, err)

	return strings.TrimSpace(string(raw))
}

// TestIntegrationGHShim covers the stand-in end to end, under the shell an agent would run it with.
func TestIntegrationGHShim(t *testing.T) {
	const minted = "echo ghs_minted\n"

	tests := []shimCase{
		{
			name:      "a token minted for this call alone, and handed over rather than printed",
			komrad:    minted,
			args:      []string{"pr", "create", "--title", "a change"},
			wantSaw:   "ghs_minted pr create --title a change",
			wantAsked: "github token --checkout {dir} --if-configured",
		},
		{
			name:       "komrad holds the credential and will not give it",
			komrad:     "echo 'no github app is configured' >&2\nexit 1\n",
			args:       []string{"pr", "create"},
			wantExit:   1,
			wantAsked:  "github token --checkout {dir} --if-configured",
			wantStderr: "no github app is configured\n",
		},
		{
			name:      "a machine naming no app, where gh keeps what it was logged in with",
			komrad:    "exit 0\n",
			args:      []string{"pr", "list"},
			wantSaw:   "pr list",
			wantAsked: "github token --checkout {dir} --if-configured",
		},
		{
			name:    "a GH_TOKEN somebody else set",
			env:     []string{"GH_TOKEN=ghp_mine"},
			komrad:  minted,
			args:    []string{"pr", "list"},
			wantSaw: "ghp_mine pr list",
		},
		{
			name:    "a GITHUB_TOKEN somebody else set, which gh reads itself",
			env:     []string{"GITHUB_TOKEN=ghp_theirs"},
			komrad:  minted,
			args:    []string{"pr", "list"},
			wantSaw: "pr list",
		},
		{
			name:       "the exit code and both streams are the real binary's",
			komrad:     minted,
			beyond:     "echo 'all checks passing'\necho 'a warning' >&2\nexit 3\n",
			args:       []string{"pr", "checks"},
			wantExit:   3,
			wantAsked:  "github token --checkout {dir} --if-configured",
			wantStdout: "all checks passing\n",
			wantStderr: "a warning\n",
		},
		{
			name:         "a machine with no gh but the stand-in itself",
			komrad:       minted,
			notInstalled: true,
			args:         []string{"pr", "create"},
			wantExit:     127,
			wantStderr:   "komrad: gh is not installed on this machine\n",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			runCase(t, "gh", tt)
		})
	}
}

// TestIntegrationGLabShim covers the glab stand-in end to end, under the shell an agent would run
// it with.
func TestIntegrationGLabShim(t *testing.T) {
	const read = "echo 'token: glpat_connected'\n"

	tests := []shimCase{
		{
			name:      "the connection's config handed over in a file only its owner reads",
			komrad:    read,
			args:      []string{"mr", "create", "--title", "a change"},
			wantSaw:   "-rw------- token: glpat_connected mr create --title a change",
			wantAsked: "gitlab config --checkout {dir} --if-configured",
		},
		{
			name:      "a config the shell would expand, handed over as written",
			komrad:    "echo 'token: glpat_$(touch pwned)'\n",
			args:      []string{"mr", "list"},
			wantSaw:   "-rw------- token: glpat_$(touch pwned) mr list",
			wantAsked: "gitlab config --checkout {dir} --if-configured",
		},
		{
			name:       "the exit code and both streams are the real binary's, the config gone after it",
			komrad:     read,
			beyond:     "echo 'pipeline passed'\necho 'a warning' >&2\nexit 3\n",
			args:       []string{"ci", "status"},
			wantExit:   3,
			wantAsked:  "gitlab config --checkout {dir} --if-configured",
			wantStdout: "pipeline passed\n",
			wantStderr: "a warning\n",
		},
		{
			name:       "komrad holds the credential and will not give it",
			komrad:     "echo 'the gitlab connection has no credential' >&2\nexit 1\n",
			args:       []string{"mr", "create"},
			wantExit:   1,
			wantAsked:  "gitlab config --checkout {dir} --if-configured",
			wantStderr: "the gitlab connection has no credential\n",
		},
		{
			name:      "a checkout on no connected gitlab, where glab keeps what it was logged in with",
			komrad:    "exit 0\n",
			args:      []string{"mr", "list"},
			wantSaw:   "mr list",
			wantAsked: "gitlab config --checkout {dir} --if-configured",
		},
		{
			name:    "a GITLAB_TOKEN somebody else set",
			env:     []string{"GITLAB_TOKEN=glpat_mine"},
			komrad:  read,
			args:    []string{"mr", "list"},
			wantSaw: "mr list",
		},
		{
			name:    "a GITLAB_ACCESS_TOKEN somebody else set, which glab reads itself",
			env:     []string{"GITLAB_ACCESS_TOKEN=glpat_theirs"},
			komrad:  read,
			args:    []string{"mr", "list"},
			wantSaw: "mr list",
		},
		{
			name:    "an OAUTH_TOKEN somebody else set, which glab reads itself",
			env:     []string{"OAUTH_TOKEN=oauth_theirs"},
			komrad:  read,
			args:    []string{"mr", "list"},
			wantSaw: "mr list",
		},
		{
			name:         "a machine with no glab but the stand-in itself",
			komrad:       read,
			notInstalled: true,
			args:         []string{"mr", "create"},
			wantExit:     127,
			wantStderr:   "komrad: glab is not installed on this machine\n",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			runCase(t, "glab", tt)
		})
	}
}

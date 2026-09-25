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

// shimCase is one run of the stand-in: the environment it is given, what komrad answers, what
// the gh beyond it does, and what all of that should come to.
type shimCase struct {
	name string
	env  []string
	// komrad is the body of the token command the stand-in asks.
	komrad string
	// gh is the body of the binary beyond the stand-in, an empty one writing down what it was
	// handed.
	gh string
	// noGH leaves the machine with no gh but the stand-in itself.
	noGH     bool
	args     []string
	wantExit int
	// wantGHSaw is the token and arguments the gh beyond wrote down, empty for a gh never run.
	wantGHSaw string
	// wantAsked is what komrad was asked for, {dir} standing for the checkout, and empty for a
	// komrad never asked.
	wantAsked  string
	wantStdout string
	wantStderr string
}

func runCase(t *testing.T, tt shimCase) {
	t.Helper()

	root, err := filepath.EvalSymlinks(t.TempDir())
	require.NoError(t, err)

	cache := filepath.Join(root, "cache")
	asked := filepath.Join(root, "asked")
	saw := filepath.Join(root, "saw")

	komrad := writeFakeBinary(t, filepath.Join(root, "komrad"), "echo \"$@\" >"+asked+"\n"+tt.komrad)

	beyond := filepath.Join(root, "beyond")
	require.NoError(t, os.MkdirAll(beyond, 0o755))
	if !tt.noGH {
		body := tt.gh
		if body == "" {
			body = "echo \"$GH_TOKEN $*\" >" + saw + "\n"
		}
		writeFakeBinary(t, filepath.Join(beyond, "gh"), body)
	}

	dir, err := WriteGHShim(cache, komrad)
	require.NoError(t, err)
	require.Equal(t, filepath.Join(cache, ToolsDirName), dir)

	work := filepath.Join(root, "work")
	require.NoError(t, os.MkdirAll(work, 0o755))

	cmd := exec.Command(filepath.Join(dir, "gh"), tt.args...) //nolint:gosec // the stand-in this test wrote
	cmd.Dir = work
	cmd.Env = append([]string{"PATH=" + dir + ":" + beyond, "HOME=" + root}, tt.env...)

	var stdout, stderr strings.Builder
	cmd.Stdout, cmd.Stderr = &stdout, &stderr

	exitCode := 0

	var exit *exec.ExitError
	if err := cmd.Run(); err != nil {
		require.ErrorAs(t, err, &exit)
		exitCode = exit.ExitCode()
	}

	assert.Equal(t, tt.wantExit, exitCode)
	assert.Equal(t, tt.wantGHSaw, readIfThere(t, saw))
	assert.Equal(t, strings.ReplaceAll(tt.wantAsked, "{dir}", work), readIfThere(t, asked))
	assert.Equal(t, tt.wantStdout, stdout.String())
	assert.Equal(t, tt.wantStderr, stderr.String())
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
			wantGHSaw: "ghs_minted pr create --title a change",
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
			wantGHSaw: "pr list",
			wantAsked: "github token --checkout {dir} --if-configured",
		},
		{
			name:      "a GH_TOKEN somebody else set",
			env:       []string{"GH_TOKEN=ghp_mine"},
			komrad:    minted,
			args:      []string{"pr", "list"},
			wantGHSaw: "ghp_mine pr list",
		},
		{
			name:      "a GITHUB_TOKEN somebody else set, which gh reads itself",
			env:       []string{"GITHUB_TOKEN=ghp_theirs"},
			komrad:    minted,
			args:      []string{"pr", "list"},
			wantGHSaw: "pr list",
		},
		{
			name:       "the exit code and both streams are the real binary's",
			komrad:     minted,
			gh:         "echo 'all checks passing'\necho 'a warning' >&2\nexit 3\n",
			args:       []string{"pr", "checks"},
			wantExit:   3,
			wantAsked:  "github token --checkout {dir} --if-configured",
			wantStdout: "all checks passing\n",
			wantStderr: "a warning\n",
		},
		{
			name:       "a machine with no gh but the stand-in itself",
			komrad:     minted,
			noGH:       true,
			args:       []string{"pr", "create"},
			wantExit:   127,
			wantStderr: "komrad: gh is not installed on this machine\n",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			runCase(t, tt)
		})
	}
}

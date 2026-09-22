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

// writeScript puts an executable shell script on disk and answers its path.
func writeScript(t *testing.T, path, body string) string {
	t.Helper()

	require.NoError(t, os.MkdirAll(filepath.Dir(path), 0o755))
	require.NoError(t, os.WriteFile(path, []byte("#!/bin/sh\n"+body), 0o755)) //nolint:gosec // a script a test runs

	return path
}

// shimRun is one run of the stand-in: what it wrote, what gh saw, and how it ended.
type shimRun struct {
	dir      string
	stdout   string
	stderr   string
	exitCode int
	askedFor string
	ghSaw    string
}

// runShim writes the stand-in with a weave that answers as the body says, a gh beyond it, and
// runs it.
func runShim(t *testing.T, weaveBody string, args ...string) shimRun {
	t.Helper()

	return runShimWith(t, nil, weaveBody, args...)
}

// runShimWith is runShim with more of an environment than the PATH the stand-in needs.
func runShimWith(t *testing.T, env []string, weaveBody string, args ...string) shimRun {
	t.Helper()

	root := t.TempDir()
	cache := filepath.Join(root, "cache")
	asked := filepath.Join(root, "asked")
	saw := filepath.Join(root, "saw")

	weave := writeScript(t, filepath.Join(root, "weave"), "echo \"$@\" >"+asked+"\n"+weaveBody)
	beyond := filepath.Join(root, "beyond")
	writeScript(t, filepath.Join(beyond, "gh"), "echo \"$GH_TOKEN $*\" >"+saw+"\n")

	dir, err := WriteGHShim(cache, weave)
	require.NoError(t, err)
	require.Equal(t, filepath.Join(cache, ToolsDirName), dir)

	work := filepath.Join(root, "work")
	require.NoError(t, os.MkdirAll(work, 0o755))

	cmd := exec.Command(filepath.Join(dir, "gh"), args...) //nolint:gosec // the stand-in this test wrote
	cmd.Dir = work
	cmd.Env = append([]string{"PATH=" + dir + ":" + beyond, "HOME=" + root}, env...)

	var stdout, stderr strings.Builder
	cmd.Stdout, cmd.Stderr = &stdout, &stderr
	err = cmd.Run()

	run := shimRun{dir: work, stdout: stdout.String(), stderr: stderr.String()}

	var exit *exec.ExitError
	if err != nil {
		require.ErrorAs(t, err, &exit)
		run.exitCode = exit.ExitCode()
	}

	run.askedFor = readIfThere(t, asked)
	run.ghSaw = readIfThere(t, saw)

	return run
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

// TestGHShimHandsTheTokenToTheRealBinary covers the stand-in's whole job: a token minted for
// this call alone, the arguments forwarded, and the checkout named.
func TestGHShimHandsTheTokenToTheRealBinary(t *testing.T) {
	run := runShim(t, "echo ghs_minted\n", "pr", "create", "--title", "a change")

	assert.Equal(t, 0, run.exitCode)
	assert.Equal(t, "ghs_minted pr create --title a change", run.ghSaw)
	assert.Equal(t, "github token --checkout "+run.dir, run.askedFor, "the checkout it was run in decides whether weave answers")
	assert.NotContains(t, run.stdout, "ghs_minted", "the token is handed over rather than printed")
}

// TestGHShimRefusesWhenWeaveWillNotMint covers the failure an agent has to see rather than a
// gh that quietly runs with no credential.
func TestGHShimRefusesWhenWeaveWillNotMint(t *testing.T) {
	run := runShim(t, "echo 'no github app is configured' >&2\nexit 1\n", "pr", "create")

	assert.Equal(t, 1, run.exitCode)
	assert.Contains(t, run.stderr, "no github app is configured")
	assert.Empty(t, run.ghSaw, "gh is never reached without a credential")
}

// TestGHShimLeavesATokenSomebodyElseSetAlone covers a person or a harness that put a
// credential of its own in the environment, which the stand-in does not overwrite.
func TestGHShimLeavesATokenSomebodyElseSetAlone(t *testing.T) {
	run := runShimWith(t, []string{"GH_TOKEN=ghp_mine"}, "echo ghs_minted\n", "pr", "list")

	assert.Equal(t, 0, run.exitCode)
	assert.Equal(t, "ghp_mine pr list", run.ghSaw)
	assert.Empty(t, run.askedFor, "weave is not asked for a token nobody needs")
}

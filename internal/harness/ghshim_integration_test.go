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

// shimRun is one run of the stand-in: what it wrote, what gh saw, and how it ended.
type shimRun struct {
	dir      string
	stdout   string
	stderr   string
	exitCode int
	askedFor string
	ghSaw    string
}

// shimCase is one way of standing the pieces up: the environment the stand-in is run with,
// what weave answers, what the gh beyond it does, and the arguments.
type shimCase struct {
	env   []string
	weave string
	// gh is the body of the binary beyond the stand-in, an empty one writing down what it was
	// handed.
	gh string
	// noGH leaves the machine with no gh but the stand-in itself.
	noGH bool
	args []string
}

// runShim writes the stand-in with a weave that answers as the body says, a gh beyond it, and
// runs it.
func runShim(t *testing.T, weaveBody string, args ...string) shimRun {
	t.Helper()

	return runCase(t, shimCase{weave: weaveBody, args: args})
}

func runCase(t *testing.T, tt shimCase) shimRun {
	t.Helper()

	root := t.TempDir()
	cache := filepath.Join(root, "cache")
	asked := filepath.Join(root, "asked")
	saw := filepath.Join(root, "saw")

	weave := writeFakeBinary(t, filepath.Join(root, "weave"), "echo \"$@\" >"+asked+"\n"+tt.weave)

	beyond := filepath.Join(root, "beyond")
	require.NoError(t, os.MkdirAll(beyond, 0o755))
	ghBody := tt.gh
	if ghBody == "" {
		ghBody = "echo \"$GH_TOKEN $*\" >" + saw + "\n"
	}
	if !tt.noGH {
		writeFakeBinary(t, filepath.Join(beyond, "gh"), ghBody)
	}

	dir, err := WriteGHShim(cache, weave)
	require.NoError(t, err)
	require.Equal(t, filepath.Join(cache, ToolsDirName), dir)

	work := filepath.Join(root, "work")
	require.NoError(t, os.MkdirAll(work, 0o755))

	cmd := exec.Command(filepath.Join(dir, "gh"), tt.args...) //nolint:gosec // the stand-in this test wrote
	cmd.Dir = work
	cmd.Env = append([]string{"PATH=" + dir + ":" + beyond, "HOME=" + root}, tt.env...)

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
	assert.Equal(t, "github token --checkout "+run.dir+" --if-configured", run.askedFor,
		"the checkout it was run in decides whether weave answers")
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
	run := runCase(t, shimCase{
		env:   []string{"GH_TOKEN=ghp_mine"},
		weave: "echo ghs_minted\n",
		args:  []string{"pr", "list"},
	})

	assert.Equal(t, 0, run.exitCode)
	assert.Equal(t, "ghp_mine pr list", run.ghSaw)
	assert.Empty(t, run.askedFor, "weave is not asked for a token nobody needs")
}

// TestGHShimIsTheRealBinaryOnceItHasTheToken covers what an agent reads back from gh: the
// exit code and both streams are the real binary's, the stand-in having exec'd itself away.
func TestGHShimIsTheRealBinaryOnceItHasTheToken(t *testing.T) {
	run := runCase(t, shimCase{
		weave: "echo ghs_minted\n",
		gh:    "echo 'all checks passing'\necho 'a warning' >&2\nexit 3\n",
		args:  []string{"pr", "checks"},
	})

	assert.Equal(t, 3, run.exitCode)
	assert.Equal(t, "all checks passing\n", run.stdout)
	assert.Equal(t, "a warning\n", run.stderr)
}

// TestGHShimOnAMachineWithNoGH covers the stand-in being the only gh there is, which says so
// rather than leaving an agent to read a failure from a shell.
func TestGHShimOnAMachineWithNoGH(t *testing.T) {
	run := runCase(t, shimCase{weave: "echo ghs_minted\n", noGH: true, args: []string{"pr", "create"}})

	assert.Equal(t, 127, run.exitCode)
	assert.Contains(t, run.stderr, "gh is not installed")
	assert.Empty(t, run.askedFor, "nothing is minted for a call that cannot happen")
}

// TestGHShimFallsThroughOnAMachineWithNoApp covers a laptop or a box logged in by hand, where
// weave names no App and gh keeps the credential it already had.
func TestGHShimFallsThroughOnAMachineWithNoApp(t *testing.T) {
	run := runCase(t, shimCase{weave: "exit 0\n", args: []string{"pr", "list"}})

	assert.Equal(t, 0, run.exitCode)
	assert.Equal(t, "pr list", run.ghSaw, "gh runs with the environment it would have had")
	assert.Equal(t, "github token --checkout "+run.dir+" --if-configured", run.askedFor)
}

// TestGHShimLeavesAGitHubTokenAlone covers the other variable gh reads, which the stand-in
// passes through the same way.
func TestGHShimLeavesAGitHubTokenAlone(t *testing.T) {
	run := runCase(t, shimCase{
		env:   []string{"GITHUB_TOKEN=ghp_theirs"},
		weave: "echo ghs_minted\n",
		args:  []string{"pr", "list"},
	})

	assert.Equal(t, 0, run.exitCode)
	assert.Equal(t, "pr list", run.ghSaw, "gh reads GITHUB_TOKEN itself, and GH_TOKEN was never set")
	assert.Empty(t, run.askedFor, "weave is not asked for a token nobody needs")
}

package harness

import (
	"fmt"
	"os"
	"path/filepath"
)

// ToolsDirName is the directory under the cache dir holding the stand-ins weave puts ahead of
// an agent's PATH.
const ToolsDirName = "weave-agent-tools"

// ghShim is the stand-in an agent reaches as gh.
const ghShim = `#!/bin/sh
# Weave's stand-in for gh. GitHub's CLI reads its credential from the environment and has no
# credential helper, so this hands it a token minted for this call alone.
self=%s
weave=%s

real=
IFS=:
for dir in $PATH; do
	[ -n "$dir" ] || dir=.
	if [ "$dir" != "$self" ] && [ -x "$dir/gh" ]; then
		real=$dir/gh
		break
	fi
done
unset IFS

if [ -z "$real" ]; then
	echo "weave: gh is not installed on this machine" >&2
	exit 127
fi

if [ -n "${GH_TOKEN:-}" ] || [ -n "${GITHUB_TOKEN:-}" ]; then
	exec "$real" "$@"
fi

token=$("$weave" github token --checkout "$(pwd)" --if-configured) || exit 1
if [ -n "$token" ]; then
	export GH_TOKEN="$token"
fi

exec "$real" "$@"
`

// WriteGHShim writes the gh stand-in under the cache dir, answering the directory holding it
// and an empty one when there is no weave binary to point it at.
func WriteGHShim(cacheDir, weaveBinary string) (string, error) {
	if cacheDir == "" || weaveBinary == "" {
		return "", nil
	}

	dir := filepath.Join(cacheDir, ToolsDirName)
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return "", fmt.Errorf("failed to create %q: %w", dir, err)
	}

	path := filepath.Join(dir, "gh")
	if err := writeScript(path, fmt.Sprintf(ghShim, QuoteArg(dir), QuoteArg(weaveBinary))); err != nil {
		return "", err
	}

	return dir, nil
}

// writeScript puts a script where a shell can run it, through a file of its own renamed over
// the target.
func writeScript(path, body string) error {
	// A launch rewrites the stand-in while another agent may be part-way through running it, and
	// a shell reads a script as it goes, so the old inode has to stay whole until it is done.
	temporary, err := os.CreateTemp(filepath.Dir(path), ".gh-*")
	if err != nil {
		return fmt.Errorf("failed to write %q: %w", path, err)
	}

	defer func() { _ = os.Remove(temporary.Name()) }() //nolint:errcheck // gone already on success

	if _, err := temporary.WriteString(body); err != nil {
		_ = temporary.Close()

		return fmt.Errorf("failed to write %q: %w", path, err)
	}

	if err := temporary.Chmod(0o755); err != nil { //nolint:gosec // a stand-in on PATH is run, not read
		_ = temporary.Close()

		return fmt.Errorf("failed to write %q: %w", path, err)
	}

	if err := temporary.Close(); err != nil {
		return fmt.Errorf("failed to write %q: %w", path, err)
	}

	return os.Rename(temporary.Name(), path)
}

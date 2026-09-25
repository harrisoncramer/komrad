package harness

import (
	"fmt"
	"os"
	"path/filepath"
)

// ToolsDirName is the directory under the cache dir holding the stand-ins komrad puts ahead of
// an agent's PATH.
const ToolsDirName = "komrad-agent-tools"

// ghShim is the stand-in an agent reaches as gh.
const ghShim = `#!/bin/sh
# Komrad's stand-in for gh. GitHub's CLI reads its credential from the environment and has no
# credential helper, so this hands it a token minted for this call alone.
self=%s
komrad=%s

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
	echo "komrad: gh is not installed on this machine" >&2
	exit 127
fi

if [ -n "${GH_TOKEN:-}" ] || [ -n "${GITHUB_TOKEN:-}" ]; then
	exec "$real" "$@"
fi

token=$("$komrad" github token --checkout "$(pwd)" --if-configured) || exit 1
if [ -n "$token" ]; then
	export GH_TOKEN="$token"
fi

exec "$real" "$@"
`

// WriteGHShim writes the gh stand-in under the cache dir, answering the directory holding it
// and an empty one when there is no komrad binary to point it at.
func WriteGHShim(cacheDir, komradBinary string) (string, error) {
	if cacheDir == "" || komradBinary == "" {
		return "", nil
	}

	dir := filepath.Join(cacheDir, ToolsDirName)
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return "", fmt.Errorf("failed to create %q: %w", dir, err)
	}

	path := filepath.Join(dir, "gh")
	if err := writeScript(path, fmt.Sprintf(ghShim, QuoteArg(dir), QuoteArg(komradBinary))); err != nil {
		return "", err
	}

	return dir, nil
}

// writeScript puts a script where a shell can run it, through a file of its own renamed over
// the target.
func writeScript(path, body string) error {
	// Explainer: every agent shares this one path and each launch rewrites it. A shell reads a
	// script as it runs, a line at a time, and takes no copy of it first. Writing over the file
	// in place would therefore hand a gh already running in another agent the second half of a
	// script it did not start. Renaming a finished file over the target puts a new inode there.
	// The running invocation keeps the old one until it exits.
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

	if err := os.Rename(temporary.Name(), path); err != nil {
		return fmt.Errorf("failed to write %q: %w", path, err)
	}

	return nil
}

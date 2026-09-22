package harness

import (
	"fmt"
	"os"
	"path/filepath"
)

// ToolsDirName is the directory under the cache dir holding the stand-ins weave puts ahead of
// an agent's PATH.
const ToolsDirName = "weave-agent-tools"

// ghShim is the stand-in an agent reaches as gh, which mints a token for the one call rather
// than leaving one in the pane to go stale.
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

GH_TOKEN=$("$weave" github token --checkout "$(pwd)") || exit 1
export GH_TOKEN
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
	body := fmt.Sprintf(ghShim, QuoteArg(dir), QuoteArg(weaveBinary))
	if err := os.WriteFile(path, []byte(body), 0o755); err != nil { //nolint:gosec // a stand-in on PATH is run, not read
		return "", fmt.Errorf("failed to write %q: %w", path, err)
	}

	return dir, nil
}

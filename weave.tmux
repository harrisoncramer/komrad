#!/usr/bin/env bash
# TPM entrypoint. Finds a weave binary and lets it bind the panel key.
set -euo pipefail

CURRENT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"

# The binding itself is `weave tmux`, in the binary, so the key is bound to the path the
# binary is actually at and this file has nothing to know about how weave was installed.
#
# An installed weave wins over one in this checkout, and the checkout is the fallback rather
# than the only candidate, which is what it used to be. Nothing is built either way: a machine
# with no Go toolchain, or a checkout that does not compile, would otherwise get a key bound
# to a path with nothing at it.
candidates=()
if bin="$(command -v weave)"; then
	candidates+=("$bin")
fi
candidates+=("$CURRENT_DIR/bin/weave")

for candidate in "${candidates[@]}"; do
	if [ -x "$candidate" ]; then
		exec "$candidate" tmux
	fi
done

# TPM sources this while tmux parses its config, where a failure is otherwise invisible.
# Said twice because neither channel is always there: stderr is shown for a run-shell with
# a pane to show it in, and the message needs a client attached to land.
message="weave: no weave binary on PATH or at $CURRENT_DIR/bin/weave, so the panel key is not bound"
echo "$message" >&2
tmux display-message "$message" 2>/dev/null || true

exit 1

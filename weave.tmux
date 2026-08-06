#!/usr/bin/env bash
# TPM entrypoint. Finds a weave binary and lets it bind the panel key.
set -euo pipefail

# The binding itself is `weave tmux`, in the binary, so the key is bound to the path the
# binary is actually at and this file has nothing to know about how weave was installed.
# It builds nothing: a machine with no Go toolchain, or a checkout that does not compile,
# would otherwise get a key bound to a path with nothing at it.
if bin="$(command -v weave)"; then
	exec "$bin" tmux
fi

# TPM sources this while tmux parses its config, where a failure is otherwise invisible.
# Said twice because neither channel is always there: stderr is shown for a run-shell with
# a pane to show it in, and the message needs a client attached to land.
message="weave: no weave binary on PATH, so the panel key is not bound"
echo "$message" >&2
tmux display-message "$message" 2>/dev/null || true

exit 1

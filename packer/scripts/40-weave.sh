#!/usr/bin/env bash
set -euo pipefail

install -m 0755 /tmp/weave /usr/local/bin/weave
rm -f /tmp/weave

/usr/local/bin/weave version

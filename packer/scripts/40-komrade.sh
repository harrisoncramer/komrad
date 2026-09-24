#!/usr/bin/env bash
set -euo pipefail

install -m 0755 /tmp/komrade /usr/local/bin/komrade
rm -f /tmp/komrade

/usr/local/bin/komrade version

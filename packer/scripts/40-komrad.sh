#!/usr/bin/env bash
set -euo pipefail

install -m 0755 /tmp/komrad /usr/local/bin/komrad
rm -f /tmp/komrad

/usr/local/bin/komrad version

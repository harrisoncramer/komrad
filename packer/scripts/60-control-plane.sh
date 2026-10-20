#!/usr/bin/env bash
set -euo pipefail

install -m 0700 /tmp/files/komrad-owner-database-url /usr/local/sbin/komrad-owner-database-url
install -m 0700 /tmp/files/komrad-control-plane-credentials /usr/local/sbin/komrad-control-plane-credentials
install -m 0644 /tmp/files/komrad-control-plane-credentials.service /etc/systemd/system/komrad-control-plane-credentials.service
install -m 0644 /tmp/files/komrad-control-plane-credentials.timer /etc/systemd/system/komrad-control-plane-credentials.timer
install -m 0644 /tmp/files/komrad-control-plane.service /etc/systemd/system/komrad-control-plane.service
install -m 0644 /tmp/files/nftables-control-plane.conf /etc/nftables.conf

systemctl enable komrad-control-plane-credentials.service
systemctl enable komrad-control-plane-credentials.timer
systemctl enable komrad-control-plane.service

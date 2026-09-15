#!/usr/bin/env bash
set -euo pipefail

export DEBIAN_FRONTEND=noninteractive

apt-get autoremove -y
apt-get clean
rm -rf /var/lib/apt/lists/* /tmp/files
cloud-init clean --logs --seed
rm -f /etc/machine-id /var/lib/dbus/machine-id
touch /etc/machine-id

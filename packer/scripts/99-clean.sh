#!/usr/bin/env bash
set -euo pipefail

export DEBIAN_FRONTEND=noninteractive

systemctl disable ssh.service ssh.socket
systemctl mask ssh.service ssh.socket

apt-get autoremove -y
apt-get clean
rm -rf /var/lib/apt/lists/* /tmp/files
cloud-init clean --logs --seed
rm -f /etc/machine-id /var/lib/dbus/machine-id
touch /etc/machine-id

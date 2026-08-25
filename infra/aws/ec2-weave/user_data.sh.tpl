#!/usr/bin/env bash
# Bootstrap for the weave EC2 instance. Runs once, as root, via cloud-init
# user-data on first boot. Gets the box to the point docs/install.md's
# quickstart starts from - tmux, a local Postgres, and the weave binary, with
# the schema migrated and `weave doctor` clean. The panel auto-opens on
# every connection via Session Manager's shellProfile preference
# (main.tf: aws_ssm_document.run_as), not anything in this script. It does
# not wire up git/gh/Claude auth - that is a later pass.
#
# Note for anyone editing this file: it is rendered through Terraform's
# templatefile(), which also uses "$${...}" for interpolation. Only
# architecture below is a real Terraform variable; every other "$" in this
# script is deliberately brace-free bash so Terraform leaves it alone.
set -euo pipefail

# shellcheck disable=SC2154 # architecture comes from Terraform's templatefile(), not bash
ARCH="${architecture}"
WEAVE_USER="ubuntu"
WEAVE_HOME="/home/$WEAVE_USER"
WEAVE_DATABASE_URL="postgres://weave:weave@localhost:5432/weave?sslmode=disable"

exec > >(tee -a /var/log/weave-bootstrap.log) 2>&1
export DEBIAN_FRONTEND=noninteractive

apt-get update
apt-get install -y --no-install-recommends ca-certificates curl gnupg tmux git jq

# Canonical's Ubuntu AMIs ship the SSM Agent as a snap by default (AWS docs:
# AMI id 20180627 or later, which 24.04 qualifies for), so this confirms it
# rather than assumes it. It's the only way in: no SSH port, no key.
if ! snap list amazon-ssm-agent >/dev/null 2>&1; then
	snap install amazon-ssm-agent --classic
fi
systemctl enable --now snap.amazon-ssm-agent.amazon-ssm-agent.service

# Ubuntu's own archive lags the Postgres major version weave tracks (see
# docker-compose.yml: postgres:18), so pull PGDG's repo instead.
install -d /usr/share/postgresql-common/pgdg
curl -fsSL https://www.postgresql.org/media/keys/ACCC4CF8.asc \
	-o /usr/share/postgresql-common/pgdg/apt.postgresql.org.asc
# Parsed rather than sourced, so shellcheck -x isn't stuck trying to open a
# file that only exists on the target Ubuntu box, not the linting machine.
VERSION_CODENAME=$(awk -F= '$1 == "VERSION_CODENAME" { print $2 }' /etc/os-release)
echo "deb [signed-by=/usr/share/postgresql-common/pgdg/apt.postgresql.org.asc] https://apt.postgresql.org/pub/repos/apt $VERSION_CODENAME-pgdg main" \
	>/etc/apt/sources.list.d/pgdg.list
apt-get update
apt-get install -y --no-install-recommends postgresql-18

# GitHub's own repo, for `gh`. Not authenticated yet - that lands with the
# secrets pass.
install -d /etc/apt/keyrings
curl -fsSL https://cli.github.com/packages/githubcli-archive-keyring.gpg \
	-o /etc/apt/keyrings/githubcli-archive-keyring.gpg
echo "deb [arch=$(dpkg --print-architecture) signed-by=/etc/apt/keyrings/githubcli-archive-keyring.gpg] https://cli.github.com/packages stable main" \
	>/etc/apt/sources.list.d/github-cli.list
apt-get update
apt-get install -y --no-install-recommends gh

# The weave binary, off the release tag that never moves (docs/install.md).
RELEASE_URL="https://github.com/harrisoncramer/weave/releases/download/tip"
curl -fsSL -o /usr/local/bin/weave "$RELEASE_URL/weave-linux-$ARCH"
curl -fsSL -o /tmp/weave-checksums.txt "$RELEASE_URL/checksums.txt"
(
	cd /usr/local/bin
	grep "weave-linux-$ARCH\$" /tmp/weave-checksums.txt | sed "s/weave-linux-$ARCH/weave/" | shasum -a 256 -c -
)
chmod +x /usr/local/bin/weave
rm -f /tmp/weave-checksums.txt

# A role and database matching docker-compose.yml's dev defaults. The
# official Postgres image makes POSTGRES_USER the cluster's actual superuser
# rather than a plain login role, which `weave readonly-role` (CREATE ROLE)
# and `weave migrate` both rely on - a plain LOGIN role can't do either.
# Postgres only listens on localhost here, so this is no more exposed than
# the same password is in the checkout's own docker-compose.
sudo -u postgres psql -v ON_ERROR_STOP=1 <<-'SQL'
	DO $$
	BEGIN
		IF NOT EXISTS (SELECT FROM pg_roles WHERE rolname = 'weave') THEN
			CREATE ROLE weave LOGIN SUPERUSER PASSWORD 'weave';
		END IF;
	END
	$$;
SQL
sudo -u postgres psql -v ON_ERROR_STOP=1 -tAc "SELECT 1 FROM pg_database WHERE datname = 'weave'" | grep -q 1 ||
	sudo -u postgres createdb -O weave weave

# /etc/environment, not .bashrc: PAM applies it to every login-shell session
# (`sudo -u ubuntu -i`, SSM, SSH), whereas Ubuntu's default .bashrc opens with
# a guard that skips the rest of the file for a non-interactive shell - which
# is exactly what `sudo -u ubuntu -i weave <command>` starts, so anything
# appended there is silently never read by scripted logins.
grep -qxF "WEAVE_DATABASE_URL=\"$WEAVE_DATABASE_URL\"" /etc/environment ||
	echo "WEAVE_DATABASE_URL=\"$WEAVE_DATABASE_URL\"" >>/etc/environment

grep -qxF "run-shell 'weave tmux'" "$WEAVE_HOME/.tmux.conf" 2>/dev/null ||
	echo "run-shell 'weave tmux'" >>"$WEAVE_HOME/.tmux.conf"
chown "$WEAVE_USER:$WEAVE_USER" "$WEAVE_HOME/.tmux.conf"

# Same sequence as docs/install.md's quickstart, run once so the box is
# provably healthy before anyone connects to it. readonly-role and migrate
# must succeed - a failure there is this script's own bug. doctor is allowed
# to report failures without killing the script: on a freshly bootstrapped
# box it correctly flags the claude/codex CLIs as missing, since installing
# and authenticating those is a later pass, not this one. Its output still
# lands in this log for a human to read.
sudo -u "$WEAVE_USER" env WEAVE_DATABASE_URL="$WEAVE_DATABASE_URL" weave readonly-role
sudo -u "$WEAVE_USER" env WEAVE_DATABASE_URL="$WEAVE_DATABASE_URL" weave migrate
sudo -u "$WEAVE_USER" env WEAVE_DATABASE_URL="$WEAVE_DATABASE_URL" weave doctor || true

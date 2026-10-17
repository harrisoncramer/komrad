#!/bin/sh
# Installs the komrad binary for this machine and runs komrad setup.
#
#     curl -fsSL https://github.com/harrisoncramer/komrad/releases/latest/download/install.sh | sh
#
# KOMRAD_VERSION picks the release, a tag such as v0.3.0 or tip, and defaults to the newest
# stable one. KOMRAD_INSTALL_DIR picks the directory, and defaults to ~/.local/bin.
set -eu

fail() {
	echo "komrad install: $*" >&2
	exit 1
}

main() {
	repo="https://github.com/harrisoncramer/komrad"
	version="${KOMRAD_VERSION:-latest}"
	dir="${KOMRAD_INSTALL_DIR:-$HOME/.local/bin}"

	case "$(uname -s)" in
	Darwin) os=darwin ;;
	Linux) os=linux ;;
	*) fail "no komrad build for $(uname -s)" ;;
	esac

	case "$(uname -m)" in
	arm64 | aarch64) arch=arm64 ;;
	x86_64 | amd64) arch=amd64 ;;
	*) fail "no komrad build for $(uname -m)" ;;
	esac

	if [ "$version" = latest ]; then
		base="$repo/releases/latest/download"
	else
		base="$repo/releases/download/$version"
	fi

	if command -v sha256sum >/dev/null 2>&1; then
		sha256="sha256sum"
	elif command -v shasum >/dev/null 2>&1; then
		sha256="shasum -a 256"
	else
		fail "neither sha256sum nor shasum is installed, so the download cannot be checked"
	fi

	binary="komrad-$os-$arch"
	tmp="$(mktemp -d)"
	trap 'rm -rf "$tmp"' EXIT

	echo "downloading $binary from $base"
	curl -fsSL -o "$tmp/$binary" "$base/$binary"
	curl -fsSL -o "$tmp/checksums.txt" "$base/checksums.txt"

	grep " $binary\$" "$tmp/checksums.txt" >"$tmp/expected" || fail "checksums.txt has no entry for $binary"
	(cd "$tmp" && $sha256 -c expected >/dev/null) || fail "$binary does not match checksums.txt"

	upgrade=false
	if [ -e "$dir/komrad" ]; then
		upgrade=true
	fi

	mkdir -p "$dir"
	chmod +x "$tmp/$binary"
	mv -f "$tmp/$binary" "$dir/komrad"
	echo "installed komrad at $dir/komrad"

	case ":$PATH:" in
	*":$dir:"*) ;;
	*) echo "$dir is not on your PATH; add it in your shell's profile" >&2 ;;
	esac

	if [ "$upgrade" = true ]; then
		echo "komrad was already installed. If it was set up, finish the upgrade with: komrad stop && komrad migrate"
		echo "If setup never finished, run: komrad setup"
		return
	fi

	if (exec </dev/tty) 2>/dev/null; then
		"$dir/komrad" setup </dev/tty
	else
		"$dir/komrad" setup --no-input --embedded-postgres
	fi
}

main "$@"

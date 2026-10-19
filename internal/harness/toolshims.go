package harness

import (
	"fmt"
	"os"
	"path/filepath"
	"slices"
	"strings"

	"komrad/config"
	"komrad/lib/xfile"
	"komrad/lib/xslice"
)

// ToolsDirName is the directory under the cache dir holding the stand-ins komrad puts ahead of
// an agent's PATH.
const ToolsDirName = "komrad-agent-tools"

// toolShim is a forge's CLI and how its stand-in hands it a credential.
type toolShim struct {
	tool string
	// presets are the variables the tool reads a token from itself, any one of which leaves it be.
	presets []string
	// handoff is the shell that asks komrad for the credential and either exports it or runs the tool with it and exits.
	handoff string
}

// toolShims are the forge CLIs an agent reaches through komrad, neither of which reads a git
// credential helper.
var toolShims = []toolShim{
	{
		tool:    "gh",
		presets: []string{"GH_TOKEN", "GITHUB_TOKEN"},
		handoff: `token=$("$komrad" github token --checkout "$(pwd)" --if-configured) || exit 1
if [ -n "$token" ]; then
	export GH_TOKEN="$token"
fi`,
	},
	{
		tool:    "glab",
		presets: []string{"GITLAB_TOKEN", "GITLAB_ACCESS_TOKEN", "OAUTH_TOKEN"},
		handoff: `config=$("$komrad" gitlab config --checkout "$(pwd)" --if-configured) || exit 1
if [ -n "$config" ]; then
	scoped=$(mktemp -d) || exit 1
	trap 'rm -rf "$scoped"' EXIT
	trap 'exit 129' HUP
	trap 'exit 130' INT
	trap 'exit 143' TERM
	(umask 077 && printf '%s\n' "$config" >"$scoped/config.yml") || exit 1
	GLAB_CONFIG_DIR=$scoped "$real" "$@"
	exit
fi`,
	},
}

// signedRedirectHosts are the hosts GitHub sends gh's downloads to, which gh reaches directly.
const signedRedirectHosts = ".blob.core.windows.net,.actions.githubusercontent.com,objects.githubusercontent.com,release-assets.githubusercontent.com,avatars.githubusercontent.com"

// forgeProxyHandoff is gh's handoff on an engine whose GitHub requests go through the forge proxy.
func forgeProxyHandoff(proxy config.ForgeProxy) string {
	return fmt.Sprintf("export GH_TOKEN=%s HTTPS_PROXY=%s SSL_CERT_FILE=%s NO_PROXY=%s",
		QuoteArg(config.ForgeProxyPlaceholder), QuoteArg(proxy.URL), QuoteArg(proxy.CAFile), QuoteArg(signedRedirectHosts))
}

// shimsFor are the stand-ins for a machine, gh's sending its requests through the forge proxy
// when there is one.
func shimsFor(proxy config.ForgeProxy) []toolShim {
	if !proxy.IsConfigured() {
		return toolShims
	}

	shims := slices.Clone(toolShims)
	for i := range shims {
		if shims[i].tool == "gh" {
			shims[i].handoff = forgeProxyHandoff(proxy)
		}
	}

	return shims
}

const shimTemplate =`#!/bin/sh
# Komrad's stand-in for {tool}, handing it a credential read for this call alone.
self={self}
komrad={komrad}

real=
IFS=:
for dir in $PATH; do
	[ -n "$dir" ] || dir=.
	if [ "$dir" != "$self" ] && [ -x "$dir/{tool}" ]; then
		real=$dir/{tool}
		break
	fi
done
unset IFS

if [ -z "$real" ]; then
	echo "komrad: {tool} is not installed on this machine" >&2
	exit 127
fi

if {presets}; then
	exec "$real" "$@"
fi

{handoff}

exec "$real" "$@"
`

func (s toolShim) script(dir, komradBinary string) string {
	presets := xslice.Map(s.presets, func(name string) string {
		return fmt.Sprintf(`[ -n "${%s:-}" ]`, name)
	})

	return strings.NewReplacer(
		"{tool}", s.tool,
		"{self}", QuoteArg(dir),
		"{komrad}", QuoteArg(komradBinary),
		"{presets}", strings.Join(presets, " || "),
		"{handoff}", s.handoff,
	).Replace(shimTemplate)
}

// WriteToolShimsParams is where the stand-ins go and what they hand their tools.
type WriteToolShimsParams struct {
	CacheDir     string
	KomradBinary string
	ForgeProxy   config.ForgeProxy
}

// WriteToolShims writes the gh and glab stand-ins under the cache dir, answering the directory
// holding them and an empty one when there is no komrad binary to point them at.
func WriteToolShims(params WriteToolShimsParams) (string, error) {
	if params.CacheDir == "" || params.KomradBinary == "" {
		return "", nil
	}

	dir := filepath.Join(params.CacheDir, ToolsDirName)
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return "", fmt.Errorf("failed to create %q: %w", dir, err)
	}

	for _, shim := range shimsFor(params.ForgeProxy) {
		if err := writeScript(filepath.Join(dir, shim.tool), shim.script(dir, params.KomradBinary)); err != nil {
			return "", err
		}
	}

	return dir, nil
}

// writeScript puts a script where a shell can run it, through a file of its own renamed over
// the target.
func writeScript(path, body string) error {
	// Explainer: every agent shares this one path and each launch rewrites it. A shell reads a
	// script as it runs, a line at a time, and takes no copy of it first. Writing over the file
	// in place would therefore hand a gh already running in another agent the second half of a
	// script it did not start. Renaming a finished file over the target puts a new inode there.
	// The running invocation keeps the old one until it exits.
	if err := xfile.WriteAtomic(path, []byte(body), 0o700); err != nil {
		return fmt.Errorf("failed to write %q: %w", path, err)
	}

	return nil
}

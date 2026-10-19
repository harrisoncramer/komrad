package forges

import (
	"os"
	"path/filepath"
	"strings"

	"komrad/config"
	"komrad/internal/forge"
	"komrad/internal/forge/github"
	"komrad/internal/forge/gitlab"
	githubapp "komrad/internal/github/app"
	forgev1 "komrad/proto/gen/forge/v1"
)

// builders makes the forge for each kind this build knows.
var builders = map[forgev1.Kind]forge.Builder{
	forgev1.Kind_KIND_GITHUB: github.Build,
	forgev1.Kind_KIND_GITLAB: gitlab.Build,
}

// TokenCachePath is where the App's minted token is kept under a cache directory.
func TokenCachePath(cacheDir string) string {
	dir := strings.TrimSpace(cacheDir)
	if dir == "" {
		return ""
	}

	if err := os.MkdirAll(dir, 0o700); err != nil {
		return ""
	}

	return filepath.Join(dir, "github-token.json")
}

// AppFrom is the GitHub App the config names, unconfigured when it names none, with its minted
// tokens kept at cachePath or nowhere when that is empty.
func AppFrom(cfg config.GitHubApp, cachePath string) (forge.App, error) {
	identity := githubapp.Identity{
		ID:             strings.TrimSpace(cfg.AppID),
		PrivateKeyPath: config.ExpandHome(strings.TrimSpace(cfg.PrivateKeyPath)),
	}
	if strings.TrimSpace(cfg.InstallationID) != "" {
		installation, err := githubapp.InstallationIDFromString(cfg.InstallationID)
		if err != nil {
			return forge.App{}, err
		}

		identity.InstallationID = installation
	}

	return forge.App{Cache: githubapp.Cache{Path: cachePath, Identity: identity}}, nil
}

// ForEngine makes the forge for each kind, sending GitHub's hosts through the forge proxy when
// the engine names one.
func ForEngine(proxy config.ForgeProxy) (map[forgev1.Kind]forge.Builder, error) {
	if !proxy.IsConfigured() {
		return builders, nil
	}

	transport, err := github.NewForgeProxyTransport(github.NewForgeProxyTransportParams{
		ProxyURL: proxy.URL,
		CAFile:   config.ExpandHome(proxy.CAFile),
	})
	if err != nil {
		return nil, err
	}

	return map[forgev1.Kind]forge.Builder{
		forgev1.Kind_KIND_GITHUB: github.BuildThrough(transport),
		forgev1.Kind_KIND_GITLAB: gitlab.Build,
	}, nil
}

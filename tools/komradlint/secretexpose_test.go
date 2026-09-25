package komradlint

import (
	"testing"

	"komradlint/config"

	"golang.org/x/tools/go/analysis/analysistest"
)

// TestSecretExpose covers a package using the forge's Secret: an Expose outside the allow list
// is a finding, an Expose on another type is not, and a Secret laundered through an interface
// is the residue the rule does not see.
func TestSecretExpose(t *testing.T) {
	analyzer := newSecretExpose(config.SecretExposeConfig{
		Allow: []string{
			"(komrad/internal/forge.Credential).HeaderValue",
			"(*komrad/internal/forge.FileStore).Set",
		},
	})

	analysistest.Run(t, analysistest.TestData(), analyzer, "komrad/secrets", "komrad/internal/forge")
}

// TestSecretExposeSkip covers the rule turned off, which reports nothing anywhere.
func TestSecretExposeSkip(t *testing.T) {
	analyzer := newSecretExpose(config.SecretExposeConfig{Skip: true})

	analysistest.Run(t, analysistest.TestData(), analyzer, "komrad/quiet")
}

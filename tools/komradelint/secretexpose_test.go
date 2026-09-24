package komradelint

import (
	"testing"

	"komradelint/config"

	"golang.org/x/tools/go/analysis/analysistest"
)

// TestSecretExpose covers a package using the forge's Secret: an Expose outside the allow list
// is a finding, an Expose on another type is not, and a Secret laundered through an interface
// is the residue the rule does not see.
func TestSecretExpose(t *testing.T) {
	analyzer := newSecretExpose(config.SecretExposeConfig{
		Allow: []string{
			"(komrade/internal/forge.Credential).HeaderValue",
			"(*komrade/internal/forge.FileStore).Set",
		},
	})

	analysistest.Run(t, analysistest.TestData(), analyzer, "komrade/secrets", "komrade/internal/forge")
}

// TestSecretExposeSkip covers the rule turned off, which reports nothing anywhere.
func TestSecretExposeSkip(t *testing.T) {
	analyzer := newSecretExpose(config.SecretExposeConfig{Skip: true})

	analysistest.Run(t, analysistest.TestData(), analyzer, "komrade/quiet")
}

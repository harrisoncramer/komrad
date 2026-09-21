package weavelint

import (
	"os"
	"path/filepath"
	"slices"
	"testing"

	"weavelint/config"

	"golang.org/x/tools/go/analysis/analysistest"
)

// TestNoMise covers the word in a comment, a string, an identifier and an environment
// variable, beside words that only contain the letters.
func TestNoMise(t *testing.T) {
	analyzer, err := newNoMise(config.NoMiseConfig{})
	if err != nil {
		t.Fatal(err)
	}

	analysistest.Run(t, analysistest.TestData(), analyzer, "nomise")
}

// TestNoMiseExclude covers a file the settings leave alone.
func TestNoMiseExclude(t *testing.T) {
	analyzer, err := newNoMise(config.NoMiseConfig{Exclude: []string{`nomiseskip/`}})
	if err != nil {
		t.Fatal(err)
	}

	analysistest.Run(t, analysistest.TestData(), analyzer, "nomiseskip")
}

// TestNoMiseSkip covers the rule turned off, which reports nothing.
func TestNoMiseSkip(t *testing.T) {
	analyzer, err := newNoMise(config.NoMiseConfig{Skip: true})
	if err != nil {
		t.Fatal(err)
	}

	analysistest.Run(t, analysistest.TestData(), analyzer, "weave/quiet")
}

// TestListPages covers the README beside pages nested under docs, with nothing else read.
func TestListPages(t *testing.T) {
	root := t.TempDir()
	for _, page := range []string{"README.md", "docs/install.md", "docs/internal/engine.md", "docs/notes.txt", "notes.md"} {
		if err := os.MkdirAll(filepath.Dir(filepath.Join(root, page)), 0o755); err != nil {
			t.Fatal(err)
		}

		if err := os.WriteFile(filepath.Join(root, page), []byte("run mise install\n"), 0o644); err != nil {
			t.Fatal(err)
		}
	}

	pages, err := listPages(root)
	if err != nil {
		t.Fatal(err)
	}

	want := []string{"README.md", "docs/install.md", "docs/internal/engine.md"}
	if !slices.Equal(pages, want) {
		t.Errorf("listed %v, want %v", pages, want)
	}
}

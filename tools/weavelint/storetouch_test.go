package weavelint

import (
	"path/filepath"
	"testing"

	"weavelint/config"

	"golang.org/x/tools/go/analysis/analysistest"
)

// TestStoreTouch covers a store over the watched tables: a write followed by a touch passes,
// through the handle or through a helper named touch, a write with none or with the touch
// before it is a finding, a write through a CTE is read
// through to the table it changes, a query built from models.New is a query all the same, and
// a read locked FOR UPDATE, a conflict arm's DO UPDATE and a write to an unwatched table are
// not writes the rule is about.
func TestStoreTouch(t *testing.T) {
	analyzer, err := newStoreTouch(config.StoreTouchConfig{
		Stores:  []string{`internal/store/`},
		Queries: filepath.Join(analysistest.TestData(), "src", "weave", "db", "queries"),
		Tables:  []string{"agents", "marks", "workflow_hooks"},
	})
	if err != nil {
		t.Fatal(err)
	}

	analysistest.Run(t, analysistest.TestData(), analyzer, "weave/internal/store/agent")
}

// TestStoreTouchOutsideStores covers a package the store patterns leave alone.
func TestStoreTouchOutsideStores(t *testing.T) {
	analyzer, err := newStoreTouch(config.StoreTouchConfig{
		Stores:  []string{`internal/store/agent/`},
		Queries: filepath.Join(analysistest.TestData(), "src", "weave", "db", "queries"),
		Tables:  []string{"agents"},
	})
	if err != nil {
		t.Fatal(err)
	}

	analysistest.Run(t, analysistest.TestData(), analyzer, "weave/internal/store/quiet")
}

// TestStoreTouchSkip covers the rule turned off, which reports nothing anywhere.
func TestStoreTouchSkip(t *testing.T) {
	analyzer, err := newStoreTouch(config.StoreTouchConfig{Skip: true, Stores: []string{`internal/store/`}, Tables: []string{"agents"}})
	if err != nil {
		t.Fatal(err)
	}

	analysistest.Run(t, analysistest.TestData(), analyzer, "weave/internal/store/quiet")
}

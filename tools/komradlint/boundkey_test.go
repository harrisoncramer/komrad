package komradlint

import (
	"testing"

	"komradlint/config"

	"golang.org/x/tools/go/analysis/analysistest"
)

// TestBoundKey covers navigation matched anywhere, an action key or an untraced slice matched outside a declared path, a general comparison checked only on keys it can trace, and the paths that may match any key.
func TestBoundKey(t *testing.T) {
	analyzer, err := newBoundKey(config.BoundKeyConfig{
		Roots: []config.BoundKeyRoot{
			{Type: "boundkey.Keybindings"},
			{Type: "boundkey.FormKeys", Prefix: "Form"},
		},
		Matchers:    []string{"boundkey.isBound", "boundkey.directionOf"},
		Compares:    []string{"slices.Contains"},
		Navigation:  []string{"Nav", "Messages.Open", "Form.Back"},
		Sources:     []string{"boundkey.model.quitKeys"},
		Dispatchers: []string{"boundkey.model.dispatch"},
		Inputs:      []string{"boundkey.model.save"},
	})
	if err != nil {
		t.Fatalf("failed to build the analyzer: %v", err)
	}

	analysistest.Run(t, analysistest.TestData(), analyzer, "boundkey")
}

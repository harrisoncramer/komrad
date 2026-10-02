package forge

import (
	"testing"

	"github.com/stretchr/testify/assert"
)

// TestNaming covers how each forge writes a pull request's number, noun and short form.
func TestNaming(t *testing.T) {
	t.Parallel()

	tests := []struct {
		kind   string
		ref    string
		noun   string
		abbrev string
	}{
		{kind: KindGitHub, ref: "#12", noun: "pull request", abbrev: "pr"},
		{kind: KindGitLab, ref: "!12", noun: "merge request", abbrev: "mr"},
		{kind: "", ref: "#12", noun: "pull request", abbrev: "pr"},
	}

	for _, tt := range tests {
		t.Run(tt.kind, func(t *testing.T) {
			t.Parallel()

			assert.Equal(t, tt.ref, Ref(tt.kind, 12))
			assert.Equal(t, tt.noun, Noun(tt.kind))
			assert.Equal(t, tt.abbrev, Abbrev(tt.kind))
		})
	}
}

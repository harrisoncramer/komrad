package forge

import (
	"testing"

	"github.com/stretchr/testify/assert"
)

// TestNaming covers how each forge writes a pull request's number and noun.
func TestNaming(t *testing.T) {
	t.Parallel()

	tests := []struct {
		kind string
		ref  string
		noun string
	}{
		{kind: KindGitHub, ref: "#12", noun: "pull request"},
		{kind: KindGitLab, ref: "!12", noun: "merge request"},
		{kind: "", ref: "#12", noun: "pull request"},
	}

	for _, tt := range tests {
		t.Run(tt.kind, func(t *testing.T) {
			t.Parallel()

			assert.Equal(t, tt.ref, Ref(tt.kind, 12))
			assert.Equal(t, tt.noun, Noun(tt.kind))
		})
	}
}

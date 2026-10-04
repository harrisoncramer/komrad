package forge

import (
	"testing"

	"github.com/stretchr/testify/assert"
)

// TestNaming covers how each forge writes a pull request's number, noun, short form and name.
func TestNaming(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name   string
		kind   string
		ref    string
		noun   string
		abbrev string
		called string
	}{
		{
			name:   "github",
			kind:   KindGitHub,
			ref:    "#12",
			noun:   "pull request",
			abbrev: "pr",
			called: "GitHub",
		},
		{
			name:   "gitlab",
			kind:   KindGitLab,
			ref:    "!12",
			noun:   "merge request",
			abbrev: "mr",
			called: "GitLab",
		},
		{
			name:   "no forge",
			kind:   "",
			ref:    "#12",
			noun:   "pull request",
			abbrev: "pr",
			called: "the forge",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			assert.Equal(t, tt.ref, Ref(tt.kind, 12))
			assert.Equal(t, tt.noun, Noun(tt.kind))
			assert.Equal(t, tt.abbrev, Abbrev(tt.kind))
			assert.Equal(t, tt.called, Name(tt.kind))
		})
	}
}

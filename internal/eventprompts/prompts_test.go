package eventprompts

import (
	"strings"
	"testing"
	"unicode/utf8"

	"github.com/stretchr/testify/assert"
)

const maxBody = 10000

// TestAppendCutsThePromptsToFit pins that prompts too long for the mail are cut rather than refused.
func TestAppendCutsThePromptsToFit(t *testing.T) {
	tests := []struct {
		name    string
		body    string
		prompt  string
		wantCut bool
	}{
		{
			name:    "prompts past the room left are cut",
			body:    strings.Repeat("x", maxBody-500),
			prompt:  strings.Repeat("a", 500),
			wantCut: true,
		},
		{
			name:   "prompts are measured in characters rather than bytes",
			body:   strings.Repeat("x", maxBody-1500),
			prompt: strings.Repeat("検", 500),
		},
		{
			name:    "and cut in characters",
			body:    strings.Repeat("é", maxBody-500),
			prompt:  strings.Repeat("検", 500),
			wantCut: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			prompts := Prompts{
				{Event: PullRequestChecksFailing, Prompt: tt.prompt},
				{Event: PullRequestBehind, Prompt: tt.prompt},
			}

			body := prompts.Append(tt.body, []string{PullRequestChecksFailing, PullRequestBehind}, maxBody)

			assert.True(t, strings.HasPrefix(body, tt.body+"\n\n"+promptsHeading), "the body komrad wrote is kept whole")
			if !tt.wantCut {
				assert.True(t, strings.HasSuffix(body, tt.prompt), "every prompt fits")

				return
			}
			assert.Equal(t, maxBody, utf8.RuneCountInString(body))
			assert.True(t, strings.HasSuffix(body, promptsCut), "and the cut is marked")
		})
	}
}

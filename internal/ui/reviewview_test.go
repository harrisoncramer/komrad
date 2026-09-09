package ui

import (
	"testing"

	"weave/internal/diff"
	"weave/internal/review"
	agentv1 "weave/proto/gen/agent/v1"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// TestTheCaptionOfARenamedThreadSaysWhatMoved covers the sentence a reader has open while they
// are looking at a comment in a file the agent renamed: the file is on screen a few rows above,
// so the caption says the code under it changed rather than that the file has gone.
func TestTheCaptionOfARenamedThreadSaysWhatMoved(t *testing.T) {
	const baseSHA = "0e1f2a3b4c5d6e7f80910a1b2c3d4e5f60718293"

	view := review.NewView(&diff.Patch{
		Root:    "/repo",
		BaseSHA: baseSHA,
		Files: []diff.File{{
			OldPath: "greet.go",
			NewPath: "hello.go",
			Hunks: []diff.Hunk{{
				Header: "@@ -1 +1 @@",
				Lines: []diff.Line{
					{Kind: diff.Context, Text: "package greet", OldNumber: 1, NewNumber: 1},
					{Kind: diff.Deletion, Text: "old", OldNumber: 2},
					{Kind: diff.Addition, Text: "new", NewNumber: 2},
				},
			}},
		}},
	}, false)

	thread := &agentv1.ReviewThread{
		Id:          "t1",
		BaseSha:     baseSHA,
		FilePath:    "greet.go",
		OldStart:    2,
		OldEnd:      2,
		QuotedLines: []string{" what line two used to say"},
	}

	view.PinThreads([]*agentv1.ReviewThread{thread})

	var m model
	m.review.view = view

	require.True(t, view.IsGhosted(thread), "the thread is drawn on a line of the renamed file")
	assert.Equal(t, "the code under this has changed · written against", m.reviewOriginSaid(thread))
}

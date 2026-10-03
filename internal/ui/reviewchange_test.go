package ui_test

import (
	"slices"
	"strings"
	"testing"

	agentv1 "komrad/proto/gen/agent/v1"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// TestReviewChangeBlock covers what the sidebar says about the change under review.
func TestReviewChangeBlock(t *testing.T) {
	t.Run("the band over the diff holds only the title and the filter", func(t *testing.T) {
		harness := openReviewAt(t, newReviewServer(t), panelHarnessParams{})

		frame := harness.readFrame()
		assert.NotContains(t, readStageText(frame), "1 file, +1 -1")
		assert.Contains(t, readSidebarProse(frame), "1 file, +1 -1")
	})

	t.Run("the change comes before the pull request and the files", func(t *testing.T) {
		harness := openReviewAt(t, newDescribedReviewServer(t, "Why it changed."), panelHarnessParams{})

		sidebar := readSidebarProse(harness.readFrame())
		scope, pullRequest, files := strings.Index(sidebar, "all changes"), strings.Index(sidebar, "#7"), strings.Index(sidebar, "▸ greet.go")
		require.NotEqual(t, -1, scope)
		assert.Less(t, scope, pullRequest)
		assert.Less(t, pullRequest, files)
	})

	t.Run("an unsent comment names the key that sends it", func(t *testing.T) {
		server := newReviewServer(t)
		server.threads = []*agentv1.ReviewThread{newThreadOnLastLine(newUnsentComment())}
		harness := openReviewAt(t, server, panelHarnessParams{})

		assert.Contains(t, readSidebarProse(harness.readFrame()), "s sends 1 comment to the agent")
	})

	t.Run("the narrow layout puts the change at the top of the sidebar", func(t *testing.T) {
		harness := openReviewAt(t, newReviewServer(t), panelHarnessParams{width: 45})

		frame := harness.readFrame()
		band, scope, files := strings.Index(frame, "Review · auth"), strings.Index(frame, "all changes"), strings.Index(frame, "▸ greet.go")
		require.NotEqual(t, -1, scope)
		assert.Less(t, band, scope)
		assert.Less(t, scope, files)
	})
}

// TestReviewChangeFolds covers the change block giving up rows before the pull request does.
func TestReviewChangeFolds(t *testing.T) {
	tests := []struct {
		name    string
		height  int
		threads []*agentv1.ReviewThread
		rows    []string
	}{
		{
			name:    "a tall pane gives each part a row of its own",
			height:  40,
			threads: []*agentv1.ReviewThread{newThreadOnLastLine(newUnsentComment(), newSentComment("m2"))},
			rows:    []string{"all changes", "1 file, +1 -1", "1 unsent · 1 waiting", "s sends 1 comment to the agent"},
		},
		{
			name:    "a shorter pane joins the delivery to the stat",
			height:  24,
			threads: []*agentv1.ReviewThread{newThreadOnLastLine(newUnsentComment(), newSentComment("m2"))},
			rows:    []string{"all changes", "1 file, +1 -1 · 1 unsent · 1 waiting", "s sends 1 comment to the agent"},
		},
		{
			name:   "a short pane joins the scope, the stat and the delivery",
			height: 16,
			rows:   []string{"all changes · 1 file, +1 -1"},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			server := newDescribedReviewServer(t, "Why it changed.\n\nline two\n\nline three\n\nline four")
			server.threads = tt.threads
			harness := openReviewAt(t, server, panelHarnessParams{height: tt.height})

			sidebar := readSidebar(harness.readFrame())
			top := slices.Index(sidebar, tt.rows[0])
			require.NotEqual(t, -1, top)
			assert.Equal(t, append(slices.Clone(tt.rows), ""), sidebar[top:min(top+len(tt.rows)+1, len(sidebar))])
			assert.Contains(t, sidebar, "#7 fix the greeting")
		})
	}
}

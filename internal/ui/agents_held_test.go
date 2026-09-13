package ui

import (
	"testing"
	"time"

	agentstore "weave/internal/store/agent"

	"github.com/stretchr/testify/assert"
)

// Explainer: a hold is taken on one reading, so an agent that was about to start a turn reads
// like one that has stopped, and a pane that goes unreadable afterwards says nothing about
// whether it stopped at all. The row admits both rather than letting go on the person's behalf.

// TestAHeldRowSaysWhichThingIsTrue pins what a held row draws in each case.
func TestAHeldRowSaysWhichThingIsTrue(t *testing.T) {
	at := time.Now()

	held := func(state agentstore.State) agentItem {
		return agentItem{agent: agentstore.Agent{
			Name:   "telescope",
			State:  state,
			HeldAt: &at,
		}, alive: true}
	}

	tests := []struct {
		name  string
		item  agentItem
		wants string
	}{
		{name: "at rest, which is what a hold claims", item: held(agentstore.StateIdle), wants: heldNotice},
		{name: "writing again", item: held(agentstore.StateThinking), wants: heldWorkingNotice},
		{name: "stopped at a prompt anybody could answer", item: held(agentstore.StateWaiting), wants: heldPromptedNotice},
		{name: "a pane nobody can read", item: held(agentstore.StateUncertain), wants: heldUnreadableNotice},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			said := noticeTexts(agentNotices(tt.item))

			assert.Contains(t, said, tt.wants)
			assert.Len(t, heldSaid(said), 1, "a row says one thing about a hold, not two")
		})
	}
}

// heldSaid is the notices about the hold itself, which is what a row may only say once.
func heldSaid(said []string) []string {
	kept := make([]string, 0, len(said))

	for _, one := range said {
		if one == heldNotice || one == heldWorkingNotice || one == heldPromptedNotice || one == heldUnreadableNotice {
			kept = append(kept, one)
		}
	}

	return kept
}

// noticeTexts is what a row's notices say, without their icons.
func noticeTexts(notices []notice) []string {
	said := make([]string, 0, len(notices))
	for _, n := range notices {
		said = append(said, n.text)
	}

	return said
}

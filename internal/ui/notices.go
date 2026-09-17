package ui

import (
	"fmt"

	agentstore "weave/internal/store/agent"
	reviewstore "weave/internal/store/review"
	"weave/internal/ui/paint"
)

// notice is one of the marks a row draws said in words, carrying the part it plays rather than a
// colour, since the region it is drawn in decides that.
type notice struct {
	icon string
	tone paint.Tone
	text string
}

const (
	failedToCreateNotice  = "setup failed, it never launched"
	failedToCleanupNotice = "teardown failed, its directory is still there"
	failedRunNotice       = "a script weave fired failed, log unread"
	unreadMailNotice      = "mail waiting, it has not read it"
	worktreeRemovedNotice = "its directory no longer exists"
	sequenceNotice        = "a rebase is stopped part way in it"
	unviewedNotice        = "unread output in its pane"
	hiddenNotice          = "hidden from the sidebar"
	heldNotice            = "paused: you stopped it and have its worktree"
	heldWorkingNotice     = "paused, but it has started working again"
	heldUnreadableNotice  = "paused, but its pane cannot be read, so it may be writing"
	heldPromptedNotice    = "paused, but stopped at a prompt, so a write lands if anybody answers"
)

const inLocalReviewNotice = "in review, reviewer owed an answer"

// localVerdictNotice says where the last released local review of an agent's newest round left
// it, and says nothing for a verdict that draws nothing.
func localVerdictNotice(verdict string) string {
	switch reviewstore.Verdict(verdict) {
	case reviewstore.VerdictApproved:
		return "local reviewer approved this round"
	case reviewstore.VerdictChangesRequested:
		return "local reviewer asked for changes"
	case reviewstore.VerdictCommented:
		return ""
	}

	return ""
}

// localReviewRoundsNotice counts against the agent holding the diff.
func localReviewRoundsNotice(rounds int32) string {
	if rounds == 1 {
		return "reviewed once"
	}

	return fmt.Sprintf("reviewed %d times", rounds)
}

// agentNotices orders the marks by how much each wants a person's attention.
func agentNotices(item agentItem) []notice {
	state := displayState(item)

	notices := failureNotices(state)
	notices = appendNotice(notices, item.failedRun, failedRunIcon, paint.ToneDanger, failedRunNotice)
	notices = appendNotice(notices, item.unreadMail, unreadMailIcon, paint.ToneAccent, unreadMailNotice)
	notices = appendNotice(notices, item.agent.WorktreeRemoved && state != agentstore.StateFailedToCreate, worktreeRemovedIcon, paint.ToneDanger, worktreeRemovedNotice)
	notices = appendNotice(notices, !item.agent.WorktreeRemoved && item.agent.SequenceInProgress && state != agentstore.StateFailedToCreate, sequenceInProgressIcon, paint.ToneWarning, sequenceNotice)
	notices = appendNotice(notices, item.unviewed, unviewedIcon, paint.ToneSuccess, unviewedNotice)
	if held, ok := heldFact(item); ok {
		notices = append(notices, held)
	}

	notices = appendNotice(notices, item.agent.Hidden, hiddenIcon, paint.ToneMuted, hiddenNotice)

	icon, tone := localVerdictIcon(item.localVerdict)
	said := localVerdictNotice(item.localVerdict)
	if icon == "" || said == "" {
		notices = appendNotice(notices, item.inLocalReview, inLocalReviewIcon, paint.ToneAccent, inLocalReviewNotice)
	}

	notices = appendNotice(notices, icon != "" && said != "", icon, tone, said)

	return appendNotice(notices, item.localRounds > 0, localReviewRoundsIcon, paint.ToneMuted, localReviewRoundsNotice(item.localRounds))
}

// failureNotices is the whole of what an archived row has to say.
func failureNotices(state agentstore.State) []notice {
	notices := appendNotice(nil, state == agentstore.StateFailedToCreate, stateIcon(state), stateTone(state), failedToCreateNotice)

	return appendNotice(notices, state == agentstore.StateFailedToCleanup, stateIcon(state), stateTone(state), failedToCleanupNotice)
}

func appendNotice(notices []notice, show bool, icon string, tone paint.Tone, text string) []notice {
	if !show {
		return notices
	}

	return append(notices, notice{
		icon: icon,
		tone: tone,
		text: text,
	})
}

// Explainer: a hold is taken on one reading of a pane, and an agent that was about to start a
// turn reads exactly like one that has stopped. The reconciler goes on reading that pane every
// few seconds, so weave finds out, and what it does about it is say so rather than quietly let
// go of a tree somebody has an editor open on. A pane that has since gone unreadable is its own
// answer: it says nothing about whether the agent stopped, which is a different thing to tell
// somebody with an editor open than that it did.

// heldFact is what the row has to say about an agent somebody is holding.
func heldFact(item agentItem) (notice, bool) {
	if item.agent.HeldAt == nil {
		return notice{}, false
	}

	if item.alive {
		switch item.agent.State {
		case agentstore.StateThinking:
			return notice{
				icon: heldIcon,
				tone: paint.ToneDanger,
				text: heldWorkingNotice,
			}, true
		case agentstore.StateWaiting:
			return notice{
				icon: heldIcon,
				tone: paint.ToneDanger,
				text: heldPromptedNotice,
			}, true
		case agentstore.StateUncertain:
			return notice{
				icon: heldIcon,
				tone: paint.ToneWarning,
				text: heldUnreadableNotice,
			}, true
		case agentstore.StateIdle, agentstore.StateSettingUp,
			agentstore.StateFailedToCreate, agentstore.StateFailedToCleanup:
		}
	}

	return notice{
		icon: heldIcon,
		tone: paint.ToneWarning,
		text: heldNotice,
	}, true
}

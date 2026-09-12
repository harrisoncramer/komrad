package scenario

import (
	"time"

	agentstore "weave/internal/store/agent"
	agentv1 "weave/proto/gen/agent/v1"
	workflowv1 "weave/proto/gen/workflow/v1"
)

// reviewAgentID is the agent whose diff the review scenario is about.
const reviewAgentID = "agent-telescope"

// Review is one agent part way through a change, with three rounds behind it and a
// conversation on the current one.
func Review() Scenario {
	return Scenario{
		Name:      "review",
		Blurb:     "one agent's diff, with rounds behind it and comments on the current one",
		NeedsRepo: true,
		Workflows: []*workflowv1.Workflow{
			{
				Id:       "wf-review",
				Name:     "review flow",
				Cwd:      "~/weave",
				Harness:  "claude",
				Worktree: &workflowv1.WorktreeConfig{Enabled: true, BaseBranch: "main"},
			},
		},
		Agents:  reviewCast(),
		Rounds:  reviewRounds(),
		Reviews: reviewBatches(),
		Threads: reviewThreads(),
		Viewed: []*agentv1.ReviewViewedFile{
			{RoundId: "round-3", FilePath: "internal/ui/reviewband.go"},
		},
	}
}

func reviewCast() []*agentv1.Agent {
	return []*agentv1.Agent{
		{
			Id:             reviewAgentID,
			Name:           "telescope",
			Prompt:         "make the review screen say which mode it is in",
			PaneId:         "%10",
			State:          string(agentstore.StateThinking),
			Harness:        "claude",
			WorkflowId:     "wf-review",
			SessionId:      "session-telescope",
			Alive:          true,
			Branch:         ptr("agent/telescope"),
			CreatedAt:      ago(4 * time.Hour),
			StateChangedAt: ago(90 * time.Second),
			Metadata:       []*agentv1.AgentMetadata{{Key: "branch", Value: "agent/telescope"}},
		},
		{
			Id:             "agent-treesitter",
			Name:           "treesitter",
			Prompt:         "settle the review on the band",
			PaneId:         "%11",
			State:          string(agentstore.StateIdle),
			Harness:        "claude",
			WorkflowId:     "wf-review",
			SessionId:      "session-treesitter",
			Alive:          true,
			Branch:         ptr("agent/treesitter"),
			CreatedAt:      ago(6 * time.Hour),
			StateChangedAt: ago(22 * time.Minute),
		},
	}
}

// reviewRounds are three snapshots of the same branch, whose shas are filled in once the
// repository behind them exists.
func reviewRounds() []*agentv1.ReviewRound {
	return []*agentv1.ReviewRound{
		{
			Id:        "round-1",
			AgentId:   reviewAgentID,
			Ordinal:   1,
			CreatedAt: ago(3 * time.Hour),
		},
		{
			Id:        "round-2",
			AgentId:   reviewAgentID,
			Ordinal:   2,
			CreatedAt: ago(80 * time.Minute),
		},
		{
			Id:        "round-3",
			AgentId:   reviewAgentID,
			Ordinal:   3,
			CreatedAt: ago(6 * time.Minute),
		},
		// The idle agent's own round, which is where editing can be tried.
		{
			Id:        "round-t",
			AgentId:   "agent-treesitter",
			Ordinal:   3,
			CreatedAt: ago(20 * time.Minute),
		},
	}
}

// reviewBatches are the two reviews already sent.
func reviewBatches() []*agentv1.Review {
	return []*agentv1.Review{
		{
			Id:           "batch-1",
			AgentId:      reviewAgentID,
			RoundId:      "round-1",
			RoundOrdinal: 1,
			AuthorKind:   agentv1.ReviewAuthorKind_REVIEW_AUTHOR_KIND_HUMAN,
			Verdict:      agentv1.ReviewVerdict_REVIEW_VERDICT_CHANGES_REQUESTED,
			ReleasedAt:   ago(2 * time.Hour),
			CreatedAt:    ago(3 * time.Hour),
			Body:         "the mode line is the right idea, but it reads as a label rather than as a state",
		},
		{
			Id:           "batch-2",
			AgentId:      reviewAgentID,
			RoundId:      "round-2",
			RoundOrdinal: 2,
			AuthorKind:   agentv1.ReviewAuthorKind_REVIEW_AUTHOR_KIND_HUMAN,
			Verdict:      agentv1.ReviewVerdict_REVIEW_VERDICT_CHANGES_REQUESTED,
			ReleasedAt:   ago(70 * time.Minute),
			CreatedAt:    ago(75 * time.Minute),
		},
	}
}

func reviewThreads() []*agentv1.ReviewThread {
	return []*agentv1.ReviewThread{
		{
			Id:           "thread-1",
			AgentId:      reviewAgentID,
			ReviewId:     "batch-2",
			RoundId:      "round-2",
			RoundOrdinal: 2,
			FilePath:     "internal/ui/reviewband.go",
			OldStart:     41,
			OldEnd:       41,
			NewStart:     41,
			NewEnd:       41,
			QuotedLines:  []string{"-\t\tm.reviewModeView(),"},
			CreatedAt:    ago(75 * time.Minute),
			UpdatedAt:    ago(60 * time.Minute),
			Messages: []*agentv1.ReviewMessage{
				{
					Id:          "message-1",
					ThreadId:    "thread-1",
					AuthorKind:  agentv1.ReviewAuthorKind_REVIEW_AUTHOR_KIND_HUMAN,
					Body:        "the row reads the mode twice, once here and once in the view it calls",
					Delivery:    agentv1.ReviewDeliveryState_REVIEW_DELIVERY_STATE_DELIVERED,
					SentAt:      ago(70 * time.Minute),
					DeliveredAt: ago(69 * time.Minute),
					CreatedAt:   ago(75 * time.Minute),
				},
				{
					Id:            "message-2",
					ThreadId:      "thread-1",
					AuthorKind:    agentv1.ReviewAuthorKind_REVIEW_AUTHOR_KIND_AGENT,
					AuthorAgentId: ptr(reviewAgentID),
					Body:          "hoisted it into one read and handed it down, so the row asks once",
					CreatedAt:     ago(60 * time.Minute),
				},
			},
			ResolvedAt: ago(58 * time.Minute),
		},
		{
			Id:           "thread-2",
			AgentId:      reviewAgentID,
			ReviewId:     "batch-3",
			RoundId:      "round-3",
			RoundOrdinal: 3,
			FilePath:     "internal/ui/reviewmode.go",
			OldStart:     0,
			OldEnd:       0,
			NewStart:     26,
			NewEnd:       28,
			QuotedLines:  []string{"+\tcase modeEditing:"},
			CreatedAt:    ago(4 * time.Minute),
			UpdatedAt:    ago(4 * time.Minute),
			Messages: []*agentv1.ReviewMessage{
				{
					Id:         "message-3",
					ThreadId:   "thread-2",
					AuthorKind: agentv1.ReviewAuthorKind_REVIEW_AUTHOR_KIND_HUMAN,
					Body:       "say what the editor is holding rather than that it is open",
					Delivery:   agentv1.ReviewDeliveryState_REVIEW_DELIVERY_STATE_UNSENT,
					CreatedAt:  ago(4 * time.Minute),
				},
			},
		},
		{
			Id:            "thread-3",
			AgentId:       reviewAgentID,
			ReviewId:      "batch-2",
			RoundId:       "round-3",
			RoundOrdinal:  3,
			FilePath:      "internal/ui/reviewmode.go",
			OldStart:      0,
			OldEnd:        0,
			NewStart:      40,
			NewEnd:        40,
			QuotedLines:   []string{"+func (m model) reviewWatchingView() string {"},
			CreatedAt:     ago(50 * time.Minute),
			UpdatedAt:     ago(3 * time.Minute),
			UnreadByHuman: true,
			Messages: []*agentv1.ReviewMessage{
				{
					Id:          "message-4",
					ThreadId:    "thread-3",
					AuthorKind:  agentv1.ReviewAuthorKind_REVIEW_AUTHOR_KIND_HUMAN,
					Body:        "this is four returns deep, does the held case belong here at all?",
					Delivery:    agentv1.ReviewDeliveryState_REVIEW_DELIVERY_STATE_DELIVERED,
					SentAt:      ago(48 * time.Minute),
					DeliveredAt: ago(47 * time.Minute),
					CreatedAt:   ago(50 * time.Minute),
				},
				{
					Id:            "message-5",
					ThreadId:      "thread-3",
					AuthorKind:    agentv1.ReviewAuthorKind_REVIEW_AUTHOR_KIND_AGENT,
					AuthorAgentId: ptr(reviewAgentID),
					Body:          "it is the one case that is about somebody else, so I left it and named it",
					CreatedAt:     ago(3 * time.Minute),
				},
			},
		},
	}
}

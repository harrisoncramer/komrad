package agents

import (
	"context"
	"errors"
	"fmt"

	"weave/internal/review"
	reviewstore "weave/internal/store/review"
)

// HarvestedBody is what a thread weave read off the round's checkout says, which is attribution
// rather than prose because the suggestion beside it already shows what the edit was.
const HarvestedBody = "The person reviewing wrote this edit themselves. Apply it as written; " +
	"if it no longer applies because the code moved, say so in this thread rather than half-applying it."

// harvestRunCeiling is how many runs one harvest may write before weave stops and asks, which is
// the number a formatter or a generator run in the checkout goes far past and a person does not.
const harvestRunCeiling = 25

// harvestFileCeiling is the same for files, since a generator rewrites few lines in very many
// files and would otherwise slip under the run ceiling.
const harvestFileCeiling = 8

// ErrHarvestTooLarge is a harvest that would open more comments than a person plausibly wrote,
// which is what a format-on-save or a generator run in the round's checkout looks like.
var ErrHarvestTooLarge = errors.New("that edit is too large to read as review comments")

type HarvestRoundEditsParams struct {
	AgentID string
	RoundID string
	// Runs are the edited runs read off the checkout, anchored by the panel because the line
	// numbers and quoted lines a thread is pinned by come from the patch.
	Runs []review.Run
	// Force writes a harvest that went past the ceiling, which is the reviewer answering for it.
	Force bool
}

// HarvestResult is what one harvest did, which is what the panel says back to the reviewer.
type HarvestResult struct {
	Opened  int
	Moved   int
	Deleted int
	// Skipped are runs written over a comment that has already gone out, which weave leaves
	// where they are rather than rewriting somebody's released words.
	Skipped []review.Run
}

// HarvestRoundEdits turns what the reviewer wrote in the round's checkout into review threads,
// reconciling against the threads an earlier harvest of the same round already wrote.
func (s *ReviewService) HarvestRoundEdits(
	ctx context.Context,
	params HarvestRoundEditsParams,
) (*HarvestResult, error) {
	round, err := s.rounds.GetRound(ctx, params.RoundID)
	if err != nil {
		return nil, err
	}

	if round.AgentID != params.AgentID {
		return nil, fmt.Errorf("round %q is not a round of agent %q", params.RoundID, params.AgentID)
	}

	if err := withinHarvestCeiling(params); err != nil {
		return nil, err
	}

	pinned, frozen, err := s.harvestGround(ctx, params.AgentID, params.RoundID)
	if err != nil {
		return nil, err
	}

	plan := review.Reconcile(params.Runs, pinned, frozen)

	// Explainer: The writes are not one transaction, since the service reaches the store through
	// narrow interfaces that hold no pool. A harvest that dies part way leaves the round holding
	// some of the answer and the next harvest reconciles the rest, which is what reconciling is
	// for.
	result := HarvestResult{Skipped: plan.Skipped}

	for _, move := range plan.Move {
		if err := s.moveHarvested(ctx, move); err != nil {
			return nil, err
		}

		result.Moved++
	}

	for _, run := range plan.Open {
		if _, err := s.CreateReviewThread(ctx, createFromRun(params, run)); err != nil {
			return nil, err
		}

		result.Opened++
	}

	for _, id := range plan.Delete {
		if err := s.threads.DeleteThread(ctx, id); err != nil {
			return nil, err
		}

		result.Deleted++
	}

	return &result, nil
}

// harvestGround is the two sets a harvest reconciles against: the threads it may move or delete,
// and the threads it must leave alone and protect what touches them.
func (s *ReviewService) harvestGround(
	ctx context.Context,
	agentID, roundID string,
) (pinned []review.Pinned, frozen []review.Frozen, err error) {
	harvested, err := s.threads.ListHarvested(ctx, agentID, roundID)
	if err != nil {
		return nil, nil, err
	}

	ids := make([]string, 0, len(harvested))
	for _, thread := range harvested {
		ids = append(ids, thread.ID)
	}

	said, err := s.messages.ListMessages(ctx, ids)
	if err != nil {
		return nil, nil, err
	}

	// The lines a harvested thread stands for live on its opening message rather than on the
	// thread, so the comparison that decides whether a run has changed needs them read back.
	suggestion := map[string][]string{}
	for _, message := range said {
		if _, seen := suggestion[message.ThreadID]; !seen {
			suggestion[message.ThreadID] = message.Suggestion
		}
	}

	own := make(map[string]bool, len(harvested))
	for _, thread := range harvested {
		// A harvested thread somebody has answered in is a conversation, so it stops being
		// weave's to rewrite the moment there is a second message in it.
		if thread.MessageCount > 1 {
			continue
		}

		own[thread.ID] = true

		pinned = append(pinned, review.Pinned{
			ID:       thread.ID,
			FilePath: thread.FilePath,
			NewStart: int(thread.NewStart),
			NewEnd:   int(thread.NewEnd),
			Lines:    suggestion[thread.ID],
		})
	}

	all, err := s.threads.ListThreads(ctx, agentID)
	if err != nil {
		return nil, nil, err
	}

	for _, thread := range all {
		if thread.RoundID != roundID || own[thread.ID] {
			continue
		}

		frozen = append(frozen, review.Frozen{
			FilePath: thread.FilePath,
			NewStart: int(thread.NewStart),
			NewEnd:   int(thread.NewEnd),
		})
	}

	return pinned, frozen, nil
}

// moveHarvested repins a thread onto the run it now stands for and rewrites the suggestion it
// carries, which are two writes because the range is the thread's and the lines are its message's.
func (s *ReviewService) moveHarvested(ctx context.Context, move review.Move) error {
	if _, err := s.threads.MoveThread(ctx, reviewstore.MoveThreadParams{
		ID:          move.ID,
		OldStart:    int32(move.Run.OldStart),
		OldEnd:      int32(move.Run.OldEnd),
		NewStart:    int32(move.Run.NewStart),
		NewEnd:      int32(move.Run.NewEnd),
		QuotedLines: move.Run.QuotedLines,
	}); err != nil {
		return err
	}

	messages, err := s.messages.ListMessages(ctx, []string{move.ID})
	if err != nil {
		return err
	}

	if len(messages) == 0 {
		return nil
	}

	_, err = s.messages.UpdateMessage(ctx, messages[0].ID, HarvestedBody, move.Run.Lines)

	return err
}

func createFromRun(params HarvestRoundEditsParams, run review.Run) CreateReviewThreadParams {
	return CreateReviewThreadParams{
		AgentID:     params.AgentID,
		RoundID:     params.RoundID,
		Harvested:   true,
		FilePath:    run.FilePath,
		OldStart:    int32(run.OldStart),
		OldEnd:      int32(run.OldEnd),
		NewStart:    int32(run.NewStart),
		NewEnd:      int32(run.NewEnd),
		QuotedLines: run.QuotedLines,
		Suggestion:  run.Lines,
		Body:        HarvestedBody,
		Author:      ReviewAuthor{Kind: reviewstore.AuthorHuman},
	}
}

// withinHarvestCeiling refuses a harvest big enough to be a formatter or a generator rather than
// a person, unless the reviewer has said to write it anyway.
func withinHarvestCeiling(params HarvestRoundEditsParams) error {
	if params.Force {
		return nil
	}

	files := map[string]bool{}
	for _, run := range params.Runs {
		files[run.FilePath] = true
	}

	if len(params.Runs) > harvestRunCeiling {
		return fmt.Errorf("%w: %d runs", ErrHarvestTooLarge, len(params.Runs))
	}

	if len(files) > harvestFileCeiling {
		return fmt.Errorf("%w: %d files", ErrHarvestTooLarge, len(files))
	}

	return nil
}

package reviews

import (
	"context"
	"errors"
	"fmt"

	"komrad/internal/fail"
	"komrad/internal/store"
	reviewstore "komrad/internal/store/review"
)

// errForgeMessageNotHeld answers a person who accepts a message the agent can already read.
var errForgeMessageNotHeld = fail.Precondition("only a held forge comment needs accepting")

// ErrForgeMessageAccepted answers a person who accepts a forge comment a second time.
var ErrForgeMessageAccepted = fail.AlreadyExists("that forge comment is already accepted")

// errForgeRootHeld answers a person who accepts a held reply in a thread whose held first
// comment is not yet accepted, which the agent could not read the reply alongside.
var errForgeRootHeld = fail.Precondition("this thread opens with a held forge comment, so accept that first")

// AcceptForgeMessageParams is the held forge message the person is passing on to the agent.
type AcceptForgeMessageParams struct {
	MessageID string
	Author    ReviewAuthor
}

// AcceptForgeMessage copies a held forge comment into the person's open review, which releases it
// to the agent as their own words once the review is sent, and reopens its thread if settled.
func (s *ReviewService) AcceptForgeMessage(
	ctx context.Context,
	params AcceptForgeMessageParams,
) (*reviewstore.Message, error) {
	if !params.Author.isHuman() {
		return nil, ErrReviewHumanOnly
	}

	message, err := s.reviews.GetMessage(ctx, params.MessageID)
	if errors.Is(err, store.ErrNotFound) {
		return nil, fmt.Errorf("%w: %q", ErrReviewMessageNotFound, params.MessageID)
	}
	if err != nil {
		return nil, err
	}
	if message.AuthorKind != reviewstore.AuthorForge || !message.Held {
		return nil, fmt.Errorf("%w: %q", errForgeMessageNotHeld, params.MessageID)
	}

	messages, err := s.reviews.ListMessages(ctx, []string{message.ThreadID})
	if err != nil {
		return nil, err
	}
	if err := requireAcceptable(*message, messages); err != nil {
		return nil, err
	}

	thread, err := s.reviews.GetThread(ctx, message.ThreadID)
	if err != nil {
		return nil, err
	}

	open, err := s.openReviewFor(ctx, openReviewForParams{
		TaskID: thread.TaskID,
		Author: params.Author,
	})
	if err != nil {
		return nil, err
	}

	copied, err := s.reviews.CreateMessage(ctx, reviewstore.CreateMessageParams{
		ThreadID:     thread.ID,
		ReviewID:     &open.ID,
		Suggestion:   message.Suggestion,
		AuthorKind:   params.Author.Kind,
		Body:         message.Body,
		AcceptedFrom: &message.ID,
	})
	if err != nil {
		return nil, err
	}

	if thread.ResolvedByKind != "" {
		if _, err := s.reviews.SetThreadResolved(ctx, reviewstore.SetThreadResolvedParams{
			ID:       thread.ID,
			Resolved: false,
			ByKind:   params.Author.Kind,
		}); err != nil {
			return nil, err
		}
	}

	return copied, nil
}

// requireAcceptable refuses a forge message already accepted, or a reply whose held first message
// has no copy yet.
func requireAcceptable(message reviewstore.Message, thread []reviewstore.Message) error {
	root := message
	for _, other := range thread {
		if other.ID < root.ID {
			root = other
		}
	}

	rootAccepted := !root.Held
	for _, other := range thread {
		if other.AcceptedFrom == nil {
			continue
		}
		if *other.AcceptedFrom == message.ID {
			return fmt.Errorf("%w: %q", ErrForgeMessageAccepted, message.ID)
		}
		if *other.AcceptedFrom == root.ID {
			rootAccepted = true
		}
	}

	if root.ID != message.ID && !rootAccepted {
		return fmt.Errorf("%w: %q", errForgeRootHeld, message.ID)
	}

	return nil
}

package workflow

import (
	"context"
	"fmt"

	"komrad/db/models"
	"komrad/lib/protoenum"
	eventv1 "komrad/proto/gen/event/v1"

	"github.com/google/uuid"
)

// EventPrompt is what a workflow asks of its agents when komrad sends them one event.
type EventPrompt struct {
	Event  eventv1.Event
	Prompt string
	Muted  bool
}

func (s *WorkflowStore) setEventPrompts(ctx context.Context, workflowID uuid.UUID, prompts []EventPrompt) error {
	if err := s.queries.DeleteWorkflowEventPrompts(ctx, workflowID); err != nil {
		return fmt.Errorf("failed to clear prompts for workflow %q: %w", workflowID, err)
	}

	for position, prompt := range prompts {
		if err := s.queries.InsertWorkflowPrompt(ctx, models.InsertWorkflowPromptParams{
			WorkflowID: workflowID,
			Event:      protoenum.Name(prompt.Event),
			Prompt:     prompt.Prompt,
			Position:   int32(position),
			Muted:      prompt.Muted,
		}); err != nil {
			return fmt.Errorf("failed to add the %s prompt to workflow %q: %w", protoenum.Name(prompt.Event), workflowID, err)
		}
	}

	return nil
}

func (s *WorkflowStore) eventPromptsFor(ctx context.Context, workflowID uuid.UUID) ([]EventPrompt, error) {
	rows, err := s.queries.ListEventPromptsForWorkflow(ctx, workflowID)
	if err != nil {
		return nil, fmt.Errorf("failed to read the prompts of workflow %q: %w", workflowID, err)
	}

	prompts := make([]EventPrompt, 0, len(rows))
	for _, row := range rows {
		prompt, err := eventPromptFromRow(row.Event, row.Prompt, row.Muted)
		if err != nil {
			return nil, fmt.Errorf("failed to read a prompt of workflow %q: %w", workflowID, err)
		}
		prompts = append(prompts, *prompt)
	}

	return prompts, nil
}

func (s *WorkflowStore) eventPromptsForWorkflows(ctx context.Context, ids []uuid.UUID) (map[uuid.UUID][]EventPrompt, error) {
	if len(ids) == 0 {
		return map[uuid.UUID][]EventPrompt{}, nil
	}

	rows, err := s.queries.ListEventPromptsForWorkflows(ctx, ids)
	if err != nil {
		return nil, fmt.Errorf("failed to read the prompts of %d workflows: %w", len(ids), err)
	}

	prompts := make(map[uuid.UUID][]EventPrompt)
	for _, row := range rows {
		prompt, err := eventPromptFromRow(row.Event, row.Prompt, row.Muted)
		if err != nil {
			return nil, fmt.Errorf("failed to read a prompt of workflow %q: %w", row.WorkflowID, err)
		}
		prompts[row.WorkflowID] = append(prompts[row.WorkflowID], *prompt)
	}

	return prompts, nil
}

// eventPromptFromRow reads a stored prompt.
func eventPromptFromRow(event, prompt string, muted bool) (*EventPrompt, error) {
	parsed, err := protoenum.Parse[eventv1.Event](event)
	if err != nil {
		return nil, err
	}

	return &EventPrompt{Event: parsed, Prompt: prompt, Muted: muted}, nil
}

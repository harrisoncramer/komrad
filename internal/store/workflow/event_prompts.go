package workflow

import (
	"context"
	"fmt"

	"komrad/db/models"
	"komrad/lib/xslice"
)

// EventPrompt is what a workflow asks of its agents when komrad sends them one event.
type EventPrompt struct {
	Event  string
	Prompt string
}

// GetEventPrompt is the prompt the workflow attaches to an event, and the empty string when it attaches none.
func (w Workflow) GetEventPrompt(event string) string {
	for _, prompt := range w.EventPrompts {
		if prompt.Event == event {
			return prompt.Prompt
		}
	}

	return ""
}

func (s *WorkflowStore) setEventPrompts(ctx context.Context, workflowID string, prompts []EventPrompt) error {
	if err := s.queries.DeleteWorkflowEventPrompts(ctx, workflowID); err != nil {
		return fmt.Errorf("failed to clear prompts for workflow %q: %w", workflowID, err)
	}

	for position, prompt := range prompts {
		if err := s.queries.InsertWorkflowPrompt(ctx, models.InsertWorkflowPromptParams{
			WorkflowID: workflowID,
			Event:      prompt.Event,
			Prompt:     prompt.Prompt,
			Position:   int32(position),
		}); err != nil {
			return fmt.Errorf("failed to add the %s prompt to workflow %q: %w", prompt.Event, workflowID, err)
		}
	}

	return nil
}

func (s *WorkflowStore) eventPromptsFor(ctx context.Context, workflowID string) ([]EventPrompt, error) {
	rows, err := s.queries.ListEventPromptsForWorkflow(ctx, workflowID)
	if err != nil {
		return nil, fmt.Errorf("failed to read the prompts of workflow %q: %w", workflowID, err)
	}

	return xslice.Map(rows, func(row *models.ListEventPromptsForWorkflowRow) EventPrompt {
		return EventPrompt{Event: row.Event, Prompt: row.Prompt}
	}), nil
}

func (s *WorkflowStore) eventPromptsForWorkflows(ctx context.Context, ids []string) (map[string][]EventPrompt, error) {
	if len(ids) == 0 {
		return nil, nil
	}

	rows, err := s.queries.ListEventPromptsForWorkflows(ctx, ids)
	if err != nil {
		return nil, fmt.Errorf("failed to read the prompts of %d workflows: %w", len(ids), err)
	}

	prompts := make(map[string][]EventPrompt)
	for _, row := range rows {
		prompts[row.WorkflowID] = append(prompts[row.WorkflowID], EventPrompt{Event: row.Event, Prompt: row.Prompt})
	}

	return prompts, nil
}

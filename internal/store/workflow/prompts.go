package workflow

import (
	"context"
	"fmt"

	"komrad/db/models"
	"komrad/lib/xslice"
)

// Prompt is what a workflow asks of its agents when komrad sends them one notice.
type Prompt struct {
	Notice string
	Prompt string
}

// GetPrompt is the prompt the workflow attaches to a notice, and the empty string when it attaches none.
func (w Workflow) GetPrompt(notice string) string {
	for _, prompt := range w.Prompts {
		if prompt.Notice == notice {
			return prompt.Prompt
		}
	}

	return ""
}

func (s *WorkflowStore) setPrompts(ctx context.Context, workflowID string, prompts []Prompt) error {
	if err := s.queries.DeleteWorkflowPrompts(ctx, workflowID); err != nil {
		return fmt.Errorf("failed to clear prompts for workflow %q: %w", workflowID, err)
	}

	for position, prompt := range prompts {
		if err := s.queries.InsertWorkflowPrompt(ctx, models.InsertWorkflowPromptParams{
			WorkflowID: workflowID,
			Notice:     prompt.Notice,
			Prompt:     prompt.Prompt,
			Position:   int32(position),
		}); err != nil {
			return fmt.Errorf("failed to add the %s prompt to workflow %q: %w", prompt.Notice, workflowID, err)
		}
	}

	return nil
}

func (s *WorkflowStore) promptsFor(ctx context.Context, workflowID string) ([]Prompt, error) {
	rows, err := s.queries.ListPromptsForWorkflow(ctx, workflowID)
	if err != nil {
		return nil, fmt.Errorf("failed to read the prompts of workflow %q: %w", workflowID, err)
	}

	return xslice.Map(rows, func(row *models.ListPromptsForWorkflowRow) Prompt {
		return Prompt{Notice: row.Notice, Prompt: row.Prompt}
	}), nil
}

func (s *WorkflowStore) promptsForWorkflows(ctx context.Context, ids []string) (map[string][]Prompt, error) {
	if len(ids) == 0 {
		return nil, nil
	}

	rows, err := s.queries.ListPromptsForWorkflows(ctx, ids)
	if err != nil {
		return nil, fmt.Errorf("failed to read the prompts of %d workflows: %w", len(ids), err)
	}

	prompts := make(map[string][]Prompt)
	for _, row := range rows {
		prompts[row.WorkflowID] = append(prompts[row.WorkflowID], Prompt{Notice: row.Notice, Prompt: row.Prompt})
	}

	return prompts, nil
}

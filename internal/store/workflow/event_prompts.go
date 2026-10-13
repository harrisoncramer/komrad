package workflow

import (
	"context"
	"fmt"
	"slices"
	"strings"
	"unicode/utf8"

	"komrad/db/models"
	"komrad/internal/eventprompts"
	"komrad/internal/fail"
	"komrad/lib/xslice"
)

var ErrUnknownEventPrompt = fail.InvalidArgument("a prompt must name an event komrad sends")

var ErrDuplicateEventPrompt = fail.InvalidArgument("a workflow holds one prompt per event")

var ErrPromptTooLong = fail.InvalidArgument(fmt.Sprintf("a prompt runs to at most %d characters", eventprompts.MaxPrompt))

// NormalizeEventPrompts trims what a caller sent, drops the entries that neither prompt nor mute, and
// refuses an event komrad does not send or one named twice.
func (c *Config) NormalizeEventPrompts() error {
	kept := make(eventprompts.Prompts, 0, len(c.EventPrompts))
	for _, prompt := range c.EventPrompts {
		prompt.Event = strings.TrimSpace(prompt.Event)
		prompt.Prompt = strings.TrimSpace(prompt.Prompt)
		if prompt.Prompt == "" && !prompt.Muted {
			continue
		}
		if !eventprompts.IsKnown(prompt.Event) {
			return fmt.Errorf("%w: %q is not one of %s", ErrUnknownEventPrompt, prompt.Event, strings.Join(eventprompts.Names(), ", "))
		}
		if slices.ContainsFunc(kept, func(k eventprompts.Prompt) bool { return k.Event == prompt.Event }) {
			return fmt.Errorf("%w: %s", ErrDuplicateEventPrompt, prompt.Event)
		}
		if utf8.RuneCountInString(prompt.Prompt) > eventprompts.MaxPrompt {
			return fmt.Errorf("%w: %s runs to %d characters", ErrPromptTooLong, prompt.Event, utf8.RuneCountInString(prompt.Prompt))
		}

		kept = append(kept, prompt)
	}
	c.EventPrompts = kept

	return nil
}

func (s *WorkflowStore) setEventPrompts(ctx context.Context, workflowID string, prompts eventprompts.Prompts) error {
	if err := s.queries.DeleteWorkflowEventPrompts(ctx, workflowID); err != nil {
		return fmt.Errorf("failed to clear prompts for workflow %q: %w", workflowID, err)
	}

	for position, prompt := range prompts {
		if err := s.queries.InsertWorkflowPrompt(ctx, models.InsertWorkflowPromptParams{
			WorkflowID: workflowID,
			Event:      prompt.Event,
			Prompt:     prompt.Prompt,
			Position:   int32(position),
			Muted:      prompt.Muted,
		}); err != nil {
			return fmt.Errorf("failed to add the %s prompt to workflow %q: %w", prompt.Event, workflowID, err)
		}
	}

	return nil
}

func (s *WorkflowStore) eventPromptsFor(ctx context.Context, workflowID string) (eventprompts.Prompts, error) {
	rows, err := s.queries.ListEventPromptsForWorkflow(ctx, workflowID)
	if err != nil {
		return nil, fmt.Errorf("failed to read the prompts of workflow %q: %w", workflowID, err)
	}

	return xslice.Map(rows, func(row *models.ListEventPromptsForWorkflowRow) eventprompts.Prompt {
		return eventprompts.Prompt{
			Event:  row.Event,
			Prompt: row.Prompt,
			Muted:  row.Muted,
		}
	}), nil
}

func (s *WorkflowStore) eventPromptsForWorkflows(ctx context.Context, ids []string) (map[string]eventprompts.Prompts, error) {
	if len(ids) == 0 {
		return map[string]eventprompts.Prompts{}, nil
	}

	rows, err := s.queries.ListEventPromptsForWorkflows(ctx, ids)
	if err != nil {
		return nil, fmt.Errorf("failed to read the prompts of %d workflows: %w", len(ids), err)
	}

	prompts := make(map[string]eventprompts.Prompts)
	for _, row := range rows {
		prompts[row.WorkflowID] = append(prompts[row.WorkflowID], eventprompts.Prompt{
			Event:  row.Event,
			Prompt: row.Prompt,
			Muted:  row.Muted,
		})
	}

	return prompts, nil
}

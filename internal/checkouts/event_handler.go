package checkouts

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"

	"komrad/internal/eventprompts"
	"komrad/internal/events"
	"komrad/internal/mail"
	"komrad/internal/store"
	agentstore "komrad/internal/store/agent"
	mailstore "komrad/internal/store/mail"
	taskstore "komrad/internal/store/task"

	"github.com/riverqueue/river"
)

const rebasedTopic = "worktree_rebased"

// rebased is a task's worktree having been rebased, or a rebase of it having failed.
type rebased struct {
	TaskID string `json:"task_id"`
}

// Topic names the handler that tells the task's writer.
func (rebased) Topic() string {
	return rebasedTopic
}

// rebaseOutcome is what the latest rebase of a task's worktree came to.
type rebaseOutcome struct {
	Result  *RebaseResult `json:"result,omitempty"`
	Failure string        `json:"failure,omitempty"`
}

// NewEventHandler builds the handler the router is given for rebases.
func NewEventHandler() events.HandlerFor[rebased] {
	return handleRebased
}

// handleRebased tells the task's writer what the latest rebase of its worktree came to.
func handleRebased(ctx context.Context, tx store.DBTX, mailer *mail.MailService, rebased rebased) ([]river.JobArgs, error) {
	raw, err := taskstore.NewTaskStore(tx).GetLastRebase(ctx, rebased.TaskID)
	if errors.Is(err, store.ErrNotFound) || (err == nil && raw == nil) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}

	var outcome rebaseOutcome
	if err := json.Unmarshal(raw, &outcome); err != nil {
		return nil, fmt.Errorf("failed to read the last rebase of task %q: %w", rebased.TaskID, err)
	}

	body := cutMail(outcome.Failure)
	if outcome.Result != nil {
		body = rebaseMail(*outcome.Result)
	}

	return nil, mailWriter(ctx, tx, mailer, mailWriterParams{
		TaskID: rebased.TaskID,
		Kind:   mailstore.KindWorktreeRebased,
		Body:   body,
		Events: []string{outcome.event()},
	})
}

const pushedTopic = "worktree_pushed"

// pushed is a task's branch having been force-pushed to origin from the panel.
type pushed struct {
	TaskID string `json:"task_id"`
	Branch string `json:"branch"`
	SHA    string `json:"sha"`
}

// Topic names the handler that tells the task's writer.
func (pushed) Topic() string {
	return pushedTopic
}

// NewPushedHandler builds the handler the router is given for force-pushes.
func NewPushedHandler() events.HandlerFor[pushed] {
	return handlePushed
}

// handlePushed tells the task's writer its branch on origin was overwritten.
func handlePushed(ctx context.Context, tx store.DBTX, mailer *mail.MailService, pushed pushed) ([]river.JobArgs, error) {
	return nil, mailWriter(ctx, tx, mailer, mailWriterParams{
		TaskID: pushed.TaskID,
		Kind:   mailstore.KindWorktreePushed,
		Body:   pushedMail(pushed),
	})
}

const abortedTopic = "worktree_aborted"

// aborted is what a task's worktree had part-way through having been aborted from the panel,
// or an abort that cut its writer's turn having failed.
type aborted struct {
	TaskID  string       `json:"task_id"`
	Result  *AbortResult `json:"result,omitempty"`
	Failure string       `json:"failure,omitempty"`
}

// Topic names the handler that tells the task's writer.
func (aborted) Topic() string {
	return abortedTopic
}

// NewAbortedHandler builds the handler the router is given for aborts.
func NewAbortedHandler() events.HandlerFor[aborted] {
	return handleAborted
}

// handleAborted tells the task's writer what became of the sequence it had part-way through.
func handleAborted(ctx context.Context, tx store.DBTX, mailer *mail.MailService, aborted aborted) ([]river.JobArgs, error) {
	return nil, mailWriter(ctx, tx, mailer, mailWriterParams{
		TaskID: aborted.TaskID,
		Kind:   mailstore.KindWorktreeAborted,
		Body:   abortedMail(aborted),
	})
}

// mailWriterParams is a piece of mail for whoever writes a task.
type mailWriterParams struct {
	TaskID string
	Kind   mailstore.Kind
	Body   string
	Events []string
}

// mailWriter sends the mail to whoever writes the task as the event is handled, and to nobody
// when the task has no writer.
func mailWriter(ctx context.Context, tx store.DBTX, mailer *mail.MailService, params mailWriterParams) error {
	writer, err := agentstore.NewAgentStore(tx).GetTaskWriter(ctx, params.TaskID)
	if errors.Is(err, store.ErrNotFound) {
		return nil
	}
	if err != nil {
		return fmt.Errorf("failed to read the writer of task %q: %w", params.TaskID, err)
	}

	_, err = events.Send(ctx, mailer, mail.SendFromKomradParams{
		ToAgentID: writer.ID,
		Kind:      params.Kind,
		SubjectID: writer.ID,
		Body:      params.Body,
		Events:    params.Events,
	})

	return err
}

// event is the event the outcome starts.
func (o rebaseOutcome) event() string {
	switch {
	case o.Result == nil:
		return eventprompts.WorktreeRebaseFailed
	case o.Result.Conflicted:
		return eventprompts.WorktreeRebaseConflicted
	case o.Result.Moved:
		return eventprompts.WorktreeLanded
	default:
		return eventprompts.WorktreeRebased
	}
}

package agents

import (
	"context"
	"errors"
	"fmt"
	"strings"

	"claude-agents/internal/harness"
	agentstore "claude-agents/internal/store/agent"
	"claude-agents/internal/store/transcript"
)

type transcriptStore interface {
	Get(ctx context.Context, agentID string) (transcript.Transcript, error)
	Upsert(ctx context.Context, params transcript.UpsertParams) error
	AppendTurns(ctx context.Context, agentID string, turns []transcript.Turn) error
	DeleteTurns(ctx context.Context, agentID string) error
}

// agentLister is all the syncer needs from the agent domain, which agents exist
// to sync. Archived ones are included, since their transcripts stay searchable.
type agentLister interface {
	ListAll(ctx context.Context) ([]agentstore.Agent, error)
}

// logHarness is all the syncer needs from a harness, where a conversation's log is
// and what has been written to it since the last pass. The format that log is in
// never reaches here.
type logHarness interface {
	LogPath(sessionID string) string
	ReadLog(path string, offset int64) (harness.LogRead, error)
}

// TranscriptSyncer keeps each agent's searchable transcript in step with the log its
// CLI writes to disk. Rather than hook into that CLI's lifecycle, which fires
// unreliably, it reads the on-disk log directly on a timer, so missing a pass loses
// nothing.
type TranscriptSyncer struct {
	agents  agentLister
	store   transcriptStore
	harness logHarness
}

// NewTranscriptSyncer builds a syncer over the store, reading logs through the
// harness that wrote them.
func NewTranscriptSyncer(agents agentLister, transcripts transcriptStore, h logHarness) *TranscriptSyncer {
	return &TranscriptSyncer{agents: agents, store: transcripts, harness: h}
}

// Sync folds any new transcript text for every tracked agent into the store. Each agent
// reads only the bytes appended since its last sync, so a pass is cheap even over a large
// corpus.
//
// An agent that cannot be folded in does not stop the pass. The failures that reach here
// are mostly properties of one agent's log rather than of the machine, so they recur
// every pass, and the walk is ordered newest first: giving up at the first one would
// leave every older agent behind it never indexed again. They are still returned
// together, so a log that never syncs shows up somewhere other than a search that quietly
// finds nothing.
func (ts *TranscriptSyncer) Sync(ctx context.Context) error {
	agents, err := ts.agents.ListAll(ctx)
	if err != nil {
		return fmt.Errorf("failed to list agents for transcript sync: %w", err)
	}

	var failures []error
	for _, agent := range agents {
		if agent.SessionID == "" {
			continue
		}
		if err := ts.syncOne(ctx, agent); err != nil {
			failures = append(failures, fmt.Errorf("failed to sync transcript for %q: %w", agent.Name, err))
		}
	}

	return errors.Join(failures...)
}

// syncOne reads the tail of one agent's log, appends its flattened text to the
// stored transcript, and indexes the turns it found. A missing log is a no-op, as
// is a log with nothing new complete enough to read.
func (ts *TranscriptSyncer) syncOne(ctx context.Context, agent agentstore.Agent) error {
	path := ts.harness.LogPath(agent.SessionID)
	if path == "" {
		return nil
	}

	tr, err := ts.store.Get(ctx, agent.ID)
	if err != nil {
		return err
	}

	read, err := ts.harness.ReadLog(path, tr.SyncedOffset)
	if err != nil {
		return err
	}

	// The log no longer continues from where the last pass left off, so the stored
	// offset points into content that is gone and reading starts over.
	offset, content := tr.SyncedOffset, tr.Content
	if read.Restart {
		offset, content = 0, ""
	}
	if read.Consumed == 0 {
		return nil
	}

	// The indexed turns describe text that is about to be replaced, so they go
	// before this pass numbers the turns it read.
	if read.Restart {
		if err := ts.store.DeleteTurns(ctx, agent.ID); err != nil {
			return err
		}
	}

	content, turns := appendTurns(content, read.Turns)

	if err := ts.store.Upsert(ctx, transcript.UpsertParams{
		AgentID: agent.ID,
		Content: content,
		Offset:  offset + read.Consumed,
	}); err != nil {
		return err
	}

	return ts.store.AppendTurns(ctx, agent.ID, turns)
}

// appendTurns joins newly-synced turns onto the transcript so far, blank-line separated,
// and returns the grown text alongside the turns ready to index. Each turn carries where
// it landed in that text, which is what the panel reads back to scroll a hit into view
// rather than hunt for the words again.
func appendTurns(existing string, turns []harness.Turn) (string, []transcript.Turn) {
	var content strings.Builder
	content.WriteString(existing)

	indexed := make([]transcript.Turn, 0, len(turns))
	for _, t := range turns {
		if content.Len() > 0 {
			content.WriteString("\n\n")
		}

		indexed = append(indexed, transcript.Turn{
			Role:    t.Role,
			Content: t.Content,
			Offset:  int64(content.Len()),
		})
		content.WriteString(t.Content)
	}

	return content.String(), indexed
}

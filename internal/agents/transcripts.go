package agents

import (
	"context"
	"errors"
	"fmt"
	"strings"

	"weave/internal/harness"
	agentstore "weave/internal/store/agent"
	"weave/internal/store/transcript"
)

type transcriptReadWriter interface {
	Get(ctx context.Context, agentID string) (transcript.Transcript, error)
	Upsert(ctx context.Context, params transcript.UpsertParams) error
	AppendTurns(ctx context.Context, agentID string, turns []transcript.Turn) error
	DeleteTurns(ctx context.Context, agentID string) error
}

// agentSessionLister is the agent store as TranscriptSyncer needs it: every agent to
// walk, and the session id to stamp once a harness names one.
type agentSessionLister interface {
	ListAll(ctx context.Context) ([]agentstore.Agent, error)
	SetSessionID(ctx context.Context, id, sessionID string) error
}

// TranscriptSyncer keeps each agent's searchable transcript in step with the log its CLI
// writes to disk. Rather than hook into that CLI's lifecycle, which fires unreliably, it
// reads the log on a timer, so missing a pass loses nothing.
type TranscriptSyncer struct {
	agents    agentSessionLister
	store     transcriptReadWriter
	harnesses harnessGetter
}

// TranscriptSyncerParams carries the syncer's dependencies.
type TranscriptSyncerParams struct {
	Agents      agentSessionLister
	Transcripts transcriptReadWriter
	Harnesses   harnessGetter
}

// NewTranscriptSyncer builds a syncer over the store, reading each agent's log
// through the harness that wrote it.
func NewTranscriptSyncer(p TranscriptSyncerParams) *TranscriptSyncer {
	return &TranscriptSyncer{
		agents:    p.Agents,
		store:     p.Transcripts,
		harnesses: p.Harnesses,
	}
}

// Sync folds any new transcript text for every tracked agent into the store, reading only
// the bytes appended since that agent's last sync.
//
// An agent that cannot be folded in does not stop the pass. These failures are properties
// of one agent's log rather than of the machine, so they recur every pass, and the walk is
// newest first: giving up at the first would leave every older agent behind it unindexed.
// They are still returned together, so a log that never syncs shows up somewhere other
// than a search that quietly finds nothing.
func (ts *TranscriptSyncer) Sync(ctx context.Context) error {
	agents, err := ts.agents.ListAll(ctx)
	if err != nil {
		return fmt.Errorf("failed to list agents for transcript sync: %w", err)
	}

	// An agent with no conversation yet cannot be synced, so the id it will hold is
	// adopted first. Both walks see the same listing, so adoption knows which
	// conversations the other agents already hold.
	var failures []error
	for _, agent := range ts.adoptSessions(ctx, agents) {
		if agent.SessionID == "" {
			continue
		}
		if err := ts.syncOne(ctx, agent); err != nil {
			failures = append(failures, fmt.Errorf("failed to sync transcript for %q: %w", agent.Name, err))
		}
	}

	return errors.Join(failures...)
}

// adoptSessions gives every agent still missing a conversation the one its harness started
// for it, and returns the agents with the ids it settled on. A harness that names its own
// conversations cannot be told which id to use, so it is asked afterwards, and it knows
// which conversation is this agent's because the launch told the CLI which agent it was.
//
// A failure to adopt is not reported: the conversation appears a second or two after the
// launch, so the passes before it are ordinary and the next pass tries again.
func (ts *TranscriptSyncer) adoptSessions(ctx context.Context, agents []agentstore.Agent) []agentstore.Agent {
	for i, agent := range agents {
		if agent.SessionID != "" {
			continue
		}

		harness, err := ts.harnesses.GetByID(agent.Harness)
		if err != nil {
			continue
		}

		found, err := harness.FindSession(agent.ID)
		if err != nil || found == "" {
			continue
		}
		if err := ts.agents.SetSessionID(ctx, agent.ID, found); err != nil {
			continue
		}

		agents[i].SessionID = found
	}

	return agents
}

// syncOne reads the tail of one agent's log, appends its flattened text to the
// stored transcript, and indexes the turns it found. A missing log is a no-op, as
// is a log with nothing new complete enough to read.
func (ts *TranscriptSyncer) syncOne(ctx context.Context, agent agentstore.Agent) error {
	// An agent recorded against a harness this binary does not implement is reported
	// rather than skipped, since its transcript will never advance again.
	hn, err := ts.harnesses.GetByID(agent.Harness)
	if err != nil {
		return err
	}

	path := hn.GetLogPath(agent.SessionID)
	if path == "" {
		return nil
	}

	tr, err := ts.store.Get(ctx, agent.ID)
	if err != nil {
		return err
	}

	read, err := hn.ReadLog(path, tr.SyncedOffset)
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
// and returns the grown text alongside the turns ready to index. Each turn carries where it
// landed, which is what the panel reads back to scroll a hit into view.
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

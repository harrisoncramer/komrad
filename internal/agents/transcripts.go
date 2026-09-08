package agents

import (
	"context"
	"errors"
	"fmt"
	"strings"

	"weave/internal/harness"
	agentstore "weave/internal/store/agent"
	transcriptstore "weave/internal/store/transcript"
)

type transcriptReadWriter interface {
	Get(ctx context.Context, agentID string) (transcriptstore.Transcript, error)
	Upsert(ctx context.Context, params transcriptstore.UpsertParams) error
	AppendTurns(ctx context.Context, agentID string, turns []transcriptstore.Turn) error
	DeleteTurns(ctx context.Context, agentID string) error
}

type agentSessionReadWriter interface {
	ListAll(ctx context.Context) ([]agentstore.Agent, error)
	SetSessionID(ctx context.Context, id, sessionID string) error
}

type TranscriptSyncer struct {
	agents    agentSessionReadWriter
	store     transcriptReadWriter
	harnesses harnessGetter
}

type TranscriptSyncerParams struct {
	Agents      agentSessionReadWriter
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

// Sync folds any new transcript text for every tracked agent into the store, reading only the
// bytes appended since that agent's last sync.
func (ts *TranscriptSyncer) Sync(ctx context.Context) error {
	agents, err := ts.agents.ListAll(ctx)
	if err != nil {
		return fmt.Errorf("failed to list agents for transcript sync: %w", err)
	}

	var failures []error
	for _, agent := range ts.adoptSessions(ctx, agents) {
		if agent.SessionID == "" {
			continue
		}
		if err := ts.syncOne(ctx, agent); err != nil {
			// A bad log fails every pass, so stopping at the first would leave every older
			// agent behind it unindexed.
			failures = append(failures, fmt.Errorf("failed to sync transcript for %q: %w", agent.Name, err))
		}
	}

	return errors.Join(failures...)
}

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

func (ts *TranscriptSyncer) syncOne(ctx context.Context, agent agentstore.Agent) error {
	hn, err := ts.harnesses.GetByID(agent.Harness)
	if err != nil {
		return err
	}

	path := hn.LogPath(agent.SessionID)
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

	if read.Consumed == 0 && !read.Restart {
		return nil
	}

	offset, content := tr.SyncedOffset, tr.Content
	if read.Restart {
		offset, content = 0, ""

		if err := ts.store.DeleteTurns(ctx, agent.ID); err != nil {
			return err
		}
	}

	content, turns := appendTurns(content, read.Turns)

	if err := ts.store.Upsert(ctx, transcriptstore.UpsertParams{
		AgentID: agent.ID,
		Content: content,
		Offset:  offset + read.Consumed,
	}); err != nil {
		return err
	}

	return ts.store.AppendTurns(ctx, agent.ID, turns)
}

func appendTurns(existing string, turns []harness.Turn) (string, []transcriptstore.Turn) {
	var content strings.Builder
	content.WriteString(existing)

	indexed := make([]transcriptstore.Turn, 0, len(turns))
	for _, t := range turns {
		if content.Len() > 0 {
			content.WriteString("\n\n")
		}

		indexed = append(indexed, transcriptstore.Turn{
			Role:    t.Role,
			Content: t.Content,
			Offset:  int64(content.Len()),
		})
		content.WriteString(t.Content)
	}

	return content.String(), indexed
}

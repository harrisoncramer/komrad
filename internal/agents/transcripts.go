package agents

import (
	"context"
	"errors"
	"fmt"
	"strings"

	"weave/internal/harness"
	agentstore "weave/internal/store/agent"
	transcriptstore "weave/internal/store/transcript"
	usagestore "weave/internal/store/usage"
	"weave/lib/xslice"
)

type transcriptReadWriter interface {
	Get(ctx context.Context, agentID string) (transcriptstore.Transcript, error)
	Upsert(ctx context.Context, params transcriptstore.UpsertParams) error
	AppendTurns(ctx context.Context, agentID string, turns []transcriptstore.Turn) error
	DeleteTurns(ctx context.Context, agentID string) error
}

type usageWriter interface {
	Append(ctx context.Context, agentID string, reported []usagestore.Reported) error
	Prune(ctx context.Context) error
}

type agentSessionLister interface {
	ListAll(ctx context.Context) ([]agentstore.Agent, error)
	SetSessionID(ctx context.Context, id, sessionID string) error
}

type TranscriptSyncer struct {
	agents    agentSessionLister
	store     transcriptReadWriter
	usage     usageWriter
	harnesses harnessGetter
}

type TranscriptSyncerParams struct {
	Agents      agentSessionLister
	Transcripts transcriptReadWriter
	Usage       usageWriter
	Harnesses   harnessGetter
}

// NewTranscriptSyncer builds a syncer over the store, reading each agent's log
// through the harness that wrote it.
func NewTranscriptSyncer(p TranscriptSyncerParams) *TranscriptSyncer {
	return &TranscriptSyncer{
		agents:    p.Agents,
		store:     p.Transcripts,
		usage:     p.Usage,
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

	if err := ts.usage.Prune(ctx); err != nil {
		failures = append(failures, err)
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

	offset, content := tr.SyncedOffset, tr.Content
	if read.Restart {
		offset, content = 0, ""
	}
	if read.Consumed == 0 {
		return nil
	}

	if read.Restart {
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

	if err := ts.store.AppendTurns(ctx, agent.ID, turns); err != nil {
		return err
	}

	// A re-read after a restart replays the same exchanges under the same keys, so the usage
	// rows are left to settle on their own rather than dropped alongside the turns.
	return ts.usage.Append(ctx, agent.ID, reportedUsage(read.Usage))
}

func reportedUsage(reported []harness.Usage) []usagestore.Reported {
	return xslice.Map(reported, func(u harness.Usage) usagestore.Reported {
		return usagestore.Reported{
			Key:        u.Key,
			At:         u.At,
			Input:      u.Input,
			CacheRead:  u.CacheRead,
			CacheWrite: u.CacheWrite,
			Output:     u.Output,
		}
	})
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

package transcripts

import (
	"context"
	"errors"
	"fmt"

	"weave/internal/harness"
	agentstore "weave/internal/store/agent"
	transcriptstore "weave/internal/store/transcript"
	"weave/lib/xslice"
)

type transcriptReadWriter interface {
	OpenSession(ctx context.Context, params transcriptstore.OpenSessionParams) (transcriptstore.Session, error)
	Advance(ctx context.Context, sessionID string, offset int64) error
	AppendTurns(ctx context.Context, params transcriptstore.AppendTurnsParams) error
	DeleteSessionTurns(ctx context.Context, sessionID string) error
}

type agentSessionReadWriter interface {
	ListForTranscriptSync(ctx context.Context) ([]agentstore.Agent, error)
	SetSessionID(ctx context.Context, id, sessionID string) error
}

type harnessGetter interface {
	GetByID(id string) (harness.Harness, error)
}

type TranscriptSyncer struct {
	agents    agentSessionReadWriter
	store     transcriptReadWriter
	harnesses harnessGetter
}

type NewTranscriptSyncerParams struct {
	Agents      agentSessionReadWriter
	Transcripts transcriptReadWriter
	Harnesses   harnessGetter
}

// NewTranscriptSyncer builds a syncer over the store, reading each agent's log through the
// harness that wrote it.
func NewTranscriptSyncer(params NewTranscriptSyncerParams) *TranscriptSyncer {
	return &TranscriptSyncer{
		agents:    params.Agents,
		store:     params.Transcripts,
		harnesses: params.Harnesses,
	}
}

// Sync folds any new transcript text for every agent whose log can still hold unread turns into
// the store, reading only the bytes appended since that agent's last sync.
func (ts *TranscriptSyncer) Sync(ctx context.Context) error {
	agents, err := ts.agents.ListForTranscriptSync(ctx)
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

	session, err := ts.store.OpenSession(ctx, transcriptstore.OpenSessionParams{
		AgentID:   agent.ID,
		SessionID: agent.SessionID,
	})
	if err != nil {
		return err
	}

	read, err := readLog(hn, agent.SessionID, session.SyncedOffset)
	if err != nil {
		return err
	}

	if read.Consumed == 0 && !read.Restart {
		// A stamp on a log that cannot grow is what takes an archived agent out of the next listing.
		if agent.ArchivedAt != nil {
			return ts.store.Advance(ctx, session.ID, session.SyncedOffset)
		}

		return nil
	}

	offset := session.SyncedOffset
	if read.Restart {
		offset = 0

		if err := ts.store.DeleteSessionTurns(ctx, session.ID); err != nil {
			return err
		}
	}

	if err := ts.store.AppendTurns(ctx, transcriptstore.AppendTurnsParams{
		AgentID:   agent.ID,
		SessionID: session.ID,
		Turns:     turns(read.Turns),
	}); err != nil {
		return err
	}

	return ts.store.Advance(ctx, session.ID, offset+read.Consumed)
}

// readLog reads whatever a session's log holds past offset, an unwritten log reading as empty.
func readLog(hn harness.Harness, sessionID string, offset int64) (harness.LogRead, error) {
	path := hn.LogPath(sessionID)
	if path == "" {
		return harness.LogRead{}, nil
	}

	return hn.ReadLog(path, offset)
}

func turns(read []harness.Turn) []transcriptstore.Turn {
	return xslice.Map(read, func(turn harness.Turn) transcriptstore.Turn {
		return transcriptstore.Turn{Role: turn.Role, Content: turn.Content}
	})
}

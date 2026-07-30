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

// sessionStore is how the syncer records the conversation an agent turned out to be
// holding. It writes only that, since nothing else about the agent is its to change.
type sessionStore interface {
	SetSessionID(ctx context.Context, id, sessionID string) error
}

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

// logRegistry resolves the harness that wrote an agent's log, the only thing that
// knows where that log is and how to read it. The format never reaches here.
type logRegistry interface {
	Resolve(id string) (harness.Harness, error)
}

// TranscriptSyncer keeps each agent's searchable transcript in step with the log its
// CLI writes to disk. Rather than hook into that CLI's lifecycle, which fires
// unreliably, it reads the on-disk log directly on a timer, so missing a pass loses
// nothing.
type TranscriptSyncer struct {
	agents    agentLister
	sessions  sessionStore
	store     transcriptStore
	harnesses logRegistry
}

// TranscriptSyncerParams carries the syncer's dependencies.
type TranscriptSyncerParams struct {
	Agents      agentLister
	Sessions    sessionStore
	Transcripts transcriptStore
	Harnesses   logRegistry
}

// NewTranscriptSyncer builds a syncer over the store, reading each agent's log
// through the harness that wrote it.
func NewTranscriptSyncer(p TranscriptSyncerParams) *TranscriptSyncer {
	return &TranscriptSyncer{
		agents:    p.Agents,
		sessions:  p.Sessions,
		store:     p.Transcripts,
		harnesses: p.Harnesses,
	}
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

	// An agent with no conversation yet cannot be synced, and the id it will hold has
	// to be adopted before the first pass that can read anything. Both walks see the
	// same listing, so adoption knows which conversations the other agents already
	// hold.
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

// adoptSessions gives every agent still missing a conversation the one its harness
// started for it, and returns the agents with the ids it settled on.
//
// A harness that names its own conversations is the reason this exists. The panel
// cannot tell such an agent which id to use, so it asks afterwards which
// conversations appeared in that agent's directory since it was created, and takes
// the earliest one no other agent already holds. Two agents launched into the same
// directory within a pass are told apart that way rather than by guessing.
//
// A failure to adopt is not reported. The conversation appears a second or two after
// the launch, so the passes before it are the ordinary case rather than a fault, and
// the next pass tries again.
func (ts *TranscriptSyncer) adoptSessions(ctx context.Context, agents []agentstore.Agent) []agentstore.Agent {
	held := make(map[string]struct{}, len(agents))
	var pending []int
	for i, agent := range agents {
		if agent.SessionID != "" {
			held[agent.SessionID] = struct{}{}

			continue
		}

		pending = append(pending, i)
	}

	for _, i := range pending {
		agent := agents[i]

		harness, err := ts.harnesses.Resolve(agent.Harness)
		if err != nil {
			continue
		}

		candidates, err := harness.FindSessions(agent.Cwd, agent.CreatedAt)
		if err != nil {
			continue
		}

		for _, candidate := range candidates {
			if _, taken := held[candidate]; taken {
				continue
			}
			if err := ts.sessions.SetSessionID(ctx, agent.ID, candidate); err != nil {
				break
			}

			held[candidate] = struct{}{}
			agents[i].SessionID = candidate

			break
		}
	}

	return agents
}

// syncOne reads the tail of one agent's log, appends its flattened text to the
// stored transcript, and indexes the turns it found. A missing log is a no-op, as
// is a log with nothing new complete enough to read.
func (ts *TranscriptSyncer) syncOne(ctx context.Context, agent agentstore.Agent) error {
	// An agent recorded against a harness this binary does not implement is reported
	// rather than skipped. Its transcript will never advance again, and a search that
	// quietly finds nothing is how that goes unnoticed.
	resolved, err := ts.harnesses.Resolve(agent.Harness)
	if err != nil {
		return err
	}

	path := resolved.GetLogPath(agent.SessionID)
	if path == "" {
		return nil
	}

	tr, err := ts.store.Get(ctx, agent.ID)
	if err != nil {
		return err
	}

	read, err := resolved.ReadLog(path, tr.SyncedOffset)
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

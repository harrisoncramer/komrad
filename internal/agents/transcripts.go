package agents

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"

	agentstore "claude-agents/internal/store/agent"
	"claude-agents/internal/store/transcript"
)

// transcriptStore is the part of the store the transcript syncer reads and
// writes.
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

// TranscriptSyncer keeps each agent's searchable transcript in step with the
// JSONL Claude Code writes to disk. Rather than hook into Claude's lifecycle,
// which fires unreliably, it reads the on-disk log directly on a timer. The file
// is named for the agent's claude session id, so the path is known without any
// cooperation from Claude. Missing a pass loses nothing, since the next pass
// reads whatever the file now holds.
type TranscriptSyncer struct {
	agents      agentLister
	store       transcriptStore
	projectsDir string
}

// NewTranscriptSyncer builds a syncer over the store, reading logs from
// projectsDir, which is Claude's ~/.claude/projects.
func NewTranscriptSyncer(agents agentLister, transcripts transcriptStore, projectsDir string) *TranscriptSyncer {
	return &TranscriptSyncer{agents: agents, store: transcripts, projectsDir: projectsDir}
}

// Sync folds any new transcript text for every tracked agent into the store. It
// is idempotent and self-limiting. Each agent reads only the bytes appended since
// its last sync, so a pass is cheap even over a large corpus, and an agent whose
// log is unchanged is skipped entirely.
//
// An agent that cannot be folded in does not stop the pass. The failures that
// reach here are mostly properties of one agent's log rather than of the machine,
// so they recur on every pass, and the walk is ordered newest first: giving up at
// the first one would leave every older agent behind it never indexed again. The
// failures are still returned together, so a log that never syncs is visible in
// the log rather than only in a search that quietly finds nothing.
func (ts *TranscriptSyncer) Sync(ctx context.Context) error {
	agents, err := ts.agents.ListAll(ctx)
	if err != nil {
		return fmt.Errorf("failed to list agents for transcript sync: %w", err)
	}

	var failures []error
	for _, agent := range agents {
		if agent.ClaudeSessionID == "" {
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
// is a log with no new complete lines.
func (ts *TranscriptSyncer) syncOne(ctx context.Context, agent agentstore.Agent) error {
	path := sessionLogPath(ts.projectsDir, agent.ClaudeSessionID)
	if path == "" {
		return nil
	}

	info, err := os.Stat(path)
	if err != nil {
		return nil //nolint:nilerr // a log that is not on disk yet is not an error
	}

	tr, err := ts.store.Get(ctx, agent.ID)
	if err != nil {
		return err
	}

	// The log shrank, so Claude rewrote it, which a --resume can do. The stored
	// offset no longer points into the same content, so read from the start to match
	// the file again.
	offset, content := tr.SyncedOffset, tr.Content
	reindex := info.Size() < offset
	if reindex {
		offset, content = 0, ""
	}
	if info.Size() == offset {
		return nil
	}

	appended, consumed, err := readNewLines(path, offset)
	if err != nil {
		return err
	}
	if consumed == 0 {
		return nil
	}

	// The indexed turns describe text that is about to be replaced, so they go
	// before this pass numbers the turns it read.
	if reindex {
		if err := ts.store.DeleteTurns(ctx, agent.ID); err != nil {
			return err
		}
	}

	content, turns := appendTurns(content, flattenTranscript(appended))

	if err := ts.store.Upsert(ctx, transcript.UpsertParams{
		AgentID: agent.ID,
		Content: content,
		Offset:  offset + consumed,
	}); err != nil {
		return err
	}

	return ts.store.AppendTurns(ctx, agent.ID, turns)
}

// sessionLogPath finds the JSONL log for a claude session id under projectsDir,
// or "" when no log exists. The file is named for the session id inside a
// per-directory folder, so a glob on the id finds it without reproducing Claude's
// version-specific directory-name encoding.
func sessionLogPath(projectsDir, claudeSessionID string) string {
	matches, err := filepath.Glob(filepath.Join(projectsDir, "*", claudeSessionID+".jsonl"))
	if err != nil || len(matches) == 0 {
		return ""
	}

	return matches[0]
}

// readNewLines reads from offset to the end of the file and returns the complete
// lines found there together with how many bytes they span. A trailing partial
// line, written while Claude is mid-turn, is left unconsumed so the next pass
// reads it once whole. The offset advances by the bytes of complete lines only,
// never the raw read length.
func readNewLines(path string, offset int64) ([]byte, int64, error) {
	file, err := os.Open(path)
	if err != nil {
		return nil, 0, fmt.Errorf("failed to open transcript %q: %w", path, err)
	}
	defer func() { _ = file.Close() }()

	if _, err := file.Seek(offset, io.SeekStart); err != nil {
		return nil, 0, fmt.Errorf("failed to seek transcript %q: %w", path, err)
	}

	tail, err := io.ReadAll(file)
	if err != nil {
		return nil, 0, fmt.Errorf("failed to read transcript %q: %w", path, err)
	}

	lastNL := strings.LastIndexByte(string(tail), '\n')
	if lastNL < 0 {
		return nil, 0, nil
	}

	complete := tail[:lastNL+1]
	return complete, int64(len(complete)), nil
}

// transcriptLine is the part of a JSONL entry the flattener reads. Only user and
// assistant turns carry conversation. Everything else is skipped by type.
type transcriptLine struct {
	Type    string `json:"type"`
	IsMeta  bool   `json:"isMeta"`
	Message *struct {
		Content json.RawMessage `json:"content"`
	} `json:"message"`
}

// contentBlock is one block of a structured message. Only text blocks hold prose,
// so tool_use, tool_result, thinking, and images are dropped.
type contentBlock struct {
	Type string `json:"type"`
	Text string `json:"text"`
}

// turn is one turn of conversation pulled out of a log, who spoke and what they
// said.
type turn struct {
	role string
	text string
}

// flattenTranscript turns raw JSONL bytes into the turns worth searching, the
// user's prompts and the assistant's prose in order. Tool calls, tool results,
// thinking, and non-conversation entries are left out so the index stays lean and a
// match reads as something actually said.
func flattenTranscript(data []byte) []turn {
	var turns []turn
	for _, raw := range strings.Split(string(data), "\n") {
		raw = strings.TrimSpace(raw)
		if raw == "" {
			continue
		}

		var line transcriptLine
		if err := json.Unmarshal([]byte(raw), &line); err != nil {
			continue
		}
		if line.IsMeta || line.Message == nil {
			continue
		}
		if line.Type != "user" && line.Type != "assistant" {
			continue
		}

		if text := blockText(line.Message.Content); text != "" {
			turns = append(turns, turn{role: line.Type, text: text})
		}
	}

	return turns
}

// blockText extracts the prose from a message's content, which is either a plain
// string or an array of typed blocks. Only text blocks survive.
func blockText(content json.RawMessage) string {
	var plain string
	if err := json.Unmarshal(content, &plain); err == nil {
		return cleanText(plain)
	}

	var blocks []contentBlock
	if err := json.Unmarshal(content, &blocks); err != nil {
		return ""
	}

	var parts []string
	for _, block := range blocks {
		if block.Type != "text" {
			continue
		}
		if text := cleanText(block.Text); text != "" {
			parts = append(parts, text)
		}
	}

	return strings.Join(parts, "\n\n")
}

// cleanText trims a turn's prose and drops the one byte the transcript columns
// cannot hold. Postgres refuses a NUL in a text value outright, and the log this
// reads is written by another process entirely, so a single turn carrying one
// would refuse every write for that agent from then on.
//
// It is dropped here, as the prose is read, rather than at the write. The turns a
// pass produces carry the offset they sit at in the transcript text, and that text
// is built from these strings, so anything removed later would leave every offset
// after it pointing a byte too far.
func cleanText(text string) string {
	return strings.TrimSpace(strings.ReplaceAll(text, "\x00", ""))
}

// appendTurns joins newly-synced turns onto the transcript so far, blank-line
// separated so they stay readable, and returns the grown text alongside the turns
// ready to index. Each turn carries where it landed in that text. This is the only
// place that knows, and the panel reads it back to scroll a hit into view rather
// than hunt for the words again.
func appendTurns(existing string, turns []turn) (string, []transcript.Turn) {
	var content strings.Builder
	content.WriteString(existing)

	indexed := make([]transcript.Turn, 0, len(turns))
	for _, t := range turns {
		if content.Len() > 0 {
			content.WriteString("\n\n")
		}

		indexed = append(indexed, transcript.Turn{
			Role:    t.role,
			Content: t.text,
			Offset:  int64(content.Len()),
		})
		content.WriteString(t.text)
	}

	return content.String(), indexed
}

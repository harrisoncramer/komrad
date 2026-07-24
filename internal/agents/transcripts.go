package agents

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"

	"claude-agents/internal/store"
)

// transcriptStore is the part of the store the transcript syncer reads and
// writes. It lists every agent, including deleted ones, so search spans the full
// history, and it reads and writes the per-session transcript.
type transcriptStore interface {
	ListAll(ctx context.Context) ([]store.Agent, error)
	GetTranscript(ctx context.Context, sessionID string) (store.Transcript, error)
	UpsertTranscript(ctx context.Context, sessionID, content string, offset int64) error
}

// TranscriptSyncer keeps each session's searchable transcript in step with the
// JSONL Claude Code writes to disk. Rather than hook into Claude's lifecycle,
// which fires unreliably, it reads the on-disk log directly on a timer. The file
// is named for the session's claude id, which the tool assigns at launch, so the
// path is known without any cooperation from Claude. Missing a pass loses
// nothing, since the next pass reads whatever the file now holds.
type TranscriptSyncer struct {
	store       transcriptStore
	projectsDir string
}

// NewTranscriptSyncer builds a syncer over the store, reading logs from
// projectsDir, which is Claude's ~/.claude/projects.
func NewTranscriptSyncer(s transcriptStore, projectsDir string) *TranscriptSyncer {
	return &TranscriptSyncer{store: s, projectsDir: projectsDir}
}

// Sync folds any new transcript text for every tracked session into the store. It
// is idempotent and self-limiting. Each session reads only the bytes appended
// since its last sync, so a pass is cheap even over a large corpus, and a session
// whose log is unchanged is skipped entirely.
func (ts *TranscriptSyncer) Sync(ctx context.Context) error {
	agents, err := ts.store.ListAll(ctx)
	if err != nil {
		return fmt.Errorf("failed to list agents for transcript sync: %w", err)
	}

	for _, agent := range agents {
		if agent.ClaudeSessionID == "" {
			continue
		}
		if err := ts.syncOne(ctx, agent); err != nil {
			return fmt.Errorf("failed to sync transcript for %q: %w", agent.Name, err)
		}
	}

	return nil
}

// syncOne reads the tail of one session's log and appends its flattened text to
// the stored transcript. A missing log is a no-op, as is a log with no new
// complete lines.
func (ts *TranscriptSyncer) syncOne(ctx context.Context, agent store.Agent) error {
	path := ts.logPath(agent.ClaudeSessionID)
	if path == "" {
		return nil
	}

	info, err := os.Stat(path)
	if err != nil {
		return nil //nolint:nilerr // a log that is not on disk yet is not an error
	}

	tr, err := ts.store.GetTranscript(ctx, agent.ID)
	if err != nil {
		return err
	}

	offset, content := tr.SyncedOffset, tr.Content
	// The log shrank, so Claude rewrote it, which a --resume can do. The stored
	// offset no longer points into the same content, so reindex from the start to
	// match the file again.
	if info.Size() < offset {
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

	if text := flattenTranscript(appended); text != "" {
		content = appendTurn(content, text)
	}

	return ts.store.UpsertTranscript(ctx, agent.ID, content, offset+consumed)
}

// logPath resolves a claude session id to its JSONL log, or "" when no log
// exists. The file is named for the session id under a per-directory folder, so
// a glob on the id finds it without reproducing Claude's directory-name encoding,
// which is version-specific.
func (ts *TranscriptSyncer) logPath(claudeSessionID string) string {
	matches, err := filepath.Glob(filepath.Join(ts.projectsDir, "*", claudeSessionID+".jsonl"))
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

// flattenTranscript turns raw JSONL bytes into the plain conversation text worth
// searching, the user's prompts and the assistant's prose in order. Tool calls,
// tool results, thinking, and all non-conversation entries are left out so the
// index stays lean and a match reads as something actually said.
func flattenTranscript(data []byte) string {
	var turns []string
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
			turns = append(turns, text)
		}
	}

	return strings.Join(turns, "\n\n")
}

// blockText extracts the prose from a message's content, which is either a plain
// string or an array of typed blocks. Only text survives.
func blockText(content json.RawMessage) string {
	var plain string
	if err := json.Unmarshal(content, &plain); err == nil {
		return strings.TrimSpace(plain)
	}

	var blocks []contentBlock
	if err := json.Unmarshal(content, &blocks); err != nil {
		return ""
	}

	var parts []string
	for _, block := range blocks {
		if block.Type == "text" && strings.TrimSpace(block.Text) != "" {
			parts = append(parts, strings.TrimSpace(block.Text))
		}
	}

	return strings.Join(parts, "\n\n")
}

// appendTurn joins newly-synced text onto the transcript so far, blank-line
// separated, so appended turns stay readable in a snippet.
func appendTurn(existing, added string) string {
	if existing == "" {
		return added
	}

	return existing + "\n\n" + added
}

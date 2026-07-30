package claude

import (
	"encoding/json"
	"path/filepath"
	"strings"

	"claude-agents/internal/harness"
)

// GetLogPath finds the JSONL log for a session id under Claude's projects dir, or ""
// when no log exists. A glob on the id finds it without reproducing Claude's
// version-specific directory-name encoding.
func (h *Harness) GetLogPath(sessionID string) string {
	if sessionID == "" {
		return ""
	}

	matches, err := filepath.Glob(filepath.Join(h.projectsDir, "*", sessionID+".jsonl"))
	if err != nil || len(matches) == 0 {
		return ""
	}

	return matches[0]
}

// ReadLog reads whatever the log at path holds past offset and flattens it into turns.
// Claude appends a record per line, and a shrunk file means it rewrote the log, which a
// --resume can do.
func (h *Harness) ReadLog(path string, offset int64) (harness.LogRead, error) {
	return harness.ReadAppendOnlyLog(path, offset, flattenTranscript)
}

// transcriptLine is the part of a JSONL entry the flattener reads. Only user and
// assistant turns carry conversation, so everything else is skipped by type.
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

// flattenTranscript turns raw JSONL bytes into the turns worth searching, the user's
// prompts and the assistant's prose in order. Tool calls, tool results, thinking, and
// non-conversation entries are left out so a match reads as something actually said.
func flattenTranscript(data []byte) []harness.Turn {
	var turns []harness.Turn
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
			turns = append(turns, harness.Turn{Role: line.Type, Content: text})
		}
	}

	return turns
}

// blockText extracts the prose from a message's content, which is either a plain
// string or an array of typed blocks. Only text blocks survive.
func blockText(content json.RawMessage) string {
	var plain string
	if err := json.Unmarshal(content, &plain); err == nil {
		return harness.CleanText(plain)
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
		if text := harness.CleanText(block.Text); text != "" {
			parts = append(parts, text)
		}
	}

	return strings.Join(parts, "\n\n")
}

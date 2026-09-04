package claude

import (
	"encoding/json"
	"path/filepath"
	"strings"
	"time"

	"weave/internal/harness"
)

// GetLogPath finds the JSONL log for a session id by globbing the id, which avoids reproducing
// Claude's version-specific directory-name encoding.
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

// ReadLog reads whatever the log at path holds past offset and flattens it into turns, treating
// a shrunk file as a log Claude rewrote on a resume.
func (h *Harness) ReadLog(path string, offset int64) (harness.LogRead, error) {
	return harness.ReadAppendOnlyLog(path, offset, flattenTranscript)
}

type transcriptLine struct {
	Type      string `json:"type"`
	IsMeta    bool   `json:"isMeta"`
	Timestamp string `json:"timestamp"`
	Message   *struct {
		ID      string          `json:"id"`
		Content json.RawMessage `json:"content"`
		Usage   *messageUsage   `json:"usage"`
	} `json:"message"`
}

type messageUsage struct {
	InputTokens              int64 `json:"input_tokens"`
	CacheCreationInputTokens int64 `json:"cache_creation_input_tokens"`
	CacheReadInputTokens     int64 `json:"cache_read_input_tokens"`
	OutputTokens             int64 `json:"output_tokens"`
}

type contentBlock struct {
	Type string `json:"type"`
	Text string `json:"text"`
}

func flattenTranscript(data []byte) ([]harness.Turn, []harness.Usage) {
	var (
		turns []harness.Turn
		usage []harness.Usage
	)
	counted := map[string]bool{}
	for raw := range strings.SplitSeq(string(data), "\n") {
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

		if reported, ok := lineUsage(line); ok && !counted[reported.Key] {
			counted[reported.Key] = true
			usage = append(usage, reported)
		}

		if text := blockText(line.Message.Content); text != "" {
			turns = append(turns, harness.Turn{Role: line.Type, Content: text})
		}
	}

	return turns, usage
}

// lineUsage reads what one line reported spending, keyed by the message id because Claude
// writes a line per content block and repeats the whole usage object on every one of them.
func lineUsage(line transcriptLine) (harness.Usage, bool) {
	reported := line.Message.Usage
	if reported == nil || line.Message.ID == "" {
		return harness.Usage{}, false
	}

	at, err := time.Parse(time.RFC3339, line.Timestamp)
	if err != nil {
		return harness.Usage{}, false
	}

	return harness.Usage{
		Key:        line.Message.ID,
		At:         at,
		Input:      reported.InputTokens,
		CacheRead:  reported.CacheReadInputTokens,
		CacheWrite: reported.CacheCreationInputTokens,
		Output:     reported.OutputTokens,
	}, true
}

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

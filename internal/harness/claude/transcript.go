package claude

import (
	"encoding/json"
	"path/filepath"
	"strings"

	"weave/internal/harness"
)

// LogPath finds the JSONL log for a session id by globbing the id, so nothing here has to
// reproduce Claude's directory-name encoding.
func (h *Harness) LogPath(sessionID string) string {
	if sessionID == "" {
		return ""
	}

	matches, err := filepath.Glob(filepath.Join(h.projectsDir(), "*", sessionID+".jsonl"))
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
	Type    string `json:"type"`
	IsMeta  bool   `json:"isMeta"`
	Message *struct {
		Content json.RawMessage `json:"content"`
	} `json:"message"`
}

type contentBlock struct {
	Type string `json:"type"`
	Text string `json:"text"`
}

func flattenTranscript(data []byte) []harness.Turn {
	var turns []harness.Turn
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

		if text := blockText(line.Message.Content); text != "" {
			turns = append(turns, harness.Turn{Role: line.Type, Content: text})
		}
	}

	return turns
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

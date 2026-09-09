package codex

import (
	"encoding/json"
	"path/filepath"
	"strings"

	"weave/internal/harness"
)

// LogPath finds the rollout for a session id by globbing the id, so nothing here has to
// reproduce Codex's dated directory layout or read the SQLite index beside it.
func (h *Harness) LogPath(sessionID string) string {
	if sessionID == "" {
		return ""
	}

	matches, err := filepath.Glob(filepath.Join(h.sessionsDir, "*", "*", "*", "rollout-*-"+sessionID+".jsonl"))
	if err != nil || len(matches) == 0 {
		return ""
	}

	return matches[0]
}

// ReadLog reads whatever the rollout at path holds past offset and flattens it into turns,
// treating a file shorter than the stored offset as a different conversation.
func (h *Harness) ReadLog(path string, offset int64) (harness.LogRead, error) {
	return harness.ReadAppendOnlyLog(path, offset, flattenRollout)
}

type rolloutRecord struct {
	Type    string `json:"type"`
	Payload struct {
		Type    string         `json:"type"`
		Role    string         `json:"role"`
		Content []contentBlock `json:"content"`
	} `json:"payload"`
}

type contentBlock struct {
	Type string `json:"type"`
	Text string `json:"text"`
}

const environmentContext = "<environment_context>"

const (
	briefingOpen  = "<panel_briefing>"
	briefingClose = "</panel_briefing>"
)

func flattenRollout(data []byte) []harness.Turn {
	var turns []harness.Turn
	for _, raw := range strings.Split(string(data), "\n") {
		raw = strings.TrimSpace(raw)
		if raw == "" {
			continue
		}

		var record rolloutRecord
		if err := json.Unmarshal([]byte(raw), &record); err != nil {
			continue
		}
		if record.Type != "response_item" || record.Payload.Type != "message" {
			continue
		}
		if record.Payload.Role != "user" && record.Payload.Role != "assistant" {
			continue
		}

		text := blockText(record.Payload.Content)
		if text == "" || strings.HasPrefix(text, environmentContext) {
			continue
		}
		if text = withoutBriefing(text); text == "" {
			continue
		}

		turns = append(turns, harness.Turn{Role: record.Payload.Role, Content: text})
	}

	return turns
}

func withoutBriefing(text string) string {
	if !strings.HasPrefix(text, briefingOpen) {
		return text
	}

	_, after, found := strings.Cut(text, briefingClose)
	if !found {
		return ""
	}

	return strings.TrimSpace(after)
}

func blockText(blocks []contentBlock) string {
	var parts []string
	for _, block := range blocks {
		if block.Type != "input_text" && block.Type != "output_text" {
			continue
		}
		if text := harness.CleanText(block.Text); text != "" {
			parts = append(parts, text)
		}
	}

	return strings.Join(parts, "\n\n")
}

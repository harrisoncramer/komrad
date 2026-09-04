package codex

import (
	"encoding/json"
	"fmt"
	"path/filepath"
	"strings"
	"time"

	"weave/internal/harness"
)

// GetLogPath finds the rollout for a session id by globbing the id, which avoids reproducing
// Codex's year, month, and day layout or consulting the SQLite index beside it.
func (h *Harness) GetLogPath(sessionID string) string {
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
// where a file shorter than the stored offset is a different conversation under the same path.
func (h *Harness) ReadLog(path string, offset int64) (harness.LogRead, error) {
	rollout := filepath.Base(path)

	return harness.ReadAppendOnlyLog(path, offset, func(data []byte) ([]harness.Turn, []harness.Usage) {
		return flattenRollout(rollout, data)
	})
}

type rolloutRecord struct {
	Type      string `json:"type"`
	Timestamp string `json:"timestamp"`
	Payload   struct {
		Type    string         `json:"type"`
		Role    string         `json:"role"`
		Content []contentBlock `json:"content"`
		Info    *struct {
			Last  tokenCounts `json:"last_token_usage"`
			Total tokenCounts `json:"total_token_usage"`
		} `json:"info"`
	} `json:"payload"`
}

type tokenCounts struct {
	InputTokens       int64 `json:"input_tokens"`
	CachedInputTokens int64 `json:"cached_input_tokens"`
	OutputTokens      int64 `json:"output_tokens"`
	TotalTokens       int64 `json:"total_tokens"`
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

func flattenRollout(rollout string, data []byte) ([]harness.Turn, []harness.Usage) {
	var (
		turns []harness.Turn
		usage []harness.Usage
	)
	for _, raw := range strings.Split(string(data), "\n") {
		raw = strings.TrimSpace(raw)
		if raw == "" {
			continue
		}

		var record rolloutRecord
		if err := json.Unmarshal([]byte(raw), &record); err != nil {
			continue
		}
		if reported, ok := recordUsage(rollout, record); ok {
			usage = append(usage, reported)

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

	return turns, usage
}

// recordUsage reads one token_count event as what that exchange added, keyed by the pair of
// figures the event carries rather than by its timestamp, since Codex reports one exchange
// more than once and the timestamp is the only thing that differs between the reports.
func recordUsage(rollout string, record rolloutRecord) (harness.Usage, bool) {
	info := record.Payload.Info
	if record.Type != "event_msg" || record.Payload.Type != "token_count" || info == nil {
		return harness.Usage{}, false
	}

	at, err := time.Parse(time.RFC3339, record.Timestamp)
	if err != nil {
		return harness.Usage{}, false
	}

	// The pair is unique only within one conversation, since the running total starts over in
	// each rollout and two of them do collide, so the file it was read from scopes the key.
	key := fmt.Sprintf("%s#%d#%d", rollout, info.Total.TotalTokens, info.Last.TotalTokens)

	// Codex counts its cached reads inside the input figure rather than beside it, so the
	// uncached remainder is what belongs in the column Claude calls input.
	return harness.Usage{
		Key:       key,
		At:        at,
		Input:     max(info.Last.InputTokens-info.Last.CachedInputTokens, 0),
		CacheRead: info.Last.CachedInputTokens,
		Output:    info.Last.OutputTokens,
	}, true
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

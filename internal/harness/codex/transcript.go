package codex

import (
	"encoding/json"
	"path/filepath"
	"strings"

	"claude-agents/internal/harness"
)

// GetLogPath finds the rollout for a session id, or "" when none exists. Codex names
// the file after the timestamp and the id both, under a year, month, and day of
// directories, so a glob on the id finds it without reproducing that layout or
// consulting the SQLite index beside it.
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

// ReadLog reads whatever the rollout at path holds past offset and flattens it into
// turns. Codex appends to a rollout and never rewrites one, so a file shorter than the
// stored offset is a different conversation under the same path.
func (h *Harness) ReadLog(path string, offset int64) (harness.LogRead, error) {
	return harness.ReadAppendOnlyLog(path, offset, flattenRollout)
}

// rolloutRecord is the part of a rollout line the flattener reads. A rollout carries
// the conversation alongside its own bookkeeping, so only response_item records of
// type message hold anything that was said.
type rolloutRecord struct {
	Type    string `json:"type"`
	Payload struct {
		Type    string         `json:"type"`
		Role    string         `json:"role"`
		Content []contentBlock `json:"content"`
	} `json:"payload"`
}

// contentBlock is one block of a message. Codex names them for their direction, so a
// prompt arrives as input_text and an answer as output_text.
type contentBlock struct {
	Type string `json:"type"`
	Text string `json:"text"`
}

// environmentContext opens the synthetic user turn Codex injects at the start of every
// conversation, carrying the working directory and the shell. It is addressed to the
// model rather than said by anyone, so it is dropped the way the developer turns are.
const environmentContext = "<environment_context>"

// briefingOpen and briefingClose fence the panel's own briefing, which reaches a Codex
// agent as the opening message because there is nowhere else to put it. It is addressed
// to the model rather than said by anyone, so it is dropped here for the same reason.
const (
	briefingOpen  = "<panel_briefing>"
	briefingClose = "</panel_briefing>"
)

// flattenRollout turns raw rollout bytes into the turns worth searching, the user's
// prompts and the assistant's prose in order.
//
// Everything Codex says to itself is left out. The developer role carries its
// permissions rules, its own identity, and the notice that a turn was interrupted; the
// event_msg, world_state, and turn_context records are UI and accounting; and the first
// user turn is a synthetic block of environment context. What survives is what a person
// would recognise as the conversation.
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

// withoutBriefing strips the panel's briefing from an opening turn, leaving whatever the
// agent was actually asked to do. A turn that was nothing but the briefing comes back
// empty and is dropped.
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

// blockText joins the prose blocks of one message. Only the text blocks hold anything
// said, so an image or a tool payload contributes nothing.
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

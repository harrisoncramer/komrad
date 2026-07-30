package codex

import (
	"fmt"
	"os"
	"path/filepath"
	"testing"

	"claude-agents/internal/harness"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// The records below are shaped after a real rollout from codex-cli 0.144.4. Every
// conversation opens with a session_meta, three developer turns Codex addresses to
// itself, and a synthetic user turn carrying the environment, before anything a person
// said appears.
const (
	metaRecord = `{"timestamp":"2026-07-30T12:41:39Z","type":"session_meta",` +
		`"payload":{"id":"019fb3e7-2cb6-7ab3-86a0-51a670a33140","session_id":"019fb3e7-2cb6-7ab3-86a0-51a670a33140","cwd":"/repo/worktree"}}`

	permissionsRecord = `{"type":"response_item","payload":{"type":"message","role":"developer",` +
		`"content":[{"type":"input_text","text":"<permissions instructions>\nFilesystem sandboxing defines which files can be read"}]}}`

	identityRecord = `{"type":"response_item","payload":{"type":"message","role":"developer",` +
		`"content":[{"type":"input_text","text":"You are ` + "`/root`" + `, the primary agent in a team of agents"}]}}`

	abortedRecord = `{"type":"response_item","payload":{"type":"message","role":"developer",` +
		`"content":[{"type":"input_text","text":"<turn_aborted>\nThe previous turn was interrupted on purpose."}]}}`

	environmentRecord = `{"type":"response_item","payload":{"type":"message","role":"user",` +
		`"content":[{"type":"input_text","text":"<environment_context>\n  <cwd>/repo/worktree</cwd>\n  <shell>zsh</shell>"}]}}`

	promptRecord = `{"type":"response_item","payload":{"type":"message","role":"user",` +
		`"content":[{"type":"input_text","text":"how do I use trigram search"}]}}`

	briefedRecord = `{"type":"response_item","payload":{"type":"message","role":"user",` +
		`"content":[{"type":"input_text","text":"<panel_briefing>\nYou are a background agent run by claude-agents.\n</panel_briefing>\n\nhow do I use trigram search"}]}}`

	briefingOnlyRecord = `{"type":"response_item","payload":{"type":"message","role":"user",` +
		`"content":[{"type":"input_text","text":"<panel_briefing>\nYou are a background agent run by claude-agents.\n</panel_briefing>"}]}}`

	unclosedBriefingRecord = `{"type":"response_item","payload":{"type":"message","role":"user",` +
		`"content":[{"type":"input_text","text":"<panel_briefing>\nYou are a background agent"}]}}`

	answerRecord = `{"type":"response_item","payload":{"type":"message","role":"assistant",` +
		`"content":[{"type":"output_text","text":"Use the tsvector column."}]}}`

	eventRecord       = `{"timestamp":"2026-07-30T12:41:40Z","type":"event_msg","payload":{"type":"task_started"}}`
	worldStateRecord  = `{"timestamp":"2026-07-30T12:41:40Z","type":"world_state","payload":{}}`
	turnContextRecord = `{"timestamp":"2026-07-30T12:41:40Z","type":"turn_context","payload":{"cwd":"/repo/worktree"}}`
)

func TestFlattenRollout(t *testing.T) {
	tests := []struct {
		name string
		in   string
		want []harness.Turn
	}{
		{
			name: "the opening record names the session rather than saying anything",
			in:   metaRecord,
			want: nil,
		},
		{
			name: "what Codex tells itself is not conversation",
			in:   permissionsRecord + "\n" + identityRecord + "\n" + abortedRecord,
			want: nil,
		},
		{
			name: "the injected environment turn is dropped, though it arrives as the user",
			in:   environmentRecord,
			want: nil,
		},
		{
			name: "the UI and accounting records are dropped",
			in:   eventRecord + "\n" + worldStateRecord + "\n" + turnContextRecord,
			want: nil,
		},
		{
			name: "malformed lines are ignored",
			in:   "not json\n" + promptRecord,
			want: []harness.Turn{{Role: "user", Content: "how do I use trigram search"}},
		},
		{
			// Postgres refuses a NUL in a text value outright, and the rollout is written
			// on the other side of this process.
			name: "a NUL is dropped from the text it arrived in",
			in:   "{\"type\":\"response_item\",\"payload\":{\"type\":\"message\",\"role\":\"user\",\"content\":[{\"type\":\"input_text\",\"text\":\"be\\u0000fore\"}]}}",
			want: []harness.Turn{{Role: "user", Content: "before"}},
		},
		{
			// The briefing reaches a Codex agent as a message, so it is in the rollout like
			// anything else. Indexed, it would be the longest thing the agent ever said and
			// would answer searches for every phrase in it.
			name: "the panel's own briefing is stripped from the turn it arrived on",
			in:   briefedRecord,
			want: []harness.Turn{{Role: "user", Content: "how do I use trigram search"}},
		},
		{
			name: "a turn that was nothing but the briefing is dropped",
			in:   briefingOnlyRecord,
			want: nil,
		},
		{
			name: "and so is one whose briefing never closed, since none of it was said",
			in:   unclosedBriefingRecord,
			want: nil,
		},
		{
			name: "a whole conversation comes back as the two people in it",
			in: metaRecord + "\n" + permissionsRecord + "\n" + identityRecord + "\n" +
				environmentRecord + "\n" + eventRecord + "\n" + promptRecord + "\n" + answerRecord,
			want: []harness.Turn{
				{Role: "user", Content: "how do I use trigram search"},
				{Role: "assistant", Content: "Use the tsvector column."},
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			assert.Equal(t, tt.want, flattenRollout([]byte(tt.in)))
		})
	}
}

// TestReadLog covers what one pass over a rollout reports, which is what the syncer's
// offset bookkeeping is built on.
func TestReadLog(t *testing.T) {
	opening := metaRecord + "\n" + promptRecord + "\n"

	tests := []struct {
		name string
		// body is the rollout on disk, and offset is where the last pass left off.
		body   string
		offset int64
		// missing reads a path with no file at it at all.
		missing      bool
		wantTurns    []harness.Turn
		wantConsumed int64
		wantRestart  bool
	}{
		{
			name:    "a rollout that has not appeared yet reports nothing",
			missing: true,
		},
		{
			name:         "a first pass reads the whole rollout",
			body:         opening,
			wantTurns:    []harness.Turn{{Role: "user", Content: "how do I use trigram search"}},
			wantConsumed: int64(len(opening)),
		},
		{
			name:   "a rollout that has not moved consumes nothing",
			body:   opening,
			offset: int64(len(opening)),
		},
		{
			name:         "only what was appended is read",
			body:         opening + answerRecord + "\n",
			offset:       int64(len(opening)),
			wantTurns:    []harness.Turn{{Role: "assistant", Content: "Use the tsvector column."}},
			wantConsumed: int64(len(answerRecord) + 1),
		},
		{
			// Written while Codex is mid-turn. Leaving it unconsumed is what makes the next
			// pass read it once, whole.
			name:         "a half-written line waits for its newline",
			body:         opening + `{"type":"response_item","payload":{"type":"mess`,
			wantTurns:    []harness.Turn{{Role: "user", Content: "how do I use trigram search"}},
			wantConsumed: int64(len(opening)),
		},
		{
			name:         "a rollout shorter than the stored offset asks for a restart",
			body:         opening,
			offset:       9000,
			wantTurns:    []harness.Turn{{Role: "user", Content: "how do I use trigram search"}},
			wantConsumed: int64(len(opening)),
			wantRestart:  true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			path := filepath.Join(t.TempDir(), "rollout.jsonl")
			if !tt.missing {
				require.NoError(t, os.WriteFile(path, []byte(tt.body), 0o600))
			}

			read, err := New(Options{SessionsDir: t.TempDir()}).ReadLog(path, tt.offset)
			require.NoError(t, err)

			assert.Equal(t, tt.wantTurns, read.Turns)
			assert.Equal(t, tt.wantConsumed, read.Consumed)
			assert.Equal(t, tt.wantRestart, read.Restart)
		})
	}
}

// TestGetLogPath covers finding a conversation by the id Codex gave it. The id is
// globbed for inside the filename rather than resolved through the SQLite index beside
// the rollouts, so nothing here depends on that schema.
func TestGetLogPath(t *testing.T) {
	sessions := t.TempDir()
	const id = "019fb3e7-2cb6-7ab3-86a0-51a670a33140"

	path := writeRollout(t, sessions, "2026-07-30T12-41-39", id, metaRecord+"\n")

	harnessUnderTest := New(Options{SessionsDir: sessions})
	assert.Equal(t, path, harnessUnderTest.GetLogPath(id))
	assert.Empty(t, harnessUnderTest.GetLogPath("019fb3e7-0000-0000-0000-000000000000"), "an id with no rollout has no path")
	assert.Empty(t, harnessUnderTest.GetLogPath(""), "and neither does an agent with no session")
}

// writeRollout puts a rollout where Codex would, under a year, month, and day of
// directories with the timestamp and the session id both in the filename.
func writeRollout(t *testing.T, sessionsDir, stamp, sessionID, body string) string {
	t.Helper()

	dir := filepath.Join(sessionsDir, "2026", "07", "30")
	require.NoError(t, os.MkdirAll(dir, 0o755))

	path := filepath.Join(dir, fmt.Sprintf("rollout-%s-%s.jsonl", stamp, sessionID))
	require.NoError(t, os.WriteFile(path, []byte(body), 0o600))

	return path
}

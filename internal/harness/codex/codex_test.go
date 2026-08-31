package codex

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// TestMintSessionID pins that Codex takes no session id on the command line, so the panel has
// to adopt the conversation Codex chose.
func TestMintSessionID(t *testing.T) {
	assert.Empty(t, NewHarness(NewHarnessParams{SessionsDir: t.TempDir()}).MintSessionID())
}

// TestFindSession covers adopting that conversation by the originator its launch stamped, the
// one thing a hand-started Codex cannot accidentally match.
func TestFindSession(t *testing.T) {
	type rollout struct {
		id         string
		stamp      string
		originator string
		noMeta     bool
		unparsed   bool
	}

	tests := []struct {
		name     string
		agentID  string
		rollouts []rollout
		want     string
	}{
		{
			name:    "no rollouts at all, which is every pass before the CLI has written one",
			agentID: "agent-1",
		},
		{
			name: "an agent with no id of its own matches nothing",
			rollouts: []rollout{
				{
					id:         "sess-somebody",
					stamp:      "12-05-00",
					originator: "weave-agent-1",
				},
			},
		},
		{
			name:    "a rollout a person started by hand carries Codex's own originator",
			agentID: "agent-1",
			rollouts: []rollout{
				{
					id:         "sess-theirs",
					stamp:      "12-05-00",
					originator: "codex-tui",
				},
			},
		},
		{
			name:    "another agent's rollout carries another agent's stamp",
			agentID: "agent-1",
			rollouts: []rollout{
				{
					id:         "sess-theirs",
					stamp:      "12-05-00",
					originator: "weave-agent-2",
				},
			},
		},
		{
			name:    "a rollout caught before its opening record is complete is not yet claimable",
			agentID: "agent-1",
			rollouts: []rollout{
				{
					id:         "sess-partial",
					stamp:      "12-05-00",
					originator: "weave-agent-1",
					unparsed:   true,
				},
			},
		},
		{
			name:    "a file that opens with something other than a session_meta is not a conversation",
			agentID: "agent-1",
			rollouts: []rollout{
				{
					id:         "sess-headless",
					stamp:      "12-05-00",
					originator: "weave-agent-1",
					noMeta:     true,
				},
			},
		},
		{
			name:    "the one this agent's launch stamped",
			agentID: "agent-1",
			rollouts: []rollout{
				{
					id:         "sess-mine",
					stamp:      "12-05-00",
					originator: "weave-agent-1",
				},
			},
			want: "sess-mine",
		},
		{
			name:    "a relaunch leaves two, and the live one is the newer",
			agentID: "agent-1",
			rollouts: []rollout{
				{
					id:         "sess-abandoned",
					stamp:      "12-05-00",
					originator: "weave-agent-1",
				},
				{
					id:         "sess-live",
					stamp:      "12-06-00",
					originator: "weave-agent-1",
				},
			},
			want: "sess-live",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			sessions := t.TempDir()

			for _, r := range tt.rollouts {
				body := `{"type":"session_meta","payload":{"id":"` + r.id + `","originator":"` + r.originator + `"}}` + "\n"
				switch {
				case r.unparsed:
					body = `{"type":"session_meta","payload":{"id":"` + r.id
				case r.noMeta:
					body = `{"type":"event_msg","payload":{"type":"task_started"}}` + "\n"
				}

				writeRollout(t, sessions, "2026-07-30T"+r.stamp, r.id, body)
			}

			found, err := NewHarness(NewHarnessParams{SessionsDir: sessions}).FindSession(tt.agentID)
			require.NoError(t, err)
			assert.Equal(t, tt.want, found)
		})
	}
}

package transcripts

import (
	"testing"

	"komrade/internal/agents"
	"komrade/internal/storetest"
	"komrade/lib/xslice"

	agentstore "komrade/internal/store/agent"
	transcriptstore "komrade/internal/store/transcript"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

const missingAgentID = "00000000-0000-0000-0000-000000000000"

func newTranscriptReader(st *storetest.Stores) *TranscriptReader {
	return NewTranscriptReader(NewTranscriptReaderParams{
		Agents:      st.Agents,
		Transcripts: st.Transcripts,
	})
}

// TestTranscriptReaderRefusesAnAgentNobodyHolds pins that a transcript read names the missing agent rather than answering empty.
func TestTranscriptReaderRefusesAnAgentNobodyHolds(t *testing.T) {
	tests := []struct {
		name string
		read func(t *testing.T, reader *TranscriptReader) error
	}{
		{
			name: "reading its transcript",
			read: func(t *testing.T, reader *TranscriptReader) error {
				_, err := reader.ReadTranscript(t.Context(), missingAgentID)

				return err
			},
		},
		{
			name: "listing its last turns",
			read: func(t *testing.T, reader *TranscriptReader) error {
				_, err := reader.ListLastTurns(t.Context(), ListLastTurnsParams{AgentID: missingAgentID})

				return err
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			require.ErrorIs(t, tt.read(t, newTranscriptReader(storetest.Open(t))), agents.ErrAgentNotFound)
		})
	}
}

// TestListLastTurns pins that a listing counts the agent's own turns, one when none is asked for.
func TestListLastTurns(t *testing.T) {
	st := storetest.Open(t)
	reader := newTranscriptReader(st)
	alpha := st.SeedAgent(t, agentstore.Agent{Name: "alpha"})
	st.SeedTurns(t, alpha.ID,
		transcriptstore.Turn{
			Role:    "user",
			Content: "opening question",
		},
		transcriptstore.Turn{
			Role:    "assistant",
			Content: "a first reading",
		},
		transcriptstore.Turn{
			Role:    "user",
			Content: "a nudge",
		},
		transcriptstore.Turn{
			Role:    "assistant",
			Content: "the conclusion",
		},
	)

	tests := []struct {
		name     string
		turns    int32
		wantSeqs []int32
	}{
		{
			name:     "no count reads the last turn the agent wrote",
			wantSeqs: []int32{3},
		},
		{
			name:     "two of its own turns come with the prompt between them",
			turns:    2,
			wantSeqs: []int32{1, 2, 3},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			turns, err := reader.ListLastTurns(t.Context(), ListLastTurnsParams{
				AgentID: alpha.ID,
				Turns:   tt.turns,
			})
			require.NoError(t, err)
			assert.Equal(t, tt.wantSeqs, xslice.Map(turns, func(turn transcriptstore.StoredTurn) int32 {
				return turn.Seq
			}))
		})
	}
}

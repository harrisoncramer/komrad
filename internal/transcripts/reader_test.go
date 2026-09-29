package transcripts

import (
	"log/slog"
	"strings"
	"testing"

	"komrad/internal/agents"
	"komrad/internal/logging"
	"komrad/internal/storetest"
	"komrad/lib/xslice"

	agentstore "komrad/internal/store/agent"
	transcriptstore "komrad/internal/store/transcript"

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

// TestSearchTranscriptsScope pins which agents a search reaches: the one named, else the caller's
// own conversation unless a qualifier names which agents to search, else every agent.
func TestSearchTranscriptsScope(t *testing.T) {
	st := storetest.Open(t)
	reader := newTranscriptReader(st)
	alpha := st.SeedAgent(t, agentstore.Agent{Name: "alpha"})
	beta := st.SeedAgent(t, agentstore.Agent{Name: "beta"})
	st.SeedTurns(t, alpha.ID, transcriptstore.Turn{Role: "user", Content: "the reconciler"})
	st.SeedTurns(t, beta.ID, transcriptstore.Turn{Role: "user", Content: "the reconciler"})

	tests := []struct {
		name       string
		query      string
		agentID    *string
		caller     string
		want       []string
		wantLogged bool
	}{
		{
			name:   "a caller naming nobody reads its own conversation",
			query:  "reconciler",
			caller: alpha.ID,
			want:   []string{"alpha"},
		},
		{
			name:       "a qualifier naming agents widens it",
			query:      "reconciler agent:beta",
			caller:     alpha.ID,
			want:       []string{"beta"},
			wantLogged: true,
		},
		{
			name:       "a qualifier naming a kind widens it",
			query:      "reconciler is:live",
			caller:     alpha.ID,
			want:       []string{"alpha", "beta"},
			wantLogged: true,
		},
		{
			name:   "a qualifier naming only when or who spoke does not",
			query:  "reconciler from:me",
			caller: alpha.ID,
			want:   []string{"alpha"},
		},
		{
			name:    "the agent named",
			query:   "reconciler",
			agentID: &beta.ID,
			caller:  alpha.ID,
			want:    []string{"beta"},
		},
		{
			name:  "no caller reads every conversation",
			query: "reconciler",
			want:  []string{"alpha", "beta"},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			var written strings.Builder
			ctx := logging.WithLogger(t.Context(), logging.New(&written, slog.LevelInfo))

			got, err := reader.SearchTranscripts(ctx, SearchTranscriptsParams{
				Query:   tt.query,
				Limit:   10,
				AgentID: tt.agentID,
				Caller:  tt.caller,
			})
			require.NoError(t, err)
			assert.ElementsMatch(t, tt.want, xslice.Map(got.Matches, func(hit transcriptstore.SearchHit) string {
				return hit.Name
			}))
			assert.Equal(t, tt.wantLogged, strings.Contains(written.String(), "agent searched every agent's transcript"))
		})
	}
}

// TestSearchTranscriptsRefusesABadQualifier pins that a qualifier that does not parse is an
// error naming it rather than a search for the word.
func TestSearchTranscriptsRefusesABadQualifier(t *testing.T) {
	reader := newTranscriptReader(storetest.Open(t))

	_, err := reader.SearchTranscripts(t.Context(), SearchTranscriptsParams{Query: "since:yesterdy"})
	require.ErrorIs(t, err, ErrBadQualifier)
}

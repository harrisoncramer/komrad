package transcripts

import (
	"strings"
	"testing"
	"time"

	"komrad/internal/searchsyntax"
	transcriptstore "komrad/internal/store/transcript"
	"komrad/lib/xptr"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// TestParseSearch covers what each transcript qualifier narrows a search to and the values it refuses.
func TestParseSearch(t *testing.T) {
	zone := time.FixedZone("EDT", -4*60*60)
	now := time.Date(2026, 9, 27, 15, 30, 0, 0, zone)
	midnight := time.Date(2026, 9, 27, 0, 0, 0, 0, zone)

	tests := []struct {
		name    string
		text    string
		want    search
		wantErr string
	}{
		{
			name:    "a day and a span at once",
			text:    "since:3d on:today",
			wantErr: "on: cannot be given with since: or until:",
		},
		{
			name:    "a span after a day",
			text:    "on:today until:2h",
			wantErr: "until: cannot be given with on:",
		},
		{
			name:    "an unknown kind",
			text:    "is:tester",
			wantErr: "is:tester is not one of",
		},
		{
			name:    "an unknown speaker",
			text:    "from:them",
			wantErr: "from:them is not one of me or agent",
		},
		{
			name: "prompts",
			text: "from:me migration",
			want: search{Words: "migration", Filter: transcriptstore.Filter{Speaker: xptr.Ptr("user")}},
		},
		{
			name: "what the agent wrote",
			text: "FROM:Agent",
			want: search{Filter: transcriptstore.Filter{Speaker: xptr.Ptr("assistant")}},
		},
		{
			name: "an agent and a workflow by name, quoted to hold a space",
			text: `agent:mail workflow:"komrad web"`,
			want: search{Filter: transcriptstore.Filter{AgentName: xptr.Ptr("mail"), WorkflowName: xptr.Ptr("komrad web")}},
		},
		{
			name: "kinds wanted and left out",
			text: "is:reviewer is:implementer -is:orchestrator",
			want: search{Filter: transcriptstore.Filter{
				Kinds:         []string{"reviewer", "implementer"},
				ExcludedKinds: []string{"orchestrator"},
			}},
		},
		{
			name: "a span back from now",
			text: "since:3d until:2h",
			want: search{Filter: transcriptstore.Filter{
				Since: xptr.Ptr(now.Add(-72 * time.Hour)),
				Until: xptr.Ptr(now.Add(-2 * time.Hour)),
			}},
		},
		{
			name: "until the end of yesterday",
			text: "until:yesterday",
			want: search{Filter: transcriptstore.Filter{Until: &midnight}},
		},
		{
			name: "one day by date",
			text: "on:2026-09-20",
			want: search{Filter: transcriptstore.Filter{
				Since: xptr.Ptr(time.Date(2026, 9, 20, 0, 0, 0, 0, zone)),
				Until: xptr.Ptr(time.Date(2026, 9, 21, 0, 0, 0, 0, zone)),
			}},
		},
		{
			name: "a key this search does not know stays a word",
			text: "reconciler sort:new",
			want: search{Words: "reconciler sort:new"},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := parseSearch(tt.text, now)
			if tt.wantErr != "" {
				require.ErrorIs(t, err, searchsyntax.ErrBadQualifier)
				assert.ErrorContains(t, err, tt.wantErr)

				return
			}

			require.NoError(t, err)
			assert.Equal(t, &tt.want, got)
		})
	}
}

// TestParseSearchKnowsEveryListedQualifier pins the help the panel shows to the qualifiers the parser reads.
func TestParseSearchKnowsEveryListedQualifier(t *testing.T) {
	sample := strings.NewReplacer("NAME", "payouts", "DATE", "2026-09-20")
	for _, listed := range searchsyntax.Transcripts.Help {
		t.Run(listed.Syntax, func(t *testing.T) {
			got, err := parseSearch(sample.Replace(listed.Syntax), time.Now())
			require.NoError(t, err)
			assert.Empty(t, got.Words)
		})
	}
}

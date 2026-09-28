package transcripts

import (
	"testing"
	"time"

	transcriptstore "komrad/internal/store/transcript"
	"komrad/lib/xptr"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// TestParseQuery covers each qualifier a query can hold and the ones it refuses.
func TestParseQuery(t *testing.T) {
	zone := time.FixedZone("EDT", -4*60*60)
	now := time.Date(2026, 9, 27, 15, 30, 0, 0, zone)
	midnight := time.Date(2026, 9, 27, 0, 0, 0, 0, zone)

	tests := []struct {
		name    string
		text    string
		want    Query
		wantErr string
	}{
		{
			name:    "a value that does not parse",
			text:    "since:yesterdy",
			wantErr: "since:yesterdy is not a day",
		},
		{
			name:    "a span too long to count back from now",
			text:    "since:9999999999999w",
			wantErr: "since:9999999999999w is not a day",
		},
		{
			name:    "a qualifier with no value",
			text:    "since:",
			wantErr: "since: needs a value",
		},
		{
			name:    "a qualifier given twice",
			text:    "from:me from:agent",
			wantErr: "from: is given twice",
		},
		{
			name:    "a day and a span at once",
			text:    "since:3d on:today",
			wantErr: "on: cannot be given with since: or until:",
		},
		{
			name:    "live and archived at once",
			text:    "is:live is:archived",
			wantErr: "is:live and is:archived cannot both be given",
		},
		{
			name:    "a negated qualifier that has no opposite",
			text:    "-from:me",
			wantErr: "from: cannot be negated",
		},
		{
			name:    "an unknown kind",
			text:    "is:tester",
			wantErr: "is:tester is not one of",
		},
		{
			name:    "an unknown sort",
			text:    "sort:loud",
			wantErr: "sort:loud is not one of best, new or old",
		},
		{
			name: "words alone stay words",
			text: `reconciler -"stale pane" --force`,
			want: Query{Words: `reconciler -"stale pane" --force`},
		},
		{
			name: "prompts",
			text: "from:me migration",
			want: Query{Words: "migration", Filter: transcriptstore.Filter{Speaker: xptr.Ptr("user")}},
		},
		{
			name: "what the agent wrote",
			text: "FROM:Agent",
			want: Query{Filter: transcriptstore.Filter{Speaker: xptr.Ptr("assistant")}},
		},
		{
			name: "an agent and a workflow by name, quoted to hold a space",
			text: `agent:mail workflow:"komrad web"`,
			want: Query{Filter: transcriptstore.Filter{AgentName: xptr.Ptr("mail"), WorkflowName: xptr.Ptr("komrad web")}},
		},
		{
			name: "kinds wanted and left out",
			text: "is:reviewer is:implementer -is:orchestrator",
			want: Query{Filter: transcriptstore.Filter{
				Kinds:         []string{"reviewer", "implementer"},
				ExcludedKinds: []string{"orchestrator"},
			}},
		},
		{
			name: "live agents",
			text: "is:live",
			want: Query{Filter: transcriptstore.Filter{Archived: xptr.Ptr(false)}},
		},
		{
			name: "agents not live",
			text: "-is:live",
			want: Query{Filter: transcriptstore.Filter{Archived: xptr.Ptr(true)}},
		},
		{
			name: "a span back from now",
			text: "since:3d until:2h",
			want: Query{Filter: transcriptstore.Filter{
				Since: xptr.Ptr(now.Add(-72 * time.Hour)),
				Until: xptr.Ptr(now.Add(-2 * time.Hour)),
			}},
		},
		{
			name: "since the start of today",
			text: "since:today",
			want: Query{Filter: transcriptstore.Filter{Since: &midnight}},
		},
		{
			name: "until the end of yesterday",
			text: "until:yesterday",
			want: Query{Filter: transcriptstore.Filter{Until: &midnight}},
		},
		{
			name: "one day by date",
			text: "on:2026-09-20",
			want: Query{Filter: transcriptstore.Filter{
				Since: xptr.Ptr(time.Date(2026, 9, 20, 0, 0, 0, 0, zone)),
				Until: xptr.Ptr(time.Date(2026, 9, 21, 0, 0, 0, 0, zone)),
			}},
		},
		{
			name: "a sort",
			text: "sort:new",
			want: Query{Sort: transcriptstore.SortNewest},
		},
		{
			name: "a key this search does not know stays a word",
			text: "http://localhost:5460 state:idle",
			want: Query{Words: "http://localhost:5460 state:idle"},
		},
		{
			name: "a quoted qualifier is a phrase",
			text: `"from:me"`,
			want: Query{Words: `"from:me"`},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := ParseQuery(tt.text, now)
			if tt.wantErr != "" {
				require.ErrorIs(t, err, ErrBadQualifier)
				assert.Contains(t, err.Error(), tt.wantErr)

				return
			}

			require.NoError(t, err)
			assert.Equal(t, tt.want, got)
		})
	}
}

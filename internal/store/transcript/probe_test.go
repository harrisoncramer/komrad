//go:build rankprobe

package transcript

import (
	"flag"
	"strings"
	"testing"

	"komrade/internal/store"

	"github.com/stretchr/testify/require"
)

var (
	probeURL    = flag.String("probe.url", "", "an engine's reader url, such as postgres://komrade_engine_default_reader:...@localhost:5460/komrade")
	probeEngine = flag.String("probe.engine", "", "the id of the engine the reader belongs to")
	probeQuery  = flag.String("probe.query", "", "query to run")
	probeWatch  = flag.String("probe.watch", "", "agent name whose hits are reported wherever they land")
	probePages  = flag.Int("probe.pages", 5, "pages of ten to walk")
)

// TestProbeRanking prints where a query's hits land in a real database.
func TestProbeRanking(t *testing.T) {
	if *probeURL == "" || *probeEngine == "" || *probeQuery == "" {
		t.Skip("set -probe.url, -probe.engine and -probe.query")
	}

	ctx := t.Context()
	pool, err := store.OpenReadOnly(ctx, *probeURL, *probeEngine)
	require.NoError(t, err)
	t.Cleanup(pool.Close)

	st := NewTranscriptStore(pool)

	var (
		cursor *Cursor
		pos    int
	)
	for page := range *probePages {
		results, err := st.Search(ctx, SearchParams{
			Query:  *probeQuery,
			Limit:  10,
			Cursor: cursor,
		})
		require.NoError(t, err)

		t.Logf("page %d: %d matches, %d near", page+1, len(results.Matches), len(results.Near))
		for _, hit := range results.Matches {
			pos++
			if page == 0 || hit.Name == *probeWatch {
				t.Logf("  %4d %-28s %5.3f %s", pos, hit.Name, hit.Rank, oneLine(hit.Snippet))
			}
		}
		for _, hit := range results.Near {
			pos++
			if page == 0 || hit.Name == *probeWatch {
				t.Logf("  %4d %-28s %5.3f near %s", pos, hit.Name, hit.Rank, oneLine(hit.Snippet))
			}
		}

		cursor = results.Next
		if cursor == nil {
			break
		}
	}
}

func oneLine(s string) string {
	s = strings.NewReplacer(SnippetOpen, "[", SnippetClose, "]", "\n", " ").Replace(s)
	if len(s) > 70 {
		s = s[:70]
	}

	return s
}

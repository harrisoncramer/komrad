package client

import (
	"context"
	"errors"
	"fmt"
	"testing"

	paginationv1 "komrade/proto/gen/pagination/v1"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// TestListAll covers the loop every listing pages through: a caller asks for
// everything and has only the returned slice to go on, so a listing that stopped
// early has to say so rather than hand back the part it managed.
func TestListAll(t *testing.T) {
	tests := []struct {
		name      string
		pages     [][]string
		failOn    int
		neverEnds bool
		wantErr   bool
		want      []string
	}{
		{
			name:    "a listing that fails on its first page",
			pages:   [][]string{{"a"}},
			failOn:  1,
			wantErr: true,
		},
		{
			name:    "a listing that fails partway",
			pages:   [][]string{{"a"}, {"b"}},
			failOn:  2,
			wantErr: true,
		},
		{
			name:      "a cursor that never retires",
			pages:     [][]string{{"a"}},
			neverEnds: true,
			wantErr:   true,
		},
		{
			name:  "an empty listing",
			pages: [][]string{{}},
			want:  nil,
		},
		{
			name:  "everything on one page",
			pages: [][]string{{"a", "b"}},
			want:  []string{"a", "b"},
		},
		{
			name:  "records gathered across several pages",
			pages: [][]string{{"a", "b"}, {"c"}, {"d"}},
			want:  []string{"a", "b", "c", "d"},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			var (
				calls   int
				cursors []string
			)

			fetch := func(_ context.Context, page *paginationv1.Pagination) ([]string, string, error) {
				calls++
				cursors = append(cursors, page.GetCursor())

				if tt.failOn == calls {
					return nil, "", errors.New("the server hung up")
				}

				records := tt.pages[min(calls-1, len(tt.pages)-1)]
				if tt.neverEnds || calls < len(tt.pages) {
					return records, fmt.Sprintf("token-%d", calls), nil
				}

				return records, "", nil
			}

			got, err := listAll(t.Context(), fetch)
			if tt.wantErr {
				require.Error(t, err)
				assert.Nil(t, got, "a listing that did not finish returns nothing")

				return
			}

			require.NoError(t, err)
			assert.Equal(t, tt.want, got)

			require.Len(t, cursors, len(tt.pages))
			assert.Empty(t, cursors[0])
			for i := 1; i < len(cursors); i++ {
				assert.Equal(t, fmt.Sprintf("token-%d", i), cursors[i])
			}
		})
	}
}

// TestListAllStopsAtTheCap pins that the guard against an unretiring cursor stops
// rather than spinning, and stops where it says it does.
func TestListAllStopsAtTheCap(t *testing.T) {
	calls := 0
	fetch := func(_ context.Context, _ *paginationv1.Pagination) ([]string, string, error) {
		calls++

		return []string{"a"}, "always-another", nil
	}

	_, err := listAll(t.Context(), fetch)
	require.Error(t, err)
	assert.Equal(t, maxPages, calls, "the cap is what stopped it")
}

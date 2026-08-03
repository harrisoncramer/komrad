package pagination

import (
	"encoding/base64"
	"errors"
	"fmt"
	"testing"

	paginationv1 "weave/proto/gen/pagination/v1"

	"connectrpc.com/connect"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// testCursor stands in for a real cursor: an id-shaped field the query compares
// against a column that only accepts some of the strings that can arrive in it.
type testCursor struct {
	ID string `json:"i"`
}

func (c testCursor) Validate() error {
	if c.ID != "valid" {
		return fmt.Errorf("cursor id %q is not an id", c.ID)
	}

	return nil
}

// testResource is the listed thing the paginator mints the next token from.
type testResource struct {
	cursor testCursor
}

func (r testResource) Cursor() (testCursor, error) { return r.cursor, nil }

func token(t *testing.T, raw string) string {
	t.Helper()
	return base64.URLEncoding.EncodeToString([]byte(raw))
}

// TestNewCursor covers the three ways a page token can be wrong. All of them are the
// caller handing back something other than what it was given, so all of them are its
// own mistake rather than the server's, and a cursor that decodes into a value the
// query cannot use is no different from one that would not decode at all.
func TestNewCursor(t *testing.T) {
	tests := []struct {
		name   string
		cursor string
		want   *testCursor
	}{
		{
			name: "no token is the first page",
		},
		{
			name:   "a token holding a usable cursor is decoded",
			cursor: token(t, `{"i":"valid"}`),
			want:   &testCursor{ID: "valid"},
		},
		{
			name:   "a token that is not base64 is refused",
			cursor: "not base64!",
		},
		{
			name:   "a token that is not the cursor's shape is refused",
			cursor: token(t, `["valid"]`),
		},
		{
			name:   "a token the cursor itself refuses is refused",
			cursor: token(t, `{"i":"not-an-id"}`),
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			paginator, err := New[testCursor, testResource](&paginationv1.Pagination{Cursor: &tt.cursor})

			if tt.cursor != "" && tt.want == nil {
				require.Error(t, err)
				assert.Equal(t, connect.CodeInvalidArgument, connect.CodeOf(err))
				return
			}

			require.NoError(t, err)
			assert.Equal(t, tt.want, paginator.Cursor())
		})
	}
}

// TestNewCursorErrorCarriesTheReason pins that the cursor's own complaint reaches the
// caller. The token is opaque, so a caller told only that it is invalid has nothing to
// tell it apart from a token it mangled, replayed, or built itself.
func TestNewCursorErrorCarriesTheReason(t *testing.T) {
	cursor := token(t, `{"i":"not-an-id"}`)

	_, err := New[testCursor, testResource](&paginationv1.Pagination{Cursor: &cursor})
	require.Error(t, err)

	var connectErr *connect.Error
	require.True(t, errors.As(err, &connectErr))
	assert.Contains(t, connectErr.Message(), `cursor id "not-an-id" is not an id`)
}

// TestNextTokenRoundTrips pins that the token a page ends with is one the next request
// is served from, which is what makes the cursor's own refusal above safe to apply to
// every token that arrives.
func TestNextTokenRoundTrips(t *testing.T) {
	limit := int32(2)

	paginator, err := New[testCursor, testResource](&paginationv1.Pagination{Limit: &limit})
	require.NoError(t, err)

	next, err := paginator.NextToken([]testResource{{cursor: testCursor{ID: "first"}}, {cursor: testCursor{ID: "valid"}}})
	require.NoError(t, err)
	require.NotNil(t, next)

	resumed, err := New[testCursor, testResource](&paginationv1.Pagination{Cursor: next})
	require.NoError(t, err)
	assert.Equal(t, &testCursor{ID: "valid"}, resumed.Cursor())
}

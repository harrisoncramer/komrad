package gitlab

import (
	"net/http"
	"testing"

	"komrad/internal/forge"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// TestReplyToThread covers a reply posted as a note on the merge request discussion.
func TestReplyToThread(t *testing.T) {
	t.Parallel()

	const path = "/projects/harrisoncramer%2Fkomrad/merge_requests/1/discussions/d1/notes"

	tests := []struct {
		name     string
		threadID string
		answers  map[string]answer
		want     string
		wantErr  error
	}{
		{
			name:     "a reply on a diff discussion",
			threadID: "gid://gitlab/Discussion/d1",
			answers:  map[string]answer{path: {status: http.StatusCreated, body: `{"id":9,"type":"DiffNote"}`}},
			want:     "gid://gitlab/DiffNote/9",
		},
		{
			name:     "a reply on a discussion of the merge request as a whole",
			threadID: "gid://gitlab/Discussion/d1",
			answers:  map[string]answer{path: {status: http.StatusCreated, body: `{"id":9,"type":"DiscussionNote"}`}},
			want:     "gid://gitlab/DiscussionNote/9",
		},
		{
			name:     "a reply on a note gitlab gives no type",
			threadID: "gid://gitlab/Discussion/d1",
			answers:  map[string]answer{path: {status: http.StatusCreated, body: `{"id":9,"type":null}`}},
			want:     "gid://gitlab/Note/9",
		},
		{
			name:     "a credential gitlab will not let reply",
			threadID: "gid://gitlab/Discussion/d1",
			answers:  map[string]answer{path: {status: http.StatusForbidden, body: `{"message":"403 Forbidden"}`}},
			wantErr:  forge.ErrUnauthorized,
		},
		{
			name:     "a discussion gitlab does not have",
			threadID: "gid://gitlab/Discussion/d1",
			wantErr:  forge.ErrNotFound,
		},
		{
			name:     "a thread that is not a gitlab discussion",
			threadID: "T_1",
			wantErr:  forge.ErrNotFound,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			f, _, fake := serve(t, tt.answers)

			got, err := f.ReplyToThread(t.Context(), forge.ReplyToThreadParams{Repo: komrad, Number: 1, ThreadID: tt.threadID, OnCode: true, Body: "renamed"})
			if tt.wantErr != nil {
				require.ErrorIs(t, err, tt.wantErr)

				return
			}
			require.NoError(t, err)
			assert.Equal(t, tt.want, got)
			assert.Equal(t, call{Path: path, Method: http.MethodPost, Body: map[string]any{"body": "renamed"}}, call{Path: fake.last(t).Path, Method: fake.last(t).Method, Body: fake.last(t).Body})
		})
	}
}

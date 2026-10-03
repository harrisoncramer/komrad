package github

import (
	"net/http"
	"testing"

	"komrad/internal/forge"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// TestReplyToThread covers a reply posted on a review thread or, with no path, on the pull request conversation.
func TestReplyToThread(t *testing.T) {
	t.Parallel()

	const conversation = "/repos/harrisoncramer/komrad/issues/1/comments"

	tests := []struct {
		name     string
		onCode   bool
		answers  map[string]answer
		want     string
		wantErr  error
		wantCall call
	}{
		{
			name:     "a reply on a line thread is added to the thread",
			onCode:   true,
			answers:  map[string]answer{"AddThreadReply": {body: data(`{"addPullRequestReviewThreadReply":{"comment":{"id":"PRRC_9"}}}`)}},
			want:     "PRRC_9",
			wantCall: call{Path: "/graphql", Operation: "AddThreadReply", Variables: map[string]any{"thread": "T_1", "body": "renamed"}},
		},
		{
			name:     "a reply on the change as a whole is a new conversation comment",
			answers:  map[string]answer{conversation: {status: http.StatusCreated, body: `{"id":7,"node_id":"IC_7"}`}},
			want:     "IC_7",
			wantCall: call{Path: conversation, Body: map[string]any{"body": "renamed"}},
		},
		{
			name:    "a thread github no longer has",
			onCode:  true,
			answers: map[string]answer{"AddThreadReply": {body: `{"data":{"addPullRequestReviewThreadReply":null},"errors":[{"type":"NOT_FOUND","path":["addPullRequestReviewThreadReply"],"message":"Could not resolve to a node with the global id of 'T_1'"}]}`}},
			wantErr: forge.ErrNotFound,
		},
		{
			name:    "a reply github turns down",
			onCode:  true,
			answers: map[string]answer{"AddThreadReply": {body: `{"data":{"addPullRequestReviewThreadReply":null},"errors":[{"type":"FORBIDDEN","path":["addPullRequestReviewThreadReply"],"message":"the thread is locked"}]}`}},
			wantErr: forge.ErrReplyRefused,
		},
		{
			name:    "a conversation comment github turns down",
			answers: map[string]answer{conversation: {status: http.StatusUnprocessableEntity, body: `{"message":"Validation Failed"}`}},
			wantErr: forge.ErrReplyRefused,
		},
		{
			name:    "a pull request github no longer has",
			answers: map[string]answer{},
			wantErr: forge.ErrNotFound,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			f, _, fake := serve(t, tt.answers)

			id, err := f.ReplyToThread(t.Context(), forge.ReplyToThreadParams{
				Repo:     komrad,
				Number:   1,
				ThreadID: "T_1",
				OnCode:   tt.onCode,
				Body:     "renamed",
			})
			if tt.wantErr != nil {
				require.ErrorIs(t, err, tt.wantErr)

				return
			}
			require.NoError(t, err)
			assert.Equal(t, tt.want, id)
			seen := fake.last(t)
			assert.Equal(t, tt.wantCall, call{Path: seen.Path, Body: seen.Body, Operation: seen.Operation, Variables: seen.Variables})
		})
	}
}

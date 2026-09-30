package github

import (
	"testing"
	"time"

	"komrad/internal/forge"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

const reviewThreadsAnswer = `{"repository":{"pullRequest":{
  "reviewThreads":{"pageInfo":{"hasNextPage":false},"nodes":[
    {"id":"T_1","isResolved":false,"path":"main.go","line":3,"originalLine":2,"comments":{"pageInfo":{"hasNextPage":false},"nodes":[
      {"id":"C_1","url":"https://github.com/harrisoncramer/komrad/pull/1#discussion_r1","body":"rename this","createdAt":"2026-09-20T10:00:00Z","lastEditedAt":null,"state":"SUBMITTED","author":{"__typename":"User","login":"dana"}},
      {"id":"C_2","url":"https://github.com/harrisoncramer/komrad/pull/1#discussion_r2","body":"draft reply","createdAt":"2026-09-20T11:00:00Z","lastEditedAt":null,"state":"PENDING","author":{"__typename":"User","login":"harrisoncramer"}}
    ]}},
    {"id":"T_2","isResolved":true,"path":"main.go","line":null,"originalLine":9,"comments":{"pageInfo":{"hasNextPage":false},"nodes":[
      {"id":"C_3","url":"https://github.com/harrisoncramer/komrad/pull/1#discussion_r3","body":"lint","createdAt":"2026-09-20T10:30:00Z","lastEditedAt":"2026-09-20T10:40:00Z","state":"SUBMITTED","author":{"__typename":"Bot","login":"coderabbitai"}}
    ]}},
    {"id":"T_3","isResolved":false,"path":"main.go","line":5,"originalLine":5,"comments":{"pageInfo":{"hasNextPage":false},"nodes":[
      {"id":"C_4","url":"https://github.com/harrisoncramer/komrad/pull/1#discussion_r4","body":"unsent","createdAt":"2026-09-20T10:30:00Z","lastEditedAt":null,"state":"PENDING","author":{"__typename":"User","login":"harrisoncramer"}}
    ]}}
  ]},
  "comments":{"pageInfo":{"hasPreviousPage":false},"nodes":[
    {"id":"IC_1","url":"https://github.com/harrisoncramer/komrad/pull/1#issuecomment-1","body":"looks close","createdAt":"2026-09-20T09:00:00Z","lastEditedAt":null,"author":null}
  ]},
  "reviews":{"pageInfo":{"hasPreviousPage":false},"nodes":[
    {"id":"R_1","url":"https://github.com/harrisoncramer/komrad/pull/1#pullrequestreview-1","body":"","createdAt":"2026-09-20T10:00:00Z","lastEditedAt":null,"author":{"__typename":"User","login":"dana"}}
  ]}
}}}`

// TestReviewThreads covers which of GitHub's answers become threads, where each is anchored, and
// who wrote each comment.
func TestReviewThreads(t *testing.T) {
	t.Parallel()

	f, _, fake := serve(t, map[string]answer{"ReviewThreads": {body: data(reviewThreadsAnswer)}})

	answer, err := f.ReviewThreads(t.Context(), komrad, 1)
	require.NoError(t, err)

	edited := time.Date(2026, 9, 20, 10, 40, 0, 0, time.UTC)
	assert.Equal(t, &forge.Threads{Threads: []forge.ReviewThread{
		{
			ProviderID: "T_1",
			Path:       "main.go",
			Line:       3,
			URL:        "https://github.com/harrisoncramer/komrad/pull/1#discussion_r1",
			Comments: []forge.ReviewComment{
				{ProviderID: "C_1", Login: "dana", Body: "rename this", WrittenAt: time.Date(2026, 9, 20, 10, 0, 0, 0, time.UTC)},
			},
		},
		{
			ProviderID: "T_2",
			Path:       "main.go",
			Line:       9,
			URL:        "https://github.com/harrisoncramer/komrad/pull/1#discussion_r3",
			Resolved:   true,
			Comments: []forge.ReviewComment{
				{ProviderID: "C_3", Login: "coderabbitai", Bot: true, Body: "lint", WrittenAt: time.Date(2026, 9, 20, 10, 30, 0, 0, time.UTC), EditedAt: &edited},
			},
		},
		{
			ProviderID: "IC_1",
			URL:        "https://github.com/harrisoncramer/komrad/pull/1#issuecomment-1",
			Comments: []forge.ReviewComment{
				{ProviderID: "IC_1", Body: "looks close", WrittenAt: time.Date(2026, 9, 20, 9, 0, 0, 0, time.UTC)},
			},
		},
	}}, answer)

	seen := fake.last(t)
	assert.Equal(t, map[string]any{"owner": "harrisoncramer", "name": "komrad", "number": float64(1)}, seen.Variables)
}

// TestReviewThreadsSaysWhenTheAnswerIsCut covers a page GitHub did not return marking the answer truncated.
func TestReviewThreadsSaysWhenTheAnswerIsCut(t *testing.T) {
	t.Parallel()

	cut := `{"repository":{"pullRequest":{"reviewThreads":{"pageInfo":{"hasNextPage":true},"nodes":[]},"comments":{"pageInfo":{"hasPreviousPage":false},"nodes":[]},"reviews":{"pageInfo":{"hasPreviousPage":false},"nodes":[]}}}}`
	f, _, _ := serve(t, map[string]answer{"ReviewThreads": {body: data(cut)}})

	answer, err := f.ReviewThreads(t.Context(), komrad, 1)
	require.NoError(t, err)
	assert.True(t, answer.Truncated)
}

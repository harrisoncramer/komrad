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
    {"id":"T_1","isResolved":false,"path":"main.go","line":3,"originalLine":2,"diffSide":"RIGHT","subjectType":"LINE","comments":{"pageInfo":{"hasNextPage":false},"nodes":[
      {"id":"C_1","url":"https://github.com/harrisoncramer/komrad/pull/1#discussion_r1","body":"rename this","createdAt":"2026-09-20T10:00:00Z","lastEditedAt":null,"state":"SUBMITTED","author":{"__typename":"User","login":"dana"},"diffHunk":"@@ -1,2 +1,3 @@\n package main\n+func rename() {}","commit":{"oid":"c2"},"originalCommit":{"oid":"c1"}},
      {"id":"C_2","url":"https://github.com/harrisoncramer/komrad/pull/1#discussion_r2","body":"draft reply","createdAt":"2026-09-20T11:00:00Z","lastEditedAt":null,"state":"PENDING","author":{"__typename":"User","login":"harrisoncramer"}}
    ]}},
    {"id":"T_2","isResolved":true,"path":"main.go","line":null,"originalLine":2,"diffSide":"RIGHT","subjectType":"LINE","comments":{"pageInfo":{"hasNextPage":false},"nodes":[
      {"id":"C_3","url":"https://github.com/harrisoncramer/komrad/pull/1#discussion_r3","body":"lint","createdAt":"2026-09-20T10:30:00Z","lastEditedAt":"2026-09-20T10:40:00Z","state":"SUBMITTED","author":{"__typename":"Bot","login":"coderabbitai"},"diffHunk":"@@ -1 +1,2 @@\n package main\n+x := 1","commit":null,"originalCommit":{"oid":"c1"}}
    ]}},
    {"id":"T_3","isResolved":false,"path":"main.go","line":5,"diffSide":"RIGHT","comments":{"pageInfo":{"hasNextPage":false},"nodes":[
      {"id":"C_4","url":"https://github.com/harrisoncramer/komrad/pull/1#discussion_r4","body":"unsent","createdAt":"2026-09-20T10:30:00Z","lastEditedAt":null,"state":"PENDING","author":{"__typename":"User","login":"harrisoncramer"}}
    ]}},
    {"id":"T_4","isResolved":false,"path":"main.go","line":7,"originalLine":7,"diffSide":"LEFT","subjectType":"LINE","comments":{"pageInfo":{"hasNextPage":false},"nodes":[
      {"id":"C_5","url":"https://github.com/harrisoncramer/komrad/pull/1#discussion_r5","body":"why drop this","createdAt":"2026-09-20T10:50:00Z","lastEditedAt":null,"state":"SUBMITTED","author":{"__typename":"User","login":"dana"},"diffHunk":"@@ -6,2 +6,1 @@\n func main() {\n-\tdrop()","commit":{"oid":"c2"},"originalCommit":{"oid":"c2"}}
    ]}},
    {"id":"T_5","isResolved":false,"path":"go.mod","line":1,"originalLine":1,"diffSide":"RIGHT","subjectType":"FILE","comments":{"pageInfo":{"hasNextPage":false},"nodes":[
      {"id":"C_6","url":"https://github.com/harrisoncramer/komrad/pull/1#discussion_r6","body":"bump this","createdAt":"2026-09-20T10:55:00Z","lastEditedAt":null,"state":"SUBMITTED","author":{"__typename":"User","login":"dana"},"diffHunk":"","commit":{"oid":"c2"},"originalCommit":{"oid":"c2"}}
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
			CommitSHA:  "c2",
			Quote:      "func rename() {}",
			URL:        "https://github.com/harrisoncramer/komrad/pull/1#discussion_r1",
			Comments: []forge.ReviewComment{
				{ProviderID: "C_1", Login: "dana", Body: "rename this", WrittenAt: time.Date(2026, 9, 20, 10, 0, 0, 0, time.UTC)},
			},
		},
		{
			ProviderID: "T_2",
			Path:       "main.go",
			Line:       2,
			CommitSHA:  "c1",
			Quote:      "x := 1",
			URL:        "https://github.com/harrisoncramer/komrad/pull/1#discussion_r3",
			Resolved:   true,
			Comments: []forge.ReviewComment{
				{ProviderID: "C_3", Login: "coderabbitai", Bot: true, Body: "lint", WrittenAt: time.Date(2026, 9, 20, 10, 30, 0, 0, time.UTC), EditedAt: &edited},
			},
		},
		{
			ProviderID: "T_4",
			Path:       "main.go",
			Line:       7,
			OldSide:    true,
			CommitSHA:  "c2",
			Quote:      "\tdrop()",
			URL:        "https://github.com/harrisoncramer/komrad/pull/1#discussion_r5",
			Comments: []forge.ReviewComment{
				{ProviderID: "C_5", Login: "dana", Body: "why drop this", WrittenAt: time.Date(2026, 9, 20, 10, 50, 0, 0, time.UTC)},
			},
		},
		{
			ProviderID: "T_5",
			Path:       "go.mod",
			URL:        "https://github.com/harrisoncramer/komrad/pull/1#discussion_r6",
			Comments: []forge.ReviewComment{
				{ProviderID: "C_6", Login: "dana", Body: "bump this", WrittenAt: time.Date(2026, 9, 20, 10, 55, 0, 0, time.UTC)},
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

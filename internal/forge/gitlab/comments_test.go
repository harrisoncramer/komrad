package gitlab

import (
	"testing"
	"time"

	"komrad/internal/forge"
	"komrad/lib/xslice"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

const reviewThreadsAnswer = `{
	"data": {
		"project": {
			"mergeRequest": {
				"discussions": {
					"pageInfo": {
						"hasNextPage": false
					},
					"nodes": [
						{
							"id": "gid://gitlab/Discussion/d1",
							"resolved": true,
							"truncatedDiffLines": [
								{
									"text": "+alpha"
								},
								{
									"text": "+bravo"
								},
								{
									"text": "+charlie"
								}
							],
							"notes": {
								"pageInfo": {
									"hasNextPage": false
								},
								"nodes": [
									{
										"id": "gid://gitlab/DiffNote/1",
										"system": false,
										"internal": false,
										"body": "charlie should be capitalised",
										"createdAt": "2026-10-02T12:25:57Z",
										"lastEditedAt": "2026-10-02T12:26:06Z",
										"lastEditedBy": null,
										"url": "https://gitlab.com/harrisoncramer/komrad/-/merge_requests/1#note_1",
										"author": {
											"username": "dana",
											"bot": false
										},
										"position": {
											"positionType": "text",
											"filePath": "notes.txt",
											"oldLine": null,
											"newLine": 3,
											"diffRefs": {
												"headSha": "c2"
											}
										}
									},
									{
										"id": "gid://gitlab/DiffNote/2",
										"system": false,
										"internal": false,
										"body": "agreed, will fix",
										"createdAt": "2026-10-02T12:26:01Z",
										"lastEditedAt": "2026-10-02T12:26:04Z",
										"lastEditedBy": {
											"username": "harrisoncramer"
										},
										"url": "https://gitlab.com/harrisoncramer/komrad/-/merge_requests/1#note_2",
										"author": {
											"username": "harrisoncramer",
											"bot": false
										},
										"position": {
											"positionType": "text",
											"filePath": "notes.txt",
											"oldLine": null,
											"newLine": 3,
											"diffRefs": {
												"headSha": "c2"
											}
										}
									}
								]
							}
						},
						{
							"id": "gid://gitlab/Discussion/d2",
							"resolved": false,
							"truncatedDiffLines": [
								{
									"text": "+delta"
								},
								{
									"text": "+echo"
								}
							],
							"notes": {
								"pageInfo": {
									"hasNextPage": false
								},
								"nodes": [
									{
										"id": "gid://gitlab/DiffNote/3",
										"system": false,
										"internal": false,
										"body": "echo will change",
										"createdAt": "2026-10-02T12:25:58Z",
										"lastEditedAt": "2026-10-02T12:25:58Z",
										"lastEditedBy": null,
										"url": "https://gitlab.com/harrisoncramer/komrad/-/merge_requests/1#note_3",
										"author": {
											"username": "lint-bot",
											"bot": true
										},
										"position": {
											"positionType": "text",
											"filePath": "notes.txt",
											"oldLine": null,
											"newLine": 5,
											"diffRefs": {
												"headSha": "c1"
											}
										}
									},
									{
										"id": "gid://gitlab/DiffNote/4",
										"system": true,
										"internal": false,
										"body": "changed this line in version 2 of the diff",
										"createdAt": "2026-10-02T12:26:09Z",
										"lastEditedAt": "2026-10-02T12:26:09Z",
										"lastEditedBy": null,
										"url": "https://gitlab.com/harrisoncramer/komrad/-/merge_requests/1#note_4",
										"author": {
											"username": "harrisoncramer",
											"bot": false
										},
										"position": {
											"positionType": "text",
											"filePath": "notes.txt",
											"oldLine": null,
											"newLine": 5,
											"diffRefs": {
												"headSha": "c2"
											}
										}
									}
								]
							}
						},
						{
							"id": "gid://gitlab/Discussion/d3",
							"resolved": false,
							"truncatedDiffLines": [
								{
									"text": "-# komrad"
								}
							],
							"notes": {
								"pageInfo": {
									"hasNextPage": false
								},
								"nodes": [
									{
										"id": "gid://gitlab/DiffNote/5",
										"system": false,
										"internal": false,
										"body": "why drop the title",
										"createdAt": "2026-10-02T12:25:59Z",
										"lastEditedAt": "2026-10-02T12:25:59Z",
										"lastEditedBy": null,
										"url": "https://gitlab.com/harrisoncramer/komrad/-/merge_requests/1#note_5",
										"author": {
											"username": "dana",
											"bot": false
										},
										"position": {
											"positionType": "text",
											"filePath": "README.md",
											"oldLine": 1,
											"newLine": null,
											"diffRefs": {
												"headSha": "c2"
											}
										}
									}
								]
							}
						},
						{
							"id": "gid://gitlab/Discussion/d4",
							"resolved": false,
							"truncatedDiffLines": [],
							"notes": {
								"pageInfo": {
									"hasNextPage": false
								},
								"nodes": [
									{
										"id": "gid://gitlab/DiffNote/6",
										"system": false,
										"internal": false,
										"body": "whole file remark",
										"createdAt": "2026-10-02T12:26:00Z",
										"lastEditedAt": "2026-10-02T12:26:00Z",
										"lastEditedBy": null,
										"url": "https://gitlab.com/harrisoncramer/komrad/-/merge_requests/1#note_6",
										"author": {
											"username": "dana",
											"bot": false
										},
										"position": {
											"positionType": "file",
											"filePath": "notes.txt",
											"oldLine": null,
											"newLine": null,
											"diffRefs": {
												"headSha": "c1"
											}
										}
									}
								]
							}
						},
						{
							"id": "gid://gitlab/Discussion/d5",
							"resolved": false,
							"truncatedDiffLines": [],
							"notes": {
								"pageInfo": {
									"hasNextPage": false
								},
								"nodes": [
									{
										"id": "gid://gitlab/Note/7",
										"system": false,
										"internal": false,
										"body": "overall looks fine",
										"createdAt": "2026-10-02T12:25:59Z",
										"lastEditedAt": "2026-10-02T12:25:59Z",
										"lastEditedBy": null,
										"url": "https://gitlab.com/harrisoncramer/komrad/-/merge_requests/1#note_7",
										"author": {
											"username": "dana",
											"bot": false
										},
										"position": null
									},
									{
										"id": "gid://gitlab/Note/8",
										"system": false,
										"internal": true,
										"body": "between us",
										"createdAt": "2026-10-02T12:26:02Z",
										"lastEditedAt": "2026-10-02T12:26:02Z",
										"lastEditedBy": null,
										"url": "https://gitlab.com/harrisoncramer/komrad/-/merge_requests/1#note_8",
										"author": {
											"username": "dana",
											"bot": false
										},
										"position": null
									},
									{
										"id": "gid://gitlab/Note/9",
										"system": false,
										"internal": false,
										"body": "  ",
										"createdAt": "2026-10-02T12:26:03Z",
										"lastEditedAt": "2026-10-02T12:26:03Z",
										"lastEditedBy": null,
										"url": "https://gitlab.com/harrisoncramer/komrad/-/merge_requests/1#note_9",
										"author": null,
										"position": null
									}
								]
							}
						},
						{
							"id": "gid://gitlab/Discussion/d6",
							"resolved": false,
							"truncatedDiffLines": [],
							"notes": {
								"pageInfo": {
									"hasNextPage": false
								},
								"nodes": [
									{
										"id": "gid://gitlab/Note/10",
										"system": true,
										"internal": false,
										"body": "added 1 commit",
										"createdAt": "2026-10-02T12:26:09Z",
										"lastEditedAt": "2026-10-02T12:26:09Z",
										"lastEditedBy": null,
										"url": "https://gitlab.com/harrisoncramer/komrad/-/merge_requests/1#note_10",
										"author": {
											"username": "harrisoncramer",
											"bot": false
										},
										"position": null
									}
								]
							}
						}
					]
				}
			}
		}
	}
}`

// TestReviewThreads covers a merge request's discussions mapped into threads: resolution, a line
// GitLab has not carried to the head, the old side, a whole file, a general discussion, bot
// authors, edits, and the notes left out.
func TestReviewThreads(t *testing.T) {
	t.Parallel()

	f, _, fake := serve(t, map[string]answer{"ReviewThreads": {body: reviewThreadsAnswer}})

	answer, err := f.ReviewThreads(t.Context(), komrad, 1)
	require.NoError(t, err)

	edited := time.Date(2026, 10, 2, 12, 26, 4, 0, time.UTC)
	assert.Equal(t, &forge.Threads{Threads: []forge.ReviewThread{
		{
			ProviderID: "gid://gitlab/Discussion/d1",
			Path:       "notes.txt",
			Line:       3,
			CommitSHA:  "c2",
			Quote:      "charlie",
			URL:        "https://gitlab.com/harrisoncramer/komrad/-/merge_requests/1#note_1",
			Resolved:   true,
			Comments: []forge.ReviewComment{
				{
					ProviderID: "gid://gitlab/DiffNote/1",
					ReviewID:   "gid://gitlab/Discussion/d1",
					Login:      "dana",
					Body:       "charlie should be capitalised",
					WrittenAt:  time.Date(2026, 10, 2, 12, 25, 57, 0, time.UTC),
				},
				{
					ProviderID: "gid://gitlab/DiffNote/2",
					ReviewID:   "gid://gitlab/Discussion/d1",
					Login:      "harrisoncramer",
					Body:       "agreed, will fix",
					WrittenAt:  time.Date(2026, 10, 2, 12, 26, 1, 0, time.UTC),
					EditedAt:   &edited,
				},
			},
		},
		{
			ProviderID: "gid://gitlab/Discussion/d2",
			Path:       "notes.txt",
			Line:       5,
			CommitSHA:  "c1",
			Quote:      "echo",
			URL:        "https://gitlab.com/harrisoncramer/komrad/-/merge_requests/1#note_3",
			Comments: []forge.ReviewComment{
				{
					ProviderID: "gid://gitlab/DiffNote/3",
					ReviewID:   "gid://gitlab/Discussion/d2",
					Login:      "lint-bot",
					Bot:        true,
					Body:       "echo will change",
					WrittenAt:  time.Date(2026, 10, 2, 12, 25, 58, 0, time.UTC),
				},
			},
		},
		{
			ProviderID: "gid://gitlab/Discussion/d3",
			Path:       "README.md",
			Line:       1,
			OldSide:    true,
			CommitSHA:  "c2",
			Quote:      "# komrad",
			URL:        "https://gitlab.com/harrisoncramer/komrad/-/merge_requests/1#note_5",
			Comments: []forge.ReviewComment{
				{
					ProviderID: "gid://gitlab/DiffNote/5",
					ReviewID:   "gid://gitlab/Discussion/d3",
					Login:      "dana",
					Body:       "why drop the title",
					WrittenAt:  time.Date(2026, 10, 2, 12, 25, 59, 0, time.UTC),
				},
			},
		},
		{
			ProviderID: "gid://gitlab/Discussion/d4",
			Path:       "notes.txt",
			URL:        "https://gitlab.com/harrisoncramer/komrad/-/merge_requests/1#note_6",
			Comments: []forge.ReviewComment{
				{
					ProviderID: "gid://gitlab/DiffNote/6",
					ReviewID:   "gid://gitlab/Discussion/d4",
					Login:      "dana",
					Body:       "whole file remark",
					WrittenAt:  time.Date(2026, 10, 2, 12, 26, 0, 0, time.UTC),
				},
			},
		},
		{
			ProviderID: "gid://gitlab/Discussion/d5",
			URL:        "https://gitlab.com/harrisoncramer/komrad/-/merge_requests/1#note_7",
			Comments: []forge.ReviewComment{
				{
					ProviderID: "gid://gitlab/Note/7",
					ReviewID:   "gid://gitlab/Discussion/d5",
					Login:      "dana",
					Body:       "overall looks fine",
					WrittenAt:  time.Date(2026, 10, 2, 12, 25, 59, 0, time.UTC),
				},
			},
		},
	}}, answer)

	seen := fake.last(t)
	assert.Equal(t, map[string]any{"path": "harrisoncramer/komrad", "iid": "1", "after": nil}, seen.Variables)
}

// TestReviewThreadsReadsEveryPage covers a merge request whose discussions run past one page, read
// from each page's end cursor until GitLab says there are no more.
func TestReviewThreadsReadsEveryPage(t *testing.T) {
	t.Parallel()

	f, _, fake := serve(t, nil)
	fake.handle = func(seen call) (answer, bool) {
		if seen.Variables["after"] == nil {
			return answer{body: `{
	"data": {
		"project": {
			"mergeRequest": {
				"discussions": {
					"pageInfo": {
						"hasNextPage": true,
						"endCursor": "p1"
					},
					"nodes": [
						{
							"id": "gid://gitlab/Discussion/d1",
							"resolved": false,
							"truncatedDiffLines": [],
							"notes": {
								"pageInfo": {
									"hasNextPage": false
								},
								"nodes": [
									{
										"id": "gid://gitlab/Note/1",
										"system": true,
										"internal": false,
										"body": "added 1 commit",
										"createdAt": "2026-10-02T12:25:57Z",
										"lastEditedBy": null,
										"url": "https://gitlab.com/harrisoncramer/komrad/-/merge_requests/1#note_1",
										"author": {
											"username": "harrisoncramer",
											"bot": false
										},
										"position": null
									}
								]
							}
						}
					]
				}
			}
		}
	}
}`}, true
		}

		return answer{body: `{
	"data": {
		"project": {
			"mergeRequest": {
				"discussions": {
					"pageInfo": {
						"hasNextPage": false,
						"endCursor": "p2"
					},
					"nodes": [
						{
							"id": "gid://gitlab/Discussion/d2",
							"resolved": false,
							"truncatedDiffLines": [],
							"notes": {
								"pageInfo": {
									"hasNextPage": false
								},
								"nodes": [
									{
										"id": "gid://gitlab/Note/2",
										"system": false,
										"internal": false,
										"body": "overall looks fine",
										"createdAt": "2026-10-02T12:25:59Z",
										"lastEditedBy": null,
										"url": "https://gitlab.com/harrisoncramer/komrad/-/merge_requests/1#note_2",
										"author": {
											"username": "dana",
											"bot": false
										},
										"position": null
									}
								]
							}
						}
					]
				}
			}
		}
	}
}`}, true
	}

	answer, err := f.ReviewThreads(t.Context(), komrad, 1)
	require.NoError(t, err)

	assert.Equal(t, &forge.Threads{Threads: []forge.ReviewThread{{
		ProviderID: "gid://gitlab/Discussion/d2",
		URL:        "https://gitlab.com/harrisoncramer/komrad/-/merge_requests/1#note_2",
		Comments: []forge.ReviewComment{{
			ProviderID: "gid://gitlab/Note/2",
			ReviewID:   "gid://gitlab/Discussion/d2",
			Login:      "dana",
			Body:       "overall looks fine",
			WrittenAt:  time.Date(2026, 10, 2, 12, 25, 59, 0, time.UTC),
		}},
	}}}, answer)
	assert.Equal(t, []any{nil, "p1"}, xslice.Map(fake.calls, func(seen call) any { return seen.Variables["after"] }))
}

// TestReviewThreadsSaysWhenTheAnswerIsCut covers discussions running past the pages one read
// takes, or one discussion's notes running past a page, marking the answer truncated.
func TestReviewThreadsSaysWhenTheAnswerIsCut(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name  string
		body  string
		calls int
	}{
		{
			name: "more discussions",
			body: `{
	"data": {
		"project": {
			"mergeRequest": {
				"discussions": {
					"pageInfo": {
						"hasNextPage": true,
						"endCursor": "next"
					},
					"nodes": []
				}
			}
		}
	}
}`,
			calls: maxDiscussionPages,
		},
		{
			name: "more notes in a discussion",
			body: `{
	"data": {
		"project": {
			"mergeRequest": {
				"discussions": {
					"pageInfo": {
						"hasNextPage": false
					},
					"nodes": [
						{
							"id": "gid://gitlab/Discussion/d1",
							"resolved": false,
							"truncatedDiffLines": [],
							"notes": {
								"pageInfo": {
									"hasNextPage": true
								},
								"nodes": []
							}
						}
					]
				}
			}
		}
	}
}`,
			calls: 1,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			f, _, fake := serve(t, map[string]answer{"ReviewThreads": {body: tt.body}})

			answer, err := f.ReviewThreads(t.Context(), komrad, 1)
			require.NoError(t, err)
			assert.True(t, answer.Truncated)
			assert.Len(t, fake.calls, tt.calls)
		})
	}
}

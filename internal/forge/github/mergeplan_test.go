package github

import (
	"testing"

	"komrad/internal/forge"
	forgev1 "komrad/proto/gen/forge/v1"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// TestPlanMerge covers reading the methods a pull request may merge by, with the message for
// each, and refusing one that has finished.
func TestPlanMerge(t *testing.T) {
	t.Parallel()

	const address = "https://github.com/harrisoncramer/komrad/pull/742"

	tests := []struct {
		name    string
		answer  answer
		want    *forge.MergePlan
		wantErr error
	}{
		{
			name: "every method with the message github writes for it",
			answer: answer{body: data(`{
	"repository": {
		"viewerDefaultMergeMethod": "MERGE",
		"mergeCommitAllowed": true,
		"squashMergeAllowed": true,
		"rebaseMergeAllowed": false,
		"mergeCommitTitle": "MERGE_MESSAGE",
		"mergeCommitMessage": "PR_TITLE",
		"squashMergeCommitTitle": "COMMIT_OR_PR_TITLE",
		"squashMergeCommitMessage": "COMMIT_MESSAGES",
		"pullRequest": {
			"number": 742,
			"state": "OPEN",
			"title": "Merge by an allowed method",
			"body": "Merges stop failing.",
			"headRefName": "fix-merge",
			"headRefOid": "abc123",
			"headRepositoryOwner": {
				"login": "harrisoncramer"
			},
			"commits": {
				"totalCount": 2,
				"nodes": [
					{
						"commit": {
							"messageHeadline": "Read the branch rules",
							"messageBody": "Rulesets narrow the methods."
						}
					},
					{
						"commit": {
							"messageHeadline": "Pick an allowed method",
							"messageBody": ""
						}
					}
				]
			},
			"baseRef": {
				"branchProtectionRule": {
					"requiresLinearHistory": true
				},
				"rules": {
					"nodes": []
				}
			}
		}
	}
}`)},
			want: &forge.MergePlan{
				Options: []forge.MergeOption{
					{
						Method: forgev1.MergeMethod_MERGE_METHOD_MERGE,
						Message: &forge.CommitMessage{
							Headline: "Merge pull request #742 from harrisoncramer/fix-merge",
							Body:     "Merge by an allowed method",
						},
					},
					{
						Method:  forgev1.MergeMethod_MERGE_METHOD_SQUASH,
						Allowed: true,
						Message: &forge.CommitMessage{
							Headline: "Merge by an allowed method (#742)",
							Body:     "* Read the branch rules\n\nRulesets narrow the methods.\n\n* Pick an allowed method",
						},
					},
					{Method: forgev1.MergeMethod_MERGE_METHOD_REBASE},
				},
				DefaultMethod: forgev1.MergeMethod_MERGE_METHOD_SQUASH,
				HeadSHA:       "abc123",
			},
		},
		{
			name: "a pull request that has already merged",
			answer: answer{body: data(`{
	"repository": {
		"pullRequest": {
			"number": 742,
			"state": "MERGED"
		}
	}
}`)},
			wantErr: forge.ErrNotMergeable,
		},
		{
			name: "a pull request github does not have",
			answer: answer{body: data(`{
	"repository": {
		"pullRequest": null
	}
}`)},
			wantErr: forge.ErrNotFound,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			f, _, _ := serve(t, map[string]answer{"MergePlan": tt.answer})

			plan, err := f.PlanMerge(t.Context(), address)
			if tt.wantErr != nil {
				require.ErrorIs(t, err, tt.wantErr)

				return
			}
			require.NoError(t, err)
			assert.Equal(t, tt.want, plan)
		})
	}
}

// TestMessageFor covers the message GitHub writes for each of its commit message settings.
func TestMessageFor(t *testing.T) {
	t.Parallel()

	target := &planTarget{
		Number:      742,
		Title:       "Merge by an allowed method",
		Body:        "Merges stop failing.",
		HeadRefName: "fix-merge",
	}
	single := *target
	single.Commits.TotalCount = 1
	single.Commits.Nodes = []pullRequestCommit{{}}
	single.Commits.Nodes[0].Commit.MessageHeadline = "Pick an allowed method"
	single.Commits.Nodes[0].Commit.MessageBody = "Rulesets narrow the methods."

	tests := []struct {
		name     string
		settings messageSettings
		method   string
		target   *planTarget
		want     *forge.CommitMessage
	}{
		{
			name:     "a merge commit titled by the pull request with its body",
			settings: messageSettings{MergeCommitTitle: "PR_TITLE", MergeCommitMessage: "PR_BODY"},
			method:   "MERGE",
			target:   target,
			want:     &forge.CommitMessage{Headline: "Merge by an allowed method (#742)", Body: "Merges stop failing."},
		},
		{
			name:     "a merge commit from a branch without an owner left blank",
			settings: messageSettings{MergeCommitTitle: "MERGE_MESSAGE", MergeCommitMessage: "BLANK"},
			method:   "MERGE",
			target:   target,
			want:     &forge.CommitMessage{Headline: "Merge pull request #742 from fix-merge"},
		},
		{
			name:     "a squash titled by the pull request with its body",
			settings: messageSettings{SquashMergeCommitTitle: "PR_TITLE", SquashMergeCommitMessage: "PR_BODY"},
			method:   "SQUASH",
			target:   &single,
			want:     &forge.CommitMessage{Headline: "Merge by an allowed method (#742)", Body: "Merges stop failing."},
		},
		{
			name:     "a squash of one commit titled and written by that commit",
			settings: messageSettings{SquashMergeCommitTitle: "COMMIT_OR_PR_TITLE", SquashMergeCommitMessage: "COMMIT_MESSAGES"},
			method:   "SQUASH",
			target:   &single,
			want:     &forge.CommitMessage{Headline: "Pick an allowed method (#742)", Body: "Rulesets narrow the methods."},
		},
		{
			name:     "a squash left blank",
			settings: messageSettings{SquashMergeCommitTitle: "PR_TITLE", SquashMergeCommitMessage: "BLANK"},
			method:   "SQUASH",
			target:   &single,
			want:     &forge.CommitMessage{Headline: "Merge by an allowed method (#742)"},
		},
		{
			name:   "a rebase writes no commit of its own",
			method: "REBASE",
			target: target,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			assert.Equal(t, tt.want, tt.settings.messageFor(tt.method, tt.target))
		})
	}
}

package github

import (
	"testing"

	"github.com/stretchr/testify/assert"
)

// TestChooseMethod covers picking a merge method the repository and its base branch's rules allow.
func TestChooseMethod(t *testing.T) {
	t.Parallel()

	everything := mergeSettings{
		ViewerDefaultMergeMethod: "MERGE",
		MergeCommitAllowed:       true,
		SquashMergeAllowed:       true,
		RebaseMergeAllowed:       true,
	}
	branch := func(rules ...branchRule) *baseRef {
		base := &baseRef{}
		base.Rules.Nodes = rules

		return base
	}
	pullRequestRule := func(methods ...string) branchRule {
		return branchRule{Type: "PULL_REQUEST", Parameters: &ruleParameters{AllowedMergeMethods: methods}}
	}

	tests := []struct {
		name     string
		settings mergeSettings
		base     *baseRef
		want     string
	}{
		{
			name:     "the default when nothing refuses it",
			settings: everything,
			base:     branch(),
			want:     "MERGE",
		},
		{
			name:     "the default when the pull request has no base branch",
			settings: everything,
			want:     "MERGE",
		},
		{
			name: "the first method the repository allows when it refuses the default",
			settings: mergeSettings{
				ViewerDefaultMergeMethod: "MERGE",
				RebaseMergeAllowed:       true,
			},
			base: branch(),
			want: "REBASE",
		},
		{
			name:     "the only method a branch rule allows",
			settings: everything,
			base:     branch(pullRequestRule("SQUASH")),
			want:     "SQUASH",
		},
		{
			name:     "a method every branch rule allows",
			settings: everything,
			base:     branch(pullRequestRule("SQUASH", "REBASE"), pullRequestRule("REBASE")),
			want:     "REBASE",
		},
		{
			name:     "a branch rule that names no methods refuses none",
			settings: everything,
			base:     branch(pullRequestRule()),
			want:     "MERGE",
		},
		{
			name:     "linear history refuses merge commits",
			settings: everything,
			base:     branch(branchRule{Type: "REQUIRED_LINEAR_HISTORY"}),
			want:     "SQUASH",
		},
		{
			name:     "linear history in branch protection refuses merge commits",
			settings: everything,
			base:     &baseRef{BranchProtectionRule: &branchProtection{RequiresLinearHistory: true}},
			want:     "SQUASH",
		},
		{
			name: "rules on a method the repository refuses do not bring it back",
			settings: mergeSettings{
				ViewerDefaultMergeMethod: "REBASE",
				SquashMergeAllowed:       true,
			},
			base: branch(pullRequestRule("MERGE", "SQUASH")),
			want: "SQUASH",
		},
		{
			name: "the default when nothing is allowed",
			settings: mergeSettings{
				ViewerDefaultMergeMethod: "SQUASH",
				SquashMergeAllowed:       true,
			},
			base: branch(pullRequestRule("MERGE")),
			want: "SQUASH",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			assert.Equal(t, tt.want, tt.settings.chooseMethod(tt.base))
		})
	}
}

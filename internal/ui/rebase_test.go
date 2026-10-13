package ui

import (
	"testing"

	"github.com/stretchr/testify/assert"
)

// TestRefreshDescriptionSaysWhatWillHappen covers the rebase description naming what becomes of the branch.
func TestRefreshDescriptionSaysWhatWillHappen(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name     string
		plan     rebasePlan
		want     []string
		wantNone []string
	}{
		{
			name: "a branch with nothing of its own is just moved",
			plan: rebasePlan{
				baseRef: "origin/main",
			},
			want:     []string{"no commits of its own", "moves it to origin/main"},
			wantNone: []string{"replayed"},
		},
		{
			name: "a branch the base already holds loses those commits",
			plan: rebasePlan{
				baseRef: "origin/main",
				landed:  3,
			},
			want: []string{"already holds every commit", "drops 3 commits from it"},
		},
		{
			name: "an ordinary rebase says what it replays",
			plan: rebasePlan{
				baseRef: "origin/main",
				replay:  2,
			},
			want:     []string{"replays the branch's 2 commits onto origin/main"},
			wantNone: []string{"drops"},
		},
		{
			name: "a partly landed branch says both halves",
			plan: rebasePlan{
				baseRef: "origin/main",
				landed:  2,
				replay:  1,
			},
			want: []string{"replays the branch's 1 commit", "It drops 2 more that the base already holds"},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			description := rebaseDescription(tt.plan)
			for _, want := range tt.want {
				assert.Contains(t, description, want)
			}
			for _, none := range tt.wantNone {
				assert.NotContains(t, description, none)
			}
		})
	}
}

// TestRefreshDescriptionAlwaysSaysWhatItDoesToTheFiles covers the description saying what happens to the files.
func TestRefreshDescriptionAlwaysSaysWhatItDoesToTheFiles(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name string
		plan rebasePlan
	}{
		{
			name: "a branch the base already holds",
			plan: rebasePlan{
				baseRef: "origin/main",
				landed:  3,
			},
		},
		{
			name: "a branch with nothing of its own",
			plan: rebasePlan{baseRef: "origin/main"},
		},
		{
			name: "a branch with commits to replay",
			plan: rebasePlan{
				baseRef: "origin/main",
				replay:  2,
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			description := rebaseDescription(tt.plan)
			assert.Contains(t, description, "files change")
			assert.Contains(t, description, "komrad keeps every untracked file")
			assert.Contains(t, description, "saves a copy of the worktree")
		})
	}
}

// TestRefreshAffirmativeNamesWhatRuns covers the button naming what actually runs.
func TestRefreshAffirmativeNamesWhatRuns(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name string
		plan rebasePlan
		want string
	}{
		{
			name: "a branch with nothing of its own is moved",
			plan: rebasePlan{},
			want: "Move",
		},
		{
			name: "a branch the base already holds is moved",
			plan: rebasePlan{landed: 3},
			want: "Move",
		},
		{
			name: "a branch with commits of its own is rebased",
			plan: rebasePlan{replay: 2},
			want: "Rebase",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			assert.Equal(t, tt.want, rebaseAffirmative(tt.plan))
		})
	}
}

// TestRefreshWarning covers what the rebase confirmation warns about.
func TestRefreshWarning(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name     string
		plan     rebasePlan
		want     []string
		wantNone bool
	}{
		{
			name:     "a rebase that would run cleanly warns about nothing",
			plan:     rebasePlan{replay: 2},
			wantNone: true,
		},
		{
			name:     "the landed commits are left to the description, which says what becomes of them",
			plan:     rebasePlan{landed: 2},
			wantNone: true,
		},
		{
			name: "merge commits, which a rebase throws away before replaying what they settled",
			plan: rebasePlan{merges: 2},
			want: []string{"throws away 2 merge commits"},
		},
		{
			name: "the paths the two sides both changed",
			plan: rebasePlan{conflictPaths: []string{"a.go", "b.go"}},
			want: []string{"Both sides changed a.go, b.go"},
		},
		{
			name: "more paths than are worth reading out",
			plan: rebasePlan{conflictPaths: []string{"a.go", "b.go", "c.go", "d.go", "e.go"}},
			want: []string{"a.go, b.go, c.go and 2 more"},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			warning := rebaseWarning(tt.plan)
			if tt.wantNone {
				assert.Empty(t, warning)

				return
			}

			for _, want := range tt.want {
				assert.Contains(t, warning, want)
			}
		})
	}
}

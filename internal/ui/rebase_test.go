package ui

import (
	"testing"

	"github.com/stretchr/testify/assert"
)

// TestRefreshDescriptionSaysWhatWillHappen covers the rebase description naming what becomes of the branch.
func TestRefreshDescriptionSaysWhatWillHappen(t *testing.T) {
	t.Parallel()

	for _, tt := range []struct {
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
			want:     []string{"no commits of its own", "moved to origin/main"},
			wantNone: []string{"replayed"},
		},
		{
			name: "a branch the base already holds loses those commits",
			plan: rebasePlan{
				baseRef: "origin/main",
				landed:  3,
			},
			want: []string{"already holds every commit", "3 commits will leave the branch"},
		},
		{
			name: "an ordinary rebase says what it replays",
			plan: rebasePlan{
				baseRef: "origin/main",
				replay:  2,
			},
			want:     []string{"2 commits of its own will be replayed onto origin/main"},
			wantNone: []string{"leave the branch"},
		},
		{
			name: "a partly landed branch says both halves",
			plan: rebasePlan{
				baseRef: "origin/main",
				landed:  2,
				replay:  1,
			},
			want: []string{"1 commit of its own will be replayed", "Another 2 the base already holds"},
		},
	} {
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

	for _, plan := range []rebasePlan{
		{baseRef: "origin/main"},
		{baseRef: "origin/main", landed: 3},
		{baseRef: "origin/main", replay: 2},
	} {
		description := rebaseDescription(plan)
		assert.Contains(t, description, "files will change under it")
		assert.Contains(t, description, "nothing untracked is removed")
		assert.Contains(t, description, "copy of the worktree is kept")
	}
}

// TestRefreshAffirmativeNamesWhatRuns covers the button naming what actually runs.
func TestRefreshAffirmativeNamesWhatRuns(t *testing.T) {
	t.Parallel()

	assert.Equal(t, "Rebase", rebaseAffirmative(rebasePlan{replay: 2}))
	assert.Equal(t, "Move", rebaseAffirmative(rebasePlan{landed: 3}))
	assert.Equal(t, "Move", rebaseAffirmative(rebasePlan{}))
}

// TestRefreshWarning covers what the rebase confirmation warns about.
func TestRefreshWarning(t *testing.T) {
	t.Parallel()

	for _, tt := range []struct {
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
	} {
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

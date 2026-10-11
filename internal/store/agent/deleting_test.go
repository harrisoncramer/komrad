package agent

import (
	"context"
	"testing"

	"komrad/lib/xptr"
	"komrad/lib/xslice"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// TestDeletingClaimHidesTheRow covers a claim taking a row and its reviewers out of the page
// listing while the directories still hold them.
func TestDeletingClaimHidesTheRow(t *testing.T) {
	byID := func(ctx context.Context, f *fixture, writerID string) error {
		_, err := f.MarkDeleting(ctx, []string{writerID})

		return err
	}
	archivedByID := func(ctx context.Context, f *fixture, writerID string) error {
		_, err := f.MarkArchivedDeletingByID(ctx, []string{writerID})

		return err
	}

	tests := []struct {
		name       string
		archive    bool
		failed     bool
		claim      func(ctx context.Context, f *fixture, writerID string) error
		release    func(ctx context.Context, f *fixture, writerID string) error
		wantHidden bool
	}{
		{
			name:       "an archived claim leaves a failed writer and its reviewer alone",
			archive:    true,
			failed:     true,
			claim:      archivedByID,
			wantHidden: false,
		},
		{
			name:       "a claim by id takes a failed writer and its reviewer",
			archive:    true,
			failed:     true,
			claim:      byID,
			wantHidden: true,
		},
		{
			name:       "an archived claim leaves a live writer alone",
			archive:    false,
			claim:      archivedByID,
			wantHidden: false,
		},
		{
			name:       "a claim by id takes a live writer and its reviewer",
			archive:    false,
			claim:      byID,
			wantHidden: true,
		},
		{
			name:       "an archived claim takes an archived writer and its reviewer",
			archive:    true,
			claim:      archivedByID,
			wantHidden: true,
		},
		{
			name:    "a release by id gives the rows back",
			archive: true,
			claim:   archivedByID,
			release: func(ctx context.Context, f *fixture, writerID string) error {
				return f.ClearDeleting(ctx, []string{writerID})
			},
			wantHidden: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			f := open(t)
			ctx := t.Context()

			require.NoError(t, f.Create(ctx, Agent{Name: "author", State: StateIdle}))
			writer, err := f.Get(ctx, "author")
			require.NoError(t, err)
			require.NotNil(t, writer)
			require.NoError(t, f.Create(ctx, Agent{
				Name:   "reviewer",
				TaskID: writer.TaskID,
				Role:   RoleReviewer,
				State:  StateIdle,
			}))
			reviewer, err := f.Get(ctx, "reviewer")
			require.NoError(t, err)
			require.NotNil(t, reviewer)
			if tt.archive {
				_, err := f.Archive(ctx, []string{writer.ID, reviewer.ID})
				require.NoError(t, err)
			}
			if tt.failed {
				writer.State = StateFailedToCleanup
				require.NoError(t, f.Update(ctx, *writer))
			}

			require.NoError(t, tt.claim(ctx, f, writer.ID))
			if tt.release != nil {
				require.NoError(t, tt.release(ctx, f, writer.ID))
			}

			dirs, err := f.ListDirs(ctx)
			require.NoError(t, err)
			require.Len(t, dirs, 2, "the directories listing holds a claimed row and says so")
			for _, dir := range dirs {
				assert.Equal(t, tt.wantHidden, dir.DeletingAt != nil, dir.Name)
			}
		})
	}
}

// TestReleaseLeavesAnUnclaimedRowUntouched pins a release naming a row with no claim on it
// writing nothing, so the stamp the optimistic locks compare on does not move.
func TestReleaseLeavesAnUnclaimedRowUntouched(t *testing.T) {
	f := open(t)
	ctx := t.Context()

	require.NoError(t, f.Create(ctx, Agent{Name: "author", State: StateIdle}))
	before, err := f.Get(ctx, "author")
	require.NoError(t, err)
	require.NotNil(t, before)

	require.NoError(t, f.ClearDeleting(ctx, []string{before.ID}))

	after, err := f.Get(ctx, "author")
	require.NoError(t, err)
	require.NotNil(t, after)
	assert.Equal(t, before.UpdatedAt, after.UpdatedAt)
}

// TestDeletingClaimReturnsWhatItTook pins a claim answering the rows it took under one stamp
// and nothing a claim before it already holds, and the roots read back after it.
func TestDeletingClaimReturnsWhatItTook(t *testing.T) {
	f := open(t)
	ctx := t.Context()

	require.NoError(t, f.Create(ctx, Agent{Name: "author", State: StateIdle}))
	writer, err := f.Get(ctx, "author")
	require.NoError(t, err)
	require.NoError(t, f.Create(ctx, Agent{
		Name:   "reviewer",
		TaskID: writer.TaskID,
		Role:   RoleReviewer,
		State:  StateIdle,
	}))
	reviewer, err := f.Get(ctx, "reviewer")
	require.NoError(t, err)
	require.NoError(t, f.Create(ctx, Agent{Name: "other", State: StateIdle}))
	other, err := f.Get(ctx, "other")
	require.NoError(t, err)

	claims, err := f.MarkDeleting(ctx, []string{writer.ID})
	require.NoError(t, err)
	require.Len(t, claims, 2)
	assert.ElementsMatch(t, []string{writer.ID, reviewer.ID}, claimIDs(claims))
	assert.True(t, claims[0].DeletingAt.Equal(claims[1].DeletingAt), "one claim stamps every row it takes alike")
	assert.Equal(t, []string{writer.ID}, claimIDs(ClaimRoots(claims)))

	again, err := f.MarkDeleting(ctx, []string{writer.ID, other.ID})
	require.NoError(t, err)
	assert.Equal(t, []string{other.ID}, claimIDs(again), "a second claim takes only what the first left")

	roots, err := f.ListDeletingRoots(ctx)
	require.NoError(t, err)
	assert.ElementsMatch(t, []string{writer.ID, other.ID}, claimIDs(roots), "a reviewer its task's writer's claim took is not a root")
}

// TestAReviewerClaimedBeforeItsWriterIsARoot pins a reviewer an earlier delete took keeping a delete
// of its own once its task's writer is claimed after it.
func TestAReviewerClaimedBeforeItsWriterIsARoot(t *testing.T) {
	f := open(t)
	ctx := t.Context()

	require.NoError(t, f.Create(ctx, Agent{Name: "author", State: StateIdle}))
	writer, err := f.Get(ctx, "author")
	require.NoError(t, err)
	require.NoError(t, f.Create(ctx, Agent{
		Name:   "reviewer",
		TaskID: writer.TaskID,
		Role:   RoleReviewer,
		State:  StateIdle,
	}))
	reviewer, err := f.Get(ctx, "reviewer")
	require.NoError(t, err)

	first, err := f.MarkDeleting(ctx, []string{reviewer.ID})
	require.NoError(t, err)
	require.Len(t, first, 1)
	second, err := f.MarkDeleting(ctx, []string{writer.ID})
	require.NoError(t, err)
	require.Len(t, second, 1)

	roots, err := f.ListDeletingRoots(ctx)
	require.NoError(t, err)
	assert.ElementsMatch(t, []Claim{first[0], second[0]}, roots)
}

// TestClaimRoots pins which claimed agents start a delete of their own.
func TestClaimRoots(t *testing.T) {
	task := "t"
	tests := []struct {
		name   string
		claims []Claim
		want   []string
	}{
		{
			name:   "nothing claimed",
			claims: nil,
			want:   []string{},
		},
		{
			name:   "a reviewer claimed with its task's writer goes with it",
			claims: []Claim{{ID: "w", TaskID: &task, Role: RoleWriter}, {ID: "r", TaskID: &task, Role: RoleReviewer}},
			want:   []string{"w"},
		},
		{
			name:   "a reviewer claimed without its task's writer is a root",
			claims: []Claim{{ID: "r", TaskID: &task, Role: RoleReviewer}},
			want:   []string{"r"},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			assert.Equal(t, tt.want, claimIDs(ClaimRoots(tt.claims)))
		})
	}
}

// TestListProvisionedIDsByTask covers resolving a task-named worktree to the writer it was made
// for, never a reviewer sharing the task or a writer that kept the directory it was given, and
// none while the writer is still live.
func TestListProvisionedIDsByTask(t *testing.T) {
	f := open(t)
	ctx := t.Context()

	require.NoError(t, f.Create(ctx, Agent{Name: "author", State: StateIdle}))
	author, err := f.Get(ctx, "author")
	require.NoError(t, err)
	require.NotNil(t, author)
	require.NoError(t, f.Create(ctx, Agent{
		Name:   "reviewer",
		TaskID: author.TaskID,
		Role:   RoleReviewer,
		State:  StateIdle,
	}))
	reviewer, err := f.Get(ctx, "reviewer")
	require.NoError(t, err)
	require.NoError(t, f.Create(ctx, Agent{
		Name:             "adopter",
		ProvisionEnabled: xptr.Ptr(false),
		State:            StateIdle,
	}))
	adopter, err := f.Get(ctx, "adopter")
	require.NoError(t, err)
	require.NoError(t, f.Create(ctx, Agent{Name: "live", State: StateIdle}))
	live, err := f.Get(ctx, "live")
	require.NoError(t, err)
	_, err = f.Archive(ctx, []string{author.ID, reviewer.ID, adopter.ID})
	require.NoError(t, err)

	ids, err := f.ListProvisionedIDsByTask(ctx, []string{*author.TaskID, *adopter.TaskID, *live.TaskID, "not-a-task"})
	require.NoError(t, err)
	assert.Equal(t, []string{author.ID}, ids)
}

func claimIDs(claims []Claim) []string {
	return xslice.Map(claims, func(claim Claim) string { return claim.ID })
}

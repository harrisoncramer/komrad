package agent

import (
	"context"
	"testing"

	"komrad/lib/xslice"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// TestDeletingClaimHidesTheRow covers a claim taking a row and its children out of the page
// listing while the directories still hold them.
func TestDeletingClaimHidesTheRow(t *testing.T) {
	byID := func(ctx context.Context, f *fixture, parentID string) error {
		_, err := f.MarkDeleting(ctx, []string{parentID})

		return err
	}
	archivedByID := func(ctx context.Context, f *fixture, parentID string) error {
		_, err := f.MarkArchivedDeletingByID(ctx, []string{parentID})

		return err
	}

	tests := []struct {
		name       string
		archive    bool
		failed     bool
		claim      func(ctx context.Context, f *fixture, parentID string) error
		release    func(ctx context.Context, f *fixture, parentID string) error
		wantHidden bool
	}{
		{
			name:       "an archived claim leaves a failed parent and its child alone",
			archive:    true,
			failed:     true,
			claim:      archivedByID,
			wantHidden: false,
		},
		{
			name:       "a claim by id takes a failed parent and its child",
			archive:    true,
			failed:     true,
			claim:      byID,
			wantHidden: true,
		},
		{
			name:       "an archived claim leaves a live parent alone",
			archive:    false,
			claim:      archivedByID,
			wantHidden: false,
		},
		{
			name:       "a claim by id takes a live parent and its child",
			archive:    false,
			claim:      byID,
			wantHidden: true,
		},
		{
			name:       "an archived claim takes an archived parent and its child",
			archive:    true,
			claim:      archivedByID,
			wantHidden: true,
		},
		{
			name:    "a release by id gives the rows back",
			archive: true,
			claim:   archivedByID,
			release: func(ctx context.Context, f *fixture, parentID string) error {
				return f.ClearDeleting(ctx, []string{parentID})
			},
			wantHidden: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			f := open(t)
			ctx := t.Context()

			require.NoError(t, f.Create(ctx, Agent{Name: "author", State: StateIdle}))
			parent, err := f.Get(ctx, "author")
			require.NoError(t, err)
			require.NotNil(t, parent)
			require.NoError(t, f.Create(ctx, Agent{
				Name:          "reviewer",
				ParentAgentID: &parent.ID,
				State:         StateIdle,
			}))
			child, err := f.Get(ctx, "reviewer")
			require.NoError(t, err)
			require.NotNil(t, child)
			if tt.archive {
				_, err := f.Archive(ctx, []string{parent.ID, child.ID})
				require.NoError(t, err)
			}
			if tt.failed {
				parent.State = StateFailedToCleanup
				require.NoError(t, f.Update(ctx, *parent))
			}

			require.NoError(t, tt.claim(ctx, f, parent.ID))
			if tt.release != nil {
				require.NoError(t, tt.release(ctx, f, parent.ID))
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
	parent, err := f.Get(ctx, "author")
	require.NoError(t, err)
	require.NoError(t, f.Create(ctx, Agent{
		Name:          "reviewer",
		ParentAgentID: &parent.ID,
		State:         StateIdle,
	}))
	child, err := f.Get(ctx, "reviewer")
	require.NoError(t, err)
	require.NoError(t, f.Create(ctx, Agent{Name: "other", State: StateIdle}))
	other, err := f.Get(ctx, "other")
	require.NoError(t, err)

	claims, err := f.MarkDeleting(ctx, []string{parent.ID})
	require.NoError(t, err)
	require.Len(t, claims, 2)
	assert.ElementsMatch(t, []string{parent.ID, child.ID}, claimIDs(claims))
	assert.True(t, claims[0].DeletingAt.Equal(claims[1].DeletingAt), "one claim stamps every row it takes alike")
	assert.Equal(t, []string{parent.ID}, claimIDs(ClaimRoots(claims)))

	again, err := f.MarkDeleting(ctx, []string{parent.ID, other.ID})
	require.NoError(t, err)
	assert.Equal(t, []string{other.ID}, claimIDs(again), "a second claim takes only what the first left")

	roots, err := f.ListDeletingRoots(ctx)
	require.NoError(t, err)
	assert.ElementsMatch(t, []string{parent.ID, other.ID}, claimIDs(roots), "a child its parent's claim took is not a root")
}

// TestAChildClaimedBeforeItsParentIsARoot pins a child an earlier delete took keeping a delete
// of its own once its parent is claimed after it.
func TestAChildClaimedBeforeItsParentIsARoot(t *testing.T) {
	f := open(t)
	ctx := t.Context()

	require.NoError(t, f.Create(ctx, Agent{Name: "author", State: StateIdle}))
	parent, err := f.Get(ctx, "author")
	require.NoError(t, err)
	require.NoError(t, f.Create(ctx, Agent{
		Name:          "reviewer",
		ParentAgentID: &parent.ID,
		State:         StateIdle,
	}))
	child, err := f.Get(ctx, "reviewer")
	require.NoError(t, err)

	first, err := f.MarkDeleting(ctx, []string{child.ID})
	require.NoError(t, err)
	require.Len(t, first, 1)
	second, err := f.MarkDeleting(ctx, []string{parent.ID})
	require.NoError(t, err)
	require.Len(t, second, 1)

	roots, err := f.ListDeletingRoots(ctx)
	require.NoError(t, err)
	assert.ElementsMatch(t, []Claim{first[0], second[0]}, roots)
}

// TestClaimRoots pins which claimed agents start a delete of their own.
func TestClaimRoots(t *testing.T) {
	parent, orphanParent := "p", "gone"
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
			name:   "a child claimed with its parent goes with it",
			claims: []Claim{{ID: parent}, {ID: "c", ParentAgentID: &parent}},
			want:   []string{parent},
		},
		{
			name:   "a child claimed without its parent is a root",
			claims: []Claim{{ID: "c", ParentAgentID: &orphanParent}},
			want:   []string{"c"},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			assert.Equal(t, tt.want, claimIDs(ClaimRoots(tt.claims)))
		})
	}
}

func claimIDs(claims []Claim) []string {
	return xslice.Map(claims, func(claim Claim) string { return claim.ID })
}

package agent

import (
	"context"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// TestDeletingClaimHidesTheRow covers a claim taking a row and its children out of the page
// listing while the sweep's own page and the directories still hold them.
func TestDeletingClaimHidesTheRow(t *testing.T) {
	tests := []struct {
		name       string
		archive    bool
		failed     bool
		claim      func(ctx context.Context, f *fixture, parentID string) error
		release    func(ctx context.Context, f *fixture, parentID string) error
		wantHidden bool
	}{
		{
			name:    "an archived claim leaves a failed parent and its child alone",
			archive: true,
			failed:  true,
			claim: func(ctx context.Context, f *fixture, parentID string) error {
				return f.MarkArchivedDeletingByID(ctx, []string{parentID})
			},
			wantHidden: false,
		},
		{
			name:    "the archive claim leaves a failed parent and its child alone",
			archive: true,
			failed:  true,
			claim: func(ctx context.Context, f *fixture, _ string) error {
				return f.MarkArchivedDeleting(ctx)
			},
			wantHidden: false,
		},
		{
			name:    "a claim by id takes a failed parent and its child",
			archive: true,
			failed:  true,
			claim: func(ctx context.Context, f *fixture, parentID string) error {
				return f.MarkDeleting(ctx, []string{parentID})
			},
			wantHidden: true,
		},
		{
			name:    "an archived claim leaves a live parent alone",
			archive: false,
			claim: func(ctx context.Context, f *fixture, parentID string) error {
				return f.MarkArchivedDeletingByID(ctx, []string{parentID})
			},
			wantHidden: false,
		},
		{
			name:    "the archive claim leaves a live parent alone",
			archive: false,
			claim: func(ctx context.Context, f *fixture, _ string) error {
				return f.MarkArchivedDeleting(ctx)
			},
			wantHidden: false,
		},
		{
			name:    "a claim by id takes a live parent and its child",
			archive: false,
			claim: func(ctx context.Context, f *fixture, parentID string) error {
				return f.MarkDeleting(ctx, []string{parentID})
			},
			wantHidden: true,
		},
		{
			name:    "an archived claim takes an archived parent and its child",
			archive: true,
			claim: func(ctx context.Context, f *fixture, parentID string) error {
				return f.MarkArchivedDeletingByID(ctx, []string{parentID})
			},
			wantHidden: true,
		},
		{
			name:    "the archive claim takes an archived parent and its child",
			archive: true,
			claim: func(ctx context.Context, f *fixture, _ string) error {
				return f.MarkArchivedDeleting(ctx)
			},
			wantHidden: true,
		},
		{
			name:    "a release by id gives the rows back",
			archive: true,
			claim: func(ctx context.Context, f *fixture, _ string) error {
				return f.MarkArchivedDeleting(ctx)
			},
			release: func(ctx context.Context, f *fixture, parentID string) error {
				return f.ClearDeleting(ctx, []string{parentID})
			},
			wantHidden: false,
		},
		{
			name:    "a release of everything gives the rows back",
			archive: true,
			claim: func(ctx context.Context, f *fixture, _ string) error {
				return f.MarkArchivedDeleting(ctx)
			},
			release: func(ctx context.Context, f *fixture, _ string) error {
				return f.ClearAllDeleting(ctx)
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
			require.NoError(t, f.Create(ctx, Agent{Name: "reviewer", ParentAgentID: &parent.ID, State: StateIdle}))
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

			if tt.archive {
				sweep, err := f.ListArchiveSweepPage(ctx, 10, nil)
				require.NoError(t, err)
				assert.Len(t, sweep, 2, "the sweep reads the claimed rows it is there to take")
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

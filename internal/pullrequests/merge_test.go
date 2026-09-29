package pullrequests

import (
	"context"
	"testing"

	"komrad/internal/forge"
	"komrad/internal/store"
	forgestore "komrad/internal/store/forge"
	"komrad/internal/storetest"
	"komrad/internal/waketest"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// mergingForge records each merge and answers mergeErr.
type mergingForge struct {
	forge.Forge
	mergeErr error
	merged   []string
}

func (f *mergingForge) Merge(_ context.Context, address string) error {
	f.merged = append(f.merged, address)

	return f.mergeErr
}

// TestMergePullRequest covers which connection a merge goes through and the pull request sync
// being woken only once the forge has merged.
func TestMergePullRequest(t *testing.T) {
	const address = "https://github.com/harrisoncramer/komrad/pull/1"

	tests := []struct {
		name       string
		address    string
		mergeErr   error
		wantErr    error
		wantMerged []string
		wantWakes  int
	}{
		{
			name:    "an address with no host",
			address: "harrisoncramer/komrad#1",
			wantErr: forge.ErrNotPullRequest,
		},
		{
			name:    "a host nobody connected",
			address: "https://gitlab.com/harrisoncramer/komrad/-/merge_requests/1",
			wantErr: store.ErrNotFound,
		},
		{
			name:       "a pull request the forge will not merge",
			address:    address,
			mergeErr:   forge.ErrNotMergeable,
			wantErr:    forge.ErrNotMergeable,
			wantMerged: []string{address},
		},
		{
			name:       "a pull request the forge merges",
			address:    address,
			wantMerged: []string{address},
			wantWakes:  1,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			stores := storetest.Open(t)
			connections := forgestore.NewForgeStore(stores.Pool.GetDB())
			_, err := connections.Create(t.Context(), forgestore.CreateParams{
				Kind:       forge.KindGitHub,
				Host:       "github.com",
				Method:     forge.MethodToken,
				Login:      "harrisoncramer",
				VerifiedAt: syncNow,
			})
			require.NoError(t, err)
			merging := &mergingForge{mergeErr: tt.mergeErr}
			sync := &waketest.Counter{}
			merger := NewMerger(NewMergerParams{
				Forges: forge.NewService(forge.NewServiceParams{
					Connections: connections,
					Credentials: forge.NewFileStore(t.TempDir()),
					Builders: map[string]forge.Builder{
						forge.KindGitHub: func(string, forge.Authenticator) forge.Forge {
							return merging
						},
					},
				}),
				Sync: sync,
			})

			err = merger.MergePullRequest(t.Context(), tt.address)

			if tt.wantErr != nil {
				require.ErrorIs(t, err, tt.wantErr)
			} else {
				require.NoError(t, err)
			}
			assert.Equal(t, tt.wantMerged, merging.merged)
			assert.Equal(t, tt.wantWakes, sync.Wakes)
		})
	}
}

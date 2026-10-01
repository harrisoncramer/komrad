package pullrequests

import (
	"context"
	"testing"
	"time"

	"komrad/internal/forge"
	"komrad/internal/store"
	forgestore "komrad/internal/store/forge"
	"komrad/internal/storetest"
	"komrad/internal/waketest"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// writingForge records each merge, draft change and auto-merge change and answers err.
type writingForge struct {
	forge.Forge
	err        error
	merged     []string
	drafted    []drafted
	autoMerged []forge.SetAutoMergeParams
}

type drafted struct {
	address string
	draft   bool
}

func (f *writingForge) Merge(_ context.Context, address string) error {
	f.merged = append(f.merged, address)

	return f.err
}

func (f *writingForge) SetDraft(_ context.Context, address string, draft bool) error {
	f.drafted = append(f.drafted, drafted{address: address, draft: draft})

	return f.err
}

func (f *writingForge) SetAutoMerge(_ context.Context, params forge.SetAutoMergeParams) error {
	f.autoMerged = append(f.autoMerged, params)

	return f.err
}

// newTestWriter is a writer over a github.com connection whose forge is the one given.
func newTestWriter(t *testing.T, writing *writingForge, sync *waketest.Counter) *Writer {
	t.Helper()

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

	return NewWriter(NewWriterParams{
		Forges: forge.NewService(forge.NewServiceParams{
			Connections: connections,
			Credentials: forge.NewFileStore(t.TempDir()),
			Builders: map[string]forge.Builder{
				forge.KindGitHub: func(string, forge.Authenticator) forge.Forge {
					return writing
				},
			},
		}),
		Sync: sync,
	})
}

var settleWakes = []time.Duration{10 * time.Second, 20 * time.Second, 30 * time.Second, 40 * time.Second, 50 * time.Second}

// TestMergePullRequest covers which connection a merge goes through and the pull request sync
// being woken, now and every ten seconds for a minute, only once the forge has merged.
func TestMergePullRequest(t *testing.T) {
	const address = "https://github.com/harrisoncramer/komrad/pull/1"

	tests := []struct {
		name        string
		address     string
		mergeErr    error
		wantErr     error
		wantMerged  []string
		wantWakes   int
		wantDelayed []time.Duration
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
			name:        "a pull request the forge merges",
			address:     address,
			wantMerged:  []string{address},
			wantWakes:   1,
			wantDelayed: settleWakes,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			writing := &writingForge{err: tt.mergeErr}
			sync := &waketest.Counter{}
			writer := newTestWriter(t, writing, sync)

			err := writer.MergePullRequest(t.Context(), tt.address)

			if tt.wantErr != nil {
				require.ErrorIs(t, err, tt.wantErr)
			} else {
				require.NoError(t, err)
			}
			assert.Equal(t, tt.wantMerged, writing.merged)
			assert.Equal(t, tt.wantWakes, sync.Wakes)
			assert.Equal(t, tt.wantDelayed, sync.Delayed)
		})
	}
}

// TestSetPullRequestDraft covers the pull request sync being woken, now and every ten seconds
// for a minute, only once the forge has changed the draft.
func TestSetPullRequestDraft(t *testing.T) {
	const address = "https://github.com/harrisoncramer/komrad/pull/1"

	tests := []struct {
		name        string
		address     string
		draft       bool
		draftErr    error
		wantErr     error
		wantDrafted []drafted
		wantWakes   int
		wantDelayed []time.Duration
	}{
		{
			name:    "an address with no host",
			address: "harrisoncramer/komrad#1",
			wantErr: forge.ErrNotPullRequest,
		},
		{
			name:        "a pull request the forge will not change",
			address:     address,
			draft:       true,
			draftErr:    forge.ErrNotDraftable,
			wantErr:     forge.ErrNotDraftable,
			wantDrafted: []drafted{{address: address, draft: true}},
		},
		{
			name:        "a pull request the forge marks a draft",
			address:     address,
			draft:       true,
			wantDrafted: []drafted{{address: address, draft: true}},
			wantWakes:   1,
			wantDelayed: settleWakes,
		},
		{
			name:        "a draft the forge marks ready",
			address:     address,
			wantDrafted: []drafted{{address: address, draft: false}},
			wantWakes:   1,
			wantDelayed: settleWakes,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			writing := &writingForge{err: tt.draftErr}
			sync := &waketest.Counter{}
			writer := newTestWriter(t, writing, sync)

			err := writer.SetPullRequestDraft(t.Context(), tt.address, tt.draft)

			if tt.wantErr != nil {
				require.ErrorIs(t, err, tt.wantErr)
			} else {
				require.NoError(t, err)
			}
			assert.Equal(t, tt.wantDrafted, writing.drafted)
			assert.Equal(t, tt.wantWakes, sync.Wakes)
			assert.Equal(t, tt.wantDelayed, sync.Delayed)
		})
	}
}

// TestSetPullRequestAutoMerge covers the head the person saw reaching the forge and the pull
// request sync being woken, now and every ten seconds for a minute, only once the forge has
// changed auto-merge.
func TestSetPullRequestAutoMerge(t *testing.T) {
	const address = "https://github.com/harrisoncramer/komrad/pull/1"

	tests := []struct {
		name           string
		params         forge.SetAutoMergeParams
		autoMergeErr   error
		wantErr        error
		wantAutoMerged []forge.SetAutoMergeParams
		wantWakes      int
		wantDelayed    []time.Duration
	}{
		{
			name:    "an address with no host",
			params:  forge.SetAutoMergeParams{URL: "harrisoncramer/komrad#1", On: true},
			wantErr: forge.ErrNotPullRequest,
		},
		{
			name: "a pull request the forge will not queue",
			params: forge.SetAutoMergeParams{
				URL:     address,
				HeadSHA: "abc",
				On:      true,
			},
			autoMergeErr: forge.ErrNoAutoMerge,
			wantErr:      forge.ErrNoAutoMerge,
			wantAutoMerged: []forge.SetAutoMergeParams{{
				URL:     address,
				HeadSHA: "abc",
				On:      true,
			}},
		},
		{
			name: "a pull request the forge queues",
			params: forge.SetAutoMergeParams{
				URL:     address,
				HeadSHA: "abc",
				On:      true,
			},
			wantAutoMerged: []forge.SetAutoMergeParams{{
				URL:     address,
				HeadSHA: "abc",
				On:      true,
			}},
			wantWakes:   1,
			wantDelayed: settleWakes,
		},
		{
			name:           "a pull request the forge stops waiting on",
			params:         forge.SetAutoMergeParams{URL: address},
			wantAutoMerged: []forge.SetAutoMergeParams{{URL: address}},
			wantWakes:      1,
			wantDelayed:    settleWakes,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			writing := &writingForge{err: tt.autoMergeErr}
			sync := &waketest.Counter{}
			writer := newTestWriter(t, writing, sync)

			err := writer.SetPullRequestAutoMerge(t.Context(), tt.params)

			if tt.wantErr != nil {
				require.ErrorIs(t, err, tt.wantErr)
			} else {
				require.NoError(t, err)
			}
			assert.Equal(t, tt.wantAutoMerged, writing.autoMerged)
			assert.Equal(t, tt.wantWakes, sync.Wakes)
			assert.Equal(t, tt.wantDelayed, sync.Delayed)
		})
	}
}

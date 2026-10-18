package pullrequests

import (
	"context"
	"testing"

	"komrad/internal/forge"
	"komrad/internal/store"
	agentstore "komrad/internal/store/agent"
	forgestore "komrad/internal/store/forge"
	taskstore "komrad/internal/store/task"
	workflowstore "komrad/internal/store/workflow"
	"komrad/internal/storetest"
	"komrad/internal/waketest"
	"komrad/lib/xptr"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// writingForge records each merge, draft change, auto-merge change and re-review and answers err.
type writingForge struct {
	forge.Forge
	err        error
	merged     []forge.MergeParams
	drafted    []drafted
	autoMerged []forge.SetAutoMergeParams
	rereviewed []string
}

type drafted struct {
	address string
	draft   bool
}

func (f *writingForge) Merge(_ context.Context, params forge.MergeParams) error {
	f.merged = append(f.merged, params)

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

func (f *writingForge) RequestRereview(_ context.Context, address string) error {
	f.rereviewed = append(f.rereviewed, address)

	return f.err
}

// newTestWriter is a writer over the stores and a github.com connection whose forge is the one given.
func newTestWriter(t *testing.T, stores *storetest.Stores, writing *writingForge, sync *waketest.Counter) *Writer {
	t.Helper()

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
		Sync:         sync,
		PullRequests: stores.PullRequests,
	})
}

// TestMergePullRequest covers which connection a merge goes through and the pull request sync
// heating the host only once the forge has merged.
func TestMergePullRequest(t *testing.T) {
	const address = "https://github.com/harrisoncramer/komrad/pull/1"

	tests := []struct {
		name       string
		address    string
		child      childTask
		mergeErr   error
		wantErr    error
		wantMerged []forge.MergeParams
		wantHeated []string
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
			name:    "a pull request from a task standing on another",
			address: address,
			child:   childTask{stands: true, parentState: "OPEN"},
			wantErr: errMergeStanding,
		},
		{
			name:    "a pull request onto a task whose own pull request merged",
			address: address,
			child:   childTask{parentState: "MERGED"},
			wantErr: errMergeOntoFinished,
		},
		{
			name:       "a pull request the forge will not merge",
			address:    address,
			mergeErr:   forge.ErrNotMergeable,
			wantErr:    forge.ErrNotMergeable,
			wantMerged: []forge.MergeParams{{URL: address, Method: forge.MergeMethodSquash}},
		},
		{
			name:       "a pull request the forge merges",
			address:    address,
			wantMerged: []forge.MergeParams{{URL: address, Method: forge.MergeMethodSquash}},
			wantHeated: []string{"github.com"},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			writing := &writingForge{err: tt.mergeErr}
			sync := &waketest.Counter{}
			stores := storetest.Open(t)
			seedChildTask(t, stores, tt.child)
			writer := newTestWriter(t, stores, writing, sync)

			err := writer.MergePullRequest(t.Context(), forge.MergeParams{URL: tt.address, Method: forge.MergeMethodSquash})

			if tt.wantErr != nil {
				require.ErrorIs(t, err, tt.wantErr)
			} else {
				require.NoError(t, err)
			}
			assert.Equal(t, tt.wantMerged, writing.merged)
			assert.Equal(t, tt.wantHeated, sync.Heated)
		})
	}
}

// TestSetPullRequestDraft covers the pull request sync heating the host only once the forge has
// changed the draft.
func TestSetPullRequestDraft(t *testing.T) {
	const address = "https://github.com/harrisoncramer/komrad/pull/1"

	tests := []struct {
		name        string
		address     string
		draft       bool
		draftErr    error
		wantErr     error
		wantDrafted []drafted
		wantHeated  []string
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
			wantHeated:  []string{"github.com"},
		},
		{
			name:        "a draft the forge marks ready",
			address:     address,
			wantDrafted: []drafted{{address: address, draft: false}},
			wantHeated:  []string{"github.com"},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			writing := &writingForge{err: tt.draftErr}
			sync := &waketest.Counter{}
			writer := newTestWriter(t, storetest.Open(t), writing, sync)

			err := writer.SetPullRequestDraft(t.Context(), tt.address, tt.draft)

			if tt.wantErr != nil {
				require.ErrorIs(t, err, tt.wantErr)
			} else {
				require.NoError(t, err)
			}
			assert.Equal(t, tt.wantDrafted, writing.drafted)
			assert.Equal(t, tt.wantHeated, sync.Heated)
		})
	}
}

// TestRequestPullRequestRereview covers the pull request sync heating the host only once the
// forge has asked for another review.
func TestRequestPullRequestRereview(t *testing.T) {
	const address = "https://github.com/harrisoncramer/komrad/pull/1"

	tests := []struct {
		name           string
		address        string
		rereviewErr    error
		wantErr        error
		wantRereviewed []string
		wantHeated     []string
	}{
		{
			name:    "an address with no host",
			address: "harrisoncramer/komrad#1",
			wantErr: forge.ErrNotPullRequest,
		},
		{
			name:           "a pull request nobody asked changes of",
			address:        address,
			rereviewErr:    forge.ErrNoRereview,
			wantErr:        forge.ErrNoRereview,
			wantRereviewed: []string{address},
		},
		{
			name:           "a pull request the forge asks for another review of",
			address:        address,
			wantRereviewed: []string{address},
			wantHeated:     []string{"github.com"},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			writing := &writingForge{err: tt.rereviewErr}
			sync := &waketest.Counter{}
			writer := newTestWriter(t, storetest.Open(t), writing, sync)

			err := writer.RequestPullRequestRereview(t.Context(), tt.address)

			if tt.wantErr != nil {
				require.ErrorIs(t, err, tt.wantErr)
			} else {
				require.NoError(t, err)
			}
			assert.Equal(t, tt.wantRereviewed, writing.rereviewed)
			assert.Equal(t, tt.wantHeated, sync.Heated)
		})
	}
}

// TestSetPullRequestAutoMerge covers the head the person saw reaching the forge and the pull
// request sync heating the host only once the forge has changed auto-merge.
func TestSetPullRequestAutoMerge(t *testing.T) {
	const address = "https://github.com/harrisoncramer/komrad/pull/1"

	tests := []struct {
		name           string
		params         forge.SetAutoMergeParams
		child          childTask
		autoMergeErr   error
		wantErr        error
		wantAutoMerged []forge.SetAutoMergeParams
		wantHeated     []string
	}{
		{
			name:    "an address with no host",
			params:  forge.SetAutoMergeParams{URL: "harrisoncramer/komrad#1", On: true},
			wantErr: forge.ErrNotPullRequest,
		},
		{
			name:    "a pull request from a task standing on another",
			params:  forge.SetAutoMergeParams{URL: address, HeadSHA: "abc", On: true},
			child:   childTask{stands: true, parentState: "OPEN"},
			wantErr: errMergeStanding,
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
			wantHeated: []string{"github.com"},
		},
		{
			name:           "a pull request the forge stops waiting on",
			params:         forge.SetAutoMergeParams{URL: address},
			wantAutoMerged: []forge.SetAutoMergeParams{{URL: address}},
			wantHeated:     []string{"github.com"},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			writing := &writingForge{err: tt.autoMergeErr}
			sync := &waketest.Counter{}
			stores := storetest.Open(t)
			seedChildTask(t, stores, tt.child)
			writer := newTestWriter(t, stores, writing, sync)

			err := writer.SetPullRequestAutoMerge(t.Context(), tt.params)

			if tt.wantErr != nil {
				require.ErrorIs(t, err, tt.wantErr)
			} else {
				require.NoError(t, err)
			}
			assert.Equal(t, tt.wantAutoMerged, writing.autoMerged)
			assert.Equal(t, tt.wantHeated, sync.Heated)
		})
	}
}

// childTask is a child task's pull request at the test address onto its parent's branch, laid only once parentState names the state of the parent's own pull request.
type childTask struct {
	stands      bool
	parentState string
}

// seedChildTask records the parent and child tasks in the test repository, and their pull requests.
func seedChildTask(t *testing.T, stores *storetest.Stores, laid childTask) {
	t.Helper()

	if laid.parentState == "" {
		return
	}

	workflow := stores.SeedWorkflow(t, workflowstore.Config{Name: "parent-and-child"})
	parent := seedBranchTask(t, stores, workflow.ID, "task/parent")
	child := seedBranchTask(t, stores, workflow.ID, "task/child")
	if laid.stands {
		require.NoError(t, stores.Tasks.SetStand(t.Context(), child, &taskstore.Stand{ParentID: parent, Tip: "abc"}))
	}

	stores.SeedPullRequest(t, storetest.SeedPullRequestParams{
		RepositoryURL: taskRepository,
		Number:        2,
		HeadBranch:    "task/parent",
		HeadSHA:       "abc",
		BaseBranch:    "main",
		State:         laid.parentState,
		OpenedAt:      syncNow,
	})
	stores.SeedPullRequest(t, storetest.SeedPullRequestParams{
		RepositoryURL: taskRepository,
		Number:        1,
		HeadBranch:    "task/child",
		HeadSHA:       "abc",
		BaseBranch:    "task/parent",
		OpenedAt:      syncNow,
	})
}

// seedBranchTask records a task whose checkout is on a branch of the test repository.
func seedBranchTask(t *testing.T, stores *storetest.Stores, workflowID, branch string) string {
	t.Helper()

	agent := stores.SeedAgent(t, agentstore.Agent{Name: branch, WorkflowID: workflowID})
	stores.SeedTaskHead(t, agent.ID, taskstore.SetHeadParams{RepositoryURL: xptr.Ptr(taskRepository), Branch: xptr.Ptr(branch)})

	return *agent.TaskID
}

const taskRepository = "https://github.com/harrisoncramer/komrad"

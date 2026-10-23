package pullrequests

import (
	"context"
	"errors"
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
	forgev1 "komrad/proto/gen/forge/v1"

	"github.com/google/uuid"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

const taskRepository = "https://github.com/harrisoncramer/komrad"

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

// recordingCascade records each address whose children it was asked to move and answers err.
type recordingCascade struct {
	err    error
	merged []string
}

func (c *recordingCascade) CascadeMerged(_ context.Context, address string) error {
	c.merged = append(c.merged, address)

	return c.err
}

// newTestWriter is a writer over the stores and a github.com connection whose forge is the one given.
func newTestWriter(t *testing.T, stores *storetest.Stores, writing *writingForge, sync *waketest.Counter, cascade *recordingCascade) *Writer {
	t.Helper()

	connections := forgestore.NewForgeStore(stores.Pool.GetDB())
	_, err := connections.Create(t.Context(), forgestore.CreateParams{
		Kind:       forgev1.Kind_KIND_GITHUB,
		Host:       "github.com",
		Method:     forgev1.CredentialMethod_CREDENTIAL_METHOD_TOKEN,
		Login:      "harrisoncramer",
		VerifiedAt: syncNow,
	})
	require.NoError(t, err)

	return NewWriter(NewWriterParams{
		Forges: forge.NewService(forge.NewServiceParams{
			Connections: connections,
			Credentials: forge.NewFileStore(t.TempDir()),
			Builders: map[forgev1.Kind]forge.Builder{
				forgev1.Kind_KIND_GITHUB: func(string, forge.Authenticator) forge.Forge {
					return writing
				},
			},
		}),
		Sync:         sync,
		PullRequests: stores.PullRequests,
		Cascade:      cascade,
	})
}

// TestMergePullRequest covers which connection a merge goes through and the pull request sync
// reading the host often and moving the children only once the forge has merged.
func TestMergePullRequest(t *testing.T) {
	const address = "https://github.com/harrisoncramer/komrad/pull/1"

	tests := []struct {
		name          string
		address       string
		child         childTask
		mergeErr      error
		wantErr       error
		cascadeErr    error
		wantMerged    []forge.MergeParams
		wantReadOften []string
		wantCascaded  []string
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
			name:    "a pull request from a child",
			address: address,
			child:   childTask{hasParent: true, parentState: "OPEN"},
			wantErr: errMergeHasParent,
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
			wantMerged: []forge.MergeParams{{URL: address, Method: forgev1.MergeMethod_MERGE_METHOD_SQUASH}},
		},
		{
			name:          "a pull request the forge merges",
			address:       address,
			wantMerged:    []forge.MergeParams{{URL: address, Method: forgev1.MergeMethod_MERGE_METHOD_SQUASH}},
			wantReadOften: []string{"github.com"},
			wantCascaded:  []string{address},
		},
		{
			name:          "a merged pull request whose children komrad could not move",
			address:       address,
			cascadeErr:    errors.New("rebase failed"),
			wantMerged:    []forge.MergeParams{{URL: address, Method: forgev1.MergeMethod_MERGE_METHOD_SQUASH}},
			wantReadOften: []string{"github.com"},
			wantCascaded:  []string{address},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			writing := &writingForge{err: tt.mergeErr}
			sync := &waketest.Counter{}
			stores := storetest.Open(t)
			seedChildTask(t, stores, tt.child)
			cascade := &recordingCascade{err: tt.cascadeErr}
			writer := newTestWriter(t, stores, writing, sync, cascade)

			err := writer.MergePullRequest(t.Context(), forge.MergeParams{URL: tt.address, Method: forgev1.MergeMethod_MERGE_METHOD_SQUASH})

			if tt.wantErr != nil {
				require.ErrorIs(t, err, tt.wantErr)
			} else {
				require.NoError(t, err)
			}
			assert.Equal(t, tt.wantMerged, writing.merged)
			assert.Equal(t, tt.wantReadOften, sync.HostsReadOften)
			assert.Equal(t, tt.wantCascaded, cascade.merged)
		})
	}
}

// TestSetPullRequestDraft covers the pull request sync reading the host often only once the forge has
// changed the draft.
func TestSetPullRequestDraft(t *testing.T) {
	const address = "https://github.com/harrisoncramer/komrad/pull/1"

	tests := []struct {
		name          string
		address       string
		draft         bool
		draftErr      error
		wantErr       error
		wantDrafted   []drafted
		wantReadOften []string
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
			name:          "a pull request the forge marks a draft",
			address:       address,
			draft:         true,
			wantDrafted:   []drafted{{address: address, draft: true}},
			wantReadOften: []string{"github.com"},
		},
		{
			name:          "a draft the forge marks ready",
			address:       address,
			wantDrafted:   []drafted{{address: address, draft: false}},
			wantReadOften: []string{"github.com"},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			writing := &writingForge{err: tt.draftErr}
			sync := &waketest.Counter{}
			writer := newTestWriter(t, storetest.Open(t), writing, sync, &recordingCascade{})

			err := writer.SetPullRequestDraft(t.Context(), tt.address, tt.draft)

			if tt.wantErr != nil {
				require.ErrorIs(t, err, tt.wantErr)
			} else {
				require.NoError(t, err)
			}
			assert.Equal(t, tt.wantDrafted, writing.drafted)
			assert.Equal(t, tt.wantReadOften, sync.HostsReadOften)
		})
	}
}

// TestRequestPullRequestRereview covers the pull request sync reading the host often only once the
// forge has asked for another review.
func TestRequestPullRequestRereview(t *testing.T) {
	const address = "https://github.com/harrisoncramer/komrad/pull/1"

	tests := []struct {
		name           string
		address        string
		rereviewErr    error
		wantErr        error
		wantRereviewed []string
		wantReadOften  []string
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
			wantReadOften:  []string{"github.com"},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			writing := &writingForge{err: tt.rereviewErr}
			sync := &waketest.Counter{}
			writer := newTestWriter(t, storetest.Open(t), writing, sync, &recordingCascade{})

			err := writer.RequestPullRequestRereview(t.Context(), tt.address)

			if tt.wantErr != nil {
				require.ErrorIs(t, err, tt.wantErr)
			} else {
				require.NoError(t, err)
			}
			assert.Equal(t, tt.wantRereviewed, writing.rereviewed)
			assert.Equal(t, tt.wantReadOften, sync.HostsReadOften)
		})
	}
}

// TestSetPullRequestAutoMerge covers the head the person saw reaching the forge and the pull
// request sync reading the host often only once the forge has changed auto-merge.
func TestSetPullRequestAutoMerge(t *testing.T) {
	const address = "https://github.com/harrisoncramer/komrad/pull/1"

	tests := []struct {
		name           string
		params         forge.SetAutoMergeParams
		child          childTask
		autoMergeErr   error
		wantErr        error
		wantAutoMerged []forge.SetAutoMergeParams
		wantReadOften  []string
	}{
		{
			name:    "an address with no host",
			params:  forge.SetAutoMergeParams{URL: "harrisoncramer/komrad#1", On: true},
			wantErr: forge.ErrNotPullRequest,
		},
		{
			name:    "a pull request from a child",
			params:  forge.SetAutoMergeParams{URL: address, HeadSHA: "abc", On: true},
			child:   childTask{hasParent: true, parentState: "OPEN"},
			wantErr: errMergeHasParent,
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
			wantReadOften: []string{"github.com"},
		},
		{
			name:           "a pull request the forge stops waiting on",
			params:         forge.SetAutoMergeParams{URL: address},
			wantAutoMerged: []forge.SetAutoMergeParams{{URL: address}},
			wantReadOften:  []string{"github.com"},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			writing := &writingForge{err: tt.autoMergeErr}
			sync := &waketest.Counter{}
			stores := storetest.Open(t)
			seedChildTask(t, stores, tt.child)
			writer := newTestWriter(t, stores, writing, sync, &recordingCascade{})

			err := writer.SetPullRequestAutoMerge(t.Context(), tt.params)

			if tt.wantErr != nil {
				require.ErrorIs(t, err, tt.wantErr)
			} else {
				require.NoError(t, err)
			}
			assert.Equal(t, tt.wantAutoMerged, writing.autoMerged)
			assert.Equal(t, tt.wantReadOften, sync.HostsReadOften)
		})
	}
}

// childTask is a child task's pull request at the test address onto its parent's branch, seeded only once parentState names the state of the parent's own pull request.
type childTask struct {
	hasParent   bool
	parentState string
}

// seedChildTask records the parent and child tasks in the test repository, and their pull requests.
func seedChildTask(t *testing.T, stores *storetest.Stores, seeded childTask) {
	t.Helper()

	if seeded.parentState == "" {
		return
	}

	workflow := stores.SeedWorkflow(t, workflowstore.Config{Name: "parent-and-child"})
	parent := seedBranchTask(t, stores, workflow.ID, "task/parent")
	child := seedBranchTask(t, stores, workflow.ID, "task/child")
	if seeded.hasParent {
		require.NoError(t, stores.Tasks.SetParent(t.Context(), child, &taskstore.Parent{TaskID: parent, ForkPoint: "abc"}))
	}

	stores.SeedPullRequest(t, storetest.SeedPullRequestParams{
		RepositoryURL: taskRepository,
		Number:        2,
		HeadBranch:    "task/parent",
		HeadSHA:       "abc",
		BaseBranch:    "main",
		State:         seeded.parentState,
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
func seedBranchTask(t *testing.T, stores *storetest.Stores, workflowID uuid.UUID, branch string) uuid.UUID {
	t.Helper()

	agent := stores.SeedAgent(t, agentstore.Agent{Name: branch, WorkflowID: workflowID})
	stores.SeedTaskHead(t, agent.ID, taskstore.SetHeadParams{RepositoryURL: xptr.Ptr(taskRepository), Branch: xptr.Ptr(branch)})

	return *agent.TaskID
}

package git_test

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"

	"komrad/internal/agentdirs"
	"komrad/internal/git"
	"komrad/internal/interceptors"
	"komrad/internal/position"
	rpcgit "komrad/internal/rpc/git"
	"komrad/proto/gen/git/v1/gitv1connect"
	gitv1komrad "komrad/proto/gen/git/v1/gitv1komrad"

	"connectrpc.com/connect"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

type fakeGit struct {
	err      error
	root     string
	files    map[git.FileRevision][]byte
	count    int
	patch    []byte
	mappings []position.Mapping
	replayed *git.Replayed
	ancestor bool
}

func (f *fakeGit) GetRoot(context.Context, string) (string, error) {
	return f.root, f.err
}

func (f *fakeGit) ReadFiles(context.Context, string, []git.FileRevision) (map[git.FileRevision][]byte, error) {
	return f.files, f.err
}

func (f *fakeGit) CountChangedFiles(context.Context, git.CountChangedFilesParams) (int, error) {
	return f.count, f.err
}

func (f *fakeGit) Diff(context.Context, git.DiffParams) ([]byte, error) {
	return f.patch, f.err
}

func (f *fakeGit) MapRanges(context.Context, git.MapRangesParams) ([]position.Mapping, error) {
	return f.mappings, f.err
}

func (f *fakeGit) Replay(context.Context, git.ReplayParams) (*git.Replayed, error) {
	return f.replayed, f.err
}

func (f *fakeGit) IsAncestor(context.Context, string, string, string) (bool, error) {
	return f.ancestor, f.err
}

func serve(t *testing.T, fake *fakeGit) *git.EngineClient {
	t.Helper()

	validation, err := interceptors.NewValidation()
	require.NoError(t, err)

	mux := http.NewServeMux()
	mux.Handle(gitv1connect.NewGitServiceHandler(
		gitv1komrad.GitServiceAdapter{Impl: rpcgit.NewHandler(rpcgit.NewHandlerParams{Git: fake})},
		connect.WithInterceptors(validation),
	))

	server := httptest.NewServer(mux)
	t.Cleanup(server.Close)

	return git.NewEngineClient(gitv1connect.NewGitServiceClient(server.Client(), server.URL))
}

// TestRefuses covers what the handler refuses and which code each refusal carries.
func TestRefuses(t *testing.T) {
	tests := []struct {
		name     string
		gitErr   error
		call     func(ctx context.Context, client *git.EngineClient) error
		wantCode connect.Code
	}{
		{
			name:   "a directory outside every live agent",
			gitErr: agentdirs.ErrOutsideAgents,
			call: func(ctx context.Context, client *git.EngineClient) error {
				_, err := client.GetRoot(ctx, "/etc")

				return err
			},
			wantCode: connect.CodePermissionDenied,
		},
		{
			name:   "a directory that does not exist",
			gitErr: agentdirs.ErrNoDirectory,
			call: func(ctx context.Context, client *git.EngineClient) error {
				_, err := client.Diff(ctx, git.DiffParams{Dir: "/nowhere", FromSHA: "a", ToSHA: "b"})

				return err
			},
			wantCode: connect.CodeInvalidArgument,
		},
		{
			name: "a commit that reads as an option",
			call: func(ctx context.Context, client *git.EngineClient) error {
				_, err := client.Diff(ctx, git.DiffParams{Dir: "/w", FromSHA: "--output=/tmp/x", ToSHA: "b"})

				return err
			},
			wantCode: connect.CodeInvalidArgument,
		},
		{
			name: "a commit to map from that reads as an option",
			call: func(ctx context.Context, client *git.EngineClient) error {
				_, err := client.MapRanges(ctx, git.MapRangesParams{Dir: "/w", FromSHA: "-p", ToSHA: "b"})

				return err
			},
			wantCode: connect.CodeInvalidArgument,
		},
		{
			name: "a commit to replay onto that reads as an option",
			call: func(ctx context.Context, client *git.EngineClient) error {
				_, err := client.Replay(ctx, git.ReplayParams{Dir: "/w", FromBase: "a", OntoBase: "--x", SnapshotSHA: "c"})

				return err
			},
			wantCode: connect.CodeInvalidArgument,
		},
		{
			name: "a commit that may be an ancestor and reads as an option",
			call: func(ctx context.Context, client *git.EngineClient) error {
				_, err := client.IsAncestor(ctx, "/w", "--x", "b")

				return err
			},
			wantCode: connect.CodeInvalidArgument,
		},
		{
			name: "a file whose path would start another line of the batch",
			call: func(ctx context.Context, client *git.EngineClient) error {
				_, err := client.ReadFiles(ctx, "/w", []git.FileRevision{{SHA: "a", Path: "x\nHEAD:secret"}})

				return err
			},
			wantCode: connect.CodeInvalidArgument,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			client := serve(t, &fakeGit{err: tt.gitErr})

			err := tt.call(t.Context(), client)
			require.Error(t, err)
			assert.Equal(t, tt.wantCode, connect.CodeOf(err))
		})
	}
}

// TestAnswers covers each answer reaching the panel as the engine's git gave it.
func TestAnswers(t *testing.T) {
	present := git.FileRevision{SHA: "a1", Path: "greet.go"}
	empty := git.FileRevision{SHA: "a1", Path: "empty.go"}
	missing := git.FileRevision{SHA: "a1", Path: "gone.go"}

	mappings := []position.Mapping{
		{
			File: position.FileMapping{OldPath: "greet.go", NewPath: "hello.go"},
			Ranges: []position.RangeMapping{
				{
					Old: position.Range{Start: 3, End: 5},
					New: position.Range{Start: 3, End: 3},
				},
				{
					Old: position.Range{Start: 9, End: 9},
					New: position.Range{Start: 7, End: 10},
				},
			},
		},
		{
			File:   position.FileMapping{OldPath: "logo.png"},
			Ranges: []position.RangeMapping{},
		},
	}

	fake := &fakeGit{
		root:     "/w/agent",
		files:    map[git.FileRevision][]byte{present: []byte("package greet\n"), empty: {}},
		count:    4,
		patch:    []byte("diff --git a/greet.go b/greet.go\n"),
		mappings: mappings,
		replayed: &git.Replayed{Tree: "t1", Conflicts: []string{"greet.go"}},
		ancestor: true,
	}
	client := serve(t, fake)

	tests := []struct {
		name string
		call func(ctx context.Context) (any, error)
		want any
	}{
		{
			name: "the top of the repository",
			call: func(ctx context.Context) (any, error) {
				return client.GetRoot(ctx, "/w/agent/sub")
			},
			want: "/w/agent",
		},
		{
			name: "the files the commit holds, an empty one included, and none it does not",
			call: func(ctx context.Context) (any, error) {
				return client.ReadFiles(ctx, "/w/agent", []git.FileRevision{present, empty, missing})
			},
			want: map[git.FileRevision][]byte{present: []byte("package greet\n"), empty: {}},
		},
		{
			name: "the count of changed files",
			call: func(ctx context.Context) (any, error) {
				return client.CountChangedFiles(ctx, git.CountChangedFilesParams{Dir: "/w/agent"})
			},
			want: 4,
		},
		{
			name: "the patch",
			call: func(ctx context.Context) (any, error) {
				return client.Diff(ctx, git.DiffParams{Dir: "/w/agent", FromSHA: "a1", ToSHA: "b2"})
			},
			want: []byte("diff --git a/greet.go b/greet.go\n"),
		},
		{
			name: "a rename with a rewritten region and a point, and a deleted file drawn without content",
			call: func(ctx context.Context) (any, error) {
				return client.MapRanges(ctx, git.MapRangesParams{Dir: "/w/agent", FromSHA: "a1", ToSHA: "b2"})
			},
			want: mappings,
		},
		{
			name: "the replayed tree and the paths that would not settle",
			call: func(ctx context.Context) (any, error) {
				return client.Replay(ctx, git.ReplayParams{Dir: "/w/agent", FromBase: "a1", OntoBase: "b2", SnapshotSHA: "c3"})
			},
			want: &git.Replayed{Tree: "t1", Conflicts: []string{"greet.go"}},
		},
		{
			name: "whether one commit is an ancestor of another",
			call: func(ctx context.Context) (any, error) {
				return client.IsAncestor(ctx, "/w/agent", "a1", "b2")
			},
			want: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := tt.call(t.Context())
			require.NoError(t, err)
			assert.Equal(t, tt.want, got)
		})
	}
}

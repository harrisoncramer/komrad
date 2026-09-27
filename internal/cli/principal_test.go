package cli

import (
	"bytes"
	"context"
	"errors"
	"testing"
	"time"

	"komrad/internal/principalrole"
	"komrad/internal/principals"
	identitystore "komrad/internal/store/identity"
	"komrad/lib/xptr"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

var errBoom = errors.New("boom")

type fakePrincipalEditor struct {
	added    principals.AddParams
	bound    principals.BindParams
	previous principalrole.Role
	listed   []principals.Listed
	toggled  string
	removed  int64
	err      error
}

func (f *fakePrincipalEditor) Add(_ context.Context, params principals.AddParams) (*identitystore.Principal, error) {
	f.added = params
	if f.err != nil {
		return nil, f.err
	}

	return &identitystore.Principal{ID: "0190", Name: params.Name, Kind: params.Kind}, nil
}

func (f *fakePrincipalEditor) List(context.Context) ([]principals.Listed, error) {
	return f.listed, f.err
}

func (f *fakePrincipalEditor) Bind(_ context.Context, params principals.BindParams) (principalrole.Role, error) {
	f.bound = params

	return f.previous, f.err
}

func (f *fakePrincipalEditor) Unbind(context.Context, string, string) error {
	return f.err
}

func (f *fakePrincipalEditor) Disable(_ context.Context, name string) (*identitystore.Principal, error) {
	f.toggled = "disable"

	return &identitystore.Principal{Name: name}, f.err
}

func (f *fakePrincipalEditor) Enable(_ context.Context, name string) (*identitystore.Principal, error) {
	f.toggled = "enable"

	return &identitystore.Principal{Name: name}, f.err
}

func (f *fakePrincipalEditor) Remove(context.Context, string) (int64, error) {
	return f.removed, f.err
}

// TestPrincipalCommands covers what each subcommand hands the service and prints.
func TestPrincipalCommands(t *testing.T) {
	disabledAt := time.Now()

	tests := []struct {
		name    string
		args    []string
		editor  fakePrincipalEditor
		want    string
		wantErr error
		check   func(t *testing.T, editor *fakePrincipalEditor)
	}{
		{
			name:    "a refusal from the service is the command's error",
			args:    []string{"add", "ci", "--service"},
			editor:  fakePrincipalEditor{err: errBoom},
			wantErr: errBoom,
		},
		{
			name: "add adds a person with the identity it was given",
			args: []string{"add", "harry", "--provider", "github", "--subject", "1"},
			want: "added harry, id 0190\n",
			check: func(t *testing.T, editor *fakePrincipalEditor) {
				assert.Equal(t, principals.AddParams{
					Name:     "harry",
					Kind:     identitystore.KindPerson,
					Provider: "github",
					Subject:  "1",
				}, editor.added)
			},
		},
		{
			name: "add with --service adds a service",
			args: []string{"add", "ci", "--service"},
			want: "added ci, id 0190\n",
			check: func(t *testing.T, editor *fakePrincipalEditor) {
				assert.Equal(t, identitystore.KindService, editor.added.Kind)
			},
		},
		{
			name: "list shows identity, state and bindings, with a dash for none",
			args: []string{"list"},
			editor: fakePrincipalEditor{listed: []principals.Listed{
				{
					Principal: identitystore.Principal{
						ID:   "0191",
						Name: "ci",
						Kind: identitystore.KindService,
					},
				},
				{
					Principal: identitystore.Principal{
						ID:         "0192",
						Name:       "harry",
						Kind:       identitystore.KindPerson,
						Provider:   xptr.Ptr(identitystore.ProviderGitHub),
						Subject:    xptr.Ptr("1"),
						DisabledAt: &disabledAt,
					},
					Bindings: []identitystore.Binding{
						{
							EngineName: "home",
							Role:       principalrole.Operator,
						},
						{
							EngineName: "work",
							Role:       principalrole.Viewer,
						},
					},
				},
			}},
			want: "NAME   ID    KIND     IDENTITY  STATE     BINDINGS\n" +
				"ci     0191  service  -         active    -\n" +
				"harry  0192  person   github:1  disabled  home:operator,work:viewer\n",
		},
		{
			name: "bind says what the principal now is",
			args: []string{"bind", "ci", "home", "viewer"},
			want: "ci is viewer on home\n",
			check: func(t *testing.T, editor *fakePrincipalEditor) {
				assert.Equal(t, principals.BindParams{
					Principal: "ci",
					Engine:    "home",
					Role:      "viewer",
				}, editor.bound)
			},
		},
		{
			name:   "bind names the role it replaced",
			args:   []string{"bind", "ci", "home", "operator"},
			editor: fakePrincipalEditor{previous: principalrole.Viewer},
			want:   "ci is operator on home, was viewer\n",
		},
		{
			name: "unbind says the principal has no role left",
			args: []string{"unbind", "ci", "home"},
			want: "ci has no role on home\n",
		},
		{
			name: "disable warns that issued tokens outlive it",
			args: []string{"disable", "ci"},
			want: "disabled ci; tokens already issued keep working until they expire\n",
			check: func(t *testing.T, editor *fakePrincipalEditor) {
				assert.Equal(t, "disable", editor.toggled)
			},
		},
		{
			name: "enable enables",
			args: []string{"enable", "ci"},
			want: "enabled ci\n",
			check: func(t *testing.T, editor *fakePrincipalEditor) {
				assert.Equal(t, "enable", editor.toggled)
			},
		},
		{
			name:   "remove counts one binding in the singular",
			args:   []string{"remove", "ci"},
			editor: fakePrincipalEditor{removed: 1},
			want:   "removed ci and 1 binding; tokens already issued keep working until they expire\n",
		},
		{
			name: "remove counts no bindings in the plural",
			args: []string{"remove", "ci"},
			want: "removed ci and 0 bindings; tokens already issued keep working until they expire\n",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			editor := tt.editor
			closed := false
			cmd := newPrincipalCmd(func(context.Context) (principalEditor, func(), error) {
				return &editor, func() { closed = true }, nil
			})

			var out bytes.Buffer
			cmd.SetOut(&out)
			cmd.SetArgs(tt.args)

			err := cmd.ExecuteContext(t.Context())
			assert.True(t, closed, "the command closes what it opened")
			if tt.wantErr != nil {
				require.ErrorIs(t, err, tt.wantErr)

				return
			}

			require.NoError(t, err)
			assert.Equal(t, tt.want, out.String())
			if tt.check != nil {
				tt.check(t, &editor)
			}
		})
	}
}

// TestPrincipalCommandsStopWhenTheDatabaseRefuses covers a command whose owner connection fails.
func TestPrincipalCommandsStopWhenTheDatabaseRefuses(t *testing.T) {
	cmd := newPrincipalCmd(func(context.Context) (principalEditor, func(), error) {
		return nil, nil, errBoom
	})
	cmd.SetArgs([]string{"list"})

	require.ErrorIs(t, cmd.ExecuteContext(t.Context()), errBoom)
}

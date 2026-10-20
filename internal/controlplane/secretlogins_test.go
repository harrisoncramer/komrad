package controlplane_test

import (
	"context"
	"errors"
	"testing"

	"komrad/internal/controlplane"
	"komrad/internal/forge"

	"github.com/aws/aws-sdk-go-v2/aws"
	"github.com/aws/aws-sdk-go-v2/service/secretsmanager"
	"github.com/aws/aws-sdk-go-v2/service/secretsmanager/types"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// fakeSecrets answers for the secrets it holds, where a secret with no value has never been written.
type fakeSecrets struct {
	values map[string]*string
	reads  int
	err    error
}

func (f *fakeSecrets) GetSecretValue(_ context.Context, input *secretsmanager.GetSecretValueInput, _ ...func(*secretsmanager.Options)) (*secretsmanager.GetSecretValueOutput, error) {
	f.reads++
	if f.err != nil {
		return nil, f.err
	}
	value, ok := f.values[aws.ToString(input.SecretId)]
	if !ok || value == nil {
		return nil, &types.ResourceNotFoundException{Message: aws.String("Secrets Manager can't find the specified secret value")}
	}

	return &secretsmanager.GetSecretValueOutput{SecretString: value}, nil
}

func (f *fakeSecrets) PutSecretValue(_ context.Context, input *secretsmanager.PutSecretValueInput, _ ...func(*secretsmanager.Options)) (*secretsmanager.PutSecretValueOutput, error) {
	if f.err != nil {
		return nil, f.err
	}
	f.values[aws.ToString(input.SecretId)] = input.SecretString

	return &secretsmanager.PutSecretValueOutput{}, nil
}

// TestSecretLoginsGet covers reading an engine's GitHub login from its secret.
func TestSecretLoginsGet(t *testing.T) {
	tests := []struct {
		name        string
		values      map[string]*string
		err         error
		wantNoLogin bool
		wantErr     string
		wantLogin   string
		wantReads   int
	}{
		{
			name:        "a secret never written",
			values:      map[string]*string{"komrad/logins/harry": nil},
			wantNoLogin: true,
		},
		{
			name:        "a secret holding an empty login",
			values:      map[string]*string{"komrad/logins/harry": aws.String("")},
			wantNoLogin: true,
		},
		{
			name:    "Secrets Manager failing",
			err:     errors.New("throttled"),
			wantErr: "throttled",
		},
		{
			name:      "a login, read once",
			values:    map[string]*string{"komrad/logins/harry": aws.String("gho_harry\n")},
			wantLogin: "gho_harry",
			wantReads: 1,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			secrets := &fakeSecrets{values: tt.values, err: tt.err}
			logins := controlplane.NewSecretLogins(secrets, "komrad/logins/")

			login, err := logins.Get(t.Context(), "harry")
			if tt.wantNoLogin {
				assert.ErrorIs(t, err, forge.ErrNoCredential)

				return
			}
			if tt.wantErr != "" {
				assert.ErrorContains(t, err, tt.wantErr)

				return
			}

			require.NoError(t, err)
			assert.Equal(t, tt.wantLogin, login.Expose())

			_, err = logins.Get(t.Context(), "harry")
			require.NoError(t, err)
			assert.Equal(t, tt.wantReads, secrets.reads)
		})
	}
}

// TestSecretLoginsSet covers a login written to its secret being read back without asking for it.
func TestSecretLoginsSet(t *testing.T) {
	secrets := &fakeSecrets{values: map[string]*string{"komrad/logins/harry": nil}}
	logins := controlplane.NewSecretLogins(secrets, "komrad/logins/")

	require.NoError(t, logins.Set(t.Context(), "harry", forge.NewSecret("gho_harry")))

	login, err := logins.Get(t.Context(), "harry")
	require.NoError(t, err)
	assert.Equal(t, "gho_harry", login.Expose())
	assert.Equal(t, "gho_harry", aws.ToString(secrets.values["komrad/logins/harry"]))
	assert.Zero(t, secrets.reads)
}

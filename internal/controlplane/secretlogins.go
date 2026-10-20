package controlplane

import (
	"context"
	"errors"
	"fmt"
	"sync"

	"github.com/aws/aws-sdk-go-v2/aws"
	"github.com/aws/aws-sdk-go-v2/service/secretsmanager"
	"github.com/aws/aws-sdk-go-v2/service/secretsmanager/types"

	"komrad/internal/forge"
)

// secretReadWriter is the part of Secrets Manager the login store calls.
type secretReadWriter interface {
	GetSecretValue(ctx context.Context, input *secretsmanager.GetSecretValueInput, opts ...func(*secretsmanager.Options)) (*secretsmanager.GetSecretValueOutput, error)
	PutSecretValue(ctx context.Context, input *secretsmanager.PutSecretValueInput, opts ...func(*secretsmanager.Options)) (*secretsmanager.PutSecretValueOutput, error)
}

// SecretLogins keeps each engine's GitHub login in a Secrets Manager secret named for the
// engine, and in memory once read.
type SecretLogins struct {
	secrets secretReadWriter
	prefix  string
	mu      sync.Mutex
	logins  map[string]forge.Secret
}

// NewSecretLogins builds a login store over the secrets whose names start with prefix.
func NewSecretLogins(secrets secretReadWriter, prefix string) *SecretLogins {
	return &SecretLogins{
		secrets: secrets,
		prefix:  prefix,
		logins:  make(map[string]forge.Secret),
	}
}

// Get is the engine's login, or forge.ErrNoCredential while its secret holds none.
func (s *SecretLogins) Get(ctx context.Context, engine string) (forge.Secret, error) {
	s.mu.Lock()
	login, cached := s.logins[engine]
	s.mu.Unlock()
	if cached {
		return login, nil
	}

	output, err := s.secrets.GetSecretValue(ctx, &secretsmanager.GetSecretValueInput{SecretId: aws.String(s.prefix + engine)})
	if notFound := (*types.ResourceNotFoundException)(nil); errors.As(err, &notFound) {
		return forge.Secret{}, forge.ErrNoCredential.Saying("engine %s holds no GitHub login", engine)
	}
	if err != nil {
		return forge.Secret{}, fmt.Errorf("failed to read the GitHub login for engine %s: %w", engine, err)
	}

	login = forge.NewSecret(aws.ToString(output.SecretString))
	if login.IsZero() {
		return forge.Secret{}, forge.ErrNoCredential.Saying("engine %s holds no GitHub login", engine)
	}

	s.mu.Lock()
	s.logins[engine] = login
	s.mu.Unlock()

	return login, nil
}

// Set writes the engine's login to its secret.
func (s *SecretLogins) Set(ctx context.Context, engine string, login forge.Secret) error {
	if _, err := s.secrets.PutSecretValue(ctx, &secretsmanager.PutSecretValueInput{
		SecretId:     aws.String(s.prefix + engine),
		SecretString: aws.String(login.Expose()),
	}); err != nil {
		return fmt.Errorf("failed to write the GitHub login for engine %s: %w", engine, err)
	}

	s.mu.Lock()
	s.logins[engine] = login
	s.mu.Unlock()

	return nil
}

// Package principals owns the rules for adding principals and binding them to engines.
package principals

import (
	"context"
	"fmt"
	"regexp"
	"slices"
	"strings"

	"komrad/internal/fail"
	"komrad/internal/principalrole"
	identitystore "komrad/internal/store/identity"
	"komrad/lib/xslice"
)

var (
	// ErrBadName is a principal name the schema would refuse.
	ErrBadName = fail.InvalidArgument("a principal name is 1 to 63 lowercase letters, digits, underscores or hyphens, starting with a letter")
	// ErrBadIdentity is a person without an outside identity, or half of one.
	ErrBadIdentity = fail.InvalidArgument("a person needs a provider and a subject, and a service principal takes both or neither")
	// ErrUnknownProvider is a provider that is none of identitystore.Providers.
	ErrUnknownProvider = fail.InvalidArgument("unknown provider")
)

var namePattern = regexp.MustCompile(`^[a-z][a-z0-9_-]{0,62}$`)

type NewPrincipalServiceParams struct {
	Principals *identitystore.PrincipalStore
	Bindings   *identitystore.BindingStore
}

type PrincipalService struct {
	principals *identitystore.PrincipalStore
	bindings   *identitystore.BindingStore
}

// NewPrincipalService builds the service over the principal and binding stores, both on the owner's pool.
func NewPrincipalService(params NewPrincipalServiceParams) *PrincipalService {
	return &PrincipalService{principals: params.Principals, bindings: params.Bindings}
}

// Listed is one principal with its bindings.
type Listed struct {
	Principal identitystore.Principal
	Bindings  []identitystore.Binding
}

type AddParams struct {
	Name     string
	Kind     identitystore.Kind
	Provider string
	Subject  string
}

// Add creates a principal after checking its name and outside identity.
func (s *PrincipalService) Add(ctx context.Context, params AddParams) (*identitystore.Principal, error) {
	if !namePattern.MatchString(params.Name) {
		return nil, ErrBadName.Saying("principal name %q must be 1 to 63 lowercase letters, digits, underscores or hyphens, starting with a letter", params.Name)
	}

	if (params.Provider == "") != (params.Subject == "") || (params.Kind == identitystore.KindPerson && params.Provider == "") {
		return nil, ErrBadIdentity
	}

	created := identitystore.CreateParams{Name: params.Name, Kind: params.Kind}
	if params.Provider != "" {
		provider, err := parseProvider(params.Provider)
		if err != nil {
			return nil, err
		}

		created.Provider = &provider
		created.Subject = &params.Subject
	}

	return s.principals.Create(ctx, created)
}

// List returns every principal with its bindings, principals by name and bindings by engine name.
func (s *PrincipalService) List(ctx context.Context) ([]Listed, error) {
	principals, err := s.principals.List(ctx)
	if err != nil {
		return nil, fmt.Errorf("failed to list principals: %w", err)
	}

	bindings, err := s.bindings.List(ctx)
	if err != nil {
		return nil, fmt.Errorf("failed to list bindings: %w", err)
	}

	return xslice.Map(principals, func(principal identitystore.Principal) Listed {
		return Listed{
			Principal: principal,
			Bindings: slices.DeleteFunc(slices.Clone(bindings), func(binding identitystore.Binding) bool {
				return binding.PrincipalID != principal.ID
			}),
		}
	}), nil
}

type BindParams struct {
	Principal string
	Engine    string
	Role      string
}

// Bind gives the named principal the role on the named engine, returning the role it replaced or an empty one.
func (s *PrincipalService) Bind(ctx context.Context, params BindParams) (principalrole.Role, error) {
	role, err := principalrole.Parse(params.Role)
	if err != nil {
		return "", err
	}

	principal, err := s.principals.GetByName(ctx, params.Principal)
	if err != nil {
		return "", fmt.Errorf("failed to find the principal to bind: %w", err)
	}

	engine, err := s.bindings.GetEngineByName(ctx, params.Engine)
	if err != nil {
		return "", fmt.Errorf("failed to find the engine to bind to: %w", err)
	}

	return s.bindings.Set(ctx, identitystore.SetParams{PrincipalID: principal.ID, EngineID: engine.ID, Role: role})
}

// Unbind removes the named principal's role on the named engine, if it has one.
func (s *PrincipalService) Unbind(ctx context.Context, principalName, engineName string) error {
	principal, err := s.principals.GetByName(ctx, principalName)
	if err != nil {
		return fmt.Errorf("failed to find the principal to unbind: %w", err)
	}

	engine, err := s.bindings.GetEngineByName(ctx, engineName)
	if err != nil {
		return fmt.Errorf("failed to find the engine to unbind from: %w", err)
	}

	return s.bindings.Delete(ctx, principal.ID, engine.ID)
}

// Disable stops the named principal being issued tokens, keeping its bindings.
func (s *PrincipalService) Disable(ctx context.Context, name string) (*identitystore.Principal, error) {
	return s.principals.SetDisabled(ctx, name, true)
}

// Enable lets a disabled principal be issued tokens again, with the bindings it had.
func (s *PrincipalService) Enable(ctx context.Context, name string) (*identitystore.Principal, error) {
	return s.principals.SetDisabled(ctx, name, false)
}

// Remove deletes the named principal and its bindings, returning how many bindings went with it.
func (s *PrincipalService) Remove(ctx context.Context, name string) (int64, error) {
	return s.principals.Delete(ctx, name)
}

func parseProvider(spelled string) (identitystore.Provider, error) {
	provider := identitystore.Provider(spelled)
	if !slices.Contains(identitystore.Providers, provider) {
		names := xslice.Map(identitystore.Providers, func(provider identitystore.Provider) string {
			return string(provider)
		})

		return "", ErrUnknownProvider.Saying("unknown provider %q, want one of %s", spelled, strings.Join(names, ", "))
	}

	return provider, nil
}

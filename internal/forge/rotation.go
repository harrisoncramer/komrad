package forge

import (
	"context"
	"errors"
	"fmt"
	"slices"
	"time"

	forgestore "komrad/internal/store/forge"
)

// rotationScope is the scope a GitLab token needs to rotate itself.
const rotationScope = "api"

// proveRotatingToken asks GitLab who a pasted token belongs to and refuses one that cannot
// rotate itself.
func (s *Service) proveRotatingToken(ctx context.Context, build Builder, params ConnectParams) (string, error) {
	if params.Kind != KindGitLab {
		return "", ErrRotatingNotGitLab
	}

	login, err := s.proveToken(ctx, build, params)
	if err != nil {
		return "", err
	}

	rotator, err := pastedRotator(build, params)
	if err != nil {
		return "", err
	}
	info, err := rotator.ReadToken(ctx)
	if err != nil {
		return "", fmt.Errorf("failed to read the token from %s: %w", params.Host, err)
	}
	if !slices.Contains(info.Scopes, rotationScope) {
		return "", ErrTokenCannotRotate.Saying("the token for %s needs the %s scope to rotate itself but has %v", params.Host, rotationScope, info.Scopes)
	}

	return login, nil
}

// rotateAtConnect trades a pasted token for the first one komrad keeps.
func (s *Service) rotateAtConnect(ctx context.Context, build Builder, params ConnectParams) (Secret, error) {
	rotator, err := pastedRotator(build, params)
	if err != nil {
		return Secret{}, err
	}

	token, err := rotator.RotateToken(ctx, rotatedExpiry(s.now()))
	if err != nil {
		return Secret{}, fmt.Errorf("failed to rotate the token with %s: %w", params.Host, err)
	}

	return token, nil
}

// rotateIfDue rotates a connection's token when it expires tomorrow or sooner, with s.rotation held.
func (s *Service) rotateIfDue(ctx context.Context, connection forgestore.Connection, f Forge) error {
	rotator, err := rotatorFor(f)
	if err != nil {
		return err
	}

	info, err := rotator.ReadToken(ctx)
	if err != nil {
		return fmt.Errorf("failed to read the token for %s: %w", connection.Host, err)
	}
	if !rotationDue(info.ExpiresAt, s.now()) {
		return nil
	}

	token, err := rotator.RotateToken(ctx, rotatedExpiry(s.now()))
	if err != nil {
		return fmt.Errorf("failed to rotate the token for %s: %w", connection.Host, err)
	}

	return s.credentials.Set(ctx, connection.ID, token)
}

// pastedRotator is the pasted token, as a rotator.
func pastedRotator(build Builder, params ConnectParams) (TokenRotator, error) {
	return rotatorFor(build(params.APIURL, staticCredential{Header: HeaderPrivateToken, Token: params.Token}))
}

// rotatorFor is the forge as one that rotates its token, or ErrNoRotation.
func rotatorFor(f Forge) (TokenRotator, error) {
	rotator, ok := f.(TokenRotator)
	if !ok {
		return nil, ErrNoRotation
	}

	return rotator, nil
}

// rotationDue says whether a token expiring at expiresAt has to be rotated by now.
func rotationDue(expiresAt, now time.Time) bool {
	return !expiresAt.IsZero() && !expiresAt.After(utcDay(now).AddDate(0, 0, 1))
}

// rotatedExpiry is the day a token rotated now expires.
func rotatedExpiry(now time.Time) time.Time {
	return utcDay(now).AddDate(0, 0, 2)
}

// utcDay is the start of the UTC day t falls on.
func utcDay(t time.Time) time.Time {
	year, month, day := t.UTC().Date()

	return time.Date(year, month, day, 0, 0, 0, 0, time.UTC)
}

// revoke ends a rotating token at the forge, letting one the forge already refuses go.
func (s *Service) revoke(ctx context.Context, connection forgestore.Connection) error {
	held, err := s.HasCredential(ctx, connection)
	if err != nil || !held {
		return err
	}

	f, err := s.ForConnection(connection)
	if err != nil {
		return err
	}
	rotator, err := rotatorFor(f)
	if err != nil {
		return err
	}

	s.rotation.Lock()
	defer s.rotation.Unlock()

	err = rotator.RevokeToken(ctx)
	if errors.Is(err, ErrUnauthorized) {
		return nil
	}
	if err != nil {
		return fmt.Errorf("failed to revoke the token for %s, so revoke it on GitLab by hand: %w", connection.Host, err)
	}

	return nil
}

// RotateTokens checks every rotating token connection, which rotates each token that is due.
func (s *Service) RotateTokens(ctx context.Context) error {
	connections, err := s.connections.List(ctx)
	if err != nil {
		return err
	}

	var errs []error
	for _, connection := range connections {
		if connection.Method != MethodRotatingToken {
			continue
		}

		result, err := s.RecheckConnection(ctx, connection)
		if err != nil {
			errs = append(errs, err)

			continue
		}
		if result.Problem != nil {
			errs = append(errs, result.Problem)
		}
	}

	return errors.Join(errs...)
}

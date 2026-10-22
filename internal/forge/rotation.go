package forge

import (
	"context"
	"errors"
	"fmt"
	"slices"
	"time"

	forgestore "komrad/internal/store/forge"

	"github.com/google/uuid"
)

// rotationScope is the scope a GitLab token needs to rotate itself.
const rotationScope = "api"

// RotateTokens rechecks every rotating token connection, rotating each token near its expiry.
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

// proveRotatingToken asks GitLab who a pasted token belongs to and refuses one that cannot
// rotate itself.
func (s *Service) proveRotatingToken(ctx context.Context, build Builder, params ConnectParams) (string, error) {
	if params.Kind != KindGitLab {
		return "", errRotatingNotGitLab
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
		return "", errTokenCannotRotate.Saying("the token for %s needs the %s scope to rotate itself but has %v", params.Host, rotationScope, info.Scopes)
	}

	return login, nil
}

// keepRotatedToken trades a pasted token for the first one komrad keeps, and stores that one.
func (s *Service) keepRotatedToken(ctx context.Context, build Builder, params ConnectParams, connectionID uuid.UUID) error {
	rotator, err := pastedRotator(build, params)
	if err != nil {
		return err
	}

	token, err := rotator.RotateToken(ctx, rotatedExpiry(s.now()))
	if err != nil {
		return fmt.Errorf("failed to rotate the token with %s: %w", params.Host, err)
	}

	if err := s.credentials.Set(ctx, connectionID, token); err != nil {
		return fmt.Errorf("failed to keep the token gitlab rotated the pasted one into, so make a new token: %w", err)
	}

	return nil
}

// checkRotating verifies a rotating token and rotates it when due, as the one writer.
func (s *Service) checkRotating(ctx context.Context, connection forgestore.Connection) (string, error) {
	s.rotation.Lock()
	defer s.rotation.Unlock()

	f, err := s.heldForge(ctx, connection)
	if err != nil {
		return "", err
	}

	login, err := f.Verify(ctx)
	if err != nil {
		return "", err
	}

	if err := s.rotateIfDue(ctx, connection, f); err != nil {
		return "", err
	}

	return login, nil
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
	if !isRotationDue(info.ExpiresAt, s.now()) {
		return nil
	}

	token, err := rotator.RotateToken(ctx, rotatedExpiry(s.now()))
	if err != nil {
		return fmt.Errorf("failed to rotate the token for %s: %w", connection.Host, err)
	}

	return s.credentials.Set(ctx, connection.ID, token)
}

// revokeRotatingToken ends a rotating token at the forge, counting one the forge already refuses as ended.
func (s *Service) revokeRotatingToken(ctx context.Context, connection forgestore.Connection) error {
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

// pastedRotator is the pasted token, as a rotator.
func pastedRotator(build Builder, params ConnectParams) (tokenRotator, error) {
	return rotatorFor(build(params.APIURL, staticCredential{Header: HeaderPrivateToken, Token: params.Token}))
}

// rotatorFor is the forge as one that rotates its token, or ErrNoRotation.
func rotatorFor(f Forge) (tokenRotator, error) {
	rotator, ok := f.(tokenRotator)
	if !ok {
		return nil, errNoRotation
	}

	return rotator, nil
}

// isRotationDue says whether a token expiring at expiresAt has to be rotated by now.
func isRotationDue(expiresAt, now time.Time) bool {
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

package agent

import (
	"context"
	"crypto/rand"
	"errors"
	"fmt"
	"strings"

	"weave/db/models"
	"weave/internal/store"
)

// refAlphabet is Crockford Base32, which drops I, L, O and U from the usual thirty-six so
// nothing in a ref can be misread as something else or spell a word by accident.
const refAlphabet = "0123456789ABCDEFGHJKMNPQRSTVWXYZ"

// refLength is how many characters a minted ref carries. Six is a billion of them, which is
// far more than a panel will ever hold, and still short enough to read off a row and type.
const refLength = 6

// minRefSuffix is the shortest suffix a caller may name an agent by. Fewer characters than
// this would resolve by luck rather than by naming anything.
const minRefSuffix = 3

// maxRefMatches bounds what an ambiguous suffix reports back, since the point of the error
// is to show the caller enough matches to pick from.
const maxRefMatches = 5

// refMintAttempts bounds the search for a free ref, so a database that somehow answers
// taken every time fails the create rather than spinning.
const refMintAttempts = 8

// ErrAmbiguousRef reports that a typed ref is the tail of more than one agent's.
var ErrAmbiguousRef = errors.New("that ref names more than one agent")

// ErrRefTooShort reports that a typed ref is too short to name anything on purpose.
var ErrRefTooShort = errors.New("that ref is too short")

// Resolve returns the agent a caller named, either by its id or by the tail of its ref. An
// id is checked first, since a uuid is what every tool already passes and a ref is never
// one. Nothing matched is a nil agent rather than an error, the same as GetByID, so callers
// report an unknown ref the way they already report an unknown id.
func (s *Store) Resolve(ctx context.Context, ref string) (*Agent, error) {
	if store.IsID(ref) {
		return s.GetByID(ctx, ref)
	}

	suffix, ok := normalizeRef(ref)
	if !ok {
		return nil, nil
	}
	if len(suffix) < minRefSuffix {
		return nil, fmt.Errorf("%w: %q is under %d characters", ErrRefTooShort, ref, minRefSuffix)
	}

	rows, err := s.queries.ListAgentsByRefSuffix(ctx, models.ListAgentsByRefSuffixParams{
		Suffix: suffix,
		Lim:    maxRefMatches,
	})
	if err != nil {
		return nil, fmt.Errorf("failed to look up agents by ref %q: %w", ref, err)
	}

	switch len(rows) {
	case 0:
		return nil, nil
	case 1:
		return s.hydrateMetadataForAgent(ctx, rows[0])
	default:
		return nil, fmt.Errorf("%w: %q matches %s, so type more of it", ErrAmbiguousRef, ref, describeRefMatches(rows))
	}
}

// mintRef picks a ref no agent holds. A collision is vanishingly unlikely at this length,
// but looking for one costs a single query and the unique index would otherwise turn one
// into a failed launch.
func (s *Store) mintRef(ctx context.Context) (string, error) {
	for range refMintAttempts {
		ref, err := newRef()
		if err != nil {
			return "", err
		}

		taken, err := s.queries.AgentRefExists(ctx, ref)
		if err != nil {
			return "", fmt.Errorf("failed to check whether the ref %q is taken: %w", ref, err)
		}
		if !taken {
			return ref, nil
		}
	}

	return "", fmt.Errorf("failed to find a free ref in %d attempts", refMintAttempts)
}

func newRef() (string, error) {
	buf := make([]byte, refLength)
	if _, err := rand.Read(buf); err != nil {
		return "", fmt.Errorf("failed to mint a ref: %w", err)
	}

	// The alphabet's length divides a byte's range evenly, so indexing by the remainder
	// favours no symbol over another.
	out := make([]byte, refLength)
	for i, b := range buf {
		out[i] = refAlphabet[int(b)%len(refAlphabet)]
	}

	return string(out), nil
}

// normalizeRef maps what somebody typed onto the alphabet refs are minted from, reporting
// false when it holds a character no ref can. Case, separators and the letters Crockford
// leaves out are all forgiven, since a ref is read off a screen and typed back by hand.
func normalizeRef(typed string) (string, bool) {
	var b strings.Builder
	for _, symbol := range strings.ToUpper(typed) {
		switch symbol {
		case '-', ' ':
			continue
		case 'I', 'L':
			symbol = '1'
		case 'O':
			symbol = '0'
		}

		if !strings.ContainsRune(refAlphabet, symbol) {
			return "", false
		}

		b.WriteRune(symbol)
	}

	return b.String(), true
}

func describeRefMatches(rows []*models.Agent) string {
	named := make([]string, 0, len(rows))
	for _, row := range rows {
		named = append(named, fmt.Sprintf("%s (%s)", row.Ref, row.Name))
	}

	return fmt.Sprintf("%d agents: %s", len(rows), strings.Join(named, ", "))
}

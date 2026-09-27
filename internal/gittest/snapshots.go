package gittest

import (
	"context"
	"fmt"

	"komrad/internal/git"
)

// Snapshots freezes whatever tree its fields name and hands back the files it was given.
type Snapshots struct {
	SHA     string
	BaseSHA string
	// Files is each path as every snapshot holds it.
	Files    map[string][]string
	Released []string
	// ReleasedFrom is the checkout each ref in Released was let go of from.
	ReleasedFrom []string
	// Taking runs while each snapshot is taken, so a test can hold it open or move the world under it.
	Taking func()
}

// TakeSnapshot runs Taking and answers SHA and BaseSHA.
func (s *Snapshots) TakeSnapshot(context.Context, git.TakeSnapshotParams) (*git.Snapshot, error) {
	if s.Taking != nil {
		s.Taking()
	}

	return &git.Snapshot{
		SHA:     s.SHA,
		BaseSHA: s.BaseSHA,
	}, nil
}

// HoldRef does nothing.
func (*Snapshots) HoldRef(context.Context, string, string, string) error {
	return nil
}

// ReleaseRef records the ref and the checkout it was let go of from.
func (s *Snapshots) ReleaseRef(_ context.Context, dir, ref string) error {
	s.Released = append(s.Released, ref)
	s.ReleasedFrom = append(s.ReleasedFrom, dir)

	return nil
}

// ReadFileLines answers what Files holds for the path, refusing one it does not name.
func (s *Snapshots) ReadFileLines(_ context.Context, _, _, path string) ([]string, error) {
	lines, ok := s.Files[path]
	if !ok {
		return nil, fmt.Errorf("%w: no path %q", ErrNoGit, path)
	}

	return lines, nil
}

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
}

// TakeSnapshot answers SHA and BaseSHA.
func (s *Snapshots) TakeSnapshot(context.Context, git.TakeSnapshotParams) (*git.Snapshot, error) {
	return &git.Snapshot{
		SHA:     s.SHA,
		BaseSHA: s.BaseSHA,
	}, nil
}

// HoldRef does nothing.
func (*Snapshots) HoldRef(context.Context, string, string, string) error {
	return nil
}

// ReleaseRef records the ref.
func (s *Snapshots) ReleaseRef(_ context.Context, _, ref string) error {
	s.Released = append(s.Released, ref)

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

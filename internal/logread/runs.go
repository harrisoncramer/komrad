package logread

import (
	"context"
	"path/filepath"

	"komrad/config"
	"komrad/internal/fail"
)

// ErrNoSuchRun says the agent recorded no run by the name asked for.
var ErrNoSuchRun = fail.NotFound("the agent recorded no run by that name")

// Run names one script run an agent recorded, by the file it wrote.
type Run struct {
	AgentID string
	Name    string
}

// Runs reads the script runs agents have recorded under one directory.
type Runs struct {
	dir string
}

// NewRuns builds a reader over the runs recorded under dir.
func NewRuns(dir string) *Runs {
	return &Runs{dir: dir}
}

// List is the runs the agent has recorded, newest first.
func (r *Runs) List(agentID string) []config.RunLog {
	return config.ListRunLogs(r.dir, agentID)
}

// Newest is the last limit lines of the run, oldest first.
func (r *Runs) Newest(ctx context.Context, run Run, limit int) (*Window, error) {
	reader, err := r.reader(run)
	if err != nil {
		return nil, err
	}

	return reader.Newest(ctx, limit)
}

// Since is the lines the run wrote after offset, oldest first.
func (r *Runs) Since(ctx context.Context, run Run, offset int64, limit int) (*Window, error) {
	reader, err := r.reader(run)
	if err != nil {
		return nil, err
	}

	return reader.Since(ctx, offset, limit)
}

// reader finds the run among those listed rather than joining its name to a path, so no name
// reaches outside the agent's directory.
func (r *Runs) reader(run Run) (*Reader, error) {
	for _, listed := range r.List(run.AgentID) {
		if filepath.Base(listed.Path) == run.Name {
			return New(listed.Path), nil
		}
	}

	return nil, ErrNoSuchRun.Saying("agent %s recorded no run named %q", run.AgentID, run.Name)
}

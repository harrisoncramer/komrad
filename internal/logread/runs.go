package logread

import (
	"context"
	"path/filepath"

	"komrad/config"
	"komrad/internal/fail"

	"github.com/google/uuid"
)

// ErrNoSuchRun says the agent recorded no run by the name asked for.
var ErrNoSuchRun = fail.NotFound("the agent recorded no run by that name")

// Run names one script run an agent recorded, by the file it wrote.
type Run struct {
	AgentID uuid.UUID
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
func (r *Runs) List(agentID uuid.UUID) []config.RunLog {
	return config.ListRunLogs(r.dir, agentID)
}

// Newest is the newest lines of the run that the query asks for, oldest first.
func (r *Runs) Newest(ctx context.Context, run Run, query Query) (*Window, error) {
	reader, err := r.reader(run)
	if err != nil {
		return nil, err
	}

	return reader.newest(ctx, matching(query.Pattern, everyLine), query.Limit)
}

// Since is the lines the run wrote after offset that the query asks for, oldest first.
func (r *Runs) Since(ctx context.Context, run Run, offset int64, query Query) (*Window, error) {
	reader, err := r.reader(run)
	if err != nil {
		return nil, err
	}

	return reader.since(ctx, matching(query.Pattern, everyLine), offset, query.Limit)
}

// reader is a reader over the run the agent recorded by that name.
func (r *Runs) reader(run Run) (*reader, error) {
	for _, listed := range r.List(run.AgentID) {
		if filepath.Base(listed.Path) == run.Name {
			return New(listed.Path), nil
		}
	}

	return nil, ErrNoSuchRun.Saying("agent %s recorded no run named %q", run.AgentID, run.Name)
}

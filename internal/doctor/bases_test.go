package doctor

import (
	"context"
	"errors"
	"testing"

	"komrade/internal/git"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func basesEnv(bases []WorkflowBase, readErr error, held map[string]bool) Env {
	return Env{
		Settings: Settings{DatabaseURL: writableURL},
		ReadWorkflowBases: func(context.Context, string) ([]WorkflowBase, error) {
			return bases, readErr
		},
		ResolveBase: func(_ context.Context, dir, base string) (string, error) {
			if dir == "/not/a/repo" {
				return "", errors.New("not a repository")
			}
			if !held[base] {
				return "", git.ErrBaseMissing
			}

			return "origin/" + base, nil
		},
	}
}

// TestBasesCheckNamesTheWorkflowWhoseCloneLacksItsBase is the point of this check: the row the
// migration backfilled as main on a repository that never had one is named, and nothing else is.
func TestBasesCheckNamesTheWorkflowWhoseCloneLacksItsBase(t *testing.T) {
	bases := []WorkflowBase{
		{
			Name: "backend",
			Cwd:  "/repo/backend",
			Base: "main",
		},
		{
			Name: "docs",
			Cwd:  "/repo/docs",
			Base: "trunk",
		},
		{
			Name: "notes",
			Cwd:  "/not/a/repo",
			Base: "main",
		},
		{
			Name: "plain",
			Cwd:  "",
			Base: "main",
		},
	}

	result := newBasesCheck().Run(t.Context(), basesEnv(bases, nil, map[string]bool{"main": true}))

	assert.Equal(t, StatusWarn, result.Status)
	assert.Equal(t, "workflow_base_missing", codeOf(result.Err))
	assert.Equal(t, "docs names trunk", result.Detail)
	assert.Contains(t, result.Fix, "update the workflow")
}

// TestBasesCheckPassesWhenEveryCloneHoldsItsBase covers the ordinary machine, counting only the
// workflows whose cwd is a repository.
func TestBasesCheckPassesWhenEveryCloneHoldsItsBase(t *testing.T) {
	bases := []WorkflowBase{
		{
			Name: "backend",
			Cwd:  "/repo/backend",
			Base: "main",
		},
		{
			Name: "notes",
			Cwd:  "/not/a/repo",
			Base: "main",
		},
	}

	result := newBasesCheck().Run(t.Context(), basesEnv(bases, nil, map[string]bool{"main": true}))

	require.Equal(t, StatusOK, result.Status)
	assert.Equal(t, "1 workflow checked", result.Detail)
}

// TestBasesCheckWarnsWhenTheRowsCannotBeRead covers the store refusing, which is the database
// check's failure to explain rather than this one's.
func TestBasesCheckWarnsWhenTheRowsCannotBeRead(t *testing.T) {
	result := newBasesCheck().Run(t.Context(), basesEnv(nil, errors.New("relation is missing"), nil))

	assert.Equal(t, StatusWarn, result.Status)
	assert.Equal(t, "workflow_bases_unreadable", codeOf(result.Err))
}

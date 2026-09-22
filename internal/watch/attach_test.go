package watch

import (
	"context"
	"testing"

	notifystore "weave/internal/store/notify"
	"weave/lib/xptr"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// recordingTriggers is a store that remembers what it was asked to put a trigger on.
type recordingTriggers struct {
	resources   []notifystore.Resource
	projections []notifystore.Projection
	lookups     []notifystore.Lookup
}

func (r *recordingTriggers) EnsureResourceTrigger(_ context.Context, resource notifystore.Resource) error {
	r.resources = append(r.resources, resource)

	return nil
}

func (r *recordingTriggers) EnsureProjectionTrigger(_ context.Context, projection notifystore.Projection) error {
	r.projections = append(r.projections, projection)

	return nil
}

func (r *recordingTriggers) EnsureLookupTrigger(_ context.Context, lookup notifystore.Lookup) error {
	r.lookups = append(r.lookups, lookup)

	return nil
}

// TestEveryTableTheWatchReadsGetsItsTrigger spells out the triggers starting the watch puts on,
// so a resource or a projection leaving the list is a failure here rather than a row that
// quietly stops updating.
func TestEveryTableTheWatchReadsGetsItsTrigger(t *testing.T) {
	t.Parallel()

	recorded := &recordingTriggers{}
	require.NoError(t, ensureTriggers(t.Context(), recorded))

	assert.Equal(t, []notifystore.Resource{
		{Table: "agents", Key: "id"},
		{Table: "workflows", Key: "id"},
		{Table: "schedules", Key: "id"},
		{Table: "marks", Key: "mark"},
		{Table: "mail", Key: "id"},
		{Table: "pull_requests", Key: "id"},
	}, recorded.resources)

	assert.Equal(t, []notifystore.Projection{
		{Table: "agent_metadata", Parent: "agents", Column: "agent_id"},
		{Table: "agent_runs", Parent: "agents", Column: "agent_id"},
		{Table: "reviews", Parent: "agents", Column: "agent_id"},
		{Table: "review_rounds", Parent: "agents", Column: "agent_id"},
		{Table: "workflow_hooks", Parent: "workflows", Column: "workflow_id"},
	}, recorded.projections)

	assert.Equal(t, []notifystore.Lookup{
		{
			Table:       "pull_request_syncs",
			Parent:      "agents",
			ParentKey:   "id",
			ParentMatch: "repository_url",
			Match:       "repository_url",
			Where:       xptr.Ptr("archived_at IS NULL"),
		},
		{
			Table:       "workflow_configs",
			Parent:      "workflows",
			ParentKey:   "id",
			ParentMatch: "workflow_config_id",
			Match:       "id",
		},
		{
			Table:       "workflow_provisioner_configs",
			Parent:      "workflows",
			ParentKey:   "id",
			ParentMatch: "provisioner_config_id",
			Match:       "id",
		},
	}, recorded.lookups)
}

// TestEnsureTriggersStopsAtTheFirstFailure pins that a table the trigger will not go on fails
// the start rather than leaving the rest half attached.
func TestEnsureTriggersStopsAtTheFirstFailure(t *testing.T) {
	t.Parallel()

	require.Error(t, ensureTriggers(t.Context(), refusingTriggers{}))
}

type refusingTriggers struct{}

func (refusingTriggers) EnsureResourceTrigger(context.Context, notifystore.Resource) error {
	return assert.AnError
}

func (refusingTriggers) EnsureProjectionTrigger(context.Context, notifystore.Projection) error {
	return assert.AnError
}

func (refusingTriggers) EnsureLookupTrigger(context.Context, notifystore.Lookup) error {
	return assert.AnError
}

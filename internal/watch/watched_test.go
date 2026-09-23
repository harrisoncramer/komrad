package watch_test

import (
	"testing"

	"weave/internal/watch"
	"weave/internal/watched"
	"weave/lib/xslice"
	watchv1 "weave/proto/gen/watch/v1"

	"github.com/stretchr/testify/assert"
)

// TestLoadersAreTheWatchedResources pins one loader per declared resource and nothing else.
func TestLoadersAreTheWatchedResources(t *testing.T) {
	want := xslice.Map(watched.All(), func(resource watch.Resource) watchv1.Resource { return resource.Enum })
	assert.ElementsMatch(t, want, watch.ListLoadedResources(watch.NewEmptyRows()))
}

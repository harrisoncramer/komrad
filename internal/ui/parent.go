package ui

import (
	"errors"
	"fmt"
	"strings"

	"komrad/config"
	"komrad/internal/ui/api"
	"komrad/internal/ui/form"
	"komrad/internal/ui/panel"
	"komrad/internal/ui/screen"
	taskv1 "komrad/proto/gen/task/v1"

	"charm.land/huh/v2"
)

var errNotOrphaned = errors.New("komrad still knows where this task's own commits begin")

type pickForkPointValues struct {
	Commit string
}

// pickForkPointRefusal returns the reason a task refuses a new fork point, or nil when it allows one.
func pickForkPointRefusal(l panel.Listing, task panel.Task) error {
	if err := rebaseOntoBaseRefusal(l, task); err != nil {
		return err
	}
	if task.Parent.GetBlocked() != taskv1.CascadeBlock_CASCADE_BLOCK_ORPHANED {
		return errNotOrphaned
	}

	return nil
}

// cascadeBlockSaid is what blocks a child's cascade in words, empty while nothing blocks it.
func cascadeBlockSaid(block taskv1.CascadeBlock, parent string) string {
	switch block {
	case taskv1.CascadeBlock_CASCADE_BLOCK_CONFLICTED:
		return "behind " + parent + ", conflicts"
	case taskv1.CascadeBlock_CASCADE_BLOCK_ORPHANED:
		return "orphaned from " + parent
	case taskv1.CascadeBlock_CASCADE_BLOCK_PARENT_CLOSED:
		return "on " + parent + ", closed unmerged"
	case taskv1.CascadeBlock_CASCADE_BLOCK_UNSPECIFIED:
	}

	return ""
}

// openPickForkPoint opens the form that picks an orphaned child's fork point.
func (a *agentsScreen) openPickForkPoint(m *model, task panel.Task) screen.Request {
	values := &pickForkPointValues{}
	parent := m.listing.Tasks[task.Parent.GetTaskId()].Slug

	return form.Open{Form: form.Form{
		Build: func(form.Inputs) (*huh.Form, []string) {
			return pickForkPointForm(fixedKeys(), values, task.Slug, parent)
		},
		Submit: func(form.Inputs) screen.Request {
			commit := strings.TrimSpace(values.Commit)

			return a.deps.clients.Write(
				"picking the fork point of "+task.Slug,
				fmt.Sprintf("Could not pick the fork point of task %q on %s", task.Slug, parent),
				func(a api.Clients) error { return a.PickForkPoint(task.ID, commit) },
			)
		},
	}}
}

func pickForkPointForm(keys config.FixedKeys, values *pickForkPointValues, slug, parent string) (*huh.Form, []string) {
	return form.Of(keys,
		huh.NewInput().Key("commit").Title(fmt.Sprintf("Fork point of %q on %s", slug, parent)).
			Description(fmt.Sprintf("the newest commit of %s the branch still holds, where its own commits begin", parent)).
			Validate(checkForkPoint).Value(&values.Commit),
	)
}

func checkForkPoint(commit string) error {
	if strings.TrimSpace(commit) == "" {
		return errors.New("name a commit")
	}

	return nil
}

package ui

import (
	"errors"
	"fmt"
	"strings"

	"komrad/config"
	"komrad/internal/ui/panel"

	tea "charm.land/bubbletea/v2"
	"charm.land/huh/v2"
)

var errStandNotLost = errors.New("komrad still knows where this task's own commits begin")

type placeStandValues struct {
	Commit string
}

// placeStandRefusal returns the reason a task refuses a new place on the task it stands on, or nil when it allows one.
func placeStandRefusal(l listing, task panel.Task) error {
	if err := rebaseOntoBaseRefusal(l, task); err != nil {
		return err
	}
	if task.Stand.GetBlocked() != "lost" {
		return errStandNotLost
	}

	return nil
}

// openPlaceStand opens the form that names the commit a lost task's own commits sit on.
func openPlaceStand(m *model, task panel.Task) tea.Cmd {
	values := &placeStandValues{}
	parent := m.listing.tasks[task.Stand.GetTaskId()].Slug

	return m.openForm(rebuildableForm(rebuildableFormParams{
		Kind:  formTask,
		Build: func(m *model) (*huh.Form, []string) { return placeStandForm(fixedKeys(), values, task.Slug, parent) },
		Submit: holdSubmit(func(m *model) tea.Cmd {
			commit := strings.TrimSpace(values.Commit)

			return m.writeCmd(
				"placing "+task.Slug,
				fmt.Sprintf("Could not place task %q on %s", task.Slug, parent),
				func(a api) error { return a.placeStand(task.ID, commit) },
			)
		}),
	}))
}

func placeStandForm(keys config.FixedKeys, values *placeStandValues, slug, parent string) (*huh.Form, []string) {
	return formOf(keys,
		huh.NewInput().Key("commit").Title(fmt.Sprintf("Where %q stands on %s", slug, parent)).
			Description(fmt.Sprintf("the newest commit of %s the branch still holds, where its own commits begin", parent)).
			Validate(checkStandCommit).Value(&values.Commit),
	)
}

func checkStandCommit(commit string) error {
	if strings.TrimSpace(commit) == "" {
		return errors.New("name a commit")
	}

	return nil
}

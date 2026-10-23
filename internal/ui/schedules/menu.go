package schedules

import (
	"komrad/internal/ui/menu"
	"komrad/internal/ui/panel"
	"komrad/internal/ui/screen"
)

// BuildItemMenu builds the band on the hovered schedule.
func (w *Screen) BuildItemMenu(in screen.Inputs, _ menu.NestedMenuBuilder) (menu.Menu, bool) {
	item, ok := w.selected(in)
	if !ok {
		return menu.Menu{}, false
	}

	return menu.WithNested(w.scheduleMenu(item.schedule), "archive", w.deps.Keys.Menu.Archive, w.archiveMenu(item.schedule)), true
}

// BuildScreenMenu builds the schedules screen's own band, for writing a new schedule.
func (w *Screen) BuildScreenMenu(_ screen.Inputs, _ menu.NestedMenuBuilder) (menu.Menu, bool) {
	return menu.Menu{
		Title: "schedules",
		Entries: []menu.Entry{
			menu.Leaving("new schedule", w.deps.Keys.ScheduleMenu.New, w.newScheduleForm),
		},
	}, true
}

// scheduleMenu is the band on the hovered schedule.
func (w *Screen) scheduleMenu(schedule panel.Schedule) menu.Menu {
	toggle := "pause"
	if !schedule.Enabled {
		toggle = "resume"
	}

	return menu.Menu{
		Title: schedule.Name,
		Entries: []menu.Entry{
			menu.Leaving("edit", w.deps.Keys.ScheduleMenu.Edit, func(screen.Inputs) screen.Request { return w.editScheduleForm(schedule) }),
			menu.Holding(toggle, w.deps.Keys.ScheduleMenu.Toggle, func(screen.Inputs) screen.Request { return w.toggleSchedule(schedule) }),
		},
	}
}

func (w *Screen) archiveMenu(schedule panel.Schedule) menu.Menu {
	return menu.Menu{
		Title: "archive",
		Entries: []menu.Entry{{
			Label:     "archive",
			Keys:      w.deps.Keys.ArchiveMenu.Archive,
			AfterPick: menu.Hold,
			Pick:      func(screen.Inputs) screen.Request { return w.archiveSchedule(schedule) },
		}},
	}
}

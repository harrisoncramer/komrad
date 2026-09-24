package ui

import (
	"weave/config"
	"weave/internal/ui/layout"
	agentv1 "weave/proto/gen/agent/v1"
	watchv1 "weave/proto/gen/watch/v1"
	workflowv1 "weave/proto/gen/workflow/v1"
)

func watchedPanel() model {
	return model{
		api: api{
			agents:    emptyLists{},
			workflows: emptyLists{},
			schedules: emptyLists{},
		},
		frame:    layout.New(&recordingTmux{}, config.DefaultLayout()),
		keys:     config.DefaultKeybindings(),
		screen:   screenAgents,
		reactors: newWorldReactors(),
	}
}

func agentRowOf(agent *agentv1.Agent) *watchv1.Row {
	return &watchv1.Row{Row: &watchv1.Row_Agent{Agent: agent}}
}

func workflowRowOf(workflow *workflowv1.Workflow) *watchv1.Row {
	return &watchv1.Row{Row: &watchv1.Row_Workflow{Workflow: workflow}}
}

func putEvent(row *watchv1.Row) *watchv1.WatchEvent {
	return &watchv1.WatchEvent{Event: &watchv1.WatchEvent_Changes{Changes: &watchv1.Changes{Puts: []*watchv1.Row{row}}}}
}

func snapshotEvent(rows ...*watchv1.Row) *watchv1.WatchEvent {
	return &watchv1.WatchEvent{Event: &watchv1.WatchEvent_Snapshot{Snapshot: &watchv1.Snapshot{Rows: rows}}}
}

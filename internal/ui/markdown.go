package ui

import (
	"fmt"
	"strings"

	"weave/lib/dates"

	agentstore "weave/internal/store/agent"
	schedulestore "weave/internal/store/schedule"
	workflowstore "weave/internal/store/workflow"
)

// The stage shows a resource with no pane of its own as rendered markdown. These build
// those documents, and nothing here reads the model: a document is a function of the
// record it describes.

func orDash(v string) string {
	if strings.TrimSpace(v) == "" {
		return "—"
	}

	return v
}

// recordField is one line of a record's numbered fields: a label and the value a digit
// key copies to the clipboard.
type recordField struct {
	label string
	value string
}

// record is a screen's rendered document alongside the values its numbered fields
// copy, in the order they are numbered, so a digit key can act on whichever one it
// names without re-parsing the markdown it produced.
type record struct {
	markdown string
	values   []string
}

// renderRecord builds a record: a title, an optional one-line notice under it, the
// numbered fields in order, then free-form prose such as a workflow's context or a
// schedule's prompt, then a closing footer note. The fields come first because they
// are what a digit key copies and what most presses of the info key are after, while
// the prose beneath them can run long.
func renderRecord(title, notice string, fields []recordField, prose, footer string) record {
	var b strings.Builder
	fmt.Fprintf(&b, "# %s\n\n", title)
	if notice != "" {
		b.WriteString(notice + "\n\n")
	}
	b.WriteString("---\n\n")

	values := make([]string, 0, len(fields))
	for i, f := range fields {
		fmt.Fprintf(&b, "%d. %s: %s\n", i+1, f.label, f.value)
		values = append(values, f.value)
	}

	if prose != "" {
		b.WriteString("\n" + prose + "\n")
	}
	if footer != "" {
		b.WriteString("\n" + footer + "\n")
	}

	return record{markdown: b.String(), values: values}
}

// workflowFields are a workflow's own numbered fields: how many agents it holds, then
// the defaults future agents under it inherit.
func workflowFields(w workflowstore.Workflow, agentCount int) []recordField {
	return append([]recordField{{"agents", fmt.Sprintf("%d", agentCount)}}, workflowConfigFields(w)...)
}

// workflowConfigFields are the defaults a workflow hands to the agents it starts,
// shared between a workflow's own record and a schedule's, which fires into the same
// workflow.
func workflowConfigFields(w workflowstore.Workflow) []recordField {
	return []recordField{
		{"default agent", orDash(w.Harness)},
		{"directory", orDash(w.Cwd)},
		{"flags", orDash(w.Flags)},
		{"setup", orDash(w.SetupAction)},
		{"cleanup", orDash(w.CleanupAction)},
	}
}

// workflowMarkdown renders a workflow's name, its numbered fields, and its shared
// context for the stage.
func workflowMarkdown(w workflowstore.Workflow, agentCount int) record {
	return renderRecord(w.Name, "", workflowFields(w, agentCount), workflowProse(w), "")
}

// workflowContextMarkdown is a workflow's shared context alone, which is what the stage
// holds while the fields beside it are drawn in the sidebar. The context is the one thing
// about a workflow that runs long enough to want a pane of its own.
func workflowContextMarkdown(w workflowstore.Workflow) string {
	return "# " + w.Name + "\n\n" + workflowProse(w) + "\n"
}

// workflowProse is the context an agent started here inherits, or a note that none is set.
func workflowProse(w workflowstore.Workflow) string {
	if strings.TrimSpace(w.Context) == "" {
		return "_no shared context set_"
	}

	return w.Context
}

// agentFields are an agent's own numbered fields, in the order they were launched
// with, plus whatever it has since recorded about its own work under metadata. The
// metadata values go in bare, since they are mostly URLs and markdown link syntax
// would hide the target this section exists to show.
func agentFields(agent agentstore.Agent, schedule string) []recordField {
	fields := []recordField{
		{"agent", orDash(agent.Harness)},
		{"directory", orDash(agent.Cwd)},
		{"flags", orDash(agent.Flags)},
		// Both ids are shown because both get typed into something: the agent id is
		// what the panel and its tools call this agent, and the session id is what
		// its CLI calls the conversation, which is what a resume by hand takes.
		{"agent_id", orDash(agent.ID)},
		{"session_id", orDash(agent.SessionID)},
	}
	if schedule != "" {
		fields = append(fields, recordField{"launched by", schedule})
	}
	if created := dates.Friendly(agent.CreatedAt); created != "" {
		fields = append(fields, recordField{"created", created})
	}
	if agent.Tag != nil {
		fields = append(fields, recordField{"tag", *agent.Tag})
	}
	for _, entry := range agent.Metadata {
		fields = append(fields, recordField{entry.Key, entry.Value})
	}

	return fields
}

// actionMarkdown renders an action's record for the stage: its name and what its own header
// declares about it. The file's text is not shown, since an action is as readily a compiled
// program as a script.
func actionMarkdown(action actionItem) string {
	var b strings.Builder
	fmt.Fprintf(&b, "# %s\n\n", action.name)
	if !action.executable {
		b.WriteString("_not executable — chmod +x to run it_\n\n")
	}
	if action.description != "" {
		b.WriteString(action.description + "\n\n")
	} else {
		b.WriteString("_no description declared_\n\n")
	}
	b.WriteString("---\n\n")
	if action.timeout > 0 {
		fmt.Fprintf(&b, "- timeout: %s\n", action.timeout)
	} else {
		b.WriteString("- timeout: — (none declared)\n")
	}

	// The run goes ahead without the lines that did not read, so a timeout spelled wrong is
	// shown rather than left as a limit nothing applies.
	if len(action.problems) > 0 {
		b.WriteString("\n_some of its header was not understood_\n\n")
		for _, problem := range action.problems {
			fmt.Fprintf(&b, "- %s\n", problem)
		}
	}

	return b.String()
}

// noSchedulesMarkdown is what the stage shows with an empty list, so the pane says what
// the screen is for rather than sitting blank.
const noSchedulesMarkdown = "# Schedules\n\n" +
	"A schedule launches an agent on a cron expression.\n\n" +
	"Press the new key to write one.\n"

// scheduleFields are a schedule's own numbered fields, its own settings first and then
// whatever the workflow it fires into hands each run.
func scheduleFields(sched schedulestore.Schedule, workflow *workflowstore.Workflow) []recordField {
	fields := []recordField{
		{"cron", orDash(sched.Cron)},
		{"timezone", orDash(sched.Timezone)},
	}

	if workflow == nil {
		return append(fields, recordField{"workflow", "— (archived)"})
	}

	fields = append(fields, recordField{"workflow", workflow.Name})

	return append(fields, workflowConfigFields(*workflow)...)
}

// scheduleMarkdown renders a schedule's record for the stage.
func scheduleMarkdown(item scheduleItem, workflow *workflowstore.Workflow) record {
	sched := item.schedule

	var notice string
	if sched.Enabled {
		notice = fmt.Sprintf("_next run %s_", orDash(dates.Friendly(item.nextRun)))
	} else {
		notice = "_paused — will not run_"
	}

	prose := "_no prompt set_"
	if strings.TrimSpace(sched.Prompt) != "" {
		prose = sched.Prompt
	}

	var footer string
	if sched.HideAgents {
		footer = "Its runs start hidden."
	}

	return renderRecord(sched.Name, notice, scheduleFields(sched, workflow), prose, footer)
}

// emptyArchiveMarkdown is what the stage shows with nothing in the archive, so the pane
// says why it is blank.
const emptyArchiveMarkdown = "# Archive\n\n_nothing archived_\n\nAgents archived from the panel are kept here until you delete them.\n"

// archiveMarkdown renders an archived agent's stored record for the stage: what the agents
// list shows, minus the liveness, plus what the two keys here would do to it.
func archiveMarkdown(agent agentstore.Agent, schedule string) record {
	fields := []recordField{
		{"agent", orDash(agent.Harness)},
		{"directory", orDash(agent.Cwd)},
		{"flags", orDash(agent.Flags)},
		{"agent_id", orDash(agent.ID)},
		{"session_id", orDash(agent.SessionID)},
	}
	if schedule != "" {
		fields = append(fields, recordField{"launched by", schedule})
	}
	if created := dates.Friendly(agent.CreatedAt); created != "" {
		fields = append(fields, recordField{"created", created})
	}
	if agent.ArchivedAt != nil {
		if archived := dates.Friendly(*agent.ArchivedAt); archived != "" {
			fields = append(fields, recordField{"archived", archived})
		}
	}

	return renderRecord(agent.Name, "_archived — kept until deleted_", fields, "",
		"Restore brings it back and picks the conversation up where it stopped.")
}

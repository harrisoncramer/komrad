package ui

import (
	"fmt"
	"strings"

	"weave/lib/dates"

	agentstore "weave/internal/store/agent"
	schedulestore "weave/internal/store/schedule"
	workflowstore "weave/internal/store/workflow"
)

// What a resource is made of, in two halves. The fields are label and value pairs, which the
// details under the list draw and copy from, and the documents are the prose the stage pages
// beside them. Nothing here reads the model: both are a function of the record they describe.

func orDash(v string) string {
	if strings.TrimSpace(v) == "" {
		return "—"
	}

	return v
}

// recordField is one line of the details under the list: a label and the value the copy
// key takes.
type recordField struct {
	label string
	value string
}

// renderDocument builds what the stage pages: a title, an optional one-line notice under it,
// free-form prose such as a workflow's context or a schedule's prompt, then a closing footer
// note. The fields a resource carries are not in here; they are drawn beside this, in the
// details under the list.
func renderDocument(title, notice, prose, footer string) string {
	var b strings.Builder
	fmt.Fprintf(&b, "# %s\n\n", title)
	if notice != "" {
		b.WriteString(notice + "\n\n")
	}
	if prose != "" {
		b.WriteString(prose + "\n")
	}
	if footer != "" {
		b.WriteString("\n" + footer + "\n")
	}

	return b.String()
}

// workflowFields are a workflow's own fields: how many agents it holds, then the defaults
// future agents under it inherit.
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

// workflowContextMarkdown is a workflow's shared context, which is what the stage holds
// while the fields beside it are drawn in the details. The context is the one thing about a
// workflow that runs long enough to want a pane of its own.
func workflowContextMarkdown(w workflowstore.Workflow) string {
	return renderDocument(w.Name, "", workflowProse(w), "")
}

// workflowProse is the context an agent started here inherits, or a note that none is set.
func workflowProse(w workflowstore.Workflow) string {
	if strings.TrimSpace(w.Context) == "" {
		return "_no shared context set_"
	}

	return w.Context
}

// agentFields are an agent's own fields, in the order they were launched
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

// scheduleFields are a schedule's own fields, its own settings first and then
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

// scheduleMarkdown renders the prompt a schedule fires with for the stage, under when it
// next runs. The prompt is the part of a schedule that runs long, which is why it is here
// rather than in the details beside it.
func scheduleMarkdown(item scheduleItem) string {
	sched := item.schedule

	notice := "_paused — will not run_"
	if sched.Enabled {
		notice = fmt.Sprintf("_next run %s_", orDash(dates.Friendly(item.nextRun)))
	}

	prose := "_no prompt set_"
	if strings.TrimSpace(sched.Prompt) != "" {
		prose = sched.Prompt
	}

	var footer string
	if sched.HideAgents {
		footer = "Its runs start hidden."
	}

	return renderDocument(sched.Name, notice, prose, footer)
}

// emptyArchiveMarkdown is what the stage shows with nothing in the archive, so the pane
// says why it is blank.
const emptyArchiveMarkdown = "# Archive\n\n_nothing archived_\n\nAgents archived from the panel are kept here until you delete them.\n"

// archiveMarkdown says why an archived agent's pane is empty and what the keys here would do
// to it. What it was launched with is in the details beside this.
func archiveMarkdown(agent agentstore.Agent) string {
	return renderDocument(agent.Name, "_archived — kept until deleted_", "",
		"Restore brings it back and picks the conversation up where it stopped.")
}

// archiveFields are an archived agent's own fields: what the agents list shows for a live
// one, plus when it was put away.
func archiveFields(agent agentstore.Agent, schedule string) []recordField {
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

	return fields
}

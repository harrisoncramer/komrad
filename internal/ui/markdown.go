package ui

import (
	"fmt"
	"strings"

	"claude-agents/lib/dates"

	agentstore "claude-agents/internal/store/agent"
	workflowstore "claude-agents/internal/store/workflow"
)

// The stage shows a resource that has no pane of its own as rendered markdown, so
// every screen has a document for whatever it is hovering. These build them, and
// nothing here reads the model: a document is a function of the record it describes.

func orDash(v string) string {
	if strings.TrimSpace(v) == "" {
		return "—"
	}

	return v
}

// workflowMarkdown renders a workflow's name, shared context, and config for the
// stage.
func workflowMarkdown(w workflowstore.Workflow, agents int) string {
	var b strings.Builder
	fmt.Fprintf(&b, "# %s\n\n", w.Name)
	if strings.TrimSpace(w.Context) != "" {
		b.WriteString(w.Context + "\n\n")
	} else {
		b.WriteString("_no shared context set_\n\n")
	}
	b.WriteString("---\n\n")
	fmt.Fprintf(&b, "- agents: %d\n", agents)
	fmt.Fprintf(&b, "- directory: %s\n", orDash(w.Cwd))
	fmt.Fprintf(&b, "- flags: %s\n", orDash(w.Flags))

	return b.String()
}

// agentMarkdown renders an agent's stored record for the stage. A dormant agent has
// no live pane to show, so this is all there is of it. A live one shows the same
// record on demand, since its running pane occupies the stage and hides the cwd and
// flags it was launched with. The values are the record as stored at launch rather
// than what the running agent has done since.
func agentMarkdown(agent agentstore.Agent, alive bool, schedule string) string {
	var b strings.Builder
	fmt.Fprintf(&b, "# %s\n\n", agent.Name)
	if !alive {
		b.WriteString("_dormant — not running_\n\n")
	}
	b.WriteString("---\n\n")
	fmt.Fprintf(&b, "- agent: %s\n", orDash(agent.Harness))
	fmt.Fprintf(&b, "- directory: %s\n", orDash(agent.Cwd))
	fmt.Fprintf(&b, "- flags: %s\n", orDash(agent.Flags))
	fmt.Fprintf(&b, "- resume: %s\n", orDash(agent.SessionID))
	if schedule != "" {
		fmt.Fprintf(&b, "- launched by: %s\n", schedule)
	}
	if created := dates.Friendly(agent.CreatedAt); created != "" {
		fmt.Fprintf(&b, "- created: %s\n", created)
	}
	if agent.Mark != nil {
		fmt.Fprintf(&b, "- mark: %s\n", *agent.Mark)
	}
	if agent.Hidden {
		b.WriteString("- hidden from the list\n")
	}
	// What the agent recorded about its own work, which is the one part of this record
	// it wrote rather than took at launch. The values go in bare: they are mostly URLs,
	// terminals linkify those on their own, and markdown link syntax would hide the
	// target this section exists to show. An agent that recorded nothing draws no
	// heading at all rather than an empty one.
	if len(agent.Metadata) > 0 {
		b.WriteString("\n## metadata\n\n")
		for _, entry := range agent.Metadata {
			fmt.Fprintf(&b, "- %s: %s\n", entry.Key, entry.Value)
		}
	}
	if !alive {
		b.WriteString("\nPress open to restore.\n")
	}

	return b.String()
}

// noSchedulesMarkdown is what the stage shows with an empty list, so the pane
// beside it says what the screen is for rather than sitting blank.
const noSchedulesMarkdown = "# Schedules\n\n" +
	"A schedule launches an agent on a cron expression.\n\n" +
	"Press the new key to write one.\n"

// scheduleMarkdown renders a schedule's record for the stage.
func scheduleMarkdown(item scheduleItem, workflow *workflowstore.Workflow) string {
	sched := item.schedule

	var b strings.Builder
	fmt.Fprintf(&b, "# %s\n\n", sched.Name)
	if sched.Enabled {
		fmt.Fprintf(&b, "_next run %s_\n\n", orDash(dates.Friendly(item.nextRun)))
	} else {
		b.WriteString("_paused — will not run_\n\n")
	}

	if strings.TrimSpace(sched.Prompt) != "" {
		b.WriteString(sched.Prompt + "\n\n")
	} else {
		b.WriteString("_no prompt set_\n\n")
	}

	b.WriteString("---\n\n")
	fmt.Fprintf(&b, "- cron: %s\n", orDash(sched.Cron))
	fmt.Fprintf(&b, "- timezone: %s\n", orDash(sched.Timezone))
	if sched.HideAgents {
		b.WriteString("- its runs start hidden\n")
	}

	// The directory and flags belong to the workflow and are read as the schedule
	// fires, so they are shown under the workflow that supplies them rather than as
	// settings of the schedule itself.
	if workflow == nil {
		b.WriteString("- workflow: — (deleted)\n")

		return b.String()
	}

	fmt.Fprintf(&b, "- workflow: %s\n", workflow.Name)
	fmt.Fprintf(&b, "- directory: %s\n", orDash(workflow.Cwd))
	fmt.Fprintf(&b, "- flags: %s\n", orDash(workflow.Flags))

	return b.String()
}

// emptyTrashMarkdown is what the stage shows with nothing in the trash, so the
// pane says why it is blank rather than sitting empty.
const emptyTrashMarkdown = "# Trash\n\n_nothing deleted_\n\nAgents deleted from the panel are kept here until you destroy them.\n"

// trashMarkdown renders a deleted agent's stored record for the stage. It is the
// same record the agents list shows, minus the liveness, plus what the two keys
// here would do to it.
func trashMarkdown(agent agentstore.Agent, schedule string) string {
	var b strings.Builder
	fmt.Fprintf(&b, "# %s\n\n", agent.Name)
	b.WriteString("_deleted — kept until destroyed_\n\n")
	b.WriteString("---\n\n")
	fmt.Fprintf(&b, "- agent: %s\n", orDash(agent.Harness))
	fmt.Fprintf(&b, "- directory: %s\n", orDash(agent.Cwd))
	fmt.Fprintf(&b, "- flags: %s\n", orDash(agent.Flags))
	fmt.Fprintf(&b, "- resume: %s\n", orDash(agent.SessionID))
	if schedule != "" {
		fmt.Fprintf(&b, "- launched by: %s\n", schedule)
	}
	if created := dates.Friendly(agent.CreatedAt); created != "" {
		fmt.Fprintf(&b, "- created: %s\n", created)
	}
	b.WriteString("\nRestore brings it back and picks the conversation up where it stopped.\n")

	return b.String()
}

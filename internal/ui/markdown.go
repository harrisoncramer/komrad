package ui

import (
	"bytes"
	"fmt"
	"strings"

	"weave/lib/dates"

	agentstore "weave/internal/store/agent"
	workflowstore "weave/internal/store/workflow"
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

// textual reports whether bytes are text the stage can show. A NUL byte is what tells them
// apart, which is how less itself decides, and nothing worth reading in the stage carries one.
func textual(text []byte) bool {
	return bytes.IndexByte(text, 0) < 0
}

// fencedBlock is a block of text somebody else wrote, ready to sit in a stage document. The
// renderer indents a fenced block whole and leaves what is inside it alone, so the text is shown
// as it was written rather than read as markdown and reflowed to the pane.
//
// The fence is three backticks, or one longer than the longest run inside the text. Text
// carrying a fence of its own would otherwise close the block early and leave the rest of itself
// rendering as prose.
func fencedBlock(text []byte) string {
	if !textual(text) {
		return "_not text, so there is nothing to show_\n"
	}

	body := strings.TrimRight(string(text), "\n")
	if body == "" {
		return "_empty_\n"
	}

	fence := strings.Repeat("`", max(3, longestBacktickRun(body)+1))

	return fmt.Sprintf("%s\n%s\n%s\n", fence, body, fence)
}

// longestBacktickRun is the longest unbroken run of backticks in a string, which is what a fence
// around it has to beat.
func longestBacktickRun(body string) int {
	longest, run := 0, 0
	for _, char := range body {
		if char != '`' {
			run = 0

			continue
		}

		run++
		longest = max(longest, run)
	}

	return longest
}

// actionMarkdown renders an action for the stage, its name over the start of its own text as the
// service read it. An action is as readily a compiled program as a script, so the body goes
// through fencedBlock, which shows a script as the code it is and says so rather than spilling a
// binary into the pane.
func actionMarkdown(action actionItem) string {
	var b strings.Builder
	fmt.Fprintf(&b, "# %s\n\n", action.name)
	if !action.executable {
		b.WriteString("_not executable — chmod +x to run it_\n\n")
	}

	b.WriteString(fencedBlock(action.preview))
	if action.truncated && textual(action.preview) {
		b.WriteString("\n_only the start of the file is shown_\n")
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

// emptyArchiveMarkdown is what the stage shows with nothing in the archive, so the
// pane says why it is blank rather than sitting empty.
const emptyArchiveMarkdown = "# Archive\n\n_nothing archived_\n\nAgents archived from the panel are kept here until you delete them.\n"

// archiveMarkdown renders an archived agent's stored record for the stage. It is the
// same record the agents list shows, minus the liveness, plus what the two keys
// here would do to it.
func archiveMarkdown(agent agentstore.Agent, schedule string) string {
	var b strings.Builder
	fmt.Fprintf(&b, "# %s\n\n", agent.Name)
	b.WriteString("_archived — kept until deleted_\n\n")
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
	if agent.ArchivedAt != nil {
		if archived := dates.Friendly(*agent.ArchivedAt); archived != "" {
			fmt.Fprintf(&b, "- archived: %s\n", archived)
		}
	}
	b.WriteString("\nRestore brings it back and picks the conversation up where it stopped.\n")

	return b.String()
}

package ui

import (
	"bytes"
	"fmt"
	"strings"

	"weave/lib/dates"

	agentstore "weave/internal/store/agent"
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

// workflowMarkdown renders a workflow's name, shared context, and config for the stage.
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

// agentMarkdown renders an agent's stored record for the stage, which is all there is of a
// dormant agent and what a live one shows on demand, since its pane otherwise hides the
// cwd and flags it was launched with. The values are as stored at launch.
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
	// Both ids are shown because both get typed into something: the agent id is what the
	// panel and its tools call this agent, and the session id is what its CLI calls the
	// conversation, which is what a resume by hand takes.
	fmt.Fprintf(&b, "- agent_id: %s\n", orDash(agent.ID))
	fmt.Fprintf(&b, "- session_id: %s\n", orDash(agent.SessionID))
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
	// What the agent recorded about its own work, the one part of this record it wrote
	// rather than took at launch. The values go in bare, since they are mostly URLs and
	// markdown link syntax would hide the target this section exists to show.
	if len(agent.Metadata) > 0 {
		b.WriteString("\n## metadata\n\n")
		for _, entry := range agent.Metadata {
			fmt.Fprintf(&b, "- %s: %s\n", entry.Key, entry.Value)
		}
	}
	if !alive {
		b.WriteString("\nPress open to resume.\n")
	}

	return b.String()
}

// isTextual reports whether bytes are text the stage can show, deciding on a NUL byte the
// way less itself does.
func isTextual(text []byte) bool {
	return bytes.IndexByte(text, 0) < 0
}

// fencedBlock is a block of text somebody else wrote, ready to sit in a stage document.
// The renderer leaves what is inside a fence alone, so the text shows as it was written
// rather than reflowed to the pane.
//
// The fence is three backticks, or one longer than the longest run inside the text, since
// text carrying a fence of its own would otherwise close the block early.
func fencedBlock(text []byte) string {
	if !isTextual(text) {
		return "_not text, so there is nothing to show_\n"
	}

	body := strings.TrimRight(string(text), "\n")
	if body == "" {
		return "_empty_\n"
	}

	fence := strings.Repeat("`", max(3, longestBacktickRun(body)+1))

	return fmt.Sprintf("%s\n%s\n%s\n", fence, body, fence)
}

// longestBacktickRun is the longest unbroken run of backticks in a string, which a fence
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

// actionMarkdown renders an action for the stage: its name, what its header declares, and
// the start of its own text. An action is as readily a compiled program as a script, so the
// body goes through fencedBlock rather than spilling a binary into the pane.
//
// A file that declares nothing says nothing here, rather than carrying a line about each
// field it left out, since the text below is the whole answer for one of those.
func actionMarkdown(action actionItem) string {
	var b strings.Builder
	fmt.Fprintf(&b, "# %s\n\n", action.name)
	if !action.executable {
		b.WriteString("_not executable — chmod +x to run it_\n\n")
	}
	if action.description != "" {
		b.WriteString(action.description + "\n\n")
	}
	if action.timeout > 0 {
		fmt.Fprintf(&b, "_gives up after %s_\n\n", action.timeout)
	}
	// The lines weave could not read. They are shown because the run went ahead without
	// them, so a timeout that was spelled wrong is otherwise a limit the user believes they
	// set and nothing ever applies.
	if len(action.problems) > 0 {
		b.WriteString("_some of its header was not understood_\n\n")
		for _, problem := range action.problems {
			fmt.Fprintf(&b, "- %s\n", problem)
		}
		b.WriteString("\n")
	}

	b.WriteString(fencedBlock(action.preview))
	if action.truncated && isTextual(action.preview) {
		b.WriteString("\n_only the start of the file is shown_\n")
	}

	return b.String()
}

// noSchedulesMarkdown is what the stage shows with an empty list, so the pane says what
// the screen is for rather than sitting blank.
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

	// The directory and flags belong to the workflow and are read as the schedule fires,
	// so they show under the workflow rather than as settings of the schedule.
	if workflow == nil {
		b.WriteString("- workflow: — (archived)\n")

		return b.String()
	}

	fmt.Fprintf(&b, "- workflow: %s\n", workflow.Name)
	fmt.Fprintf(&b, "- directory: %s\n", orDash(workflow.Cwd))
	fmt.Fprintf(&b, "- flags: %s\n", orDash(workflow.Flags))

	return b.String()
}

// emptyArchiveMarkdown is what the stage shows with nothing in the archive, so the pane
// says why it is blank.
const emptyArchiveMarkdown = "# Archive\n\n_nothing archived_\n\nAgents archived from the panel are kept here until you delete them.\n"

// archiveMarkdown renders an archived agent's stored record for the stage: what the agents
// list shows, minus the liveness, plus what the two keys here would do to it.
func archiveMarkdown(agent agentstore.Agent, schedule string) string {
	var b strings.Builder
	fmt.Fprintf(&b, "# %s\n\n", agent.Name)
	b.WriteString("_archived — kept until deleted_\n\n")
	b.WriteString("---\n\n")
	fmt.Fprintf(&b, "- agent: %s\n", orDash(agent.Harness))
	fmt.Fprintf(&b, "- directory: %s\n", orDash(agent.Cwd))
	fmt.Fprintf(&b, "- flags: %s\n", orDash(agent.Flags))
	fmt.Fprintf(&b, "- agent_id: %s\n", orDash(agent.ID))
	fmt.Fprintf(&b, "- session_id: %s\n", orDash(agent.SessionID))
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

package panel

import (
	"cmp"
	"maps"
	"slices"
	"strings"
	"time"

	"komrad/internal/assemble"
	"komrad/internal/ui/paint/label"
	"komrad/lib/xslice"
	agentv1 "komrad/proto/gen/agent/v1"
)

// Listing is what the screens render: the resources the watch has delivered, and the live and
// archived rows assembled from them.
type Listing struct {
	World             assemble.World
	Agents            []AgentRow
	Workflows         []Workflow
	ArchivedAgents    []AgentRow
	ArchivedWorkflows []Workflow
	Tasks             Tasks
	Schedules         []Schedule
	ArchivedSchedules []Schedule
	Worktrees         []Worktree
	DocumentTypes     []*agentv1.DocumentType
	AgentsByID        map[string]AgentRow
	Reviewers         map[string][]AgentRow
	Generation        int
}

// Assemble renders the listings, live and archived, from the world as it stands now.
func (l *Listing) Assemble(now time.Time) {
	l.Generation++
	agents := agentRowsFrom(assemble.AgentRows(l.World, now))
	l.Tasks = tasksFrom(assemble.TaskRows(l.World))
	l.Worktrees = worktreesFrom(l.World.Worktrees, agents, l.Tasks)
	isArchivedAgent := func(row AgentRow) bool {
		return row.Agent.ArchivedAt != nil
	}
	l.Agents = slices.DeleteFunc(slices.Clone(agents), isArchivedAgent)
	l.ArchivedAgents = slices.DeleteFunc(agents, func(row AgentRow) bool {
		return !isArchivedAgent(row)
	})
	l.AgentsByID = xslice.KeyBy(l.Agents, func(item AgentRow) string { return item.Agent.ID })
	l.Reviewers = ReviewersByWriter(l.Agents, WritersByTask(l.Agents))

	workflows := workflowsFrom(slices.Collect(maps.Values(l.World.Workflows)))
	slices.SortFunc(workflows, func(a, b Workflow) int {
		return cmp.Or(strings.Compare(strings.ToLower(a.Name), strings.ToLower(b.Name)), strings.Compare(a.ID, b.ID))
	})
	isArchivedWorkflow := func(workflow Workflow) bool {
		return workflow.ArchivedAt != nil
	}
	l.Workflows = slices.DeleteFunc(slices.Clone(workflows), isArchivedWorkflow)
	l.ArchivedWorkflows = slices.DeleteFunc(workflows, func(workflow Workflow) bool {
		return !isArchivedWorkflow(workflow)
	})
	schedules := xslice.Map(slices.Collect(maps.Values(l.World.Schedules)), scheduleFrom)
	slices.SortFunc(schedules, func(a, b Schedule) int { return strings.Compare(b.ID, a.ID) })
	isArchivedSchedule := func(schedule Schedule) bool {
		return schedule.ArchivedAt != nil
	}
	l.Schedules = slices.DeleteFunc(slices.Clone(schedules), isArchivedSchedule)
	l.ArchivedSchedules = slices.DeleteFunc(schedules, func(schedule Schedule) bool {
		return !isArchivedSchedule(schedule)
	})
	l.DocumentTypes = documentTypesFrom(l.World.DocumentTypes)
}

// IsEmpty reports whether there is no workflow and no agent to list.
func (l Listing) IsEmpty() bool {
	return len(l.Workflows) == 0 && len(l.Agents) == 0
}

// LiveAgents is the listing as bare rows, for what counts over the agents a screen is showing
// rather than over the marks beside them.
func (l Listing) LiveAgents() []Agent {
	return xslice.Map(l.Agents, func(it AgentRow) Agent { return it.Agent })
}

// LiveAgent is the live agent with the id.
func (l Listing) LiveAgent(id string) (AgentRow, bool) {
	for _, item := range l.Agents {
		if item.Agent.ID == id {
			return item, true
		}
	}

	return AgentRow{}, false
}

// AnyAgent is the agent with the id, live or archived.
func (l Listing) AnyAgent(id string) (AgentRow, bool) {
	if item, ok := l.LiveAgent(id); ok {
		return item, true
	}

	for _, item := range l.ArchivedAgents {
		if item.Agent.ID == id {
			return item, true
		}
	}

	return AgentRow{}, false
}

// AgentName is what an agent is called in the panel, its workflow's name for an orchestrator.
func (l Listing) AgentName(agent Agent) string {
	if agent.Role == RoleOrchestrator {
		return l.WorkflowName(agent.WorkflowID)
	}

	return agent.Name
}

// AgentCount is how many live agents a workflow holds, its orchestrator left out.
func (l Listing) AgentCount(workflowID string) int {
	return xslice.Count(l.Agents, func(it AgentRow) bool {
		return it.Agent.WorkflowID == workflowID && it.Agent.Role != RoleOrchestrator
	})
}

// ArchivedCount is how many archived agents a workflow holds.
func (l Listing) ArchivedCount(workflowID string) int {
	return xslice.Count(l.ArchivedAgents, func(item AgentRow) bool { return item.Agent.WorkflowID == workflowID })
}

// AgentsOf is the agents the last listing found inside a workflow.
func (l Listing) AgentsOf(workflowID string) []Agent {
	var held []Agent
	for _, item := range l.Agents {
		if item.Agent.WorkflowID == workflowID {
			held = append(held, item.Agent)
		}
	}

	return held
}

// LiveOrchestrators is the orchestrators a schedule may mail.
func (l Listing) LiveOrchestrators() []Agent {
	var orchestrators []Agent
	for _, row := range l.Agents {
		if row.Agent.Role == RoleOrchestrator {
			orchestrators = append(orchestrators, row.Agent)
		}
	}

	return orchestrators
}

// WorkflowName is what a workflow is called, live or archived, or a dash when none has the id.
func (l Listing) WorkflowName(id string) string {
	for _, w := range slices.Concat(l.Workflows, l.ArchivedWorkflows) {
		if w.ID == id {
			return w.Name
		}
	}

	return label.Dash
}

// WorkflowFor is the agent's workflow, live or archived, and the zero workflow when none has its id.
func (l Listing) WorkflowFor(agent Agent) Workflow {
	for _, workflow := range slices.Concat(l.Workflows, l.ArchivedWorkflows) {
		if workflow.ID == agent.WorkflowID {
			return workflow
		}
	}

	return Workflow{}
}

// WorkflowDirs is the directory each workflow launches its agents in, keyed by workflow id.
func (l Listing) WorkflowDirs() map[string]string {
	dirs := make(map[string]string, len(l.Workflows))
	for _, w := range l.Workflows {
		dirs[w.ID] = w.Cwd
	}

	return dirs
}

// ScheduleName is the name of the schedule that launched the agent, empty when none did.
func (l Listing) ScheduleName(agent Agent) string {
	if agent.ScheduleID == nil {
		return ""
	}

	for _, schedule := range l.Schedules {
		if schedule.ID == *agent.ScheduleID {
			return schedule.Name
		}
	}

	return ""
}

// ScheduledWorkflows is the workflows at least one schedule launches into.
func (l Listing) ScheduledWorkflows() map[string]bool {
	scheduled := make(map[string]bool, len(l.Schedules))
	for _, schedule := range l.Schedules {
		scheduled[schedule.WorkflowID] = true
	}

	return scheduled
}

// taskAgents is the agents on a task, the archived ones for an archived task and the live ones otherwise.
func (l Listing) taskAgents(task Task) []AgentRow {
	pool := l.Agents
	if task.ArchivedAt != nil {
		pool = l.ArchivedAgents
	}

	return xslice.Filter(pool, func(item AgentRow) bool { return item.Agent.TaskID == task.ID })
}

// AgentsOnTasks is the live agents on any of the tasks, hidden ones included.
func (l Listing) AgentsOnTasks(tasks []Task) []Agent {
	ids := TaskIDs(tasks)
	onTasks := xslice.Filter(l.Agents, func(item AgentRow) bool { return slices.Contains(ids, item.Agent.TaskID) })

	return xslice.Map(onTasks, func(item AgentRow) Agent { return item.Agent })
}

// FindTaskWriter is the task's writer, when the listing holds it.
func (l Listing) FindTaskWriter(task Task) (AgentRow, bool) {
	onTask := l.taskAgents(task)
	at := slices.IndexFunc(onTask, func(item AgentRow) bool { return item.Agent.Role == RoleWriter })
	if at < 0 {
		return AgentRow{}, false
	}

	return onTask[at], true
}

// FindWriterInCheckout is the task's writer while it works in the task's checkout.
func (l Listing) FindWriterInCheckout(task Task) (AgentRow, bool) {
	writer, ok := l.FindTaskWriter(task)
	if !ok || task.Cwd == "" || writer.Agent.Cwd != task.Cwd {
		return AgentRow{}, false
	}

	return writer, true
}

// HasOwnWorktree reports whether a task sits in a worktree komrad cut for it that is still on disk.
func (l Listing) HasOwnWorktree(task Task) bool {
	return !task.IsWorktreeRemoved() && l.hasWorktreeToDelete(task)
}

// hasWorktreeToDelete reports whether komrad cut a worktree for a task that no delete has removed yet.
func (l Listing) hasWorktreeToDelete(task Task) bool {
	if task.Cwd == "" || task.WorktreeRemovalReason == WorktreeDeleted {
		return false
	}

	workflows := slices.Concat(l.Workflows, l.ArchivedWorkflows)
	index := slices.IndexFunc(workflows, func(w Workflow) bool { return w.ID == task.WorkflowID })
	if index < 0 {
		return false
	}

	return workflows[index].Worktree.Enabled
}

// HasAnyWorktreeToDelete reports whether komrad cut a worktree for any of the tasks that no delete
// has removed yet, on disk or gone.
func (l Listing) HasAnyWorktreeToDelete(tasks []Task) bool {
	return slices.ContainsFunc(tasks, l.hasWorktreeToDelete)
}

// IsReviewable reports whether an agent has work of its own to read.
func (l Listing) IsReviewable(agent Agent) bool {
	return l.ReviewDir(agent) != "" && agent.Role != RoleReviewer
}

// ReviewDir is the checkout an agent's review reads, its workflow's once the task's worktree is removed,
// and empty when a removed worktree left no snapshot to read.
func (l Listing) ReviewDir(agent Agent) string {
	task := l.Tasks.FindForAgent(agent)
	switch {
	case !task.IsWorktreeRemoved():
		return agent.Cwd
	case task.Snapshots == 0:
		return ""
	}

	return l.WorkflowFor(agent).Cwd
}

// DocumentTypeNames is the name of every document type, by name.
func (l Listing) DocumentTypeNames() []string {
	return xslice.Map(l.DocumentTypes, func(t *agentv1.DocumentType) string { return t.GetName() })
}

// WritersByTask is the writer of each task among the agents, keyed by its task.
func WritersByTask(agents []AgentRow) map[string]AgentRow {
	writers := make(map[string]AgentRow)
	for _, item := range agents {
		if item.Agent.Role == RoleWriter && item.Agent.TaskID != "" {
			writers[item.Agent.TaskID] = item
		}
	}

	return writers
}

// FindWriter is the writer of a reviewer's task, when it is among the writers.
func FindWriter(item AgentRow, writers map[string]AgentRow) (AgentRow, bool) {
	if item.Agent.Role != RoleReviewer {
		return AgentRow{}, false
	}
	writer, ok := writers[item.Agent.TaskID]

	return writer, ok
}

// ReviewersByWriter is the reviewers on each writer's task, keyed by the writer.
func ReviewersByWriter(agents []AgentRow, writers map[string]AgentRow) map[string][]AgentRow {
	reviewers := make(map[string][]AgentRow)
	for _, item := range agents {
		if writer, ok := FindWriter(item, writers); ok {
			reviewers[writer.Agent.ID] = append(reviewers[writer.Agent.ID], item)
		}
	}

	return reviewers
}

// documentTypesFrom is the document types the watch holds, by name.
func documentTypesFrom(held map[string]*agentv1.DocumentType) []*agentv1.DocumentType {
	types := slices.Collect(maps.Values(held))
	slices.SortFunc(types, func(a, b *agentv1.DocumentType) int {
		return cmp.Or(strings.Compare(strings.ToLower(a.GetName()), strings.ToLower(b.GetName())), strings.Compare(a.GetName(), b.GetName()))
	})

	return types
}

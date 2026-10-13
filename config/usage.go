package config

// defaultMaxAgents is how many agents may exist at once when the config names no number,
// well above what anyone opens by hand and low enough to catch a caller in a loop.
const defaultMaxAgents = 50

// defaultMaxReviewers is how many reviewers one task may hold at once when the config names no
// number. It is far lower than the panel's own ceiling because they all read the one checkout.
const defaultMaxReviewers = 3

// defaultWriterAutoCompaction is the conversation size a writer compacts at.
const defaultWriterAutoCompaction = 250_000

// defaultReviewerAutoCompaction is the conversation size a reviewer compacts at.
const defaultReviewerAutoCompaction = 250_000

// defaultOrchestratorAutoCompaction is the conversation size an orchestrator compacts at.
const defaultOrchestratorAutoCompaction = 150_000

// Usage is how much agents may run and how large their conversations may grow.
type Usage struct {
	// The number of unarchived agents this engine may run at once. Past it, komrad refuses a create.
	MaxAgents int `mapstructure:"max_agents"`
	// The limits on one task.
	Task TaskUsage `mapstructure:"task"`
	// The limits on a writer, the agent that lands a task's work on its branch.
	Writer WriterUsage `mapstructure:"writer"`
	// The limits on a reviewer, the agent that reads a task's work.
	Reviewer ReviewerUsage `mapstructure:"reviewer"`
	// The limits on a workflow's orchestrator.
	Orchestrator OrchestratorUsage `mapstructure:"orchestrator"`
}

// TaskUsage is what one task may hold.
type TaskUsage struct {
	// The number of live reviewers one task may hold at once. Reviewers read their task's checkout,
	// so this is deliberately far lower than max_agents.
	MaxReviewers int `mapstructure:"max_reviewers"`
}

// WriterUsage is what one writer may use.
type WriterUsage struct {
	// The conversation size in tokens an agent is held under, summarizing its history a little
	// before it gets there. Lower costs less per turn and forgets more. 0 leaves the agent CLI's
	// own default, and a value the CLI will not take is moved to the nearest one it will.
	AutoCompaction int `mapstructure:"auto_compaction"`
}

// ReviewerUsage is what one reviewer may use.
type ReviewerUsage struct {
	// The conversation size in tokens an agent is held under, summarizing its history a little
	// before it gets there. Lower costs less per turn and forgets more. 0 leaves the agent CLI's
	// own default, and a value the CLI will not take is moved to the nearest one it will.
	AutoCompaction int `mapstructure:"auto_compaction"`
	// Archives a reviewer once it releases an approval when on. Turn it off to keep approved
	// reviewers in the sidebar until you archive them yourself.
	ArchiveOnApproval bool `mapstructure:"archive_on_approval"`
}

// OrchestratorUsage is what one orchestrator may use.
type OrchestratorUsage struct {
	// The conversation size in tokens an agent is held under, summarizing its history a little
	// before it gets there. Lower costs less per turn and forgets more. 0 leaves the agent CLI's
	// own default, and a value the CLI will not take is moved to the nearest one it will.
	AutoCompaction int `mapstructure:"auto_compaction"`
	// The number of live tasks one orchestrator may have started at once. 0 sets no limit of its own,
	// leaving max_agents as the cap.
	MaxTasks int `mapstructure:"max_tasks"`
}

// newUsage is the usage limits a config that names none runs on.
func newUsage() Usage {
	return Usage{
		MaxAgents:    defaultMaxAgents,
		Task:         TaskUsage{MaxReviewers: defaultMaxReviewers},
		Writer:       WriterUsage{AutoCompaction: defaultWriterAutoCompaction},
		Reviewer:     ReviewerUsage{AutoCompaction: defaultReviewerAutoCompaction, ArchiveOnApproval: true},
		Orchestrator: OrchestratorUsage{AutoCompaction: defaultOrchestratorAutoCompaction},
	}
}

func (u *Usage) normalize() {
	if u.MaxAgents == 0 {
		u.MaxAgents = defaultMaxAgents
	}
	if u.Task.MaxReviewers == 0 {
		u.Task.MaxReviewers = defaultMaxReviewers
	}
}

func (u Usage) validate() error {
	// A negative ceiling would read as zero and take the default, leaving the file saying
	// one thing while the panel does another.
	limits := []struct {
		key   string
		value int
		unit  string
	}{
		{"usage.max_agents", u.MaxAgents, "a number of agents"},
		{"usage.task.max_reviewers", u.Task.MaxReviewers, "a number of agents"},
		{"usage.orchestrator.max_tasks", u.Orchestrator.MaxTasks, "a number of tasks"},
		{"usage.writer.auto_compaction", u.Writer.AutoCompaction, "a number of tokens"},
		{"usage.reviewer.auto_compaction", u.Reviewer.AutoCompaction, "a number of tokens"},
		{"usage.orchestrator.auto_compaction", u.Orchestrator.AutoCompaction, "a number of tokens"},
	}
	for _, limit := range limits {
		if err := nonNegative(limit.key, limit.value, limit.unit); err != nil {
			return err
		}
	}

	return nil
}

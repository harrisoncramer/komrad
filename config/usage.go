package config

// defaultMaxAgents is how many agents may exist at once when the config names no number,
// well above what anyone opens by hand and low enough to catch a caller in a loop.
const defaultMaxAgents = 50

// defaultMaxChildren is how many children one agent may hold at once when the config names no
// number. It is far lower than the panel's own ceiling because they all work in the one
// checkout, so a third is usually a sign an orchestrator is fanning out where it wanted agents
// of their own.
const defaultMaxChildren = 3

// defaultImplementerAutoCompaction is the conversation size an implementer compacts at.
const defaultImplementerAutoCompaction = 250_000

// defaultReviewerAutoCompaction is the conversation size a reviewer compacts at.
const defaultReviewerAutoCompaction = 250_000

// defaultOrchestratorAutoCompaction is the conversation size an orchestrator compacts at.
const defaultOrchestratorAutoCompaction = 150_000

// Usage is how much agents may run and how large their conversations may grow.
type Usage struct {
	// How many unarchived agents this engine may run at once, past which a create is refused.
	MaxAgents int `mapstructure:"max_agents"`
	// What an implementer, the agent that lands work on a branch of its own, may use.
	Implementer ImplementerUsage `mapstructure:"implementer"`
	// What a reviewer, the child that reads another agent's work, may use.
	Reviewer ReviewerUsage `mapstructure:"reviewer"`
	// What a workflow's orchestrator may use.
	Orchestrator OrchestratorUsage `mapstructure:"orchestrator"`
}

// ImplementerUsage is what one implementer may use.
type ImplementerUsage struct {
	// The conversation size in tokens an agent is held under, summarizing its history a little
	// before it gets there. Lower costs less per turn and forgets more. 0 leaves the agent CLI's
	// own default, and a value the CLI will not take is moved to the nearest one it will.
	AutoCompaction int `mapstructure:"auto_compaction"`
	// How many children, such as its reviewer, one implementer may hold at once. Children share
	// their parent's checkout, so this is deliberately far lower than max_agents.
	MaxChildren int `mapstructure:"max_children"`
}

// ReviewerUsage is what one reviewer may use.
type ReviewerUsage struct {
	// The conversation size in tokens an agent is held under, summarizing its history a little
	// before it gets there. Lower costs less per turn and forgets more. 0 leaves the agent CLI's
	// own default, and a value the CLI will not take is moved to the nearest one it will.
	AutoCompaction int `mapstructure:"auto_compaction"`
}

// OrchestratorUsage is what one orchestrator may use.
type OrchestratorUsage struct {
	// The conversation size in tokens an agent is held under, summarizing its history a little
	// before it gets there. Lower costs less per turn and forgets more. 0 leaves the agent CLI's
	// own default, and a value the CLI will not take is moved to the nearest one it will.
	AutoCompaction int `mapstructure:"auto_compaction"`
	// How many agents one orchestrator may have spawned and running at once, not counting their
	// children. 0 sets no limit of its own, leaving max_agents as the cap.
	MaxChildren int `mapstructure:"max_children"`
}

// newUsage is the usage limits a config that names none runs on.
func newUsage() Usage {
	return Usage{
		MaxAgents: defaultMaxAgents,
		Implementer: ImplementerUsage{
			AutoCompaction: defaultImplementerAutoCompaction,
			MaxChildren:    defaultMaxChildren,
		},
		Reviewer:     ReviewerUsage{AutoCompaction: defaultReviewerAutoCompaction},
		Orchestrator: OrchestratorUsage{AutoCompaction: defaultOrchestratorAutoCompaction},
	}
}

func (u *Usage) normalize() {
	if u.MaxAgents == 0 {
		u.MaxAgents = defaultMaxAgents
	}
	if u.Implementer.MaxChildren == 0 {
		u.Implementer.MaxChildren = defaultMaxChildren
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
		{"usage.implementer.max_children", u.Implementer.MaxChildren, "a number of agents"},
		{"usage.orchestrator.max_children", u.Orchestrator.MaxChildren, "a number of agents"},
		{"usage.implementer.auto_compaction", u.Implementer.AutoCompaction, "a number of tokens"},
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

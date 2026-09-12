package harness

import "context"

// Target is the agent a channel speaks to or an observer reads, carrying only what reaching it
// takes.
type Target struct {
	AgentID   string
	Pane      string
	HarnessID string
}

// Channel is how weave says something to an agent.
type Channel interface {
	Ready(ctx context.Context, target Target) (bool, error)

	Send(ctx context.Context, target Target, text string) (bool, error)

	Interrupt(ctx context.Context, target Target) error

	Forget(agentID string)
}

// Observer is how weave learns what an agent is doing.
type Observer interface {
	Observe(ctx context.Context, target Target) (Observation, error)
}

// Observation is one reading of an agent, however it was taken.
type Observation struct {
	Activity Activity

	// Held is the text the agent's input box holds, empty for a source that has no box to read.
	Held []string

	// Frame is the capture the reading was taken from, empty for a source that reads no screen.
	Frame string
}

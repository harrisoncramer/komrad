// Package scenario holds the data a demo of the panel is drawn against, one scenario per screen
// worth looking at.
package scenario

import (
	"fmt"
	"slices"
	"strings"
	"time"

	agentv1 "weave/proto/gen/agent/v1"
	schedulev1 "weave/proto/gen/schedule/v1"
	workflowv1 "weave/proto/gen/workflow/v1"
)

// Scenario is everything the fake service answers with.
type Scenario struct {
	Name           string
	Blurb          string
	Agents         []*agentv1.Agent
	ArchivedAgents []*agentv1.Agent
	Workflows      []*workflowv1.Workflow
	Schedules      []*schedulev1.Schedule
	Mail           []*agentv1.Mail
	Rounds         []*agentv1.ReviewRound
	Reviews        []*agentv1.Review
	Threads        []*agentv1.ReviewThread
	Viewed         []*agentv1.ReviewViewedFile
	// NeedsRepo says the screens here run git, so a throwaway repository is built before the panel
	// starts and handed back through WithRepo.
	NeedsRepo bool
}

// WithRepoParams is the throwaway repository a scenario was given, and the commits in it a round
// stands on.
type WithRepoParams struct {
	Dir string
	// BaseSHA is where the branch left main.
	BaseSHA string
	// Snapshots are the commits the scenario's rounds stand on, oldest first.
	Snapshots []string
}

// WithRepo points the scenario at a real repository.
func (s Scenario) WithRepo(params WithRepoParams) Scenario {
	for _, agent := range s.Agents {
		agent.Cwd = params.Dir
	}

	// A round stands on the commit its own ordinal names, so two agents' rounds can share the
	// repository without either one's numbering deciding the other's.
	for _, round := range s.Rounds {
		round.BaseSha = params.BaseSHA

		if at := int(round.GetOrdinal()) - 1; at >= 0 && at < len(params.Snapshots) {
			round.SnapshotSha = params.Snapshots[at]
		}
	}

	for _, thread := range s.Threads {
		thread.BaseSha = params.BaseSHA
	}

	return s
}

// All is every scenario a demo can be started with, the first being the default.
func All() []Scenario {
	return []Scenario{Busy(), Review(), Empty()}
}

// Named is the scenario with that name.
func Named(name string) (Scenario, error) {
	held := All()

	at := slices.IndexFunc(held, func(s Scenario) bool { return s.Name == name })
	if at < 0 {
		return Scenario{}, fmt.Errorf("no scenario called %q, try one of: %s", name, strings.Join(Names(), ", "))
	}

	return held[at], nil
}

// Names is what each scenario is called, in the order All returns them.
func Names() []string {
	var out []string

	for _, held := range All() {
		out = append(out, held.Name)
	}

	return out
}

// ago is a timestamp the given duration in the past, in the format the service sends.
func ago(d time.Duration) string {
	return time.Now().Add(-d).Format(time.RFC3339)
}

// in is a timestamp the given duration from now.
func in(d time.Duration) string {
	return time.Now().Add(d).Format(time.RFC3339)
}

func ptr[T any](value T) *T { return &value }

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
}

// All is every scenario a demo can be started with, the first being the default.
func All() []Scenario {
	return []Scenario{Busy(), Empty()}
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

// in is a timestamp the given duration from now, which is what an expiry is.
func in(d time.Duration) string {
	return time.Now().Add(d).Format(time.RFC3339)
}

func ptr[T any](value T) *T { return &value }

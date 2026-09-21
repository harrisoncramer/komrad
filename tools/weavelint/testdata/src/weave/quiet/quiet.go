package quiet

import (
	"weave/internal/forge"
	agentv1 "weave/proto/gen/agent/v1"
)

func leak(s forge.Secret) string {
	return s.Expose()
}

func short() string { return "one line" }

func toAgent(id string) *agentv1.Agent {
	return &agentv1.Agent{Id: id}
}

func agentFromProto(a *agentv1.Agent) string {
	return a.Id
}

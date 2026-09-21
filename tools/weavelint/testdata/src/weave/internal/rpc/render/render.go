package render

import agentv1 "weave/proto/gen/agent/v1"

func Agent(id string) *agentv1.Agent {
	return &agentv1.Agent{Id: id}
}

package render

import agentv1 "komrad/proto/gen/agent/v1"

func Agent(id string) *agentv1.Agent {
	return &agentv1.Agent{Id: id}
}

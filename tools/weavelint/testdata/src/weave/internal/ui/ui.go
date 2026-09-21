package ui

import agentv1 "weave/proto/gen/agent/v1"

type agent struct{ id string }

func agentFrom(a *agentv1.Agent) agent {
	return agent{id: a.Id}
}

func protoToAgent(a *agentv1.Agent) agent { // want `protoToAgent is named after the proto: name it agentFrom`
	return agent{id: a.Id}
}

func agentFromProto(a *agentv1.Agent) agent { // want `agentFromProto is named after the proto: name it agentFrom`
	return agent{id: a.Id}
}

func renderHit(a *agentv1.Agent) agent { // want `renderHit reads a proto message: name it hitFrom`
	return agent{id: a.Id}
}

func renderTitle(a agent) string {
	return a.id
}

func toProto(a agent) *agentv1.Agent {
	return &agentv1.Agent{Id: a.id}
}

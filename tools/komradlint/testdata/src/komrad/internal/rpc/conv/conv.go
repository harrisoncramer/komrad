package conv

import agentv1 "komrad/proto/gen/agent/v1"

type agent struct{ id string }

type verdict int

func renderAgent(a agent) *agentv1.Agent {
	return &agentv1.Agent{Id: a.id}
}

func renderAgents(listed []agent) []*agentv1.Agent {
	out := make([]*agentv1.Agent, 0, len(listed))
	for _, a := range listed {
		out = append(out, renderAgent(a))
	}

	return out
}

func toAgent(a agent) *agentv1.Agent { // want `toAgent builds a proto message: name it renderAgent`
	return &agentv1.Agent{Id: a.id}
}

func verdictOf(v verdict) agentv1.ReviewVerdict { // want `verdictOf builds a proto message: name it renderVerdictOf`
	return agentv1.ReviewVerdict(v)
}

func agentFrom(a *agentv1.Agent) agent {
	return agent{id: a.Id}
}

type service struct{}

func (service) agent(a agent) *agentv1.Agent {
	return renderAgent(a)
}

func tokenFor(a agent) *agentv1.Agent { // want `tokenFor builds a proto message: name it renderTokenFor`
	return &agentv1.Agent{Id: a.id}
}

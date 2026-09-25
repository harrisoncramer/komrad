package ui

import (
	"komrad/lib/xslice"
)

// selectAgents is a selection holding the named agents, spelled by id as the tables read.
func selectAgents(ids ...string) selection {
	return selection{keys: xslice.Map(ids, agentRowKey)}
}

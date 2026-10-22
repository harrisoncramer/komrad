package ui

import (
	"komrad/internal/ui/list"
	"komrad/lib/xslice"
)

// selectAgents is a selection holding the named agents, spelled by id as the tables read.
func selectAgents(ids ...string) list.Selection {
	return list.Selection{Keys: xslice.Map(ids, agentRowKey)}
}

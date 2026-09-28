// Package searchsyntax lists the qualifiers a transcript search takes, for the panel to show.
package searchsyntax

// Qualifier is one qualifier as it is typed and what it narrows the search to.
type Qualifier struct {
	Syntax  string
	Meaning string
}

// Qualifiers is every qualifier a transcript search takes, in the order they are shown.
var Qualifiers = []Qualifier{
	{Syntax: "from:me", Meaning: "prompts, mail included"},
	{Syntax: "from:agent", Meaning: "what the agent wrote"},
	{Syntax: "agent:NAME", Meaning: "agents named like NAME"},
	{Syntax: "workflow:NAME", Meaning: "agents in that workflow"},
	{Syntax: "is:reviewer", Meaning: "or implementer, orchestrator"},
	{Syntax: "-is:reviewer", Meaning: "leave that kind out"},
	{Syntax: "is:live", Meaning: "or is:archived"},
	{Syntax: "since:3d", Meaning: "or 5h, 2w, today, a date"},
	{Syntax: "until:DATE", Meaning: "up to the end of that day"},
	{Syntax: "on:DATE", Meaning: "that one day"},
	{Syntax: "sort:new", Meaning: "or sort:old, sort:best"},
}

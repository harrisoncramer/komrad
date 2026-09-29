// Package searchsyntax lists the qualifiers a transcript search takes, for the panel to show.
package searchsyntax

// The keys a qualifier is typed with, before its colon.
const (
	KeyFrom     = "from"
	KeyAgent    = "agent"
	KeyWorkflow = "workflow"
	KeyIs       = "is"
	KeySort     = "sort"
	KeySince    = "since"
	KeyUntil    = "until"
	KeyOn       = "on"
)

// Qualifier is one qualifier as it is typed and what it narrows the search to.
type Qualifier struct {
	Syntax  string
	Meaning string
}

// Qualifiers is every qualifier a transcript search takes, in the order they are shown.
var Qualifiers = []Qualifier{
	{Syntax: KeyFrom + ":me", Meaning: "prompts, mail included"},
	{Syntax: KeyFrom + ":agent", Meaning: "what the agent wrote"},
	{Syntax: KeyAgent + ":NAME", Meaning: "agents named like NAME"},
	{Syntax: KeyWorkflow + ":NAME", Meaning: "agents in that workflow"},
	{Syntax: KeyIs + ":reviewer", Meaning: "or implementer, orchestrator"},
	{Syntax: "-" + KeyIs + ":reviewer", Meaning: "leave that kind out"},
	{Syntax: KeyIs + ":live", Meaning: "or is:archived"},
	{Syntax: KeySince + ":3d", Meaning: "or 5h, 2w, today, a date"},
	{Syntax: KeyUntil + ":DATE", Meaning: "up to the end of that day"},
	{Syntax: KeyOn + ":DATE", Meaning: "that one day"},
	{Syntax: KeySort + ":new", Meaning: "or sort:old, sort:best"},
}

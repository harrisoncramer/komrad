// Package screen names the panel's screens, the row one opens on, and the requests a screen hands
// the root.
package screen

// ID names one of the panel's screens.
type ID int

const (
	Agents ID = iota
	Schedules
	Review
	Worktrees
	Documents
	Logs
	StatusChecks
	StatusCheckLogs
)

// IDs is every screen, in the order the panel lists them.
var IDs = []ID{Agents, Schedules, Review, Worktrees, Documents, Logs, StatusChecks, StatusCheckLogs}

// Target is the row a screen must land on, each screen reading the fields it understands.
type Target struct {
	AgentID string
	// TaskID is the task the screen opens on, for a screen that shows a task's work.
	TaskID string
	// OpenedFrom is the screen that opened this one.
	OpenedFrom ID
	// Resume says the screen is being gone back to and keeps what it held.
	Resume bool
}

// Request is what a screen asks the root to do.
type Request interface {
	Request()
}

// Open leaves the screen that is up for another, landing it on the target.
type Open struct {
	Screen ID
	Target Target
}

// Report says why something the user asked for failed, and says nothing when it did not.
type Report struct {
	Summary string
	Err     error
}

// Hide puts the panel away.
type Hide struct{}

// String is the screen as an error names it.
func (id ID) String() string {
	switch id {
	case Agents:
		return "the agents list"
	case Schedules:
		return "the schedules"
	case Review:
		return "the review"
	case Worktrees:
		return "the worktrees"
	case Documents:
		return "the documents"
	case Logs:
		return "the logs"
	case StatusChecks:
		return "the status checks"
	case StatusCheckLogs:
		return "the status check logs"
	}

	return ""
}

// Request marks Open as a request.
func (Open) Request() {}

// Request marks Report as a request.
func (Report) Request() {}

// Request marks Hide as a request.
func (Hide) Request() {}

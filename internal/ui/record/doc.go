// Package record holds the record a list renders under itself for the row under the cursor. A
// record is a set of fields and notices, and each notice says in words what one of the row's marks
// means.
//
// Each screen builds the record for its hovered row, and the root renders it. The package also
// holds the fields that say how a workflow launches its agents. The root and the agents, schedules,
// worktrees and documents screens import record. It imports only the panel, paint and label
// packages, and never the root or a domain.
package record

// Package panel holds what komrad has told the panel, in the panel's own shape. It holds the
// agents, tasks, workflows, schedules, worktrees and pull requests, and the listing it assembles
// from each world the watch stream sends.
//
// The package converts the protos into those shapes once, so the screens never read a proto. The
// root, api, column, form, menu, git menu, record and screen import panel, and so does every
// screen domain but logs. It imports only the label package under internal/ui, and never the root
// or a domain.
package panel

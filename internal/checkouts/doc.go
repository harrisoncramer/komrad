// Package checkouts keeps each task's checkout in step with its base branch and with what the
// panel shows of it.
//
// The [Service] rebases and pushes a task's checkout on request, and undoes a sequence stopped
// part-way through it. It refuses a tree git cannot move and holds a lock on the checkout while it
// writes. Its cascade rebases a child task onto its parent's newest commit after the parent moves,
// and onto the base branch once the parent's pull request merges. When a task leaves its parent,
// the cascade points its pull request at the base branch. A child whose branch holds neither its
// fork point nor any of its parent's commits is orphaned, and its writer gets mail saying so.
//
// The [CwdSyncer] looks at every live task's checkout on each pass and records its branch, its
// diff stat, any rebase or merge stopped part-way, and which snapshots the base branch now holds.
package checkouts

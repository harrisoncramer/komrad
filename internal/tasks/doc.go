// Package tasks holds the rules for tasks, the outcomes agents work toward.
//
// The [TaskService] creates and finds tasks, puts an agent on one when it launches, and claims a
// task's checkout ahead of the directory the launch makes. It keeps a task to one writer and to
// its own workflow, and lets only an agent on the task or its workflow's orchestrator write its
// notes. A child task records its parent and the commit it forks from, and a parent with an
// undeleted child cannot be deleted without it. The service also runs each task's hooks as agents
// open and archive it, and removes the worktrees of archived tasks.
package tasks

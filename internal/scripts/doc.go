// Package scripts finds, lists and runs the executable files a person writes for komrad to run
// against one agent. The scripts are the provisioner that makes its directory, the deprovisioner
// that removes it, and the hooks bound to moments in its life.
//
// A script's header comments say what it does and how long it may run. Komrad reads the header
// for the listing, passes the agent to the script in its environment, and stops it at its timeout.
// The package also installs the scripts this build ships, such as the provisioner that makes a
// git worktree, so making a directory is the script's job and not komrad's.
package scripts

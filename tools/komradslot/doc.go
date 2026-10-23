// Command komradslot gives each komrad checkout a throwaway instance of its own. It claims a slot
// for a checkout and prints the slot's environment, refuses a command whose environment is not the
// checkout's slot, drops the slot's database, and takes an instance down. The test instance tasks
// under mise run it, and its reap command takes down every slot whose checkout is gone or that
// has sat idle.
package main

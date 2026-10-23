// Package mail stores the mail in each agent's mailbox. It sends mail, lets the agent claim it,
// and expires mail past its deadline. Komrad's own mail about an event is one unread mail per
// subject, rewritten as new events arrive.
//
// The package also holds the nudge, the right to tell an idle agent that unread mail is waiting,
// which only mail the agent has not been told about grants.
package mail

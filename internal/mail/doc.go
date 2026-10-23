// Package mail holds the rules for the mail agents and komrad send each other, and delivers it.
//
// The [MailService] sends ordinary mail between agents, a schedule's prompt on the person's
// behalf, and mail from komrad itself, which replaces unread mail of the same kind about the same
// subject. The [DeliveryService] tells each agent with waiting mail that it has some, and mail to
// a dormant agent waits until the agent resumes.
package mail

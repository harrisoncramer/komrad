// Package events turns a change into mail for the agents it concerns.
//
// A service records an [Event] on a River queue in the same transaction as the change. The
// [Router] hands each event to the [Handler] for its topic, which writes the mail in the
// transaction that marks the event done, so an event is mailed once even when it retries.
package events

package config

import (
	"fmt"
	"strings"
	"time"
)

// defaultDeliveryDeadline is how long mail stays deliverable when the config names no
// duration. Long enough that an agent left alone over lunch still gets its mail, short
// enough that nothing chases a recipient nobody is coming back to.
const defaultDeliveryDeadline = "3h"

// DefaultMaxExchange is how much mail two agents may write each other when the config names no
// limit, set at more than twice the longest exchange the panel has recorded finishing, since a
// cap set too high costs one long loop and a cap set too low severs a review as it concludes.
const DefaultMaxExchange = 40

// Mail is how long komrad keeps trying to hand mail to the agent it was addressed
// to. Delivery waits on the recipient: an agent that is busy gets its mail as its turn
// ends, and one that has stopped is nudged, so an agent nobody is running holds mail until
// it comes back or the deadline passes.
type Mail struct {
	// How long mail stays deliverable, as a Go duration such as "3h" or "45m". Mail past
	// it is neither delivered nor readable, and the row is kept as a record that it never
	// arrived. A duration komrad cannot read, or one that is not positive, is refused.
	DeliveryDeadline string `mapstructure:"delivery_deadline"`
	// How much mail two agents may write each other before a send between them is
	// refused. It counts the pair in both directions over every thread, so opening a new
	// subject is not a way around it. When a send is refused komrad marks a row for
	// attention and tells the recipient its mail was stopped, so an exchange that is cut
	// off does not leave the other side waiting on a reply that will never arrive. A
	// negative limit is refused.
	MaxExchange int `mapstructure:"max_exchange"`
}

// GetDeliveryDeadline is how long mail stays deliverable, parsed. A duration that will
// not parse is an error rather than the default, since a deadline somebody wrote and komrad
// quietly ignored is worse than one it refuses.
func (m Mail) GetDeliveryDeadline() (time.Duration, error) {
	if m.DeliveryDeadline == "" {
		m.DeliveryDeadline = defaultDeliveryDeadline
	}

	parsed, err := time.ParseDuration(strings.TrimSpace(m.DeliveryDeadline))
	if err != nil {
		return 0, fmt.Errorf("unreadable delivery deadline %q: %w", m.DeliveryDeadline, err)
	}
	if parsed <= 0 {
		return 0, fmt.Errorf("the delivery deadline must be positive, not %q", m.DeliveryDeadline)
	}

	return parsed, nil
}

// GetMaxExchange is how much mail two agents may write each other. An unset limit takes the
// default; a negative one is an error rather than the default, for the same reason an
// unparseable deadline is: a config somebody wrote and komrad quietly ignored is worse than one
// it refuses.
func (m Mail) GetMaxExchange() (int, error) {
	if m.MaxExchange == 0 {
		m.MaxExchange = DefaultMaxExchange
	}
	if m.MaxExchange < 0 {
		return 0, fmt.Errorf("the mail exchange limit must be positive, not %d", m.MaxExchange)
	}

	return m.MaxExchange, nil
}

// validate rejects the two mail settings the server reads at send time.
func (m Mail) validate() error {
	if _, err := m.GetDeliveryDeadline(); err != nil {
		return fmt.Errorf("mail.delivery_deadline: %w", err)
	}
	if _, err := m.GetMaxExchange(); err != nil {
		return fmt.Errorf("mail.max_exchange: %w", err)
	}

	return nil
}

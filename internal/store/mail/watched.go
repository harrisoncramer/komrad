package mail

import (
	"context"

	"weave/internal/watch"
	watchv1 "weave/proto/gen/watch/v1"
)

// WatchedMail declares the mail to the watch.
var WatchedMail = watch.Resource{
	Enum:      watchv1.Resource_RESOURCE_MAIL,
	Table:     "mail",
	KeyColumn: "id",
}

// ListWatched returns whichever of keys hold unclaimed mail, or every unclaimed piece when keys is nil.
func (s *MailStore) ListWatched(ctx context.Context, keys []string) ([]Mail, error) {
	if keys == nil {
		return s.List(ctx)
	}

	return s.ListByKey(ctx, keys)
}

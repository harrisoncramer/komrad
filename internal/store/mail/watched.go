package mail

import (
	"context"
	"fmt"

	"komrade/internal/watch"
	"komrade/lib/xslice"
	watchv1 "komrade/proto/gen/watch/v1"
)

// WatchedMail declares the mail to the watch.
var WatchedMail = watch.Resource{
	Enum:      watchv1.Resource_RESOURCE_MAIL,
	Table:     "mail",
	KeyColumn: "id",
}

// ListWatched returns whichever of keys hold unclaimed mail, or every unclaimed piece when keys is nil.
func (s *MailStore) ListWatched(ctx context.Context, keys []string) ([]Mail, error) {
	rows, err := s.queries.ListWatchedMail(ctx, keys)
	if err != nil {
		return nil, fmt.Errorf("failed to list the watched mail: %w", err)
	}

	return xslice.Map(rows, fromRow), nil
}

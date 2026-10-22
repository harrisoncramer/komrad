// Package xuuid holds helpers for uuids.
package xuuid

import "github.com/google/uuid"

// ParseOrNil parses text as a uuid, returning uuid.Nil for text that does not parse.
func ParseOrNil(text string) uuid.UUID {
	id, err := uuid.Parse(text)
	if err != nil {
		return uuid.Nil
	}

	return id
}

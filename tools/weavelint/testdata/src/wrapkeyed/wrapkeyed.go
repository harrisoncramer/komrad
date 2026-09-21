package wrapkeyed

import (
	"errors"
	"fmt"
)

var errBase = errors.New("base")

func keyed() error {
	return fmt.Errorf("mail.max_exchange: %w", errBase)
}

package wrap

import "fmt"

func stub() error {
	return fmt.Errorf("a test's own wording: %w", errBase)
}

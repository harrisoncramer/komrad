// Package render holds what the handlers' resource renderers have in common.
package render

import "github.com/google/uuid"

// SummaryHead is how much of a long free-text field a summary listing returns.
const SummaryHead = 200

func formatOptionalID(id *uuid.UUID) *string {
	if id == nil {
		return nil
	}

	text := id.String()

	return &text
}

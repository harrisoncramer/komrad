package notes

import (
	"context"
	"testing"

	"komrad/internal/store/notestore"
)

func TestRead(t *testing.T) {
	_, _ = notestore.NewNoteStore(nil).Get(context.Background(), "a")
}

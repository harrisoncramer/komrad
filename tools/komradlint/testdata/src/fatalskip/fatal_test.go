package fatalskip

import (
	"errors"
	"testing"
)

func TestFatal(t *testing.T) {
	t.Fatal(errors.New("boom"))
}

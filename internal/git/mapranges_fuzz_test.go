package git

import (
	"testing"

	"github.com/stretchr/testify/assert"
)

// FuzzMapPatch is the one input weave does not choose: a patch is git's output today and could be
// a stranger's tomorrow, and a malformed hunk header must be an error rather than a panic in the
// panel's own process.
func FuzzMapPatch(f *testing.F) {
	f.Add([]byte(""))
	f.Add([]byte("diff --git a/a.go b/a.go\n--- a/a.go\n+++ b/a.go\n@@ -1 +1 @@\n-a\n+b\n"))
	f.Add([]byte("diff --git a/a.go b/a.go\n--- a/a.go\n+++ b/a.go\n@@ -0,0 +1,2 @@\n+a\n+b\n"))
	f.Add([]byte("diff --git a/a.go b/a.go\n--- a/a.go\n+++ b/a.go\n@@ -9999999999999999999,1 +1,1 @@\n-a\n+b\n"))
	f.Add([]byte("diff --git a/a.go b/b.go\nsimilarity index 90%\nrename from a.go\nrename to b.go\n"))

	f.Fuzz(func(t *testing.T, raw []byte) {
		mappings, err := mapPatch(raw)
		if err != nil {
			return
		}

		for _, mapping := range mappings {
			for _, current := range mapping.Ranges {
				assert.LessOrEqual(t, current.Old.Start, current.Old.End, "a region cannot end before it starts")
				assert.LessOrEqual(t, current.New.Start, current.New.End, "a region cannot end before it starts")
			}
		}
	})
}

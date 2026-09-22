//go:build ignore

// Command gen writes the watch trigger attachments from the resources in internal/watch.
// It runs from that package's directory via go:generate, so the migration it writes is three
// levels up.
package main

import (
	"fmt"
	"os"
	"os/exec"

	"weave/internal/watch"
)

const outputPath = "../../db/migrations/20260922050000_watch_attachments.sql"

func main() {
	if err := os.WriteFile(outputPath, watch.RenderAttachments(), 0o644); err != nil {
		fmt.Fprintf(os.Stderr, "failed to write %s: %v\n", outputPath, err)
		os.Exit(1)
	}

	// Atlas keeps a sum of the migrations, which a rewritten one moves.
	hash := exec.Command("atlas", "migrate", "hash", "--dir", "file://migrations")
	hash.Dir = "../../db"
	hash.Stderr = os.Stderr
	if err := hash.Run(); err != nil {
		fmt.Fprintf(os.Stderr, "failed to rehash the migrations: %v\n", err)
		os.Exit(1)
	}

	fmt.Printf("wrote %s\n", outputPath)
}

package main

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"

	"weave/internal/cli"

	"github.com/spf13/cobra"
	"github.com/spf13/cobra/doc"
)

func main() {
	if err := run("docs/reference"); err != nil {
		fmt.Fprintln(os.Stderr, "gen-docs:", err)
		os.Exit(1)
	}
}

func run(dir string) error {
	root := cli.NewRootCommand()
	prepare(root)

	// Generating somewhere else first names the pages the command tree has without
	// restating cobra's own traversal and filename rules.
	staging, err := os.MkdirTemp("", "gen-docs")
	if err != nil {
		return fmt.Errorf("creating staging directory: %w", err)
	}

	return errors.Join(rebuild(root, dir, staging), os.RemoveAll(staging))
}

// rebuild leaves dir holding the pages the command tree has and nothing else, so a page whose
// command was removed or renamed does not stay behind.
func rebuild(root *cobra.Command, dir, staging string) error {
	if err := doc.GenMarkdownTree(root, staging); err != nil {
		return fmt.Errorf("generating markdown: %w", err)
	}

	if err := os.MkdirAll(dir, 0o755); err != nil {
		return fmt.Errorf("creating %s: %w", dir, err)
	}

	staged, err := os.ReadDir(staging)
	if err != nil {
		return fmt.Errorf("reading staged pages: %w", err)
	}

	written := make(map[string]struct{}, len(staged))
	for _, entry := range staged {
		name := entry.Name()

		page, err := os.ReadFile(filepath.Join(staging, name))
		if err != nil {
			return fmt.Errorf("reading staged page %s: %w", name, err)
		}

		if err := os.WriteFile(filepath.Join(dir, name), page, 0o644); err != nil {
			return fmt.Errorf("writing %s: %w", name, err)
		}

		written[name] = struct{}{}
	}

	existing, err := os.ReadDir(dir)
	if err != nil {
		return fmt.Errorf("reading %s: %w", dir, err)
	}

	for _, entry := range existing {
		name := entry.Name()
		if entry.IsDir() || filepath.Ext(name) != ".md" {
			continue
		}

		if _, ok := written[name]; ok {
			continue
		}

		if err := os.Remove(filepath.Join(dir, name)); err != nil {
			return fmt.Errorf("removing %s: %w", name, err)
		}
	}

	return nil
}

func prepare(cmd *cobra.Command) {
	cmd.DisableAutoGenTag = true
	cmd.Hidden = false

	for _, c := range cmd.Commands() {
		prepare(c)
	}
}

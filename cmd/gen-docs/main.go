package main

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"

	"weave/config"
	"weave/internal/cli"

	"github.com/spf13/cobra"
)

func main() {
	if err := run(context.Background(), ".", "docs/src/data"); err != nil {
		fmt.Fprintln(os.Stderr, "gen-docs:", err)
		os.Exit(1)
	}
}

// run writes the reference data the docs site renders, reading the sources under root.
func run(ctx context.Context, root, dir string) error {
	notes, err := readComments(ctx, filepath.Join(root, "proto", "proto"))
	if err != nil {
		return err
	}

	settings, err := config.Document(filepath.Join(root, "config"))
	if err != nil {
		return fmt.Errorf("documenting the config: %w", err)
	}

	command := cli.NewRootCommand()
	prepare(command)

	if err := os.MkdirAll(dir, 0o755); err != nil {
		return fmt.Errorf("creating %s: %w", dir, err)
	}

	for name, value := range map[string]any{
		"api.json":    buildAPI(notes),
		"cli.json":    buildCLI(command),
		"config.json": settings,
	} {
		if err := writeJSON(filepath.Join(dir, name), value); err != nil {
			return err
		}
	}

	return nil
}

func writeJSON(path string, value any) error {
	encoded, err := json.MarshalIndent(value, "", "  ")
	if err != nil {
		return fmt.Errorf("encoding %s: %w", path, err)
	}

	if err := os.WriteFile(path, append(encoded, '\n'), 0o644); err != nil {
		return fmt.Errorf("writing %s: %w", path, err)
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

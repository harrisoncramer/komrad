// Package main gives each komrad checkout a throwaway instance of its own.
package main

import (
	"context"
	"flag"
	"fmt"
	"io"
	"log"
	"os"
	"os/signal"
	"path/filepath"
	"time"
)

func main() {
	os.Exit(run(os.Args[1:]))
}

func run(args []string) int {
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt)
	defer stop()

	logger := log.New(os.Stderr, "komradslot: ", 0)
	w := world{
		getenv: os.Getenv, //nolint:fussy // check exists to compare the environment against the slot.
		log:    logger,
		cmds:   execRunner{out: os.Stderr},
	}

	if err := w.run(ctx, args, os.Stdout); err != nil {
		logger.Print(err)

		return 1
	}

	return 0
}

// world is what a command reads and runs besides the registry.
type world struct {
	getenv func(string) string
	log    *log.Logger
	cmds   runner
}

func (w world) run(ctx context.Context, args []string, out io.Writer) error {
	usage := fmt.Errorf("usage: komradslot env|check|drop|down --root checkout [--registry dir]")
	if len(args) == 0 {
		return usage
	}

	flags := flag.NewFlagSet(args[0], flag.ContinueOnError)
	flags.SetOutput(io.Discard)
	root := flags.String("root", "", "the checkout the instance belongs to")
	dir := flags.String("registry", "", "the slot registry, ~/.local/state/komrad-test/instances when empty")

	if err := flags.Parse(args[1:]); err != nil {
		return fmt.Errorf("%w: %w", usage, err)
	}

	if *root == "" {
		return usage
	}

	checkout, err := resolve(*root)
	if err != nil {
		return err
	}

	reg, err := w.registry(*dir)
	if err != nil {
		return err
	}

	switch args[0] {
	case "env":
		return printEnv(ctx, reg, checkout, out)
	case "check":
		return check(reg, checkout, w.getenv)
	case "drop":
		return drop(ctx, reg, checkout)
	case "down":
		return down(ctx, reg, checkout)
	default:
		return usage
	}
}

func (w world) registry(dir string) (registry, error) {
	if dir == "" {
		home, err := os.UserHomeDir()
		if err != nil {
			return registry{}, fmt.Errorf("failed to find the home directory: %w", err)
		}

		dir = filepath.Join(home, ".local", "state", "komrad-test", "instances")
	}

	return registry{dir: dir, lockTimeout: 30 * time.Second, cmds: w.cmds, log: w.log}, nil
}

func resolve(root string) (string, error) {
	abs, err := filepath.Abs(root)
	if err != nil {
		return "", fmt.Errorf("failed to resolve %s: %w", root, err)
	}

	resolved, err := filepath.EvalSymlinks(abs)
	if err != nil {
		return "", fmt.Errorf("failed to resolve %s: %w", root, err)
	}

	return resolved, nil
}

// printEnv claims the checkout's slot and prints what it derives, one NAME=value per line.
func printEnv(ctx context.Context, reg registry, root string, out io.Writer) error {
	n, err := reg.claim(ctx, root)
	if err != nil {
		return err
	}

	if err := writeConfig(root, reg.slotDir(n)); err != nil {
		return err
	}

	for _, s := range buildSettings(reg.slotDir(n), n) {
		if _, err := fmt.Fprintf(out, "%s=%s\n", s.name, s.value); err != nil {
			return fmt.Errorf("failed to print the environment: %w", err)
		}
	}

	return nil
}

// check refuses unless the environment is exactly what the checkout's slot derives.
func check(reg registry, root string, getenv func(string) string) error {
	n, ok, err := reg.lookup(root)
	if err != nil {
		return err
	}

	if !ok {
		return fmt.Errorf("%s holds no slot, so there is no throwaway instance to run against", root)
	}

	for _, s := range buildSettings(reg.slotDir(n), n) {
		if have := getenv(s.name); have != s.value {
			return fmt.Errorf("%s is %q but slot %d needs %q; refusing to run against it", s.name, have, n, s.value)
		}
	}

	return nil
}

// drop drops the database and logins of the checkout's slot and keeps the slot.
func drop(ctx context.Context, reg registry, root string) error {
	n, ok, err := reg.lookup(root)
	if err != nil {
		return err
	}

	if !ok {
		return fmt.Errorf("%s holds no slot, so there is no database to drop", root)
	}

	return reg.drop(ctx, n)
}

// down takes the checkout's instance down and gives its slot back.
func down(ctx context.Context, reg registry, root string) error {
	n, ok, err := reg.retire(root)
	if err != nil {
		return err
	}

	if !ok {
		reg.log.Printf("%s holds no slot", root)

		return nil
	}

	return reg.down(ctx, n)
}

// Package main gives each komrad checkout a throwaway instance of its own.
package main

import (
	"context"
	"fmt"
	"io"
	"log"
	"net"
	"os"
	"os/signal"
	"path/filepath"
	"time"

	"github.com/spf13/cobra"
)

func main() {
	os.Exit(run())
}

func run() int {
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt)
	defer stop()

	logger := log.New(os.Stderr, "komradslot: ", 0)
	w := world{
		getenv: os.Getenv, //nolint:fussy // check exists to compare the environment against the slot.
		log:    logger,
		cmds:   execRunner{out: os.Stderr},
	}

	if err := newRootCmd(w).ExecuteContext(ctx); err != nil {
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

func newRootCmd(w world) *cobra.Command {
	var root, dir string

	cmd := &cobra.Command{
		Use:           "komradslot",
		Short:         "Give each komrad checkout a throwaway instance of its own",
		SilenceUsage:  true,
		SilenceErrors: true,
	}

	cmd.PersistentFlags().StringVar(&root, "root", "", "the checkout the instance belongs to")
	cmd.PersistentFlags().StringVar(&dir, "registry", "", "the slot registry, ~/.local/state/komrad-test/instances when empty")

	slotCmd := func(use, short string, fn func(ctx context.Context, reg registry, checkout string, out io.Writer) error) *cobra.Command {
		return &cobra.Command{
			Use:   use,
			Short: short,
			Args:  cobra.NoArgs,
			RunE: func(cmd *cobra.Command, _ []string) error {
				if root == "" {
					return fmt.Errorf("%s needs --root", cmd.CommandPath())
				}

				checkout, err := resolve(root)
				if err != nil {
					return err
				}

				reg, err := w.registry(dir)
				if err != nil {
					return err
				}

				return fn(cmd.Context(), reg, checkout, cmd.OutOrStdout())
			},
		}
	}

	cmd.AddCommand(
		slotCmd("env", "Claim the checkout's slot and print its environment", printEnv),
		slotCmd("check", "Refuse unless the environment is the checkout's slot", func(_ context.Context, reg registry, checkout string, _ io.Writer) error {
			return check(reg, checkout, w.getenv)
		}),
		slotCmd("drop", "Drop the database and logins of the checkout's slot", func(ctx context.Context, reg registry, checkout string, _ io.Writer) error {
			return drop(ctx, reg, checkout)
		}),
		slotCmd("down", "Take the checkout's instance down and give its slot back", func(ctx context.Context, reg registry, checkout string, _ io.Writer) error {
			return down(ctx, reg, checkout)
		}),
		&cobra.Command{
			Use:   "reap",
			Short: "Take down every slot whose checkout is gone or that has sat idle",
			Args:  cobra.NoArgs,
			RunE: func(cmd *cobra.Command, _ []string) error {
				reg, err := w.registry(dir)
				if err != nil {
					return err
				}

				return reg.reap(cmd.Context(), "")
			},
		},
	)

	return cmd
}

func (w world) registry(dir string) (registry, error) {
	if dir == "" {
		home, err := os.UserHomeDir()
		if err != nil {
			return registry{}, fmt.Errorf("failed to find the home directory: %w", err)
		}

		dir = filepath.Join(home, ".local", "state", "komrad-test", "instances")
	}

	return registry{
		dir:         dir,
		lockTimeout: 30 * time.Second,
		cmds:        w.cmds,
		log:         w.log,
		listening:   isListening,
	}, nil
}

func isListening(ctx context.Context, addr string) bool {
	dialer := net.Dialer{Timeout: 200 * time.Millisecond}

	conn, err := dialer.DialContext(ctx, "tcp", addr)
	if err != nil {
		return false
	}

	return conn.Close() == nil
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

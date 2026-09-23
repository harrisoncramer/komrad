package store

import (
	"bytes"
	"context"
	"fmt"
	"os/exec"
	"strings"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
)

const passwordCommandWait = 10 * time.Second

// Option changes how a pool connects.
type Option func(cfg *pgxpool.Config)

// WithPasswordCommand has every new connection take its password from what command prints.
func WithPasswordCommand(command string) Option {
	return func(cfg *pgxpool.Config) {
		if strings.TrimSpace(command) == "" {
			return
		}

		cfg.BeforeConnect = func(ctx context.Context, conn *pgx.ConnConfig) error {
			password, err := runPasswordCommand(ctx, command)
			if err != nil {
				return err
			}
			conn.Password = password

			return nil
		}
	}
}

func runPasswordCommand(ctx context.Context, command string) (string, error) {
	ctx, cancel := context.WithTimeout(ctx, passwordCommandWait)
	defer cancel()

	var stderr bytes.Buffer
	cmd := exec.CommandContext(ctx, "sh", "-c", command)
	cmd.Stderr = &stderr

	out, err := cmd.Output()
	if err != nil {
		return "", fmt.Errorf("the database password command failed: %w: %s", err, strings.TrimSpace(stderr.String()))
	}

	password := strings.TrimRight(string(out), "\r\n")
	if password == "" {
		return "", fmt.Errorf("the database password command printed nothing")
	}

	return password, nil
}

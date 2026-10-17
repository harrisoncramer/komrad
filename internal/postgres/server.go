package postgres

import (
	"context"
	"errors"
	"fmt"
	"net/url"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"syscall"

	"komrad/internal/fail"

	"github.com/jackc/pgx/v5"
)

const (
	// ownerLogin is the superuser initdb creates, which owns komrad's schema.
	ownerLogin = "komrad"
	database   = "komrad"
	// startTimeout is how many seconds pg_ctl waits for the server to accept connections.
	startTimeout = "60"
	// settingsFile holds the settings komrad rewrites on every start, which postgresql.conf includes.
	settingsFile = "komrad.conf"
)

// ErrClusterExists is what Init returns when dir already holds a cluster.
var ErrClusterExists = fail.AlreadyExists("a Postgres cluster is already set up")

var errNoCluster = fail.Precondition("no Postgres is set up")

// dataDir is where the cluster of one major version lives under dir.
func dataDir(dir string) string {
	major, _, _ := strings.Cut(Version, ".")

	return filepath.Join(dir, "data-"+major)
}

// OwnerURL is the url of the login that owns komrad's database in dir.
func OwnerURL(dir, password string) string {
	return ownerURL(dir, password, database)
}

func ownerURL(dir, password, name string) string {
	return (&url.URL{
		Scheme:   "postgres",
		User:     url.UserPassword(ownerLogin, password),
		Path:     "/" + name,
		RawQuery: url.Values{"host": {dir}}.Encode(),
	}).String()
}

// Init sets up a new cluster in dir whose owner login takes password, starts it, and creates
// komrad's database.
func Init(ctx context.Context, dir, password string) error {
	if err := unpack(dir); err != nil {
		return err
	}

	unlock, err := lock(dir)
	if err != nil {
		return err
	}
	defer unlock()

	data := dataDir(dir)
	if _, err := os.Stat(data); err == nil {
		return fmt.Errorf("%w in %s", ErrClusterExists, data)
	}

	if err := initdb(ctx, dir, data, password); err != nil {
		return err
	}

	if err := start(ctx, dir); err != nil {
		return err
	}

	return createDatabase(ctx, dir, password)
}

// Up starts the Postgres in dir unless it is already running.
func Up(ctx context.Context, dir string) error {
	if err := unpack(dir); err != nil {
		return err
	}

	unlock, err := lock(dir)
	if err != nil {
		return err
	}
	defer unlock()

	if IsRunning(ctx, dir) {
		return nil
	}

	if _, err := os.Stat(dataDir(dir)); err != nil {
		return fmt.Errorf("%w in %s, so run komrad setup", errNoCluster, dir)
	}

	return start(ctx, dir)
}

// lock holds the lock every start and init in dir takes, across processes.
func lock(dir string) (func(), error) {
	file, err := os.OpenFile(filepath.Join(dir, "lock"), os.O_CREATE|os.O_RDWR, 0o600)
	if err != nil {
		return nil, fmt.Errorf("failed to open the Postgres lock in %s: %w", dir, err)
	}

	if err := syscall.Flock(int(file.Fd()), syscall.LOCK_EX); err != nil {
		return nil, errors.Join(fmt.Errorf("failed to take the Postgres lock in %s: %w", dir, err), file.Close())
	}

	return func() { _ = file.Close() }, nil
}

// initdb creates the cluster in a staging directory and moves it into data once it is whole.
func initdb(ctx context.Context, dir, data, password string) error {
	staging := data + ".init"
	if err := os.RemoveAll(staging); err != nil {
		return fmt.Errorf("failed to clear %s: %w", staging, err)
	}

	passwordFile := filepath.Join(dir, ".password")
	if err := os.WriteFile(passwordFile, []byte(password+"\n"), 0o600); err != nil {
		return fmt.Errorf("failed to write the owner's password for initdb: %w", err)
	}
	defer os.Remove(passwordFile) //nolint:errcheck // a leftover holds a password komrad's config already holds

	if err := run(ctx, program(dir, "initdb"),
		"-D", staging,
		"-U", ownerLogin,
		"--pwfile", passwordFile,
		"-A", "scram-sha-256",
		"--encoding", "UTF8",
		"--locale-provider", "builtin",
		"--builtin-locale", "C.UTF-8",
	); err != nil {
		return err
	}

	include := fmt.Sprintf("\ninclude_if_exists = '%s'\n", settingsFile)
	if err := appendFile(filepath.Join(staging, "postgresql.conf"), include); err != nil {
		return err
	}

	if err := os.Rename(staging, data); err != nil { //nolint:forbidigo // a directory, which xfile.WriteAtomic does not move
		return fmt.Errorf("failed to move the new cluster into %s: %w", data, err)
	}

	return nil
}

// start writes the settings that keep Postgres off the network and on the socket in dir,
// then starts it and waits for it to accept connections.
func start(ctx context.Context, dir string) error {
	data := dataDir(dir)
	if err := os.WriteFile(filepath.Join(data, settingsFile), []byte(settings(dir)), 0o600); err != nil {
		return fmt.Errorf("failed to write %s: %w", settingsFile, err)
	}

	return run(ctx, program(dir, "pg_ctl"),
		"start",
		"-D", data,
		"-l", filepath.Join(dir, "postgres.log"),
		"-w",
		"-t", startTimeout,
	)
}

// settings is the configuration file that listens on no address, only on the socket in dir.
func settings(dir string) string {
	directories := `"` + strings.ReplaceAll(dir, `"`, `""`) + `"`

	return strings.Join([]string{
		"listen_addresses = ''",
		"port = " + port,
		"unix_socket_directories = '" + strings.ReplaceAll(directories, "'", "''") + "'",
		"",
	}, "\n")
}

func createDatabase(ctx context.Context, dir, password string) error {
	conn, err := pgx.Connect(ctx, ownerURL(dir, password, "postgres"))
	if err != nil {
		return fmt.Errorf("failed to connect to the new Postgres: %w", err)
	}
	defer conn.Close(ctx)

	if _, err := conn.Exec(ctx, "CREATE DATABASE "+database); err != nil {
		return fmt.Errorf("failed to create the %s database: %w", database, err)
	}

	return nil
}

func run(ctx context.Context, name string, args ...string) error {
	cmd := exec.CommandContext(ctx, name, args...)
	cmd.Env = environment()

	out, err := cmd.CombinedOutput()
	if err != nil {
		return fmt.Errorf("%s failed: %w: %s", filepath.Base(name), err, strings.TrimSpace(string(out)))
	}

	return nil
}

// environment is this process's environment without the PG variables that would point
// Postgres's own programs somewhere else.
func environment() []string {
	kept := make([]string, 0, len(os.Environ()))
	for _, variable := range os.Environ() {
		if !strings.HasPrefix(variable, "PG") {
			kept = append(kept, variable)
		}
	}

	return kept
}

func appendFile(path, text string) error {
	file, err := os.OpenFile(path, os.O_APPEND|os.O_WRONLY, 0)
	if err != nil {
		return err
	}

	if _, err := file.WriteString(text); err != nil {
		return errors.Join(err, file.Close())
	}

	return file.Close()
}

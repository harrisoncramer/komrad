package postgres

import (
	"context"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"strings"
	"syscall"

	"komrad/config"
	"komrad/internal/command"
	"komrad/internal/fail"

	"github.com/jackc/pgx/v5"
)

const (
	// startTimeout is how many seconds pg_ctl waits for the server to accept connections.
	startTimeout = "60"
	// settingsFile holds the settings komrad rewrites on every start, which postgresql.conf includes.
	settingsFile = "komrad.conf"
	// signInFile holds the sign-in rule komrad rewrites on every start.
	signInFile = "pg_hba.conf"
	// signInRule lets every login in over the socket without a password, since only the person can
	// open the directory that holds it.
	signInRule = "local all all trust\n" //nolint:dupword // the database and the login columns both say all
	// maxConnections is how many connections the embedded Postgres accepts at once.
	maxConnections = "300"
)

// ErrClusterExists is what CreateCluster returns when dir already holds a cluster.
var ErrClusterExists = fail.AlreadyExists("an embedded Postgres cluster is already set up")

var errNoCluster = fail.Precondition("no embedded Postgres is set up")

var errRunAsRoot = fail.Precondition("the embedded Postgres refuses to run as root, so run komrad as another login, or keep the database in a Postgres you already run with komrad setup --no-embedded-postgres")

var errCannotRun = fail.Precondition("this machine cannot run the embedded Postgres, which needs glibc 2.28 or newer on Linux or macOS 12 or newer, so keep the database in a Postgres you already run with komrad setup --no-embedded-postgres")

// CreateCluster sets up a new cluster in dir, starts it, and creates komrad's database.
func CreateCluster(ctx context.Context, dir string) error {
	unlock, err := lockAndUnpack(dir)
	if err != nil {
		return fmt.Errorf("failed to create a cluster in %s: %w", dir, err)
	}
	defer unlock()

	data := getDataDir(dir)
	if HasCluster(dir) {
		return fmt.Errorf("%w in %s", ErrClusterExists, data)
	}

	if err := runInitdb(ctx, dir, data); err != nil {
		return fmt.Errorf("failed to create a cluster in %s: %w", data, err)
	}

	if err := startServer(ctx, dir); err != nil {
		return errors.Join(fmt.Errorf("failed to start the new cluster in %s: %w", data, err), stopAndRemoveCluster(context.WithoutCancel(ctx), dir))
	}

	if err := createDatabase(ctx, dir); err != nil {
		return errors.Join(fmt.Errorf("failed to set up the new cluster in %s: %w", dir, err), stopAndRemoveCluster(context.WithoutCancel(ctx), dir))
	}

	if err := removeStalePrograms(dir); err != nil {
		return fmt.Errorf("failed to set up the new cluster in %s: %w", dir, err)
	}

	return nil
}

// RemoveCluster stops the embedded Postgres in dir and deletes its cluster, with every row in it.
func RemoveCluster(ctx context.Context, dir string) error {
	unlock, err := takeLock(dir)
	if err != nil {
		return fmt.Errorf("failed to remove the cluster in %s: %w", dir, err)
	}
	defer unlock()

	return stopAndRemoveCluster(ctx, dir)
}

// Start starts the embedded Postgres in dir, has it reload its settings when it is already running, and
// restarts it on this build's programs when it runs the programs another build unpacked.
func Start(ctx context.Context, dir string) error {
	unlock, err := lockAndUnpack(dir)
	if err != nil {
		return fmt.Errorf("failed to start the embedded Postgres in %s: %w", dir, err)
	}
	defer unlock()

	stale, err := listStalePrograms(dir)
	if err != nil {
		return fmt.Errorf("failed to start the embedded Postgres in %s: %w", dir, err)
	}

	if IsRunning(ctx, dir) {
		if len(stale) == 0 {
			if err := reloadServer(ctx, dir); err != nil {
				return fmt.Errorf("failed to reload the embedded Postgres in %s: %w", dir, err)
			}

			return nil
		}

		if err := stopServer(ctx, dir, "fast"); err != nil {
			return fmt.Errorf("failed to stop the embedded Postgres in %s to start this build's programs: %w", dir, err)
		}
	}

	if err := startStoppedServer(ctx, dir); err != nil {
		return err
	}

	if err := removeStalePrograms(dir); err != nil {
		return fmt.Errorf("failed to start the embedded Postgres in %s: %w", dir, err)
	}

	return nil
}

// EnsureRunning starts the embedded Postgres in dir when it accepts no connections, reporting whether it started it.
func EnsureRunning(ctx context.Context, dir string) (bool, error) {
	if IsRunning(ctx, dir) {
		return false, nil
	}

	unlock, err := lockAndUnpack(dir)
	if err != nil {
		return false, fmt.Errorf("failed to start the embedded Postgres in %s: %w", dir, err)
	}
	defer unlock()

	if IsRunning(ctx, dir) {
		return false, nil
	}

	if err := startStoppedServer(ctx, dir); err != nil {
		return false, err
	}

	return true, nil
}

// startStoppedServer starts the stopped embedded Postgres in dir, clearing the lock files it left before the machine last booted.
func startStoppedServer(ctx context.Context, dir string) error {
	if !HasCluster(dir) {
		return fmt.Errorf("%w in %s, so run komrad setup", errNoCluster, dir)
	}

	if err := removeStaleLockFiles(dir); err != nil {
		return fmt.Errorf("failed to clear the stale lock files in %s: %w", dir, err)
	}

	if err := startServer(ctx, dir); err != nil {
		return fmt.Errorf("failed to start the embedded Postgres in %s: %w", dir, err)
	}

	return nil
}

// HasCluster reports whether dir holds a cluster for the embedded Postgres this build carries.
func HasCluster(dir string) bool {
	_, err := os.Stat(getDataDir(dir))

	return err == nil
}

// getDataDir is where the cluster of one major version lives under dir.
func getDataDir(dir string) string {
	return filepath.Join(dir, "data-"+getMajorVersion())
}

func stopAndRemoveCluster(ctx context.Context, dir string) error {
	data := getDataDir(dir)
	if _, err := os.Stat(getPIDFilePath(dir)); err == nil {
		if err := stopServer(ctx, dir, "immediate"); err != nil {
			return fmt.Errorf("failed to stop the embedded Postgres in %s: %w", data, err)
		}
	}

	if err := os.RemoveAll(data); err != nil {
		return fmt.Errorf("failed to remove %s: %w", data, err)
	}

	return nil
}

// lockAndUnpack takes the lock in dir and unpacks the embedded Postgres programs under it.
func lockAndUnpack(dir string) (func(), error) {
	if os.Geteuid() == 0 {
		return nil, errRunAsRoot
	}

	if err := os.MkdirAll(dir, 0o700); err != nil {
		return nil, fmt.Errorf("failed to create %s: %w", dir, err)
	}

	unlock, err := takeLock(dir)
	if err != nil {
		return nil, fmt.Errorf("failed to lock %s: %w", dir, err)
	}

	if err := unpackEmbeddedPrograms(dir); err != nil {
		unlock()

		return nil, fmt.Errorf("failed to unpack the embedded Postgres into %s: %w", dir, err)
	}

	return unlock, nil
}

// takeLock holds the lock every start and init in dir takes, across processes.
func takeLock(dir string) (func(), error) {
	file, err := os.OpenFile(filepath.Join(dir, "lock"), os.O_CREATE|os.O_RDWR, 0o600)
	if err != nil {
		return nil, fmt.Errorf("failed to open the embedded Postgres lock in %s: %w", dir, err)
	}

	if err := syscall.Flock(int(file.Fd()), syscall.LOCK_EX); err != nil {
		return nil, errors.Join(fmt.Errorf("failed to take the embedded Postgres lock in %s: %w", dir, err), file.Close())
	}

	return func() { _ = file.Close() }, nil
}

// runInitdb creates the cluster in a staging directory and moves it into data once it is whole.
func runInitdb(ctx context.Context, dir, data string) error {
	staging := data + ".init"
	if err := os.RemoveAll(staging); err != nil {
		return fmt.Errorf("failed to clear %s: %w", staging, err)
	}

	if err := runProgram(ctx, getProgramPath(dir, "initdb"),
		"-D", staging,
		"-U", config.EmbeddedPostgresOwner,
		"-A", "trust",
		"--encoding", "UTF8",
		"--no-locale",
		"--locale-provider", "builtin",
		"--builtin-locale", "C.UTF-8",
	); err != nil {
		return fmt.Errorf("failed to initialise %s: %w", staging, err)
	}

	include := fmt.Sprintf("\ninclude_if_exists = '%s'\n", settingsFile)
	if err := appendFile(filepath.Join(staging, "postgresql.conf"), include); err != nil {
		return fmt.Errorf("failed to include %s from postgresql.conf: %w", settingsFile, err)
	}

	if err := os.Rename(staging, data); err != nil { //nolint:forbidigo // a directory, which xfile.WriteAtomic does not move
		return fmt.Errorf("failed to move the new cluster into %s: %w", data, err)
	}

	return nil
}

// removeStaleLockFiles deletes the lock files the embedded Postgres in dir wrote before the machine last booted.
func removeStaleLockFiles(dir string) error {
	boot, err := readBootTime()
	if err != nil {
		return fmt.Errorf("failed to read when the machine booted: %w", err)
	}

	for _, path := range getLockFilePaths(dir) {
		info, err := os.Stat(path)
		if errors.Is(err, fs.ErrNotExist) {
			continue
		}
		if err != nil {
			return fmt.Errorf("failed to read %s: %w", path, err)
		}

		if info.ModTime().After(*boot) {
			continue
		}

		if err := os.Remove(path); err != nil {
			return fmt.Errorf("failed to remove the stale %s: %w", path, err)
		}
	}

	return nil
}

// startServer writes the settings of the embedded Postgres in dir, then starts it and waits for it to
// accept connections.
func startServer(ctx context.Context, dir string) error {
	if err := writeSettings(dir); err != nil {
		return err
	}

	return runProgram(ctx, getProgramPath(dir, "pg_ctl"),
		"start",
		"-D", getDataDir(dir),
		"-l", filepath.Join(dir, "postgres.log"),
		"-w",
		"-t", startTimeout,
	)
}

// reloadServer writes the settings of the running embedded Postgres in dir and has it read them again.
func reloadServer(ctx context.Context, dir string) error {
	if err := writeSettings(dir); err != nil {
		return err
	}

	return runProgram(ctx, getProgramPath(dir, "pg_ctl"), "reload", "-D", getDataDir(dir))
}

// stopServer stops the embedded Postgres in dir with the shutdown mode pg_ctl names, and waits for it to exit.
func stopServer(ctx context.Context, dir, mode string) error {
	return runProgram(ctx, getProgramPath(dir, "pg_ctl"), "stop", "-D", getDataDir(dir), "-m", mode, "-w")
}

// writeSettings writes the settings that keep the embedded Postgres in dir off the network and let every
// login in over its socket.
func writeSettings(dir string) error {
	data := getDataDir(dir)
	if err := os.WriteFile(filepath.Join(data, settingsFile), []byte(buildSettingsFile(dir)), 0o600); err != nil {
		return fmt.Errorf("failed to write %s: %w", settingsFile, err)
	}

	if err := os.WriteFile(filepath.Join(data, signInFile), []byte(signInRule), 0o600); err != nil {
		return fmt.Errorf("failed to write %s: %w", signInFile, err)
	}

	return nil
}

// buildSettingsFile is the configuration file that listens on no address, only on the socket in dir.
func buildSettingsFile(dir string) string {
	directories := `"` + strings.ReplaceAll(dir, `"`, `""`) + `"`

	return strings.Join([]string{
		"listen_addresses = ''",
		"port = " + config.EmbeddedPostgresPort,
		"unix_socket_directories = '" + strings.ReplaceAll(directories, "'", "''") + "'",
		"max_connections = " + maxConnections,
		"",
	}, "\n")
}

func createDatabase(ctx context.Context, dir string) error {
	conn, err := pgx.Connect(ctx, config.BuildEmbeddedPostgresURL(dir, config.EmbeddedPostgresOwner, "postgres"))
	if err != nil {
		return fmt.Errorf("failed to connect to the new embedded Postgres: %w", err)
	}
	defer conn.Close(ctx)

	if _, err := conn.Exec(ctx, "CREATE DATABASE "+config.EmbeddedPostgresDatabase); err != nil {
		return fmt.Errorf("failed to create the %s database: %w", config.EmbeddedPostgresDatabase, err)
	}

	return nil
}

func runProgram(ctx context.Context, path string, args ...string) error {
	if err := command.Run(ctx, command.RunParams{
		Path: path,
		Args: args,
		Env:  getEnvironmentWithoutPG(),
	}); err != nil {
		if errors.Is(err, fs.ErrNotExist) {
			return fmt.Errorf("failed to run %s: %w: %w", filepath.Base(path), errCannotRun, err)
		}

		return fmt.Errorf("failed to run %s: %w", filepath.Base(path), err)
	}

	return nil
}

// getEnvironmentWithoutPG is this process's environment without the PG variables that would point
// the embedded Postgres programs somewhere else.
func getEnvironmentWithoutPG() []string {
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
		return fmt.Errorf("failed to open %s: %w", path, err)
	}

	if _, err := file.WriteString(text); err != nil {
		return errors.Join(fmt.Errorf("failed to append to %s: %w", path, err), file.Close())
	}

	return file.Close()
}

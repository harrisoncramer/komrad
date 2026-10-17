package postgres

import (
	"context"
	"errors"
	"fmt"
	"io/fs"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"syscall"

	"komrad/internal/command"
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

// ErrClusterExists is what CreateCluster returns when dir already holds a cluster.
var ErrClusterExists = fail.AlreadyExists("a Postgres cluster is already set up")

var errNoCluster = fail.Precondition("no Postgres is set up")

// BuildOwnerURL is the url of the login that owns komrad's database in dir.
func BuildOwnerURL(dir, password string) string {
	return buildOwnerURL(dir, password, database)
}

// CreateCluster sets up a new cluster in dir whose owner login takes password, starts it, and creates
// komrad's database.
func CreateCluster(ctx context.Context, dir, password string) error {
	unlock, err := lockAndUnpack(dir)
	if err != nil {
		return fmt.Errorf("failed to create a cluster in %s: %w", dir, err)
	}
	defer unlock()

	data := getDataDir(dir)
	if HasCluster(dir) {
		return fmt.Errorf("%w in %s", ErrClusterExists, data)
	}

	if err := runInitdb(ctx, dir, data, password); err != nil {
		return fmt.Errorf("failed to create a cluster in %s: %w", data, err)
	}

	if err := startServer(ctx, dir); err != nil {
		return errors.Join(fmt.Errorf("failed to start the new cluster in %s: %w", data, err), stopAndRemoveCluster(context.WithoutCancel(ctx), dir))
	}

	if err := createDatabase(ctx, dir, password); err != nil {
		return errors.Join(fmt.Errorf("failed to set up the new cluster in %s: %w", dir, err), stopAndRemoveCluster(context.WithoutCancel(ctx), dir))
	}

	return nil
}

// RemoveCluster stops the Postgres in dir and deletes its cluster, with every row in it.
func RemoveCluster(ctx context.Context, dir string) error {
	unlock, err := takeLock(dir)
	if err != nil {
		return fmt.Errorf("failed to remove the cluster in %s: %w", dir, err)
	}
	defer unlock()

	return stopAndRemoveCluster(ctx, dir)
}

// Start starts the Postgres in dir unless it is already running.
func Start(ctx context.Context, dir string) error {
	unlock, err := lockAndUnpack(dir)
	if err != nil {
		return fmt.Errorf("failed to start Postgres in %s: %w", dir, err)
	}
	defer unlock()

	if IsRunning(ctx, dir) {
		return nil
	}

	if !HasCluster(dir) {
		return fmt.Errorf("%w in %s, so move your config file aside and run komrad setup", errNoCluster, dir)
	}

	if err := removeStaleLockFiles(dir); err != nil {
		return fmt.Errorf("failed to clear the stale lock files in %s: %w", dir, err)
	}

	if err := startServer(ctx, dir); err != nil {
		return fmt.Errorf("failed to start Postgres in %s: %w", dir, err)
	}

	return nil
}

// HasCluster reports whether dir holds a cluster for the Postgres this build carries.
func HasCluster(dir string) bool {
	_, err := os.Stat(getDataDir(dir))

	return err == nil
}

// getDataDir is where the cluster of one major version lives under dir.
func getDataDir(dir string) string {
	major, _, _ := strings.Cut(Version, ".")

	return filepath.Join(dir, "data-"+major)
}

func buildOwnerURL(dir, password, name string) string {
	return (&url.URL{
		Scheme:   "postgres",
		User:     url.UserPassword(ownerLogin, password),
		Path:     "/" + name,
		RawQuery: url.Values{"host": {dir}, "port": {port}}.Encode(),
	}).String()
}

func stopAndRemoveCluster(ctx context.Context, dir string) error {
	data := getDataDir(dir)
	if _, err := os.Stat(getPIDFilePath(dir)); err == nil {
		if err := runProgram(ctx, getProgramPath(dir, "pg_ctl"), "stop", "-D", data, "-m", "immediate", "-w"); err != nil {
			return fmt.Errorf("failed to stop Postgres in %s: %w", data, err)
		}
	}

	if err := os.RemoveAll(data); err != nil {
		return fmt.Errorf("failed to remove %s: %w", data, err)
	}

	return nil
}

// lockAndUnpack takes the lock in dir and unpacks the bundled programs under it.
func lockAndUnpack(dir string) (func(), error) {
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return nil, fmt.Errorf("failed to create %s: %w", dir, err)
	}

	unlock, err := takeLock(dir)
	if err != nil {
		return nil, fmt.Errorf("failed to lock %s: %w", dir, err)
	}

	if err := unpackBundledPrograms(dir); err != nil {
		unlock()

		return nil, fmt.Errorf("failed to unpack Postgres into %s: %w", dir, err)
	}

	return unlock, nil
}

// takeLock holds the lock every start and init in dir takes, across processes.
func takeLock(dir string) (func(), error) {
	file, err := os.OpenFile(filepath.Join(dir, "lock"), os.O_CREATE|os.O_RDWR, 0o600)
	if err != nil {
		return nil, fmt.Errorf("failed to open the Postgres lock in %s: %w", dir, err)
	}

	if err := syscall.Flock(int(file.Fd()), syscall.LOCK_EX); err != nil {
		return nil, errors.Join(fmt.Errorf("failed to take the Postgres lock in %s: %w", dir, err), file.Close())
	}

	return func() { _ = file.Close() }, nil
}

// runInitdb creates the cluster in a staging directory and moves it into data once it is whole.
func runInitdb(ctx context.Context, dir, data, password string) error {
	staging := data + ".init"
	if err := os.RemoveAll(staging); err != nil {
		return fmt.Errorf("failed to clear %s: %w", staging, err)
	}

	passwordFile := filepath.Join(dir, ".password")
	if err := os.WriteFile(passwordFile, []byte(password+"\n"), 0o600); err != nil {
		return fmt.Errorf("failed to write the owner's password for initdb: %w", err)
	}
	defer os.Remove(passwordFile) //nolint:errcheck // a leftover holds a password komrad's config already holds

	if err := runProgram(ctx, getProgramPath(dir, "initdb"),
		"-D", staging,
		"-U", ownerLogin,
		"--pwfile", passwordFile,
		"-A", "scram-sha-256",
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

// removeStaleLockFiles deletes the lock files a Postgres in dir wrote before the machine last booted.
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

// startServer writes the settings that keep Postgres off the network and on the socket in dir,
// then starts it and waits for it to accept connections.
func startServer(ctx context.Context, dir string) error {
	data := getDataDir(dir)
	if err := os.WriteFile(filepath.Join(data, settingsFile), []byte(buildSettingsFile(dir)), 0o600); err != nil {
		return fmt.Errorf("failed to write %s: %w", settingsFile, err)
	}

	return runProgram(ctx, getProgramPath(dir, "pg_ctl"),
		"start",
		"-D", data,
		"-l", filepath.Join(dir, "postgres.log"),
		"-w",
		"-t", startTimeout,
	)
}

// buildSettingsFile is the configuration file that listens on no address, only on the socket in dir.
func buildSettingsFile(dir string) string {
	directories := `"` + strings.ReplaceAll(dir, `"`, `""`) + `"`

	return strings.Join([]string{
		"listen_addresses = ''",
		"port = " + port,
		"unix_socket_directories = '" + strings.ReplaceAll(directories, "'", "''") + "'",
		"",
	}, "\n")
}

func createDatabase(ctx context.Context, dir, password string) error {
	conn, err := pgx.Connect(ctx, buildOwnerURL(dir, password, "postgres"))
	if err != nil {
		return fmt.Errorf("failed to connect to the new Postgres: %w", err)
	}
	defer conn.Close(ctx)

	if _, err := conn.Exec(ctx, "CREATE DATABASE "+database); err != nil {
		return fmt.Errorf("failed to create the %s database: %w", database, err)
	}

	return nil
}

func runProgram(ctx context.Context, path string, args ...string) error {
	if err := command.Run(ctx, command.RunParams{
		Path: path,
		Args: args,
		Env:  getEnvironmentWithoutPG(),
	}); err != nil {
		return fmt.Errorf("failed to run %s: %w", filepath.Base(path), err)
	}

	return nil
}

// getEnvironmentWithoutPG is this process's environment without the PG variables that would point
// Postgres's own programs somewhere else.
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

//go:build integration

package postgres

import (
	"context"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"syscall"
	"testing"
	"time"

	"komrad/config"
	"komrad/db"
	"komrad/internal/store"

	"github.com/jackc/pgx/v5"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

const (
	testPassword = "komrad-test"
	testWait     = 10 * time.Second
	testTick     = 50 * time.Millisecond
)

// TestIntegrationBundleHoldsWhatKomradRuns covers the programs and extension komrad needs being in the build.
func TestIntegrationBundleHoldsWhatKomradRuns(t *testing.T) {
	require.True(t, IsEmbedded(), "integration runs build with the postgres tag")

	dir := t.TempDir()
	require.NoError(t, unpackEmbeddedPrograms(dir))

	for _, name := range []string{"bin/postgres", "bin/initdb", "bin/pg_ctl", "bin/pg_upgrade", "share/extension/pg_trgm.control"} {
		assert.FileExists(t, filepath.Join(getUnpackedDir(dir), name))
	}
}

// TestIntegrationCreateClusterListensOnlyOnTheSocket covers a new cluster answering the owner
// over its socket without a password and nobody over the network, whatever port and locale the
// environment names.
func TestIntegrationCreateClusterListensOnlyOnTheSocket(t *testing.T) {
	t.Setenv("PGPORT", "1")
	t.Setenv("LC_ALL", "missing.UTF-8")
	dir := createCluster(t)
	conn := connect(t, buildURL(dir, config.EmbeddedPostgresOwner))

	var listen, connections string
	require.NoError(t, conn.QueryRow(t.Context(), "SHOW listen_addresses").Scan(&listen))
	assert.Empty(t, listen)
	require.NoError(t, conn.QueryRow(t.Context(), "SHOW max_connections").Scan(&connections))
	assert.Equal(t, maxConnections, connections)

	_, err := conn.Exec(t.Context(), "CREATE EXTENSION pg_trgm")
	require.NoError(t, err)
}

// TestIntegrationStartReloadsTheSignInRule covers Start letting every login in over the socket of an
// embedded Postgres that is already running under a rule asking for a password.
func TestIntegrationStartReloadsTheSignInRule(t *testing.T) {
	dir := createCluster(t)
	require.NoError(t, os.WriteFile(filepath.Join(getDataDir(dir), signInFile), []byte("local all all scram-sha-256\n"), 0o600)) //nolint:dupword // the database and the login columns both say all
	require.NoError(t, runProgram(t.Context(), getProgramPath(dir, "pg_ctl"), "reload", "-D", getDataDir(dir)))
	require.Eventually(t, func() bool {
		conn, err := pgx.Connect(t.Context(), buildURL(dir, config.EmbeddedPostgresOwner))
		if err == nil {
			_ = conn.Close(t.Context())
		}

		return err != nil
	}, testWait, testTick)

	require.NoError(t, Start(t.Context(), dir))

	require.Eventually(t, func() bool {
		conn, err := pgx.Connect(t.Context(), buildURL(dir, config.EmbeddedPostgresOwner))
		if err != nil {
			return false
		}

		return conn.Close(t.Context()) == nil
	}, testWait, testTick)
}

// TestIntegrationStartKeepsOnePostgres covers Start leaving a running embedded Postgres alone, however many
// processes ask at once.
func TestIntegrationStartKeepsOnePostgres(t *testing.T) {
	dir := createCluster(t)
	pid := postmasterPID(t, dir)

	require.NoError(t, Start(t.Context(), dir))
	assert.Equal(t, pid, postmasterPID(t, dir))

	require.NoError(t, runProgram(t.Context(), getProgramPath(dir, "pg_ctl"), "stop", "-D", getDataDir(dir), "-w"))

	var wg sync.WaitGroup
	errs := make([]error, 2)
	for i := range errs {
		wg.Go(func() { errs[i] = Start(t.Context(), dir) })
	}
	wg.Wait()

	for _, err := range errs {
		require.NoError(t, err)
	}
	assert.True(t, IsRunning(t.Context(), dir))
}

// TestIntegrationStartReplacesAnotherBuildsPrograms covers a server running the programs another build
// unpacked being restarted on this build's programs, and the other build's programs being removed.
func TestIntegrationStartReplacesAnotherBuildsPrograms(t *testing.T) {
	dir := createCluster(t)
	require.NoError(t, runProgram(t.Context(), getProgramPath(dir, "pg_ctl"), "stop", "-D", getDataDir(dir), "-w"))

	other := filepath.Join(dir, Version+"-other")
	require.NoError(t, os.Rename(getUnpackedDir(dir), other)) //nolint:forbidigo // a directory, which xfile.WriteAtomic does not move
	require.NoError(t, runProgram(t.Context(), filepath.Join(other, "bin", "pg_ctl"), "start", "-D", getDataDir(dir), "-l", filepath.Join(dir, "postgres.log"), "-w"))
	pid := postmasterPID(t, dir)

	require.NoError(t, Start(t.Context(), dir))

	assert.NotEqual(t, pid, postmasterPID(t, dir))
	assert.True(t, IsRunning(t.Context(), dir))
	assert.NoDirExists(t, other)
	assert.DirExists(t, getUnpackedDir(dir))
}

// TestIntegrationStartRecoversFromACrash covers rows surviving an embedded Postgres killed without warning, and
// lock files from before a reboot whose PID a live process now holds.
func TestIntegrationStartRecoversFromACrash(t *testing.T) {
	dir := createCluster(t)

	func() {
		conn, err := pgx.Connect(t.Context(), buildURL(dir, config.EmbeddedPostgresOwner))
		require.NoError(t, err)
		defer conn.Close(t.Context())

		_, err = conn.Exec(t.Context(), "CREATE TABLE kept (id int); INSERT INTO kept VALUES (1)")
		require.NoError(t, err)
	}()

	require.NoError(t, syscall.Kill(-postmasterPID(t, dir), syscall.SIGKILL))
	require.Eventually(t, func() bool { return !IsRunning(t.Context(), dir) }, testWait, testTick)

	livePID := postmasterPID(t, createCluster(t))

	for _, path := range getLockFilePaths(dir) {
		contents, err := os.ReadFile(path)
		require.NoError(t, err)

		_, rest, _ := strings.Cut(string(contents), "\n")
		require.NoError(t, os.WriteFile(path, []byte(strconv.Itoa(livePID)+"\n"+rest), 0o600))
		require.NoError(t, os.Chtimes(path, time.Unix(0, 0), time.Unix(0, 0)))
	}

	require.NoError(t, Start(t.Context(), dir))

	var count int
	require.NoError(t, connect(t, buildURL(dir, config.EmbeddedPostgresOwner)).QueryRow(t.Context(), "SELECT count(*) FROM kept").Scan(&count))
	assert.Equal(t, 1, count)
}

// TestIntegrationCreateClusterRefusesASecondCluster covers CreateCluster never writing over a cluster that is there.
func TestIntegrationCreateClusterRefusesASecondCluster(t *testing.T) {
	dir := createCluster(t)

	require.ErrorIs(t, CreateCluster(t.Context(), dir), ErrClusterExists)
}

// TestIntegrationCreateClusterMakesOneClusterWhenTwoAskAtOnce covers two first starts unpacking and initialising one dir together.
func TestIntegrationCreateClusterMakesOneClusterWhenTwoAskAtOnce(t *testing.T) {
	dir, err := os.MkdirTemp("/tmp", "komrad-pg") //nolint:usetesting // a socket path under the test directory runs past the length unix allows
	require.NoError(t, err)
	t.Cleanup(func() { require.NoError(t, os.RemoveAll(dir)) })
	t.Cleanup(func() { _ = RemoveCluster(context.WithoutCancel(t.Context()), dir) })

	var wg sync.WaitGroup
	errs := make([]error, 2)
	for i := range errs {
		wg.Go(func() { errs[i] = CreateCluster(t.Context(), dir) })
	}
	wg.Wait()

	var created int
	for _, err := range errs {
		if err == nil {
			created++

			continue
		}
		require.ErrorIs(t, err, ErrClusterExists)
	}
	assert.Equal(t, 1, created)
	assert.True(t, IsRunning(t.Context(), dir))
}

// TestIntegrationRemoveClusterLeavesRoomForANewOne covers a removed cluster being stopped and gone, so CreateCluster can start over.
func TestIntegrationRemoveClusterLeavesRoomForANewOne(t *testing.T) {
	dir := createCluster(t)

	require.NoError(t, RemoveCluster(t.Context(), dir))

	assert.False(t, IsRunning(t.Context(), dir))
	assert.NoDirExists(t, getDataDir(dir))
	require.NoError(t, CreateCluster(t.Context(), dir))
}

// TestIntegrationEngineConnectsOverTheSocket covers komrad's schema and an engine's login working in
// the embedded Postgres.
func TestIntegrationEngineConnectsOverTheSocket(t *testing.T) {
	dir := createCluster(t)
	owner := buildURL(dir, config.EmbeddedPostgresOwner)

	require.NoError(t, store.Migrate(t.Context(), owner))
	require.NoError(t, store.EnsureEngine(t.Context(), owner, db.EnsureEngineParams{
		Name:     "default",
		Password: testPassword,
	}))

	pool, err := store.Open(t.Context(), buildURL(dir, db.EngineLogin("default")))
	require.NoError(t, err)
	t.Cleanup(func() { require.NoError(t, pool.Close()) })

	assert.Equal(t, "default", pool.GetEngine().Name)
}

// TestIntegrationRunProgramNamesWhatTheMachineLacks covers a program the machine has no loader for, as on a
// Linux without glibc.
func TestIntegrationRunProgramNamesWhatTheMachineLacks(t *testing.T) {
	path := filepath.Join(t.TempDir(), "initdb")
	require.NoError(t, os.WriteFile(path, []byte("#!/missing/loader\n"), 0o700))

	assert.ErrorIs(t, runProgram(t.Context(), path), errCannotRun)
}

func createCluster(t *testing.T) string {
	t.Helper()

	dir, err := os.MkdirTemp("/tmp", "komrad-pg") //nolint:usetesting // a socket path under the test directory runs past the length unix allows
	require.NoError(t, err)
	t.Cleanup(func() { require.NoError(t, os.RemoveAll(dir)) })

	require.NoError(t, CreateCluster(t.Context(), dir))
	t.Cleanup(func() {
		_ = runProgram(context.WithoutCancel(t.Context()), getProgramPath(dir, "pg_ctl"), "stop", "-D", getDataDir(dir), "-m", "immediate")
	})

	return dir
}

func buildURL(dir, login string) string {
	return config.BuildEmbeddedPostgresURL(dir, login, config.EmbeddedPostgresDatabase)
}

func connect(t *testing.T, url string) *pgx.Conn {
	t.Helper()

	conn, err := pgx.Connect(t.Context(), url)
	require.NoError(t, err)
	t.Cleanup(func() { require.NoError(t, conn.Close(t.Context())) })

	return conn
}

func postmasterPID(t *testing.T, dir string) int {
	t.Helper()

	contents, err := os.ReadFile(getPIDFilePath(dir))
	require.NoError(t, err)

	first, _, _ := strings.Cut(string(contents), "\n")
	pid, err := strconv.Atoi(first)
	require.NoError(t, err)

	return pid
}

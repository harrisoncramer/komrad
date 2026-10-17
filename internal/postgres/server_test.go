//go:build integration

package postgres

import (
	"context"
	"net/url"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"syscall"
	"testing"
	"time"

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
	require.True(t, IsBundled(), "integration runs build with the postgres tag")

	dir := t.TempDir()
	require.NoError(t, unpackBundledPrograms(dir))

	for _, name := range []string{"bin/postgres", "bin/initdb", "bin/pg_ctl", "bin/pg_upgrade", "share/extension/pg_trgm.control"} {
		assert.FileExists(t, filepath.Join(getUnpackedDir(dir, Version), name))
	}
}

// TestIntegrationCreateClusterStartsAPostgresOnlyTheSocketReaches covers a new cluster answering the owner
// over its socket and nobody over the network or without a password, whatever port and locale the
// environment names.
func TestIntegrationCreateClusterStartsAPostgresOnlyTheSocketReaches(t *testing.T) {
	t.Setenv("PGPORT", "1")
	t.Setenv("LC_ALL", "missing.UTF-8")
	dir := initCluster(t)
	conn := connect(t, BuildOwnerURL(dir, testPassword))

	var listen string
	require.NoError(t, conn.QueryRow(t.Context(), "SHOW listen_addresses").Scan(&listen))
	assert.Empty(t, listen)

	_, err := conn.Exec(t.Context(), "CREATE EXTENSION pg_trgm")
	require.NoError(t, err)

	_, err = pgx.Connect(t.Context(), BuildOwnerURL(dir, ""))
	require.Error(t, err, "a login without the password is refused")
}

// TestIntegrationStartKeepsOnePostgres covers Start leaving a running Postgres alone, however many
// processes ask at once.
func TestIntegrationStartKeepsOnePostgres(t *testing.T) {
	dir := initCluster(t)
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

// TestIntegrationStartRecoversFromACrash covers rows surviving a Postgres killed without warning, and
// lock files from before a reboot whose PID a live process now holds.
func TestIntegrationStartRecoversFromACrash(t *testing.T) {
	dir := initCluster(t)

	func() {
		conn, err := pgx.Connect(t.Context(), BuildOwnerURL(dir, testPassword))
		require.NoError(t, err)
		defer conn.Close(t.Context())

		_, err = conn.Exec(t.Context(), "CREATE TABLE kept (id int); INSERT INTO kept VALUES (1)")
		require.NoError(t, err)
	}()

	require.NoError(t, syscall.Kill(-postmasterPID(t, dir), syscall.SIGKILL))
	require.Eventually(t, func() bool { return !IsRunning(t.Context(), dir) }, testWait, testTick)

	livePID := postmasterPID(t, initCluster(t))

	for _, path := range getLockFilePaths(dir) {
		contents, err := os.ReadFile(path)
		require.NoError(t, err)

		_, rest, _ := strings.Cut(string(contents), "\n")
		require.NoError(t, os.WriteFile(path, []byte(strconv.Itoa(livePID)+"\n"+rest), 0o600))
		require.NoError(t, os.Chtimes(path, time.Unix(0, 0), time.Unix(0, 0)))
	}

	require.NoError(t, Start(t.Context(), dir))

	var count int
	require.NoError(t, connect(t, BuildOwnerURL(dir, testPassword)).QueryRow(t.Context(), "SELECT count(*) FROM kept").Scan(&count))
	assert.Equal(t, 1, count)
}

// TestIntegrationCreateClusterRefusesASecondCluster covers CreateCluster never writing over a cluster that is there.
func TestIntegrationCreateClusterRefusesASecondCluster(t *testing.T) {
	dir := initCluster(t)

	require.ErrorIs(t, CreateCluster(t.Context(), dir, testPassword), ErrClusterExists)
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
		wg.Go(func() { errs[i] = CreateCluster(t.Context(), dir, testPassword) })
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

// TestIntegrationRemoveLeavesRoomForANewCluster covers a removed cluster being stopped and gone, so CreateCluster can start over.
func TestIntegrationRemoveLeavesRoomForANewCluster(t *testing.T) {
	dir := initCluster(t)

	require.NoError(t, RemoveCluster(t.Context(), dir))

	assert.False(t, IsRunning(t.Context(), dir))
	assert.NoDirExists(t, getDataDir(dir))
	require.NoError(t, CreateCluster(t.Context(), dir, testPassword))
}

// TestIntegrationEngineJoinsOverTheSocket covers komrad's schema and an engine's login working in
// the embedded Postgres.
func TestIntegrationEngineJoinsOverTheSocket(t *testing.T) {
	dir := initCluster(t)
	owner := BuildOwnerURL(dir, testPassword)

	require.NoError(t, store.Migrate(t.Context(), owner))
	require.NoError(t, store.EnsureEngine(t.Context(), owner, db.EnsureEngineParams{
		Name:     "default",
		Password: testPassword,
	}))

	engineURL, err := url.Parse(owner)
	require.NoError(t, err)
	engineURL.User = url.UserPassword(db.EngineLogin("default"), testPassword)

	pool, err := store.Open(t.Context(), engineURL.String())
	require.NoError(t, err)
	t.Cleanup(func() { require.NoError(t, pool.Close()) })

	assert.Equal(t, "default", pool.GetEngine().Name)
}

func initCluster(t *testing.T) string {
	t.Helper()

	dir, err := os.MkdirTemp("/tmp", "komrad-pg") //nolint:usetesting // a socket path under the test directory runs past the length unix allows
	require.NoError(t, err)
	t.Cleanup(func() { require.NoError(t, os.RemoveAll(dir)) })

	require.NoError(t, CreateCluster(t.Context(), dir, testPassword))
	t.Cleanup(func() {
		_ = runProgram(context.WithoutCancel(t.Context()), getProgramPath(dir, "pg_ctl"), "stop", "-D", getDataDir(dir), "-m", "immediate")
	})

	return dir
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

//go:build ignore

package main

import (
	"bytes"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"strings"
	"time"

	"github.com/spf13/cobra"

	"komrad/internal/watch"
	"komrad/internal/watched"
)

var migrationName = regexp.MustCompile(`^[a-z][a-z0-9_]*$`)

func main() {
	cmd := &cobra.Command{
		Use:           "gen NAME",
		Short:         "Write a migration that rebuilds every watch trigger",
		Args:          cobra.ExactArgs(1),
		SilenceUsage:  true,
		SilenceErrors: true,
		RunE: func(cmd *cobra.Command, args []string) error {
			return writeMigration(cmd.OutOrStdout(), args[0])
		},
	}

	if err := cmd.Execute(); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
}

func writeMigration(out io.Writer, name string) error {
	if !migrationName.MatchString(name) {
		return fmt.Errorf("migration name must be lowercase words joined by underscores, with no timestamp or extension")
	}

	path := filepath.Join("db", "migrations", time.Now().UTC().Format("20060102150405")+"_"+name+".sql")
	file, err := os.OpenFile(path, os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0o644)
	if err != nil {
		return fmt.Errorf("create %s: %w", path, err)
	}

	if _, err := file.Write(render()); err != nil {
		_ = file.Close()
		return fmt.Errorf("write %s: %w", path, err)
	}
	if err := file.Close(); err != nil {
		return fmt.Errorf("close %s: %w", path, err)
	}

	hash := exec.Command("atlas", "migrate", "hash", "--dir", "file://migrations")
	hash.Dir = "db"
	hash.Stderr = os.Stderr
	if err := hash.Run(); err != nil {
		return fmt.Errorf("hash migrations: %w", err)
	}

	_, err = fmt.Fprintln(out, path)

	return err
}

func render() []byte {
	functions := make([]string, len(watch.TriggerFunctions))
	for i, function := range watch.TriggerFunctions {
		functions[i] = "'" + function.Name + "'::regproc"
	}

	var output bytes.Buffer
	output.WriteString("SET LOCAL lock_timeout = '5s';\n\n")
	for _, function := range watch.TriggerFunctions {
		output.WriteString(function.BuildSQL())
		output.WriteString("\n")
	}
	fmt.Fprintf(&output, `DO $$
DECLARE
    stale record;
BEGIN
    FOR stale IN
    SELECT
        tgname,
        tgrelid::regclass AS target
    FROM
        pg_trigger
    WHERE
        NOT tgisinternal
        AND tgfoid IN (%s)
        LOOP
            EXECUTE FORMAT('DROP TRIGGER %%I ON %%s', stale.tgname, stale.target);
        END LOOP;
END;
$$;

`, strings.Join(functions, ", "))

	for _, trigger := range watch.BuildTriggers(watched.All()) {
		output.WriteString(trigger.BuildSQL())
	}

	return output.Bytes()
}

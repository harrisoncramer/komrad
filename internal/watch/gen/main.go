//go:build ignore

package main

import (
	"bytes"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"strings"
	"time"

	"weave/internal/watch"
)

var migrationName = regexp.MustCompile(`^[a-z][a-z0-9_]*$`)

func main() {
	if len(os.Args) != 2 {
		fmt.Fprintln(os.Stderr, "usage: go run ./internal/watch/gen/main.go <name>")
		os.Exit(2)
	}

	name := os.Args[1]
	if !migrationName.MatchString(name) {
		fmt.Fprintln(os.Stderr, "migration name must be lowercase words joined by underscores, with no timestamp or extension")
		os.Exit(2)
	}

	path := filepath.Join("db", "migrations", time.Now().UTC().Format("20060102150405")+"_"+name+".sql")
	file, err := os.OpenFile(path, os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0o644)
	if err != nil {
		fmt.Fprintf(os.Stderr, "create %s: %v\n", path, err)
		os.Exit(1)
	}

	if _, err := file.Write(render()); err != nil {
		_ = file.Close()
		fmt.Fprintf(os.Stderr, "write %s: %v\n", path, err)
		os.Exit(1)
	}
	if err := file.Close(); err != nil {
		fmt.Fprintf(os.Stderr, "close %s: %v\n", path, err)
		os.Exit(1)
	}

	hash := exec.Command("atlas", "migrate", "hash", "--dir", "file://migrations")
	hash.Dir = "db"
	hash.Stderr = os.Stderr
	if err := hash.Run(); err != nil {
		fmt.Fprintf(os.Stderr, "hash migrations: %v\n", err)
		os.Exit(1)
	}

	fmt.Println(path)
}

func render() []byte {
	functions := make([]string, len(watch.TriggerFunctions))
	for i, function := range watch.TriggerFunctions {
		functions[i] = quote(function) + "::regproc"
	}

	var output bytes.Buffer
	output.WriteString("SET LOCAL lock_timeout = '5s';\n\n")
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

	for _, trigger := range watch.Triggers() {
		args := make([]string, len(trigger.Args))
		for i, arg := range trigger.Args {
			args[i] = quote(arg)
		}
		fmt.Fprintf(&output, "CREATE TRIGGER %s AFTER INSERT OR UPDATE OR DELETE ON %s FOR EACH ROW EXECUTE FUNCTION %s(%s);\n", trigger.Name, trigger.Table, trigger.Function, strings.Join(args, ", "))
	}

	return output.Bytes()
}

func quote(value string) string {
	return "'" + strings.ReplaceAll(value, "'", "''") + "'"
}

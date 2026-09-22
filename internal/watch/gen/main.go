//go:build ignore

package main

import (
	"bytes"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"

	"weave/internal/watch"
)

func main() {
	if len(os.Args) != 2 {
		fmt.Fprintln(os.Stderr, "usage: go run ./internal/watch/gen <migration-name>")
		os.Exit(2)
	}

	name := os.Args[1]
	if filepath.Base(name) != name || !strings.HasSuffix(name, ".sql") {
		fmt.Fprintln(os.Stderr, "migration name must be a .sql filename")
		os.Exit(2)
	}

	path := filepath.Join("db", "migrations", name)
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
}

func render() []byte {
	var output bytes.Buffer
	for _, resource := range watch.Resources() {
		fmt.Fprintf(&output, "DROP TRIGGER IF EXISTS watch_notify ON %s;\n", resource.Table)
		fmt.Fprintf(&output, "CREATE TRIGGER watch_notify AFTER INSERT OR UPDATE OR DELETE ON %s FOR EACH ROW EXECUTE FUNCTION watch_notify(%s);\n\n", resource.Table, quote(resource.Key))

		for _, projection := range resource.Projects {
			name := "watch_project_" + resource.Table
			fmt.Fprintf(&output, "DROP TRIGGER IF EXISTS %s ON %s;\n", name, projection.Table)
			if projection.Column != "" {
				fmt.Fprintf(&output, "CREATE TRIGGER %s AFTER INSERT OR UPDATE OR DELETE ON %s FOR EACH ROW EXECUTE FUNCTION watch_project(%s, %s);\n\n", name, projection.Table, quote(resource.Table), quote(projection.Column))
				continue
			}

			fmt.Fprintf(&output, "CREATE TRIGGER %s AFTER INSERT OR UPDATE OR DELETE ON %s FOR EACH ROW EXECUTE FUNCTION watch_project_lookup(%s, %s, %s, %s, %s);\n\n", name, projection.Table, quote(resource.Table), quote(resource.Key), quote(projection.ParentMatch), quote(projection.Match), quote(projection.ParentMustBeNull))
		}
	}

	output.WriteString("DROP FUNCTION IF EXISTS attach_watch_trigger(text, text);\n")
	output.WriteString("DROP FUNCTION IF EXISTS attach_projection_trigger(text, text, text);\n")
	output.WriteString("DROP FUNCTION IF EXISTS attach_projection_lookup_trigger(text, text, text, text, text, text);\n")
	output.WriteString("DROP FUNCTION IF EXISTS watch_trigger_is_current(text, text, text, text[]);\n")

	return output.Bytes()
}

func quote(value string) string {
	return "'" + strings.ReplaceAll(value, "'", "''") + "'"
}

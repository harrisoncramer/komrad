package migrator

import (
	"cmp"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"io/fs"
	"maps"
	"regexp"
	"slices"
	"strconv"
	"strings"

	"komrad/db"
	"komrad/db/isolation"
	"komrad/lib/xslice"

	"github.com/jackc/pgx/v5"
)

// TriggerFunctionsUnit is the trigger unit holding the functions every other unit's triggers run.
const TriggerFunctionsUnit = "_functions"

// versionLength is how much of the fingerprint is kept.
const versionLength = 12

var triggerFileName = regexp.MustCompile(`^(.+)\.v(\d+)\.sql$`)

// sources is where a pass reads its migrations and trigger units from.
type sources struct {
	migrations fs.FS
	triggers   fs.FS
	isolate    func(context.Context, pgx.Tx) error
}

// unit is one trigger file, the unit it replaces and the version it carries.
type unit struct {
	name    string
	version int
	file    string
}

// ParseTriggerFileName splits a trigger file's name into its unit and version.
func ParseTriggerFileName(name string) (string, int, error) {
	match := triggerFileName.FindStringSubmatch(name)
	if match == nil {
		return "", 0, fmt.Errorf("trigger file %s is not named unit.vN.sql", name)
	}

	version, err := strconv.Atoi(match[2])
	if err != nil || version < 1 {
		return "", 0, fmt.Errorf("trigger file %s has no version above zero", name)
	}

	return match[1], version, nil
}

// SchemaVersion fingerprints the embedded migrations and triggers, and changes whenever one is added or edited.
func SchemaVersion() (string, error) {
	src, err := openSources()
	if err != nil {
		return "", err
	}

	return schemaVersion(src)
}

// openSources opens the embedded migrations and trigger units, each rooted at the directory
// holding them, so a version is the bare file name.
func openSources() (*sources, error) {
	migrations, err := fs.Sub(db.Migrations, "migrations")
	if err != nil {
		return nil, fmt.Errorf("failed to open the embedded migrations: %w", err)
	}

	triggers, err := fs.Sub(db.Triggers, "triggers")
	if err != nil {
		return nil, fmt.Errorf("failed to open the embedded triggers: %w", err)
	}

	return &sources{
		migrations: migrations,
		triggers:   triggers,
		isolate:    isolation.Apply,
	}, nil
}

func (s *sources) list() (*contents, error) {
	migrations, err := listMigrationVersions(s.migrations)
	if err != nil {
		return nil, err
	}

	units, err := listUnits(s.triggers)
	if err != nil {
		return nil, err
	}

	return &contents{migrations: migrations, units: units}, nil
}

// schemaVersion hashes each migration's and trigger file's name alongside its contents.
func schemaVersion(src *sources) (string, error) {
	all, err := src.list()
	if err != nil {
		return "", err
	}

	var content []byte
	add := func(files fs.FS, name, label string) error {
		sqlText, err := fs.ReadFile(files, name)
		if err != nil {
			return fmt.Errorf("failed to read %q: %w", name, err)
		}

		content = append(content, label...)
		content = append(content, 0)
		content = append(content, sqlText...)
		content = append(content, 0)

		return nil
	}

	for _, version := range all.migrations {
		if err := add(src.migrations, version, version); err != nil {
			return "", err
		}
	}
	for _, u := range all.units {
		if err := add(src.triggers, u.file, "triggers/"+u.file); err != nil {
			return "", err
		}
	}

	sum := sha256.Sum256(content)

	return hex.EncodeToString(sum[:])[:versionLength], nil
}

// listMigrationVersions lists the embedded migration file names in the order they must run,
// which is filename order because Atlas prefixes each with a timestamp.
func listMigrationVersions(files fs.FS) ([]string, error) {
	entries, err := fs.ReadDir(files, ".")
	if err != nil {
		return nil, fmt.Errorf("failed to read migrations: %w", err)
	}

	sqlFiles := xslice.Filter(entries, func(entry fs.DirEntry) bool { return strings.HasSuffix(entry.Name(), ".sql") })
	versions := xslice.Map(sqlFiles, func(entry fs.DirEntry) string { return entry.Name() })
	slices.Sort(versions)

	return versions, nil
}

// listUnits lists the trigger files, the functions first and then each table by name.
func listUnits(files fs.FS) ([]unit, error) {
	entries, err := fs.ReadDir(files, ".")
	if err != nil {
		return nil, fmt.Errorf("failed to read triggers: %w", err)
	}

	byName := map[string]unit{}
	for _, entry := range entries {
		if !strings.HasSuffix(entry.Name(), ".sql") {
			continue
		}

		name, version, err := ParseTriggerFileName(entry.Name())
		if err != nil {
			return nil, err
		}
		if other, ok := byName[name]; ok {
			return nil, fmt.Errorf("trigger unit %s has two files, %s and %s", name, other.file, entry.Name())
		}

		byName[name] = unit{
			name:    name,
			version: version,
			file:    entry.Name(),
		}
	}

	units := slices.Collect(maps.Values(byName))
	slices.SortFunc(units, func(a, b unit) int {
		switch {
		case a.name == TriggerFunctionsUnit:
			return -1
		case b.name == TriggerFunctionsUnit:
			return 1
		}

		return cmp.Compare(a.name, b.name)
	})

	return units, nil
}

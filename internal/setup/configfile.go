package setup

import (
	"bytes"
	_ "embed"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"text/template"

	"komrad/internal/fail"
	"komrad/lib/xfile"

	"github.com/pelletier/go-toml/v2"
)

//go:embed config.toml.tmpl
var configTemplateText string

var configTemplate = template.Must(template.New("config.toml").Funcs(template.FuncMap{"quote": strconv.Quote}).Parse(configTemplateText))

// errConfigExists is what a write returns when the config file was there before setup ran.
var errConfigExists = fail.AlreadyExists("the config file already exists")

// ConfigFile is the config file setup writes from its config template, which it never changes
// once someone else has written it.
type ConfigFile struct {
	Path    string
	answers answers
	created bool
}

// answers are the settings setup fills the config template with.
type answers struct {
	DefaultHarness   string
	AutoSetup        bool
	EmbeddedPostgres bool
	OwnerDatabaseURL string
	DatabaseURL      string
}

// isWritable reports whether setup may write the file, which it may only while the file is missing or setup made it.
func (f *ConfigFile) isWritable() bool {
	if f.created {
		return true
	}

	_, err := os.Stat(f.Path)

	return errors.Is(err, fs.ErrNotExist)
}

// describeRefusal says the file is not setup's to change, and which setting to change there instead.
func (f *ConfigFile) describeRefusal(setting string) string {
	return fmt.Sprintf("%s already exists, so set %s there", shortenHome(f.Path), setting)
}

func (f *ConfigFile) write(answer func(*answers)) error {
	if !f.isWritable() {
		return fmt.Errorf("%w at %s", errConfigExists, shortenHome(f.Path))
	}

	next := f.answers
	answer(&next)

	var rendered bytes.Buffer
	if err := configTemplate.Execute(&rendered, next); err != nil {
		return fmt.Errorf("failed to fill in the config template: %w", err)
	}
	content := strings.TrimLeft(rendered.String(), "\n")

	var parsed map[string]any
	if err := toml.Unmarshal([]byte(content), &parsed); err != nil {
		return fmt.Errorf("failed to write %s, since the result would not parse: %w", f.Path, err)
	}

	if err := writeAtomically(f.Path, []byte(content)); err != nil {
		return err
	}
	f.answers = next
	f.created = true

	return nil
}

func writeAtomically(path string, content []byte) error {
	dir := filepath.Dir(path)
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return fmt.Errorf("failed to create %s: %w", dir, err)
	}

	if err := xfile.WriteAtomic(path, content, 0o600); err != nil {
		return fmt.Errorf("failed to write %s: %w", path, err)
	}

	return nil
}

package main

import (
	"bufio"
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"sort"
	"strings"
)

type module struct {
	path string
	dir  string
	rel  string
}

// describe is the plan for every module in the repository at root, measured against base under the given build tags.
func describe(root, base, tags string) (plan, error) {
	absolute, err := filepath.Abs(root)
	if err != nil {
		return plan{}, fmt.Errorf("failed to resolve %s: %w", root, err)
	}

	files, err := changedFiles(absolute, base)
	if err != nil {
		return plan{}, err
	}

	modules, err := modulesUnder(absolute)
	if err != nil {
		return plan{}, err
	}

	dirs := dirsOf(absolute, files)
	result := plan{Modules: []moduleRef{}}

	for _, mod := range modules {
		pkgs, err := listPackages(mod.dir, tags)
		if err != nil {
			return plan{}, err
		}

		untagged, err := listPackages(mod.dir, "")
		if err != nil {
			return plan{}, err
		}

		refs, dependents := touched(pkgs, mod.dir, dirs, buildable(untagged))
		result.Modules = append(result.Modules, moduleRef{
			Path:     mod.path,
			Dir:      mod.rel,
			Packages: refs,
		})
		result.UntestedDependents += dependents
	}

	return result, nil
}

// changedFiles is every path the branch changed against base, plus everything not yet added.
func changedFiles(root, base string) ([]string, error) {
	files := []string{}

	mergeBase, err := git(root, "merge-base", base, "HEAD")
	if err != nil {
		fmt.Fprintf(os.Stderr, "komradpkg: no %s to diff against, so only uncommitted files count as changed\n", base)
	} else {
		diffed, err := git(root, "diff", "--name-only", strings.TrimSpace(mergeBase))
		if err != nil {
			return nil, err
		}

		files = append(files, lines(diffed)...)
	}

	untracked, err := git(root, "ls-files", "--others", "--exclude-standard")
	if err != nil {
		return nil, err
	}

	return append(files, lines(untracked)...), nil
}

// modulesUnder is the repository's own module and every module under tools, in a stable order.
func modulesUnder(root string) ([]module, error) {
	dirs := []string{root}

	entries, err := os.ReadDir(filepath.Join(root, "tools"))
	if err != nil && !os.IsNotExist(err) {
		return nil, fmt.Errorf("failed to read the tools directory: %w", err)
	}

	for _, entry := range entries {
		if !entry.IsDir() {
			continue
		}

		dir := filepath.Join(root, "tools", entry.Name())
		if _, err := os.Stat(filepath.Join(dir, "go.mod")); err == nil {
			dirs = append(dirs, dir)
		}
	}

	sort.Strings(dirs[1:])

	modules := make([]module, 0, len(dirs))

	for _, dir := range dirs {
		path, err := modulePath(filepath.Join(dir, "go.mod"))
		if err != nil {
			return nil, err
		}

		rel, err := filepath.Rel(root, dir)
		if err != nil {
			rel = dir
		}

		modules = append(modules, module{
			path: path,
			dir:  dir,
			rel:  rel,
		})
	}

	return modules, nil
}

// modulePath is the path a go.mod declares.
func modulePath(path string) (string, error) {
	file, err := os.Open(path)
	if err != nil {
		return "", fmt.Errorf("failed to read %s: %w", path, err)
	}

	defer file.Close()

	scanner := bufio.NewScanner(file)
	for scanner.Scan() {
		if declared, ok := strings.CutPrefix(strings.TrimSpace(scanner.Text()), "module "); ok {
			return strings.TrimSpace(declared), nil
		}
	}

	return "", fmt.Errorf("%s declares no module path", path)
}

// listPackages is what go list says about every package in the module rooted at dir.
func listPackages(dir, tags string) ([]listed, error) {
	command := exec.Command("go", "list", "-e", "-tags", tags, "-json=ImportPath,Dir,Deps,TestImports,XTestImports,GoFiles,TestGoFiles,XTestGoFiles", "./...") //nolint:noctx // the caller is a one-shot command with no deadline of its own
	command.Dir = dir
	command.Stderr = os.Stderr

	out, err := command.Output()
	if err != nil {
		return nil, fmt.Errorf("failed to list the packages in %s: %w", dir, err)
	}

	pkgs := []listed{}
	decoder := json.NewDecoder(bytes.NewReader(out))

	for {
		var pkg listed

		if err := decoder.Decode(&pkg); err != nil {
			if errors.Is(err, io.EOF) {
				break
			}

			return nil, fmt.Errorf("failed to read what go list said about %s: %w", dir, err)
		}

		pkgs = append(pkgs, pkg)
	}

	return pkgs, nil
}

func git(root string, args ...string) (string, error) {
	command := exec.Command("git", args...) //nolint:noctx // the caller is a one-shot command with no deadline of its own
	command.Dir = root

	out, err := command.Output()
	if err != nil {
		return "", fmt.Errorf("failed to run git %s: %w", strings.Join(args, " "), err)
	}

	return string(out), nil
}

func lines(text string) []string {
	out := []string{}

	for _, line := range strings.Split(text, "\n") {
		if trimmed := strings.TrimSpace(line); trimmed != "" {
			out = append(out, trimmed)
		}
	}

	return out
}

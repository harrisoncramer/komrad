package main

import (
	"encoding/json"
	"fmt"
	"io"
	"path/filepath"
	"slices"
	"sort"
)

type listed struct {
	ImportPath   string
	Dir          string
	Deps         []string
	TestImports  []string
	XTestImports []string
}

type pkgRef struct {
	ImportPath string `json:"importPath"`
	Dir        string `json:"dir"`
}

type moduleRef struct {
	Path     string   `json:"path"`
	Dir      string   `json:"dir"`
	Packages []pkgRef `json:"packages"`
}

type plan struct {
	Modules            []moduleRef `json:"modules"`
	UntestedDependents int         `json:"untestedDependents"`
}

// touched is the packages of one module holding a changed file, and the count of packages that import them without being changed themselves.
func touched(pkgs []listed, moduleDir string, changedDirs map[string]bool) ([]pkgRef, int) {
	refs := []pkgRef{}
	changed := map[string]bool{}

	for _, pkg := range pkgs {
		if pkg.Dir == "" || !changedDirs[pkg.Dir] {
			continue
		}

		changed[pkg.ImportPath] = true

		rel, err := filepath.Rel(moduleDir, pkg.Dir)
		if err != nil {
			rel = pkg.Dir
		}

		refs = append(refs, pkgRef{ImportPath: pkg.ImportPath, Dir: rel})
	}

	sort.Slice(refs, func(i, j int) bool { return refs[i].ImportPath < refs[j].ImportPath })

	dependents := 0

	for _, pkg := range pkgs {
		if changed[pkg.ImportPath] {
			continue
		}

		for _, dep := range slices.Concat(pkg.Deps, pkg.TestImports, pkg.XTestImports) {
			if changed[dep] {
				dependents++

				break
			}
		}
	}

	return refs, dependents
}

// dirsOf is the set of directories holding a changed Go file, as absolute paths under root.
func dirsOf(root string, files []string) map[string]bool {
	dirs := map[string]bool{}

	for _, file := range files {
		if filepath.Ext(file) != ".go" {
			continue
		}

		dirs[filepath.Join(root, filepath.Dir(file))] = true
	}

	return dirs
}

func writePlan(out io.Writer, value plan) error {
	encoder := json.NewEncoder(out)
	encoder.SetIndent("", "  ")

	if err := encoder.Encode(value); err != nil {
		return fmt.Errorf("failed to write the plan: %w", err)
	}

	return nil
}

package main

import (
	"cmp"
	"encoding/json"
	"fmt"
	"io"
	"path/filepath"
	"slices"
)

type listed struct {
	ImportPath   string   `json:"ImportPath"`
	Dir          string   `json:"Dir"`
	Deps         []string `json:"Deps"`
	TestImports  []string `json:"TestImports"`
	XTestImports []string `json:"XTestImports"`
	GoFiles      []string `json:"GoFiles"`
	TestGoFiles  []string `json:"TestGoFiles"`
	XTestGoFiles []string `json:"XTestGoFiles"`
}

type pkgRef struct {
	ImportPath string `json:"importPath"`
	Dir        string `json:"dir"`
	Testable   bool   `json:"testable"`
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
func touched(pkgs []listed, moduleDir string, changedDirs, testable map[string]bool) ([]pkgRef, int) {
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

		refs = append(refs, pkgRef{
			ImportPath: pkg.ImportPath,
			Dir:        rel,
			Testable:   testable[pkg.ImportPath],
		})
	}

	slices.SortFunc(refs, func(a, b pkgRef) int { return cmp.Compare(a.ImportPath, b.ImportPath) })

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

// buildable is the packages holding at least one Go file the current build tags let in.
func buildable(pkgs []listed) map[string]bool {
	set := map[string]bool{}

	for _, pkg := range pkgs {
		if len(pkg.GoFiles)+len(pkg.TestGoFiles)+len(pkg.XTestGoFiles) > 0 {
			set[pkg.ImportPath] = true
		}
	}

	return set
}

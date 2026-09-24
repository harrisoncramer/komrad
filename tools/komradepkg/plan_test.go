package main

import (
	"reflect"
	"testing"
)

var fixture = []listed{
	{
		ImportPath: "komrade/a",
		Dir:        "/repo/a",
		GoFiles:    []string{"a.go"},
	},
	{
		ImportPath: "komrade/b",
		Dir:        "/repo/b",
		Deps:       []string{"komrade/a"},
		GoFiles:    []string{"b.go"},
	},
	{
		ImportPath:  "komrade/c",
		Dir:         "/repo/c",
		TestImports: []string{"komrade/a"},
		GoFiles:     []string{"c.go"},
	},
	{
		ImportPath:   "komrade/d",
		Dir:          "/repo/d",
		XTestImports: []string{"komrade/a"},
		GoFiles:      []string{"d.go"},
	},
	{
		ImportPath: "komrade/e",
		Dir:        "/repo/e",
		GoFiles:    []string{"e.go"},
	},
	{
		ImportPath:  "komrade/f",
		Dir:         "/repo/f",
		TestImports: []string{"komrade/a"},
	},
}

// TestTouched covers which packages a change reaches, through imports and through test imports alike.
func TestTouched(t *testing.T) {
	tests := []struct {
		name           string
		changed        []string
		wantRefs       []pkgRef
		wantDependents int
	}{
		{
			name:     "nothing changed",
			changed:  nil,
			wantRefs: []pkgRef{},
		},
		{
			name:    "a package everything reaches",
			changed: []string{"/repo/a"},
			wantRefs: []pkgRef{
				{
					ImportPath: "komrade/a",
					Dir:        "a",
					Testable:   true,
				},
			},
			wantDependents: 4,
		},
		{
			name:    "an importer that changed is not its own dependent",
			changed: []string{"/repo/a", "/repo/b", "/repo/c", "/repo/d", "/repo/f"},
			wantRefs: []pkgRef{
				{
					ImportPath: "komrade/a",
					Dir:        "a",
					Testable:   true,
				},
				{
					ImportPath: "komrade/b",
					Dir:        "b",
					Testable:   true,
				},
				{
					ImportPath: "komrade/c",
					Dir:        "c",
					Testable:   true,
				},
				{
					ImportPath: "komrade/d",
					Dir:        "d",
					Testable:   true,
				},
				{
					ImportPath: "komrade/f",
					Dir:        "f",
					Testable:   false,
				},
			},
			wantDependents: 0,
		},
		{
			name:    "a package that only builds under a tag is listed but not testable",
			changed: []string{"/repo/f"},
			wantRefs: []pkgRef{
				{
					ImportPath: "komrade/f",
					Dir:        "f",
					Testable:   false,
				},
			},
			wantDependents: 0,
		},
		{
			name:    "a package nothing imports",
			changed: []string{"/repo/e"},
			wantRefs: []pkgRef{
				{
					ImportPath: "komrade/e",
					Dir:        "e",
					Testable:   true,
				},
			},
			wantDependents: 0,
		},
		{
			name:     "a directory holding no package",
			changed:  []string{"/repo/docs"},
			wantRefs: []pkgRef{},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			dirs := map[string]bool{}
			for _, dir := range tt.changed {
				dirs[dir] = true
			}

			refs, dependents := touched(fixture, "/repo", dirs, buildable(fixture))

			if !reflect.DeepEqual(refs, tt.wantRefs) {
				t.Errorf("packages: got %v, want %v", refs, tt.wantRefs)
			}

			if dependents != tt.wantDependents {
				t.Errorf("dependents: got %d, want %d", dependents, tt.wantDependents)
			}
		})
	}
}

// TestBuildable covers which packages hold a file the current build tags let in.
func TestBuildable(t *testing.T) {
	tests := []struct {
		name string
		pkg  listed
		want bool
	}{
		{
			name: "a package with source files",
			pkg: listed{
				ImportPath: "komrade/a",
				GoFiles:    []string{"a.go"},
			},
			want: true,
		},
		{
			name: "a package with only external test files",
			pkg: listed{
				ImportPath:   "komrade/b",
				XTestGoFiles: []string{"b_test.go"},
			},
			want: true,
		},
		{
			name: "a package whose every file is behind a tag",
			pkg:  listed{ImportPath: "komrade/c"},
			want: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := buildable([]listed{tt.pkg})[tt.pkg.ImportPath]; got != tt.want {
				t.Errorf("got %v, want %v", got, tt.want)
			}
		})
	}
}

// TestDirsOf covers which directories a list of changed files points at.
func TestDirsOf(t *testing.T) {
	tests := []struct {
		name  string
		files []string
		want  map[string]bool
	}{
		{
			name:  "only Go files count",
			files: []string{"internal/ui/view.go", "docs/remote.md", "mise/tasks/check/quick"},
			want:  map[string]bool{"/repo/internal/ui": true},
		},
		{
			name:  "a file at the root is the root package",
			files: []string{"main.go"},
			want:  map[string]bool{"/repo": true},
		},
		{
			name:  "two files in one directory are one directory",
			files: []string{"internal/ui/view.go", "internal/ui/update.go"},
			want:  map[string]bool{"/repo/internal/ui": true},
		},
		{
			name:  "nothing changed",
			files: nil,
			want:  map[string]bool{},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := dirsOf("/repo", tt.files)

			if !reflect.DeepEqual(got, tt.want) {
				t.Errorf("got %v, want %v", got, tt.want)
			}
		})
	}
}

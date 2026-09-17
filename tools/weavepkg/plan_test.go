package main

import (
	"reflect"
	"testing"
)

// TestTouched covers which packages a change reaches, through imports and through test imports alike.
func TestTouched(t *testing.T) {
	pkgs := []listed{
		{ImportPath: "weave/a", Dir: "/repo/a"},
		{
			ImportPath: "weave/b",
			Dir:        "/repo/b",
			Deps:       []string{"weave/a"},
		},
		{
			ImportPath:  "weave/c",
			Dir:         "/repo/c",
			TestImports: []string{"weave/a"},
		},
		{
			ImportPath:   "weave/d",
			Dir:          "/repo/d",
			XTestImports: []string{"weave/a"},
		},
		{ImportPath: "weave/e", Dir: "/repo/e"},
	}

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
			name:           "a package everything reaches",
			changed:        []string{"/repo/a"},
			wantRefs:       []pkgRef{{ImportPath: "weave/a", Dir: "a"}},
			wantDependents: 3,
		},
		{
			name:           "an importer that changed is not its own dependent",
			changed:        []string{"/repo/a", "/repo/b", "/repo/c", "/repo/d"},
			wantRefs:       []pkgRef{{ImportPath: "weave/a", Dir: "a"}, {ImportPath: "weave/b", Dir: "b"}, {ImportPath: "weave/c", Dir: "c"}, {ImportPath: "weave/d", Dir: "d"}},
			wantDependents: 0,
		},
		{
			name:           "a package nothing imports",
			changed:        []string{"/repo/e"},
			wantRefs:       []pkgRef{{ImportPath: "weave/e", Dir: "e"}},
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

			refs, dependents := touched(pkgs, "/repo", dirs)

			if !reflect.DeepEqual(refs, tt.wantRefs) {
				t.Errorf("packages: got %v, want %v", refs, tt.wantRefs)
			}

			if dependents != tt.wantDependents {
				t.Errorf("dependents: got %d, want %d", dependents, tt.wantDependents)
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

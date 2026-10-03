package main

import (
	"bytes"
	"cmp"
	"fmt"
	"go/ast"
	"os"
	"slices"
	"strings"
	"unicode"
)

type edit struct {
	offset  int
	oldName string
	newName string
}

// rename rewrites every use of each candidate, and the doc comment that opens with its name.
func rename(loaded *program, found []*candidate) error {
	edits := map[string][]edit{}
	byDefinition := map[position]*candidate{}
	for _, c := range found {
		byDefinition[c.defined] = c
		for use := range c.uses {
			edits[use.file] = append(edits[use.file], edit{offset: use.offset, oldName: c.obj.Name(), newName: c.newName})
		}
	}

	for _, doc := range docEdits(loaded, byDefinition) {
		edits[doc.file] = append(edits[doc.file], doc.edit)
	}

	for file, fileEdits := range edits {
		if err := applyEdits(file, fileEdits); err != nil {
			return err
		}
	}

	return nil
}

type docEdit struct {
	file string
	edit edit
}

func docEdits(loaded *program, byDefinition map[position]*candidate) []docEdit {
	var edits []docEdit
	seen := map[position]bool{}
	add := func(doc *ast.CommentGroup, ident *ast.Ident) {
		if doc == nil || len(doc.List) == 0 {
			return
		}

		c, ok := byDefinition[loaded.position(ident.Pos())]
		if !ok || !opensWith(doc.List[0].Text, c.obj.Name()) {
			return
		}

		at := loaded.position(doc.List[0].Pos())
		at.offset += len("// ")
		if seen[at] {
			return
		}

		seen[at] = true
		edits = append(edits, docEdit{file: at.file, edit: edit{offset: at.offset, oldName: c.obj.Name(), newName: c.newName}})
	}

	for _, pkg := range loaded.packages {
		if isExternalTest(pkg) {
			continue
		}

		for _, file := range pkg.Syntax {
			for _, decl := range file.Decls {
				addDeclDocs(decl, add)
			}
		}
	}

	return edits
}

func addDeclDocs(decl ast.Decl, add func(*ast.CommentGroup, *ast.Ident)) {
	switch typed := decl.(type) {
	case *ast.FuncDecl:
		add(typed.Doc, typed.Name)
	case *ast.GenDecl:
		for _, spec := range typed.Specs {
			ident, doc := specName(spec)
			if ident == nil {
				continue
			}

			add(doc, ident)
			if len(typed.Specs) == 1 {
				add(typed.Doc, ident)
			}
		}
	}
}

func specName(spec ast.Spec) (*ast.Ident, *ast.CommentGroup) {
	switch typed := spec.(type) {
	case *ast.TypeSpec:
		return typed.Name, typed.Doc
	case *ast.ValueSpec:
		if len(typed.Names) != 1 {
			return nil, nil
		}

		return typed.Names[0], typed.Doc
	default:
		return nil, nil
	}
}

// opensWith says whether a line comment's first word is exactly that name.
func opensWith(comment, name string) bool {
	rest, ok := strings.CutPrefix(comment, "// "+name)
	if !ok {
		return false
	}

	if rest == "" {
		return true
	}

	next := rune(rest[0])

	return !unicode.IsLetter(next) && !unicode.IsDigit(next) && next != '_'
}

func applyEdits(file string, edits []edit) error {
	source, err := os.ReadFile(file)
	if err != nil {
		return fmt.Errorf("failed to read %s: %w", file, err)
	}

	slices.SortFunc(edits, func(a, b edit) int { return cmp.Compare(b.offset, a.offset) })
	edits = slices.CompactFunc(edits, func(a, b edit) bool { return a.offset == b.offset })
	for _, e := range edits {
		if !bytes.HasPrefix(source[e.offset:], []byte(e.oldName)) {
			return fmt.Errorf("%s at offset %d does not hold %s", file, e.offset, e.oldName)
		}

		source = slices.Concat(source[:e.offset], []byte(e.newName), source[e.offset+len(e.oldName):])
	}

	info, err := os.Stat(file)
	if err != nil {
		return fmt.Errorf("failed to stat %s: %w", file, err)
	}

	if err := os.WriteFile(file, source, info.Mode().Perm()); err != nil {
		return fmt.Errorf("failed to write %s: %w", file, err)
	}

	return nil
}

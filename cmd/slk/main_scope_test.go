package main

import (
	"go/ast"
	"go/parser"
	"go/token"
	"sort"
	"strings"
	"testing"
)

// main.go is the entrypoint, not a junk drawer. Every other declaration in
// package main belongs in a file named for its topic — see the files this
// package already has: markread.go, presence.go, sections.go and the rest.
//
// Adding an entry here is a design decision, not a formality. Say why the
// declaration cannot live in a topical file. "It was convenient" is not a
// reason; moving a declaration between files in one package is a one-line
// change that the compiler cannot get wrong.
//
// Phase 1 of the architecture refactor emptied this file down to the seven
// names below. It had grown from 4,842 lines to 5,421 in the four months
// before that, which is why the rule is enforced rather than documented.
// See docs/superpowers/specs/2026-09-22-phase1-main-go-splits-design.md.
var mainGoAllowed = map[string]string{
	"version":            "build stamp, set via -ldflags",
	"commit":             "build stamp, set via -ldflags",
	"date":               "build stamp, set via -ldflags",
	"main":               "process entrypoint",
	"printHelp":          "usage text for main",
	"newImageHTTPClient": "constructed by run; single caller",
	"run":                "composition root — Phase 2 decomposes this",
}

func TestMainGoHoldsOnlyTheEntrypoint(t *testing.T) {
	fset := token.NewFileSet()
	f, err := parser.ParseFile(fset, "main.go", nil, parser.SkipObjectResolution)
	if err != nil {
		t.Fatalf("parsing main.go: %v", err)
	}

	seen := map[string]bool{}
	var offenders []string

	for _, d := range f.Decls {
		for _, name := range declaredNames(d) {
			seen[name] = true
			if _, ok := mainGoAllowed[name]; !ok {
				offenders = append(offenders, name+"  ("+fset.Position(d.Pos()).String()+")")
			}
		}
	}

	if len(offenders) > 0 {
		sort.Strings(offenders)
		t.Errorf("main.go declares %d symbol(s) that belong in a topical file:\n  %s\n\n"+
			"Move each into a file in cmd/slk named for its topic — create one if no\n"+
			"existing file fits. If a symbol genuinely cannot live anywhere else, add it\n"+
			"to mainGoAllowed in this file with a reason.",
			len(offenders), strings.Join(offenders, "\n  "))
	}

	// A stale allow-list is as misleading as a missing one. When Phase 2
	// extracts run(), its entry must be deleted rather than left behind.
	var stale []string
	for name := range mainGoAllowed {
		if !seen[name] {
			stale = append(stale, name)
		}
	}
	if len(stale) > 0 {
		sort.Strings(stale)
		t.Errorf("mainGoAllowed lists %d symbol(s) no longer declared in main.go: %s\n\n"+
			"Delete the entries — they no longer describe anything.",
			len(stale), strings.Join(stale, ", "))
	}
}

// declaredNames returns every package-scope name a declaration introduces.
// A method is reported as "(*T).Method" so it cannot be confused with a
// plain function of the same name.
func declaredNames(d ast.Decl) []string {
	switch v := d.(type) {
	case *ast.FuncDecl:
		if v.Recv != nil && len(v.Recv.List) > 0 {
			var recv string
			switch t := v.Recv.List[0].Type.(type) {
			case *ast.StarExpr:
				if id, ok := t.X.(*ast.Ident); ok {
					recv = "*" + id.Name
				}
			case *ast.Ident:
				recv = t.Name
			}
			return []string{"(" + recv + ")." + v.Name.Name}
		}
		return []string{v.Name.Name}
	case *ast.GenDecl:
		if v.Tok == token.IMPORT {
			return nil
		}
		var names []string
		for _, s := range v.Specs {
			switch sp := s.(type) {
			case *ast.TypeSpec:
				names = append(names, sp.Name.Name)
			case *ast.ValueSpec:
				for _, n := range sp.Names {
					if n.Name != "_" {
						names = append(names, n.Name)
					}
				}
			}
		}
		return names
	}
	return nil
}

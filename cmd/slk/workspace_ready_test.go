package main

import (
	"go/ast"
	"go/parser"
	"go/token"
	"testing"
)

// main cannot be invoked without starting the TUI. Pin the startup ownership
// boundary structurally: detach the UI's slices before launching the event
// owner, so neither UI mutations nor the clone itself race workspace repairs.
func TestWorkspaceReadySnapshotsPrecedeEventOwner(t *testing.T) {
	file, err := parser.ParseFile(token.NewFileSet(), "main.go", nil, parser.SkipObjectResolution)
	if err != nil {
		t.Fatal(err)
	}
	var ready *ast.CompositeLit
	var start token.Pos
	ast.Inspect(file, func(node ast.Node) bool {
		if literal, ok := node.(*ast.CompositeLit); ok {
			if typ, ok := literal.Type.(*ast.SelectorExpr); ok && typ.Sel.Name == "WorkspaceReadyMsg" {
				if ready != nil {
					t.Fatal("multiple WorkspaceReadyMsg literals: check each snapshot boundary")
				}
				ready = literal
			}
		}
		if launch, ok := node.(*ast.GoStmt); ok {
			if call, ok := launch.Call.Fun.(*ast.SelectorExpr); ok && call.Sel.Name == "Run" {
				if receiver, ok := call.X.(*ast.SelectorExpr); ok && receiver.Sel.Name == "ConnMgr" {
					if start.IsValid() {
						t.Fatal("multiple ConnMgr.Run launches: check each snapshot boundary")
					}
					start = launch.Pos()
				}
			}
		}
		return true
	})
	if ready == nil || !start.IsValid() || ready.End() >= start {
		t.Fatal("startup snapshots must be built before starting the event owner")
	}
	cloned := make(map[string]bool)
	for _, elt := range ready.Elts {
		field, ok := elt.(*ast.KeyValueExpr)
		if !ok {
			continue
		}
		key, ok := field.Key.(*ast.Ident)
		if !ok || (key.Name != "Channels" && key.Name != "FinderItems") {
			continue
		}
		call, ok := field.Value.(*ast.CallExpr)
		if !ok || len(call.Args) != 1 {
			continue
		}
		fn, ok := call.Fun.(*ast.SelectorExpr)
		if !ok || fn.Sel.Name != "Clone" {
			continue
		}
		pkg, ok := fn.X.(*ast.Ident)
		if !ok || pkg.Name != "slices" {
			continue
		}
		source, ok := call.Args[0].(*ast.SelectorExpr)
		if ok && source.Sel.Name == key.Name {
			cloned[key.Name] = true
		}
	}
	for _, field := range []string{"Channels", "FinderItems"} {
		if !cloned[field] {
			t.Errorf("WorkspaceReadyMsg.%s must clone the workspace slice", field)
		}
	}
}

package ui

import (
	"go/ast"
	"go/parser"
	"go/token"
	"path/filepath"
	"strings"
	"testing"
)

// toastSetters are the only functions that may put text in the status
// bar's toast slot: toastWithClear, which schedules the clear for that
// toast, and toastUntilReplaced, for a toast a later one replaces on
// purpose.
var toastSetters = map[string]bool{"toastWithClear": true, "toastUntilReplaced": true}

// TestToastsGoThroughToastSetters enforces the toast rule in AGENTS.md.
// A clear tick clears only the toast it was scheduled for (statusbar
// ToastSeq), so a toast set directly with statusbar.SetToast and no
// clear of its own stays until another toast replaces it. Clearing the
// slot, SetToast(""), is allowed anywhere.
func TestToastsGoThroughToastSetters(t *testing.T) {
	paths, err := filepath.Glob("*.go")
	if err != nil {
		t.Fatal(err)
	}
	fset := token.NewFileSet()
	for _, path := range paths {
		if strings.HasSuffix(path, "_test.go") {
			continue
		}
		f, err := parser.ParseFile(fset, path, nil, 0)
		if err != nil {
			t.Fatal(err)
		}
		for _, decl := range f.Decls {
			fn, isFunc := decl.(*ast.FuncDecl)
			if isFunc && fn.Recv == nil && toastSetters[fn.Name.Name] {
				continue
			}
			ast.Inspect(decl, func(n ast.Node) bool {
				call, ok := n.(*ast.CallExpr)
				if !ok || !isStatusbarSetToast(call) {
					return true
				}
				if lit, ok := call.Args[0].(*ast.BasicLit); ok && lit.Value == `""` {
					return true
				}
				t.Errorf("%s: statusbar.SetToast with text; use toastWithClear, or toastUntilReplaced for a toast a later one replaces",
					fset.Position(call.Pos()))
				return true
			})
		}
	}
}

// isStatusbarSetToast reports whether call is <x>.statusbar.SetToast(arg).
func isStatusbarSetToast(call *ast.CallExpr) bool {
	sel, ok := call.Fun.(*ast.SelectorExpr)
	if !ok || sel.Sel.Name != "SetToast" || len(call.Args) != 1 {
		return false
	}
	owner, ok := sel.X.(*ast.SelectorExpr)
	return ok && owner.Sel.Name == "statusbar"
}

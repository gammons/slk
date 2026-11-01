package slackclient

import (
	"go/ast"
	"go/parser"
	gotoken "go/token"
	"path/filepath"
	"strings"
	"testing"
)

// TestClient_ReadsTheLoginOnlyThroughItsAccessors enforces loginMu's
// rule. SetLogin replaces api, token and cookie while requests run, so a
// Client method that reads one of them directly races with it, and only
// under a mid-session login refresh, where no ordinary test looks. The
// accessors (login, webAPI) and the two writers (SetLogin, Connect) are
// the only methods that touch the fields.
func TestClient_ReadsTheLoginOnlyThroughItsAccessors(t *testing.T) {
	allowed := map[string]bool{"login": true, "webAPI": true, "SetLogin": true, "Connect": true}
	guarded := map[string]bool{"api": true, "token": true, "cookie": true}

	names, err := filepath.Glob("*.go")
	if err != nil {
		t.Fatal(err)
	}
	fset := gotoken.NewFileSet()
	checked := 0
	for _, name := range names {
		if strings.HasSuffix(name, "_test.go") {
			continue
		}
		f, err := parser.ParseFile(fset, name, nil, 0)
		if err != nil {
			t.Fatalf("parsing %s: %v", name, err)
		}
		for _, decl := range f.Decls {
			fn, ok := decl.(*ast.FuncDecl)
			if !ok || fn.Body == nil || allowed[fn.Name.Name] {
				continue
			}
			recv := clientReceiver(fn)
			if recv == "" {
				continue
			}
			checked++
			ast.Inspect(fn.Body, func(n ast.Node) bool {
				sel, ok := n.(*ast.SelectorExpr)
				if !ok || !guarded[sel.Sel.Name] {
					return true
				}
				if id, ok := sel.X.(*ast.Ident); ok && id.Name == recv {
					t.Errorf("%s: (*Client).%s reads %s.%s directly; use login() or webAPI(), which hold loginMu",
						fset.Position(sel.Pos()), fn.Name.Name, recv, sel.Sel.Name)
				}
				return true
			})
		}
	}
	if checked == 0 {
		t.Fatal("found no *Client methods; this test is reading the wrong files")
	}
}

// clientReceiver returns the receiver name of a method on *Client, or "".
func clientReceiver(fn *ast.FuncDecl) string {
	if fn.Recv == nil || len(fn.Recv.List) != 1 || len(fn.Recv.List[0].Names) != 1 {
		return ""
	}
	star, ok := fn.Recv.List[0].Type.(*ast.StarExpr)
	if !ok {
		return ""
	}
	if id, ok := star.X.(*ast.Ident); !ok || id.Name != "Client" {
		return ""
	}
	return fn.Recv.List[0].Names[0].Name
}

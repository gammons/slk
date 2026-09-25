package ui

import (
	"go/ast"
	"go/parser"
	"go/token"
	"io/fs"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
)

// The TUI reaches Slack, SQLite, the network, the filesystem and the OS
// only through the ports in internal/core. These tests fail when a
// non-test file under internal/ui (or core itself) reaches past them.

// tuiBannedImports are packages whose presence in the TUI would mean
// it's doing the application's I/O itself.
var tuiBannedImports = []string{
	"net",
	"net/http",
	"os/exec",
	"database/sql",
	"golang.design/x/clipboard",
	"github.com/gorilla/websocket",
	"modernc.org/sqlite",
	"github.com/gammons/slk/internal/slack",
	"github.com/gammons/slk/internal/slack/boot",
	"github.com/gammons/slk/internal/slack/edge",
	"github.com/gammons/slk/internal/slack/membership",
	"github.com/gammons/slk/internal/slackhttp",
	"github.com/gammons/slk/internal/slackdesktop",
	"github.com/gammons/slk/internal/bootstrap",
	"github.com/gammons/slk/internal/cache",
	"github.com/gammons/slk/internal/config",
	"github.com/gammons/slk/internal/editor",
	"github.com/gammons/slk/internal/export",
	"github.com/gammons/slk/internal/filedl",
	"github.com/gammons/slk/internal/notify",
	"github.com/gammons/slk/internal/service",
	"github.com/gammons/slk/internal/wake",
	"github.com/gammons/slk/internal/avatar",
}

// slack-go is allowed only where its Block Kit types are the input
// being parsed and rendered.
const slackGo = "github.com/slack-go/slack"

var slackGoAllowed = map[string]bool{
	"messages/blockkit": true,
}

// tuiBannedCalls are pkg.Func selectors the TUI must not use. Environment
// lookups (os.Getenv, os.UserHomeDir) are the TUI reading its own
// terminal and input, and stay allowed.
var tuiBannedCalls = map[string][]string{
	"os": {"Open", "OpenFile", "Create", "ReadFile", "WriteFile", "ReadDir",
		"Stat", "Lstat", "Mkdir", "MkdirAll", "MkdirTemp", "CreateTemp",
		"Remove", "RemoveAll", "Rename", "Chmod", "Chdir", "StartProcess", "DirFS"},
	"github.com/gammons/slk/internal/image": {"NewFetcher", "Fetcher", "NewCache", "Cache", "MigrateAvatars"},
}

func TestTUIReachesTheAppOnlyThroughCore(t *testing.T) {
	walkGoFiles(t, ".", func(rel string, f *ast.File, fset *token.FileSet) {
		dir := filepath.ToSlash(filepath.Dir(rel))
		names := map[string]string{} // local name -> import path
		for _, imp := range f.Imports {
			path, _ := strconv.Unquote(imp.Path.Value)
			for _, banned := range tuiBannedImports {
				if path == banned {
					t.Errorf("%s imports %s; go through an internal/core port", rel, path)
				}
			}
			if path == slackGo && !slackGoAllowed[dir] {
				t.Errorf("%s imports %s outside the Block Kit renderer", rel, path)
			}
			name := path[strings.LastIndex(path, "/")+1:]
			if imp.Name != nil {
				name = imp.Name.Name
			}
			names[name] = path
		}
		ast.Inspect(f, func(n ast.Node) bool {
			sel, ok := n.(*ast.SelectorExpr)
			if !ok {
				return true
			}
			pkg, ok := sel.X.(*ast.Ident)
			if !ok {
				return true
			}
			for _, fn := range tuiBannedCalls[names[pkg.Name]] {
				if sel.Sel.Name == fn {
					t.Errorf("%s: %s.%s; go through an internal/core port", fset.Position(sel.Pos()), pkg.Name, fn)
				}
			}
			return true
		})
	})
}

// TestCoreDoesNotImportTUIOrDirectIO checks core's ban list, not the
// stronger claim its old name made. internal/image is a deliberate,
// documented exemption (see ImageFetcher in ports.go): core.ImageFetcher's
// vocabulary is built from imgpkg's own types, and internal/image itself
// reaches net/http and os to fetch and decode images. Add it to the
// switch below only alongside introducing core-owned equivalents for
// those types — see ImageFetcher's doc comment for the tradeoff.
func TestCoreDoesNotImportTUIOrDirectIO(t *testing.T) {
	walkGoFiles(t, "../core", func(rel string, f *ast.File, _ *token.FileSet) {
		for _, imp := range f.Imports {
			path, _ := strconv.Unquote(imp.Path.Value)
			switch {
			case strings.HasPrefix(path, "github.com/gammons/slk/internal/ui"),
				strings.HasPrefix(path, "charm.land/"),
				path == "os", path == "os/exec", path == "net", path == "net/http", path == "database/sql":
				t.Errorf("core/%s imports %s", rel, path)
			}
		}
	})
}

// walkGoFiles parses every non-test .go file under root.
func walkGoFiles(t *testing.T, root string, visit func(rel string, f *ast.File, fset *token.FileSet)) {
	t.Helper()
	fset := token.NewFileSet()
	err := filepath.WalkDir(root, func(path string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if d.IsDir() && d.Name() == "testdata" {
			return filepath.SkipDir
		}
		if d.IsDir() || !strings.HasSuffix(path, ".go") || strings.HasSuffix(path, "_test.go") {
			return nil
		}
		f, err := parser.ParseFile(fset, path, nil, parser.SkipObjectResolution)
		if err != nil {
			return err
		}
		rel, _ := filepath.Rel(root, path)
		visit(rel, f, fset)
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
}

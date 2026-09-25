# Phase 1 — `cmd/slk/main.go` Topical Splits Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Move 3,655 lines of package-scope declarations out of `cmd/slk/main.go` into sixteen new topical files plus one append, leaving `main.go` at 1,647 lines holding only the entrypoint and `run()`, and add a test that keeps it that way.

**Architecture:** Pure code motion within one Go package. File placement is invisible to the Go compiler inside a package, so every declaration moves byte-for-byte with its doc comment. A purpose-built tool performs each move and prunes — never synthesizes — each file's import block. A second tool fingerprints every declaration's exact source bytes before and after, so "pure motion" is proven mechanically rather than asserted in review.

**Tech Stack:** Go 1.26.5, stdlib `go/ast` + `go/parser` + `go/format`. No new module dependencies. Tools are throwaway, built in `/tmp`, never committed.

**Spec:** [`../specs/2026-09-22-phase1-main-go-splits-design.md`](../specs/2026-09-22-phase1-main-go-splits-design.md)

---

## Global Constraints

Every task's requirements implicitly include this section.

- **Baseline commit:** `b733cce`. Every line number and byte count in this plan was measured there. If `main.go` has changed, re-measure before starting — do not adjust numbers by guesswork.
- **Branch:** all work on `refactor/phase1-main-go-splits`, one PR.
- **Go toolchain:** 1.26.5. Build with `go build ./...`, format check with `gofmt -l .` (must print nothing for tracked files; `.worktrees/` output is expected and ignorable).
- **Never hand-edit a moved declaration.** Not whitespace, not a comment inside it, not an import. Task 20's purity check compares SHA-256 of each declaration's exact bytes and will fail.
- **Never synthesize an import.** `main.go` has four aliased imports, two shadowing packages that are also imported (`slackclient` vs `slack-go/slack`, `imgpkg` vs `image`). The `movedecl` tool copies the import block verbatim and deletes only what is unused. Do not run `goimports`, and do not add an import by hand.
- **One commit per task** for tasks 2–19. Commit messages use the repo's conventional-commit style: `refactor(cmd/slk): ...`.
- **Every commit must build and pass `go test ./cmd/slk/`.** The PR is bisectable; a commit that does not build is a defect even if the next one fixes it.
- **Do not fix behaviour.** If you find a bug, record it for the Task 21 issue and move on. A refactor commit that also changes behaviour cannot be reviewed.
- **Scope:** `run()` does not move. Neither do `main`, `printHelp`, `newImageHTTPClient`, or the build-stamp vars. The three F2 data races inside `run()` are Phase 2 and are being fixed independently by PR #147 — do not touch them.

### The Standard Move Procedure (SMP)

Tasks 2–18 are all the same four steps. Each task states only its own parameters: destination file, symbol list, expected tool output, expected `main.go` line count, and any comment correction.

```bash
# (a) perform the move — $DEST and $SYMS come from the task
/tmp/phase1tools/movedecl/movedecl \
  -from cmd/slk/main.go -to "cmd/slk/$DEST" -syms "$SYMS"

# (b) verify the tree
go build ./... && go vet ./cmd/slk/ && gofmt -l cmd/slk/ && go test ./cmd/slk/

# (c) verify the size landed where the plan says
wc -l cmd/slk/main.go

# (d) commit
git add cmd/slk/main.go "cmd/slk/$DEST" && git commit -m "$MSG"
```

**Symbol syntax.** A plain declaration is its name (`xdgConfig`). A method is receiver-qualified (`(*rtmEventHandler).OnMessage`, `(sectionsProviderAdapter).Ready`). A `const`/`var`/`type` block is named by its first spec name. `movedecl` exits non-zero and names anything it cannot find, so a typo fails loudly rather than silently moving less than intended.

**If step (b) fails**, the cause is almost always an import: `undefined: X` means an import was wrongly pruned, `imported and not used` means one was wrongly kept. Both are `movedecl` bugs, not reasons to hand-edit. Fix the tool, `git checkout cmd/slk/`, and rerun.

---

## Task 1: Build the move and purity tooling

Nothing moves until both tools exist and the purity checker has been shown capable of failing. A verification tool that has only ever printed "identical" is worthless.

**Files:**
- Create: `/tmp/phase1tools/movedecl/main.go` (throwaway, not in the repo)
- Create: `/tmp/phase1tools/purity/main.go` (throwaway, not in the repo)
- Create: `/tmp/phase1-before.txt` (baseline fingerprint)

**Interfaces:**
- Produces: `/tmp/phase1tools/movedecl/movedecl` and `/tmp/phase1tools/purity/purity` binaries, used by every later task; `/tmp/phase1-before.txt`, consumed by Task 20.

- [ ] **Step 1: Create the branch**

```bash
cd /path/to/slk
git checkout -b refactor/phase1-main-go-splits
git log --oneline -1   # expect: b733cce (or later; see Global Constraints)
```

- [ ] **Step 2: Write `movedecl`**

Create `/tmp/phase1tools/movedecl/main.go`:

```go
// movedecl moves whole top-level declarations between files of one Go
// package, byte-for-byte, and prunes each file's import block to what it
// still uses. It never synthesizes an import, so aliases cannot be lost.
//
//	movedecl -from cmd/slk/main.go -to cmd/slk/paths.go -syms xdgConfig,xdgData,xdgCache
//
// Symbols are declaration names. Methods are written "(*T).Method" or
// "(T).Method". A const/var/type block is named by its first spec name.
package main

import (
	"bytes"
	"flag"
	"fmt"
	"go/ast"
	"go/format"
	"go/parser"
	"go/token"
	"os"
	"sort"
	"strings"
)

func declName(d ast.Decl) string {
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
			return "(" + recv + ")." + v.Name.Name
		}
		return v.Name.Name
	case *ast.GenDecl:
		for _, s := range v.Specs {
			switch sp := s.(type) {
			case *ast.TypeSpec:
				return sp.Name.Name
			case *ast.ValueSpec:
				if len(sp.Names) > 0 {
					return sp.Names[0].Name
				}
			}
		}
	}
	return ""
}

func declStart(fset *token.FileSet, d ast.Decl) token.Pos {
	switch v := d.(type) {
	case *ast.FuncDecl:
		if v.Doc != nil {
			return v.Doc.Pos()
		}
	case *ast.GenDecl:
		if v.Doc != nil {
			return v.Doc.Pos()
		}
	}
	return d.Pos()
}

// usedPkgNames returns every identifier used as the X of a selector, which
// is a superset of the file's package references. Over-keeping an import
// is caught by the compiler as "imported and not used"; under-keeping is
// caught as "undefined". Both directions are verified by `go build`.
func usedPkgNames(f *ast.File) map[string]bool {
	used := map[string]bool{}
	ast.Inspect(f, func(n ast.Node) bool {
		if se, ok := n.(*ast.SelectorExpr); ok {
			if id, ok := se.X.(*ast.Ident); ok {
				used[id.Name] = true
			}
		}
		return true
	})
	return used
}

func localName(spec *ast.ImportSpec) string {
	if spec.Name != nil {
		return spec.Name.Name
	}
	p := strings.Trim(spec.Path.Value, `"`)
	if i := strings.LastIndex(p, "/"); i >= 0 {
		p = p[i+1:]
	}
	return p
}

// prune drops import specs whose local name is unused, by deleting their
// exact byte ranges from src. It never reprints the AST: reprinting
// discards blank lines between declarations and would make the move
// non-pure. Blank and dot imports are always kept.
//
// The result is passed through format.Source, which is gofmt over text and
// preserves blank-line structure.
func prune(src []byte) ([]byte, error) {
	fset := token.NewFileSet()
	f, err := parser.ParseFile(fset, "x.go", src, parser.ParseComments)
	if err != nil {
		return nil, err
	}
	base := fset.File(f.Pos()).Base()
	used := usedPkgNames(f)

	type span struct{ lo, hi int }
	var cuts []span
	for _, d := range f.Decls {
		gd, ok := d.(*ast.GenDecl)
		if !ok || gd.Tok != token.IMPORT {
			continue
		}
		dropped := 0
		for _, s := range gd.Specs {
			is := s.(*ast.ImportSpec)
			n := localName(is)
			if n == "_" || n == "." || used[n] {
				continue
			}
			dropped++
			lo := int(is.Pos()) - base
			if is.Doc != nil {
				lo = int(is.Doc.Pos()) - base
			}
			hi := int(is.End()) - base
			if is.Comment != nil {
				hi = int(is.Comment.End()) - base
			}
			// take the whole line
			for lo > 0 && src[lo-1] != '\n' {
				lo--
			}
			for hi < len(src) && src[hi] != '\n' {
				hi++
			}
			if hi < len(src) {
				hi++
			}
			cuts = append(cuts, span{lo, hi})
		}
		if dropped == len(gd.Specs) {
			// every spec went: remove the whole declaration
			cuts = cuts[:len(cuts)-dropped]
			lo, hi := int(gd.Pos())-base, int(gd.End())-base
			for hi < len(src) && src[hi] != '\n' {
				hi++
			}
			if hi < len(src) {
				hi++
			}
			if hi < len(src) && src[hi] == '\n' {
				hi++
			}
			cuts = append(cuts, span{lo, hi})
		}
	}

	out := append([]byte(nil), src...)
	sort.Slice(cuts, func(i, j int) bool { return cuts[i].lo < cuts[j].lo })
	for i := len(cuts) - 1; i >= 0; i-- {
		out = append(out[:cuts[i].lo], out[cuts[i].hi:]...)
	}
	return format.Source(out)
}

func main() {
	from := flag.String("from", "", "source file")
	to := flag.String("to", "", "destination file (created, or appended to if it exists)")
	syms := flag.String("syms", "", "comma-separated declaration names")
	flag.Parse()
	if *from == "" || *to == "" || *syms == "" {
		fmt.Fprintln(os.Stderr, "usage: movedecl -from F -to F -syms a,b,(*T).M")
		os.Exit(2)
	}

	src, err := os.ReadFile(*from)
	check(err)
	fset := token.NewFileSet()
	f, err := parser.ParseFile(fset, *from, src, parser.ParseComments)
	check(err)
	base := fset.File(f.Pos()).Base()

	want := map[string]bool{}
	var order []string
	for _, s := range strings.Split(*syms, ",") {
		s = strings.TrimSpace(s)
		if s != "" {
			want[s] = true
			order = append(order, s)
		}
	}

	type span struct{ lo, hi int }
	found := map[string]span{}
	var importBlock []byte
	for _, d := range f.Decls {
		if gd, ok := d.(*ast.GenDecl); ok && gd.Tok == token.IMPORT {
			importBlock = src[int(gd.Pos())-base : int(gd.End())-base]
			continue
		}
		n := declName(d)
		if !want[n] {
			continue
		}
		lo := int(declStart(fset, d)) - base
		hi := int(d.End()) - base
		// extend through the rest of the line and one trailing blank line
		for hi < len(src) && src[hi] != '\n' {
			hi++
		}
		if hi < len(src) {
			hi++
		}
		if hi < len(src) && src[hi] == '\n' {
			hi++
		}
		found[n] = span{lo, hi}
	}

	var missing []string
	for _, n := range order {
		if _, ok := found[n]; !ok {
			missing = append(missing, n)
		}
	}
	if len(missing) > 0 {
		fmt.Fprintf(os.Stderr, "movedecl: not found in %s: %s\n", *from, strings.Join(missing, ", "))
		os.Exit(1)
	}

	// Collect moved text in source order, so the destination preserves
	// the original relative ordering of the declarations.
	spans := make([]span, 0, len(found))
	for _, s := range found {
		spans = append(spans, s)
	}
	sort.Slice(spans, func(i, j int) bool { return spans[i].lo < spans[j].lo })
	var moved bytes.Buffer
	for _, s := range spans {
		moved.Write(src[s.lo:s.hi])
	}

	// Cut from source, highest offset first.
	out := append([]byte(nil), src...)
	for i := len(spans) - 1; i >= 0; i-- {
		out = append(out[:spans[i].lo], out[spans[i].hi:]...)
	}

	var dst bytes.Buffer
	if existing, err := os.ReadFile(*to); err == nil {
		dst.Write(bytes.TrimRight(existing, "\n"))
		dst.WriteString("\n\n")
		dst.Write(moved.Bytes())
	} else {
		dst.WriteString("package " + f.Name.Name + "\n\n")
		dst.Write(importBlock)
		dst.WriteString("\n\n")
		dst.Write(moved.Bytes())
	}

	prunedSrc, err := prune(out)
	check(err)
	prunedDst, err := prune(dst.Bytes())
	check(err)
	check(os.WriteFile(*from, prunedSrc, 0o644))
	check(os.WriteFile(*to, prunedDst, 0o644))
	fmt.Printf("moved %d decls (%d bytes) %s -> %s\n", len(spans), moved.Len(), *from, *to)
}

func check(err error) {
	if err != nil {
		fmt.Fprintln(os.Stderr, "movedecl:", err)
		os.Exit(1)
	}
}
```

- [ ] **Step 3: Write `purity`**

Create `/tmp/phase1tools/purity/main.go`:

```go
// purity fingerprints a Go package's non-test files so that pure code
// motion can be proven rather than reviewed.
//
//	purity ./cmd/slk > /tmp/before.txt     # at the base commit
//	purity ./cmd/slk > /tmp/after.txt      # at the branch tip
//	diff /tmp/before.txt /tmp/after.txt    # must be empty
//
// It emits two sorted sections:
//
//	DECL <sha256> <name>   one line per non-import top-level declaration,
//	                       hashing its exact source bytes from the start of
//	                       its doc comment through its closing token.
//	IMPORT <alias> <path>  one line per distinct import, alias included.
//
// Neither section mentions a filename, so moving a declaration between
// files in the package is invisible to it — which is exactly the claim
// being tested. Any edit to a moved declaration, or any lost or
// synthesized import alias, changes the output.
package main

import (
	"crypto/sha256"
	"fmt"
	"go/ast"
	"go/parser"
	"go/token"
	"os"
	"path/filepath"
	"sort"
	"strings"
)

func declName(d ast.Decl) string {
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
			return "(" + recv + ")." + v.Name.Name
		}
		return v.Name.Name
	case *ast.GenDecl:
		var names []string
		for _, s := range v.Specs {
			switch sp := s.(type) {
			case *ast.TypeSpec:
				names = append(names, sp.Name.Name)
			case *ast.ValueSpec:
				for _, n := range sp.Names {
					names = append(names, n.Name)
				}
			}
		}
		return strings.Join(names, ",")
	}
	return "?"
}

func main() {
	if len(os.Args) != 2 {
		fmt.Fprintln(os.Stderr, "usage: purity <dir>")
		os.Exit(2)
	}
	entries, err := os.ReadDir(os.Args[1])
	if err != nil {
		fmt.Fprintln(os.Stderr, "purity:", err)
		os.Exit(1)
	}
	var decls, imports []string
	seenImport := map[string]bool{}

	for _, e := range entries {
		name := e.Name()
		if e.IsDir() || !strings.HasSuffix(name, ".go") || strings.HasSuffix(name, "_test.go") {
			continue
		}
		path := filepath.Join(os.Args[1], name)
		src, err := os.ReadFile(path)
		if err != nil {
			fmt.Fprintln(os.Stderr, "purity:", err)
			os.Exit(1)
		}
		fset := token.NewFileSet()
		f, err := parser.ParseFile(fset, path, src, parser.ParseComments)
		if err != nil {
			fmt.Fprintln(os.Stderr, "purity:", err)
			os.Exit(1)
		}
		base := fset.File(f.Pos()).Base()

		for _, d := range f.Decls {
			if gd, ok := d.(*ast.GenDecl); ok && gd.Tok == token.IMPORT {
				for _, s := range gd.Specs {
					is := s.(*ast.ImportSpec)
					alias := "-"
					if is.Name != nil {
						alias = is.Name.Name
					}
					line := fmt.Sprintf("IMPORT %s %s", alias, strings.Trim(is.Path.Value, `"`))
					if !seenImport[line] {
						seenImport[line] = true
						imports = append(imports, line)
					}
				}
				continue
			}
			lo := int(d.Pos()) - base
			switch v := d.(type) {
			case *ast.FuncDecl:
				if v.Doc != nil {
					lo = int(v.Doc.Pos()) - base
				}
			case *ast.GenDecl:
				if v.Doc != nil {
					lo = int(v.Doc.Pos()) - base
				}
			}
			hi := int(d.End()) - base
			sum := sha256.Sum256(src[lo:hi])
			decls = append(decls, fmt.Sprintf("DECL %x %s", sum[:8], declName(d)))
		}
	}
	sort.Strings(decls)
	sort.Strings(imports)
	for _, l := range decls {
		fmt.Println(l)
	}
	for _, l := range imports {
		fmt.Println(l)
	}
	fmt.Fprintf(os.Stderr, "purity: %d decls, %d distinct imports\n", len(decls), len(imports))
}
```

- [ ] **Step 4: Build both tools**

```bash
cd /tmp/phase1tools/movedecl && go mod init movedecl 2>/dev/null; go build -o movedecl .
cd /tmp/phase1tools/purity  && go mod init purity   2>/dev/null; go build -o purity .
```

Expected: no output from either `go build`.

- [ ] **Step 5: Capture the baseline fingerprint**

```bash
cd /path/to/slk
/tmp/phase1tools/purity/purity ./cmd/slk > /tmp/phase1-before.txt
```

Expected on stderr: `purity: 223 decls, 69 distinct imports`

If those counts differ, the tree is not at `b733cce`. Stop and re-measure the whole plan before continuing.

- [ ] **Step 6: Prove the purity checker can fail**

A checker that has only ever said "identical" proves nothing. Perturb one declaration by a single byte and confirm it is detected, then restore.

```bash
cd /path/to/slk
cp cmd/slk/main.go /tmp/main.orig
# add one space inside a function body
perl -0pi -e 's/func xdgCache\(\) string \{\n/func xdgCache() string {\n\n/' cmd/slk/main.go
/tmp/phase1tools/purity/purity ./cmd/slk > /tmp/phase1-perturbed.txt
diff /tmp/phase1-before.txt /tmp/phase1-perturbed.txt
```

Expected: a two-line diff showing the `DECL <hash> xdgCache` line changing hash. **If `diff` reports no difference, the checker is broken — stop and fix it.**

```bash
cp /tmp/main.orig cmd/slk/main.go
diff <(/tmp/phase1tools/purity/purity ./cmd/slk 2>/dev/null) /tmp/phase1-before.txt
```

Expected: no output — tree restored.

- [ ] **Step 7: Confirm the working tree is clean**

```bash
git status --short
```

Expected: no output. The tools live in `/tmp` and are never committed; there is nothing to commit for this task.

---

## Task 2: Move `paths.go`

The smallest move, chosen first so the procedure is validated on three trivial functions before anything large depends on it.

**Files:**
- Create: `cmd/slk/paths.go`
- Modify: `cmd/slk/main.go`

**Interfaces:**
- Consumes: `movedecl` binary from Task 1.
- Produces: `cmd/slk/paths.go` holding `xdgConfig`, `xdgData`, `xdgCache` — unchanged signatures, still package-scope, callable from anywhere in `package main` exactly as before.

- [ ] **Step 1: Move the declarations**

```bash
/tmp/phase1tools/movedecl/movedecl -from cmd/slk/main.go -to cmd/slk/paths.go \
  -syms 'xdgConfig,xdgData,xdgCache'
```

Expected: `moved 3 decls (586 bytes) cmd/slk/main.go -> cmd/slk/paths.go`

- [ ] **Step 2: Verify the tree**

```bash
go build ./... && go vet ./cmd/slk/ && gofmt -l cmd/slk/ && go test ./cmd/slk/
```

Expected: no output from build/vet/gofmt; `ok github.com/gammons/slk/cmd/slk`.

- [ ] **Step 3: Verify the size**

```bash
wc -l cmd/slk/main.go cmd/slk/paths.go
```

Expected: `5397 cmd/slk/main.go` and `30 cmd/slk/paths.go`.

- [ ] **Step 4: Confirm the diff is deletion-only**

```bash
git diff --stat cmd/slk/main.go
```

Expected: `1 file changed, 24 deletions(-)` — zero insertions. Every move in tasks 2–18 must show deletions only in `main.go`.

- [ ] **Step 5: Commit**

```bash
git add cmd/slk/main.go cmd/slk/paths.go
git commit -m "refactor(cmd/slk): move XDG path helpers to paths.go"
```

---

## Task 3: Move `thread_subscriptions.go`

The only append to an existing file. `OnThreadSubscriptionChanged` joins the thread-subscription code it belongs with, following the `peer_status.go` precedent of handler methods living with their topic.

**Files:**
- Modify: `cmd/slk/thread_subscriptions.go` (append), `cmd/slk/main.go`

**Interfaces:**
- Consumes: `movedecl` from Task 1.
- Produces: `(*rtmEventHandler).OnThreadSubscriptionChanged` relocated. The method set of `rtmEventHandler` is unchanged; it still satisfies the same event-handler interface in `internal/slack`.

- [ ] **Step 1: Move the declaration**

```bash
/tmp/phase1tools/movedecl/movedecl -from cmd/slk/main.go -to cmd/slk/thread_subscriptions.go \
  -syms '(*rtmEventHandler).OnThreadSubscriptionChanged'
```

Expected: `moved 1 decls (1838 bytes) cmd/slk/main.go -> cmd/slk/thread_subscriptions.go`

Note this appends to the existing file rather than creating one, so its import block is extended, not replaced.

- [ ] **Step 2: Verify the tree**

```bash
go build ./... && go vet ./cmd/slk/ && gofmt -l cmd/slk/ && go test ./cmd/slk/
```

Expected: clean; `ok github.com/gammons/slk/cmd/slk`.

- [ ] **Step 3: Verify the size**

```bash
wc -l cmd/slk/main.go cmd/slk/thread_subscriptions.go
```

Expected: `5359 cmd/slk/main.go` and `285 cmd/slk/thread_subscriptions.go` (was 247).

- [ ] **Step 4: Commit**

```bash
git add cmd/slk/main.go cmd/slk/thread_subscriptions.go
git commit -m "refactor(cmd/slk): move OnThreadSubscriptionChanged beside its subscription code"
```

---

## Task 4: Move `usergroups.go`

**Files:**
- Create: `cmd/slk/usergroups.go`
- Modify: `cmd/slk/main.go`

**Interfaces:**
- Produces: `usergroupHandles`, `slugifyHandle` relocated, unchanged.

- [ ] **Step 1: Move the declarations**

```bash
/tmp/phase1tools/movedecl/movedecl -from cmd/slk/main.go -to cmd/slk/usergroups.go \
  -syms 'usergroupHandles,slugifyHandle'
```

Expected: `moved 2 decls (1273 bytes) cmd/slk/main.go -> cmd/slk/usergroups.go`

- [ ] **Step 2: Verify the tree**

```bash
go build ./... && go vet ./cmd/slk/ && gofmt -l cmd/slk/ && go test ./cmd/slk/
```

Expected: clean; `ok github.com/gammons/slk/cmd/slk`.

- [ ] **Step 3: Verify the size**

```bash
wc -l cmd/slk/main.go cmd/slk/usergroups.go
```

Expected: `5317 cmd/slk/main.go` and `49 cmd/slk/usergroups.go`.

- [ ] **Step 4: Commit**

```bash
git add cmd/slk/main.go cmd/slk/usergroups.go
git commit -m "refactor(cmd/slk): move usergroup handle helpers to usergroups.go"
```

---

## Task 5: Move `conversations.go`

**Files:**
- Create: `cmd/slk/conversations.go`
- Modify: `cmd/slk/main.go`

**Interfaces:**
- Produces: `(*rtmEventHandler).OnConversationOpened`, `(*rtmEventHandler).addConversation`, `(*rtmEventHandler).publishConversation` relocated, unchanged.

- [ ] **Step 1: Move the declarations**

```bash
/tmp/phase1tools/movedecl/movedecl -from cmd/slk/main.go -to cmd/slk/conversations.go \
  -syms '(*rtmEventHandler).OnConversationOpened,(*rtmEventHandler).addConversation,(*rtmEventHandler).publishConversation'
```

Expected: `moved 3 decls (2963 bytes) cmd/slk/main.go -> cmd/slk/conversations.go`

- [ ] **Step 2: Verify the tree**

```bash
go build ./... && go vet ./cmd/slk/ && gofmt -l cmd/slk/ && go test ./cmd/slk/
```

Expected: clean; `ok github.com/gammons/slk/cmd/slk`.

- [ ] **Step 3: Verify the size**

```bash
wc -l cmd/slk/main.go cmd/slk/conversations.go
```

Expected: `5237 cmd/slk/main.go` and `88 cmd/slk/conversations.go`.

- [ ] **Step 4: Commit**

```bash
git add cmd/slk/main.go cmd/slk/conversations.go
git commit -m "refactor(cmd/slk): move conversation-opened handling to conversations.go"
```

---

## Task 6: Move `membership.go`

`OnPrefChange` goes here rather than into a `prefs.go`: its doc comment states the only pref slk reacts to is `muted_channels`, and its body routes entirely into `MuteStore` and `refreshMutedForActive`. It is mute logic wearing a generic name.

**Files:**
- Create: `cmd/slk/membership.go`
- Modify: `cmd/slk/main.go`

**Interfaces:**
- Produces: `(*rtmEventHandler).OnPrefChange`, `(*rtmEventHandler).OnMemberJoined`, `(*rtmEventHandler).OnMemberLeft`, `(*rtmEventHandler).refreshMutedForActive`, `muteRefreshMsg` relocated. `muteRefreshMsg` is referenced by `cmd/slk/event_handler_test.go`, which is unaffected — it is still a package-scope function.

- [ ] **Step 1: Move the declarations**

```bash
/tmp/phase1tools/movedecl/movedecl -from cmd/slk/main.go -to cmd/slk/membership.go \
  -syms '(*rtmEventHandler).OnPrefChange,(*rtmEventHandler).OnMemberJoined,(*rtmEventHandler).OnMemberLeft,(*rtmEventHandler).refreshMutedForActive,muteRefreshMsg'
```

Expected: `moved 5 decls (3533 bytes) cmd/slk/main.go -> cmd/slk/membership.go`

- [ ] **Step 2: Verify the tree**

```bash
go build ./... && go vet ./cmd/slk/ && gofmt -l cmd/slk/ && go test ./cmd/slk/
```

Expected: clean; `ok github.com/gammons/slk/cmd/slk`.

- [ ] **Step 3: Verify the size**

```bash
wc -l cmd/slk/main.go cmd/slk/membership.go
```

Expected: `5145 cmd/slk/main.go` and `100 cmd/slk/membership.go`.

- [ ] **Step 4: Commit**

```bash
git add cmd/slk/main.go cmd/slk/membership.go
git commit -m "refactor(cmd/slk): move membership and mute-pref handling to membership.go"
```

---

## Task 7: Move `attachments.go`

**Files:**
- Create: `cmd/slk/attachments.go`
- Modify: `cmd/slk/main.go`

**Interfaces:**
- Produces: `extractAttachments`, `extractBlocks`, `extractLegacyAttachments`, `collectThumbs`, `pickAttachmentURL` relocated. All five are called from the functions that will later land in `history.go` and `rtm_handler.go`; package-scope visibility is unchanged, so ordering between tasks does not matter.

- [ ] **Step 1: Move the declarations**

```bash
/tmp/phase1tools/movedecl/movedecl -from cmd/slk/main.go -to cmd/slk/attachments.go \
  -syms 'extractAttachments,extractBlocks,extractLegacyAttachments,collectThumbs,pickAttachmentURL'
```

Expected: `moved 5 decls (3432 bytes) cmd/slk/main.go -> cmd/slk/attachments.go`

- [ ] **Step 2: Verify the tree**

```bash
go build ./... && go vet ./cmd/slk/ && gofmt -l cmd/slk/ && go test ./cmd/slk/
```

Expected: clean; `ok github.com/gammons/slk/cmd/slk`.

- [ ] **Step 3: Verify the size**

```bash
wc -l cmd/slk/main.go cmd/slk/attachments.go
```

Expected: `5048 cmd/slk/main.go` and `106 cmd/slk/attachments.go`.

- [ ] **Step 4: Commit**

```bash
git add cmd/slk/main.go cmd/slk/attachments.go
git commit -m "refactor(cmd/slk): move Slack attachment extraction to attachments.go"
```

---

## Task 8: Move `workspace_search.go`

Named `workspace_search.go`, not `search.go`, because `cmd/slk/channel_search.go` already exists and covers a different thing: this is server-side message search, that is channel-name filtering.

`formatSearchTimestamp` comes here because its only caller is `searchResultItems`. `formatTimestamp` does **not** — it has seven callers, none in search, and lands in `history.go` in Task 18.

**Files:**
- Create: `cmd/slk/workspace_search.go`
- Modify: `cmd/slk/main.go`

**Interfaces:**
- Produces: `searchWorkspaceFunc`, `userIDShapeRe`, `searchResultItems`, `formatSearchTimestamp` relocated. `searchResultItems` is exercised by `cmd/slk/search_items_test.go`, which needs no change.

- [ ] **Step 1: Move the declarations**

```bash
/tmp/phase1tools/movedecl/movedecl -from cmd/slk/main.go -to cmd/slk/workspace_search.go \
  -syms 'searchWorkspaceFunc,userIDShapeRe,searchResultItems,formatSearchTimestamp'
```

Expected: `moved 4 decls (4548 bytes) cmd/slk/main.go -> cmd/slk/workspace_search.go`

- [ ] **Step 2: Verify the tree**

```bash
go build ./... && go vet ./cmd/slk/ && gofmt -l cmd/slk/ && go test ./cmd/slk/
```

Expected: clean; `ok github.com/gammons/slk/cmd/slk`.

- [ ] **Step 3: Verify the size**

```bash
wc -l cmd/slk/main.go cmd/slk/workspace_search.go
```

Expected: `4934 cmd/slk/main.go` and `129 cmd/slk/workspace_search.go`.

- [ ] **Step 4: Commit**

```bash
git add cmd/slk/main.go cmd/slk/workspace_search.go
git commit -m "refactor(cmd/slk): move workspace message search to workspace_search.go"
```

---

## Task 9: Move `sections.go`

`sectionsProviderAdapter` moves with the section handlers it feeds, rather than staying beside `WorkspaceContext` where it happens to be declared today.

**Files:**
- Create: `cmd/slk/sections.go`
- Modify: `cmd/slk/main.go`

**Interfaces:**
- Produces: `sectionsProviderAdapter` and its two methods, plus `(*rtmEventHandler).refreshSectionsForActive` and the four `OnChannelSection*` handlers, relocated.

- [ ] **Step 1: Move the declarations**

```bash
/tmp/phase1tools/movedecl/movedecl -from cmd/slk/main.go -to cmd/slk/sections.go \
  -syms 'sectionsProviderAdapter,(sectionsProviderAdapter).Ready,(sectionsProviderAdapter).OrderedSlackSections,(*rtmEventHandler).refreshSectionsForActive,(*rtmEventHandler).OnChannelSectionUpserted,(*rtmEventHandler).OnChannelSectionDeleted,(*rtmEventHandler).OnChannelSectionChannelsUpserted,(*rtmEventHandler).OnChannelSectionChannelsRemoved'
```

Expected: `moved 8 decls (4393 bytes) cmd/slk/main.go -> cmd/slk/sections.go`

Note the two adapter methods have a **value** receiver, written `(sectionsProviderAdapter).Ready` with no star. Getting this wrong makes `movedecl` exit 1 naming the symbol.

- [ ] **Step 2: Verify the tree**

```bash
go build ./... && go vet ./cmd/slk/ && gofmt -l cmd/slk/ && go test ./cmd/slk/
```

Expected: clean; `ok github.com/gammons/slk/cmd/slk`.

- [ ] **Step 3: Verify the size**

```bash
wc -l cmd/slk/main.go cmd/slk/sections.go
```

Expected: `4810 cmd/slk/main.go` and `132 cmd/slk/sections.go`.

- [ ] **Step 4: Commit**

```bash
git add cmd/slk/main.go cmd/slk/sections.go
git commit -m "refactor(cmd/slk): move channel-section handling to sections.go"
```

---

## Task 10: Move `dump.go`

The three diagnostic subcommands reachable from `main`, not from `run`.

**Files:**
- Create: `cmd/slk/dump.go`
- Modify: `cmd/slk/main.go`

**Interfaces:**
- Produces: `listWorkspaces`, `dumpPrefs`, `dumpSections` relocated. All three are called from `main`, which stays in `main.go` — same package, so the call sites are untouched.

- [ ] **Step 1: Move the declarations**

```bash
/tmp/phase1tools/movedecl/movedecl -from cmd/slk/main.go -to cmd/slk/dump.go \
  -syms 'listWorkspaces,dumpPrefs,dumpSections'
```

Expected: `moved 3 decls (4490 bytes) cmd/slk/main.go -> cmd/slk/dump.go`

- [ ] **Step 2: Verify the tree**

```bash
go build ./... && go vet ./cmd/slk/ && gofmt -l cmd/slk/ && go test ./cmd/slk/
```

Expected: clean; `ok github.com/gammons/slk/cmd/slk`.

- [ ] **Step 3: Verify the size**

```bash
wc -l cmd/slk/main.go cmd/slk/dump.go
```

Expected: `4675 cmd/slk/main.go` and `149 cmd/slk/dump.go`.

- [ ] **Step 4: Commit**

```bash
git add cmd/slk/main.go cmd/slk/dump.go
git commit -m "refactor(cmd/slk): move diagnostic dump subcommands to dump.go"
```

---

## Task 11: Move `presence.go`

Presence and DND bootstrap plus the three presence handler methods. Note `cmd/slk/peer_status.go` already holds `OnUserStatusChange`, `OnUserInvalidated`, `OnDNDInvalidated` and `OnUserDNDChange` — this task deliberately does **not** merge into it, because those are peer-status refresh concerns while these are the workspace's own presence subscription.

**Files:**
- Create: `cmd/slk/presence.go`
- Modify: `cmd/slk/main.go`

**Interfaces:**
- Produces: `bootstrapPresenceAndDND`, `subscribeWorkspacePresence`, `workspacePresenceIDs`, and the `OnPresenceChange` / `OnSelfPresenceChange` / `OnDNDChange` handlers relocated. `cmd/slk/peer_status_test.go` is unaffected.

- [ ] **Step 1: Move the declarations**

```bash
/tmp/phase1tools/movedecl/movedecl -from cmd/slk/main.go -to cmd/slk/presence.go \
  -syms 'bootstrapPresenceAndDND,subscribeWorkspacePresence,workspacePresenceIDs,(*rtmEventHandler).OnPresenceChange,(*rtmEventHandler).OnSelfPresenceChange,(*rtmEventHandler).OnDNDChange'
```

Expected: `moved 6 decls (4938 bytes) cmd/slk/main.go -> cmd/slk/presence.go`

- [ ] **Step 2: Verify the tree**

```bash
go build ./... && go vet ./cmd/slk/ && gofmt -l cmd/slk/ && go test ./cmd/slk/
```

Expected: clean; `ok github.com/gammons/slk/cmd/slk`.

- [ ] **Step 3: Verify the size**

```bash
wc -l cmd/slk/main.go cmd/slk/presence.go
```

Expected: `4523 cmd/slk/main.go` and `162 cmd/slk/presence.go`.

- [ ] **Step 4: Commit**

```bash
git add cmd/slk/main.go cmd/slk/presence.go
git commit -m "refactor(cmd/slk): move presence and DND bootstrap to presence.go"
```

---

## Task 12: Move `users.go` and correct three comments

The hottest region in the file — PRs #147, #167 and #168 all edit it. This task also carries three of the six comment corrections, because `resolveUser` is named by filename in `internal/bootstrap`.

`messageAuthor` comes here even though all three of its callers land in `history.go`. Topic wins: it resolves a display name through the same `userNames`/cache path as its neighbours here, and grouping it with them is what makes the user-naming logic findable in one place. This is deliberate; do not "fix" it by caller locality.

**Files:**
- Create: `cmd/slk/users.go`
- Modify: `cmd/slk/main.go`, `internal/bootstrap/revalidate.go`, `internal/bootstrap/revalidate_test.go`

**Interfaces:**
- Produces: `lookupUserCached`, `resolveUserCached`, `resolveUser`, `resolveDMNames`, `messageAuthor` relocated. `cmd/slk/user_resolver_test.go` and `cmd/slk/on_message_mention_test.go` are unaffected.

- [ ] **Step 1: Move the declarations**

```bash
/tmp/phase1tools/movedecl/movedecl -from cmd/slk/main.go -to cmd/slk/users.go \
  -syms 'lookupUserCached,resolveUserCached,resolveUser,resolveDMNames,messageAuthor'
```

Expected: `moved 5 decls (7494 bytes) cmd/slk/main.go -> cmd/slk/users.go`

- [ ] **Step 2: Correct the first stale comment**

In `internal/bootstrap/revalidate.go`, find:

```go
// userDisplayName picks the name to show, mirroring the fallback chain
// resolveUser already uses (main.go:2432): display name, then real
// name, then the handle.
```

Replace the middle line so the citation names the package, not a file and a line number that is already wrong:

```go
// userDisplayName picks the name to show, mirroring the fallback chain
// resolveUser already uses (in cmd/slk): display name, then real
// name, then the handle.
```

- [ ] **Step 3: Correct the second stale comment**

In `internal/bootstrap/revalidate.go`, find:

```go
// isExternal reports whether a user's home team differs from this
// workspace's — a Slack Connect or shared-channel guest. Same test
// resolveUser applies (main.go:2440), including the empty guard: a
// result with no team_id is unknown, not foreign.
```

Replace the third line:

```go
// isExternal reports whether a user's home team differs from this
// workspace's — a Slack Connect or shared-channel guest. Same test
// resolveUser applies (in cmd/slk), including the empty guard: a
// result with no team_id is unknown, not foreign.
```

- [ ] **Step 4: Correct the third stale comment**

In `internal/bootstrap/revalidate_test.go`, find:

```go
	// Same test resolveUser applies (main.go:2440), empty guard
	// included: a result with no team_id is unknown, not foreign, and
	// marking it external puts a Slack Connect badge on a colleague.
```

Replace the first line:

```go
	// Same test resolveUser applies (in cmd/slk), empty guard
	// included: a result with no team_id is unknown, not foreign, and
	// marking it external puts a Slack Connect badge on a colleague.
```

- [ ] **Step 5: Verify the tree**

```bash
go build ./... && go vet ./cmd/slk/ && gofmt -l cmd/slk/ internal/bootstrap/ && go test ./cmd/slk/ ./internal/bootstrap/
```

Expected: clean; `ok` for both packages.

- [ ] **Step 6: Verify the size and the comment diff**

```bash
wc -l cmd/slk/main.go cmd/slk/users.go
git diff --stat internal/bootstrap/
```

Expected: `4312 cmd/slk/main.go`, `222 cmd/slk/users.go`; and `2 files changed, 3 insertions(+), 3 deletions(-)` — comment text only, nothing else.

- [ ] **Step 7: Commit**

```bash
git add cmd/slk/main.go cmd/slk/users.go internal/bootstrap/revalidate.go internal/bootstrap/revalidate_test.go
git commit -m "refactor(cmd/slk): move user name resolution to users.go

Also retires three comment citations of the form (main.go:2432) in
internal/bootstrap. Every line-number citation into main.go in this repo
is already wrong; replacing them with the package name rather than a new
filename means they do not rot again when Phase 2 moves things."
```

---

## Task 13: Move `workspace.go`

The workspace value types and the router over them. `mostRecentlyVisitedChannel` comes along because it reads the router's last-visited map.

**Files:**
- Create: `cmd/slk/workspace.go`
- Modify: `cmd/slk/main.go`

**Interfaces:**
- Produces: `UnresolvedDM`, `WorkspaceContext` and its four methods, `workspaceRouter` and its six, and `mostRecentlyVisitedChannel`, relocated. `WorkspaceContext` is referenced across the package and by tests in `cmd/slk/bootstrap_adapters_test.go`; all unaffected.

- [ ] **Step 1: Move the declarations**

```bash
/tmp/phase1tools/movedecl/movedecl -from cmd/slk/main.go -to cmd/slk/workspace.go \
  -syms 'UnresolvedDM,WorkspaceContext,(*WorkspaceContext).UserGroups,(*WorkspaceContext).SetUserGroups,(*WorkspaceContext).CustomEmoji,(*WorkspaceContext).SetCustomEmoji,workspaceRouter,newWorkspaceRouter,(*workspaceRouter).Active,(*workspaceRouter).Set,(*workspaceRouter).ByID,(*workspaceRouter).Add,(*workspaceRouter).All,mostRecentlyVisitedChannel'
```

Expected: `moved 14 decls (11578 bytes) cmd/slk/main.go -> cmd/slk/workspace.go`

- [ ] **Step 2: Verify the tree**

```bash
go build ./... && go vet ./cmd/slk/ && gofmt -l cmd/slk/ && go test ./cmd/slk/
```

Expected: clean; `ok github.com/gammons/slk/cmd/slk`.

- [ ] **Step 3: Verify the size**

```bash
wc -l cmd/slk/main.go cmd/slk/workspace.go
```

Expected: `4059 cmd/slk/main.go` and `266 cmd/slk/workspace.go`.

- [ ] **Step 4: Commit**

```bash
git add cmd/slk/main.go cmd/slk/workspace.go
git commit -m "refactor(cmd/slk): move WorkspaceContext and the workspace router to workspace.go"
```

---

## Task 14: Move `markread.go` and correct one comment

RFC #236 names "marking a conversation read" as its flagship example of a feature that should collapse into a widget. Isolating it here makes that future change legible.

**Files:**
- Create: `cmd/slk/markread.go`
- Modify: `cmd/slk/main.go`, `internal/ui/reducer_focus_test.go`

**Interfaces:**
- Produces: `threadMarker`, `markThreadRead`, `channelMarker`, `messageMentionsSelf`, `countMentionsSince`, `markChannelRead`, `markChannelReadAndNotify`, `markChannelReadAsync`, and the `OnChannelMarked` / `OnThreadMarked` handlers, relocated. `cmd/slk/mark_read_mention_test.go` and `cmd/slk/event_handler_marked_test.go` are unaffected.

- [ ] **Step 1: Move the declarations**

```bash
/tmp/phase1tools/movedecl/movedecl -from cmd/slk/main.go -to cmd/slk/markread.go \
  -syms 'threadMarker,markThreadRead,channelMarker,messageMentionsSelf,countMentionsSince,markChannelRead,markChannelReadAndNotify,markChannelReadAsync,(*rtmEventHandler).OnChannelMarked,(*rtmEventHandler).OnThreadMarked'
```

Expected: `moved 10 decls (13877 bytes) cmd/slk/main.go -> cmd/slk/markread.go`

- [ ] **Step 2: Correct the stale comment**

In `internal/ui/reducer_focus_test.go`, find:

```go
// channel_marked event, which cmd/slk/main.go's OnChannelMarked turns
```

Replace with:

```go
// channel_marked event, which cmd/slk's OnChannelMarked turns
```

- [ ] **Step 3: Verify the tree**

```bash
go build ./... && go vet ./cmd/slk/ && gofmt -l cmd/slk/ internal/ui/ && go test ./cmd/slk/
```

Expected: clean; `ok github.com/gammons/slk/cmd/slk`.

- [ ] **Step 4: Verify the size and the comment diff**

```bash
wc -l cmd/slk/main.go cmd/slk/markread.go
git diff --stat internal/ui/
```

Expected: `3737 cmd/slk/main.go`, `334 cmd/slk/markread.go`; and `1 file changed, 1 insertion(+), 1 deletion(-)`.

- [ ] **Step 5: Commit**

```bash
git add cmd/slk/main.go cmd/slk/markread.go internal/ui/reducer_focus_test.go
git commit -m "refactor(cmd/slk): move channel and thread mark-read to markread.go"
```

---

## Task 15: Move `user_resolver.go`

A self-contained type with its own 740-line test file. The largest single-type extraction in the plan.

**Files:**
- Create: `cmd/slk/user_resolver.go`
- Modify: `cmd/slk/main.go`

**Interfaces:**
- Produces: `userResolverConcurrency`, `userResolverBatchWindow`, `userBatcher`, `userResolver` and its seven methods, plus `bestBotIcon`, relocated. `cmd/slk/user_resolver_test.go` needs no change; it constructs `userResolver` directly and that type is still package-scope.

- [ ] **Step 1: Move the declarations**

```bash
/tmp/phase1tools/movedecl/movedecl -from cmd/slk/main.go -to cmd/slk/user_resolver.go \
  -syms 'userResolverConcurrency,userResolverBatchWindow,userBatcher,userResolver,newUserResolver,(*userResolver).Request,(*userResolver).resolveOne,(*userResolver).flush,(*userResolver).ResolveNow,(*userResolver).applyEdgeUser,(*userResolver).RequestBot,bestBotIcon'
```

Expected: `moved 12 decls (15187 bytes) cmd/slk/main.go -> cmd/slk/user_resolver.go`

- [ ] **Step 2: Verify the tree**

```bash
go build ./... && go vet ./cmd/slk/ && gofmt -l cmd/slk/ && go test ./cmd/slk/
```

Expected: clean; `ok github.com/gammons/slk/cmd/slk`.

- [ ] **Step 3: Verify the size**

```bash
wc -l cmd/slk/main.go cmd/slk/user_resolver.go
```

Expected: `3304 cmd/slk/main.go` and `449 cmd/slk/user_resolver.go`.

- [ ] **Step 4: Commit**

```bash
git add cmd/slk/main.go cmd/slk/user_resolver.go
git commit -m "refactor(cmd/slk): move the batching user resolver to user_resolver.go"
```

---

## Task 16: Move `rtm_handler.go` and correct one comment

The handler type itself plus the message and reaction events. Everything else that was a method on this type has already gone to its topic file in tasks 3, 5, 6, 9 and 11; `OnConnect` and friends go in Task 17. This is what keeps `rtm_handler.go` at 492 lines instead of 1,090.

**Files:**
- Create: `cmd/slk/rtm_handler.go`
- Modify: `cmd/slk/main.go`, `internal/ui/reducer_workspace.go`

**Interfaces:**
- Produces: `rtmEventHandler`, `discoveryRetryAfter`, `(*rtmEventHandler).discoverConversation`, `OnMessage`, `OnMessageDeleted`, `OnReactionAdded`, `OnReactionRemoved`, `OnUserTyping`, relocated. `cmd/slk/event_handler_test.go` and `cmd/slk/reconnect_sync_test.go` are unaffected.

- [ ] **Step 1: Move the declarations**

```bash
/tmp/phase1tools/movedecl/movedecl -from cmd/slk/main.go -to cmd/slk/rtm_handler.go \
  -syms 'rtmEventHandler,discoveryRetryAfter,(*rtmEventHandler).discoverConversation,(*rtmEventHandler).OnMessage,(*rtmEventHandler).OnMessageDeleted,(*rtmEventHandler).OnReactionAdded,(*rtmEventHandler).OnReactionRemoved,(*rtmEventHandler).OnUserTyping'
```

Expected: `moved 8 decls (18991 bytes) cmd/slk/main.go -> cmd/slk/rtm_handler.go`

- [ ] **Step 2: Correct the stale comment**

In `internal/ui/reducer_workspace.go`, find:

```go
		// and FinderItems from the rtmEventHandler in cmd/slk/main.go;
```

Replace with:

```go
		// and FinderItems from the rtmEventHandler in cmd/slk;
```

Eight lines above, the same file already reads "cmd/slk's `rtmEventHandler`". This makes the two consistent.

- [ ] **Step 3: Verify the tree**

```bash
go build ./... && go vet ./cmd/slk/ && gofmt -l cmd/slk/ internal/ui/ && go test ./cmd/slk/
```

Expected: clean; `ok github.com/gammons/slk/cmd/slk`.

- [ ] **Step 4: Verify the size and the comment diff**

```bash
wc -l cmd/slk/main.go cmd/slk/rtm_handler.go
git diff --stat internal/ui/reducer_workspace.go
```

Expected: `2830 cmd/slk/main.go`, `492 cmd/slk/rtm_handler.go`; and `1 file changed, 1 insertion(+), 1 deletion(-)`.

- [ ] **Step 5: Commit**

```bash
git add cmd/slk/main.go cmd/slk/rtm_handler.go internal/ui/reducer_workspace.go
git commit -m "refactor(cmd/slk): move the RTM handler type and message events to rtm_handler.go"
```

---

## Task 17: Move `connect.go` and correct one comment

`connectWorkspace` plus the connection-lifecycle handler methods that manage the same socket.

**Files:**
- Create: `cmd/slk/connect.go`
- Modify: `cmd/slk/main.go`, `cmd/slk/bootstrap_adapters_test.go`

**Interfaces:**
- Produces: `shouldReloadTimeout`, `connectWorkspace`, `(*rtmEventHandler).OnConnect`, `refreshActiveMembership`, `syncOnReconnect`, `OnDisconnect`, relocated. `connectWorkspace`'s signature is unchanged — Phase 2 restructures its internals, this task does not.

- [ ] **Step 1: Move the declarations**

```bash
/tmp/phase1tools/movedecl/movedecl -from cmd/slk/main.go -to cmd/slk/connect.go \
  -syms 'shouldReloadTimeout,connectWorkspace,(*rtmEventHandler).OnConnect,(*rtmEventHandler).refreshActiveMembership,(*rtmEventHandler).syncOnReconnect,(*rtmEventHandler).OnDisconnect'
```

Expected: `moved 6 decls (24255 bytes) cmd/slk/main.go -> cmd/slk/connect.go`

- [ ] **Step 2: Correct the first stale comment**

In `cmd/slk/bootstrap_adapters_test.go`, find:

```go
// so this covers the adapter link and the reconnect test covers the
// write. The literal in main.go between them is reviewed, not tested.
```

Replace with:

```go
// so this covers the adapter link and the reconnect test covers the
// write. The literal between them is reviewed, not tested.
```

The file reference is redundant: `connectWorkspace` is already named three lines above, and naming it is what stays true when the file changes.

- [ ] **Step 3: Correct the second stale comment**

> Added during execution. The plan's original sweep missed this site because it
> grepped for the moving symbol and the string `main.go` on the same line, and
> this citation wraps. See the correction note in §7 of the spec.

In `internal/bootstrap/revalidate.go`, find:

```go
// "dm". That is recoverable rather than lost: connectWorkspace
// re-derives "app" from the cached users' is_bot on every boot
// (main.go:1941), so the column is corrected before it is rendered.
```

Replace the third line so the citation names the package rather than a file and
a line number that is already wrong:

```go
// "dm". That is recoverable rather than lost: connectWorkspace
// re-derives "app" from the cached users' is_bot on every boot
// (in cmd/slk), so the column is corrected before it is rendered.
```

- [ ] **Step 4: Verify the tree**

```bash
go build ./... && go vet ./cmd/slk/ && gofmt -l cmd/slk/ internal/bootstrap/ && go test ./cmd/slk/ ./internal/bootstrap/
```

Expected: clean; `ok` for both packages.

- [ ] **Step 5: Verify the size and the comment diff**

```bash
wc -l cmd/slk/main.go cmd/slk/connect.go
git diff --stat internal/bootstrap/
```

Expected: `2272 cmd/slk/main.go`, `576 cmd/slk/connect.go`; and
`1 file changed, 1 insertion(+), 1 deletion(-)`.

- [ ] **Step 6: Commit**

```bash
git add cmd/slk/main.go cmd/slk/connect.go cmd/slk/bootstrap_adapters_test.go internal/bootstrap/revalidate.go
git commit -m "refactor(cmd/slk): move workspace connect and reconnect lifecycle to connect.go

Also retires two comment citations that named connectWorkspace by file —
one in cmd/slk, one in internal/bootstrap — replacing them with the
package name so they survive Phase 2 moving things again."
```

---

## Task 18: Move `history.go`

The last and largest move. `formatTimestamp` comes here rather than to `workspace_search.go`: it has seven callers — two in `run`, **four in the functions in this file**, one in `OnMessage` — and none in search.

**Files:**
- Create: `cmd/slk/history.go`
- Modify: `cmd/slk/main.go`

**Interfaces:**
- Produces: `fetchOlderMessages`, `fetchMessagesAround`, `convertAndCacheHistory`, `summarizeMessages`, `summarizeCachedRows`, `enrichPerfStats`, `loadCachedMessages`, `enrichCachedRow`, `loadCachedThreadReplies`, `fetchChannelMessages`, `fetchThreadReplies`, `formatTimestamp`, relocated. `cmd/slk/summarize_test.go` and `cmd/slk/cache_render_test.go` are unaffected.

- [ ] **Step 1: Move the declarations**

```bash
/tmp/phase1tools/movedecl/movedecl -from cmd/slk/main.go -to cmd/slk/history.go \
  -syms 'fetchOlderMessages,fetchMessagesAround,convertAndCacheHistory,summarizeMessages,summarizeCachedRows,enrichPerfStats,loadCachedMessages,enrichCachedRow,loadCachedThreadReplies,fetchChannelMessages,fetchThreadReplies,formatTimestamp'
```

Expected: `moved 12 decls (22703 bytes) cmd/slk/main.go -> cmd/slk/history.go`

- [ ] **Step 2: Verify the tree**

```bash
go build ./... && go vet ./cmd/slk/ && gofmt -l cmd/slk/ && go test ./cmd/slk/
```

Expected: clean; `ok github.com/gammons/slk/cmd/slk`.

- [ ] **Step 3: Verify the final size**

```bash
wc -l cmd/slk/main.go cmd/slk/history.go
```

Expected: `1647 cmd/slk/main.go` and `639 cmd/slk/history.go`.

This is the exit number. `main.go` has gone from 5,421 to 1,647.

- [ ] **Step 4: Confirm what is left**

```bash
grep -n '^func \|^var \|^type \|^const ' cmd/slk/main.go
```

Expected exactly five lines: `var (` (the build stamps), `func main`, `func printHelp`, `func newImageHTTPClient`, `func run`.

- [ ] **Step 5: Commit**

```bash
git add cmd/slk/main.go cmd/slk/history.go
git commit -m "refactor(cmd/slk): move message history fetch and cache enrichment to history.go"
```

---

## Task 19: Add the regrowth guard

`main.go` grew from 4,842 to 5,421 lines in the four months after the refactor baseline was taken. Moving lines once without a guard means doing this again. This test is the only thing in the PR that is not code motion.

**Files:**
- Create: `cmd/slk/main_scope_test.go`

**Interfaces:**
- Consumes: the post-Task-18 state of `main.go`.
- Produces: `TestMainGoHoldsOnlyTheEntrypoint`, run by `go test ./cmd/slk/`.

- [ ] **Step 1: Write the test**

Create `cmd/slk/main_scope_test.go`:

```go
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
```

- [ ] **Step 2: Run it and verify it passes**

```bash
go test ./cmd/slk/ -run TestMainGoHoldsOnlyTheEntrypoint -v
```

Expected:

```
=== RUN   TestMainGoHoldsOnlyTheEntrypoint
--- PASS: TestMainGoHoldsOnlyTheEntrypoint (0.00s)
PASS
```

- [ ] **Step 3: Prove it fails on an offender**

A guard that has only ever passed is not a guard. Add a declaration that should be rejected:

```bash
cp cmd/slk/main.go /tmp/main.bak
printf '\nfunc sneakyHelper() string { return "" }\n\ntype sneakyType struct{}\n' >> cmd/slk/main.go
go test ./cmd/slk/ -run TestMainGoHoldsOnlyTheEntrypoint
```

Expected: FAIL, naming both symbols with file:line —

```
main.go declares 2 symbol(s) that belong in a topical file:
  sneakyHelper  (main.go:1649:1)
  sneakyType  (main.go:1651:1)
```

Restore:

```bash
cp /tmp/main.bak cmd/slk/main.go
```

- [ ] **Step 4: Prove it fails on a stale allow-list entry**

```bash
sed -i 's|"printHelp":          "usage text for main",|"printHelp":          "usage text for main",\n\t"ghostSymbol":        "does not exist",|' cmd/slk/main_scope_test.go
go test ./cmd/slk/ -run TestMainGoHoldsOnlyTheEntrypoint
```

Expected: FAIL with `mainGoAllowed lists 1 symbol(s) no longer declared in main.go: ghostSymbol`.

Restore:

```bash
sed -i '/ghostSymbol/d' cmd/slk/main_scope_test.go
go test ./cmd/slk/ -run TestMainGoHoldsOnlyTheEntrypoint
```

Expected: PASS.

- [ ] **Step 5: Verify the tree is exactly as before the experiments**

```bash
git status --short
```

Expected: only `?? cmd/slk/main_scope_test.go`. If `main.go` or any other file shows as modified, the restores above did not work — fix before committing.

- [ ] **Step 6: Commit**

```bash
gofmt -l cmd/slk/main_scope_test.go   # expect no output
git add cmd/slk/main_scope_test.go
git commit -m "test(cmd/slk): keep main.go to the entrypoint

main.go grew from 4,842 to 5,421 lines in the four months after the
refactor baseline was taken. Phase 1 moved 3,655 of those into topical
files; this asserts they stay there, by allow-listing the seven
declarations main.go is permitted to hold rather than by capping its size.
An allow-list needs no re-blessing as run() changes size, and Phase 2
deletes the run entry rather than adjusting a number."
```

---

## Task 20: Whole-branch verification and PR

The purity proof runs once, over the whole branch. Everything before this point was per-commit sanity; this is the evidence that goes in the PR description.

**Files:**
- Create: `/tmp/phase1-after.txt`, `/tmp/phase1-rebase-table.md` (both throwaway; the table's contents go into the PR description)

**Interfaces:**
- Consumes: `/tmp/phase1-before.txt` from Task 1.

- [ ] **Step 1: Run the purity proof**

```bash
/tmp/phase1tools/purity/purity ./cmd/slk > /tmp/phase1-after.txt
diff /tmp/phase1-before.txt /tmp/phase1-after.txt && echo "PURITY: IDENTICAL"
```

Expected: `PURITY: IDENTICAL`, with `purity: 223 decls, 69 distinct imports` on stderr both times.

**If `diff` produces output, stop.** A `DECL` line means a declaration's bytes changed — find it, `git log -S` the hash, and restore the original. An `IMPORT` line means an alias was lost or invented, which is the specific failure mode §2 of the spec exists to prevent.

- [ ] **Step 2: Run the full verification suite**

```bash
go build ./...
go vet ./...
gofmt -l . | grep -v '^\.worktrees/' ; echo "(no output above = clean)"
go test ./... -race
```

Expected: build and vet silent; gofmt clean; all packages `ok`, 57 of them, zero failures.

- [ ] **Step 3: Confirm the scope of the change**

```bash
git diff --stat main...HEAD -- . ':(exclude)cmd/slk' ':(exclude)docs'
```

Expected: exactly four files — `internal/bootstrap/revalidate.go`, `internal/bootstrap/revalidate_test.go`, `internal/ui/reducer_focus_test.go`, `internal/ui/reducer_workspace.go` — totalling `4 files changed, 7 insertions(+), 7 deletions(-)`, all comment text. (`revalidate.go` carries three of the seven edits and `revalidate_test.go` two.)

```bash
git diff main...HEAD --stat -- cmd/slk | tail -1
```

Expected: `19 files changed` — 16 new topical files, `thread_subscriptions.go`, `main.go`, and `main_scope_test.go`.

- [ ] **Step 4: Generate the rebase table**

Six open PRs touch `main.go` and will conflict. They need a symbol → file → sha table so a rebasing author can find a declaration without searching seventeen files.

The authoritative symbol lists are already in this plan — they are the `-syms` arguments in tasks 2–18. All that is missing is each file's commit sha. Each destination file is touched by exactly one commit in the range, so:

```bash
for f in paths thread_subscriptions usergroups conversations membership \
         attachments workspace_search sections dump presence users \
         workspace markread user_resolver rtm_handler connect history; do
  printf '%-24s %s\n' "$f.go" \
    "$(git log --format=%h -1 main..HEAD -- "cmd/slk/$f.go")"
done
```

Expected: seventeen lines, each with a distinct 7-character sha, none empty. An empty sha means that task's commit did not land — stop and check.

Now write `/tmp/phase1-rebase-table.md` as a Markdown table with one row per symbol: take each task's `-syms` list verbatim, pair each entry with that task's destination file and the sha printed above. The table has **107 rows** — one per moved declaration, matching the sum of the `moved N decls` counts in tasks 2–18. Sort it by symbol name, since alphabetical is how a rebasing author will use it.

- [ ] **Step 5: Open the PR**

Push and open the PR. The description must contain:

1. **What moved and why** — one paragraph, plus the line-count before/after.
2. **The purity proof output** — the `PURITY: IDENTICAL` result and the decl/import counts, stated as the evidence that no declaration was altered.
3. **The rebase table** from step 4, under a heading that names the six affected PRs (#109, #147, #150, #167, #168, #227) so their authors find it.
4. **The rebase procedure**, verbatim:

   > Phase 1 moved code between files in package `cmd/slk`. No declaration was
   > renamed, re-signatured or semantically changed; only import blocks were
   > re-split. To rebase:
   >
   > 1. `git rebase origin/main`
   > 2. Conflicts in `main.go` appear as your hunk against deleted context. Take
   >    `main.go` from upstream wholesale: `git checkout --theirs cmd/slk/main.go`
   > 3. Re-apply your original hunk to the declaration in its new home, found in
   >    the table above. Because the declaration is byte-identical, the hunk
   >    applies as-is.
   > 4. Verify with `git diff origin/main...HEAD` — it should show only your
   >    intended change.
   >
   > #109 and #227 touch only `run()` and `main()`, which did not move, and
   > should rebase cleanly.

5. **A note that #147 is not superseded** — it fixes an F2 data race inside the
   region that moved to `users.go`; this PR does not attempt that fix.

- [ ] **Step 6: Update the tracking document**

Mark Phase 1 complete in `docs/superpowers/plans/2026-09-06-architecture-refactor.md`:

- Status table row 1: change `**spec approved**` to `**complete**`, and add the plan link.
- Append an "Achieved" subsection to the Phase 1 section recording the measured outcome: `main.go` 5,421 → 1,647; 3,655 lines into 17 files; purity identical at 223 decls / 69 imports; six comment corrections; one new test.

```bash
git add docs/superpowers/plans/2026-09-06-architecture-refactor.md
git commit -m "docs: record Phase 1 as complete"
```

---

## Task 21: File the pre-existing comment-rot issue

Task 12, 14, 16 and 17 fixed the six comments Phase 1 *invalidated*. The sweep that found them also found four that were already wrong before this work started. Those are a separate concern with a separate fix, and per the tracking document's ground rule 2 they do not belong in a refactor PR.

**Files:** none — this is a GitHub issue.

- [ ] **Step 1: Verify the three claims still hold**

```bash
cd /path/to/slk && git checkout main
grep -n 'refreshChannel' internal/ui/msgs.go internal/ui/reducer_focus_test.go
grep -rn 'func.*refreshChannel' cmd/slk/    # expect: no output
sed -n '2076p' cmd/slk/main.go
```

Expected: two comments referencing `rtmEventHandler.refreshChannel`; no such function anywhere in `cmd/slk`; line 2076 is a `context.WithTimeout` call, not the handler struct literal that cites it.

Note: this list was four sites when the plan was written. `internal/bootstrap/revalidate.go:416` moved out of it during execution — it names `connectWorkspace`, which Phase 1 moves, so Task 17 retires it rather than leaving it for this issue.

- [ ] **Step 2: File the issue**

Title: `Stale cross-file comment citations in and around cmd/slk`

Body must cover:

| Site | Claim | Reality at `b733cce` |
|---|---|---|
| `internal/ui/msgs.go:76` | `rtmEventHandler.refreshChannel` | no such symbol anywhere in the repo |
| `internal/ui/reducer_focus_test.go:885` | `rtmEventHandler.refreshChannel` | no such symbol anywhere in the repo |
| `cmd/slk/main.go:4447` | `main.go:2076` | a `context.WithTimeout` call |

Plus:

- **Why it is not fixed in the Phase 1 PR:** repairing the `refreshChannel`
  references requires deciding what they *should* describe, which is a judgement
  about current behaviour, not code motion. A refactor PR that also changes
  behaviour cannot be reviewed.
- **The general lesson, which is the point of the issue:** a line-number
  citation into another file rots silently and is worthless within weeks. Every
  one in this repository is wrong. Cite the symbol; the compiler and `grep` will
  keep that honest.
- A note that Phase 1 retired five further `main.go:NNNN` citations as a side
  effect of making them file-agnostic, so the three above are all that remain.

- [ ] **Step 3: Cross-reference**

Link the issue from the Phase 1 PR description, and link the PR from the issue.

---

## Appendix: verified measurements

Every number in this plan was produced by executing the full sequence against a scratch clone of `b733cce` before the plan was written. The dry run confirmed: build green, `go vet` clean, `gofmt` clean, `go test ./cmd/slk/` ok, `go test ./... -race` ok across 57 packages, and `PURITY: IDENTICAL` at 223 declarations and 69 distinct imports.

`main.go` line count after each move, in task order:

| Task | File | main.go |
|---|---|---|
| 2 | `paths.go` | 5,397 |
| 3 | `thread_subscriptions.go` | 5,359 |
| 4 | `usergroups.go` | 5,317 |
| 5 | `conversations.go` | 5,237 |
| 6 | `membership.go` | 5,145 |
| 7 | `attachments.go` | 5,048 |
| 8 | `workspace_search.go` | 4,934 |
| 9 | `sections.go` | 4,810 |
| 10 | `dump.go` | 4,675 |
| 11 | `presence.go` | 4,523 |
| 12 | `users.go` | 4,312 |
| 13 | `workspace.go` | 4,059 |
| 14 | `markread.go` | 3,737 |
| 15 | `user_resolver.go` | 3,304 |
| 16 | `rtm_handler.go` | 2,830 |
| 17 | `connect.go` | 2,272 |
| 18 | `history.go` | **1,647** |

Final file sizes: `history.go` 639, `connect.go` 576, `rtm_handler.go` 492, `user_resolver.go` 449, `markread.go` 334, `thread_subscriptions.go` 285 (was 247), `workspace.go` 266, `users.go` 222, `presence.go` 162, `dump.go` 149, `sections.go` 132, `workspace_search.go` 129, `attachments.go` 106, `membership.go` 100, `conversations.go` 88, `usergroups.go` 49, `paths.go` 30.

If any expected value in a task does not match, the tree is not at `b733cce` or a previous task deviated. Stop and reconcile rather than adjusting the number.

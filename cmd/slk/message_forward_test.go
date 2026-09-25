package main

import (
	"bytes"
	"go/ast"
	"go/format"
	"go/parser"
	"go/token"
	"testing"
)

// The composition root cannot be invoked without starting the TUI. Pin this
// small wiring closure structurally: it must resolve the captured team, reject
// unavailable clients, and delegate without send/compose/cache side effects.
// The core adapter and Slack client have runtime forwarding tests.
func TestMessageForwardWiring(t *testing.T) {
	fset := token.NewFileSet()
	file, err := parser.ParseFile(fset, "main.go", nil, parser.SkipObjectResolution)
	if err != nil {
		t.Fatal(err)
	}
	var forward ast.Expr
	bundles := 0
	ast.Inspect(file, func(node ast.Node) bool {
		bundle, ok := node.(*ast.CompositeLit)
		if !ok {
			return true
		}
		typ, ok := bundle.Type.(*ast.SelectorExpr)
		if !ok || typ.Sel.Name != "MessageServiceFuncs" {
			return true
		}
		bundles++
		for _, elt := range bundle.Elts {
			field, ok := elt.(*ast.KeyValueExpr)
			if !ok {
				continue
			}
			if key, ok := field.Key.(*ast.Ident); ok && key.Name == "Forward" {
				forward = field.Value
			}
		}
		return true
	})
	if bundles != 1 || forward == nil {
		t.Fatalf("want one message service bundle with Forward wired; bundles=%d, forward=%v", bundles, forward)
	}
	want, err := parser.ParseExpr(`func(ctx context.Context, teamID string, sourceChannelID ids.ChannelID, ts ids.MessageTS, destinationChannelID ids.ChannelID) (core.ForwardResult, error) {
		wctx := router.ByID(teamID)
		if wctx == nil || wctx.Client == nil {
			return core.ForwardResult{}, fmt.Errorf("forwarding message: workspace %q is unavailable", teamID)
		}
		postedTS, permalink, err := wctx.Client.ForwardMessage(ctx, string(sourceChannelID), string(ts), string(destinationChannelID))
		if err != nil {
			return core.ForwardResult{}, err
		}
		return core.ForwardResult{TS: ids.MessageTS(postedTS), Text: permalink}, nil
	}`)
	if err != nil {
		t.Fatal(err)
	}
	var gotText, wantText bytes.Buffer
	// Ignore source positions so comments and blank lines do not affect the check.
	if err := format.Node(&gotText, token.NewFileSet(), forward); err != nil {
		t.Fatal(err)
	}
	if err := format.Node(&wantText, token.NewFileSet(), want); err != nil {
		t.Fatal(err)
	}
	if gotText.String() != wantText.String() {
		t.Errorf("forwarding must use the captured workspace and delegate directly\ngot:\n%s\nwant:\n%s", &gotText, &wantText)
	}
}

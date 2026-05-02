# CommonMark Compose Conversion Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Translate CommonMark in the compose box (`**bold**`, `~~strike~~`, `[label](url)`, `- list items`, fenced code blocks) to Slack's wire formats at send time, so messages render the same way everywhere — slk, other Slack clients, push notifications.

**Architecture:** A new `internal/slack/mrkdwn` package exposes `Convert(text) (mrkdwn, *RichTextBlock)`. The three send methods on `internal/slack/client.go` (`SendMessage`, `SendReply`, `EditMessage`) gain an extra return value carrying the converted mrkdwn so callers can use it for optimistic display. Goldmark parses CommonMark; a custom AST walker emits both a mrkdwn fallback string and a slack-go `RichTextBlock`.

**Tech Stack:** Go 1.26, `github.com/yuin/goldmark` (CommonMark parser, new dep), existing `github.com/slack-go/slack` for block types.

**Spec:** [`docs/superpowers/specs/2026-05-01-commonmark-compose-design.md`](../specs/2026-05-01-commonmark-compose-design.md)

---

### Task 1: Add goldmark dependency

**Files:**
- Modify: `go.mod`
- Modify: `go.sum`

- [ ] **Step 1: Add the dependency**

```bash
go get github.com/yuin/goldmark@v1.7.13
go mod tidy
```

Expected: `go.mod` gains `github.com/yuin/goldmark v1.7.13` in the `require` block, `go.sum` gains corresponding entries.

- [ ] **Step 2: Verify it builds**

```bash
go build ./...
```

Expected: no errors.

- [ ] **Step 3: Smoke-test goldmark from a throwaway file**

Create `cmd/goldmark-smoke/main.go`:

```go
package main

import (
	"bytes"
	"fmt"

	"github.com/yuin/goldmark"
	"github.com/yuin/goldmark/extension"
)

func main() {
	md := goldmark.New(goldmark.WithExtensions(extension.Strikethrough))
	var buf bytes.Buffer
	if err := md.Convert([]byte("**hello** ~~strike~~"), &buf); err != nil {
		panic(err)
	}
	fmt.Println(buf.String())
}
```

Run: `go run ./cmd/goldmark-smoke`
Expected output: `<p><strong>hello</strong> <del>strike</del></p>`

Then delete the smoke directory: `rm -rf cmd/goldmark-smoke`

- [ ] **Step 4: Commit**

```bash
git add go.mod go.sum
git commit -m "deps: add github.com/yuin/goldmark for CommonMark parsing

Will be used by internal/slack/mrkdwn to translate CommonMark in the
compose box to Slack mrkdwn + rich_text blocks at send time."
```

---

### Task 2: Pre-tokenize Slack wire forms (`tokens.go`)

Goldmark would parse `<@U123>`, `<#C123|name>`, `<!here>`, and `<https://...|label>` as autolinks or HTML and damage them. We replace each with a private-use sentinel before parsing and restore them during the AST walk.

**Files:**
- Create: `internal/slack/mrkdwn/doc.go`
- Create: `internal/slack/mrkdwn/tokens.go`
- Create: `internal/slack/mrkdwn/tokens_test.go`

- [ ] **Step 1: Write failing tests**

Create `internal/slack/mrkdwn/tokens_test.go`:

```go
package mrkdwn

import "testing"

func TestTokenize_NoTokens(t *testing.T) {
	got, table := tokenize("hello world")
	if got != "hello world" {
		t.Errorf("text changed: %q", got)
	}
	if len(table) != 0 {
		t.Errorf("expected empty table, got %d entries", len(table))
	}
}

func TestTokenize_UserMention(t *testing.T) {
	got, table := tokenize("hi <@U12345>!")
	want := "hi \uE000" + "0" + "\uE001!"
	if got != want {
		t.Errorf("got %q want %q", got, want)
	}
	if len(table) != 1 {
		t.Fatalf("expected 1 entry, got %d", len(table))
	}
	if table[0].kind != tokUser || table[0].id != "U12345" {
		t.Errorf("table[0] = %+v", table[0])
	}
}

func TestTokenize_ChannelMentionWithName(t *testing.T) {
	got, table := tokenize("see <#C123|general>")
	want := "see \uE000" + "0" + "\uE001"
	if got != want {
		t.Errorf("got %q want %q", got, want)
	}
	if table[0].kind != tokChannel || table[0].id != "C123" || table[0].label != "general" {
		t.Errorf("table[0] = %+v", table[0])
	}
}

func TestTokenize_ChannelMentionBare(t *testing.T) {
	got, table := tokenize("<#C9>")
	want := "\uE000" + "0" + "\uE001"
	if got != want {
		t.Errorf("got %q want %q", got, want)
	}
	if table[0].kind != tokChannel || table[0].id != "C9" || table[0].label != "" {
		t.Errorf("table[0] = %+v", table[0])
	}
}

func TestTokenize_BroadcastHere(t *testing.T) {
	_, table := tokenize("<!here> deploy")
	if table[0].kind != tokBroadcast || table[0].id != "here" {
		t.Errorf("table[0] = %+v", table[0])
	}
}

func TestTokenize_BroadcastSubteam(t *testing.T) {
	_, table := tokenize("<!subteam^S01|@team>")
	if table[0].kind != tokBroadcast || table[0].id != "subteam^S01" || table[0].label != "@team" {
		t.Errorf("table[0] = %+v", table[0])
	}
}

func TestTokenize_LinkLabeled(t *testing.T) {
	_, table := tokenize("<https://slack.com|Slack>")
	if table[0].kind != tokLink || table[0].id != "https://slack.com" || table[0].label != "Slack" {
		t.Errorf("table[0] = %+v", table[0])
	}
}

func TestTokenize_LinkBare(t *testing.T) {
	_, table := tokenize("<https://slack.com>")
	if table[0].kind != tokLink || table[0].id != "https://slack.com" || table[0].label != "" {
		t.Errorf("table[0] = %+v", table[0])
	}
}

func TestTokenize_Multiple(t *testing.T) {
	got, table := tokenize("hi <@U1> and <@U2>")
	want := "hi \uE000" + "0" + "\uE001 and \uE000" + "1" + "\uE001"
	if got != want {
		t.Errorf("got %q want %q", got, want)
	}
	if len(table) != 2 {
		t.Fatalf("expected 2 entries, got %d", len(table))
	}
}

func TestSentinelRoundTrip(t *testing.T) {
	in := "**bold** <@U1> and `code` and <https://x.com|x>"
	tokenized, table := tokenize(in)
	got := detokenizeText(tokenized, table)
	if got != in {
		t.Errorf("round trip changed text:\n  in:  %q\n  out: %q", in, got)
	}
}

func TestParseSentinel(t *testing.T) {
	// Layout: "hello " (6 bytes) + \uE000 (3) + "7" (1) + \uE001 (3) + " world" (6).
	// Sentinel begins at byte 6 and ends at byte 13.
	s := "hello \uE0007\uE001 world"
	idx, end, ok := parseSentinel(s, 6)
	if !ok {
		t.Fatal("expected to parse sentinel at byte 6")
	}
	if idx != 7 {
		t.Errorf("idx = %d, want 7", idx)
	}
	if end != 13 {
		t.Errorf("end = %d, want 13", end)
	}
}

func TestParseSentinel_NotASentinel(t *testing.T) {
	_, _, ok := parseSentinel("hello", 0)
	if ok {
		t.Fatal("expected ok=false for plain text")
	}
}
```

(`reflect` import in the test file is not needed; remove it.)

- [ ] **Step 2: Run tests, confirm they fail**

```bash
go test ./internal/slack/mrkdwn/ -run Tokenize -v
```

Expected: `package internal/slack/mrkdwn does not exist` or compile error referencing missing types/functions.

- [ ] **Step 3: Implement `tokens.go`**

Create `internal/slack/mrkdwn/doc.go`:

```go
// Package mrkdwn translates CommonMark-style markdown in the compose
// box into Slack's wire formats: a mrkdwn fallback string (for the
// chat.postMessage `text` field and notifications) and a rich_text
// block (for the `blocks` array). The single entry point is Convert.
//
// The package preserves Slack wire-form tokens (<@U123>, <#C123|name>,
// <!here>, <https://...|label>) as opaque atoms so they don't get
// mangled by the CommonMark parser; they become typed elements (user,
// channel, broadcast, link) in the rich_text block.
package mrkdwn
```

Create `internal/slack/mrkdwn/tokens.go`:

```go
package mrkdwn

import (
	"regexp"
	"strconv"
	"strings"
	"unicode/utf8"
)

// Sentinel runes from the Unicode private-use area. Guaranteed not to
// collide with real text and treated as opaque characters by goldmark.
const (
	sentinelStart = '\uE000'
	sentinelEnd   = '\uE001'
)

type tokenKind int

const (
	tokUser tokenKind = iota
	tokChannel
	tokBroadcast
	tokLink
)

// token holds the original wire-form payload for one <...> match.
//
// For tokUser:      id = "U12345",        label = ""
// For tokChannel:   id = "C12345",        label = "general" (may be "")
// For tokBroadcast: id = "here" / "channel" / "subteam^S01",
//                   label = "" or "@team" (only for subteam form)
// For tokLink:      id = "https://x.com", label = "Slack" (may be "")
type token struct {
	kind  tokenKind
	id    string
	label string
}

// Patterns are tried in order; first match wins. None overlap given
// their leading characters (<@, <#, <!, <h).
var (
	reUser      = regexp.MustCompile(`<@([UW][A-Z0-9]+)>`)
	reChannel   = regexp.MustCompile(`<#([CG][A-Z0-9]+)(?:\|([^>]*))?>`)
	reBroadcast = regexp.MustCompile(`<!([a-z]+(?:\^[A-Za-z0-9]+)?)(?:\|([^>]*))?>`)
	reLink      = regexp.MustCompile(`<(https?://[^|>]+)(?:\|([^>]*))?>`)
)

// tokenize replaces all Slack wire-form tokens in s with sentinel
// markers and returns the rewritten string plus an ordered table.
// The marker for table index N is the three-rune sequence
// sentinelStart, decimal digits of N, sentinelEnd.
func tokenize(s string) (string, []token) {
	var table []token
	var b strings.Builder
	b.Grow(len(s))

	i := 0
	for i < len(s) {
		// Fast path: skip ahead until we see '<'.
		j := strings.IndexByte(s[i:], '<')
		if j < 0 {
			b.WriteString(s[i:])
			break
		}
		b.WriteString(s[i : i+j])
		i += j

		// Try each pattern at position i.
		matched := false
		for _, p := range []struct {
			re   *regexp.Regexp
			kind tokenKind
		}{
			{reUser, tokUser},
			{reChannel, tokChannel},
			{reBroadcast, tokBroadcast},
			{reLink, tokLink},
		} {
			loc := p.re.FindStringSubmatchIndex(s[i:])
			if loc == nil || loc[0] != 0 {
				continue
			}
			// loc is relative to s[i:].
			full := s[i : i+loc[1]]
			tok := token{kind: p.kind}
			tok.id = s[i+loc[2] : i+loc[3]]
			// reUser has one capture group (length-4 loc); the
			// other patterns have two (length-6 loc with optional
			// second group). Guard before reading the label.
			if len(loc) >= 6 && loc[4] >= 0 {
				tok.label = s[i+loc[4] : i+loc[5]]
			}
			table = append(table, tok)
			b.WriteRune(sentinelStart)
			b.WriteString(strconv.Itoa(len(table) - 1))
			b.WriteRune(sentinelEnd)
			i += len(full)
			matched = true
			break
		}
		if !matched {
			// Not a recognised Slack token; leave the '<' in place.
			b.WriteByte('<')
			i++
		}
	}

	return b.String(), table
}

// parseSentinel inspects s starting at byte offset start. If a
// sentinel-wrapped numeric index lives there, returns (index,
// end-byte-offset, true). The end offset points one byte past the
// closing sentinel rune.
func parseSentinel(s string, start int) (int, int, bool) {
	if start >= len(s) {
		return 0, 0, false
	}
	r, sz := utf8.DecodeRuneInString(s[start:])
	if r != sentinelStart {
		return 0, 0, false
	}
	digitStart := start + sz
	pos := digitStart
	for pos < len(s) {
		c := s[pos] // digits are ASCII, byte-level check is safe
		if c >= '0' && c <= '9' {
			pos++
			continue
		}
		break
	}
	if pos == digitStart {
		return 0, 0, false
	}
	if pos >= len(s) {
		return 0, 0, false
	}
	r, sz = utf8.DecodeRuneInString(s[pos:])
	if r != sentinelEnd {
		return 0, 0, false
	}
	idx, err := strconv.Atoi(s[digitStart:pos])
	if err != nil {
		return 0, 0, false
	}
	return idx, pos + sz, true
}

// detokenizeText restores all sentinel markers in s back to their
// original Slack wire-form tokens. Used to build the mrkdwn fallback
// (where mentions stay as <@U123>).
func detokenizeText(s string, table []token) string {
	if len(table) == 0 {
		return s
	}
	var b strings.Builder
	b.Grow(len(s))
	i := 0
	for i < len(s) {
		idx, end, ok := parseSentinel(s, i)
		if !ok {
			r, sz := utf8.DecodeRuneInString(s[i:])
			if sz == 0 {
				break
			}
			b.WriteRune(r)
			i += sz
			continue
		}
		if idx < 0 || idx >= len(table) {
			// Index out of range — emit raw bytes literally as a
			// safety fallback. Should never happen in practice.
			b.WriteString(s[i:end])
			i = end
			continue
		}
		b.WriteString(wireForm(table[idx]))
		i = end
	}
	return b.String()
}

// wireForm reconstructs the original <...> Slack wire token.
func wireForm(t token) string {
	switch t.kind {
	case tokUser:
		return "<@" + t.id + ">"
	case tokChannel:
		if t.label == "" {
			return "<#" + t.id + ">"
		}
		return "<#" + t.id + "|" + t.label + ">"
	case tokBroadcast:
		if t.label == "" {
			return "<!" + t.id + ">"
		}
		return "<!" + t.id + "|" + t.label + ">"
	case tokLink:
		if t.label == "" {
			return "<" + t.id + ">"
		}
		return "<" + t.id + "|" + t.label + ">"
	}
	return ""
}
```

- [ ] **Step 4: Run tests, confirm they pass**

```bash
go test ./internal/slack/mrkdwn/ -run Tokenize -v
go test ./internal/slack/mrkdwn/ -run Sentinel -v
```

Expected: all tests pass.

- [ ] **Step 5: Commit**

```bash
git add internal/slack/mrkdwn/doc.go internal/slack/mrkdwn/tokens.go internal/slack/mrkdwn/tokens_test.go
git commit -m "feat(mrkdwn): pre-tokenize Slack wire forms

Replace <@U…>, <#C…|name>, <!here>, <https://…|label> with private-use
Unicode sentinels before goldmark sees them, so the CommonMark parser
doesn't treat <…> as autolinks or HTML."
```

---

### Task 3: Convert scaffolding — empty input, plain text, paragraphs

**Files:**
- Create: `internal/slack/mrkdwn/convert.go`
- Create: `internal/slack/mrkdwn/walk.go`
- Create: `internal/slack/mrkdwn/convert_test.go`

- [ ] **Step 1: Write failing tests**

Create `internal/slack/mrkdwn/convert_test.go`:

```go
package mrkdwn

import (
	"encoding/json"
	"testing"

	"github.com/slack-go/slack"
)

// blockJSON serialises a *slack.RichTextBlock to JSON for readable
// test failure messages.
func blockJSON(b *slack.RichTextBlock) string {
	if b == nil {
		return "nil"
	}
	out, _ := json.Marshal(b)
	return string(out)
}

func TestConvert_EmptyInput(t *testing.T) {
	mr, blk := Convert("")
	if mr != "" {
		t.Errorf("mrkdwn = %q, want empty", mr)
	}
	if blk != nil {
		t.Errorf("block = %s, want nil", blockJSON(blk))
	}
}

func TestConvert_WhitespaceOnly(t *testing.T) {
	mr, blk := Convert("   \n  ")
	if mr != "" {
		t.Errorf("mrkdwn = %q, want empty for whitespace-only input", mr)
	}
	if blk != nil {
		t.Errorf("block = %s, want nil", blockJSON(blk))
	}
}

func TestConvert_PlainText(t *testing.T) {
	mr, blk := Convert("hello world")
	if mr != "hello world" {
		t.Errorf("mrkdwn = %q, want %q", mr, "hello world")
	}
	if blk == nil {
		t.Fatal("block is nil")
	}
	if len(blk.Elements) != 1 {
		t.Fatalf("got %d elements, want 1", len(blk.Elements))
	}
	sec, ok := blk.Elements[0].(*slack.RichTextSection)
	if !ok {
		t.Fatalf("element[0] is %T, want *RichTextSection", blk.Elements[0])
	}
	if len(sec.Elements) != 1 {
		t.Fatalf("section has %d elements, want 1", len(sec.Elements))
	}
	te, ok := sec.Elements[0].(*slack.RichTextSectionTextElement)
	if !ok {
		t.Fatalf("section element is %T, want *RichTextSectionTextElement", sec.Elements[0])
	}
	if te.Text != "hello world" {
		t.Errorf("text = %q, want %q", te.Text, "hello world")
	}
	if te.Style != nil {
		t.Errorf("style = %+v, want nil", te.Style)
	}
}

func TestConvert_TwoParagraphs(t *testing.T) {
	mr, blk := Convert("para one\n\npara two")
	want := "para one\n\npara two"
	if mr != want {
		t.Errorf("mrkdwn = %q, want %q", mr, want)
	}
	if blk == nil {
		t.Fatal("block is nil")
	}
	if len(blk.Elements) != 2 {
		t.Fatalf("got %d elements, want 2 (one section per paragraph)", len(blk.Elements))
	}
}
```

- [ ] **Step 2: Run tests, confirm failure**

```bash
go test ./internal/slack/mrkdwn/ -run Convert -v
```

Expected: compile error (`Convert` not defined).

- [ ] **Step 3: Implement minimal `Convert` and walker**

Create `internal/slack/mrkdwn/convert.go`:

```go
package mrkdwn

import (
	"strings"

	"github.com/slack-go/slack"
	"github.com/yuin/goldmark"
	"github.com/yuin/goldmark/extension"
	"github.com/yuin/goldmark/text"
)

// Convert parses CommonMark in input and returns the Slack-compatible
// payload pair: a mrkdwn fallback string and a rich_text block.
//
// input may contain Slack wire-form tokens (<@U…>, <#C…|name>,
// <!here>, <https://…|label>); these are preserved as opaque tokens
// in the mrkdwn fallback and become typed elements (user, channel,
// broadcast, link) in the block.
//
// For empty / whitespace-only input, returns ("", nil).
func Convert(input string) (string, *slack.RichTextBlock) {
	if strings.TrimSpace(input) == "" {
		return "", nil
	}

	tokenized, table := tokenize(input)

	md := goldmark.New(
		goldmark.WithExtensions(extension.Strikethrough),
	)
	doc := md.Parser().Parse(text.NewReader([]byte(tokenized)))

	w := newWalker([]byte(tokenized), table)
	w.walkDocument(doc)

	mr := strings.TrimRight(w.mrkdwn.String(), "\n")
	mr = detokenizeText(mr, table)

	if len(w.block.Elements) == 0 {
		return mr, nil
	}
	return mr, w.block
}
```

Create `internal/slack/mrkdwn/walk.go`:

```go
package mrkdwn

import (
	"strings"

	"github.com/slack-go/slack"
	"github.com/yuin/goldmark/ast"
)

// walker accumulates two parallel outputs as it walks a goldmark AST:
// the mrkdwn fallback string (in mrkdwn) and a rich_text block (in block).
//
// The current rich_text section being assembled lives in curSection;
// inline element appenders (text, mention, link) push into it. Block-
// level appenders (paragraph, list, code-block) flush curSection into
// block.Elements first, then create a new container.
type walker struct {
	source []byte
	table  []token

	mrkdwn     strings.Builder
	block      *slack.RichTextBlock
	curSection *slack.RichTextSection

	// inheritedStyle is applied to every text element appended via
	// appendText. Inline-formatting walk methods toggle the relevant
	// flag for the duration of their child walk.
	inheritedStyle slack.RichTextSectionTextStyle
}

func newWalker(source []byte, table []token) *walker {
	return &walker{
		source: source,
		table:  table,
		block:  slack.NewRichTextBlock(""),
	}
}

func (w *walker) walkDocument(n ast.Node) {
	for c := n.FirstChild(); c != nil; c = c.NextSibling() {
		w.walkBlock(c)
	}
	w.flushSection()
}

// walkBlock dispatches block-level nodes. Every block-level node
// flushes any in-progress inline section first.
func (w *walker) walkBlock(n ast.Node) {
	switch n := n.(type) {
	case *ast.Paragraph:
		w.flushSection()
		w.curSection = slack.NewRichTextSection()
		w.walkInlineChildren(n)
		w.flushSection()
		// Paragraph separator in mrkdwn is a blank line.
		w.mrkdwn.WriteString("\n\n")
	default:
		// Other block types (List, FencedCodeBlock, Heading, Blockquote)
		// will be handled in later tasks. For now, walk children as
		// inline so we don't lose plain-text fallback content.
		w.walkInlineChildren(n)
	}
}

// flushSection moves the current in-progress section into block.Elements.
func (w *walker) flushSection() {
	if w.curSection != nil && len(w.curSection.Elements) > 0 {
		w.block.Elements = append(w.block.Elements, w.curSection)
	}
	w.curSection = nil
}

// walkInlineChildren walks the children of n as inline content.
func (w *walker) walkInlineChildren(n ast.Node) {
	for c := n.FirstChild(); c != nil; c = c.NextSibling() {
		w.walkInline(c)
	}
}

func (w *walker) walkInline(n ast.Node) {
	switch n := n.(type) {
	case *ast.Text:
		seg := n.Segment
		w.appendText(string(w.source[seg.Start:seg.Stop]))
		if n.HardLineBreak() || n.SoftLineBreak() {
			w.appendText("\n")
		}
	default:
		// Other inline nodes (Emphasis, CodeSpan, Link, etc.) are
		// handled in later tasks. Walk children to preserve text.
		w.walkInlineChildren(n)
	}
}

// appendText writes s to both outputs with the current inherited
// style. Empty s is a no-op.
func (w *walker) appendText(s string) {
	if s == "" {
		return
	}
	w.mrkdwn.WriteString(s)
	if w.curSection == nil {
		w.curSection = slack.NewRichTextSection()
	}
	te := slack.NewRichTextSectionTextElement(s, w.copyStyle())
	w.curSection.Elements = append(w.curSection.Elements, te)
}

// copyStyle returns a pointer to a copy of inheritedStyle, or nil if
// no flags are set (so we don't emit "style":{} on the wire).
func (w *walker) copyStyle() *slack.RichTextSectionTextStyle {
	s := w.inheritedStyle
	if s == (slack.RichTextSectionTextStyle{}) {
		return nil
	}
	return &s
}
```

- [ ] **Step 4: Run tests, confirm pass**

```bash
go test ./internal/slack/mrkdwn/ -run Convert -v
```

Expected: all four `TestConvert_*` cases pass. The tokenize/sentinel tests from Task 2 still pass.

- [ ] **Step 5: Commit**

```bash
git add internal/slack/mrkdwn/convert.go internal/slack/mrkdwn/walk.go internal/slack/mrkdwn/convert_test.go
git commit -m "feat(mrkdwn): scaffold Convert with paragraph + plain text

Empty input returns (\"\", nil). Plain text becomes a single section
with one text element. Paragraph breaks (blank lines) split into
separate rich_text sections. Inline emphasis, code, and links are
deferred to subsequent tasks."
```

---

### Task 4: Bold (strong emphasis)

Goldmark parses both `**bold**` and `__bold__` as `*ast.Emphasis` with `Level == 2`. We translate both to single-asterisk mrkdwn `*bold*` and apply `Style.Bold = true` to the inner text elements.

**Files:**
- Modify: `internal/slack/mrkdwn/walk.go`
- Modify: `internal/slack/mrkdwn/convert_test.go`

- [ ] **Step 1: Add failing tests**

Append to `internal/slack/mrkdwn/convert_test.go`:

```go
func TestConvert_BoldDoubleAsterisk(t *testing.T) {
	mr, blk := Convert("**hello**")
	if mr != "*hello*" {
		t.Errorf("mrkdwn = %q, want %q", mr, "*hello*")
	}
	sec := blk.Elements[0].(*slack.RichTextSection)
	te := sec.Elements[0].(*slack.RichTextSectionTextElement)
	if te.Text != "hello" {
		t.Errorf("text = %q, want %q", te.Text, "hello")
	}
	if te.Style == nil || !te.Style.Bold {
		t.Errorf("expected Style.Bold = true, got %+v", te.Style)
	}
}

func TestConvert_BoldDoubleUnderscore(t *testing.T) {
	mr, blk := Convert("__hello__")
	if mr != "*hello*" {
		t.Errorf("mrkdwn = %q, want %q", mr, "*hello*")
	}
	sec := blk.Elements[0].(*slack.RichTextSection)
	te := sec.Elements[0].(*slack.RichTextSectionTextElement)
	if te.Style == nil || !te.Style.Bold {
		t.Errorf("expected Style.Bold = true, got %+v", te.Style)
	}
}

func TestConvert_BoldWithSurroundingText(t *testing.T) {
	mr, blk := Convert("hi **there** friend")
	if mr != "hi *there* friend" {
		t.Errorf("mrkdwn = %q, want %q", mr, "hi *there* friend")
	}
	sec := blk.Elements[0].(*slack.RichTextSection)
	if len(sec.Elements) != 3 {
		t.Fatalf("got %d elements, want 3 (plain, bold, plain)", len(sec.Elements))
	}
	mid := sec.Elements[1].(*slack.RichTextSectionTextElement)
	if mid.Text != "there" || mid.Style == nil || !mid.Style.Bold {
		t.Errorf("middle element = %+v / style %+v, want bold 'there'", mid, mid.Style)
	}
}
```

- [ ] **Step 2: Run tests, confirm failure**

```bash
go test ./internal/slack/mrkdwn/ -run TestConvert_Bold -v
```

Expected: failures because the walker doesn't yet handle Emphasis nodes.

- [ ] **Step 3: Implement Emphasis Level 2**

Edit `internal/slack/mrkdwn/walk.go` — extend `walkInline`:

```go
func (w *walker) walkInline(n ast.Node) {
	switch n := n.(type) {
	case *ast.Text:
		seg := n.Segment
		w.appendText(string(w.source[seg.Start:seg.Stop]))
		if n.HardLineBreak() || n.SoftLineBreak() {
			w.appendText("\n")
		}
	case *ast.Emphasis:
		if n.Level == 2 {
			w.walkBold(n)
			return
		}
		// Level 1 (italic) handled in Task 5; for now walk children
		// without styling so plain-text fallback is preserved.
		w.walkInlineChildren(n)
	default:
		w.walkInlineChildren(n)
	}
}

func (w *walker) walkBold(n ast.Node) {
	w.mrkdwn.WriteString("*")
	prev := w.inheritedStyle
	w.inheritedStyle.Bold = true
	w.walkInlineChildren(n)
	w.inheritedStyle = prev
	w.mrkdwn.WriteString("*")
}
```

- [ ] **Step 4: Run tests, confirm pass**

```bash
go test ./internal/slack/mrkdwn/ -v
```

Expected: all `TestConvert_Bold*` pass; existing tests still pass.

- [ ] **Step 5: Commit**

```bash
git add internal/slack/mrkdwn/walk.go internal/slack/mrkdwn/convert_test.go
git commit -m "feat(mrkdwn): translate **bold** and __bold__ to *bold*"
```

---

### Task 5: Italic with `*x*` preservation rule

Goldmark parses both `_italic_` and `*italic*` as `*ast.Emphasis` with `Level == 1`. We treat them differently:

- `_x_`: emit mrkdwn `_x_` and apply `Style.Italic = true`.
- `*x*`: emit literal `*x*` (asterisks pass through) and apply NO italic style. This preserves Slack mrkdwn-style bold for users who type `*bold*` directly.

We distinguish the two forms by inspecting the source byte at the node's start position (`n.Lines().At(0).Start`).

**Files:**
- Modify: `internal/slack/mrkdwn/walk.go`
- Modify: `internal/slack/mrkdwn/convert_test.go`

- [ ] **Step 1: Add failing tests**

Append to `convert_test.go`:

```go
func TestConvert_ItalicUnderscore(t *testing.T) {
	mr, blk := Convert("_hello_")
	if mr != "_hello_" {
		t.Errorf("mrkdwn = %q, want %q", mr, "_hello_")
	}
	sec := blk.Elements[0].(*slack.RichTextSection)
	te := sec.Elements[0].(*slack.RichTextSectionTextElement)
	if te.Text != "hello" {
		t.Errorf("text = %q, want %q", te.Text, "hello")
	}
	if te.Style == nil || !te.Style.Italic {
		t.Errorf("expected Style.Italic = true, got %+v", te.Style)
	}
}

func TestConvert_AsteriskPreservedAsLiteral(t *testing.T) {
	// *x* in CommonMark is italic, but in Slack mrkdwn it's bold.
	// We preserve the asterisks as literal characters so users who
	// type Slack-style *bold* don't get it converted to _italic_.
	mr, blk := Convert("*hello*")
	if mr != "*hello*" {
		t.Errorf("mrkdwn = %q, want %q", mr, "*hello*")
	}
	// Block side: text elements concatenate to "*hello*", no italic.
	sec := blk.Elements[0].(*slack.RichTextSection)
	var got string
	for _, el := range sec.Elements {
		te, ok := el.(*slack.RichTextSectionTextElement)
		if !ok {
			t.Fatalf("element is %T, want only text elements", el)
		}
		if te.Style != nil && (te.Style.Italic || te.Style.Bold) {
			t.Errorf("element %+v has style %+v, want no italic/bold for literal asterisks", te, te.Style)
		}
		got += te.Text
	}
	if got != "*hello*" {
		t.Errorf("concatenated text = %q, want %q", got, "*hello*")
	}
}

func TestConvert_AsteriskRoundTripStable(t *testing.T) {
	// After one conversion, **bold** -> *bold*. Re-converting the
	// result must NOT change it (this is what happens on edit).
	mr1, _ := Convert("**bold**")
	if mr1 != "*bold*" {
		t.Fatalf("first pass: %q, want *bold*", mr1)
	}
	mr2, _ := Convert(mr1)
	if mr2 != "*bold*" {
		t.Errorf("second pass: %q, want *bold* (round-trip stable)", mr2)
	}
}
```

- [ ] **Step 2: Run tests, confirm failure**

```bash
go test ./internal/slack/mrkdwn/ -run "TestConvert_Italic|TestConvert_Asterisk" -v
```

Expected: failures — italic isn't styled, and `*x*` currently emits `_x_` (italic) because `Level == 1` is the underscore branch by default.

Actually with the current implementation (Task 4), Level 1 emphasis just walks children without styling, so `*hello*` produces text "hello" with no style — but the asterisks are LOST. That's the bug we're fixing.

- [ ] **Step 3: Implement source-byte introspection**

Edit `walk.go` — replace the `case *ast.Emphasis:` arm in `walkInline`:

```go
	case *ast.Emphasis:
		if n.Level == 2 {
			w.walkBold(n)
			return
		}
		// Level 1: distinguish _italic_ from *italic*. Goldmark's
		// emphasis node carries source-line info but not the exact
		// delimiter byte, so we look at the byte just before the
		// first child's segment start. The delimiter is one byte
		// before the inner content (after lazy-line continuation
		// is resolved by goldmark).
		if w.emphasisDelimiter(n) == '_' {
			w.walkItalic(n)
		} else {
			w.walkAsteriskLiteral(n)
		}
```

Add the helpers below `walkBold`:

```go
// emphasisDelimiter returns the rune used to start a Level-1 emphasis
// node ('_' or '*'). It inspects the byte immediately before the
// first inline child's source segment.
func (w *walker) emphasisDelimiter(n ast.Node) byte {
	first := n.FirstChild()
	if first == nil {
		return '_'
	}
	tn, ok := first.(*ast.Text)
	if !ok {
		// Nested inline (e.g. <em><code>x</code></em>). Walk down
		// to the first text node.
		for c := first.FirstChild(); c != nil; c = c.FirstChild() {
			if t, ok := c.(*ast.Text); ok {
				tn = t
				break
			}
		}
		if tn == nil {
			return '_'
		}
	}
	pos := tn.Segment.Start - 1
	if pos < 0 || pos >= len(w.source) {
		return '_'
	}
	return w.source[pos]
}

// walkItalic emits _x_ and sets the italic style flag for children.
func (w *walker) walkItalic(n ast.Node) {
	w.mrkdwn.WriteString("_")
	prev := w.inheritedStyle
	w.inheritedStyle.Italic = true
	w.walkInlineChildren(n)
	w.inheritedStyle = prev
	w.mrkdwn.WriteString("_")
}

// walkAsteriskLiteral emits *...* with NO italic style on children.
// The asterisks become literal text in the rich_text block (merged
// with surrounding text, no style).
func (w *walker) walkAsteriskLiteral(n ast.Node) {
	w.appendText("*")
	w.walkInlineChildren(n)
	w.appendText("*")
}
```

Note: `walkAsteriskLiteral` writes the asterisks via `appendText` so they go into BOTH outputs (mrkdwn buffer AND a text element in the rich_text section). The `mrkdwn` write happens inside `appendText` already; we don't need a separate `mrkdwn.WriteString("*")` call.

- [ ] **Step 4: Run tests, confirm pass**

```bash
go test ./internal/slack/mrkdwn/ -v
```

Expected: italic and asterisk-preservation tests pass; bold tests still pass.

- [ ] **Step 5: Commit**

```bash
git add internal/slack/mrkdwn/walk.go internal/slack/mrkdwn/convert_test.go
git commit -m "feat(mrkdwn): translate _italic_; preserve *x* as literal

CommonMark single-asterisk emphasis (*x*) collides with Slack mrkdwn
single-asterisk bold. We preserve the asterisks as literal text so
users who type *bold* mrkdwn-style don't have it silently converted
to _italic_. Underscore italic (_x_) is translated normally."
```

---

### Task 6: Strikethrough, inline code, links

**Files:**
- Modify: `internal/slack/mrkdwn/walk.go`
- Modify: `internal/slack/mrkdwn/convert_test.go`

- [ ] **Step 1: Add failing tests**

```go
func TestConvert_Strikethrough(t *testing.T) {
	mr, blk := Convert("~~done~~")
	if mr != "~done~" {
		t.Errorf("mrkdwn = %q, want ~done~", mr)
	}
	te := blk.Elements[0].(*slack.RichTextSection).Elements[0].(*slack.RichTextSectionTextElement)
	if te.Text != "done" || te.Style == nil || !te.Style.Strike {
		t.Errorf("got %+v / %+v, want strike=true on 'done'", te, te.Style)
	}
}

func TestConvert_InlineCode(t *testing.T) {
	mr, blk := Convert("type `make build` to compile")
	want := "type `make build` to compile"
	if mr != want {
		t.Errorf("mrkdwn = %q, want %q", mr, want)
	}
	sec := blk.Elements[0].(*slack.RichTextSection)
	if len(sec.Elements) != 3 {
		t.Fatalf("got %d elements, want 3 (text, code, text)", len(sec.Elements))
	}
	mid := sec.Elements[1].(*slack.RichTextSectionTextElement)
	if mid.Text != "make build" || mid.Style == nil || !mid.Style.Code {
		t.Errorf("middle = %+v / %+v, want code 'make build'", mid, mid.Style)
	}
}

func TestConvert_LinkLabeled(t *testing.T) {
	mr, blk := Convert("see [Slack](https://slack.com) docs")
	want := "see <https://slack.com|Slack> docs"
	if mr != want {
		t.Errorf("mrkdwn = %q, want %q", mr, want)
	}
	sec := blk.Elements[0].(*slack.RichTextSection)
	if len(sec.Elements) != 3 {
		t.Fatalf("got %d elements, want 3 (text, link, text)", len(sec.Elements))
	}
	link, ok := sec.Elements[1].(*slack.RichTextSectionLinkElement)
	if !ok {
		t.Fatalf("middle element is %T, want *RichTextSectionLinkElement", sec.Elements[1])
	}
	if link.URL != "https://slack.com" || link.Text != "Slack" {
		t.Errorf("link = %+v", link)
	}
}

func TestConvert_LinkLabelEqualsURL(t *testing.T) {
	// Goldmark autolinks are off, so [https://x](https://x) is the
	// only natural way to get a bare-URL link from CommonMark input.
	mr, blk := Convert("see [https://x.com](https://x.com)")
	want := "see <https://x.com|https://x.com>"
	if mr != want {
		t.Errorf("mrkdwn = %q, want %q", mr, want)
	}
	link := blk.Elements[0].(*slack.RichTextSection).Elements[1].(*slack.RichTextSectionLinkElement)
	if link.URL != "https://x.com" {
		t.Errorf("URL = %q", link.URL)
	}
}
```

- [ ] **Step 2: Run tests, confirm failure**

```bash
go test ./internal/slack/mrkdwn/ -run "Strikethrough|InlineCode|Link" -v
```

Expected: failures.

- [ ] **Step 3: Extend walker**

Edit `walk.go` — add cases to `walkInline`:

```go
	case *ast.CodeSpan:
		w.mrkdwn.WriteString("`")
		prev := w.inheritedStyle
		w.inheritedStyle.Code = true
		// CodeSpan children are *ast.Text nodes containing the body.
		w.walkInlineChildren(n)
		w.inheritedStyle = prev
		w.mrkdwn.WriteString("`")
	case *ast.Link:
		w.handleLink(n)
```

Strikethrough lives in goldmark's extension package as `extensionAST.Strikethrough`. Add the import:

```go
import (
	"strings"

	"github.com/slack-go/slack"
	"github.com/yuin/goldmark/ast"
	extensionAST "github.com/yuin/goldmark/extension/ast"
)
```

And another case in `walkInline`:

```go
	case *extensionAST.Strikethrough:
		w.mrkdwn.WriteString("~")
		prev := w.inheritedStyle
		w.inheritedStyle.Strike = true
		w.walkInlineChildren(n)
		w.inheritedStyle = prev
		w.mrkdwn.WriteString("~")
```

Add `handleLink`:

```go
// handleLink emits a CommonMark [label](url) as Slack mrkdwn
// <url|label> and a RichTextSectionLinkElement in the block.
func (w *walker) handleLink(n *ast.Link) {
	url := string(n.Destination)

	// Build the label by walking children into a temporary string
	// builder so we can use it both for the mrkdwn '<url|label>'
	// form and the link element's Text field.
	label := w.collectInlineText(n)

	w.mrkdwn.WriteString("<")
	w.mrkdwn.WriteString(url)
	w.mrkdwn.WriteString("|")
	w.mrkdwn.WriteString(label)
	w.mrkdwn.WriteString(">")

	if w.curSection == nil {
		w.curSection = slack.NewRichTextSection()
	}
	link := slack.NewRichTextSectionLinkElement(url, label, w.copyStyle())
	w.curSection.Elements = append(w.curSection.Elements, link)
}

// collectInlineText concatenates the text content of n's children,
// stripping inline formatting markers. Used for link labels where
// Slack's wire form expects plain text after the '|'.
func (w *walker) collectInlineText(n ast.Node) string {
	var b strings.Builder
	var walk func(ast.Node)
	walk = func(c ast.Node) {
		if t, ok := c.(*ast.Text); ok {
			b.Write(w.source[t.Segment.Start:t.Segment.Stop])
			return
		}
		for cc := c.FirstChild(); cc != nil; cc = cc.NextSibling() {
			walk(cc)
		}
	}
	for c := n.FirstChild(); c != nil; c = c.NextSibling() {
		walk(c)
	}
	// Restore Slack wire-form tokens that may live inside the label.
	return detokenizeText(b.String(), w.table)
}
```

- [ ] **Step 4: Run tests, confirm pass**

```bash
go test ./internal/slack/mrkdwn/ -v
```

Expected: strikethrough, inline code, and link tests pass.

- [ ] **Step 5: Commit**

```bash
git add internal/slack/mrkdwn/walk.go internal/slack/mrkdwn/convert_test.go
git commit -m "feat(mrkdwn): translate ~~strike~~, inline code, links

~~strike~~ -> ~strike~ with Style.Strike. \`code\` stays as \`code\`
with Style.Code. [label](url) -> <url|label> mrkdwn and a
RichTextSectionLinkElement in the block."
```

---

### Task 7: Detokenize Slack wire forms during walk

The walker currently passes through sentinel-wrapped placeholders as text. We need to:

1. In the mrkdwn output, replace placeholders with their original `<...>` wire form. (`detokenizeText` already does this for the final output, applied at the end of `Convert`.)
2. In the rich_text block, emit a typed element (user / channel / broadcast / link) wherever a placeholder appears in a text run, splitting the surrounding text into separate elements.

**Files:**
- Modify: `internal/slack/mrkdwn/walk.go`
- Modify: `internal/slack/mrkdwn/convert_test.go`

- [ ] **Step 1: Add failing tests**

```go
func TestConvert_UserMention(t *testing.T) {
	mr, blk := Convert("hi <@U12345>!")
	want := "hi <@U12345>!"
	if mr != want {
		t.Errorf("mrkdwn = %q, want %q", mr, want)
	}
	sec := blk.Elements[0].(*slack.RichTextSection)
	if len(sec.Elements) != 3 {
		t.Fatalf("got %d elements, want 3 (text, user, text)", len(sec.Elements))
	}
	user, ok := sec.Elements[1].(*slack.RichTextSectionUserElement)
	if !ok {
		t.Fatalf("middle is %T, want *RichTextSectionUserElement", sec.Elements[1])
	}
	if user.UserID != "U12345" {
		t.Errorf("UserID = %q, want U12345", user.UserID)
	}
}

func TestConvert_ChannelMention(t *testing.T) {
	mr, blk := Convert("see <#C123|general> please")
	if mr != "see <#C123|general> please" {
		t.Errorf("mrkdwn = %q", mr)
	}
	ch := blk.Elements[0].(*slack.RichTextSection).Elements[1].(*slack.RichTextSectionChannelElement)
	if ch.ChannelID != "C123" {
		t.Errorf("ChannelID = %q, want C123", ch.ChannelID)
	}
}

func TestConvert_Broadcast(t *testing.T) {
	mr, blk := Convert("<!here> deploy now")
	if mr != "<!here> deploy now" {
		t.Errorf("mrkdwn = %q", mr)
	}
	bc := blk.Elements[0].(*slack.RichTextSection).Elements[0].(*slack.RichTextSectionBroadcastElement)
	if bc.Range != "here" {
		t.Errorf("Range = %q, want here", bc.Range)
	}
}

func TestConvert_BoldContainingMention(t *testing.T) {
	mr, blk := Convert("**Hi <@U1>**")
	if mr != "*Hi <@U1>*" {
		t.Errorf("mrkdwn = %q, want *Hi <@U1>*", mr)
	}
	sec := blk.Elements[0].(*slack.RichTextSection)
	if len(sec.Elements) != 2 {
		t.Fatalf("got %d elements, want 2 (text, user)", len(sec.Elements))
	}
	te := sec.Elements[0].(*slack.RichTextSectionTextElement)
	if te.Text != "Hi " || te.Style == nil || !te.Style.Bold {
		t.Errorf("text = %+v / %+v, want bold 'Hi '", te, te.Style)
	}
	user := sec.Elements[1].(*slack.RichTextSectionUserElement)
	if user.UserID != "U1" {
		t.Errorf("UserID = %q", user.UserID)
	}
	if user.Style == nil || !user.Style.Bold {
		t.Errorf("user.Style = %+v, want bold inherited", user.Style)
	}
}
```

- [ ] **Step 2: Run tests, confirm failure**

Expected: the three placeholder bytes leak into the rich_text text as literal sentinels.

- [ ] **Step 3: Replace `appendText` with sentinel-aware variant**

Edit `walk.go` — rewrite `appendText` to scan for sentinels and split runs:

```go
// appendText writes s to both outputs with the current inherited
// style. Slack wire-form sentinels embedded in s are split out into
// typed rich_text elements (user / channel / broadcast / link) and
// restored to their original <...> form in the mrkdwn output.
func (w *walker) appendText(s string) {
	if s == "" {
		return
	}
	// Mrkdwn side: write as-is. The detokenize pass at the end of
	// Convert restores all sentinels in one go.
	w.mrkdwn.WriteString(s)

	// Block side: split on sentinels.
	if w.curSection == nil {
		w.curSection = slack.NewRichTextSection()
	}
	i := 0
	for i < len(s) {
		idx, end, ok := parseSentinel(s, i)
		if !ok {
			// Find the next sentinel boundary (or end of string)
			// and emit the run between [i, j) as a text element.
			j := nextSentinelStart(s, i)
			if j > i {
				te := slack.NewRichTextSectionTextElement(s[i:j], w.copyStyle())
				w.curSection.Elements = append(w.curSection.Elements, te)
			}
			i = j
			continue
		}
		if idx >= 0 && idx < len(w.table) {
			w.curSection.Elements = append(w.curSection.Elements, w.tokenElement(w.table[idx]))
		} else {
			// Out-of-range index, emit raw bytes.
			te := slack.NewRichTextSectionTextElement(s[i:end], w.copyStyle())
			w.curSection.Elements = append(w.curSection.Elements, te)
		}
		i = end
	}
}

// nextSentinelStart returns the byte index of the next sentinelStart
// rune in s at or after i, or len(s) if none.
func nextSentinelStart(s string, i int) int {
	idx := strings.IndexRune(s[i:], sentinelStart)
	if idx < 0 {
		return len(s)
	}
	return i + idx
}

// tokenElement converts a wire-form token into the corresponding
// rich_text element with the current inherited style applied where
// the schema supports a style.
func (w *walker) tokenElement(t token) slack.RichTextSectionElement {
	style := w.copyStyle()
	switch t.kind {
	case tokUser:
		return slack.NewRichTextSectionUserElement(t.id, style)
	case tokChannel:
		return slack.NewRichTextSectionChannelElement(t.id, style)
	case tokBroadcast:
		// Broadcasts don't carry a style on the wire (slack-go's
		// RichTextSectionBroadcastElement has no Style field).
		return slack.NewRichTextSectionBroadcastElement(t.id)
	case tokLink:
		text := t.label
		if text == "" {
			text = t.id
		}
		return slack.NewRichTextSectionLinkElement(t.id, text, style)
	}
	return nil
}
```

- [ ] **Step 4: Run tests, confirm pass**

```bash
go test ./internal/slack/mrkdwn/ -v
```

Expected: all four mention/broadcast tests plus existing ones pass.

- [ ] **Step 5: Commit**

```bash
git add internal/slack/mrkdwn/walk.go internal/slack/mrkdwn/convert_test.go
git commit -m "feat(mrkdwn): split text runs on Slack wire-form sentinels

Mention/channel/broadcast/link placeholders embedded in text become
typed rich_text section elements (user / channel / broadcast / link)
with the current inherited style. Mrkdwn fallback restores the
original <...> wire form via detokenizeText."
```

---

### Task 8: Lists (bullet, ordered, nested)

Goldmark parses `- a\n- b` as `*ast.List` with `Marker == '-'` containing `*ast.ListItem` children. Each list item contains a `*ast.TextBlock` of inline content. Nested lists become `*ast.List` children of a `*ast.ListItem`.

**Files:**
- Modify: `internal/slack/mrkdwn/walk.go`
- Modify: `internal/slack/mrkdwn/convert_test.go`

- [ ] **Step 1: Add failing tests**

```go
func TestConvert_UnorderedList(t *testing.T) {
	mr, blk := Convert("- one\n- two")
	want := "• one\n• two"
	if mr != want {
		t.Errorf("mrkdwn = %q, want %q", mr, want)
	}
	if len(blk.Elements) != 1 {
		t.Fatalf("got %d block elements, want 1 list", len(blk.Elements))
	}
	list, ok := blk.Elements[0].(*slack.RichTextList)
	if !ok {
		t.Fatalf("element[0] is %T, want *RichTextList", blk.Elements[0])
	}
	if list.Style != slack.RTEListBullet {
		t.Errorf("list style = %q, want bullet", list.Style)
	}
	if list.Indent != 0 {
		t.Errorf("Indent = %d, want 0", list.Indent)
	}
	if len(list.Elements) != 2 {
		t.Fatalf("got %d items, want 2", len(list.Elements))
	}
	first := list.Elements[0].(*slack.RichTextSection)
	te := first.Elements[0].(*slack.RichTextSectionTextElement)
	if te.Text != "one" {
		t.Errorf("first item text = %q", te.Text)
	}
}

func TestConvert_OrderedList(t *testing.T) {
	mr, blk := Convert("1. one\n2. two")
	want := "1. one\n2. two"
	if mr != want {
		t.Errorf("mrkdwn = %q, want %q", mr, want)
	}
	list := blk.Elements[0].(*slack.RichTextList)
	if list.Style != slack.RTEListOrdered {
		t.Errorf("list style = %q, want ordered", list.Style)
	}
}

func TestConvert_NestedList(t *testing.T) {
	mr, blk := Convert("- a\n    - b")
	wantMr := "• a\n  • b"
	if mr != wantMr {
		t.Errorf("mrkdwn = %q, want %q", mr, wantMr)
	}
	// Block side: two RichTextList elements at the top level
	// (slack's wire shape — nested lists are flat with Indent=N).
	if len(blk.Elements) != 2 {
		t.Fatalf("got %d block elements, want 2 lists (flattened nested)", len(blk.Elements))
	}
	outer := blk.Elements[0].(*slack.RichTextList)
	if outer.Indent != 0 {
		t.Errorf("outer.Indent = %d, want 0", outer.Indent)
	}
	inner := blk.Elements[1].(*slack.RichTextList)
	if inner.Indent != 1 {
		t.Errorf("inner.Indent = %d, want 1", inner.Indent)
	}
}
```

- [ ] **Step 2: Run tests, confirm failure**

```bash
go test ./internal/slack/mrkdwn/ -run List -v
```

Expected: failures.

- [ ] **Step 3: Implement list handling**

Edit `walk.go`. Add to the `walkBlock` switch:

```go
	case *ast.List:
		w.walkList(n, 0)
```

Add `walkList`:

```go
// walkList emits a rich_text_list block (flat at Slack's level —
// nested CommonMark lists are emitted as separate top-level lists
// with increasing Indent). Mrkdwn fallback uses U+2022 bullets for
// unordered and "N. " for ordered, with two-space indent per level.
func (w *walker) walkList(n *ast.List, indent int) {
	w.flushSection()

	style := slack.RTEListBullet
	if n.IsOrdered() {
		style = slack.RTEListOrdered
	}

	list := slack.NewRichTextList(style, indent)

	// We may emit additional sibling lists for nested children; collect
	// them in a slice so we can append in the right order after the
	// parent list is finalised.
	var nested []*slack.RichTextList

	itemIdx := 0
	for c := n.FirstChild(); c != nil; c = c.NextSibling() {
		item, ok := c.(*ast.ListItem)
		if !ok {
			continue
		}
		// Inline body of the item lives under a TextBlock or
		// Paragraph child (goldmark's ListItemMarker handling).
		sec := slack.NewRichTextSection()
		prev := w.curSection
		w.curSection = sec

		// Emit mrkdwn marker for the item.
		w.mrkdwn.WriteString(strings.Repeat("  ", indent))
		if n.IsOrdered() {
			w.mrkdwn.WriteString(itoa(itemIdx + 1))
			w.mrkdwn.WriteString(". ")
		} else {
			w.mrkdwn.WriteString("• ")
		}
		itemIdx++

		// Walk item children: inline body then any nested lists.
		for sub := item.FirstChild(); sub != nil; sub = sub.NextSibling() {
			switch sub := sub.(type) {
			case *ast.TextBlock, *ast.Paragraph:
				w.walkInlineChildren(sub)
			case *ast.List:
				// Defer nested list — render after this item's
				// inline body, but as a SIBLING list (Slack flat shape).
				w.mrkdwn.WriteString("\n")
				w.walkListInto(sub, indent+1, &nested)
				// Note: walkListInto wrote its own mrkdwn lines.
			default:
				w.walkInline(sub)
			}
		}
		w.mrkdwn.WriteString("\n")

		w.curSection = prev
		list.Elements = append(list.Elements, sec)
	}
	// Trim the trailing newline added by the last item so that the
	// list doesn't add an extra blank line before the next block.
	trimTrailingNewline(&w.mrkdwn)

	w.block.Elements = append(w.block.Elements, list)
	for _, nl := range nested {
		w.block.Elements = append(w.block.Elements, nl)
	}
}

// walkListInto walks a nested list, appending its block-level result
// to the given slice (instead of w.block.Elements directly), so that
// the parent walkList can interleave nested lists in the right order.
func (w *walker) walkListInto(n *ast.List, indent int, out *[]*slack.RichTextList) {
	// Save and restore w.block state by swapping a temporary block.
	prev := w.block
	tmp := slack.NewRichTextBlock("")
	w.block = tmp
	w.walkList(n, indent)
	w.block = prev
	for _, e := range tmp.Elements {
		if l, ok := e.(*slack.RichTextList); ok {
			*out = append(*out, l)
		}
	}
}

// itoa wraps strconv.Itoa to keep imports tidy.
func itoa(n int) string { return strconv.Itoa(n) }

// trimTrailingNewline removes one trailing '\n' from b if present.
func trimTrailingNewline(b *strings.Builder) {
	s := b.String()
	if strings.HasSuffix(s, "\n") {
		b.Reset()
		b.WriteString(s[:len(s)-1])
	}
}
```

Add `strconv` to imports.

- [ ] **Step 4: Run tests, confirm pass**

```bash
go test ./internal/slack/mrkdwn/ -v
```

Expected: list tests pass.

- [ ] **Step 5: Commit**

```bash
git add internal/slack/mrkdwn/walk.go internal/slack/mrkdwn/convert_test.go
git commit -m "feat(mrkdwn): translate lists to RichTextList blocks

Bulleted lists (- / * / +) become RichTextList{Style:bullet}, ordered
(1. / 2.) become RichTextList{Style:ordered}. Nested CommonMark lists
emit sibling RichTextList elements with Indent=N (Slack's wire shape
for nested lists is flat with explicit indent). Mrkdwn fallback uses
U+2022 bullets and 'N. ' for ordered, two-space indent per level."
```

---

### Task 9: Code blocks (fenced and indented)

**Files:**
- Modify: `internal/slack/mrkdwn/walk.go`
- Modify: `internal/slack/mrkdwn/convert_test.go`

- [ ] **Step 1: Add failing tests**

```go
func TestConvert_FencedCodeBlock(t *testing.T) {
	in := "```\nfoo\nbar\n```"
	mr, blk := Convert(in)
	wantMr := "```\nfoo\nbar\n```"
	if mr != wantMr {
		t.Errorf("mrkdwn = %q, want %q", mr, wantMr)
	}
	if len(blk.Elements) != 1 {
		t.Fatalf("got %d elements, want 1", len(blk.Elements))
	}
	pre, ok := blk.Elements[0].(*slack.RichTextPreformatted)
	if !ok {
		t.Fatalf("element[0] is %T, want *RichTextPreformatted", blk.Elements[0])
	}
	if len(pre.Elements) != 1 {
		t.Fatalf("got %d preformatted children, want 1 text", len(pre.Elements))
	}
	te := pre.Elements[0].(*slack.RichTextSectionTextElement)
	if te.Text != "foo\nbar\n" {
		t.Errorf("text = %q, want \"foo\\nbar\\n\"", te.Text)
	}
}

func TestConvert_IndentedCodeBlock(t *testing.T) {
	// Four-space indent triggers CommonMark indented code blocks.
	in := "    foo\n    bar"
	mr, blk := Convert(in)
	wantMr := "```\nfoo\nbar\n```"
	if mr != wantMr {
		t.Errorf("mrkdwn = %q, want %q", mr, wantMr)
	}
	pre := blk.Elements[0].(*slack.RichTextPreformatted)
	te := pre.Elements[0].(*slack.RichTextSectionTextElement)
	if te.Text != "foo\nbar\n" {
		t.Errorf("text = %q", te.Text)
	}
}
```

- [ ] **Step 2: Run tests, confirm failure**

```bash
go test ./internal/slack/mrkdwn/ -run CodeBlock -v
```

- [ ] **Step 3: Implement**

Edit `walk.go` — add cases to `walkBlock` switch:

```go
	case *ast.FencedCodeBlock:
		w.walkCodeBlock(n)
	case *ast.CodeBlock:
		w.walkCodeBlock(n)
```

Add `walkCodeBlock`:

```go
// walkCodeBlock collects all line content of n (works for both
// FencedCodeBlock and CodeBlock) and emits ```body``` mrkdwn plus a
// RichTextPreformatted block.
func (w *walker) walkCodeBlock(n ast.Node) {
	w.flushSection()
	var b strings.Builder
	for i := 0; i < n.Lines().Len(); i++ {
		seg := n.Lines().At(i)
		b.Write(w.source[seg.Start:seg.Stop])
	}
	body := b.String()

	w.mrkdwn.WriteString("```\n")
	w.mrkdwn.WriteString(body)
	if !strings.HasSuffix(body, "\n") {
		w.mrkdwn.WriteString("\n")
	}
	w.mrkdwn.WriteString("```\n")

	pre := &slack.RichTextPreformatted{
		Type: slack.RTEPreformatted,
		Elements: []slack.RichTextSectionElement{
			slack.NewRichTextSectionTextElement(body, nil),
		},
	}
	w.block.Elements = append(w.block.Elements, pre)
}
```

Note: `slack.RTEPreformatted` is the type constant. Verify with `go doc github.com/slack-go/slack RichTextElementType` if it isn't auto-completed.

- [ ] **Step 4: Run tests, confirm pass**

```bash
go test ./internal/slack/mrkdwn/ -v
```

- [ ] **Step 5: Commit**

```bash
git add internal/slack/mrkdwn/walk.go internal/slack/mrkdwn/convert_test.go
git commit -m "feat(mrkdwn): translate code blocks to RichTextPreformatted

Both fenced (\`\`\`...\`\`\`) and 4-space indented code blocks become
RichTextPreformatted in the block, with a fenced \`\`\` body in the
mrkdwn fallback for clients that don't render blocks."
```

---

### Task 10: Headings, blockquotes, escapes (raw passthrough)

Per the spec, we don't translate headings or blockquotes — they pass through as their original raw mrkdwn (`# Title` stays text, `> quote` stays text). Backslash escapes (`\*`) emit literal characters with no styling.

**Files:**
- Modify: `internal/slack/mrkdwn/walk.go`
- Modify: `internal/slack/mrkdwn/convert_test.go`

- [ ] **Step 1: Add failing tests**

```go
func TestConvert_HeadingPlainText(t *testing.T) {
	mr, blk := Convert("# My Title")
	want := "# My Title"
	if mr != want {
		t.Errorf("mrkdwn = %q, want %q", mr, want)
	}
	sec := blk.Elements[0].(*slack.RichTextSection)
	if len(sec.Elements) != 1 {
		t.Fatalf("got %d elements, want 1 plain text", len(sec.Elements))
	}
	te := sec.Elements[0].(*slack.RichTextSectionTextElement)
	if te.Text != "# My Title" {
		t.Errorf("text = %q, want %q", te.Text, "# My Title")
	}
	if te.Style != nil {
		t.Errorf("style = %+v, want nil", te.Style)
	}
}

func TestConvert_BlockquotePassthrough(t *testing.T) {
	mr, blk := Convert("> a quoted line")
	want := "> a quoted line"
	if mr != want {
		t.Errorf("mrkdwn = %q, want %q", mr, want)
	}
	sec := blk.Elements[0].(*slack.RichTextSection)
	te := sec.Elements[0].(*slack.RichTextSectionTextElement)
	if te.Text != "> a quoted line" {
		t.Errorf("text = %q", te.Text)
	}
}

func TestConvert_BackslashEscape(t *testing.T) {
	mr, blk := Convert(`\*not bold\*`)
	want := "*not bold*"
	if mr != want {
		t.Errorf("mrkdwn = %q, want %q", mr, want)
	}
	te := blk.Elements[0].(*slack.RichTextSection).Elements[0].(*slack.RichTextSectionTextElement)
	if te.Text != "*not bold*" {
		t.Errorf("text = %q", te.Text)
	}
	if te.Style != nil {
		t.Errorf("style = %+v, want nil for escaped asterisks", te.Style)
	}
}
```

- [ ] **Step 2: Run tests, confirm failure**

```bash
go test ./internal/slack/mrkdwn/ -run "Heading|Blockquote|Backslash" -v
```

Expected: heading and blockquote tests fail because the walker emits child text without the `#`/`>` prefix.

- [ ] **Step 3: Implement passthrough**

Edit `walk.go`. Add to `walkBlock`:

```go
	case *ast.Heading:
		w.walkRawBlock(n)
	case *ast.Blockquote:
		w.walkRawBlock(n)
```

Add `walkRawBlock`:

```go
// walkRawBlock emits the original source bytes of a block-level node
// verbatim. Used for headings and blockquotes which we don't
// translate — Slack mrkdwn has no headings, and `>` is already a
// valid blockquote marker on the wire.
func (w *walker) walkRawBlock(n ast.Node) {
	w.flushSection()
	var b strings.Builder
	for i := 0; i < n.Lines().Len(); i++ {
		seg := n.Lines().At(i)
		b.Write(w.source[seg.Start:seg.Stop])
	}
	body := strings.TrimRight(b.String(), "\n")

	// Headings come without their leading "# " in the source segments
	// (goldmark consumes the marker). Blockquotes come without the
	// leading "> ". Reconstruct the original prefix.
	prefix := ""
	if h, ok := n.(*ast.Heading); ok {
		prefix = strings.Repeat("#", h.Level) + " "
	} else if _, ok := n.(*ast.Blockquote); ok {
		// Walk our own children to grab inline text; blockquote
		// segments aren't directly populated.
		var sb strings.Builder
		for c := n.FirstChild(); c != nil; c = c.NextSibling() {
			collectRawInline(&sb, c, w.source)
		}
		body = sb.String()
		prefix = "> "
	}

	w.mrkdwn.WriteString(prefix)
	w.mrkdwn.WriteString(body)
	w.mrkdwn.WriteString("\n\n")

	if w.curSection == nil {
		w.curSection = slack.NewRichTextSection()
	}
	te := slack.NewRichTextSectionTextElement(prefix+body, nil)
	w.curSection.Elements = append(w.curSection.Elements, te)
	w.flushSection()
}

// collectRawInline appends the source bytes of all *ast.Text descendants
// of n to b, joining adjacent text segments with no separator.
func collectRawInline(b *strings.Builder, n ast.Node, source []byte) {
	if t, ok := n.(*ast.Text); ok {
		b.Write(source[t.Segment.Start:t.Segment.Stop])
		return
	}
	for c := n.FirstChild(); c != nil; c = c.NextSibling() {
		collectRawInline(b, c, source)
	}
}
```

For backslash escapes: goldmark's parser consumes the `\` and emits a plain `*ast.Text` with the escaped character only. So `\*not bold\*` becomes a Text node with content `*not bold*` — already what we want, no code change required. The test should pass once the existing `*ast.Text` handler runs.

- [ ] **Step 4: Run tests, confirm pass**

```bash
go test ./internal/slack/mrkdwn/ -v
```

Expected: all tests pass.

- [ ] **Step 5: Commit**

```bash
git add internal/slack/mrkdwn/walk.go internal/slack/mrkdwn/convert_test.go
git commit -m "feat(mrkdwn): pass headings and blockquotes through verbatim

Slack has no heading construct, and mrkdwn already supports > as
blockquote, so we don't translate them. Backslash-escape sequences
(\\\\\\*) parse as literal text via goldmark's CommonMark handling —
no extra code needed."
```

---

### Task 11: Wire up `client.SendMessage` + its callsite

This task changes `SendMessage`'s signature AND updates `cmd/slk/main.go`'s `SetMessageSender` callsite in the same commit so the build stays green.

**Files:**
- Modify: `internal/slack/client.go`
- Modify: `internal/slack/client_test.go`
- Modify: `cmd/slk/main.go`

- [ ] **Step 1: Add capture infrastructure to the mock**

Edit `internal/slack/client_test.go`. Find the `mockSlackAPI` struct (around line 29) and add capture fields:

```go
type mockSlackAPI struct {
	authTestFn               func() (*slack.AuthTestResponse, error)
	getConversationRepliesFn func(params *slack.GetConversationRepliesParameters) ([]slack.Message, bool, string, error)
	getEmojiFn               func() (map[string]string, error)
	getPermalinkContextFn    func(ctx context.Context, params *slack.PermalinkParameters) (string, error)
	setUserPresenceContextFn func(ctx context.Context, presence string) error
	getUserPresenceContextFn func(ctx context.Context, user string) (*slack.UserPresence, error)
	setSnoozeContextFn       func(ctx context.Context, minutes int) (*slack.DNDStatus, error)
	endSnoozeContextFn       func(ctx context.Context) (*slack.DNDStatus, error)
	endDNDContextFn          func(ctx context.Context) error
	getDNDInfoContextFn      func(ctx context.Context, user *string, options ...slack.ParamOption) (*slack.DNDStatus, error)
	uploadFileContextFn      func(ctx context.Context, params slack.UploadFileParameters) (*slack.FileSummary, error)

	// New: capture for PostMessage / UpdateMessage. Tests set these
	// to record the channel/timestamp + options slice and return a
	// canned timestamp.
	postMessageFn   func(channelID string, options ...slack.MsgOption) (string, string, error)
	updateMessageFn func(channelID, timestamp string, options ...slack.MsgOption) (string, string, string, error)
}
```

Replace the existing `PostMessage` and `UpdateMessage` methods (around line 80) with delegating versions:

```go
func (m *mockSlackAPI) PostMessage(channelID string, options ...slack.MsgOption) (string, string, error) {
	if m.postMessageFn != nil {
		return m.postMessageFn(channelID, options...)
	}
	return "", "", nil
}

func (m *mockSlackAPI) UpdateMessage(channelID, timestamp string, options ...slack.MsgOption) (string, string, string, error) {
	if m.updateMessageFn != nil {
		return m.updateMessageFn(channelID, timestamp, options...)
	}
	return "", "", "", nil
}
```

- [ ] **Step 2: Add test helper for inspecting captured options**

Add to `client_test.go` (after the mock definitions, before existing tests):

```go
// capturedSendArgs holds the text and blocks recovered from a
// captured *slack.MsgOption set by replaying them against an empty
// slack.Msg via UnsafeApplyMsgOptions.
type capturedSendArgs struct {
	channel string
	ts      string
	text    string
	blocks  []slack.Block
}

// captureSendOptions runs the given options through Slack's option
// applier so we can inspect the resulting wire-form values.
// channelID is what the caller passed; ts is empty for new sends.
func captureSendOptions(t *testing.T, channelID, ts string, options ...slack.MsgOption) capturedSendArgs {
	t.Helper()
	_, vals, err := slack.UnsafeApplyMsgOptions("xoxc-fake", channelID, "https://example.invalid/api/", options...)
	if err != nil {
		t.Fatalf("UnsafeApplyMsgOptions: %v", err)
	}
	out := capturedSendArgs{channel: channelID, ts: ts, text: vals.Get("text")}
	if blocksJSON := vals.Get("blocks"); blocksJSON != "" {
		var raw []map[string]any
		if err := json.Unmarshal([]byte(blocksJSON), &raw); err != nil {
			t.Fatalf("unmarshal blocks: %v", err)
		}
		// Re-marshal each block and let slack-go decode it into a
		// concrete *RichTextBlock for assertions.
		for _, b := range raw {
			data, _ := json.Marshal(b)
			var msg slack.Message
			if err := json.Unmarshal([]byte(`{"blocks":[`+string(data)+`]}`), &msg); err != nil {
				t.Fatalf("unmarshal block into Message: %v", err)
			}
			out.blocks = append(out.blocks, msg.Blocks.BlockSet...)
		}
	}
	return out
}
```

Add `encoding/json` to the test file's imports if not already present.

- [ ] **Step 3: Add the failing test**

```go
func TestSendMessage_BuildsRichTextBlock(t *testing.T) {
	var captured []slack.MsgOption
	var capturedChannel string
	mock := &mockSlackAPI{
		postMessageFn: func(channelID string, options ...slack.MsgOption) (string, string, error) {
			capturedChannel = channelID
			captured = options
			return channelID, "1700000000.000100", nil
		},
	}
	c := &Client{api: mock}

	ts, sentMrkdwn, err := c.SendMessage(context.Background(), "C1", "**hello** world")
	if err != nil {
		t.Fatalf("SendMessage: %v", err)
	}
	if ts != "1700000000.000100" {
		t.Errorf("ts = %q, want canned timestamp", ts)
	}
	if sentMrkdwn != "*hello* world" {
		t.Errorf("sentMrkdwn = %q, want %q", sentMrkdwn, "*hello* world")
	}
	if capturedChannel != "C1" {
		t.Errorf("channel = %q, want C1", capturedChannel)
	}

	args := captureSendOptions(t, capturedChannel, "", captured...)
	if args.text != "*hello* world" {
		t.Errorf("wire text = %q, want %q", args.text, "*hello* world")
	}
	if len(args.blocks) != 1 {
		t.Fatalf("got %d blocks, want 1 rich_text", len(args.blocks))
	}
	if _, ok := args.blocks[0].(*slack.RichTextBlock); !ok {
		t.Errorf("block[0] is %T, want *RichTextBlock", args.blocks[0])
	}
}

func TestSendMessage_PlainTextSendsBothTextAndBlocks(t *testing.T) {
	// Even plain text gets a rich_text block (uniform wire shape).
	var captured []slack.MsgOption
	mock := &mockSlackAPI{
		postMessageFn: func(channelID string, options ...slack.MsgOption) (string, string, error) {
			captured = options
			return channelID, "1.0", nil
		},
	}
	c := &Client{api: mock}
	_, mr, err := c.SendMessage(context.Background(), "C1", "hello")
	if err != nil {
		t.Fatalf("SendMessage: %v", err)
	}
	if mr != "hello" {
		t.Errorf("mrkdwn = %q", mr)
	}
	args := captureSendOptions(t, "C1", "", captured...)
	if args.text != "hello" {
		t.Errorf("wire text = %q", args.text)
	}
	if len(args.blocks) != 1 {
		t.Errorf("blocks = %d, want 1", len(args.blocks))
	}
}

func TestSendMessage_EmptyTextSendsNoBlocks(t *testing.T) {
	// Empty input should produce empty mrkdwn and no blocks.
	var captured []slack.MsgOption
	mock := &mockSlackAPI{
		postMessageFn: func(channelID string, options ...slack.MsgOption) (string, string, error) {
			captured = options
			return channelID, "1.0", nil
		},
	}
	c := &Client{api: mock}
	_, mr, _ := c.SendMessage(context.Background(), "C1", "")
	if mr != "" {
		t.Errorf("mrkdwn = %q, want empty", mr)
	}
	args := captureSendOptions(t, "C1", "", captured...)
	if args.text != "" {
		t.Errorf("text = %q, want empty", args.text)
	}
	if len(args.blocks) != 0 {
		t.Errorf("blocks = %d, want 0", len(args.blocks))
	}
}
```

- [ ] **Step 4: Run tests, confirm failure**

```bash
go test ./internal/slack/ -run TestSendMessage -v
```

Expected: compile errors — `SendMessage` returns `(string, error)` not `(string, string, error)`.

- [ ] **Step 5: Update `SendMessage` signature**

Edit `internal/slack/client.go` (line 479-485):

```go
// SendMessage posts a new message to the specified channel.
// Returns the timestamp of the sent message and the converted mrkdwn
// text actually sent (callers use this for optimistic display so it
// matches what other Slack clients will render).
func (c *Client) SendMessage(ctx context.Context, channelID, text string) (string, string, error) {
	mr, block := mrkdwn.Convert(text)
	opts := []slack.MsgOption{slack.MsgOptionText(mr, false)}
	if block != nil {
		opts = append(opts, slack.MsgOptionBlocks(block))
	}
	_, ts, err := c.api.PostMessage(channelID, opts...)
	if err != nil {
		return "", "", fmt.Errorf("sending message: %w", err)
	}
	return ts, mr, nil
}
```

Add the import to the top of `client.go`:

```go
	"github.com/gammons/slk/internal/slack/mrkdwn"
```

- [ ] **Step 6: Update the `SetMessageSender` callsite**

Edit `cmd/slk/main.go` around line 587:

```go
		app.SetMessageSender(func(channelID, text string) tea.Msg {
			ctx := context.Background()
			ts, sentMrkdwn, err := client.SendMessage(ctx, channelID, text)
			if err != nil {
				log.Printf("Warning: failed to send message: %v", err)
				return nil
			}
			userName := "you"
			if resolved, ok := userNames[client.UserID()]; ok {
				userName = resolved
			}
			return ui.MessageSentMsg{
				ChannelID: channelID,
				Message: messages.MessageItem{
					TS:        ts,
					UserID:    client.UserID(),
					UserName:  userName,
					Text:      sentMrkdwn,
					Timestamp: formatTimestamp(ts, tsFormat),
				},
			}
		})
```

The change: assign the second return value to `sentMrkdwn` and use it as `MessageItem.Text` instead of the raw `text` argument.

- [ ] **Step 7: Verify everything builds and tests pass**

```bash
go build ./...
go test ./internal/slack/... -run TestSendMessage -v
go test ./...
```

Expected: clean build, new tests pass, no other tests broken.

- [ ] **Step 8: Commit**

```bash
git add internal/slack/client.go internal/slack/client_test.go cmd/slk/main.go
git commit -m "feat(slack): SendMessage builds rich_text block + returns sentMrkdwn

CommonMark in compose now translates to Slack's mrkdwn fallback +
rich_text block on send. The optimistic message uses the converted
mrkdwn so slk's own renderer (which expects mrkdwn) displays it
correctly. SetMessageSender callsite updated for the new return arity."
```

---

### Task 12: Wire up `client.SendReply` + its callsite

**Files:**
- Modify: `internal/slack/client.go`
- Modify: `internal/slack/client_test.go`
- Modify: `cmd/slk/main.go`

- [ ] **Step 1: Add failing test**

Append to `client_test.go`:

```go
func TestSendReply_BuildsRichTextBlock(t *testing.T) {
	var captured []slack.MsgOption
	mock := &mockSlackAPI{
		postMessageFn: func(channelID string, options ...slack.MsgOption) (string, string, error) {
			captured = options
			return channelID, "1700000000.000200", nil
		},
	}
	c := &Client{api: mock}

	ts, sentMrkdwn, err := c.SendReply(context.Background(), "C1", "1700000000.000100", "see [docs](https://x.com)")
	if err != nil {
		t.Fatalf("SendReply: %v", err)
	}
	if ts != "1700000000.000200" {
		t.Errorf("ts = %q", ts)
	}
	if sentMrkdwn != "see <https://x.com|docs>" {
		t.Errorf("sentMrkdwn = %q", sentMrkdwn)
	}

	args := captureSendOptions(t, "C1", "", captured...)
	if args.text != "see <https://x.com|docs>" {
		t.Errorf("wire text = %q", args.text)
	}
	if len(args.blocks) != 1 {
		t.Errorf("expected 1 block, got %d", len(args.blocks))
	}
	// Confirm thread_ts is set in the captured opts.
	_, vals, _ := slack.UnsafeApplyMsgOptions("xoxc-fake", "C1", "https://example.invalid/api/", captured...)
	if vals.Get("thread_ts") != "1700000000.000100" {
		t.Errorf("thread_ts = %q, want parent ts", vals.Get("thread_ts"))
	}
}
```

- [ ] **Step 2: Run test, confirm failure**

```bash
go test ./internal/slack/ -run TestSendReply -v
```

Expected: compile error — `SendReply` returns `(string, error)`.

- [ ] **Step 3: Update `SendReply`**

Edit `client.go` around line 663:

```go
// SendReply posts a threaded reply to the specified message.
// Returns the timestamp and the converted mrkdwn text actually sent.
func (c *Client) SendReply(ctx context.Context, channelID, threadTS, text string) (string, string, error) {
	mr, block := mrkdwn.Convert(text)
	opts := []slack.MsgOption{
		slack.MsgOptionText(mr, false),
		slack.MsgOptionTS(threadTS),
	}
	if block != nil {
		opts = append(opts, slack.MsgOptionBlocks(block))
	}
	_, ts, err := c.api.PostMessage(channelID, opts...)
	if err != nil {
		return "", "", fmt.Errorf("sending reply: %w", err)
	}
	return ts, mr, nil
}
```

- [ ] **Step 4: Update `SetThreadReplySender` callsite**

Edit `cmd/slk/main.go` around line 748:

```go
		app.SetThreadReplySender(func(channelID, threadTS, text string) tea.Msg {
			ctx := context.Background()
			ts, sentMrkdwn, err := client.SendReply(ctx, channelID, threadTS, text)
			if err != nil {
				log.Printf("Warning: failed to send thread reply: %v", err)
				return nil
			}
			userName := "you"
			if resolved, ok := userNames[client.UserID()]; ok {
				userName = resolved
			}
			return ui.ThreadReplySentMsg{
				ChannelID: channelID,
				ThreadTS:  threadTS,
				Message: messages.MessageItem{
					TS:        ts,
					UserID:    client.UserID(),
					UserName:  userName,
					Text:      sentMrkdwn,
					Timestamp: formatTimestamp(ts, tsFormat),
					ThreadTS:  threadTS,
				},
			}
		})
```

- [ ] **Step 5: Verify build and tests**

```bash
go build ./...
go test ./internal/slack/ -run TestSendReply -v
go test ./...
```

Expected: clean.

- [ ] **Step 6: Commit**

```bash
git add internal/slack/client.go internal/slack/client_test.go cmd/slk/main.go
git commit -m "feat(slack): SendReply builds rich_text block + returns sentMrkdwn

Thread replies now go through the same CommonMark conversion as
top-level messages. SetThreadReplySender callsite updated."
```

---

### Task 13: Wire up `client.EditMessage` + its callsite

**Files:**
- Modify: `internal/slack/client.go`
- Modify: `internal/slack/client_test.go`
- Modify: `cmd/slk/main.go`

- [ ] **Step 1: Add failing test**

Append to `client_test.go`:

```go
func TestEditMessage_BuildsRichTextBlock(t *testing.T) {
	var (
		capturedChannel string
		capturedTS      string
		captured        []slack.MsgOption
	)
	mock := &mockSlackAPI{
		updateMessageFn: func(channelID, timestamp string, options ...slack.MsgOption) (string, string, string, error) {
			capturedChannel = channelID
			capturedTS = timestamp
			captured = options
			return channelID, timestamp, "*new* text", nil
		},
	}
	c := &Client{api: mock}

	sentMrkdwn, err := c.EditMessage(context.Background(), "C1", "1700000000.000100", "**new** text")
	if err != nil {
		t.Fatalf("EditMessage: %v", err)
	}
	if sentMrkdwn != "*new* text" {
		t.Errorf("sentMrkdwn = %q", sentMrkdwn)
	}
	if capturedChannel != "C1" || capturedTS != "1700000000.000100" {
		t.Errorf("captured (%q, %q)", capturedChannel, capturedTS)
	}

	args := captureSendOptions(t, capturedChannel, capturedTS, captured...)
	if args.text != "*new* text" {
		t.Errorf("wire text = %q", args.text)
	}
	if len(args.blocks) != 1 {
		t.Errorf("expected 1 block, got %d", len(args.blocks))
	}
}
```

- [ ] **Step 2: Run test, confirm failure**

```bash
go test ./internal/slack/ -run TestEditMessage -v
```

Expected: compile error — `EditMessage` returns `error` only.

- [ ] **Step 3: Update `EditMessage`**

Edit `client.go` around line 700:

```go
// EditMessage updates an existing message's text. Returns the
// converted mrkdwn text that was sent (callers may use it for
// optimistic display, but the message-changed WS echo is the
// authoritative source of truth for the displayed body).
func (c *Client) EditMessage(ctx context.Context, channelID, ts, text string) (string, error) {
	mr, block := mrkdwn.Convert(text)
	opts := []slack.MsgOption{slack.MsgOptionText(mr, false)}
	if block != nil {
		opts = append(opts, slack.MsgOptionBlocks(block))
	}
	_, _, _, err := c.api.UpdateMessage(channelID, ts, opts...)
	if err != nil {
		return "", fmt.Errorf("editing message: %w", err)
	}
	return mr, nil
}
```

- [ ] **Step 4: Update `SetMessageEditor` callsite**

Edit `cmd/slk/main.go` around line 610:

```go
		app.SetMessageEditor(func(channelID, ts, text string) tea.Msg {
			ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
			defer cancel()
			// EditMessage returns the converted mrkdwn but we ignore
			// it here: the message_changed WS echo updates the local
			// copy with the server-stored text via UpdateMessageInPlace
			// (internal/ui/app.go:1382). MessageEditedMsg only carries
			// success/fail status.
			_, err := client.EditMessage(ctx, channelID, ts, text)
			if err != nil {
				log.Printf("Warning: failed to edit message %s/%s: %v", channelID, ts, err)
			}
			return ui.MessageEditedMsg{ChannelID: channelID, TS: ts, Err: err}
		})
```

- [ ] **Step 5: Verify build and tests**

```bash
go build ./...
go test ./internal/slack/ -run TestEditMessage -v
go test ./...
```

Expected: clean.

- [ ] **Step 6: Commit**

```bash
git add internal/slack/client.go internal/slack/client_test.go cmd/slk/main.go
git commit -m "feat(slack): EditMessage builds rich_text block + returns sentMrkdwn

Edits now go through CommonMark conversion. The callsite ignores the
new return value because edit display is updated via the
message_changed WS echo, not the optimistic path."
```

---

### Task 14: README updates

**Files:**
- Modify: `README.md`

- [ ] **Step 1: Add CommonMark bullet to the Compose section**

Find the section "### Compose" (around line 38). Add a new bullet after the existing ones:

```markdown
- CommonMark in compose: type `**bold**`, `~~strike~~`, `[label](url)`, `- list items`, `1. numbered`, or fenced ```code blocks``` and slk converts them on send to Slack's mrkdwn + rich_text format. Already-mrkdwn syntax (`*bold*`, `_italic_`, `~strike~`) passes through unchanged. Single-asterisk emphasis (`*x*`) is preserved as literal text since it conflicts with Slack mrkdwn bold.
```

The exact diff is:

```diff
 ### Compose
 - Multi-line input, `Shift+Enter` for newlines
 - Inline `@mention` autocomplete (resolves to `<@UserID>` on send)
 - Special mentions: `@here`, `@channel`, `@everyone`
 - Bracketed paste — paste multi-line text from the system clipboard without it being interpreted as keystrokes
 - Smart paste (`Ctrl+V`) — pastes a clipboard image as an attachment, or a copied file path as an attached file, or falls through to text. Multiple attachments + caption send together via Slack's V2 file-upload API. Note: use `Ctrl+V` (not your terminal's `Ctrl+Shift+V` paste shortcut) — terminal-initiated paste only delivers text, never image bytes.
+- CommonMark in compose: type `**bold**`, `~~strike~~`, `[label](url)`, `- list items`, `1. numbered`, or fenced ```code blocks``` and slk converts them on send to Slack's mrkdwn + rich_text format. Already-mrkdwn syntax (`*bold*`, `_italic_`, `~strike~`) passes through unchanged. Single-asterisk emphasis (`*x*`) is preserved as literal text since it conflicts with Slack mrkdwn bold.
```

- [ ] **Step 2: Add caveat to the Tradeoffs section**

Find "**Image rendering caveats:**" (around line 148). Add a new caveats subsection BEFORE that one:

```markdown
**Markdown caveats:**
- Editing a message you originally formatted with markdown may flatten the rich_text formatting on Slack clients that prefer blocks. The mrkdwn fallback (`*bold*`, etc.) still renders correctly everywhere.
- Headings (`# Title`) and blockquotes (`> quote`) are passed through verbatim — Slack has no heading construct and `>` is already valid mrkdwn.
- Tables, footnotes, task lists, and reference-style links are not translated.

```

- [ ] **Step 3: Commit**

```bash
git add README.md
git commit -m "docs(readme): document CommonMark conversion in compose

New bullet under Compose; new Markdown-caveats subsection under
Tradeoffs covering the edit-flattening limitation and unsupported
features."
```

---

### Task 15: Manual integration verification

This is a manual smoke test against a real Slack workspace; no automated tests added.

**Files:** none.

- [ ] **Step 1: Build and run slk against a test workspace**

```bash
make build
./bin/slk
```

Pick a workspace and a channel where you can post freely (e.g., a personal #scratch channel or DM to yourself).

- [ ] **Step 2: Verify each formatting feature**

Send each of the following messages, one at a time, and confirm the result both in slk's own pane AND in the Slack web client side-by-side:

1. `**bold text**` → bold in slk, bold in web Slack.
2. `__also bold__` → bold in both.
3. `_italic text_` → italic in both.
4. `*should stay literal*` → literal asterisks in both (NOT italic, NOT bold-with-double-asterisks-style).
5. `~~strikethrough~~` → strike in both.
6. `[GitHub](https://github.com)` → clickable "GitHub" linking to https://github.com in both.
7. `` `inline code` `` → monospace in both.
8. ` ```code block\nfoo\nbar\n``` ` → preformatted block in both.
9. Bullet list:
   ```
   - one
   - two
   - three
   ```
10. Numbered list:
    ```
    1. first
    2. second
    ```
11. Nested:
    ```
    - outer
        - inner
    ```
12. Mixed: `**bold @yourself**` → bold text including the mention; mention resolves to your name.
13. Fence + bold: a code block containing `**not bold**` — must NOT be styled.

- [ ] **Step 3: Verify edit roundtrip**

Send `**before edit**`. Press `E` to edit. The compose box should show `*before edit*` (the converted mrkdwn). Add a word, press Enter. Confirm:
- The edit succeeds (no Slack API error).
- The displayed text in slk updates to reflect the edit.
- On the Slack web client, the edited message also reflects the new text.

(Per the spec, the rich_text block formatting may flatten on this edit roundtrip. The mrkdwn fallback should still render correctly in slk.)

- [ ] **Step 4: Verify thread replies**

Open a thread (`Enter` on a message), reply with `**bold reply**`. Confirm bold rendering in both clients.

- [ ] **Step 5: Verify nothing broke**

Send a plain message (no markdown). Confirm it appears identically in both clients. Send a message that contains a literal `<` or `>` (e.g., `5 < 10`). Confirm it doesn't get parsed as a Slack token.

- [ ] **Step 6: Run the full test suite one more time**

```bash
go test ./...
go vet ./...
```

Expected: all clean.

- [ ] **Step 7: No commit**

This task adds no code. If everything checks out, the feature is done.

If anything unexpected shows up, capture a minimal repro and add it as a regression test in `internal/slack/mrkdwn/convert_test.go` before fixing.

---

<!-- END OF PLAN -->

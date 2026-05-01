# CommonMark Compose Conversion

Date: 2026-05-01

## Problem

Messages composed in slk's compose box are sent verbatim to Slack via
`MsgOptionText(text, false)`. Slack expects its own `mrkdwn` dialect
(`*bold*`, `_italic_`, `~strike~`, no list syntax) for any text-side
formatting and `rich_text` blocks for structural elements (lists, code
blocks). slk's display-side renderer also accepts only mrkdwn.

Users who type CommonMark — `**bold**`, `~~strike~~`, `[label](url)`,
`- list items` — see their messages render as raw text:

- In slk's own message pane, because the renderer's regex
  `\*([^*\n]+)\*` (`internal/ui/messages/render.go:19`) requires single
  asterisks, and there is no list handling at all.
- On other Slack clients, because Slack's server does not transform
  CommonMark to mrkdwn — the official desktop/web client does that
  client-side before posting.

The asymmetry the user observes ("other people's messages render fine,
mine don't") is dialect mismatch, not a code path divergence: every
message in slk renders through the same `RenderSlackMarkdown`
(`internal/ui/messages/render.go:341`).

## Goal

Translate CommonMark in the compose box to Slack's wire formats at send
time, so the user can type natural markdown and see it render the same
way everywhere — slk, other Slack clients, push notifications.

## Scope

### Translated by the converter

- `**bold**` and `__bold__` → mrkdwn `*bold*`
- `~~strike~~` → mrkdwn `~strike~`
- `[label](url)` → mrkdwn `<url|label>`
- `- item`, `* item`, `1. item` → `rich_text_list` block
- Fenced code blocks (` ``` `) → `rich_text_preformatted` block
- Indented code blocks (4-space) → `rich_text_preformatted` block
- Inline `` `code` `` → preserved (already valid mrkdwn)

### Preserved as literal text (NOT translated)

- `*x*` (single-asterisk emphasis): Slack mrkdwn already treats this as
  bold. Translating to `_x_` would break users who type mrkdwn-style
  bold. The asterisks pass through to the mrkdwn fallback unchanged; in
  the rich_text block the inner content carries no italic style flag.
- `_italic_` (single-underscore emphasis): already valid mrkdwn,
  passes through with italic style applied to the rich_text.
- `~strike~` (single-tilde): already valid mrkdwn, passes through.
- `# Title` headings: Slack has no heading construct; emit as plain
  text "# Title" rather than fake-translating to bold paragraphs.
- `> quoted` blockquotes: emit as plain mrkdwn `> quoted` text. Slack
  renders mrkdwn `>` as a quote on every client; we don't build a
  `rich_text_quote` block in v1.

### Out of scope for v1

- Tables, footnotes, task lists, autolinks — these goldmark extensions
  stay disabled (only `Strikethrough` is enabled).
- Bold-italic combined (`***x***`) gets natural AST handling but no
  special-cased mrkdwn output beyond what the walker produces.
- Reference-style links (`[label][ref]` + `[ref]: url`).
- Reverse converter (rich_text → CommonMark) for round-tripping edits.
- Per-user config flag to disable conversion.
- Live preview in compose box (showing `**bold**` as bold while
  typing).

## Architecture

A new package `internal/slack/mrkdwn/` exposes one entry point:

```go
// Convert parses CommonMark in text and returns the Slack-compatible
// payload pair: a mrkdwn fallback string and a rich_text block. text
// may contain Slack wire-form tokens (<@U…>, <#C…|name>, <!here>,
// <https://…|label>); these are preserved as opaque tokens in the
// mrkdwn fallback and become typed elements (user / channel /
// broadcast / link) in the block. Returns ("", nil) for empty input.
func Convert(text string) (mrkdwn string, block *slack.RichTextBlock)
```

Files:

```
internal/slack/mrkdwn/
├── convert.go        # Public API
├── walk.go           # Goldmark AST walker
├── tokens.go         # Pre-tokenization of <...> Slack wire forms
├── convert_test.go   # Table-driven tests
└── doc.go            # Package docstring
```

New dependencies: `github.com/yuin/goldmark` and
`github.com/yuin/goldmark/extension` (only `Strikethrough` enabled).

Reuses existing slack-go types (`slack.RichTextBlock`,
`slack.RichTextSection`, `slack.RichTextList`,
`slack.RichTextPreformatted`, plus the section element types). No new
JSON types.

## Wire integration

The three message-sending methods on `internal/slack/client.go` gain an
extra return value carrying the converted mrkdwn so callers can use it
for optimistic display. Current → new signatures:

```go
SendMessage(ctx, channelID, text)              (ts, error)            -> (ts, sentMrkdwn, error)
SendReply  (ctx, channelID, threadTS, text)    (ts, error)            -> (ts, sentMrkdwn, error)
EditMessage(ctx, channelID, ts, text)          (error)                -> (sentMrkdwn, error)
```

Each method internally:

```go
m, block := mrkdwn.Convert(text)
opts := []slack.MsgOption{slack.MsgOptionText(m, false)}
if block != nil {
    opts = append(opts, slack.MsgOptionBlocks(block))
}
_, ts, err := c.api.PostMessage(channelID, opts...)
return ts, m, err
```

The interface `SlackAPI` (mockable, defined at
`internal/slack/client.go:21`) is unchanged. The mock continues to
capture options; tests assert that both `MsgOptionText` and
`MsgOptionBlocks` are present.

`compose.TranslateMentionsForSend` (`internal/ui/compose/model.go:739`)
remains unchanged. Its output (text containing `<@U…>`, `<#C…|name>`,
`<!here>`) is what gets fed into the converter.

## Conversion algorithm

### Step 1: Pre-tokenize Slack wire forms

Goldmark would parse `<...>` as autolinks or HTML and damage Slack
tokens. Before parsing:

1. Scan input for these patterns, in priority order:
   - `<@U[A-Z0-9]+>` — user mention
   - `<#C[A-Z0-9]+(?:\|[^>]+)?>` — channel mention
   - `<![a-z]+(?:\^[A-Za-z0-9]+)?(?:\|[^>]+)?>` — broadcast (`<!here>`,
     `<!subteam^S123|@team>`)
   - `<https?://[^|>]+(?:\|[^>]+)?>` — labeled or bare URL
2. Replace each match with `\uE000<index>\uE001` (Unicode private-use
   sentinels — guaranteed not to collide with real text).
3. Keep an ordered map index → (kind, payload).

Goldmark sees only sentinels (treated as opaque text), so it never
mis-parses Slack tokens.

### Step 2: Configure goldmark

```go
md := goldmark.New(
    goldmark.WithExtensions(extension.Strikethrough),
    // No autolinks, no HTML, no tables, no task lists, no footnotes.
)
```

We do not use goldmark's renderer. We walk the AST ourselves.

### Step 3: Walk the AST

Two parallel builders:

- `mrkdwnBuf strings.Builder` — accumulates fallback string.
- `block *slack.RichTextBlock` — accumulates rich_text elements.

Node-to-output mapping:

| Goldmark node | Mrkdwn fallback | Rich_text element |
|---|---|---|
| `*ast.Document` | walk children | walk children |
| `*ast.Paragraph` | inline run + `\n\n` | new `RichTextSection` |
| `*ast.TextBlock` (inside list items) | inline run | inline elements appended to current section |
| `*ast.List` (`-`/`*`/`+` marker) | each item as `• item\n` | new `RichTextList{Style: "bullet", Indent: depth}` |
| `*ast.List` (`1.` marker) | each item as `1. item\n` (1-based counter) | new `RichTextList{Style: "ordered", Indent: depth}` |
| `*ast.ListItem` | child run | new `RichTextSection` inside the list |
| `*ast.FencedCodeBlock` | ` ```\nbody\n``` ` | `RichTextPreformatted` with one text element |
| `*ast.CodeBlock` (4-space indent) | wrap content in ` ``` ... ``` ` | `RichTextPreformatted` with one text element (same as fenced) |
| `*ast.Heading` | raw text incl. `#` characters | `RichTextSection` with one plain text element |
| `*ast.Blockquote` | each line `> line` | `RichTextSection` with text `> line` (no quote block) |
| `*ast.Text` | append literal text | append `RichTextSectionTextElement` with inherited style |
| `*ast.Emphasis` (Level 1) | see "Italic preservation" below | text element with `Style.Italic = true` only when source uses `_..._` |
| `*ast.Emphasis` (Level 2, strong) | `*body*` | text element with `Style.Bold = true` |
| `*ast.CodeSpan` | `` `body` `` | text element with `Style.Code = true` |
| `*ast.Link` | `<href\|label>` (escape `>` in label) | `RichTextSectionLinkElement{URL: href, Text: label}` |
| Strikethrough (extension) | `~body~` | text element with `Style.Strike = true` |
| Sentinel `\uE000<n>\uE001` | original wire form `<…>` | proper user / channel / broadcast / link element |

### Step 4: Italic preservation (the `*x*` rule)

Goldmark marks both `*x*` and `_x_` as `*ast.Emphasis` with
`Level == 1`. To distinguish them, the walker inspects the source
bytes covered by the node's segment:

```go
emph := node.(*ast.Emphasis)
seg  := emph.Lines().At(0)
firstByte := source[seg.Start]
if firstByte == '_' {
    // _x_: render as mrkdwn italic AND set Style.Italic in rich_text.
    mrkdwnBuf.WriteString("_")
    walkChildren(emph, italicStyleOn)
    mrkdwnBuf.WriteString("_")
} else {
    // *x*: preserve the asterisks as literal text in mrkdwn,
    // emit no italic style flag in rich_text. Slack's mrkdwn parser
    // (and slk's renderer) interpret *x* as bold; on rich_text-rendering
    // clients the asterisks render as plain characters around the
    // text. End result is acceptable for v1.
    mrkdwnBuf.WriteString("*")
    walkChildrenAsLiteral(emph)
    mrkdwnBuf.WriteString("*")
}
```

"Walking as literal" still processes inline code, links, and mentions
inside the run; it just suppresses the italic style flag.

### Step 5: Output assembly

- Trim trailing newlines from `mrkdwnBuf` (leading whitespace is
  preserved verbatim — paragraphs may legitimately start with spaces).
- If the block has zero children (input was empty after pre-tokenization
  and trimming), return `("", nil)`.
- Otherwise return both. Even single-paragraph plain-text input gets a
  one-section, one-text-element block — uniform wire shape.

## Optimistic display path

```
compose.Value()
  → "Hello @bob **bold**"
compose.TranslateMentionsForSend
  → "Hello <@U123> **bold**"
client.SendMessage(...) [calls mrkdwn.Convert internally]
  → wire: text="Hello <@U123> *bold*",
          blocks=[RichTextBlock with bold "bold"]
  → returns (ts, sentMrkdwn="Hello <@U123> *bold*", err)
SetMessageSender callback
  → MessageItem{ TS: ts, Text: sentMrkdwn, … }
MessageSentMsg → messagepane.AppendMessage → renderMessagePlain
  → RenderSlackMarkdown sees "*bold*" → renders as bold ✓
```

The user types `**bold**`, sends, immediately sees their message
rendered as bold in slk — same way other clients render it.

Three callbacks in `cmd/slk/main.go` need updating:

- `SetMessageSender` (line 587) — calls `client.SendMessage`. Capture
  new `sentMrkdwn` return value, use it as `MessageItem.Text` instead
  of `text` in the `MessageSentMsg.Message`.
- `SetThreadReplySender` (line 748) — calls `client.SendReply`. Same
  pattern: use `sentMrkdwn` for `ThreadReplySentMsg.Message.Text`.
- `SetMessageEditor` (line 610) — calls `client.EditMessage`. The
  callback receives the new `sentMrkdwn` return value but does NOT
  need to plumb it into `MessageEditedMsg`: edit display is updated
  via the `message_changed` WebSocket echo (`internal/ui/app.go:1382-1395`,
  which calls `UpdateMessageInPlace` with the server-stored text and
  bypasses the `isSelfSent` dedup). The callback can ignore the new
  return; we keep the signature change for symmetry with the other
  two methods. `MessageEditedMsg` itself is unchanged.

## Known limitation: edit flattening

When a user presses `E` to edit a message they originally formatted with
`**bold**`:

- slk loads the message body — Slack stored `*bold*` (the converted
  mrkdwn).
- The compose box shows `*bold*`, not the original `**bold**`.
- If the user re-submits unchanged, the converter sees `*bold*`. Per
  the italic-preservation rule, `*x*` is preserved as literal
  asterisks — no bold style flag in the new rich_text block.
- Wire result: `text="Hello *bold*"`, `blocks=[plain-text "Hello *bold*"]`
- Display via mrkdwn fallback (slk + most Slack clients): bold ✓
- Display via rich_text block on clients that prefer it: plain
  characters around plain text ✗

The fallback ensures consistent rendering on every client we currently
care about. A proper fix requires a reverse converter (rich_text →
CommonMark) to repopulate the compose box on edit; deferred to a later
milestone.

## Testing

### `internal/slack/mrkdwn/convert_test.go` (table-driven)

Each case is `(name, input, wantMrkdwn, wantBlockJSON)`. Block
comparisons use a JSON serializer for readability. Sample coverage:

- Bold: `**hello**`, `__hello__` → `*hello*` + bold style
- Italic underscore: `_hello_` → `_hello_` + italic style
- Italic asterisk preserved: `*hello*` → `*hello*` + NO italic style
- Strike: `~~strike~~` → `~strike~` + strike style
- Inline code: `` `code` `` → `` `code` `` + code style
- Link with label: `[Slack](https://slack.com)` →
  `<https://slack.com|Slack>` + link element
- User mention preserved: `Hi <@U12345>!` → mention element +
  surrounding text elements
- Channel mention preserved: `see <#C123|general>` → channel element
- Broadcast preserved: `<!here> deploy` → broadcast element
- Unordered list: `- one\n- two` → `• one\n• two` + bullet
  rich_text_list with two sections
- Ordered list: `1. one\n2. two` → preserved + ordered list
- Nested list (4-space indent): `- a\n    - b` → two list elements,
  second with `Indent=1`
- Fenced code: ` ```\nfoo\n``` ` → preserved + RichTextPreformatted
- Indented code (4-space): preserved + RichTextPreformatted
- Heading raw: `# Title` → `# Title` + plain text element
- Blockquote stays mrkdwn: `> quoted` → `> quoted` text in section
- Bold containing mention: `**Hi <@U1>**` → `*Hi <@U1>*` with
  bold-styled text + bold-styled user element
- Escape: `\*not bold\*` → `*not bold*` plain text, no bold style
- Empty: `""` → `("", nil)`
- Plain text: `hello world` → `hello world` + single section/text
- Multi-paragraph: `para one\n\npara two` → two sections
- Round-trip stability: `*hello*` converts to `*hello*` and stays that
  way (does not keep mutating across edits)

### Updates to `internal/slack/client_test.go`

`mockSlackAPI.PostMessage` and `UpdateMessage` are currently no-ops
returning empty strings (`internal/slack/client_test.go:80,84`). We
add capture function fields (`postMessageFn`, `updateMessageFn`) to
the mock following the same pattern as `authTestFn`/`getEmojiFn`, so
tests can record the options slice and return canned timestamps. A
helper extracts `text` and `blocks` from the captured options using
`slack.UnsafeApplyMsgOptions` against an empty `slack.Msg`. New
tests:

- `TestSendMessageBuildsRichTextBlock` — input `"**bold**"` produces
  `MsgOptionText("*bold*", false)` and `MsgOptionBlocks(<rich_text with
  bold>)` and returns `sentMrkdwn == "*bold*"`.
- `TestSendReplyBuildsRichTextBlock` — same as above plus
  `MsgOptionTS(threadTS)`.
- `TestEditMessageBuildsRichTextBlock` — verifies the EditMessage
  path through `mockSlackAPI.UpdateMessage`.

These are new tests — `client_test.go` currently has no coverage of
`SendMessage`, `SendReply`, or `EditMessage`. The mock's
`PostMessage`/`UpdateMessage` are no-ops returning empty strings; we
will add capture-and-replay function fields (`postMessageFn`,
`updateMessageFn`) following the existing pattern of other mock
methods (`authTestFn`, `getEmojiFn`, etc.) so tests can capture
options and return canned timestamps.

### Out-of-scope for tests

- Goldmark internals (it has its own test suite).
- slack-go marshalling of `RichTextBlock` (covered by slack-go).
- Network sends — existing mock pattern continues unchanged.

## Files touched (summary)

| File | Change |
|---|---|
| `internal/slack/mrkdwn/convert.go` | NEW — `Convert` entry point |
| `internal/slack/mrkdwn/walk.go` | NEW — AST walker |
| `internal/slack/mrkdwn/tokens.go` | NEW — wire-form pre-tokenization |
| `internal/slack/mrkdwn/convert_test.go` | NEW — table tests |
| `internal/slack/mrkdwn/doc.go` | NEW — package doc |
| `internal/slack/client.go` | EDIT — three send methods get the converter + new return value |
| `internal/slack/client_test.go` | EDIT — assert blocks + text in mock options; update existing tests for new return arity |
| `cmd/slk/main.go` | EDIT — three callbacks: `SetMessageSender` and `SetThreadReplySender` use `sentMrkdwn` for optimistic display; `SetMessageEditor` accepts new return arity but ignores it (edit echo updates via WS) |
| `go.mod` / `go.sum` | EDIT — add `github.com/yuin/goldmark` |
| `README.md` | EDIT — Compose-section bullet about CommonMark; Tradeoffs note about edit flattening |

Total: ~5 new files, ~5 edited (plus go.sum churn).

## Out-of-scope items recap

- `*italic*` (single asterisk) translation — preserved as literal.
- Headings, blockquotes — preserved as raw mrkdwn.
- Tables, footnotes, task lists, autolinks — disabled.
- Reference-style links — not actively supported.
- Reverse rich_text → CommonMark for edits — deferred.
- Per-user opt-out config flag — not added.
- Live preview in compose — separate UX work.
- `RichTextQuote` block — not built; mrkdwn `> line` suffices.

# Typed URLs outside Linkify's reach — design

## Problem

A URL typed without markup has to go out as a link element: Slack's clients render the rich_text block and do not linkify its text elements (their composer makes the link as the URL is typed). `mrkdwn.Convert` links typed URLs through Linkify with `typedURLRegexp`, but some never reached it:

- **After a quote or a colon.** Linkify starts a URL only at the head of a run or after a space, `*`, `_`, `~` or `(`, so `"https://x"`, `'https://x'` and `see:https://x` stayed text.
- **`see:https://x`** was also read by the tokenizer as the emoji `:https:`, splitting the URL.
- **Headings** were emitted as typed, so a URL in one was text.
- **Blockquotes** were flattened into one text element built from their `Text` nodes. Line breaks, formatting and code markers were lost, and a link, which has no `Text` node, was dropped from the message altogether.

## Design

`typedURLRegexp` stays the one rule for what a typed URL is; nothing here widens it (ASCII only, case-sensitive schemes from `linkscheme`).

- `afterPunctURL` (`internal/slack/mrkdwn/typedurl.go`), an inline parser on `"`, `'` and `:`: when a typed URL follows, it takes the mark as text, and goldmark offers Linkify the next position as the start of a run. Linkify then links the URL as it would anywhere else.
- The tokenizer does not read `:name:` as an emoji when a typed URL starts at `name:`, i.e. the closing colon is a scheme's (`see:https://x`, `call:tel:+15550100`). An emoji after a URL (`x/a:tada:`) is still an emoji and still ends it.
- Headings stay verbatim, but `appendRawText` emits their typed URLs as links, finding them with `typedURLLen`: `typedURLRegexp` plus Linkify's closing-paren balancing, so a URL ends where it would in a paragraph (`TestTypedURLLenMatchesLinkify`).
- Blockquotes become a `rich_text_quote`, their paragraphs walked inline like any other, with `> ` on each line of the mrkdwn fallback. A blank quote line between paragraphs is kept. A nested quote is collected as raw text that keeps its line breaks, so URLs on adjacent lines stay two links.

## Not in scope

- `[https://x]`: Linkify does not link inside a possible link label, and changing that is a change to how labels are parsed.
- Uppercase schemes: `linkscheme` matches schemes case-sensitively by design.

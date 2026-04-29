# Phase 8: Documentation

> Index: `00-overview.md`. Previous: `07-fullscreen-preview.md`.

**Goal:** Update README and STATUS to reflect the shipped feature.

---

## Task 8.1: Update STATUS.md

**Files:**
- Modify: `docs/STATUS.md`

- [ ] **Step 1: Find the "inline image rendering" line**

Run: `grep -n "Inline image rendering" docs/STATUS.md`

- [ ] **Step 2: Move from "Not Yet Implemented" to "Shipped"**

Change `[ ]` to `[x]` and move into the appropriate completed section. Also update the file-count line near the top of STATUS.md if there's one (e.g., "31 source files, 24 test files" → adjust counts).

- [ ] **Step 3: Commit**

```bash
git add docs/STATUS.md
git commit -m "docs(status): mark inline image rendering shipped"
```

---

## Task 8.2: Update README keybindings table

**Files:**
- Modify: `README.md`

- [ ] **Step 1: Add to the keybindings table**

Find the existing table (around line 228-258) and insert before the `Ctrl+y` row:

```markdown
| `O` | Normal (message) | Open full-screen image preview |
| `Esc` / `q` | Preview | Close preview |
| `Enter` | Preview | Open in system image viewer |
| Click | Any (on image) | Open full-screen preview |
```

- [ ] **Step 2: Commit**

```bash
git add README.md
git commit -m "docs(readme): add image preview keybindings to table"
```

---

## Task 8.3: Update README features list

**Files:**
- Modify: `README.md`

- [ ] **Step 1: Move "Inline image rendering" out of the roadmap**

Find the bullet `Inline image rendering (Kitty graphics → Sixel → fallback)` under "On the roadmap" (around line 101) and **remove** it.

Add a new "Images" subsection under "Features" (after "Compose" or at a sensible spot):

```markdown
### Images
- Inline image attachments render automatically in the messages pane: kitty graphics protocol on capable terminals (kitty, ghostty, recent WezTerm), sixel on foot/mlterm, half-block (`▀`) fallback everywhere else
- Click any inline image (or press `O` on the selected message) for a full-screen in-app preview
- `Enter` from the preview launches the OS image viewer
- Lazy-loaded: images download only as they scroll into view
- LRU cache at `~/.cache/slk/images/` (default 200 MB cap)
- Inside tmux, slk falls back to half-block to avoid pixel-protocol pass-through pitfalls
- Configurable via `[appearance] image_protocol` (`auto` / `kitty` / `sixel` / `halfblock` / `off`) and `max_image_rows`
```

- [ ] **Step 2: Commit**

```bash
git add README.md
git commit -m "docs(readme): document inline image rendering feature"
```

---

## Task 8.4: Update README config example

**Files:**
- Modify: `README.md`

- [ ] **Step 1: Add the new keys to the example**

Find the `[appearance]` block in the config example (around line 268-272) and add:

```toml
[appearance]
theme = "dracula"
timestamp_format = "3:04 PM"
image_protocol = "auto"   # auto | kitty | sixel | halfblock | off
max_image_rows = 20       # cap inline image height in terminal rows
```

In the `[cache]` block:

```toml
[cache]
message_retention_days = 30
max_db_size_mb = 500
max_image_cache_mb = 200
```

- [ ] **Step 2: Commit**

```bash
git add README.md
git commit -m "docs(readme): add image config keys to example"
```

---

## Task 8.5: Update tradeoffs section

**Files:**
- Modify: `README.md`

- [ ] **Step 1: Note the iTerm2 limitation**

Find the "Tradeoffs & Non-Goals" section. Under a new "Known limitations" or similar:

```markdown
**Image rendering caveats:**
- iTerm2 ≥ 3.5 implements kitty graphics but does not support unicode placeholders, so it falls back to half-block.
- Animated GIFs render as a static first frame.
- Threads side panel renders attachments as text (`[Image] <url>`); inline rendering there is on the roadmap.
- Link unfurl image previews are not yet rendered inline.
```

- [ ] **Step 2: Commit**

```bash
git add README.md
git commit -m "docs(readme): document image rendering limitations"
```

---

## Phase 8 done

The feature is shipped, documented, and visible to users.

**Final verification:**

```bash
go test ./... -v
go vet ./...
go build ./...
```

Manual smoke test:
1. Open slk in kitty.
2. Switch to a channel that has recent image attachments.
3. Confirm images render inline as pixels.
4. Click an image; preview opens.
5. Press `Esc`; preview closes.
6. Open a tmux session with kitty as outer terminal, run slk inside; confirm halfblock rendering.
7. Set `image_protocol = "off"` in config; restart slk; confirm `[Image] <url>` text rendering returns.

If everything works: tag a release and update the changelog (if there is one).

---

## Plan complete

Eight phases, each independently mergeable. The full inline image rendering feature is implemented, tested, documented, and shipped.

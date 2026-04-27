# displaywidth Emoji Width Fix — Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Fix `clipperhouse/displaywidth` to correctly measure width for emoji with skin-tone modifiers and non-adjacent VS16, then wire the fix into the slack-tui project.

**Architecture:** Modify the `graphemeWidth()` function in `width.go` to scan the full grapheme cluster for VS16 and skin-tone modifiers (U+1F3FB–U+1F3FF), instead of only checking the 3 bytes immediately after the first rune. This fixes Bugs A (non-adjacent VS16) and B (skin-tone on text-default base).

**Tech Stack:** Go, `clipperhouse/displaywidth`, `go mod replace`

---

### Task 1: Clone displaywidth locally

**Files:**
- Create: `/home/grant/local_code/displaywidth/` (clone of upstream)

- [ ] **Step 1: Clone the repo**

```bash
cd /home/grant/local_code
git clone https://github.com/clipperhouse/displaywidth.git
cd displaywidth
```

- [ ] **Step 2: Create a feature branch**

```bash
git checkout -b fix-emoji-modifier-width
```

- [ ] **Step 3: Verify tests pass on the unmodified code**

```bash
go test ./...
```

Expected: All tests pass.

---

### Task 2: Write failing tests for Bug A (non-adjacent VS16)

**Files:**
- Modify: `/home/grant/local_code/displaywidth/width_test.go`

- [ ] **Step 1: Add test cases for ZWJ + skin-tone + gender sequences with non-adjacent VS16**

Add to the `TestStringWidth` test table (after the existing keycap tests, around line 61):

```go
// Bug fix: VS16 not immediately after first rune in complex sequences
{"⛹🏻‍♂️ bouncing ball tone1", "\u26F9\U0001F3FB\u200D\u2642\uFE0F", defaultOptions, 2},
{"🕵🏻‍♂️ detective tone1", "\U0001F575\U0001F3FB\u200D\u2642\uFE0F", defaultOptions, 2},
{"🏌🏻‍♀️ golfing tone1", "\U0001F3CC\U0001F3FB\u200D\u2640\uFE0F", defaultOptions, 2},
{"🏋🏿‍♂️ weights tone5", "\U0001F3CB\U0001F3FF\u200D\u2642\uFE0F", defaultOptions, 2},
```

- [ ] **Step 2: Run tests to verify they fail**

```bash
go test -run TestStringWidth -v 2>&1 | grep -E "FAIL|bouncing|detective|golfing|weights"
```

Expected: All 4 new tests FAIL with "got 1, want 2".

---

### Task 3: Write failing tests for Bug B (skin-tone modifier on text-default base)

**Files:**
- Modify: `/home/grant/local_code/displaywidth/width_test.go`

- [ ] **Step 1: Add test cases for skin-tone modifier on text-default bases**

Add to the `TestStringWidth` test table:

```go
// Bug fix: skin-tone modifier on text-default Extended_Pictographic base
{"🕵🏻 detective skin", "\U0001F575\U0001F3FB", defaultOptions, 2},
{"☝🏽 point up skin", "\u261D\U0001F3FD", defaultOptions, 2},
{"✌🏾 victory skin", "\u270C\U0001F3FE", defaultOptions, 2},
{"✍🏿 writing hand skin", "\u270D\U0001F3FF", defaultOptions, 2},
{"🖐🏻 hand splayed skin", "\U0001F590\U0001F3FB", defaultOptions, 2},
{"⛹🏼 bouncing ball skin", "\u26F9\U0001F3FC", defaultOptions, 2},
{"🏌🏽 golfing skin", "\U0001F3CC\U0001F3FD", defaultOptions, 2},
{"🏋🏾 weights skin", "\U0001F3CB\U0001F3FE", defaultOptions, 2},
```

- [ ] **Step 2: Run tests to verify they fail**

```bash
go test -run TestStringWidth -v 2>&1 | grep -E "FAIL|detective skin|point up|victory|writing|splayed|bouncing ball skin|golfing skin|weights skin"
```

Expected: All 8 new tests FAIL with "got 1, want 2".

---

### Task 4: Implement the fix in `graphemeWidth()`

**Files:**
- Modify: `/home/grant/local_code/displaywidth/width.go`

- [ ] **Step 1: Replace the VS16-only check with a full cluster scan**

Replace the existing VS16 check block in `graphemeWidth()` (lines 174-183):

```go
	// Variation Selector 16 (VS16) requests emoji presentation
	if prop != _Wide && sz > 0 && len(s) >= sz+3 {
		vs := s[sz : sz+3]
		if isVS16(vs) {
			prop = _Wide
		}
		// VS15 (0x8E) requests text presentation but does not affect width,
		// in my reading of Unicode TR51. Falls through to return the base
		// character's property.
	}
```

With:

```go
	// Check remaining bytes in the grapheme cluster for modifiers that
	// indicate emoji presentation (width 2).
	//
	// VS16 (U+FE0F) requests emoji presentation per Unicode TR51.
	// Emoji modifiers (U+1F3FB–U+1F3FF, skin tones) form an
	// emoji_modifier_sequence per UTS#51 ED-13, which is always
	// rendered in emoji presentation.
	//
	// We scan the full cluster because these modifiers may not be
	// immediately adjacent to the base character (e.g., in ZWJ
	// sequences like ⛹🏻‍♂️ where VS16 is at the end).
	if prop != _Wide && sz > 0 && len(s) > sz {
		for i := sz; i < len(s); i++ {
			// VS16: UTF-8 is EF B8 8F
			if i+2 < len(s) && s[i] == 0xEF && s[i+1] == 0xB8 && s[i+2] == 0x8F {
				prop = _Wide
				break
			}
			// Emoji modifier (skin tone): U+1F3FB–U+1F3FF
			// UTF-8 is F0 9F 8F BB through F0 9F 8F BF
			if i+3 < len(s) && s[i] == 0xF0 && s[i+1] == 0x9F && s[i+2] == 0x8F && s[i+3] >= 0xBB && s[i+3] <= 0xBF {
				prop = _Wide
				break
			}
		}
		// VS15 (U+FE0E) requests text presentation but does not affect width,
		// in my reading of Unicode TR51.
	}
```

- [ ] **Step 2: Run all tests**

```bash
go test ./... -v
```

Expected: All tests pass, including the 12 new tests from Tasks 2 and 3. No regressions on existing tests.

- [ ] **Step 3: Commit**

```bash
git add width.go width_test.go
git commit -m "fix: detect VS16 and skin-tone modifiers anywhere in grapheme cluster

The VS16 check previously only examined the 3 bytes immediately after
the first rune (s[sz:sz+3]). In complex emoji sequences like
⛹🏻‍♂️ (person bouncing ball + skin tone + ZWJ + gender + VS16),
VS16 appears much later in the cluster and was missed, returning
width 1 instead of 2.

Similarly, skin-tone modifiers (U+1F3FB–U+1F3FF) on text-default
Extended_Pictographic bases like 🕵🏻 (detective + skin tone) were
not detected at all, also returning width 1.

Now scans the full grapheme cluster for both VS16 and skin-tone
modifiers, promoting to width 2 when found."
```

---

### Task 5: Wire the local fork into slack-tui

**Files:**
- Modify: `/home/grant/local_code/slack-tui/go.mod`

- [ ] **Step 1: Add a replace directive**

Add to go.mod:

```
replace github.com/clipperhouse/displaywidth => /home/grant/local_code/displaywidth
```

- [ ] **Step 2: Run go mod tidy**

```bash
cd /home/grant/local_code/slack-tui
go mod tidy
```

- [ ] **Step 3: Build the project to verify compilation**

```bash
go build ./...
```

Expected: Clean build, no errors.

- [ ] **Step 4: Run the audit test to verify improvements**

```bash
cd /tmp/emoji_width_test
# Update go.mod to also use the local fork
# Then re-run the audit
go run audit.go 2>&1 | head -5
```

Verify that the "Total disagreements" count has decreased and the skin-tone / ZWJ sequences now return width 2.

---

### Task 6: Push fork and prepare PR

- [ ] **Step 1: Fork on GitHub**

```bash
cd /home/grant/local_code/displaywidth
gh repo fork clipperhouse/displaywidth --remote
git push -u fork fix-emoji-modifier-width
```

- [ ] **Step 2: Create PR**

```bash
gh pr create --repo clipperhouse/displaywidth \
  --title "fix: detect VS16 and skin-tone modifiers anywhere in grapheme cluster" \
  --body "..."
```

- [ ] **Step 3: Update slack-tui replace directive to point to fork**

Replace the local path in go.mod with the GitHub fork URL:

```
replace github.com/clipperhouse/displaywidth => github.com/youruser/displaywidth v0.0.0-...
```

Or keep the local path during development and switch when the PR is merged.

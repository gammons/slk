package ui

import (
	"strings"
	"testing"

	tea "charm.land/bubbletea/v2"
	"golang.design/x/clipboard"

	"github.com/gammons/slk/internal/ui/compose"
	"github.com/gammons/slk/internal/ui/mentionpicker"
	"github.com/gammons/slk/internal/ui/messages"
	"github.com/gammons/slk/internal/ui/sidebar"
)

// ---------------------------------------------------------------------
// handleInsertMode (mode_insert.go:40)
//
// Structurally unlike every other mode handler in this package: not a
// switch table but a deep if/else nest with only three key.Matches
// calls, and it MIRRORS almost every branch between a.compose and
// a.threadCompose. Two different predicates pick the side:
//
//	editing-active Esc  -> a.editing.Panel() == PanelThread
//	everything else     -> a.focusedPanel == PanelThread && a.threadVisible
//
// Those are not the same condition, and the rows below pin the
// difference: an edit hosted in the thread panel routes to threadCompose
// even while the channel pane holds focus, and a thread-focused App with
// the panel hidden routes to the CHANNEL compose.
//
// Rows come in compose/threadCompose pairs wherever the handler does.
// Where a pair could be satisfied by a handler that always picked one
// side, the row opens a picker (or seeds text) in BOTH composes and
// asserts the untouched one really was untouched.
//
// The two six-row Esc blocks (edit-active and plain) are deliberately
// NOT collapsed into an (open, isActive) picker table, even though they
// rhyme. The rhyme is shallow: the two blocks differ in setup (beginEdit
// or not), in wantMode, in the post-condition asserted
// (editing.IsActive() vs. the mode), and three of the twelve rows carry
// extra assertions the other nine do not -- the picker-in-both-composes
// pins at :325 and :480, and the threadVisible-conjunct pin at :533.
// A table would have to parameterise all four axes, which is AGENTS.md's
// "forcing genuinely different behavior into a common shape". For
// characterization rows, whose job is to be readable against the source
// during review, the explicit form is the cheaper one to check.
// ---------------------------------------------------------------------

// insertOpts is the shared bundle. An active channel is required by the
// send path (SendMessageMsg carries it) and by typingOut.
func insertOpts() []testOpt {
	return []testOpt{
		withChannels(
			sidebar.ChannelItem{ID: "C1", Name: "general", Type: "channel"},
			sidebar.ChannelItem{ID: "C2", Name: "random", Type: "channel"},
		),
		withMessages(testMessageItems(3)...),
		withActiveChannel("C1"),
	}
}

// typeInto drives runes through a compose model's OWN Update, so the
// text and any picker state are established without the mode handler
// seeing those keystrokes.
//
// Focus first: an unfocused textarea swallows printable input (the trap
// TestKeyPress_CarriesText documents), which would leave every picker
// helper below silently inert.
func typeInto(t *testing.T, c *compose.Model, s string) {
	t.Helper()
	_ = c.Focus()
	for _, r := range s {
		var cmd tea.Cmd
		*c, cmd = c.Update(tea.KeyPressMsg{Code: r, Text: string(r)})
		_ = cmd
	}
	if got := c.Value(); got != s {
		t.Fatalf("precondition: compose value = %q, want %q (is the textarea focused?)", got, s)
	}
}

// openEmojiPicker types ":sm" -- the trigger colon at a word boundary
// plus the two query characters the picker requires before it opens
// (compose/model.go:1100-1108).
func openEmojiPicker(t *testing.T, c *compose.Model) {
	t.Helper()
	typeInto(t, c, ":sm")
	if !c.IsEmojiActive() {
		t.Fatal("precondition: emoji picker did not open")
	}
}

// openMentionPicker types "@" at the start of the buffer, the only
// position besides after a space/newline that triggers it.
func openMentionPicker(t *testing.T, c *compose.Model) {
	t.Helper()
	typeInto(t, c, "@")
	if !c.IsMentionActive() {
		t.Fatal("precondition: mention picker did not open")
	}
}

// openChannelPicker types "#", the channel picker's trigger.
func openChannelPicker(t *testing.T, c *compose.Model) {
	t.Helper()
	typeInto(t, c, "#")
	if !c.IsChannelActive() {
		t.Fatal("precondition: channel picker did not open")
	}
}

// mustMention asserts the mention picker is open on the given compose.
// Separate from openMentionPicker because these rows type a query after
// the trigger and need the assertion at the end, not in the middle.
func mustMention(t *testing.T, c *compose.Model) {
	t.Helper()
	if !c.IsMentionActive() {
		t.Fatal("precondition: mention picker is not active")
	}
}

// noPickerActive asserts none of the three compose overlays is open.
// Used on the compose that a mirrored row expects the handler NOT to
// have touched, and on the "esc exits insert mode" rows where an
// accidentally-open picker would swallow the Esc.
func noPickerActive(t *testing.T, name string, c *compose.Model) {
	t.Helper()
	if c.IsEmojiActive() || c.IsMentionActive() || c.IsChannelActive() {
		t.Fatalf("precondition: %s has a picker open (emoji=%v mention=%v channel=%v)",
			name, c.IsEmojiActive(), c.IsMentionActive(), c.IsChannelActive())
	}
}

// showThread loads a thread, marks it visible and focuses the panel --
// the state the handler's `focusedPanel == PanelThread && threadVisible`
// predicate is looking for. The thread compose is focused too, since
// every thread-side row types into it.
func showThread(t *testing.T, a *App) {
	t.Helper()
	a.threadPanel.SetThread(
		messages.MessageItem{TS: "10.0", UserID: "U1", UserName: "alice", Text: "parent", ThreadTS: "10.0"},
		[]messages.MessageItem{{TS: "11.0", UserID: "U1", UserName: "alice", Text: "reply-1"}},
		"C1", "10.0")
	a.threadVisible = true
	a.focusedPanel = PanelThread
	_ = a.threadCompose.Focus()
	if a.threadPanel.ChannelID() != "C1" || a.threadPanel.ThreadTS() != "10.0" {
		t.Fatalf("precondition: thread panel = (%q, %q), want (C1, 10.0)",
			a.threadPanel.ChannelID(), a.threadPanel.ThreadTS())
	}
}

// beginEdit records an in-progress edit and asserts it took. The
// handler's editing-Esc branch is gated on IsActive(), so a silently
// inactive controller would send every one of those rows down the
// plain-Esc path instead -- where they would still pass their mode
// assertion.
func beginEdit(a *App, panel Panel, stashed string) func(*testing.T, *App) {
	return func(t *testing.T, a2 *App) {
		t.Helper()
		a2.editing.Begin("C1", "2.0", panel, stashed)
		if !a2.editing.IsActive() {
			t.Fatal("precondition: edit controller is not active")
		}
		if a2.editing.Panel() != panel {
			t.Fatalf("precondition: editing panel = %v, want %v", a2.editing.Panel(), panel)
		}
	}
}

// firstBatchCmd runs the first command of a tea.Batch. uploadToastCmd
// batches a status-bar setter with a 2s tea.Tick; running the whole
// batch would sleep for the tick. The slice order is deterministic --
// tea.Batch's compactCmds appends in argument order
// (bubbletea/commands.go:36-46) -- even though the RUNTIME executes the
// members concurrently, which is what the "no ordering guarantees"
// comment on BatchMsg refers to.
func firstBatchCmd(t *testing.T, cmd tea.Cmd) {
	t.Helper()
	if cmd == nil {
		t.Fatal("cmd = nil, want a toast batch")
	}
	batch, ok := cmd().(tea.BatchMsg)
	if !ok {
		t.Fatalf("cmd() = %T, want tea.BatchMsg", cmd())
	}
	if len(batch) != 2 {
		t.Fatalf("batch has %d cmds, want 2 (setter + tick)", len(batch))
	}
	batch[0]()
}

// afterKeyValue feeds one more printable key straight into a compose and
// returns the resulting text. It is how the arrow-key rows observe the
// CURSOR, which compose exposes no getter for: a cursor parked at the
// start prefixes the new character, one left at the end appends it.
func afterKeyValue(c *compose.Model, r rune) string {
	*c, _ = c.Update(tea.KeyPressMsg{Code: r, Text: string(r)})
	return c.Value()
}

func TestInsertModeKeys(t *testing.T) {
	runKeyCases(t, ModeInsert, []keyCase{
		// =============================================================
		// Esc with an upload in flight (mode_insert.go:41)
		// =============================================================
		{
			// Esc must not cancel an in-flight upload, so this arm
			// short-circuits ahead of every other Esc branch --
			// including the plain exit. Staying in ModeInsert is the
			// observable that separates it from all of them.
			name:  "esc while the channel compose is uploading toasts and stays in insert mode",
			opts:  insertOpts(),
			setup: func(t *testing.T, a *App) { a.compose.SetUploading(true) },
			key:   keyCode(tea.KeyEscape),
			// The upload arm returns before SetMode.
			wantMode: ModeInsert,
			assert: func(t *testing.T, a *App, cmd tea.Cmd) {
				firstBatchCmd(t, cmd)
				if got := statusbarText(a); !strings.Contains(got, "Upload in progress") {
					t.Errorf("status bar = %q, want it to contain %q", got, "Upload in progress")
				}
				if !a.compose.Uploading() {
					t.Error("esc cleared the uploading flag; it must not cancel the upload")
				}
			},
		},
		{
			// The guard is an OR across BOTH composes, so a thread
			// upload blocks Esc even while the channel pane has focus
			// and the thread panel is hidden. Without the second
			// disjunct this row would exit to ModeNormal.
			name: "esc while the THREAD compose is uploading toasts even with the channel pane focused",
			opts: insertOpts(),
			setup: func(t *testing.T, a *App) {
				a.threadCompose.SetUploading(true)
				a.focusedPanel = PanelMessages
				if a.compose.Uploading() {
					t.Fatal("precondition: the channel compose should not be uploading")
				}
			},
			key:      keyCode(tea.KeyEscape),
			wantMode: ModeInsert,
			assert: func(t *testing.T, a *App, cmd tea.Cmd) {
				firstBatchCmd(t, cmd)
				if got := statusbarText(a); !strings.Contains(got, "Upload in progress") {
					t.Errorf("status bar = %q, want it to contain %q", got, "Upload in progress")
				}
			},
		},

		// =============================================================
		// Esc with an active edit (mode_insert.go:44-76)
		// Picker first, edit second.
		// =============================================================
		{
			name: "esc with an emoji picker open closes the picker, not the edit",
			opts: insertOpts(),
			setup: func(t *testing.T, a *App) {
				beginEdit(a, PanelMessages, "stashed draft")(t, a)
				openEmojiPicker(t, &a.compose)
			},
			key:      keyCode(tea.KeyEscape),
			wantMode: ModeInsert,
			assert: func(t *testing.T, a *App, cmd tea.Cmd) {
				if a.compose.IsEmojiActive() {
					t.Error("emoji picker still active after esc")
				}
				if !a.editing.IsActive() {
					t.Error("esc cancelled the edit; the first esc should only close the picker")
				}
				if got := a.compose.Value(); got != ":sm" {
					t.Errorf("compose value = %q, want %q untouched", got, ":sm")
				}
				if cmd != nil {
					t.Errorf("cmd = %T, want nil", cmd)
				}
			},
		},
		{
			name: "esc with a mention picker open closes the picker, not the edit",
			opts: insertOpts(),
			setup: func(t *testing.T, a *App) {
				beginEdit(a, PanelMessages, "stashed draft")(t, a)
				openMentionPicker(t, &a.compose)
			},
			key:      keyCode(tea.KeyEscape),
			wantMode: ModeInsert,
			assert: func(t *testing.T, a *App, _ tea.Cmd) {
				if a.compose.IsMentionActive() {
					t.Error("mention picker still active after esc")
				}
				if !a.editing.IsActive() {
					t.Error("esc cancelled the edit; the first esc should only close the picker")
				}
			},
		},
		{
			name: "esc with a channel picker open closes the picker, not the edit",
			opts: insertOpts(),
			setup: func(t *testing.T, a *App) {
				beginEdit(a, PanelMessages, "stashed draft")(t, a)
				openChannelPicker(t, &a.compose)
			},
			key:      keyCode(tea.KeyEscape),
			wantMode: ModeInsert,
			assert: func(t *testing.T, a *App, _ tea.Cmd) {
				if a.compose.IsChannelActive() {
					t.Error("channel picker still active after esc")
				}
				if !a.editing.IsActive() {
					t.Error("esc cancelled the edit; the first esc should only close the picker")
				}
			},
		},
		{
			// The thread mirror, and the row that pins WHICH predicate
			// this branch uses: focus is on the channel pane and the
			// thread panel is hidden, yet the edit is hosted in the
			// thread, so the THREAD compose's picker is the one that
			// closes. A handler keyed on focusedPanel would close the
			// channel compose's picker instead -- which is why one is
			// open there too.
			name: "esc closes the THREAD compose's emoji picker when the edit is hosted there, regardless of focus",
			opts: insertOpts(),
			setup: func(t *testing.T, a *App) {
				beginEdit(a, PanelThread, "stashed draft")(t, a)
				openEmojiPicker(t, &a.threadCompose)
				openMentionPicker(t, &a.compose)
				a.focusedPanel = PanelMessages
				a.threadVisible = false
			},
			key:      keyCode(tea.KeyEscape),
			wantMode: ModeInsert,
			assert: func(t *testing.T, a *App, _ tea.Cmd) {
				if a.threadCompose.IsEmojiActive() {
					t.Error("thread emoji picker still active after esc")
				}
				if !a.compose.IsMentionActive() {
					t.Error("the CHANNEL compose's picker was closed; the edit is hosted in the thread")
				}
				if !a.editing.IsActive() {
					t.Error("esc cancelled the edit; the first esc should only close the picker")
				}
			},
		},
		{
			name: "esc closes the THREAD compose's mention picker when the edit is hosted there",
			opts: insertOpts(),
			setup: func(t *testing.T, a *App) {
				beginEdit(a, PanelThread, "stashed draft")(t, a)
				openMentionPicker(t, &a.threadCompose)
				a.focusedPanel = PanelMessages
			},
			key:      keyCode(tea.KeyEscape),
			wantMode: ModeInsert,
			assert: func(t *testing.T, a *App, _ tea.Cmd) {
				if a.threadCompose.IsMentionActive() {
					t.Error("thread mention picker still active after esc")
				}
				if !a.editing.IsActive() {
					t.Error("esc cancelled the edit")
				}
			},
		},
		{
			name: "esc closes the THREAD compose's channel picker when the edit is hosted there",
			opts: insertOpts(),
			setup: func(t *testing.T, a *App) {
				beginEdit(a, PanelThread, "stashed draft")(t, a)
				openChannelPicker(t, &a.threadCompose)
				a.focusedPanel = PanelMessages
			},
			key:      keyCode(tea.KeyEscape),
			wantMode: ModeInsert,
			assert: func(t *testing.T, a *App, _ tea.Cmd) {
				if a.threadCompose.IsChannelActive() {
					t.Error("thread channel picker still active after esc")
				}
				if !a.editing.IsActive() {
					t.Error("esc cancelled the edit")
				}
			},
		},
		{
			name: "esc with no picker open cancels a channel-pane edit and restores the stashed draft",
			opts: insertOpts(),
			setup: func(t *testing.T, a *App) {
				beginEdit(a, PanelMessages, "stashed draft")(t, a)
				typeInto(t, &a.compose, "edited text")
				noPickerActive(t, "compose", &a.compose)
			},
			key:      keyCode(tea.KeyEscape),
			wantMode: ModeNormal,
			assert: func(t *testing.T, a *App, cmd tea.Cmd) {
				if a.editing.IsActive() {
					t.Error("edit still active after esc")
				}
				if got := a.compose.Value(); got != "stashed draft" {
					t.Errorf("compose value = %q, want the stashed draft restored", got)
				}
				if cmd != nil {
					t.Errorf("cmd = %T, want nil", cmd)
				}
			},
		},
		{
			// cancelEdit's own panel switch (app.go:2936-2942) decides
			// which compose gets the stashed draft. Seeding text in both
			// keeps a mixed-up restore visible.
			name: "esc with no picker open cancels a thread-pane edit and restores the stashed draft there",
			opts: insertOpts(),
			setup: func(t *testing.T, a *App) {
				beginEdit(a, PanelThread, "stashed reply")(t, a)
				typeInto(t, &a.threadCompose, "edited reply")
				typeInto(t, &a.compose, "channel draft")
				noPickerActive(t, "threadCompose", &a.threadCompose)
			},
			key:      keyCode(tea.KeyEscape),
			wantMode: ModeNormal,
			assert: func(t *testing.T, a *App, _ tea.Cmd) {
				if a.editing.IsActive() {
					t.Error("edit still active after esc")
				}
				if got := a.threadCompose.Value(); got != "stashed reply" {
					t.Errorf("thread compose value = %q, want the stashed draft restored", got)
				}
				if got := a.compose.Value(); got != "channel draft" {
					t.Errorf("channel compose value = %q, want it untouched by a thread-pane edit cancel", got)
				}
			},
		},

		// =============================================================
		// Plain Esc (mode_insert.go:77-110)
		// =============================================================
		{
			name:     "esc with an emoji picker open closes the picker and stays in insert mode",
			opts:     insertOpts(),
			setup:    func(t *testing.T, a *App) { openEmojiPicker(t, &a.compose) },
			key:      keyCode(tea.KeyEscape),
			wantMode: ModeInsert,
			assert: func(t *testing.T, a *App, _ tea.Cmd) {
				if a.compose.IsEmojiActive() {
					t.Error("emoji picker still active after esc")
				}
				if got := a.compose.Value(); got != ":sm" {
					t.Errorf("compose value = %q, want %q untouched", got, ":sm")
				}
			},
		},
		{
			name:     "esc with a mention picker open closes the picker and stays in insert mode",
			opts:     insertOpts(),
			setup:    func(t *testing.T, a *App) { openMentionPicker(t, &a.compose) },
			key:      keyCode(tea.KeyEscape),
			wantMode: ModeInsert,
			assert: func(t *testing.T, a *App, _ tea.Cmd) {
				if a.compose.IsMentionActive() {
					t.Error("mention picker still active after esc")
				}
			},
		},
		{
			name:     "esc with a channel picker open closes the picker and stays in insert mode",
			opts:     insertOpts(),
			setup:    func(t *testing.T, a *App) { openChannelPicker(t, &a.compose) },
			key:      keyCode(tea.KeyEscape),
			wantMode: ModeInsert,
			assert: func(t *testing.T, a *App, _ tea.Cmd) {
				if a.compose.IsChannelActive() {
					t.Error("channel picker still active after esc")
				}
			},
		},
		{
			// Thread mirror. Both composes hold a picker so a handler
			// that always took the channel side fails here.
			name: "esc with the thread panel focused closes the THREAD compose's emoji picker",
			opts: insertOpts(),
			setup: func(t *testing.T, a *App) {
				showThread(t, a)
				openEmojiPicker(t, &a.threadCompose)
				openMentionPicker(t, &a.compose)
			},
			key:      keyCode(tea.KeyEscape),
			wantMode: ModeInsert,
			assert: func(t *testing.T, a *App, _ tea.Cmd) {
				if a.threadCompose.IsEmojiActive() {
					t.Error("thread emoji picker still active after esc")
				}
				if !a.compose.IsMentionActive() {
					t.Error("the channel compose's picker was closed while the thread panel was focused")
				}
			},
		},
		{
			name: "esc with the thread panel focused closes the THREAD compose's mention picker",
			opts: insertOpts(),
			setup: func(t *testing.T, a *App) {
				showThread(t, a)
				openMentionPicker(t, &a.threadCompose)
			},
			key:      keyCode(tea.KeyEscape),
			wantMode: ModeInsert,
			assert: func(t *testing.T, a *App, _ tea.Cmd) {
				if a.threadCompose.IsMentionActive() {
					t.Error("thread mention picker still active after esc")
				}
			},
		},
		{
			name: "esc with the thread panel focused closes the THREAD compose's channel picker",
			opts: insertOpts(),
			setup: func(t *testing.T, a *App) {
				showThread(t, a)
				openChannelPicker(t, &a.threadCompose)
			},
			key:      keyCode(tea.KeyEscape),
			wantMode: ModeInsert,
			assert: func(t *testing.T, a *App, _ tea.Cmd) {
				if a.threadCompose.IsChannelActive() {
					t.Error("thread channel picker still active after esc")
				}
			},
		},
		{
			// Pins the `&& a.threadVisible` conjunct. Focus says thread,
			// visibility says no, so the CHANNEL compose owns the key.
			// Dropping the conjunct closes the thread picker instead and
			// leaves the channel one open.
			name: "esc with the thread panel focused but hidden closes the CHANNEL compose's picker",
			opts: insertOpts(),
			setup: func(t *testing.T, a *App) {
				openEmojiPicker(t, &a.compose)
				openMentionPicker(t, &a.threadCompose)
				a.focusedPanel = PanelThread
				a.threadVisible = false
			},
			key:      keyCode(tea.KeyEscape),
			wantMode: ModeInsert,
			assert: func(t *testing.T, a *App, _ tea.Cmd) {
				if a.compose.IsEmojiActive() {
					t.Error("channel emoji picker still active: the threadVisible conjunct was ignored")
				}
				if !a.threadCompose.IsMentionActive() {
					t.Error("the hidden thread compose's picker was closed")
				}
			},
		},
		{
			name: "esc with nothing open leaves insert mode and blurs both composes",
			opts: insertOpts(),
			setup: func(t *testing.T, a *App) {
				typeInto(t, &a.compose, "draft")
				_ = a.threadCompose.Focus()
				noPickerActive(t, "compose", &a.compose)
				noPickerActive(t, "threadCompose", &a.threadCompose)
			},
			key:      keyCode(tea.KeyEscape),
			wantMode: ModeNormal,
			assert: func(t *testing.T, a *App, cmd tea.Cmd) {
				if cmd != nil {
					t.Errorf("cmd = %T, want nil", cmd)
				}
				// Blur has no getter; a blurred textarea ignores
				// printable input, which is the observable.
				if got := afterKeyValue(&a.compose, 'X'); got != "draft" {
					t.Errorf("compose value = %q after a post-esc keystroke, want %q: it was not blurred", got, "draft")
				}
				if got := afterKeyValue(&a.threadCompose, 'X'); got != "" {
					t.Errorf("thread compose value = %q after a post-esc keystroke, want empty: it was not blurred", got)
				}
				// The draft itself survives leaving insert mode.
				if got := a.compose.Value(); got != "draft" {
					t.Errorf("compose value = %q, want the draft preserved", got)
				}
			},
		},

		// =============================================================
		// Ctrl+V -> smartPaste (mode_insert.go:114-117)
		// =============================================================
		{
			name: "ctrl+v pastes clipboard text into the channel compose",
			opts: insertOpts(),
			setup: func(t *testing.T, a *App) {
				typeInto(t, &a.compose, "draft ")
				a.clipboardAvailable = true
				a.SetClipboardReader(func(f clipboard.Format) []byte {
					if f == clipboard.FmtText {
						return []byte("pasted")
					}
					return nil
				})
			},
			key:      keyMod('v', tea.ModCtrl),
			wantMode: ModeInsert,
			assert: func(t *testing.T, a *App, _ tea.Cmd) {
				if got := a.compose.Value(); got != "draft pasted" {
					t.Errorf("compose value = %q, want %q", got, "draft pasted")
				}
			},
		},
		{
			// The paste target follows the same focus+visibility
			// predicate. Seeding both composes keeps a wrong target
			// visible.
			name: "ctrl+v pastes into the THREAD compose when the thread panel is focused and visible",
			opts: insertOpts(),
			setup: func(t *testing.T, a *App) {
				showThread(t, a)
				typeInto(t, &a.threadCompose, "reply ")
				typeInto(t, &a.compose, "draft ")
				a.clipboardAvailable = true
				a.SetClipboardReader(func(f clipboard.Format) []byte {
					if f == clipboard.FmtText {
						return []byte("pasted")
					}
					return nil
				})
			},
			key:      keyMod('v', tea.ModCtrl),
			wantMode: ModeInsert,
			assert: func(t *testing.T, a *App, _ tea.Cmd) {
				if got := a.threadCompose.Value(); got != "reply pasted" {
					t.Errorf("thread compose value = %q, want %q", got, "reply pasted")
				}
				if got := a.compose.Value(); got != "draft " {
					t.Errorf("channel compose value = %q, want %q untouched", got, "draft ")
				}
			},
		},
		{
			// clipboardAvailable is false on a freshly built App, so
			// smartPaste bails at its first line. The row exists to pin
			// that ctrl+v is still CONSUMED by this arm -- it must not
			// fall through and type a literal "v" into the compose.
			name: "ctrl+v with no clipboard is consumed, not typed",
			opts: insertOpts(),
			setup: func(t *testing.T, a *App) {
				typeInto(t, &a.compose, "draft")
				if a.clipboardAvailable {
					t.Fatal("precondition: clipboard should be unavailable on a fresh App")
				}
			},
			key:      keyMod('v', tea.ModCtrl),
			wantMode: ModeInsert,
			assert: func(t *testing.T, a *App, cmd tea.Cmd) {
				if got := a.compose.Value(); got != "draft" {
					t.Errorf("compose value = %q, want %q unchanged", got, "draft")
				}
				if cmd != nil {
					t.Errorf("cmd = %T, want nil", cmd)
				}
			},
		},

		// =============================================================
		// Ctrl+U -> clear the active compose (mode_insert.go:127-130)
		// =============================================================
		{
			name: "ctrl+u clears the channel compose",
			opts: insertOpts(),
			setup: func(t *testing.T, a *App) {
				typeInto(t, &a.compose, "draft")
				typeInto(t, &a.threadCompose, "reply")
			},
			key:      keyMod('u', tea.ModCtrl),
			wantMode: ModeInsert,
			assert: func(t *testing.T, a *App, cmd tea.Cmd) {
				if got := a.compose.Value(); got != "" {
					t.Errorf("compose value = %q, want empty", got)
				}
				if got := a.threadCompose.Value(); got != "reply" {
					t.Errorf("thread compose value = %q, want %q untouched", got, "reply")
				}
				if cmd != nil {
					t.Errorf("cmd = %T, want nil", cmd)
				}
			},
		},
		{
			name: "ctrl+u clears the THREAD compose when the thread panel is focused and visible",
			opts: insertOpts(),
			setup: func(t *testing.T, a *App) {
				showThread(t, a)
				typeInto(t, &a.threadCompose, "reply")
				typeInto(t, &a.compose, "draft")
			},
			key:      keyMod('u', tea.ModCtrl),
			wantMode: ModeInsert,
			assert: func(t *testing.T, a *App, _ tea.Cmd) {
				if got := a.threadCompose.Value(); got != "" {
					t.Errorf("thread compose value = %q, want empty", got)
				}
				if got := a.compose.Value(); got != "draft" {
					t.Errorf("channel compose value = %q, want %q untouched", got, "draft")
				}
			},
		},

		// =============================================================
		// Up / Down: jump-to-edge vs picker navigation
		// (mode_insert.go:136-146)
		// =============================================================
		{
			// Control for the next row. No picker, cursor on the first
			// (only) line, so Up jumps to the start of the textarea --
			// observable because the next character lands in FRONT.
			name:  "up on the first line with no picker jumps to the start of the textarea",
			opts:  insertOpts(),
			setup: func(t *testing.T, a *App) { typeInto(t, &a.compose, "abc") },
			key:   keyCode(tea.KeyUp),
			// The jump arm returns before anything reaches compose.
			wantMode: ModeInsert,
			assert: func(t *testing.T, a *App, cmd tea.Cmd) {
				if cmd != nil {
					t.Errorf("cmd = %T, want nil", cmd)
				}
				if got := afterKeyValue(&a.compose, 'X'); got != "Xabc" {
					t.Errorf("value after a follow-up key = %q, want %q (cursor at the start)", got, "Xabc")
				}
			},
		},
		{
			// The picker guard: with a suggestion list up, Up belongs to
			// the picker, not to the jump shortcut. Without the guard
			// the cursor would be dragged to column 0 and the follow-up
			// key would land in front of the trigger.
			name:     "up with a picker active is left to the picker instead of jumping to the start",
			opts:     insertOpts(),
			setup:    func(t *testing.T, a *App) { typeInto(t, &a.compose, "@al"); mustMention(t, &a.compose) },
			key:      keyCode(tea.KeyUp),
			wantMode: ModeInsert,
			assert: func(t *testing.T, a *App, _ tea.Cmd) {
				if !a.compose.IsMentionActive() {
					t.Error("mention picker closed on up; it should have navigated")
				}
				if got := afterKeyValue(&a.compose, 'X'); got != "@alX" {
					t.Errorf("value after a follow-up key = %q, want %q (cursor left at the end)", got, "@alX")
				}
			},
		},
		{
			name:     "down on the last line with no picker jumps to the end of the textarea",
			opts:     insertOpts(),
			setup:    func(t *testing.T, a *App) { typeInto(t, &a.compose, "abc"); a.compose.MoveCursorToStart() },
			key:      keyCode(tea.KeyDown),
			wantMode: ModeInsert,
			assert: func(t *testing.T, a *App, cmd tea.Cmd) {
				if cmd != nil {
					t.Errorf("cmd = %T, want nil", cmd)
				}
				if got := afterKeyValue(&a.compose, 'X'); got != "abcX" {
					t.Errorf("value after a follow-up key = %q, want %q (cursor at the end)", got, "abcX")
				}
			},
		},
		{
			name: "down with a picker active is left to the picker instead of jumping to the end",
			opts: insertOpts(),
			setup: func(t *testing.T, a *App) {
				typeInto(t, &a.compose, "@al")
				mustMention(t, &a.compose)
				a.compose.MoveCursorToStart()
			},
			key:      keyCode(tea.KeyDown),
			wantMode: ModeInsert,
			assert: func(t *testing.T, a *App, _ tea.Cmd) {
				if got := afterKeyValue(&a.compose, 'X'); got != "X@al" {
					t.Errorf("value after a follow-up key = %q, want %q (cursor left at the start)", got, "X@al")
				}
			},
		},

		// =============================================================
		// Picker forwarding: every key goes to the compose, Enter
		// included (mode_insert.go:158-162 and :207-211)
		// =============================================================
		{
			// Enter with a picker up completes the mention instead of
			// sending. A cmd of any kind here would mean a message went
			// out mid-completion.
			name: "enter with a mention picker active completes the mention instead of sending",
			opts: insertOpts(),
			setup: func(t *testing.T, a *App) {
				a.compose.SetUsers([]mentionpicker.User{
					{ID: "U9", DisplayName: "alice", Username: "alice", InChannel: true},
				})
				typeInto(t, &a.compose, "@al")
				mustMention(t, &a.compose)
			},
			key:      keyCode(tea.KeyEnter),
			wantMode: ModeInsert,
			assert: func(t *testing.T, a *App, cmd tea.Cmd) {
				if got := a.compose.Value(); got != "@alice " {
					t.Errorf("compose value = %q, want %q (the picker's completion)", got, "@alice ")
				}
				if a.compose.IsMentionActive() {
					t.Error("mention picker still active after enter")
				}
				if cmd != nil {
					if _, ok := cmd().(SendMessageMsg); ok {
						t.Error("enter sent a message while the mention picker was open")
					}
				}
			},
		},
		{
			// Thread mirror of the same forwarding branch.
			name: "enter with a mention picker active in the THREAD compose completes there, not sends",
			opts: insertOpts(),
			setup: func(t *testing.T, a *App) {
				showThread(t, a)
				a.threadCompose.SetUsers([]mentionpicker.User{
					{ID: "U9", DisplayName: "alice", Username: "alice", InChannel: true},
				})
				typeInto(t, &a.threadCompose, "@al")
				mustMention(t, &a.threadCompose)
			},
			key:      keyCode(tea.KeyEnter),
			wantMode: ModeInsert,
			assert: func(t *testing.T, a *App, cmd tea.Cmd) {
				if got := a.threadCompose.Value(); got != "@alice " {
					t.Errorf("thread compose value = %q, want %q", got, "@alice ")
				}
				if cmd != nil {
					if _, ok := cmd().(SendThreadReplyMsg); ok {
						t.Error("enter sent a thread reply while the mention picker was open")
					}
				}
			},
		},
		{
			name: "a printable key with a picker active in the THREAD compose types there",
			opts: insertOpts(),
			setup: func(t *testing.T, a *App) {
				showThread(t, a)
				typeInto(t, &a.threadCompose, "@al")
				typeInto(t, &a.compose, "draft")
				mustMention(t, &a.threadCompose)
			},
			key:      keyPress('i'),
			wantMode: ModeInsert,
			assert: func(t *testing.T, a *App, _ tea.Cmd) {
				if got := a.threadCompose.Value(); got != "@ali" {
					t.Errorf("thread compose value = %q, want %q", got, "@ali")
				}
				if got := a.compose.Value(); got != "draft" {
					t.Errorf("channel compose value = %q, want %q untouched", got, "draft")
				}
			},
		},

		// =============================================================
		// Newline vs send (mode_insert.go:150-152, :165-169, :213-217)
		// =============================================================
		{
			// Ctrl+J is the fallback for terminals that do not report
			// shift on Enter. It is rewritten to a bare Enter before
			// reaching the textarea, so the compose sees a newline.
			name:     "ctrl+j inserts a newline in the channel compose",
			opts:     insertOpts(),
			setup:    func(t *testing.T, a *App) { typeInto(t, &a.compose, "line1") },
			key:      keyMod('j', tea.ModCtrl),
			wantMode: ModeInsert,
			assert: func(t *testing.T, a *App, _ tea.Cmd) {
				if got := a.compose.Value(); got != "line1\n" {
					t.Errorf("compose value = %q, want %q", got, "line1\n")
				}
			},
		},
		{
			name: "shift+enter inserts a newline in the THREAD compose",
			opts: insertOpts(),
			setup: func(t *testing.T, a *App) {
				showThread(t, a)
				typeInto(t, &a.threadCompose, "line1")
			},
			key:      keyMod(tea.KeyEnter, tea.ModShift),
			wantMode: ModeInsert,
			assert: func(t *testing.T, a *App, _ tea.Cmd) {
				if got := a.threadCompose.Value(); got != "line1\n" {
					t.Errorf("thread compose value = %q, want %q", got, "line1\n")
				}
			},
		},
		{
			name: "ctrl+j inserts a newline in the THREAD compose",
			opts: insertOpts(),
			setup: func(t *testing.T, a *App) {
				showThread(t, a)
				typeInto(t, &a.threadCompose, "line1")
			},
			key:      keyMod('j', tea.ModCtrl),
			wantMode: ModeInsert,
			assert: func(t *testing.T, a *App, _ tea.Cmd) {
				if got := a.threadCompose.Value(); got != "line1\n" {
					t.Errorf("thread compose value = %q, want %q", got, "line1\n")
				}
			},
		},
		{
			// BUG?: alt+enter SENDS. Terminals that report Alt on Enter
			// are common, and the natural reading of "Shift+Enter /
			// Ctrl+J insert a newline" is that other Enter chords do
			// too. They do not. mode_insert.go:150 computes
			//
			//	isSend := code == tea.KeyEnter && !mod.Contains(tea.ModShift)
			//
			// so EVERY modifier that is not Shift leaves isSend true,
			// while :151 admits only ModShift (or ctrl+j) as a newline.
			// Alt+Enter therefore dispatches the message rather than
			// breaking the line -- silently losing a draft the user
			// meant to keep composing.
			//
			// Tracked as https://github.com/gammons/slk/issues/185.
			// WHEN THAT BUG IS FIXED: alt+enter inserts a newline, so
			// this row must be re-characterized — wantMode becomes
			// ModeInsert, cmd becomes nil, and the compose value
			// becomes "hello\n". Do not delete it; re-pin it.
			//
			// Asserted as it behaves today. Nothing was changed.
			name: "alt+enter SENDS the channel message instead of inserting a newline",
			opts: insertOpts(),
			setup: func(t *testing.T, a *App) {
				typeInto(t, &a.compose, "hello")
				noPickerActive(t, "compose", &a.compose)
			},
			key: keyMod(tea.KeyEnter, tea.ModAlt),
			// The send path runs exitInsertAfterSend (app.go:1674),
			// which is itself the first observable separating "sent"
			// from "inserted a newline".
			wantMode: ModeNormal,
			assert: func(t *testing.T, a *App, cmd tea.Cmd) {
				if cmd == nil {
					t.Fatal("cmd = nil, want SendMessageMsg: alt+enter sends today")
				}
				sent, ok := cmd().(SendMessageMsg)
				if !ok {
					t.Fatalf("cmd() = %T, want SendMessageMsg", cmd())
				}
				if sent.ChannelID != "C1" || sent.Text != "hello" {
					t.Errorf("SendMessageMsg = %+v, want {ChannelID:C1 Text:hello}", sent)
				}
				// No "\n" anywhere: the compose was Reset by the send
				// path, not extended by a newline.
				if got := a.compose.Value(); got != "" {
					t.Errorf("compose value = %q, want empty (sent and reset, not a newline)", got)
				}
			},
		},
		{
			// BUG?: the thread mirror of the same defect. isSend and
			// isNewline are computed once at :150-152, ABOVE the panel
			// split, so both composes inherit the classification.
			//
			// Same issue, https://github.com/gammons/slk/issues/185.
			// WHEN THAT BUG IS FIXED: re-pin as ModeInsert / cmd nil /
			// thread compose value "reply\n".
			name: "alt+enter SENDS the thread reply instead of inserting a newline",
			opts: insertOpts(),
			setup: func(t *testing.T, a *App) {
				showThread(t, a)
				typeInto(t, &a.threadCompose, "reply")
				noPickerActive(t, "threadCompose", &a.threadCompose)
			},
			key:      keyMod(tea.KeyEnter, tea.ModAlt),
			wantMode: ModeNormal,
			assert: func(t *testing.T, a *App, cmd tea.Cmd) {
				if cmd == nil {
					t.Fatal("cmd = nil, want SendThreadReplyMsg: alt+enter sends today")
				}
				reply, ok := cmd().(SendThreadReplyMsg)
				if !ok {
					t.Fatalf("cmd() = %T, want SendThreadReplyMsg", cmd())
				}
				if reply.ChannelID != "C1" || reply.ThreadTS != "10.0" || reply.Text != "reply" {
					t.Errorf("SendThreadReplyMsg = %+v, want {ChannelID:C1 ThreadTS:10.0 Text:reply}", reply)
				}
				if got := a.threadCompose.Value(); got != "" {
					t.Errorf("thread compose value = %q, want empty (sent and reset, not a newline)", got)
				}
			},
		},
		{
			// An empty thread compose swallows Enter: no cmd, no mode
			// change, nothing sent.
			name:     "enter on an empty THREAD compose sends nothing and stays in insert mode",
			opts:     insertOpts(),
			setup:    showThread,
			key:      keyCode(tea.KeyEnter),
			wantMode: ModeInsert,
			assert: func(t *testing.T, _ *App, cmd tea.Cmd) {
				if cmd != nil {
					t.Errorf("cmd = %T, want nil", cmd)
				}
			},
		},
		{
			// The thread-side edit submit. editing.Panel() must match
			// PanelThread for this arm; with PanelMessages the handler
			// would fall through and send a NEW reply instead.
			name: "enter while editing a thread reply submits the edit",
			opts: insertOpts(),
			setup: func(t *testing.T, a *App) {
				showThread(t, a)
				beginEdit(a, PanelThread, "")(t, a)
				typeInto(t, &a.threadCompose, "edited reply")
			},
			key:      keyCode(tea.KeyEnter),
			wantMode: ModeInsert,
			assert: func(t *testing.T, a *App, cmd tea.Cmd) {
				if cmd == nil {
					t.Fatal("cmd = nil, want EditMessageMsg")
				}
				edit, ok := cmd().(EditMessageMsg)
				if !ok {
					t.Fatalf("cmd() = %T, want EditMessageMsg", cmd())
				}
				if edit.ChannelID != "C1" || edit.TS != "2.0" || edit.NewText != "edited reply" {
					t.Errorf("EditMessageMsg = %+v, want {ChannelID:C1 TS:2.0 NewText:\"edited reply\"}", edit)
				}
				// The compose keeps its text: edits exit insert mode on
				// MessageEditedMsg, not on submit (app.go:1670-1673).
				if got := a.threadCompose.Value(); got != "edited reply" {
					t.Errorf("thread compose value = %q, want it preserved until the edit lands", got)
				}
			},
		},

		// =============================================================
		// Backspace and Tab (mode_insert.go:198-201, :207-211, :244-247)
		//
		// Neither key has an arm anywhere in mode_insert.go: both reach
		// the same compose.Update statement that printable typing does.
		// That absence IS the characterisation -- what these keys do is
		// entirely the compose model's business, and what the handler
		// contributes is only WHICH compose gets them. Rows exist
		// because the brief named both groups, and because a future
		// refactor that adds an interception here would be invisible
		// otherwise.
		// =============================================================
		{
			name:     "backspace deletes the last character of the channel compose",
			opts:     insertOpts(),
			setup:    func(t *testing.T, a *App) { typeInto(t, &a.compose, "abc") },
			key:      keyCode(tea.KeyBackspace),
			wantMode: ModeInsert,
			assert: func(t *testing.T, a *App, _ tea.Cmd) {
				if got := a.compose.Value(); got != "ab" {
					t.Errorf("compose value = %q, want %q", got, "ab")
				}
			},
		},
		{
			// The mirror. Seeding both composes keeps a wrong target
			// visible: a handler that always edited the channel side
			// would leave "draf" here.
			name: "backspace with the thread panel focused edits the THREAD compose",
			opts: insertOpts(),
			setup: func(t *testing.T, a *App) {
				showThread(t, a)
				typeInto(t, &a.threadCompose, "reply")
				typeInto(t, &a.compose, "draft")
			},
			key:      keyCode(tea.KeyBackspace),
			wantMode: ModeInsert,
			assert: func(t *testing.T, a *App, _ tea.Cmd) {
				if got := a.threadCompose.Value(); got != "repl" {
					t.Errorf("thread compose value = %q, want %q", got, "repl")
				}
				if got := a.compose.Value(); got != "draft" {
					t.Errorf("channel compose value = %q, want %q untouched", got, "draft")
				}
			},
		},
		{
			// Tab is the pickers' SECOND completion key
			// (compose/model.go:557, :650, :1132). With no picker up
			// there is nothing to complete, and the textarea drops it
			// outright: no tab character, no space, and the cursor does
			// not move -- which the follow-up keystroke is what proves.
			name: "tab with no picker active inserts nothing and leaves the cursor at the end",
			opts: insertOpts(),
			setup: func(t *testing.T, a *App) {
				typeInto(t, &a.compose, "abc")
				noPickerActive(t, "compose", &a.compose)
			},
			key:      keyCode(tea.KeyTab),
			wantMode: ModeInsert,
			assert: func(t *testing.T, a *App, cmd tea.Cmd) {
				if got := a.compose.Value(); got != "abc" {
					t.Errorf("compose value = %q, want %q: tab typed something", got, "abc")
				}
				if cmd != nil {
					t.Errorf("cmd = %T, want nil", cmd)
				}
				if got := afterKeyValue(&a.compose, 'X'); got != "abcX" {
					t.Errorf("value after a follow-up key = %q, want %q (cursor still at the end)", got, "abcX")
				}
			},
		},
		{
			// The completion the brief named. Tab only reaches the
			// picker through the forwarding branch at :207-211, so this
			// row fails both if the picker stops handling Tab and if the
			// handler stops forwarding.
			name: "tab with a mention picker active completes the mention, exactly as enter does",
			opts: insertOpts(),
			setup: func(t *testing.T, a *App) {
				a.compose.SetUsers([]mentionpicker.User{
					{ID: "U9", DisplayName: "alice", Username: "alice", InChannel: true},
				})
				typeInto(t, &a.compose, "@al")
				mustMention(t, &a.compose)
			},
			key:      keyCode(tea.KeyTab),
			wantMode: ModeInsert,
			assert: func(t *testing.T, a *App, cmd tea.Cmd) {
				if got := a.compose.Value(); got != "@alice " {
					t.Errorf("compose value = %q, want %q (the picker's completion)", got, "@alice ")
				}
				if a.compose.IsMentionActive() {
					t.Error("mention picker still active after tab")
				}
				if cmd != nil {
					t.Errorf("cmd = %T, want nil", cmd)
				}
			},
		},

		// =============================================================
		// Plain typing fallthrough (mode_insert.go:198-201, :244-247)
		// =============================================================
		{
			name: "a printable key with the thread panel focused types into the THREAD compose",
			opts: insertOpts(),
			setup: func(t *testing.T, a *App) {
				showThread(t, a)
				typeInto(t, &a.compose, "draft")
				noPickerActive(t, "threadCompose", &a.threadCompose)
			},
			key:      keyPress('x'),
			wantMode: ModeInsert,
			assert: func(t *testing.T, a *App, _ tea.Cmd) {
				if got := a.threadCompose.Value(); got != "x" {
					t.Errorf("thread compose value = %q, want %q", got, "x")
				}
				if got := a.compose.Value(); got != "draft" {
					t.Errorf("channel compose value = %q, want %q untouched", got, "draft")
				}
			},
		},
		{
			// The focus predicate again, from the other side: thread
			// focused but hidden routes typing to the CHANNEL compose.
			name: "a printable key with the thread panel focused but hidden types into the CHANNEL compose",
			opts: insertOpts(),
			setup: func(t *testing.T, a *App) {
				_ = a.compose.Focus()
				_ = a.threadCompose.Focus()
				a.focusedPanel = PanelThread
				a.threadVisible = false
			},
			key:      keyPress('x'),
			wantMode: ModeInsert,
			assert: func(t *testing.T, a *App, _ tea.Cmd) {
				if got := a.compose.Value(); got != "x" {
					t.Errorf("channel compose value = %q, want %q", got, "x")
				}
				if got := a.threadCompose.Value(); got != "" {
					t.Errorf("thread compose value = %q, want empty", got)
				}
			},
		},
	})
}

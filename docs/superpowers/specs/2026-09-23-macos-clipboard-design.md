# macOS clipboard copying (issue #242)

## Cause

All copy actions (mouse selection, message yank, and permalink copy) use
`App.clipboardWrite`, whose default is Bubble Tea's OSC 52 command. Terminal.app
does not support OSC 52, so the existing copy toast can appear without changing
the clipboard. Clipboard initialization currently controls reads only.

## Design

Wire the existing `SetClipboardWriter` hook in `cmd/slk` to a host-aware writer:

- Local macOS: run `pbcopy` with the exact text on stdin, inside a Bubble Tea
  command, with a bounded timeout. This works independently of CGO and clipboard
  read initialization, including in Terminal.app and local tmux sessions.
- SSH sessions (any of `SSH_CONNECTION`, `SSH_CLIENT`, or `SSH_TTY` set) and other
  operating systems: retain OSC 52 so copies target the terminal's clipboard,
  not a remote host's pasteboard.
- If `pbcopy` fails, log the failure without the copied text and fall back to
  OSC 52. That fallback still requires terminal support; OSC 52 has no success
  acknowledgement, and existing copy-toast semantics remain unchanged.

Keep subprocess and environment access outside `internal/ui`. Reuse the shared
writer hook rather than modifying three copy call sites or adding a dependency.
No changes to clipboard reads, Linux/Windows backends, or keybindings.

## Validation

Write regression tests for local macOS (including Terminal.app and tmux), each
SSH indicator, Linux/Windows, lazy execution, exact Unicode/multiline payloads,
and native-write failure fallback. Test the subprocess using a fake `pbcopy`
without touching the developer's clipboard. Run existing UI copy tests, then
build, vet, and the race-enabled suite.

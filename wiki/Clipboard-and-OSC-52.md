# Clipboard and OSC 52

For local macOS sessions, slk writes the system clipboard using `pbcopy`.
This supports **Terminal.app**, which does not support OSC 52, as well as other
macOS terminals and local tmux sessions. It applies to mouse-selection copy,
message yank (`y`), and permalink copy (`Y` / `C`), and does not depend on CGO
or clipboard-read initialization.

On other platforms and in SSH sessions, slk uses the OSC 52 escape so that
copies reach the terminal's clipboard rather than the remote host's clipboard.
Most modern terminal emulators (alacritty, kitty, wezterm, foot, iterm2,
recent gnome-terminal) accept these writes by default. A few need explicit
opt-in. The terminal-specific settings below apply to this OSC 52 path.

## Terminal-specific setup

- **tmux:** `set -g set-clipboard on` in your tmux config.
- **screen:** has no working OSC 52 path; consider switching to tmux.
- **kitty (older versions):** `clipboard_control write-clipboard` in
  `kitty.conf`.

## Diagnosing silent failures

If `pbcopy` fails on macOS, slk logs the error and falls back to OSC 52. Run
with `SLK_DEBUG=1` and check `slk-debug.log` for `[clipboard]` errors. The
fallback requires an OSC 52-capable terminal; it cannot work in Terminal.app.

When using OSC 52, `Copied N chars` can show in the status bar even if the
terminal drops the write. There is no reliable round-trip to detect this from
inside slk — the protocol doesn't acknowledge writes. Check your terminal's
clipboard documentation for an opt-in setting. Terminal.app cannot receive
OSC 52 copies over SSH; use an OSC 52-capable terminal for remote sessions.

## Related

- [[Terminal Compatibility|Terminal-Compatibility]] — per-terminal OSC 52 support summary

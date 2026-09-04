# Terminal Compatibility

slk works in any modern terminal, but the experience scales with what your
terminal supports. This table summarizes the important capabilities; pick
something from the top of the list for the richest experience.

| Terminal              | Inline images        | True-pixel avatars | OSC 8 links | OSC 52 clipboard | Notes                                                       |
|-----------------------|----------------------|--------------------|-------------|------------------|-------------------------------------------------------------|
| **kitty**             | kitty graphics       | yes                | yes         | yes              | Best overall experience. Older versions may need `clipboard_control write-clipboard`. |
| **ghostty**           | kitty graphics       | yes                | yes         | yes              | Recommended.                                                |
| **WezTerm** (recent)  | kitty graphics       | yes                | yes         | yes              |                                                             |
| **foot** (Wayland)    | sixel                | half-block         | yes         | yes              | Best Wayland-native option.                                 |
| **iTerm2 ≥ 3.5**      | half-block           | half-block         | yes         | yes              | Implements kitty graphics but not unicode placeholders, so slk falls back to half-block. |
| **Alacritty**         | half-block           | half-block         | yes (≥0.11) | yes              | Fast and reliable, but no inline images.                    |
| **gnome-terminal** (recent) | half-block     | half-block         | yes         | yes              |                                                             |
| **mlterm**            | sixel                | half-block         | partial     | partial          |                                                             |
| **xterm** (`-ti vt340`) | sixel              | half-block         | yes         | yes              | Detected at startup via DA1; plain `xterm` without the sixel-capable emulation is half-block. |
| **other sixel terminals** | sixel            | half-block         | varies      | varies           | DomTerm, toyterm, contour, WezTerm and friends are picked up by the startup DA1 probe. |
| **screen**            | half-block           | half-block         | no          | no               | No working OSC 52 path; consider switching to tmux.         |

## How the image protocol is picked

With `image_protocol = "auto"` (the default) slk checks, in order:

1. kitty graphics, when the terminal announces itself as kitty/ghostty —
   confirmed at startup with a graphics-protocol probe.
2. sixel, for terminals slk recognizes by name (foot, mlterm, WezTerm,
   iTerm2) **or** that answer the startup DA1 query (`CSI c`) with
   attribute `4`. This catches sixel terminals slk has never heard of,
   including xterm built with sixel support, DomTerm and toyterm.
3. half-block (`▀`) otherwise.

If your terminal renders images as chunky half-block mosaics but
`img2sixel` works in it, run slk with `SLK_DEBUG=1` and look for the
`sixel probe:` line in the debug log. Set `image_protocol = "sixel"`
explicitly if the terminal answers DA1 without advertising sixel.

## Inside tmux

Kitty graphics work inside tmux on kitty-capable terminals (kitty, Ghostty,
WezTerm) as long as tmux passthrough is enabled:

```tmux
set -g allow-passthrough on
```

Reload tmux for the setting to take effect (`tmux kill-server`, then
reattach). Verify with `tmux show -gv allow-passthrough` — expected: `on`
(or `all`). If passthrough is off, slk detects this at startup and falls
back to half-block automatically.

Sixel stays off inside tmux by policy — pixel-protocol pass-through inside
tmux is unreliable — regardless of the outer terminal.

OSC 52 clipboard requires `set -g set-clipboard on` in your tmux config.

## Unread indicator in the window title

slk sets the terminal window title to reflect unread state — for example
`slk SW (3) +1` means three channels-with-unreads in the active workspace
and at least one other workspace also has unreads. The two-letter prefix is
the active workspace's initials (matching the left-rail label).

Outside tmux this just works — modern terminals (kitty, WezTerm, Alacritty,
Ghostty, iTerm2, Windows Terminal, gnome-terminal) render title changes in
their tab/window chrome.

Inside tmux there's an extra step. tmux intercepts the title escape from
slk and only re-emits it to the outer terminal when title forwarding is on,
*and* it uses its own title template by default (`#W` = window name) rather
than the pane's title. Add both lines to `~/.tmux.conf`:

```tmux
set -g set-titles on
set -g set-titles-string '#T'
```

`#T` (active pane title) is what carries slk's string. Reload tmux for the
setting to take effect (`tmux kill-server`, then reattach). Verify with:

```bash
tmux show -gv set-titles
tmux show -gv set-titles-string
```

Expected output: `on` and `#T`.

Passing the title escape through tmux's DCS passthrough instead — which
would work with no tmux config at all — is a possible follow-up.

## Overriding the image protocol

You can override slk's image-protocol pick via the `[appearance] image_protocol`
config key (`auto` / `kitty` / `sixel` / `halfblock` / `off`). See
[[Configuration]] for details.

## Keybindings and terminal quirks

- `Ctrl+O` in insert mode is the primary binding to toggle "also send to channel" for thread replies. Some terminals or outer shell configurations intercept `Ctrl+O` (readline's `operate-and-get-next`). In slk's raw terminal mode this is delivered cleanly, but `Alt+Enter` is also available as a one-shot send-with-broadcast without needing the toggle. Note that on macOS, Terminal.app and default iTerm2 profiles do not send Meta for the Option key by default; `Alt+Enter` requires enabling "Use Option as Meta key" (or Esc+) in your terminal preferences.

## Related

- [[Clipboard and OSC 52|Clipboard-and-OSC-52]] — getting copy/paste to land
- [[Tradeoffs and Non-Goals|Tradeoffs-and-Non-Goals]] — image rendering caveats (animated GIFs, unfurls, threads pane sixel)

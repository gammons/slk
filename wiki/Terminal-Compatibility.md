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

## Focus reporting and read state

slk marks a message read on Slack's servers when it arrives in the
channel you're viewing — or in the thread panel you have open — but only
while the terminal running slk is focused, and inside tmux only once
focus reporting has proven itself (see below). Having a channel selected
doesn't mean you can see it: slk may be sitting in a background terminal,
an inactive tmux window, or an unfocused tmux pane. Marking those
messages read would diverge from Slack's own read state and suppress the
mobile notification for a message you never saw. A message that arrives
while you're away stays unread until you come back; the mark goes out on
the next focus.

Focus is detected with DECSET 1004 focus reporting, which recent versions
of the terminals at the top of the table above implement (kitty, Ghostty,
WezTerm, foot, iTerm2, Alacritty, gnome-terminal).

Inside tmux there's an extra step. tmux forwards focus events to the
programs it runs only if you ask it to. Add this to `~/.tmux.conf`:

```tmux
set -g focus-events on
```

Without that line tmux never tells slk that its pane lost focus, and slk
has no way to ask. So inside tmux slk holds auto-marking back until it
has actually seen a focus event — a gain or a loss, either one proves the
wiring works. With `focus-events on` the first switch away from or back
to slk's pane arms it, and from then on messages arriving in the channel
you're viewing are marked read while the pane is focused. Without it no
event ever arrives, auto-marking stays disarmed for the whole session,
and slk falls back to marking a channel read when you open it — exactly
what it did before this feature existed.

slk detects tmux from the `$TMUX` environment variable, read once at
startup. It does not inspect your tmux config, so with `focus-events`
left off you get that fallback silently rather than a warning.

Outside tmux there is no such gate. Terminals report focus *transitions*,
never their current state, so slk assumes it starts focused — you did
just launch it — and marks read on arrival from the very first message.
slk cannot probe for focus-reporting support either, so a terminal that
never sends focus events is indistinguishable from one that never loses
focus, and there the assumed-focused state persists for the whole
session. That risk is accepted outside tmux, where focus reporting is
widely supported; inside tmux, where the stock config breaks it for
everyone, it is not.

## Overriding the image protocol

You can override slk's image-protocol pick via the `[appearance] image_protocol`
config key (`auto` / `kitty` / `sixel` / `halfblock` / `off`). See
[[Configuration]] for details.

## Related

- [[Clipboard and OSC 52|Clipboard-and-OSC-52]] — getting copy/paste to land
- [[Tradeoffs and Non-Goals|Tradeoffs-and-Non-Goals]] — image rendering caveats (animated GIFs, unfurls, threads pane sixel)

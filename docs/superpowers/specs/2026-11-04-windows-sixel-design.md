# Sixel images on Windows Terminal — design

## Problem

Windows Terminal draws sixel graphics from version 1.22, but slk on Windows always used the half-block mosaic. Two things were missing on Windows:

- **The startup capability query never ran.** slk decides on sixel by sending DA1 (`CSI c`) and looking for attribute `4` in the reply (`ProbeSixel`). Reading that reply with a timeout was implemented with poll(2) only; on Windows `pollProbe` was a stub that always reported failure.
- **The cell size was a guess.** Sixel images are encoded in pixels for a number of cells, so slk needs a cell's pixel size. It reads it with `TIOCGWINSZ`, which Windows does not have, so Windows always used 8×16. Windows Terminal's default cell is 10×20, so images would be drawn a fifth too small for the rows reserved for them.

Tested on a Windows Terminal (PowerShell 5.1 reading console key events): `CSI c` answered `CSI ? 61;4;6;7;14;21;22;23;24;28;32;42;52 c` (sixel), `CSI 14 t` answered `CSI 4;860;1560 t` and `CSI 16 t` answered `CSI 6;20;10 t`.

## Design

- `internal/image/probe_windows.go`: `pollProbe` for a Windows console. It waits on the input handle with `WaitForSingleObject`, then reads what is queued with `ReadConsoleInputW` and decodes the key-down events' characters (`appendConsoleChars`, platform-neutral, in `consoleinput.go`). `ReadFile` is not used because a focus or mouse event signals the handle without giving it anything to return, and a read left blocked past the deadline would take the user's first keystroke from bubbletea. With this, the existing DA1 probe works on Windows unchanged, and picks sixel on any Windows terminal that advertises it.
- `ProbeCellSize` sends `CSI 16 t` and parses `CSI 6 ; H ; W t`. `startupCellPixels` (cmd/slk) uses `$COLORTERM_CELL_WIDTH/HEIGHT` or `TIOCGWINSZ` when they know (`MeasureCellPixels`), else asks the terminal, else 8×16. It asks only when images are drawn in pixels (sixel or kitty), on a terminal, outside tmux and zellij, so a unix terminal whose ioctl reports no pixels benefits too.

## Not in scope

- Re-reading the cell size after a font or zoom change: it is read once at startup, as before.
- Sixel inside tmux, which stays off by policy.

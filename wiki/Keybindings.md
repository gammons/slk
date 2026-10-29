# Keybindings

| Key | Mode | Action |
|---|---|---|
| `j` / `k` | Normal | Move down/up in channel list or messages |
| `h` / `l` | Normal | Switch focus between panels |
| `Tab` / `Shift+Tab` | Normal | Cycle focus |
| `Enter` | Normal (sidebar) | Open selected channel, or toggle a section header |
| `Space` | Normal (sidebar) | Toggle the selected section header (collapse/expand) |
| `Enter` | Normal (message) | Open thread |
| `i` | Normal | Enter insert mode |
| `Esc` | Insert / Command | Return to normal mode |
| `:` | Normal | Command prompt (`:ws`, `:marks`, `:sp`, … — see [[Commands]]) |
| `?` / `:help` | Normal | Show every keybinding and `:` command; `/` filters the list |
| `:version` | Normal | Show the running build's version and commit (`slk --version` also prints the build date) |
| `Tab` / `Shift+Tab` | Command | Complete the command name; press again to cycle through the matches, which the status bar lists beside the prompt |
| `Enter` | Insert | Send message (`[compose] stay_in_insert_after_send = true` keeps insert mode for the next one, except after sending an attachment, which returns to normal mode while it uploads) |
| `Ctrl+P`, `Ctrl+N`, `Ctrl+A`, … | Insert (empty composer) | With `[compose] stay_in_insert_after_send` on and no text, no attachment and no edit in progress, the app's Ctrl shortcuts leave insert mode and act as in normal mode; otherwise they edit the text as before. `Ctrl+W`, `Ctrl+H` and `Ctrl+K` always edit the text, so repeating them to delete never changes mode |
| `Shift+Enter` | Insert | Newline |
| `Ctrl+V` | Insert | Smart paste — image / file path / text (use `Ctrl+V`, not the terminal's `Ctrl+Shift+V`) |
| `Ctrl+U` | Insert | Clear compose (text + pending attachments) |
| `Ctrl+E` | Insert | Edit the draft in `$VISUAL` / `$EDITOR` (or `compose.editor`) |
| `Ctrl+O` | Insert (thread) | Toggle "also send to channel" for the next thread reply |
| `Alt+Enter` | Insert (thread) | Send thread reply and broadcast to channel (one-shot) |
| `Ctrl+U` / `Ctrl+D` | Normal | Half-page up / down |
| `Up` | Insert | Previous line; on the first line, jump to start of message |
| `Down` | Insert | Next line; on the last line, jump to end of message |
| `gg` / `G` | Normal | Jump to top / bottom |
| `/` | Normal | Search in channel (vim-style; searches cached history of the current channel) |
| `n` / `N` | Normal | Next / previous search match (wraps) |
| `a` / `A` | Normal | Jump to next / previous unread channel (wraps) |
| `Ctrl+h` / `Ctrl+l` | Normal | Navigate back / forward through visited channels, returning to the message you were reading when you left (`Ctrl+k` also works) |
| `Esc` | Normal (search active) | Clear active search |
| `m{a-z}` | Normal (message) | Set a session mark on the selected message — lost when slk exits |
| `m{A-Z}` | Normal (message) | Set a persistent mark — survives a restart |
| `'{letter}` / `` `{letter} `` | Normal | Jump to a mark: opens the channel, loads history if needed, opens the thread and selects the reply |
| `''` / ` `` ` | Normal | Back-jump to where you jumped from; press again to return |
| `p` | Normal (thread open) | Go to the open thread's message in its channel (closes the thread; `''` returns to it) |
| `:marks` | Normal | List every mark in the workspace with its channel and message preview |
| `:delmarks abc` | Normal | Delete marks by letter (`:delmarks a b c` works too) |
| `Backspace` | Marks overlay | Delete the selected mark |
| `Ctrl+f` | Any | Search workspace (Slack server-side; supports modifiers like `from:@user`, `in:#channel`, `before:YYYY-MM-DD`) |
| `Ctrl+b` | Any | Toggle sidebar |
| `Ctrl+]` | Any | Toggle thread panel |
| `Ctrl+a` | Normal | Toggle the Activity view (mentions, thread replies, reactions, DMs) |
| `:activity` | Normal | Open the Activity view |
| `u` | Normal (activity) | Toggle the Activity view's unread-only filter |
| `U` | Normal (activity) | Toggle the selected Activity item read/unread |
| `Ctrl+t` / `Ctrl+p` | Any | Fuzzy channel finder |
| `:ws` | Normal | Workspace picker |
| `Ctrl+w s` / `:sp` | Normal | Split the focused window into two, one above the other |
| `Ctrl+w v` / `:vsp` | Normal | Split the focused window into two, side by side |
| `Ctrl+w h/j/k/l` | Normal | Focus the window in that direction |
| `Ctrl+w w` | Normal | Cycle through windows |
| `Ctrl+w q` / `:q` | Normal | Close the focused window (not the last one) |
| `Ctrl+w o` / `:only` (`:on`) | Normal | Close every other window |
| `1`–`9` | Normal | Jump to workspace N |
| `r` | Normal (message) | Open reaction picker |
| `R` | Normal (message) | Quick-toggle existing reactions |
| `E` | Normal (message) | Edit your own message |
| `D` | Normal (message) | Delete your own message (with confirmation) |
| `x` | Normal (message) | Remove a link preview from your own message (with confirmation; a choice of previews when there are several) |
| `U` | Normal (message) | Mark selected message and everything newer as unread |
| `S` | Normal (thread) | Save thread to markdown file (`~/.local/share/slk/exports/` or `$XDG_DATA_HOME/slk/exports/`) |
| `y` | Normal (message) | Copy message text |
| `Y` / `C` | Normal (message) | Copy message permalink |
| `H` / `:huddle` | Normal | Copy the huddle link of the conversation on screen (channels, DMs, group DMs; the focused thread pane, or the highlighted Threads/Activity row, names the channel) |
| `F` | Normal (message / thread) | Forward selected message using the channel finder; Enter sends, Esc cancels |
| `O` / `v` | Normal (message) | Open full-screen image preview |
| `Esc` / `q` | Preview | Close preview |
| `Enter` | Preview | Open in system image viewer |
| `h` / `←` | Preview | Previous image (when message has multiple) |
| `l` / `→` | Preview | Next image (when message has multiple) |
| Click | Any (on image) | Open full-screen preview |
| `Ctrl+y` | Any | Switch theme |
| `Alt+y` / `Alt+Shift+y` | Normal | Next / previous theme (wraps; saved per workspace) |
| `Ctrl+s` | Any | Set status (Active / Away / DND snooze / Working from home / On vacation / Clear status) |
| `q` | Normal | Close the thread pane (does nothing when none is open) |
| `Q` | Normal | Quit (with confirmation) |
| `Ctrl+c` | Any | Quit (with confirmation) |
| `K` | Normal (message) | Show the selected message author's profile |
| `e` | Profile dialog | Copy the email shown in the dialog (or click its 📋) |
| `c` | Profile dialog | Copy the person's huddle link while they are in a huddle Slack named a channel for (or click the 📋 on the "In a huddle" row) |
| `K` / `Esc` / `q` | Profile dialog | Close |

For `:` commands and slash commands, see [[Commands]].

Custom keybinding overrides are on the roadmap — see [[Tradeoffs and Non-Goals|Tradeoffs-and-Non-Goals]].

# Commands

slk has two kinds of commands:

- **`:` commands**, typed in Normal mode, drive slk itself: workspaces,
  views, marks and windows.
- **Slash commands**, typed in a composer, are Slack's: `/away`, `/remind`,
  `/msg`, app commands and the rest.

Everything else is a key. See [[Keybindings]] for those, or press `?` (or
run `:help`) in slk for the keybindings overlay, which lists the `:`
commands too.

## `:` commands

Press `:` in Normal mode. A `:` prompt opens in the status bar. Type the
command and press `Enter` to run it. `Esc` cancels, and so does `Backspace`
on an empty prompt. `Tab` completes the command name; press it again for the
next match, or `Shift+Tab` for the previous one. The matches are listed in
the status bar beside the prompt, with the current one highlighted.

| Command | Same as | What it does |
|---|---|---|
| `:ws` | `1`–`9` jumps straight to a workspace | Open the workspace picker |
| `:activity` | `Ctrl+a`, or the sidebar's ◉ Activity row | Open the Activity view (mentions, thread replies, reactions to your messages, DMs) |
| `:huddle` | `H` | Copy the huddle link of the conversation on screen. With the thread pane focused it's the thread's channel; in the Threads or Activity view, the highlighted row's |
| `:marks` | `'` (unless `[marks] show_jump_overlay` is off) | List every mark in the workspace with its channel and a preview of the message. `:marks` always shows the list, whatever `show_jump_overlay` says |
| `:delmarks abc` | `Backspace` on a row in `:marks` | Delete marks by letter. `:delmarks a b c` works too; letters with no mark are ignored |
| `:sp` | `Ctrl+w s` | Split the focused window, one above the other |
| `:vsp` | `Ctrl+w v` | Split the focused window side by side |
| `:q` | `Ctrl+w q` | Close the focused window. This never quits slk: on the last window it says "Cannot close last window" |
| `:only` (or `:on`) | `Ctrl+w o` | Close every window except the focused one |
| `:help` | `?` | Open the keybindings overlay, which also lists every `:` command |
| `:version` | `slk --version` from the shell, which also prints the build date | Show the running build's version and short commit in the status bar |

Names must match exactly when you press `Enter`, so `:act` is not
`:activity` (`Tab` completes it); the only short form is `:on`. Anything
else says `Unknown command: <name>` in the status bar.

Windows also have `Ctrl+w h` / `j` / `k` / `l` to move focus to the window
in that direction and `Ctrl+w w` to cycle through them. A split says "Not
enough room" when the window is too small to divide. While a file is
uploading, the window commands don't run ("Upload in progress").

### Looking for something else?

These aren't `:` commands, but slk does them:

| You tried | Use |
|---|---|
| `:search` | `/` searches the current channel; `Ctrl+f` searches the whole workspace on Slack's servers |
| `:join`, `:goto`, `:chan` | `Ctrl+t` or `Ctrl+p` opens the channel finder; `/open #channel` in a composer joins and opens a channel by name |
| `:i` | `i` starts writing a message (Insert mode) |
| `:w` | `Enter` in Insert mode sends the message |
| `:q` to quit | `Q` or `Ctrl+c` quits after asking (`q` closes the thread pane) |

## Slash commands

Slash commands work in both composers, the channel's and a thread's. Press
`Enter` on text that starts with `/name` and slk runs the command instead of
posting the text.

- Start the message with a space to post text that begins with `/name` as an
  ordinary message. Text like `/tmp/foo` or a lone `/` is already posted as
  written.
- While you are editing a message, `/…` text is saved as written; it isn't
  run.
- A command can't carry files. With files attached slk says "Files can't be
  sent with a command" and sends nothing.

### Built-ins

Slack's web client runs these itself rather than sending them to Slack, so
slk does the same:

| Command | What it does |
|---|---|
| `/away` | Toggle your presence between away and active |
| `/active` | Set your presence to active |
| `/status away`, `/status active` | Set your presence. Other `/status` forms (a custom status, bare `/status`) aren't supported yet; use `Ctrl+s` for status |
| `/open #channel` | Open a channel, joining it first if you aren't in it. Takes a name with or without `#`; says "No channel named #…" when nothing matches |
| `/join #channel` | Same as `/open` |
| `/msg @someone [message]` | Open your DM with that person and put the message in its composer, ready to edit and send. slk doesn't send it for you, and if the DM already has a draft slk keeps that draft and leaves the message out |
| `/dm @someone [message]` | Same as `/msg` |
| `/leave`, `/close`, `/part` | Not supported yet; slk says so |

### Everything else

Any other command goes to Slack, as the web client sends it: `/remind`,
`/shrug`, `/me`, `/giphy` and apps' commands. Slack's answer arrives the way
it does in Slack, as a message in the conversation or as a message only you
can see.

- A message only you can see that has buttons says "b to respond" when slk
  can press them; `b` opens them. Otherwise it says to open it in Slack.
- A command that opens a dialog (a bare `/giphy`, many apps' forms) says
  "… opened a dialog slk can't show yet". Finish it in Slack.
- When a command fails, slk says why in the status bar (an unknown command is
  "/name is not a valid command") and puts what you typed back in the
  composer, unless you've started typing something else there by then.

slk doesn't have a picker listing the workspace's commands yet, so you need
to know the command's name.

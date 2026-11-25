// internal/ui/slash_command.go
//
// Slash commands typed in a composer (docs/superpowers/specs/
// 2026-10-12-slash-commands-design.md §3). Enter on text that starts
// with "/name" runs the command instead of posting the text.
package ui

import (
	"strings"
	"time"
	"unicode"
	"unicode/utf8"

	tea "charm.land/bubbletea/v2"

	"github.com/gammons/slk/internal/core"
	"github.com/gammons/slk/internal/ids"
	"github.com/gammons/slk/internal/text"
	"github.com/gammons/slk/internal/ui/compose"
)

// parseSlashCommand splits composer text into a slash command's name and
// arguments. ok is false for anything that is not a command, which then
// sends as an ordinary message: text not starting with "/" (a leading
// space is the escape hatch), "/" alone, or a first token with anything
// but [A-Za-z0-9_-] after the slash ("/tmp/foo", "/giphy:"). Any Unicode
// space separates the name from its arguments, as in strings.Fields, so a
// pasted non-breaking space still runs the command. name is
// lowercased; args keeps inner whitespace and newlines and is "" when
// only whitespace follows the name.
func parseSlashCommand(text string) (name, args string, ok bool) {
	if !strings.HasPrefix(text, "/") {
		return "", "", false
	}
	end := 1
	for end < len(text) && isCommandNameByte(text[end]) {
		end++
	}
	if end == 1 {
		return "", "", false
	}
	if r, _ := utf8.DecodeRuneInString(text[end:]); end < len(text) && !unicode.IsSpace(r) {
		return "", "", false
	}
	name = strings.ToLower(text[:end])
	args = strings.TrimLeftFunc(text[end:], unicode.IsSpace)
	if strings.TrimSpace(args) == "" {
		args = ""
	}
	return name, args, true
}

func isCommandNameByte(b byte) bool {
	return b >= 'a' && b <= 'z' || b >= 'A' && b <= 'Z' || b >= '0' && b <= '9' || b == '_' || b == '-'
}

// slashCommands are the slash built-ins slk runs itself, because Slack's
// web client handles them locally rather than through chat.command
// (spec §3.3). Keys are lowercased names with their slash. Anything not
// here runs remotely.
var slashCommands = map[string]commandFunc{
	"/away":   slashAway,
	"/active": slashActive,
	"/status": slashStatus,
	"/open":   slashOpen("/open"),
	"/join":   slashOpen("/join"),
	"/msg":    slashMsg("/msg"),
	"/dm":     slashMsg("/dm"),
	"/star":   slashStar,
	"/unstar": slashUnstar,
	"/leave":  slashUnsupported("/leave"),
	"/close":  slashUnsupported("/close"),
	"/part":   slashUnsupported("/part"),
}

// builtinSlashCommands is the picker catalog for slashCommands. Aliases
// carry AliasOf so the popup hides them; typing the alias still runs.
// Descriptions are one line. A workspace list that already has the same
// name wins, so Slack's usage and description replace these.
func builtinSlashCommands() []core.SlashCommand {
	return []core.SlashCommand{
		{Name: "/away", Desc: `Toggle your "away" status`},
		{Name: "/active", Desc: "Mark yourself as active"},
		{Name: "/status", Usage: "away | active", Desc: "Set your presence"},
		{Name: "/open", Usage: "[channel]", Desc: "Open a channel"},
		{Name: "/join", Usage: "[channel]", Desc: "Open a channel", AliasOf: "/open"},
		{Name: "/msg", Usage: "@user [message]", Desc: "Open a DM"},
		{Name: "/dm", Usage: "@user [message]", Desc: "Open a DM", AliasOf: "/msg"},
		{Name: "/star", Desc: "Star this conversation"},
		{Name: "/unstar", Desc: "Unstar this conversation"},
		{Name: "/leave", Desc: "Leave the current channel"},
		{Name: "/close", Desc: "Leave the current channel", AliasOf: "/leave"},
		{Name: "/part", Desc: "Leave the current channel", AliasOf: "/leave"},
	}
}

// slashCommandsForPicker is what both composers complete. An empty
// workspace list (the fetch has not landed, or it failed) returns nil,
// so the picker stays closed and commands still run as typed. Otherwise
// it is client.appCommands, then any built-in that list does not already
// name. Workspace entries win on a shared name. The picker hides AliasOf.
func (a *App) slashCommandsForPicker() []core.SlashCommand {
	var listed []core.SlashCommand
	if a != nil && a.commandSvc != nil {
		listed = a.commandSvc.List()
	}
	if len(listed) == 0 {
		return nil
	}
	return mergeSlashCommands(listed, builtinSlashCommands())
}

func mergeSlashCommands(first, second []core.SlashCommand) []core.SlashCommand {
	seen := make(map[string]struct{}, len(first)+len(second))
	out := make([]core.SlashCommand, 0, len(first)+len(second))
	add := func(c core.SlashCommand) {
		key := text.Fold(strings.TrimSpace(c.Name))
		if key == "" {
			return
		}
		if !strings.HasPrefix(key, "/") {
			key = "/" + key
		}
		if _, ok := seen[key]; ok {
			return
		}
		seen[key] = struct{}{}
		out = append(out, c)
	}
	for _, c := range first {
		add(c)
	}
	for _, c := range second {
		add(c)
	}
	return out
}

// wireSlashCompletion points both composers at the live command list.
func (a *App) wireSlashCompletion() {
	src := func() []core.SlashCommand { return a.slashCommandsForPicker() }
	a.compose.SetCommandSource(src)
	a.threadCompose.SetCommandSource(src)
}

// commandOrigin says where a slash command was typed: the conversation
// (workspace, channel, and thread if it was the thread composer) and the
// composer text as typed, which a command that does not run gives back.
type commandOrigin struct {
	teamID, channelID, threadTS string
	typed                       string
}

// commandRanMsg is the result of running a slash command through
// CommandService. text is the composer text as typed, kept so a failure
// can put it back; teamID/channelID/threadTS identify the conversation
// it was typed in.
type commandRanMsg struct {
	teamID, channelID, threadTS string
	name                        string
	text                        string
	err                         error
}

// trySlashCommand runs c's text as a slash command when it is one,
// called by both Enter branches of handleInsertMode before anything else
// is sent. It reports false for ordinary text and while panel is editing
// a message (an edit is saved as written), and the caller's send path
// runs unchanged. With files attached nothing is sent: a command cannot
// carry them.
func (a *App) trySlashCommand(panel Panel, c *compose.Model, channelID, threadTS string) (tea.Cmd, bool) {
	if a.editing.IsActive() && a.editing.Panel() == panel {
		return nil, false
	}
	raw := c.Value()
	name, _, ok := parseSlashCommand(raw)
	if !ok {
		return nil, false
	}
	if len(c.Attachments()) > 0 {
		return toastWithClear(a, "Files can't be sent with a command", 3*time.Second), true
	}
	// Translate @name/#name to wire form, as a send does. The name is
	// untouched by translation, so re-parsing yields the wire-form args.
	_, args, _ := parseSlashCommand(c.TranslateMentionsForSend(raw))
	c.Reset()
	a.exitInsertAfterSend()
	_, cargs, _ := splitCommandLine(name + " " + args)
	cargs.Origin = commandOrigin{teamID: a.activeTeamID, channelID: channelID, threadTS: threadTS, typed: raw}
	run := runCommand(a, slashCommands, name, cargs, func(a *App, name string, args commandArgs) tea.Cmd {
		return a.runRemoteCommand(channelID, threadTS, name, args.Rest, raw)
	})
	return tea.Batch(run, a.tryOpenActionPrompt()), true
}

// offerTypedBack puts the command as typed back into the composer it
// came from, unless that conversation has a draft by now. A thread
// command goes back to the thread composer, keyed by its thread;
// anything else to the channel composer. Reports whether it wrote.
func (a *App) offerTypedBack(o commandOrigin) bool {
	c := &a.compose
	if o.threadTS != "" {
		c = &a.threadCompose
	}
	return c.OfferDraft(o.teamID, o.channelID, o.threadTS, o.typed)
}

// refuseCommand is a slash command that did not run: it gives the typed
// text back and says why.
func (a *App) refuseCommand(o commandOrigin, text string) tea.Cmd {
	a.offerTypedBack(o)
	return toastWithClear(a, text, 3*time.Second)
}

// runRemoteCommand runs name through CommandService off the Update
// goroutine. What the command produces arrives over the WebSocket; only
// the error comes back, in commandRanMsg.
func (a *App) runRemoteCommand(channelID, threadTS, name, args, text string) tea.Cmd {
	svc, teamID := a.commandSvc, a.activeTeamID
	return func() tea.Msg {
		err := svc.Run(teamID, ids.ChannelID(channelID), ids.ThreadTS(threadTS), name, args)
		return commandRanMsg{teamID: teamID, channelID: channelID, threadTS: threadTS, name: name, text: text, err: err}
	}
}

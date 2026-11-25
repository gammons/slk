package core

// SlashCommand is one slash command the composer can complete: a workspace
// command from client.appCommands, or a built-in slk runs itself.
// AliasOf, when set, names the command this one stands in for. The picker
// hides those; typing the alias in full still runs it.
type SlashCommand struct {
	Name    string // with the leading slash, as Slack sends it ("/giphy")
	Usage   string
	Desc    string
	AppName string
	AliasOf string
}

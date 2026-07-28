package slackdesktop

// Workspace is one signed-in workspace from the Slack desktop app.
type Workspace struct {
	Name   string
	Domain string // subdomain under .slack.com
	TeamID string
	Token  string // xoxc-… from localConfig_v2
}

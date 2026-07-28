package slackdesktop

import (
	"encoding/json"
	"os"
	"path/filepath"
	"sort"
)

// Workspace is one signed-in workspace from the Slack desktop app.
type Workspace struct {
	Name   string
	Domain string // subdomain under .slack.com
	TeamID string
	Token  string // xoxc-… from localConfig_v2
}

type rootState struct {
	Workspaces map[string]struct {
		Name   string `json:"name"`
		URL    string `json:"url"`
		Domain string `json:"domain"`
		ID     string `json:"id"`
	} `json:"workspaces"`
}

func parseWorkspaces(data []byte) ([]Workspace, error) {
	var rs rootState
	if err := json.Unmarshal(data, &rs); err != nil {
		return nil, err
	}
	var out []Workspace
	for _, w := range rs.Workspaces {
		if w.Domain == "" || w.ID == "" {
			continue
		}
		out = append(out, Workspace{Name: w.Name, Domain: w.Domain, TeamID: w.ID})
	}
	if len(out) == 0 {
		return nil, ErrNotSignedIn
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Name < out[j].Name })
	return out, nil
}

// Workspaces reads and parses the desktop app's signed-in workspace list.
func Workspaces() ([]Workspace, error) {
	dir, err := ConfigDir()
	if err != nil {
		return nil, err
	}
	data, err := os.ReadFile(filepath.Join(dir, "storage", "root-state.json"))
	if err != nil {
		return nil, ErrNotSignedIn
	}
	return parseWorkspaces(data)
}

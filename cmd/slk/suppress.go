package main

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"sync"

	"github.com/pelletier/go-toml/v2"

	"github.com/gammons/slk/internal/config"
	"github.com/gammons/slk/internal/core"
)

// errSuppressRuleNotInFile is Remove's answer for a rule config.toml
// holds in a form slk does not edit (an inline table, a header with
// spaces): the user removes it by hand.
var errSuppressRuleNotInFile = core.ErrSuppressRuleNotEditable

// suppressStore holds every workspace's suppression rules
// (core.Suppression), seeded from the config and changed by the UI
// through core.SuppressionService. It is read from the WebSocket event
// goroutines (rtmEventHandler.suppressed) and the Update goroutine
// (Rules), so the map sits behind mu; each set is immutable and is
// replaced whole, only after config.toml was written. writeMu
// serialises Add and Remove, so the file write happens outside mu and
// readers never wait on disk.
type suppressStore struct {
	mu      sync.RWMutex
	writeMu sync.Mutex
	sets    map[string]*core.Suppression

	configPath string
	// tomlKey names teamID's [workspaces.<key>] block.
	tomlKey func(teamID string) string
	// teamName labels a block slk creates for a workspace that has
	// none yet.
	teamName func(teamID string) string
}

func newSuppressStore(cfg config.Config, configPath string, tomlKey, teamName func(string) string) *suppressStore {
	s := &suppressStore{
		sets:       make(map[string]*core.Suppression),
		configPath: configPath,
		tomlKey:    tomlKey,
		teamName:   teamName,
	}
	for _, w := range cfg.Workspaces {
		if w.TeamID != "" && len(w.Suppress) > 0 {
			s.sets[w.TeamID] = w.SuppressRules()
		}
	}
	return s
}

// Rules returns teamID's rules (nil when it has none).
func (s *suppressStore) Rules(teamID string) *core.Suppression {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return s.sets[teamID]
}

// Hides reports whether a message by authorID in channelID of teamID is
// suppressed. authorID is what the message is attributed to (the user,
// else the bot ID), as in the UI.
func (s *suppressStore) Hides(teamID, channelID, authorID string) bool {
	return s.Rules(teamID).Hides(channelID, authorID)
}

// Add saves rule to config.toml and then applies it. Adding a rule the
// workspace already holds does nothing.
func (s *suppressStore) Add(teamID string, rule core.SuppressRule) error {
	if teamID == "" || rule.AuthorID == "" {
		return errors.New("no workspace or author to suppress")
	}
	s.writeMu.Lock()
	defer s.writeMu.Unlock()
	cur := s.Rules(teamID)
	if cur.Has(rule) {
		return nil
	}
	if err := appendSuppressRule(s.configPath, s.tomlKey(teamID), teamID, s.teamName(teamID), rule); err != nil {
		return fmt.Errorf("save to config.toml: %w", err)
	}
	s.mu.Lock()
	s.sets[teamID] = cur.With(rule)
	s.mu.Unlock()
	return nil
}

// Remove deletes rule's block from config.toml and then stops applying
// it. Removing a rule the workspace does not hold does nothing.
func (s *suppressStore) Remove(teamID string, rule core.SuppressRule) error {
	s.writeMu.Lock()
	defer s.writeMu.Unlock()
	cur := s.Rules(teamID)
	if !cur.Has(rule) {
		return nil
	}
	removed, err := removeSuppressRule(s.configPath, s.tomlKey(teamID), rule)
	if err != nil {
		return fmt.Errorf("save to config.toml: %w", err)
	}
	if !removed {
		return errSuppressRuleNotInFile
	}
	s.mu.Lock()
	s.sets[teamID] = cur.Without(rule)
	s.mu.Unlock()
	return nil
}

// suppressHeader is the array-of-tables header of a workspace's rules.
func suppressHeader(tomlKey string) string {
	return "[[workspaces." + tomlKey + ".suppress]]"
}

// suppressBlockLines renders rule as the lines of one block.
func suppressBlockLines(tomlKey string, rule core.SuppressRule) []string {
	lines := []string{suppressHeader(tomlKey), "author = " + tomlString(rule.AuthorID)}
	if rule.ChannelID != "" {
		lines = append(lines, "channel = "+tomlString(rule.ChannelID))
	}
	if note := sanitizeComment(rule.Note); note != "" {
		lines = append(lines, "note = "+tomlString(note))
	}
	return lines
}

// readConfigLines reads configPath as lines; a missing file (and its
// directory) is created on write, so it reads as empty here.
func readConfigLines(configPath string) ([]string, error) {
	data, err := os.ReadFile(configPath)
	if errors.Is(err, os.ErrNotExist) {
		if err := os.MkdirAll(filepath.Dir(configPath), 0755); err != nil {
			return nil, err
		}
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	return strings.Split(string(data), "\n"), nil
}

// appendSuppressRule appends rule as a [[workspaces.<tomlKey>.suppress]]
// block at the end of config.toml, a textual edit like the theme saver's
// so the rest of the file is left as written. A workspace with no
// [workspaces.<tomlKey>] block gets one first (commented with its
// name), so the other savers find it.
func appendSuppressRule(configPath, tomlKey, teamID, teamName string, rule core.SuppressRule) error {
	configWriteMu.Lock()
	defer configWriteMu.Unlock()

	lines, err := readConfigLines(configPath)
	if err != nil {
		return err
	}
	for len(lines) > 0 && strings.TrimSpace(lines[len(lines)-1]) == "" {
		lines = lines[:len(lines)-1]
	}
	header := fmt.Sprintf("[workspaces.%s]", tomlKey)
	hasBlock := false
	for _, l := range lines {
		t := strings.TrimSpace(l)
		if t == header || strings.HasPrefix(t, "[workspaces."+tomlKey+".") || strings.HasPrefix(t, "[[workspaces."+tomlKey+".") {
			hasBlock = true
			break
		}
	}
	if len(lines) > 0 {
		lines = append(lines, "")
	}
	if !hasBlock {
		name := sanitizeComment(teamName)
		if name == "" {
			name = teamID
		}
		lines = append(lines, "# "+name, header, "")
	}
	lines = append(lines, suppressBlockLines(tomlKey, rule)...)
	lines = append(lines, "")
	return writeConfigAtomic(configPath, []byte(strings.Join(lines, "\n")))
}

// removeSuppressRule deletes the first [[workspaces.<tomlKey>.suppress]]
// block whose author and channel are rule's, and reports whether it
// found one. A block runs from its header to its last key line before
// the next header: comments and blank lines in front of the next header
// belong to what follows.
func removeSuppressRule(configPath, tomlKey string, rule core.SuppressRule) (bool, error) {
	configWriteMu.Lock()
	defer configWriteMu.Unlock()

	lines, err := readConfigLines(configPath)
	if err != nil || lines == nil {
		return false, err
	}
	header := suppressHeader(tomlKey)
	for start := 0; start < len(lines); start++ {
		if strings.TrimSpace(lines[start]) != header {
			continue
		}
		next := len(lines)
		for j := start + 1; j < len(lines); j++ {
			if strings.HasPrefix(strings.TrimSpace(lines[j]), "[") {
				next = j
				break
			}
		}
		end := next
		for end > start+1 {
			t := strings.TrimSpace(lines[end-1])
			if t != "" && !strings.HasPrefix(t, "#") {
				break
			}
			end--
		}
		var got struct {
			Author  string `toml:"author"`
			Channel string `toml:"channel"`
		}
		if toml.Unmarshal([]byte(strings.Join(lines[start+1:end], "\n")), &got) != nil {
			continue
		}
		if got.Author != rule.AuthorID || got.Channel != rule.ChannelID {
			continue
		}
		// Take one blank line with the block, so removing what
		// appendSuppressRule added leaves no gap behind.
		if end < len(lines) && strings.TrimSpace(lines[end]) == "" {
			end++
		} else if start > 0 && strings.TrimSpace(lines[start-1]) == "" {
			start--
		}
		out := append(append([]string(nil), lines[:start]...), lines[end:]...)
		return true, writeConfigAtomic(configPath, []byte(strings.Join(out, "\n")))
	}
	return false, nil
}

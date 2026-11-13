// internal/ui/load_images.go
//
// I: load the selected message's images that the image_loading policy
// held back. A message image whose URL the policy does not trust is drawn
// as a "not loaded" line instead of being fetched; I lists the hosts it
// would contact and asks before any request is made. The answer is
// imagesAllowedMsg, reduced in reducer_load_images.go.
package ui

import (
	"sort"
	"strconv"
	"strings"
	"time"

	"charm.land/bubbles/v2/key"
	tea "charm.land/bubbletea/v2"

	"github.com/gammons/slk/internal/bubbles/confirmprompt"
	imgpkg "github.com/gammons/slk/internal/image"
	"github.com/gammons/slk/internal/ui/messages"
)

// imagesAllowedMsg is a load prompt's answer: URLs and hosts the user
// allowed for the rest of the session.
type imagesAllowedMsg struct {
	URLs  []string
	Hosts []string
}

// heldImages returns the URLs of msg's images the policy is waiting on
// the user for, and their hosts, sorted.
func heldImages(p *imgpkg.LoadPolicy, msg messages.MessageItem) (urls, hosts []string) {
	seen := map[string]struct{}{}
	for _, u := range messages.ImageURLs(msg) {
		if p.Decide(u) != imgpkg.Ask {
			continue
		}
		urls = append(urls, u)
		h := imgpkg.HostOf(u)
		if _, ok := seen[h]; !ok {
			seen[h] = struct{}{}
			hosts = append(hosts, h)
		}
	}
	sort.Strings(hosts)
	return urls, hosts
}

// previewHeld reports whether the policy would refuse the preview of
// msg's attachment attIdx (any of its thumbnails not loading).
func previewHeld(p *imgpkg.LoadPolicy, msg messages.MessageItem, attIdx int) bool {
	if attIdx < 0 || attIdx >= len(msg.Attachments) {
		return false
	}
	for _, t := range msg.Attachments[attIdx].Thumbs {
		if t.URL != "" && p.Decide(t.URL) != imgpkg.Load {
			return true
		}
	}
	return false
}

// loadImagesOfSelected opens the load prompt for the selected message.
func (a *App) loadImagesOfSelected() tea.Cmd {
	msg, _, ok := a.selectedMessageInFocus()
	if !ok {
		return nil
	}
	return a.promptLoadImages(msg)
}

// promptLoadImages asks whether to load msg's held images, or toasts why
// there is nothing to ask.
func (a *App) promptLoadImages(msg messages.MessageItem) tea.Cmd {
	p := a.imageCtx.Policy
	if p.Mode() == imgpkg.LoadOff {
		return toastWithClear(a, "Images are off (image_loading = \"off\")", 3*time.Second)
	}
	urls, hosts := heldImages(p, msg)
	if len(urls) == 0 {
		return toastWithClear(a, "No images waiting to load on this message", 3*time.Second)
	}
	a.openConfirmChoice(loadImagesChoice(urls, hosts))
	return nil
}

// loadImagesChoice is the prompt for urls, served from hosts.
func loadImagesChoice(urls, hosts []string) confirmprompt.Choice {
	noun := "image"
	if len(urls) != 1 {
		noun = "images"
	}
	body := strconv.Itoa(len(urls)) + " " + noun + " from " + strings.Join(hosts, ", ") + "."
	external := false
	for _, h := range hosts {
		if !imgpkg.TrustedHost(h) {
			external = true
		}
	}
	if external {
		body += "\nNot hosted by Slack: loading tells that server your IP address and when you read this."
	}
	allowURLs := imagesAllowedMsg{URLs: urls}
	allowHosts := imagesAllowedMsg{Hosts: hosts}
	always := "Trust this host this session"
	if len(hosts) > 1 {
		always = "Trust these hosts this session"
	}
	return confirmprompt.Choice{
		Title:   "Load images?",
		Body:    body,
		Default: -1,
		Buttons: []confirmprompt.Button{
			{
				Label:  "Load",
				Keys:   key.NewBinding(key.WithKeys("y")),
				Action: func() tea.Msg { return allowURLs },
			},
			{
				Label:  always,
				Keys:   key.NewBinding(key.WithKeys("a")),
				Action: func() tea.Msg { return allowHosts },
			},
			{Label: "Cancel", Keys: key.NewBinding(key.WithKeys("n"))},
		},
	}
}

// allowImages records m in the policy and redraws, so the panes fetch
// what is now allowed.
func (a *App) allowImages(m imagesAllowedMsg) {
	p := a.imageCtx.Policy
	p.AllowURLs(m.URLs...)
	p.AllowHosts(m.Hosts...)
	a.invalidateAllWinModelCaches()
	a.threadPanel.InvalidateCache()
}

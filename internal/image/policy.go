package image

import (
	"net/url"
	"strings"
	"sync"
)

// LoadMode is the user's choice of which message images load without
// asking ([general] image_loading).
type LoadMode string

const (
	// LoadTrusted loads images served by Slack itself and asks before
	// fetching from any other host. The default.
	LoadTrusted LoadMode = "trusted"
	// LoadConfirm asks before every message image, Slack-hosted or not.
	LoadConfirm LoadMode = "confirm"
	// LoadOff never loads message images; they stay links.
	LoadOff LoadMode = "off"
)

// ParseLoadMode maps a config value to a LoadMode; ok is false for an
// unknown value (the caller falls back to LoadTrusted).
func ParseLoadMode(s string) (LoadMode, bool) {
	switch m := LoadMode(strings.ToLower(strings.TrimSpace(s))); m {
	case LoadTrusted, LoadConfirm, LoadOff:
		return m, true
	}
	return LoadTrusted, false
}

// Decision is what a LoadPolicy says about one image URL.
type Decision int

const (
	// Load: fetch and draw it.
	Load Decision = iota
	// Ask: draw a "not loaded" line; the user may allow it.
	Ask
	// Block: never fetch it; draw it as a link.
	Block
)

// trustedSuffixes are the hosts whose images are produced by Slack: file
// thumbnails (re-encoded by Slack from the upload, fetched with the
// workspace's credentials), avatars and custom emoji. A sender can choose
// what gets uploaded, but not where the bytes are fetched from, so loading
// them tells nobody outside Slack that, when, or from where you read the
// message. slack-imgs.com is deliberately absent: it proxies arbitrary
// third-party URLs.
var trustedSuffixes = []string{
	"files.slack.com",
	"slack-edge.com",
}

// TrustedHost reports whether host serves Slack-produced images: one of
// trustedSuffixes or a subdomain of one. Case-insensitive; pass the
// hostname without a port.
func TrustedHost(host string) bool {
	return hostMatches(strings.ToLower(host), trustedSuffixes)
}

func hostMatches(host string, suffixes []string) bool {
	if host == "" {
		return false
	}
	for _, s := range suffixes {
		if host == s || strings.HasSuffix(host, "."+s) {
			return true
		}
	}
	return false
}

// HostOf returns the lower-cased host of rawURL (with any port), or ""
// when rawURL does not parse or has none.
func HostOf(rawURL string) string {
	u, err := url.Parse(rawURL)
	if err != nil {
		return ""
	}
	return strings.ToLower(u.Host)
}

// LoadPolicy decides which message images load. It is shared by the
// render path (UI goroutine) and the fetcher (worker goroutines), so its
// allow lists are locked. A nil *LoadPolicy loads everything; production
// always installs one (cmd/slk), the nil case keeps unit tests that never
// configure images unchanged.
type LoadPolicy struct {
	mode LoadMode
	// configHosts are the user's [general] trusted_image_hosts,
	// matched like trustedSuffixes. Immutable after construction.
	configHosts []string

	mu    sync.RWMutex
	urls  map[string]struct{} // allowed this session, exact URL
	hosts map[string]struct{} // allowed this session, exact host
}

// NewLoadPolicy returns a policy for mode. extraHosts are additional
// trusted hosts (each also covering its subdomains); they only matter in
// LoadTrusted mode.
func NewLoadPolicy(mode LoadMode, extraHosts []string) *LoadPolicy {
	p := &LoadPolicy{
		mode:  mode,
		urls:  map[string]struct{}{},
		hosts: map[string]struct{}{},
	}
	for _, h := range extraHosts {
		if h = strings.ToLower(strings.TrimSpace(h)); h != "" {
			p.configHosts = append(p.configHosts, strings.TrimPrefix(h, "*."))
		}
	}
	return p
}

// Mode returns the policy's mode; LoadTrusted for a nil policy's callers
// that need to word something.
func (p *LoadPolicy) Mode() LoadMode {
	if p == nil {
		return LoadTrusted
	}
	return p.mode
}

// Decide says whether rawURL may be fetched as a message image.
//
// Only https URLs load automatically: http is at best Ask, and any other
// scheme (file, data, …) is Block. A URL or host the user allowed this
// session loads in every mode but LoadOff.
func (p *LoadPolicy) Decide(rawURL string) Decision {
	if p == nil {
		return Load
	}
	if p.mode == LoadOff {
		return Block
	}
	u, err := url.Parse(rawURL)
	if err != nil || u.Host == "" || (u.Scheme != "https" && u.Scheme != "http") {
		return Block
	}
	if p.allowedByUser(rawURL) {
		return Load
	}
	host := strings.ToLower(u.Hostname())
	if p.mode == LoadTrusted && u.Scheme == "https" &&
		(TrustedHost(host) || hostMatches(host, p.configHosts)) {
		return Load
	}
	return Ask
}

// allowedByUser reports whether the user allowed rawURL, or its host,
// this session.
func (p *LoadPolicy) allowedByUser(rawURL string) bool {
	host := HostOf(rawURL)
	p.mu.RLock()
	defer p.mu.RUnlock()
	_, urlOK := p.urls[rawURL]
	_, hostOK := p.hosts[host]
	return urlOK || hostOK
}

// AllowURLs lets each of urls load for the rest of the session.
func (p *LoadPolicy) AllowURLs(urls ...string) {
	if p == nil {
		return
	}
	p.mu.Lock()
	defer p.mu.Unlock()
	for _, u := range urls {
		p.urls[u] = struct{}{}
	}
}

// AllowHosts lets every image from each of hosts (exact host, as HostOf
// returns it) load for the rest of the session.
func (p *LoadPolicy) AllowHosts(hosts ...string) {
	if p == nil {
		return
	}
	p.mu.Lock()
	defer p.mu.Unlock()
	for _, h := range hosts {
		if h = strings.ToLower(h); h != "" {
			p.hosts[h] = struct{}{}
		}
	}
}

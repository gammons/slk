// Package version provides formatting helpers for slk's build-time
// version metadata. The build vars themselves live in package main
// (injected via -ldflags by GoReleaser); callers pass the version
// string in so this package stays free of build-time coupling.
package version

// Line returns the one-line build identity printed by `slk --version`,
// e.g. "slk v1.2.3 (commit abc1234, built 2026-01-02T03:04:05Z)".
func Line(version, commit, date string) string {
	return "slk " + version + " (commit " + commit + ", built " + date + ")"
}

// Short returns the build identity :version shows in the status bar: the
// version and the commit cut to 7 characters, e.g. "v1.2.3 · abc1234", or
// the version alone when no commit was stamped. Toasts are not clipped to
// the space the status bar has left, so the identity is kept short;
// slk --version prints the full Line.
func Short(version, commit string) string {
	if commit == "" || commit == "none" {
		return version
	}
	if len(commit) > 7 {
		commit = commit[:7]
	}
	return version + " · " + commit
}

// osc8 wraps url as a clickable OSC-8 hyperlink whose visible label is
// the URL itself. Terminals without OSC-8 support render the label as
// plain text.
func osc8(url string) string {
	return "\x1b]8;;" + url + "\x1b\\" + url + "\x1b]8;;\x1b\\"
}

// ModalFooter returns the single attribution line shown at the bottom
// of the TUI help modal, e.g.:
//
//	slk dev - Made with ❤️ by Grant Ammons (https://grant.dev)
//
// The URL is OSC-8 wrapped so supporting terminals make it clickable.
func ModalFooter(version string) string {
	return "slk " + version + " - Made with \u2764\ufe0f by Grant Ammons (" + osc8("https://grant.dev") + ")"
}

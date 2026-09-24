// This file writes a channel export to disk: one Markdown file per
// conversation (a top-level message plus its in-window replies) and an
// index.md that links them by day. It reuses ThreadToMarkdown for the
// message bodies so a channel export and a thread saved with S read the
// same. Fetching the messages is the caller's job.

package export

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"slices"
	"strings"

	"github.com/gammons/slk/internal/ui/messages"
)

const (
	// indexFilename is the entry point written next to the conversations.
	indexFilename = "index.md"
	// snippetRunes caps the message preview used as an index link label.
	snippetRunes = 80
	// clockLayout is the per-message time of day; the index header
	// names the timezone it is read in.
	clockLayout = "15:04"
)

// ErrOutputNotEmpty is returned by WriteChannel when the output
// directory already holds files. An export never overwrites or mixes
// into an earlier one.
var ErrOutputNotEmpty = errors.New("output directory is not empty")

// Conversation is one top-level channel message and the replies to it
// that fall inside the export window.
type Conversation struct {
	Parent  messages.MessageItem
	Replies []messages.MessageItem
	// ParentIsContext is true when Parent predates the window and is
	// kept only to give its in-window Replies context.
	ParentIsContext bool
}

// Channel is one channel's worth of conversations to export, along
// with the labels the index needs.
type Channel struct {
	Workspace     string
	Name          string
	Window        Window
	Conversations []Conversation
}

// nameMap adapts an ID-to-name map to the resolver signature
// messages.FlattenMrkdwn expects.
type nameMap map[string]string

// lookup returns the name for id, reporting whether it is known.
func (n nameMap) lookup(id string) (string, bool) {
	name, ok := n[id]
	return name, ok
}

// WriteChannel writes ch into dir as one Markdown file per conversation
// plus an index.md, creating dir if needed, and returns the index path.
// It fails with ErrOutputNotEmpty rather than write into a directory
// that already has content. The files are staged in a sibling
// directory and moved into dir only once every one of them is written,
// so a failed export leaves dir empty and the command can be re-run.
// Each message's DateStr and Timestamp are derived from its TS in the
// window's timezone; values set by the caller are ignored. userNames
// and channelNames resolve mentions.
func WriteChannel(dir string, ch Channel, userNames, channelNames map[string]string) (string, error) {
	if err := EnsureEmptyDir(dir); err != nil {
		return "", err
	}
	staging, err := newStagingDir(dir)
	if err != nil {
		return "", err
	}
	if err := writeConversations(staging, ch, userNames, channelNames); err != nil {
		return "", errors.Join(err, os.RemoveAll(staging))
	}
	if err := moveContents(staging, dir); err != nil {
		return "", fmt.Errorf("moving the export into place, files left in %s: %w", staging, err)
	}
	return filepath.Join(dir, indexFilename), nil
}

// newStagingDir creates the directory an export is written to before it
// is moved into dir: a uniquely named sibling of dir, so the move stays
// on one filesystem and an interrupted export leaves dir itself empty.
func newStagingDir(dir string) (string, error) {
	abs, err := filepath.Abs(dir)
	if err != nil {
		return "", fmt.Errorf("resolving output directory: %w", err)
	}
	staging, err := os.MkdirTemp(filepath.Dir(abs), filepath.Base(abs)+".partial-")
	if err != nil {
		return "", fmt.Errorf("creating staging directory: %w", err)
	}
	return staging, nil
}

// moveContents moves every entry of src into dst and removes src.
// Renaming the directory itself is not an option: macOS refuses to
// rename over an existing directory, and replacing dst would pull it
// out from under a shell whose working directory it is.
func moveContents(src, dst string) error {
	entries, err := os.ReadDir(src)
	if err != nil {
		return err
	}
	for _, e := range entries {
		if err := os.Rename(filepath.Join(src, e.Name()), filepath.Join(dst, e.Name())); err != nil {
			return err
		}
	}
	return os.Remove(src)
}

// writeConversations writes ch's conversation files and index.md into
// dir, which must exist.
func writeConversations(dir string, ch Channel, userNames, channelNames map[string]string) error {
	convs := slices.Clone(ch.Conversations)
	slices.SortFunc(convs, compareConversations)

	var index strings.Builder
	index.WriteString(indexHeader(ch, convs))
	day := ""
	used := make(map[string]bool, len(convs))
	for i, conv := range convs {
		conv = stampConversation(conv, ch.Window)
		if i == 0 || conv.Parent.DateStr != day {
			day = conv.Parent.DateStr
			index.WriteString("\n## " + dayHeading(day) + "\n\n")
		}
		name := uniqueFilename(conversationFilename(conv.Parent.TS, ch.Window), used)
		content := conversationMarkdown(ch.Name, conv, userNames, channelNames)
		if err := os.WriteFile(filepath.Join(dir, name), []byte(content), 0o644); err != nil {
			return fmt.Errorf("writing conversation %s: %w", conv.Parent.TS, err)
		}
		index.WriteString(indexEntry(conv, name, userNames, channelNames))
	}

	if err := os.WriteFile(filepath.Join(dir, indexFilename), []byte(index.String()), 0o644); err != nil {
		return fmt.Errorf("writing index: %w", err)
	}
	return nil
}

// DefaultChannelDir returns the directory a channel export is written
// to when the user names none: a folder under ExportDir named for the
// channel and the requested date range.
func DefaultChannelDir(channelName string, w Window) (string, error) {
	base, err := ExportDir()
	if err != nil {
		return "", err
	}
	name := fmt.Sprintf("slk-channel-%s-%s-to-%s", sanitizeForFilename(channelName), w.Since.Format(dateLayout), w.Until.Format(dateLayout))
	return filepath.Join(base, name), nil
}

// EnsureEmptyDir creates dir when it is missing and fails with
// ErrOutputNotEmpty when it already has entries. WriteChannel calls it
// itself; it is exported so a caller can reject a bad directory before
// spending minutes on the fetch.
func EnsureEmptyDir(dir string) error {
	entries, err := os.ReadDir(dir)
	if errors.Is(err, os.ErrNotExist) {
		if err := os.MkdirAll(dir, 0o755); err != nil {
			return fmt.Errorf("creating output directory: %w", err)
		}
		return nil
	}
	if err != nil {
		return fmt.Errorf("reading output directory: %w", err)
	}
	if len(entries) > 0 {
		return fmt.Errorf("%w: %s", ErrOutputNotEmpty, dir)
	}
	return nil
}

// compareConversations orders conversations by parent timestamp. Slack
// timestamps are fixed-width, so byte order is chronological order.
func compareConversations(a, b Conversation) int {
	return strings.Compare(a.Parent.TS, b.Parent.TS)
}

// stampConversation returns conv with every message's display date and
// time set from its TS in the window's timezone.
func stampConversation(conv Conversation, w Window) Conversation {
	conv.Parent = stampMessage(conv.Parent, w)
	replies := make([]messages.MessageItem, len(conv.Replies))
	for i, r := range conv.Replies {
		replies[i] = stampMessage(r, w)
	}
	conv.Replies = replies
	return conv
}

// stampMessage sets msg's DateStr and Timestamp from its TS. A TS that
// does not parse leaves both blank rather than inventing a time.
func stampMessage(msg messages.MessageItem, w Window) messages.MessageItem {
	msg.DateStr, msg.Timestamp = "", ""
	if t, ok := TimeFromTS(msg.TS); ok {
		t = t.In(w.Location)
		msg.DateStr = t.Format(dateLayout)
		msg.Timestamp = t.Format(clockLayout)
	}
	return msg
}

// conversationFilename names a conversation's file after its parent:
// the local date and time for readability, then the Slack sequence
// number so two messages posted in the same second stay distinct. A TS
// that does not parse yields a name that uniqueFilename must separate.
func conversationFilename(ts string, w Window) string {
	_, seq, _ := strings.Cut(ts, ".")
	stamp := "unknown"
	if t, ok := TimeFromTS(ts); ok {
		stamp = t.In(w.Location).Format("2006-01-02-150405")
	}
	return stamp + "-" + sanitizeForFilename(seq) + ".md"
}

// uniqueFilename returns name, or name with a counter before its
// extension when used already holds it, and records the result in used.
// It is what keeps one export from writing two conversations to the
// same file whatever their timestamps look like.
func uniqueFilename(name string, used map[string]bool) string {
	candidate := name
	for n := 2; used[candidate]; n++ {
		candidate = fmt.Sprintf("%s-%d.md", strings.TrimSuffix(name, ".md"), n)
	}
	used[candidate] = true
	return candidate
}

// dayHeading is the index.md heading for a day; a blank day, from a
// parent whose TS did not parse, gets a label rather than an empty
// heading.
func dayHeading(day string) string {
	if day == "" {
		return "Unknown date"
	}
	return day
}

// conversationMarkdown renders one conversation file. conv must already
// be stamped.
func conversationMarkdown(channelName string, conv Conversation, userNames, channelNames map[string]string) string {
	var b strings.Builder
	title := "# #" + channelName
	if conv.Parent.DateStr != "" {
		title += " - " + conv.Parent.DateStr + " " + conv.Parent.Timestamp
	}
	b.WriteString(title + "\n\n")
	b.WriteString("[Back to index](" + indexFilename + ")\n\n")
	if conv.ParentIsContext {
		b.WriteString("> The first message predates the export range and is included as context for its replies.\n\n")
	}
	b.WriteString(ThreadToMarkdown(conv.Parent, conv.Replies, userNames, channelNames))
	return b.String()
}

// indexHeader renders the title and summary block of index.md. convs
// is the full conversation list, used for the totals.
func indexHeader(ch Channel, convs []Conversation) string {
	w := ch.Window
	msgCount := 0
	for _, conv := range convs {
		msgCount += len(conv.Replies)
		if !conv.ParentIsContext {
			msgCount++
		}
	}

	var b strings.Builder
	b.WriteString("# #" + ch.Name + "\n\n")
	if ch.Workspace != "" {
		b.WriteString("- Workspace: " + ch.Workspace + "\n")
	}
	b.WriteString("- Range: " + w.Since.Format(dateLayout) + " to " + w.Until.Format(dateLayout) + " (end date excluded)\n")
	if w.OverlapDays > 0 {
		fmt.Fprintf(&b, "- Overlap: %d days, widening the range to %s to %s\n", w.OverlapDays, w.Start.Format(dateLayout), w.End.Format(dateLayout))
	}
	b.WriteString("- Timezone: " + w.Location.String() + "\n")
	fmt.Fprintf(&b, "- Conversations: %d\n", len(convs))
	fmt.Fprintf(&b, "- Messages: %d\n", msgCount)
	return b.String()
}

// indexEntry renders one index.md list item linking to filename. conv
// must already be stamped.
func indexEntry(conv Conversation, filename string, userNames, channelNames map[string]string) string {
	label := conv.Parent.UserName + ": " + snippet(conv.Parent, userNames, channelNames)
	if conv.Parent.Timestamp != "" {
		label = conv.Parent.Timestamp + " " + label
	}
	entry := "- [" + escapeLinkLabel(label) + "](" + filename + ")"

	var notes []string
	switch n := len(conv.Replies); n {
	case 0:
	case 1:
		notes = append(notes, "1 reply")
	default:
		notes = append(notes, fmt.Sprintf("%d replies", n))
	}
	if conv.ParentIsContext {
		notes = append(notes, "started before the range")
	}
	if len(notes) > 0 {
		entry += " - " + strings.Join(notes, ", ")
	}
	return entry + "\n"
}

// snippet returns the first non-blank line of msg's body as plain
// text, shortened to snippetRunes.
func snippet(msg messages.MessageItem, userNames, channelNames map[string]string) string {
	flat := messages.FlattenMrkdwn(messages.MessageTextSource(msg), nameMap(userNames).lookup, nameMap(channelNames).lookup)
	for line := range strings.SplitSeq(flat, "\n") {
		line = strings.TrimSpace(line)
		if line == "" {
			continue
		}
		if runes := []rune(line); len(runes) > snippetRunes {
			return string(runes[:snippetRunes]) + "..."
		}
		return line
	}
	return "(no text)"
}

// escapeLinkLabel backslash-escapes the characters that would end or
// nest a Markdown link label.
func escapeLinkLabel(s string) string {
	return strings.NewReplacer(`\`, `\\`, "[", `\[`, "]", `\]`).Replace(s)
}

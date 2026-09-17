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
// that already has content. Each message's DateStr and Timestamp are
// derived from its TS in the window's timezone; values set by the
// caller are ignored. userNames and channelNames resolve mentions.
func WriteChannel(dir string, ch Channel, userNames, channelNames map[string]string) (string, error) {
	if err := EnsureEmptyDir(dir); err != nil {
		return "", err
	}
	convs := slices.Clone(ch.Conversations)
	slices.SortFunc(convs, compareConversations)

	var index strings.Builder
	index.WriteString(indexHeader(ch, convs))
	day := ""
	for _, conv := range convs {
		conv = stampConversation(conv, ch.Window)
		if conv.Parent.DateStr != day {
			day = conv.Parent.DateStr
			index.WriteString("\n## " + day + "\n\n")
		}
		name := conversationFilename(conv.Parent.TS, ch.Window)
		content := conversationMarkdown(ch.Name, conv, userNames, channelNames)
		if err := os.WriteFile(filepath.Join(dir, name), []byte(content), 0o644); err != nil {
			return "", fmt.Errorf("writing conversation %s: %w", conv.Parent.TS, err)
		}
		index.WriteString(indexEntry(conv, name, userNames, channelNames))
	}

	indexPath := filepath.Join(dir, indexFilename)
	if err := os.WriteFile(indexPath, []byte(index.String()), 0o644); err != nil {
		return "", fmt.Errorf("writing index: %w", err)
	}
	return indexPath, nil
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
// number so two messages posted in the same second stay distinct.
func conversationFilename(ts string, w Window) string {
	_, seq, _ := strings.Cut(ts, ".")
	stamp := "unknown"
	if t, ok := TimeFromTS(ts); ok {
		stamp = t.In(w.Location).Format("2006-01-02-150405")
	}
	return stamp + "-" + sanitizeForFilename(seq) + ".md"
}

// conversationMarkdown renders one conversation file. conv must already
// be stamped.
func conversationMarkdown(channelName string, conv Conversation, userNames, channelNames map[string]string) string {
	var b strings.Builder
	b.WriteString("# #" + channelName + " - " + conv.Parent.DateStr + " " + conv.Parent.Timestamp + "\n\n")
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
	label := conv.Parent.Timestamp + " " + conv.Parent.UserName + ": " + snippet(conv.Parent, userNames, channelNames)
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

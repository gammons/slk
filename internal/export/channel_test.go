package export

import (
	"errors"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"

	"github.com/gammons/slk/internal/ui/messages"
)

const testTZ = "America/New_York"

// testChannel is a three-conversation fixture: a standalone message, a
// thread, and an older thread kept only as context for its reply. They
// are listed out of order so sorting is exercised.
func testChannel(t *testing.T) Channel {
	t.Helper()
	return Channel{
		Workspace: "Example Workspace",
		Name:      "project_alpha",
		Window:    mustWindow(t, "2026-04-01", "2026-07-01", testTZ, 14),
		Conversations: []Conversation{
			{
				Parent: messages.MessageItem{TS: tsAt(t, testTZ, "2026-04-02 14:00:00"), UserID: "U2", UserName: "bob", Text: "who owns <#C9|deploys>?", ReplyCount: 2},
				Replies: []messages.MessageItem{
					{TS: tsAt(t, testTZ, "2026-04-02 14:05:00"), UserID: "U1", UserName: "alice", Text: "<@U3> does"},
					{TS: tsAt(t, testTZ, "2026-04-03 09:00:00"), UserID: "U3", UserName: "carol", Text: "confirmed"},
				},
			},
			{
				Parent: messages.MessageItem{TS: tsAt(t, testTZ, "2026-04-01 09:30:15"), UserID: "U1", UserName: "alice", Text: "standalone *note*"},
			},
			{
				Parent:          messages.MessageItem{TS: tsAt(t, testTZ, "2025-11-20 08:00:00"), UserID: "U3", UserName: "carol", Text: "old question", ReplyCount: 5},
				Replies:         []messages.MessageItem{{TS: tsAt(t, testTZ, "2026-05-01 10:00:00"), UserID: "U2", UserName: "bob", Text: "late answer"}},
				ParentIsContext: true,
			},
		},
	}
}

// writeTestChannel exports the fixture and returns the directory.
func writeTestChannel(t *testing.T) string {
	t.Helper()
	dir := filepath.Join(t.TempDir(), "out")
	userNames := map[string]string{"U1": "alice", "U2": "bob", "U3": "carol"}
	indexPath, err := WriteChannel(dir, testChannel(t), userNames, nil)
	if err != nil {
		t.Fatalf("WriteChannel: %v", err)
	}
	if want := filepath.Join(dir, "index.md"); indexPath != want {
		t.Fatalf("index path = %q, want %q", indexPath, want)
	}
	return dir
}

// readFile returns a file's content or fails the test.
func readFile(t *testing.T, path string) string {
	t.Helper()
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("reading %s: %v", path, err)
	}
	return string(data)
}

func TestWriteChannel_OneFilePerConversationPlusIndex(t *testing.T) {
	dir := writeTestChannel(t)
	entries, err := os.ReadDir(dir)
	if err != nil {
		t.Fatalf("ReadDir: %v", err)
	}
	var names []string
	for _, e := range entries {
		names = append(names, e.Name())
	}
	want := []string{
		"2025-11-20-080000-000100.md",
		"2026-04-01-093015-000100.md",
		"2026-04-02-140000-000100.md",
		"index.md",
	}
	if strings.Join(names, ",") != strings.Join(want, ",") {
		t.Errorf("files = %v, want %v", names, want)
	}
	if siblings := dirNames(t, filepath.Dir(dir)); strings.Join(siblings, ",") != "out" {
		t.Errorf("staging directory left beside the export: %v", siblings)
	}
}

// dirNames lists dir's entry names in order.
func dirNames(t *testing.T, dir string) []string {
	t.Helper()
	entries, err := os.ReadDir(dir)
	if err != nil {
		t.Fatalf("ReadDir(%s): %v", dir, err)
	}
	var names []string
	for _, e := range entries {
		names = append(names, e.Name())
	}
	return names
}

func TestWriteChannel_FailedWriteLeavesDirEmptyForRetry(t *testing.T) {
	// A 300-digit sequence number makes a filename longer than any
	// filesystem allows, so the second conversation's write fails after
	// the first has already been written.
	bad := strings.TrimSuffix(tsAt(t, testTZ, "2026-04-01 10:00:00"), "000100") + strings.Repeat("9", 300)
	ch := Channel{
		Name:   "general",
		Window: mustWindow(t, "2026-04-01", "2026-04-02", testTZ, 0),
		Conversations: []Conversation{
			{Parent: messages.MessageItem{TS: tsAt(t, testTZ, "2026-04-01 09:00:00"), UserName: "alice", Text: "fine"}},
			{Parent: messages.MessageItem{TS: bad, UserName: "bob", Text: "unwritable"}},
		},
	}
	root := t.TempDir()
	dir := filepath.Join(root, "out")
	if _, err := WriteChannel(dir, ch, nil, nil); err == nil {
		t.Fatal("WriteChannel succeeded with an unwritable filename")
	}
	if got := dirNames(t, dir); len(got) != 0 {
		t.Errorf("failed export left files in the output directory: %v", got)
	}
	if got := dirNames(t, root); strings.Join(got, ",") != "out" {
		t.Errorf("failed export left a staging directory behind: %v", got)
	}

	ch.Conversations = ch.Conversations[:1]
	if _, err := WriteChannel(dir, ch, nil, nil); err != nil {
		t.Fatalf("retry into the same directory: %v", err)
	}
	if got := dirNames(t, dir); strings.Join(got, ",") != "2026-04-01-090000-000100.md,index.md" {
		t.Errorf("retry wrote %v", got)
	}
}

func TestWriteChannel_IntoCurrentDirectory(t *testing.T) {
	dir := t.TempDir()
	t.Chdir(dir)
	indexPath, err := WriteChannel(".", testChannel(t), nil, nil)
	if err != nil {
		t.Fatalf("WriteChannel: %v", err)
	}
	if indexPath != "index.md" {
		t.Errorf("index path = %q, want index.md", indexPath)
	}
	// Read through the absolute path: the test's own working directory
	// must still be the directory the files landed in.
	if got := dirNames(t, dir); len(got) != 4 {
		t.Errorf("files = %v, want 3 conversations plus index.md", got)
	}
	if got := readFile(t, "index.md"); !strings.Contains(got, "- Conversations: 3\n") {
		t.Errorf("index not readable relative to the working directory:\n%s", got)
	}
	if got := dirNames(t, filepath.Dir(dir)); slices.ContainsFunc(got, func(n string) bool { return strings.Contains(n, ".partial-") }) {
		t.Errorf("staging directory left beside the export: %v", got)
	}
}

func TestWriteChannel_IndexHeader(t *testing.T) {
	index := readFile(t, filepath.Join(writeTestChannel(t), "index.md"))
	for _, want := range []string{
		"# #project_alpha\n",
		"- Workspace: Example Workspace\n",
		"- Range: 2026-04-01 to 2026-07-01 (end date excluded)\n",
		"- Overlap: 14 days, widening the range to 2026-03-18 to 2026-07-15\n",
		"- Timezone: America/New_York\n",
		"- Conversations: 3\n",
		// 3 + 1 + 1: the context-only parent is not an exported message.
		"- Messages: 5\n",
	} {
		if !strings.Contains(index, want) {
			t.Errorf("index missing %q, got:\n%s", want, index)
		}
	}
}

func TestWriteChannel_IndexOmitsOverlapLineWhenZero(t *testing.T) {
	ch := testChannel(t)
	ch.Window = mustWindow(t, "2026-04-01", "2026-07-01", testTZ, 0)
	dir := filepath.Join(t.TempDir(), "out")
	if _, err := WriteChannel(dir, ch, nil, nil); err != nil {
		t.Fatalf("WriteChannel: %v", err)
	}
	if index := readFile(t, filepath.Join(dir, "index.md")); strings.Contains(index, "Overlap") {
		t.Errorf("index has an Overlap line for a zero overlap:\n%s", index)
	}
}

func TestWriteChannel_IndexLinksConversationsByDayInOrder(t *testing.T) {
	index := readFile(t, filepath.Join(writeTestChannel(t), "index.md"))
	ordered := []string{
		"## 2025-11-20\n",
		"- [08:00 carol: old question](2025-11-20-080000-000100.md) - 1 reply, started before the range\n",
		"## 2026-04-01\n",
		"- [09:30 alice: standalone *note*](2026-04-01-093015-000100.md)\n",
		"## 2026-04-02\n",
		"- [14:00 bob: who owns #deploys?](2026-04-02-140000-000100.md) - 2 replies\n",
	}
	pos := 0
	for _, want := range ordered {
		i := strings.Index(index[pos:], want)
		if i < 0 {
			t.Fatalf("index missing %q after offset %d, got:\n%s", want, pos, index)
		}
		pos += i + len(want)
	}
}

func TestWriteChannel_ConversationFile(t *testing.T) {
	got := readFile(t, filepath.Join(writeTestChannel(t), "2026-04-02-140000-000100.md"))
	for _, want := range []string{
		"# #project_alpha - 2026-04-02 14:00\n",
		"[Back to index](index.md)\n",
		"**bob** — 2026-04-02 14:00\nwho owns #deploys?\n",
		"**alice** — 2026-04-02 14:05\n@carol does\n",
		"**carol** — 2026-04-03 09:00\nconfirmed\n",
	} {
		if !strings.Contains(got, want) {
			t.Errorf("conversation missing %q, got:\n%s", want, got)
		}
	}
	if strings.Contains(got, "predates the export range") {
		t.Errorf("in-range conversation carries the context note:\n%s", got)
	}
}

func TestWriteChannel_ContextParentIsLabelled(t *testing.T) {
	got := readFile(t, filepath.Join(writeTestChannel(t), "2025-11-20-080000-000100.md"))
	note := strings.Index(got, "> The first message predates the export range")
	parent := strings.Index(got, "**carol** — 2025-11-20 08:00\nold question\n")
	reply := strings.Index(got, "**bob** — 2026-05-01 10:00\nlate answer\n")
	if note < 0 || parent < 0 || reply < 0 {
		t.Fatalf("missing note, parent or reply, got:\n%s", got)
	}
	if note >= parent || parent >= reply {
		t.Errorf("want note, then parent, then reply; got offsets %d, %d, %d", note, parent, reply)
	}
}

func TestWriteChannel_TimesFollowTheWindowTimezone(t *testing.T) {
	ch := Channel{
		Name:   "general",
		Window: mustWindow(t, "2026-04-01", "2026-04-02", "Asia/Tokyo", 0),
		// 23:30 on Mar 31 in New York is 12:30 on Apr 1 in Tokyo.
		Conversations: []Conversation{{Parent: messages.MessageItem{TS: tsAt(t, testTZ, "2026-03-31 23:30:00"), UserName: "alice", Text: "hi", DateStr: "stale", Timestamp: "stale"}}},
	}
	dir := filepath.Join(t.TempDir(), "out")
	if _, err := WriteChannel(dir, ch, nil, nil); err != nil {
		t.Fatalf("WriteChannel: %v", err)
	}
	got := readFile(t, filepath.Join(dir, "2026-04-01-123000-000100.md"))
	if !strings.Contains(got, "**alice** — 2026-04-01 12:30\n") {
		t.Errorf("message not stamped in Asia/Tokyo, got:\n%s", got)
	}
	if strings.Contains(got, "stale") {
		t.Errorf("caller-supplied display time leaked through:\n%s", got)
	}
}

func TestWriteChannel_SameSecondParentsGetDistinctFiles(t *testing.T) {
	base := strings.TrimSuffix(tsAt(t, testTZ, "2026-04-01 09:00:00"), "000100")
	ch := Channel{
		Name:   "general",
		Window: mustWindow(t, "2026-04-01", "2026-04-02", testTZ, 0),
		Conversations: []Conversation{
			{Parent: messages.MessageItem{TS: base + "000200", UserName: "bob", Text: "second"}},
			{Parent: messages.MessageItem{TS: base + "000100", UserName: "alice", Text: "first"}},
		},
	}
	dir := filepath.Join(t.TempDir(), "out")
	if _, err := WriteChannel(dir, ch, nil, nil); err != nil {
		t.Fatalf("WriteChannel: %v", err)
	}
	for name, want := range map[string]string{"2026-04-01-090000-000100.md": "first", "2026-04-01-090000-000200.md": "second"} {
		if got := readFile(t, filepath.Join(dir, name)); !strings.Contains(got, want) {
			t.Errorf("%s missing %q, got:\n%s", name, want, got)
		}
	}
}

func TestWriteChannel_UnparseableTimestampsGetDistinctFiles(t *testing.T) {
	ch := Channel{
		Name:   "general",
		Window: mustWindow(t, "2026-04-01", "2026-04-02", testTZ, 0),
		Conversations: []Conversation{
			{Parent: messages.MessageItem{TS: "", UserName: "a", Text: "FIRST"}},
			{Parent: messages.MessageItem{TS: "garbage", UserName: "b", Text: "SECOND"}},
		},
	}
	dir := filepath.Join(t.TempDir(), "out")
	if _, err := WriteChannel(dir, ch, nil, nil); err != nil {
		t.Fatalf("WriteChannel: %v", err)
	}
	for name, want := range map[string]string{"unknown-unknown.md": "FIRST", "unknown-unknown-2.md": "SECOND"} {
		got := readFile(t, filepath.Join(dir, name))
		if !strings.Contains(got, want) {
			t.Errorf("%s missing %q, got:\n%s", name, want, got)
		}
		if !strings.HasPrefix(got, "# #general\n\n") {
			t.Errorf("%s title carries a blank stamp, got:\n%s", name, got)
		}
	}
	index := readFile(t, filepath.Join(dir, "index.md"))
	for _, want := range []string{
		"\n## Unknown date\n\n",
		"- [a: FIRST](unknown-unknown.md)\n",
		"- [b: SECOND](unknown-unknown-2.md)\n",
	} {
		if !strings.Contains(index, want) {
			t.Errorf("index missing %q, got:\n%s", want, index)
		}
	}
}

func TestUniqueFilename(t *testing.T) {
	used := map[string]bool{}
	for i, want := range []string{"a.md", "a-2.md", "a-3.md"} {
		if got := uniqueFilename("a.md", used); got != want {
			t.Errorf("call %d = %q, want %q", i+1, got, want)
		}
	}
	if got := uniqueFilename("b.md", used); got != "b.md" {
		t.Errorf("unused name = %q, want b.md", got)
	}
}

func TestWriteChannel_EmptyExportStillWritesIndex(t *testing.T) {
	ch := Channel{Name: "quiet", Window: mustWindow(t, "2026-04-01", "2026-04-02", testTZ, 0)}
	dir := filepath.Join(t.TempDir(), "out")
	if _, err := WriteChannel(dir, ch, nil, nil); err != nil {
		t.Fatalf("WriteChannel: %v", err)
	}
	index := readFile(t, filepath.Join(dir, "index.md"))
	if !strings.Contains(index, "- Conversations: 0\n") || !strings.Contains(index, "- Messages: 0\n") {
		t.Errorf("empty index lacks zero totals:\n%s", index)
	}
}

func TestWriteChannel_RefusesNonEmptyDir(t *testing.T) {
	dir := t.TempDir()
	keep := filepath.Join(dir, "notes.txt")
	if err := os.WriteFile(keep, []byte("mine"), 0o644); err != nil {
		t.Fatal(err)
	}
	_, err := WriteChannel(dir, testChannel(t), nil, nil)
	if !errors.Is(err, ErrOutputNotEmpty) {
		t.Fatalf("err = %v, want ErrOutputNotEmpty", err)
	}
	entries, _ := os.ReadDir(dir)
	if len(entries) != 1 || readFile(t, keep) != "mine" {
		t.Errorf("refused export still touched the directory: %v", entries)
	}
}

func TestEnsureEmptyDir(t *testing.T) {
	root := t.TempDir()
	if err := EnsureEmptyDir(root); err != nil {
		t.Errorf("existing empty dir: %v", err)
	}
	nested := filepath.Join(root, "a", "b")
	if err := EnsureEmptyDir(nested); err != nil {
		t.Fatalf("missing dir: %v", err)
	}
	if info, err := os.Stat(nested); err != nil || !info.IsDir() {
		t.Errorf("missing dir was not created: %v", err)
	}
	// root now holds "a".
	if err := EnsureEmptyDir(root); !errors.Is(err, ErrOutputNotEmpty) {
		t.Errorf("non-empty dir: err = %v, want ErrOutputNotEmpty", err)
	}
}

func TestDefaultChannelDir(t *testing.T) {
	t.Setenv("XDG_DATA_HOME", "/data")
	w := mustWindow(t, "2026-04-01", "2026-07-01", testTZ, 14)
	got, err := DefaultChannelDir("project alpha", w)
	if err != nil {
		t.Fatalf("DefaultChannelDir: %v", err)
	}
	// Named for the requested range, not the overlap-widened one.
	want := filepath.Join("/data", "slk", "exports", "slk-channel-project-alpha-2026-04-01-to-2026-07-01")
	if got != want {
		t.Errorf("dir = %q, want %q", got, want)
	}
}

func TestSnippet(t *testing.T) {
	cases := []struct {
		name string
		msg  messages.MessageItem
		want string
	}{
		{"first non-blank line", messages.MessageItem{Text: "\n\n  hello  \nworld"}, "hello"},
		{"entities flattened", messages.MessageItem{Text: "<https://example.com|docs> &amp; <@U1>"}, "docs & @alice"},
		{"no text", messages.MessageItem{}, "(no text)"},
		{"truncated by runes", messages.MessageItem{Text: strings.Repeat("é", 100)}, strings.Repeat("é", 80) + "..."},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := snippet(tc.msg, map[string]string{"U1": "alice"}, nil); got != tc.want {
				t.Errorf("snippet = %q, want %q", got, tc.want)
			}
		})
	}
}

func TestIndexEntry_EscapesLinkLabel(t *testing.T) {
	conv := Conversation{Parent: messages.MessageItem{UserName: "alice", Timestamp: "09:00", Text: `[WIP] fix a\b`}}
	got := indexEntry(conv, "f.md", nil, nil)
	if want := `- [09:00 alice: \[WIP\] fix a\\b](f.md)` + "\n"; got != want {
		t.Errorf("entry = %q, want %q", got, want)
	}
}

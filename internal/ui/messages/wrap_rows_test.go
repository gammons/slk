package messages

import (
	"bytes"
	"fmt"
	goimage "image"
	"strings"
	"testing"
	"time"

	"charm.land/lipgloss/v2"
	"github.com/charmbracelet/x/ansi"
	"github.com/gammons/slk/internal/bubbles/entrylist"
	emojiutil "github.com/gammons/slk/internal/emoji"
	imgpkg "github.com/gammons/slk/internal/image"
	"github.com/gammons/slk/internal/ui/peerstatus"
)

func TestWrapRows_LongTimestampOnlyOnSelected(t *testing.T) {
	pinLongTSClock(t)
	items := longTSItems()
	m := New(items, "general")
	const width = 40
	r := RenderMessage(items[0], width, m.renderContext(), RenderOptions{})
	w := WrapRows(items[0], r, width, NewWrapStyles(true, false))

	if len(w.Lines) != len(w.Selected) || len(w.Lines) != len(w.Plain) {
		t.Fatalf("variants differ in height: lines=%d selected=%d plain=%d", len(w.Lines), len(w.Selected), len(w.Plain))
	}
	if w.ShortSrc != r.Content {
		t.Errorf("ShortSrc should be the short-timestamp content when the selected header changes")
	}
	if got := ansi.Strip(strings.Join(w.Lines, "\n")); strings.Contains(got, "Tue Sep 29") {
		t.Errorf("unselected rows carry the long timestamp:\n%s", got)
	}
	if got := ansi.Strip(strings.Join(w.Selected, "\n")); !strings.Contains(got, "Tue Sep 29, 3:40 PM") {
		t.Errorf("selected rows lack the long timestamp:\n%s", got)
	}
	for i, p := range w.Plain {
		if strings.Contains(p.Text, "▌") {
			t.Errorf("plain row %d carries the border column: %q", i, p.Text)
		}
	}
	short := NewWrapStyles(true, false).SelectedVariant(w.ShortSrc, width)
	if len(short) != len(w.Selected) {
		t.Errorf("short variant height %d != selected %d", len(short), len(w.Selected))
	}
}

func TestWrapRows_OnlyShortTimestampWhenCompact(t *testing.T) {
	pinLongTSClock(t)
	items := longTSItems()
	m := New(items, "general")
	const width = 40
	r := RenderMessage(items[0], width, m.renderContext(), RenderOptions{})
	w := WrapRows(items[0], r, width, NewWrapStyles(true, true))

	if got := ansi.Strip(strings.Join(w.Selected, "\n")); strings.Contains(got, "Tue Sep 29") {
		t.Errorf("selected rows carry the long timestamp in compact mode:\n%s", got)
	}
}

func TestWrapRows_NoShortSrcWithoutLongTimestamp(t *testing.T) {
	pinLongTSClock(t)
	items := []MessageItem{{TS: "", UserID: "U1", UserName: "priya", Text: "hi", Timestamp: "3:40 PM"}}
	m := New(items, "general")
	r := RenderMessage(items[0], 40, m.renderContext(), RenderOptions{})
	if w := WrapRows(items[0], r, 40, NewWrapStyles(false, false)); w.ShortSrc != "" {
		t.Errorf("ShortSrc = %q, want empty when the selected header is unchanged", w.ShortSrc)
	}
}

func TestWrapRows_LongAuthorKeepsAvatarRowsAdjacent(t *testing.T) {
	const width = 44
	msg := MessageItem{
		TS:        "1700000000.000000",
		UserID:    "U_FIXTURE",
		UserName:  "Long Display Name (weekdays, 11am-8pm Pacific Time)",
		Timestamp: "2:12 AM",
		Text:      "body",
	}
	m := New([]MessageItem{msg}, "fixture")
	rendered := RenderMessage(msg, width, m.renderContext(), RenderOptions{Avatar: "TOP!\nLOW!"})
	header := strings.Join(strings.Fields(ansi.Strip(rendered.Header)), " ")
	if !strings.Contains(header, msg.UserName) || strings.Contains(header, "…") {
		t.Fatalf("author name was shortened instead of wrapped: %q", header)
	}
	headerRows := lipgloss.Height(rendered.Header)
	if headerRows < 2 {
		t.Fatalf("long author header did not wrap: %q", header)
	}
	wrapped := WrapRows(msg, rendered, width, NewWrapStyles(true, false))
	for _, tc := range []struct {
		name string
		rows []string
	}{
		{"normal", wrapped.Lines},
		{"selected", wrapped.Selected},
	} {
		t.Run(tc.name, func(t *testing.T) {
			top, bottom := ansi.Strip(tc.rows[0]), ansi.Strip(tc.rows[1])
			topCol, bottomCol := strings.Index(top, "TOP!"), strings.Index(bottom, "LOW!")
			if topCol < 0 || bottomCol != topCol || strings.Contains(bottom, "body") {
				t.Fatalf("wrapped author split the avatar or displaced the body:\n%s", ansi.Strip(strings.Join(tc.rows, "\n")))
			}
			if !strings.Contains(ansi.Strip(tc.rows[headerRows]), "body") {
				t.Fatalf("body did not follow the full header:\n%s", ansi.Strip(strings.Join(tc.rows, "\n")))
			}
			canvas := lipgloss.NewCanvas(width, len(tc.rows))
			canvas.Compose(lipgloss.NewLayer(strings.Join(tc.rows, "\n")))
			first, continuation := canvas.CellAt(topCol+5, 0), canvas.CellAt(topCol+5, 1)
			if first == nil || continuation == nil || first.Style.Fg == nil || continuation.Style.Fg == nil {
				t.Fatal("wrapped author name lost its styling beside the avatar")
			}
			r0, g0, b0, a0 := first.Style.Fg.RGBA()
			r1, g1, b1, a1 := continuation.Style.Fg.RGBA()
			if r0 != r1 || g0 != g1 || b0 != b1 || a0 != a1 {
				t.Fatal("author continuation uses a different foreground")
			}
		})
	}
}

func TestWrapRows_WrappedHeaderKeepsTimestampWholeForSelection(t *testing.T) {
	pinLongTSClock(t)
	const width = 27
	msg := MessageItem{
		TS:        longTSAt(time.Date(2026, 9, 29, 2, 12, 0, 0, time.Local)),
		UserID:    "U_FIXTURE",
		UserName:  "abcdefghijklmno",
		Timestamp: "2:12 AM",
		Text:      "body",
		Reactions: []ReactionItem{{Emoji: "thumbsup", Count: 7}},
	}
	m := New([]MessageItem{msg}, "fixture")
	rendered := RenderMessage(msg, width, m.renderContext(), RenderOptions{Avatar: "TOP!\nLOW!"})
	if header := ansi.Strip(rendered.Header); !strings.Contains(header, msg.Timestamp) {
		t.Errorf("formatted timestamp split across header rows: %q", header)
	}
	wrapped := WrapRows(msg, rendered, width, NewWrapStyles(true, false))
	selected := ansi.Strip(strings.Join(wrapped.Selected, "\n"))
	if !strings.Contains(selected, LongTimestamp(msg.TS, msg.Timestamp)) {
		t.Errorf("selected header lost its fitting date-qualified timestamp:\n%s", selected)
	}
	if len(wrapped.Selected) != len(wrapped.Lines) {
		t.Fatalf("selection changed message height: normal=%d selected=%d", len(wrapped.Lines), len(wrapped.Selected))
	}
	headerRows := lipgloss.Height(rendered.Header)
	for _, rows := range [][]string{wrapped.Lines, wrapped.Selected} {
		top, bottom := ansi.Strip(rows[0]), ansi.Strip(rows[1])
		if col := strings.Index(top, "TOP!"); col < 0 || strings.Index(bottom, "LOW!") != col {
			t.Errorf("timestamp layout split the avatar:\n%s", ansi.Strip(strings.Join(rows, "\n")))
		}
		if !strings.Contains(ansi.Strip(rows[headerRows]), "body") {
			t.Errorf("selection moved the body below the header:\n%s", ansi.Strip(strings.Join(rows, "\n")))
		}
		for _, row := range rows {
			if lipgloss.Width(row) > width {
				t.Errorf("timestamp layout exceeded pane width: %q", ansi.Strip(row))
			}
		}
	}
	reactionFound := false
	for _, hit := range rendered.Hits {
		if hit.Kind != entrylist.HitReaction {
			continue
		}
		reactionFound = true
		for _, rows := range [][]string{wrapped.Lines, wrapped.Selected} {
			if hit.RowStart < 0 || hit.RowStart >= len(rows) || !strings.Contains(ansi.Strip(rows[hit.RowStart]), "7") {
				t.Errorf("reaction hit moved off its row after timestamp layout: row=%d", hit.RowStart)
			}
		}
	}
	if !reactionFound {
		t.Fatal("reaction must remain reachable below the wrapped header")
	}
}

func TestWrapRows_WrappedAvatarHeaderKeepsStatusImageAndTimestamp(t *testing.T) {
	pinLongTSClock(t)
	t.Setenv("TMUX", "")
	wasActive, wasCells := emojiutil.ImageModeActive(), emojiutil.ImageModeCells()
	emojiutil.SetImageMode(true, 2)
	t.Cleanup(func() { emojiutil.SetImageMode(wasActive, wasCells) })

	const width = 27
	const imageURL = "https://example.test/wrapped-header-status.png"
	key := emojiutil.EmojiCacheKey(imageURL)
	kitty := imgpkg.NewKittyRenderer(imgpkg.NewRegistry())
	kitty.SetSource(key, goimage.NewRGBA(goimage.Rect(0, 0, 2, 2)))
	placed := kitty.RenderKey(key, goimage.Pt(2, 1))
	fetcher := newFakePlaceFetcher()
	fetcher.setPrerendered(key, goimage.Pt(2, 1), placed)
	msg := MessageItem{
		TS:        longTSAt(time.Date(2026, 9, 29, 2, 12, 0, 0, time.Local)),
		UserID:    "U_FIXTURE",
		UserName:  "Long Display Name (weekdays, 11am-8pm Pacific Time)",
		Timestamp: "2:12 AM",
		Text:      "body",
	}
	m := New([]MessageItem{msg}, "fixture")
	m.SetUserStatuses(map[string]peerstatus.Status{msg.UserID: {Emoji: ":pager:", Text: "On call"}})
	m.SetEmojiContext(EmojiContext{
		PlaceCtx: emojiutil.PlaceContext{Fetcher: fetcher}, Cells: 2,
		Customs: map[string]string{"pager": imageURL},
	})
	rendered := RenderMessage(msg, width, m.renderContext(), RenderOptions{Avatar: "TOP!\nLOW!"})
	if header := ansi.Strip(rendered.Header); !strings.Contains(header, msg.Timestamp) {
		t.Fatalf("formatted timestamp split across header rows: %q", header)
	}
	wrapped := WrapRows(msg, rendered, width, NewWrapStyles(true, false))
	if len(wrapped.Lines) != len(wrapped.Selected) {
		t.Fatalf("selection changed message height: normal=%d selected=%d", len(wrapped.Lines), len(wrapped.Selected))
	}
	headerRows := lipgloss.Height(rendered.Header)
	longTimestamp := LongTimestamp(msg.TS, msg.Timestamp)
	for _, tc := range []struct {
		name     string
		rows     []string
		selected bool
	}{
		{"normal", wrapped.Lines, false},
		{"selected", wrapped.Selected, true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			frame := strings.Join(tc.rows, "\n")
			plain := ansi.Strip(frame)
			if strings.Contains(plain, longTimestamp) != tc.selected {
				t.Fatalf("date-qualified timestamp selected=%v:\n%s", tc.selected, plain)
			}
			top, bottom := ansi.Strip(tc.rows[0]), ansi.Strip(tc.rows[1])
			topCol := strings.Index(top, "TOP!")
			if topCol < 0 || strings.Index(bottom, "LOW!") != topCol {
				t.Fatalf("wrapped header split the avatar:\n%s", plain)
			}
			if headerRows >= len(tc.rows) || !strings.Contains(ansi.Strip(tc.rows[headerRows]), msg.Text) {
				t.Fatalf("body did not follow the complete header:\n%s", plain)
			}
			for _, row := range tc.rows {
				if lipgloss.Width(row) > width {
					t.Fatalf("message row exceeded pane width: %q", ansi.Strip(row))
				}
			}
			canvas := lipgloss.NewCanvas(width, len(tc.rows))
			canvas.Compose(lipgloss.NewLayer(frame))
			var author strings.Builder
			var statusCells []goimage.Point
			for y := range headerRows {
				for x := 6; x < width; x++ {
					cell := canvas.CellAt(x, y)
					if cell == nil {
						continue
					}
					author.WriteString(cell.Content)
					if !strings.HasPrefix(cell.Content, string(imgpkg.PlaceholderRune)) {
						continue
					}
					statusCells = append(statusCells, goimage.Pt(x, y))
					if cell.Style.Fg == nil {
						t.Fatal("status image lost its image ID")
					}
					r, g, b, _ := cell.Style.Fg.RGBA()
					if got := (r>>8)<<16 | (g>>8)<<8 | b>>8; got != placed.ID {
						t.Fatalf("status references image %d, want %d", got, placed.ID)
					}
				}
				author.WriteByte(' ')
			}
			name := strings.Join(strings.Fields(author.String()), " ")
			if !strings.Contains(name, msg.UserName) || strings.Contains(name, "…") {
				t.Fatalf("wrapped author was shortened: %q", name)
			}
			if strings.Contains(plain, "📟") {
				t.Fatal("warm status image fell back to the pager glyph")
			}
			if len(statusCells) != 2 || statusCells[0].Y == 0 ||
				statusCells[1] != statusCells[0].Add(goimage.Pt(1, 0)) {
				t.Fatalf("status must retain two adjacent cells on a continuation row: %v", statusCells)
			}
		})
	}
	var uploads bytes.Buffer
	for _, flush := range rendered.BodyFlushes {
		if err := flush(&uploads); err != nil {
			t.Fatal(err)
		}
	}
	if want := fmt.Sprintf("\x1b_Ga=T,f=100,t=d,i=%d,", placed.ID); !strings.Contains(uploads.String(), want) {
		t.Fatalf("status image was not uploaded for its placement: %q", uploads.String())
	}
}

func TestWrapRows_OverlongTimestampStaysWithinHeaderBudget(t *testing.T) {
	const width = 24
	msg := MessageItem{
		TS: "1.0", UserID: "U_FIXTURE", UserName: "sam", Text: "body",
		Timestamp: "2:12 AM Pacific Standard Time", IsEdited: true,
	}
	m := New([]MessageItem{msg}, "fixture")
	rendered := RenderMessage(msg, width, m.renderContext(), RenderOptions{
		Avatar: "TOP!\nLOW!", EditedMark: true,
	})
	header := strings.Join(strings.Fields(ansi.Strip(rendered.Header)), " ")
	for _, want := range []string{msg.UserName, msg.Timestamp, "(edited)"} {
		if !strings.Contains(header, want) {
			t.Errorf("overlong timestamp header lost %q: %q", want, header)
		}
	}
	for _, row := range strings.Split(rendered.Header, "\n") {
		if lipgloss.Width(row) > rendered.HeaderBudget {
			t.Errorf("overlong timestamp exceeded header budget: %q", ansi.Strip(row))
		}
	}
	wrapped := WrapRows(msg, rendered, width, NewWrapStyles(true, false))
	if len(wrapped.Lines) != len(wrapped.Selected) {
		t.Fatal("selection changed the overlong timestamp's row count")
	}
	for _, rows := range [][]string{wrapped.Lines, wrapped.Selected} {
		if !strings.Contains(ansi.Strip(rows[lipgloss.Height(rendered.Header)]), "body") {
			t.Errorf("body did not follow the overlong timestamp header: %q", ansi.Strip(strings.Join(rows, "\n")))
		}
		for _, row := range rows {
			if lipgloss.Width(row) > width {
				t.Errorf("overlong timestamp exceeded pane width: %q", ansi.Strip(row))
			}
		}
	}
}

func TestWrapRows_LongAuthorKeepsHeaderMetadata(t *testing.T) {
	const width = 44
	msg := MessageItem{
		TS:        "1700000000.000000",
		UserID:    "U_FIXTURE",
		UserName:  strings.Repeat("長い名前", 12),
		Timestamp: "2:12 AM",
		IsEdited:  true,
		Text:      "body",
	}
	m := New([]MessageItem{msg}, "fixture")
	m.SetUserStatuses(map[string]peerstatus.Status{"U_FIXTURE": {Emoji: ":pager:", Text: "On call"}})
	rendered := RenderMessage(msg, width, m.renderContext(), RenderOptions{Avatar: "TOP!\nLOW!", EditedMark: true})
	header := ansi.Strip(rendered.Header)
	if !strings.Contains(strings.ReplaceAll(header, "\n", ""), msg.UserName) || strings.Contains(header, "…") {
		t.Fatalf("wide-character name was shortened instead of wrapped: %q", header)
	}
	plain := strings.Join(strings.Fields(header), " ")
	for _, want := range []string{"📟", msg.Timestamp, "(edited)"} {
		if !strings.Contains(plain, want) {
			t.Errorf("wrapped header lost %q: %q", want, header)
		}
	}
	for _, row := range strings.Split(rendered.Header, "\n") {
		if lipgloss.Width(row) > rendered.HeaderBudget {
			t.Fatalf("author header row exceeds its width budget: %q", header)
		}
	}
	wrapped := WrapRows(msg, rendered, width, NewWrapStyles(true, false))
	for _, rows := range [][]string{wrapped.Lines, wrapped.Selected} {
		if !strings.Contains(ansi.Strip(rows[lipgloss.Height(rendered.Header)]), "body") {
			t.Fatalf("body did not follow the wrapped header: %q", ansi.Strip(strings.Join(rows, "\n")))
		}
		for _, row := range rows {
			if lipgloss.Width(row) > width {
				t.Fatalf("message row exceeds pane width: %q", ansi.Strip(row))
			}
		}
	}
}

func TestWrapRows_WrappedHeaderKeepsActionRowsAligned(t *testing.T) {
	const width = 44
	msg := MessageItem{
		TS:        "1700000000.000000",
		UserID:    "U_FIXTURE",
		UserName:  "Long Display Name (weekdays, 11am-8pm Pacific Time)",
		Timestamp: "2:12 AM",
		Text:      "body",
		Attachments: []Attachment{{
			FileID: "F_FIXTURE", Kind: "file", Name: "report.txt",
			URL: "https://example.test/report", DownloadURL: "https://example.test/report",
		}},
		ReplyCount: 2,
		Reactions:  []ReactionItem{{Emoji: "thumbsup", Count: 7}},
	}
	m := New([]MessageItem{msg}, "fixture")
	rendered := RenderMessage(msg, width, m.renderContext(), RenderOptions{
		Avatar: "TOP!\nLOW!", ImageHits: true, ReplyAffordance: true,
	})
	wrapped := WrapRows(msg, rendered, width, NewWrapStyles(true, false))
	foundFile, foundReaction := false, false
	for _, hits := range [][]entrylist.Hit{rendered.Hits, wrapped.LinkHits} {
		for _, hit := range hits {
			var want string
			switch hit.Kind {
			case entrylist.HitFile:
				want, foundFile = "report.txt", true
			case entrylist.HitReaction:
				want, foundReaction = "7", true
			default:
				continue
			}
			for _, rows := range [][]string{wrapped.Lines, wrapped.Selected} {
				if hit.RowStart < 0 || hit.RowStart >= len(rows) || !strings.Contains(ansi.Strip(rows[hit.RowStart]), want) {
					t.Fatalf("action hit points outside its rendered row: kind=%v row=%d", hit.Kind, hit.RowStart)
				}
			}
		}
	}
	if !foundFile || !foundReaction {
		t.Fatal("file and reaction actions must both remain reachable")
	}
}

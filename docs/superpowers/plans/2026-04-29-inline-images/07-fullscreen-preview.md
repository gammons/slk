# Phase 7: Full-Screen Preview Overlay

> Index: `00-overview.md`. Previous: `06-wire-kitty-sixel.md`. Next: `08-docs.md`.

**Goal:** Add a full-screen image preview overlay opened by mouse click on an inline image or by `O` on a message in normal mode. The overlay covers messages + thread panes; sidebar and status bar stay visible. `Esc`/`q` close; `Enter` opens in the system viewer.

**Spec sections covered:** Full-Screen Preview, Keybindings (additions).

---

## Task 7.1: `Preview` component

**Files:**
- Create: `internal/image/preview.go`
- Create: `internal/image/preview_test.go`

- [ ] **Step 1: Write the failing test**

Create `internal/image/preview_test.go`:

```go
package image

import (
	"image"
	imgcolor "image/color"
	"testing"
)

func TestPreview_RenderShape(t *testing.T) {
	p := NewPreview(PreviewInput{
		Name:   "screenshot.png",
		FileID: "F1",
		Img:    makeSolid(800, 600, imgcolor.RGBA{1, 2, 3, 255}),
	})
	out := p.View(60, 30, ProtoHalfBlock)
	if out == "" {
		t.Fatal("empty view")
	}
	// Caption row should mention the filename.
	if !contains(out, "screenshot.png") {
		t.Error("expected filename in caption")
	}
}

func TestPreview_Closed(t *testing.T) {
	p := Preview{}
	if !p.IsClosed() {
		t.Error("zero-value Preview should be closed")
	}
	p2 := NewPreview(PreviewInput{Name: "x", Img: makeSolid(2, 2, imgcolor.RGBA{0, 0, 0, 255})})
	if p2.IsClosed() {
		t.Error("constructed Preview should not be closed")
	}
}
```

- [ ] **Step 2: Implement**

Create `internal/image/preview.go`:

```go
package image

import (
	"fmt"
	"image"
	"strings"

	"github.com/charmbracelet/lipgloss/v2"
)

// PreviewInput is the data needed to construct a Preview overlay.
type PreviewInput struct {
	Name   string
	FileID string
	Img    image.Image
	// Path is the on-disk path used for system-viewer launch (Enter).
	Path string
}

// Preview is a stateful full-screen image overlay.
type Preview struct {
	open  bool
	name  string
	fid   string
	img   image.Image
	path  string
	w, h  int // last known render size
}

// NewPreview returns an open preview for the given image.
func NewPreview(in PreviewInput) Preview {
	return Preview{
		open: true,
		name: in.Name,
		fid:  in.FileID,
		img:  in.Img,
		path: in.Path,
	}
}

// IsClosed reports whether the preview is currently dismissed.
func (p Preview) IsClosed() bool { return !p.open }

// Close dismisses the preview.
func (p *Preview) Close() { p.open = false }

// Path returns the on-disk path of the previewed image.
func (p Preview) Path() string { return p.path }

// View renders the preview into a string of size width x height. proto is
// the active rendering protocol (kitty / sixel / halfblock).
func (p *Preview) View(width, height int, proto Protocol) string {
	if !p.open || width <= 0 || height <= 0 || p.img == nil {
		return ""
	}
	p.w, p.h = width, height

	// Reserve 1 row top + 1 row bottom for caption.
	imgRows := height - 2
	if imgRows < 1 {
		return ""
	}
	imgCols := width

	// Aspect-fit.
	srcW, srcH := p.img.Bounds().Dx(), p.img.Bounds().Dy()
	target := fitInto(srcW, srcH, imgCols, imgRows)

	// Render the image using the active protocol.
	render := RenderImage(proto, p.img, target)

	// Top caption.
	caption := fmt.Sprintf("%s  •  %dx%d", p.name, srcW, srcH)
	captionStyle := lipgloss.NewStyle().Faint(true).Width(width)

	// Compose rows.
	var b strings.Builder
	b.WriteString(captionStyle.Render(caption))
	b.WriteByte('\n')

	// Center the image horizontally within `width`.
	leftPad := (width - target.X) / 2
	pad := strings.Repeat(" ", leftPad)
	rightPad := strings.Repeat(" ", width-target.X-leftPad)

	// Vertically center within imgRows.
	topGap := (imgRows - target.Y) / 2
	for i := 0; i < topGap; i++ {
		b.WriteString(strings.Repeat(" ", width))
		b.WriteByte('\n')
	}
	for _, line := range render.Lines {
		b.WriteString(pad)
		b.WriteString(line)
		b.WriteString(rightPad)
		b.WriteByte('\n')
	}
	for i := 0; i < imgRows-target.Y-topGap; i++ {
		b.WriteString(strings.Repeat(" ", width))
		b.WriteByte('\n')
	}

	// Footer.
	hint := lipgloss.NewStyle().Faint(true).Render("Esc/q close  •  Enter open in system viewer")
	b.WriteString(hint)
	return b.String()
}

func fitInto(srcW, srcH, maxCols, maxRows int) image.Point {
	// In cell units. Use 2:1 cell aspect (cells are taller than wide).
	cellAspect := 2.0
	srcAspect := float64(srcW) / float64(srcH) / cellAspect
	w := maxCols
	h := int(float64(w) / srcAspect)
	if h > maxRows {
		h = maxRows
		w = int(float64(h) * srcAspect)
	}
	if w < 1 {
		w = 1
	}
	if h < 1 {
		h = 1
	}
	return image.Pt(w, h)
}
```

- [ ] **Step 3: Run tests**

Run: `go test ./internal/image/ -run TestPreview -v`
Expected: pass.

- [ ] **Step 4: Commit**

```bash
git add internal/image/preview.go internal/image/preview_test.go
git commit -m "feat(image): add full-screen Preview overlay component"
```

---

## Task 7.2: `OpenImagePreviewMsg` + app-level wiring

**Files:**
- Modify: `internal/ui/app.go`

- [ ] **Step 1: Add the message type and overlay field**

Near other tea.Msg types in `app.go`:

```go
// OpenImagePreviewMsg requests opening the preview overlay for a specific
// message attachment. Dispatched by the messages model on click or `O`.
type OpenImagePreviewMsg struct {
    Channel string
    TS      string
    AttIdx  int
}
```

On the App model:

```go
type App struct {
    // ...
    previewOverlay *imgpkg.Preview
}
```

- [ ] **Step 2: Update routing in `Update`**

```go
case OpenImagePreviewMsg:
    // Look up the attachment.
    msg := a.findMessage(msg.Channel, msg.TS)
    if msg == nil || msg.AttIdx >= len(msg.Attachments) {
        return a, nil
    }
    att := msg.Attachments[msg.AttIdx]
    // Pick the largest available thumb for preview quality.
    largest := imgpkg.ThumbSpec{}
    for _, t := range toImgThumbs(att.Thumbs) {
        if max(t.W, t.H) > max(largest.W, largest.H) {
            largest = t
        }
    }
    return a, func() tea.Msg {
        res, err := a.imageFetcher.Fetch(context.Background(), imgpkg.FetchRequest{
            Key:    att.FileID + "-preview",
            URL:    largest.URL,
            Target: image.Pt(largest.W, largest.H),
        })
        if err != nil {
            return statusbar.ShowError("preview: " + err.Error())
        }
        return previewLoadedMsg{Name: att.Name, FileID: att.FileID, Img: res.Img, Path: res.Source}
    }

case previewLoadedMsg:
    p := imgpkg.NewPreview(imgpkg.PreviewInput{
        Name: msg.Name, FileID: msg.FileID, Img: msg.Img, Path: msg.Path,
    })
    a.previewOverlay = &p
    return a, nil
```

Add the helper `previewLoadedMsg` struct (file-private) and the `findMessage` lookup (already likely exists in some form).

- [ ] **Step 3: Compose the overlay in `View()`**

In `App.View()`, after computing the messages+thread region, conditionally replace its rendered content with the overlay:

```go
if a.previewOverlay != nil && !a.previewOverlay.IsClosed() {
    overlayWidth := /* same width as messages+thread region */
    overlayHeight := /* same height */
    overlayContent := a.previewOverlay.View(overlayWidth, overlayHeight, a.imgProtocol)
    // Replace the messages+thread render with overlayContent.
    rightPane = overlayContent
}
```

The exact `App.View()` plumbing differs; locate the existing region composition and substitute.

- [ ] **Step 4: Handle close keys when overlay is open**

Top of `Update`:

```go
if a.previewOverlay != nil && !a.previewOverlay.IsClosed() {
    if km, ok := msg.(tea.KeyMsg); ok {
        switch km.String() {
        case "esc", "q":
            a.previewOverlay.Close()
            a.previewOverlay = nil
            return a, nil
        case "enter":
            path := a.previewOverlay.Path()
            a.previewOverlay.Close()
            a.previewOverlay = nil
            return a, openInSystemViewerCmd(path)
        }
        // Swallow other keys while overlay is open.
        return a, nil
    }
}
```

- [ ] **Step 5: Add system-viewer launcher**

```go
// openInSystemViewerCmd launches xdg-open / open / start asynchronously.
func openInSystemViewerCmd(path string) tea.Cmd {
    return func() tea.Msg {
        var cmd *exec.Cmd
        switch runtime.GOOS {
        case "darwin":
            cmd = exec.Command("open", path)
        case "windows":
            cmd = exec.Command("rundll32", "url.dll,FileProtocolHandler", path)
        default:
            cmd = exec.Command("xdg-open", path)
        }
        if err := cmd.Start(); err != nil {
            return statusbar.ShowError("open: " + err.Error())
        }
        return nil
    }
}
```

Add imports: `"os/exec"`, `"runtime"`.

- [ ] **Step 6: Verify build**

Run: `go build ./...`

- [ ] **Step 7: Commit**

```bash
git add internal/ui/app.go
git commit -m "feat(app): add full-screen preview overlay with Esc/Enter handling"
```

---

## Task 7.3: `O` keybind in messages model

**Files:**
- Modify: `internal/ui/messages/model.go`

- [ ] **Step 1: Add to the normal-mode key handler**

Locate the existing key-handling switch in `Update` (or wherever message-mode keys are routed). Add:

```go
case "O":
    if len(m.messages) == 0 {
        return m, nil
    }
    msg := m.messages[m.selected]
    for i, att := range msg.Attachments {
        if att.Kind == "image" {
            return m, func() tea.Msg {
                return OpenImagePreviewMsg{
                    Channel: m.channel, TS: msg.TS, AttIdx: i,
                }
            }
        }
    }
    return m, nil
```

- [ ] **Step 2: Test**

Append to `model_test.go`:

```go
func TestOKey_DispatchesOpenImagePreviewMsg(t *testing.T) {
    m := newTestModel(t)
    m.SetMessages([]MessageItem{{
        TS: "1.001", Attachments: []Attachment{{Kind: "image", FileID: "F1"}},
    }})
    _, cmd := m.Update(tea.KeyMsg{/* O */})
    if cmd == nil {
        t.Fatal("expected cmd")
    }
    msg := cmd()
    op, ok := msg.(OpenImagePreviewMsg)
    if !ok {
        t.Fatalf("got %T, want OpenImagePreviewMsg", msg)
    }
    if op.TS != "1.001" || op.AttIdx != 0 {
        t.Errorf("payload mismatch: %+v", op)
    }
}
```

- [ ] **Step 3: Run**

Run: `go test ./internal/ui/messages/... -v`
Expected: pass.

- [ ] **Step 4: Commit**

```bash
git add internal/ui/messages/model.go internal/ui/messages/model_test.go
git commit -m "feat(messages): add O keybind to open image preview"
```

---

## Task 7.4: Mouse click → preview

**Files:**
- Modify: `internal/ui/messages/model.go`

- [ ] **Step 1: Wire mouse handling**

Locate the existing mouse handler in the messages model (drag-to-copy lives there). Add a precedence check:

```go
case tea.MouseMsg:
    if msg.Action == tea.MouseActionPress && msg.Button == tea.MouseButtonLeft {
        // Adjust (row, col) to be relative to the messages-pane viewport.
        row, col := /* convert msg.Y, msg.X to viewport coords */
        if msgIdx, attIdx, fid, hit := m.HitTest(row, col); hit {
            return m, func() tea.Msg {
                return OpenImagePreviewMsg{
                    Channel: m.channel,
                    TS:      m.messages[msgIdx].TS,
                    AttIdx:  attIdx,
                }
            }
            _ = fid
        }
        // Fall through to the existing drag-to-copy press handler.
    }
    // ... existing drag-to-copy logic ...
```

- [ ] **Step 2: Test**

Append to `model_test.go`:

```go
func TestMouseClick_OnImage_DispatchesOpenPreview(t *testing.T) {
    m := newTestModel(t)
    // Place an image at known coordinates.
    // ...
    _ = m.View(20, 80)

    _, cmd := m.Update(tea.MouseMsg{
        Action: tea.MouseActionPress, Button: tea.MouseButtonLeft,
        X: /* col inside image */, Y: /* row inside image */,
    })
    if cmd == nil {
        t.Fatal("expected cmd")
    }
    if _, ok := cmd().(OpenImagePreviewMsg); !ok {
        t.Errorf("expected OpenImagePreviewMsg")
    }
}
```

- [ ] **Step 3: Run + commit**

```bash
go test ./internal/ui/messages/... -v
git add internal/ui/messages/
git commit -m "feat(messages): mouse click on inline image opens preview"
```

---

## Phase 7 done

Click an inline image or press `O` on a message — full-screen preview opens. `Esc` / `q` closes; `Enter` launches the OS image viewer. Sidebar + status bar remain visible behind the overlay.

**Verify:**
```bash
go test ./... -v
```

Manual: open slk in kitty / a sixel terminal / a halfblock terminal. Click an image → preview. Press `Esc` → close. Press `O` on a message with an image → preview. Press `Enter` from the preview → image viewer launches.

Continue to `08-docs.md`.

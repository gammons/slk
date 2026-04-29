package ui

import (
	"bytes"
	"log"
	"os"
	"os/exec"

	"golang.design/x/clipboard"
)

// IsWayland reports whether the runtime is a Wayland session.
//
// We treat WAYLAND_DISPLAY being set as the authoritative signal.
// (DISPLAY may also be set on Wayland sessions via XWayland, so DISPLAY
// alone does not mean X11.)
func IsWayland() bool {
	return os.Getenv("WAYLAND_DISPLAY") != ""
}

// HasWlPaste reports whether the wl-paste binary is on PATH. Required
// for the Wayland clipboard reader.
func HasWlPaste() bool {
	_, err := exec.LookPath("wl-paste")
	return err == nil
}

// WaylandClipboardReader returns a clipboardReader that shells out to
// `wl-paste` to read the Wayland compositor's clipboard. The native
// X11-based golang.design/x/clipboard library does not see images
// placed on the clipboard by Wayland-native applications even with
// XWayland active, so we bypass it entirely on Wayland sessions.
func WaylandClipboardReader() clipboardReader {
	return func(format clipboard.Format) []byte {
		switch format {
		case clipboard.FmtImage:
			return wlPasteBytes("image/png")
		case clipboard.FmtText:
			// Try utf-8 text explicitly, then fall back to whatever
			// wl-paste's default type happens to be.
			if b := wlPasteBytes("text/plain;charset=utf-8"); len(b) > 0 {
				return b
			}
			return wlPasteBytes("")
		}
		return nil
	}
}

// wlPasteBytes runs `wl-paste --no-newline` (optionally with --type)
// and returns the raw stdout bytes. Returns nil on any error or when
// the clipboard does not advertise the requested type.
func wlPasteBytes(mime string) []byte {
	args := []string{"--no-newline"}
	if mime != "" {
		args = append(args, "--type", mime)
	}
	cmd := exec.Command("wl-paste", args...)
	var stdout, stderr bytes.Buffer
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr
	if err := cmd.Run(); err != nil {
		// Common case: clipboard is empty or the requested type is
		// not advertised. wl-paste exits non-zero with a descriptive
		// stderr line. Log at debug level only.
		log.Printf("[paste] wl-paste --type=%q failed: %v (stderr=%q)", mime, err, stderr.String())
		return nil
	}
	return stdout.Bytes()
}

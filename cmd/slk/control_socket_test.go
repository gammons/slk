package main

import (
	"net"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	tea "charm.land/bubbletea/v2"

	"github.com/gammons/slk/internal/ui"
)

// shortSocketPath keeps the path under the ~104-byte sun_path limit
// that t.TempDir() can exceed on macOS.
func shortSocketPath(t *testing.T) string {
	t.Helper()
	dir, err := os.MkdirTemp("", "slkctl")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { os.RemoveAll(dir) })
	return filepath.Join(dir, "c.sock")
}

type recorder struct {
	mu   sync.Mutex
	msgs []tea.Msg
}

func (r *recorder) send(m tea.Msg) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.msgs = append(r.msgs, m)
}

func (r *recorder) all() []tea.Msg {
	r.mu.Lock()
	defer r.mu.Unlock()
	return append([]tea.Msg(nil), r.msgs...)
}

func TestControlSocket_OpenRoundTrip(t *testing.T) {
	path := shortSocketPath(t)
	t.Setenv(controlSocketEnv, path)
	var rec recorder
	ctl, err := startControlSocket(path, rec.send)
	if err != nil || ctl == nil {
		t.Fatalf("start: ctl=%v err=%v", ctl, err)
	}
	defer ctl.Close()

	info, err := os.Stat(path)
	if err != nil {
		t.Fatal(err)
	}
	if perm := info.Mode().Perm(); perm != 0o600 {
		t.Errorf("socket perm = %o, want 600", perm)
	}

	const url = "https://myteam.slack.com/archives/C054JFCBN69/p1779284733270139"
	if err := openViaControlSocket([]string{url}); err != nil {
		t.Fatalf("open: %v", err)
	}
	msgs := rec.all()
	if len(msgs) != 1 {
		t.Fatalf("got %d msgs, want 1", len(msgs))
	}
	if m, ok := msgs[0].(ui.OpenSlackLinkMsg); !ok || m.URL != url {
		t.Fatalf("msg = %#v", msgs[0])
	}
}

func TestControlSocket_RejectsNonPermalinks(t *testing.T) {
	path := shortSocketPath(t)
	t.Setenv(controlSocketEnv, path)
	var rec recorder
	ctl, err := startControlSocket(path, rec.send)
	if err != nil {
		t.Fatal(err)
	}
	defer ctl.Close()

	if err := openViaControlSocket([]string{"https://github.com/foo"}); err == nil {
		t.Fatal("client accepted a non-Slack link")
	}
	conn := dialControl(t, path)
	conn.write("open https://github.com/foo\n")
	if reply := conn.reply(); !strings.HasPrefix(reply, "error:") {
		t.Fatalf("server reply = %q, want an error", reply)
	}
	conn = dialControl(t, path)
	conn.write("send hello\n")
	if reply := conn.reply(); !strings.HasPrefix(reply, "error: unknown command") {
		t.Fatalf("server reply = %q, want unknown command", reply)
	}
	if n := len(rec.all()); n != 0 {
		t.Fatalf("%d msgs reached the program, want 0", n)
	}
}

func TestControlSocket_LeavesALiveOwnerAlone(t *testing.T) {
	path := shortSocketPath(t)
	var first, second recorder
	owner, err := startControlSocket(path, first.send)
	if err != nil {
		t.Fatal(err)
	}
	defer owner.Close()
	other, err := startControlSocket(path, second.send)
	if err != nil || other != nil {
		t.Fatalf("second start: ctl=%v err=%v, want nil, nil", other, err)
	}
	t.Setenv(controlSocketEnv, path)
	if err := openViaControlSocket([]string{"https://myteam.slack.com/archives/C1/p1779284733270139"}); err != nil {
		t.Fatal(err)
	}
	if len(first.all()) != 1 || len(second.all()) != 0 {
		t.Fatalf("first=%d second=%d, want 1 and 0", len(first.all()), len(second.all()))
	}
}

func TestControlSocket_ReplacesAStaleSocketFile(t *testing.T) {
	path := shortSocketPath(t)
	if err := os.WriteFile(path, nil, 0o600); err != nil {
		t.Fatal(err)
	}
	var rec recorder
	ctl, err := startControlSocket(path, rec.send)
	if err != nil || ctl == nil {
		t.Fatalf("start over stale file: ctl=%v err=%v", ctl, err)
	}
	ctl.Close()
}

func TestOpenViaControlSocket_NoRunningSlk(t *testing.T) {
	t.Setenv(controlSocketEnv, shortSocketPath(t))
	err := openViaControlSocket([]string{"https://myteam.slack.com/archives/C1/p1779284733270139"})
	if err == nil || !strings.Contains(err.Error(), "no running slk") {
		t.Fatalf("err = %v", err)
	}
}

type testConn struct {
	t *testing.T
	c net.Conn
}

func dialControl(t *testing.T, path string) testConn {
	t.Helper()
	c, err := net.Dial("unix", path)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { c.Close() })
	_ = c.SetDeadline(time.Now().Add(2 * time.Second))
	return testConn{t: t, c: c}
}

func (tc testConn) write(s string) {
	if _, err := tc.c.Write([]byte(s)); err != nil {
		tc.t.Fatal(err)
	}
}

func (tc testConn) reply() string {
	buf := make([]byte, 256)
	n, _ := tc.c.Read(buf)
	return strings.TrimSpace(string(buf[:n]))
}

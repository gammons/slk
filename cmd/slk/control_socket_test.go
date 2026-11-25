package main

import (
	"io"
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

func TestControlSocket_ReplacesAnAbandonedSocket(t *testing.T) {
	path := shortSocketPath(t)
	// A socket file nothing listens on, as a crashed slk leaves behind.
	dead, err := net.Listen("unix", path)
	if err != nil {
		t.Fatal(err)
	}
	dead.(*net.UnixListener).SetUnlinkOnClose(false)
	dead.Close()
	if _, err := os.Lstat(path); err != nil {
		t.Fatalf("abandoned socket missing: %v", err)
	}

	var rec recorder
	ctl, err := startControlSocket(path, rec.send)
	if err != nil || ctl == nil {
		t.Fatalf("start over abandoned socket: ctl=%v err=%v", ctl, err)
	}
	defer ctl.Close()
	t.Setenv(controlSocketEnv, path)
	if err := openViaControlSocket([]string{"https://myteam.slack.com/archives/C1/p1779284733270139"}); err != nil {
		t.Fatalf("open after takeover: %v", err)
	}
}

func TestControlSocket_RefusesToRemoveANonSocket(t *testing.T) {
	path := shortSocketPath(t)
	if err := os.WriteFile(path, []byte("keep me"), 0o600); err != nil {
		t.Fatal(err)
	}
	var rec recorder
	ctl, err := startControlSocket(path, rec.send)
	if err == nil || ctl != nil {
		t.Fatalf("start over a regular file: ctl=%v err=%v, want an error", ctl, err)
	}
	got, readErr := os.ReadFile(path)
	if readErr != nil || string(got) != "keep me" {
		t.Fatalf("regular file changed: %q, %v", got, readErr)
	}
}

func TestControlSocket_ConcurrentStartsKeepOneOwner(t *testing.T) {
	for i := 0; i < 20; i++ {
		path := shortSocketPath(t)
		const n = 4
		recs := make([]recorder, n)
		ctls := make([]io.Closer, n)
		errs := make([]error, n)
		var wg sync.WaitGroup
		for j := 0; j < n; j++ {
			wg.Add(1)
			go func(j int) {
				defer wg.Done()
				ctls[j], errs[j] = startControlSocket(path, recs[j].send)
			}(j)
		}
		wg.Wait()

		owners := 0
		for j := 0; j < n; j++ {
			if errs[j] != nil {
				t.Fatalf("run %d start %d: %v", i, j, errs[j])
			}
			if ctls[j] != nil {
				owners++
				defer ctls[j].Close()
			}
		}
		if owners != 1 {
			t.Fatalf("run %d: %d instances listening, want 1", i, owners)
		}
		t.Setenv(controlSocketEnv, path)
		if err := openViaControlSocket([]string{"https://myteam.slack.com/archives/C1/p1779284733270139"}); err != nil {
			t.Fatalf("run %d: open: %v", i, err)
		}
		got := 0
		for j := 0; j < n; j++ {
			if ctls[j] != nil {
				got = len(recs[j].all())
			}
		}
		if got != 1 {
			t.Fatalf("run %d: owner received %d msgs, want 1", i, got)
		}
	}
}

func TestReadControlLine(t *testing.T) {
	const url = "https://myteam.slack.com/archives/C054JFCBN69/p1779284733270139?thread_ts=1779284700.000100"
	if got, err := readControlLine(strings.NewReader("open " + url + "\n")); err != nil || got != "open "+url+"\n" {
		t.Fatalf("newline-terminated: %q, %v", got, err)
	}
	if got, err := readControlLine(strings.NewReader("open " + url)); err != nil || got != "open "+url {
		t.Fatalf("EOF-terminated: %q, %v", got, err)
	}
	long := "open " + url + "&pad=" + strings.Repeat("x", maxControlLine)
	if _, err := readControlLine(strings.NewReader(long)); err == nil {
		t.Fatal("over-long request without newline was accepted")
	}
	if _, err := readControlLine(strings.NewReader(long + "\n")); err == nil {
		t.Fatal("over-long request with newline was accepted")
	}
	exact := strings.Repeat("x", maxControlLine-1) + "\n"
	if _, err := readControlLine(strings.NewReader(exact)); err != nil {
		t.Fatalf("request of exactly %d bytes refused: %v", maxControlLine, err)
	}
}

func TestControlSocket_RefusesAnOverlongRequest(t *testing.T) {
	path := shortSocketPath(t)
	var rec recorder
	ctl, err := startControlSocket(path, rec.send)
	if err != nil {
		t.Fatal(err)
	}
	defer ctl.Close()
	// A valid permalink whose query runs past the limit: cutting it would
	// still parse, so the server must refuse it, not open the cut link.
	conn := dialControl(t, path)
	conn.write("open https://myteam.slack.com/archives/C1/p1779284733270139?thread_ts=1779284700.000100" + strings.Repeat("0", maxControlLine) + "\n")
	if reply := conn.reply(); !strings.HasPrefix(reply, "error:") {
		t.Fatalf("reply = %q, want an error", reply)
	}
	if n := len(rec.all()); n != 0 {
		t.Fatalf("%d msgs reached the program, want 0", n)
	}
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

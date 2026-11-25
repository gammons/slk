package main

import (
	"bufio"
	"errors"
	"fmt"
	"io"
	"net"
	"os"
	"path/filepath"
	"strings"
	"time"

	tea "charm.land/bubbletea/v2"

	"github.com/gammons/slk/internal/debuglog"
	"github.com/gammons/slk/internal/slackurl"
	"github.com/gammons/slk/internal/ui"
)

// controlSocketEnv overrides the control socket path, so a second slk
// (a test build, another terminal) can run without taking over the
// first one's socket.
const controlSocketEnv = "SLK_CONTROL_SOCKET"

// maxControlLine bounds one request. A Slack permalink is well under
// this; anything longer is not one.
const maxControlLine = 4096

func controlSocketPath() string {
	if p := os.Getenv(controlSocketEnv); p != "" {
		return p
	}
	return filepath.Join(xdgData(), "control.sock")
}

// startControlSocket listens on the control socket and forwards each
// valid permalink to the program as an OpenSlackLinkMsg. It never types
// into the TUI, so a request cannot end up in a composer. When another
// live slk already owns the socket it leaves it alone and returns nil.
//
// The ownership check, the stale-socket removal and the bind run under
// an exclusive lock on path+".lock", so two slk starting together cannot
// both find the socket dead and have the second unlink the first one's
// live listener.
func startControlSocket(path string, send func(tea.Msg)) (io.Closer, error) {
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		return nil, fmt.Errorf("creating control socket dir: %w", err)
	}
	lock, err := os.OpenFile(path+".lock", os.O_CREATE|os.O_RDWR, 0o600)
	if err != nil {
		return nil, fmt.Errorf("opening control socket lock: %w", err)
	}
	defer lock.Close()
	if err := lockFile(lock); err != nil {
		return nil, fmt.Errorf("locking control socket: %w", err)
	}
	defer func() { _ = unlockFile(lock) }()

	if conn, err := net.DialTimeout("unix", path, 500*time.Millisecond); err == nil {
		conn.Close()
		debuglog.General("control socket %s is owned by another slk; not listening", path)
		return nil, nil
	}
	if err := removeStaleSocket(path); err != nil {
		return nil, err
	}
	ln, err := net.Listen("unix", path)
	if err != nil {
		return nil, fmt.Errorf("listening on control socket: %w", err)
	}
	if err := os.Chmod(path, 0o600); err != nil {
		ln.Close()
		return nil, fmt.Errorf("restricting control socket: %w", err)
	}
	go serveControlSocket(ln, send)
	return ln, nil
}

// removeStaleSocket deletes a socket nothing answers on (left behind by a
// crash). Anything else at path is refused: a failed dial does not make a
// regular file, or a directory, a stale socket.
func removeStaleSocket(path string) error {
	info, err := os.Lstat(path)
	if errors.Is(err, os.ErrNotExist) {
		return nil
	}
	if err != nil {
		return fmt.Errorf("checking control socket path: %w", err)
	}
	if info.Mode().Type() != os.ModeSocket {
		return fmt.Errorf("control socket path %s exists and is not a socket", path)
	}
	if err := os.Remove(path); err != nil {
		return fmt.Errorf("removing stale control socket: %w", err)
	}
	return nil
}

func serveControlSocket(ln net.Listener, send func(tea.Msg)) {
	for {
		conn, err := ln.Accept()
		if err != nil {
			if errors.Is(err, net.ErrClosed) {
				return
			}
			debuglog.General("control socket accept: %v", err)
			continue
		}
		go handleControlConn(conn, send)
	}
}

func handleControlConn(conn net.Conn, send func(tea.Msg)) {
	defer conn.Close()
	_ = conn.SetDeadline(time.Now().Add(5 * time.Second))
	line, err := readControlLine(conn)
	if err != nil {
		fmt.Fprintf(conn, "error: %v\n", err)
		return
	}
	cmd, arg, _ := strings.Cut(strings.TrimSpace(line), " ")
	switch cmd {
	case "open":
		arg = strings.TrimSpace(arg)
		if _, ok := slackurl.Parse(arg); !ok {
			fmt.Fprintln(conn, "error: not a Slack message link")
			return
		}
		send(ui.OpenSlackLinkMsg{URL: arg})
		fmt.Fprintln(conn, "ok")
	default:
		fmt.Fprintf(conn, "error: unknown command %q\n", cmd)
	}
}

// readControlLine reads one request. A request ends at a newline, or at
// EOF when the client closes its side early. One that reaches
// maxControlLine without a newline is refused rather than cut, since a
// cut permalink can still parse and point somewhere else.
func readControlLine(r io.Reader) (string, error) {
	line, err := bufio.NewReader(io.LimitReader(r, maxControlLine+1)).ReadString('\n')
	if len(line) > maxControlLine {
		return "", fmt.Errorf("request longer than %d bytes", maxControlLine)
	}
	if err != nil && !errors.Is(err, io.EOF) {
		return "", err
	}
	return line, nil
}

// openViaControlSocket is `slk --open <permalink>`: it asks the running
// slk to navigate to the link.
func openViaControlSocket(args []string) error {
	if len(args) != 1 || strings.TrimSpace(args[0]) == "" {
		return errors.New("usage: slk --open <slack permalink>")
	}
	url := strings.TrimSpace(args[0])
	if _, ok := slackurl.Parse(url); !ok {
		return fmt.Errorf("not a Slack message link: %s", url)
	}
	path := controlSocketPath()
	conn, err := net.DialTimeout("unix", path, 2*time.Second)
	if err != nil {
		return fmt.Errorf("no running slk on %s: %w", path, err)
	}
	defer conn.Close()
	_ = conn.SetDeadline(time.Now().Add(5 * time.Second))
	if _, err := fmt.Fprintf(conn, "open %s\n", url); err != nil {
		return err
	}
	reply, err := bufio.NewReader(conn).ReadString('\n')
	if err != nil && !errors.Is(err, io.EOF) {
		return fmt.Errorf("reading reply: %w", err)
	}
	reply = strings.TrimSpace(reply)
	if reply != "ok" {
		return errors.New(strings.TrimPrefix(reply, "error: "))
	}
	return nil
}

package main

import (
	"context"
	"errors"
	"fmt"
	"io"
	"net/url"
	"os"
	"os/signal"
	"regexp"
	"strings"
	"syscall"
	"time"

	"charm.land/huh/v2"
	"golang.org/x/term"

	slackclient "github.com/gammons/slk/internal/slack"
)

// The browser-session flow signs a workspace in without the Slack desktop app
// (containers, servers, SSH sessions, or a browser-only user). It needs the
// same two values the desktop flow reads: the xoxc token and the d cookie.
// The d cookie is HttpOnly, so no DevTools console snippet can read it, but a
// "Copy as cURL" of any request to a Slack /api/ endpoint carries both. The
// user pastes that (the bash, cmd or PowerShell form); a bare xoxc token
// followed by the d cookie also works.
//
// Nothing re-mints such a token: startup keeps the cached one when it cannot
// read a desktop cookie (see remintTokens), so it lasts as long as the
// browser session it came from.

var (
	errPasteCancelled = errors.New("paste cancelled")
	errNoToken        = errors.New("no xoxc- token found: copy a request to /api/, not the page itself")
	errNoCookie       = errors.New("no d cookie (xoxd-) found: copy the request from a browser signed in to app.slack.com")

	xoxcPattern = regexp.MustCompile(`xoxc-[A-Za-z0-9-]+`)
	// The d cookie as "d=<value>" (bash and cmd forms, a Cookie header),
	// not d-s, xd or any other cookie whose name ends in d.
	dCookiePattern = regexp.MustCompile(`(?:^|[^A-Za-z0-9_-])d=(xoxd-[^;\s"'\\]+)`)
	// The d cookie in the PowerShell form:
	// New-Object System.Net.Cookie("d", "xoxd-...", "/", ".slack.com").
	psCookiePattern = regexp.MustCompile(`Cookie\(\s*"d"\s*,\s*"(xoxd-[^"]+)"`)
	// A value pasted on its own, without the "d=" prefix.
	bareCookiePattern = regexp.MustCompile(`^xoxd-[^;\s"'\\]+$`)
)

const browserSessionSteps = `No Slack desktop app needed: sign in from your browser instead.

  1. Open https://app.slack.com in your browser and sign in.
  2. DevTools > Network, filter on api/, click a channel, then right click
     one of the requests > Copy > Copy as cURL.
  3. Paste it below and press Enter (nothing is shown while you paste).

A bare xoxc- token works too; slk then asks for the d cookie
(DevTools > Application > Cookies > https://app.slack.com).`

// parseBrowserSession pulls the xoxc token and the d cookie out of pasted
// text. Either may be empty; the caller decides what to ask for next.
func parseBrowserSession(paste string) (token, cookie string) {
	// The cmd form escapes with carets (^", ^%, %^2F): none belongs to a
	// token or a cookie, so they all go.
	if strings.Contains(paste, `^"`) {
		paste = strings.ReplaceAll(paste, "^", "")
	}
	token = xoxcPattern.FindString(paste)
	if m := dCookiePattern.FindStringSubmatch(paste); m != nil {
		cookie = m[1]
	} else if m := psCookiePattern.FindStringSubmatch(paste); m != nil {
		cookie = m[1]
	} else if trimmed := strings.TrimSpace(paste); bareCookiePattern.MatchString(trimmed) {
		cookie = trimmed
	}
	return token, encodeCookie(cookie)
}

// encodeCookie returns the d cookie in its URL-encoded form, the one the
// desktop flow stores and the one Slack sends. DevTools' Application tab can
// show it decoded (xoxd-AbC/def+Ghi==); an encoded value has none of /+= and
// is kept as is.
func encodeCookie(cookie string) string {
	if !strings.ContainsAny(cookie, "/+=") {
		return cookie
	}
	return url.QueryEscape(cookie)
}

// pasteComplete reports whether the text read so far ends a pasted command.
// A line ending with a continuation (\ for bash, ^ for cmd, ` for
// PowerShell) asks for more. The PowerShell form needs two more rules: it
// opens with "$session = ..." lines that end with no continuation at all, so
// it is only complete once its Invoke-WebRequest command is; and its
// -Headers @{ ... } block spans lines that end with none either, so it is not
// complete while a @{ is still open.
func pasteComplete(s string) bool {
	s = strings.TrimRight(s, "\n")
	if strings.TrimSpace(s) == "" {
		return false
	}
	last := strings.TrimRight(s[strings.LastIndexByte(s, '\n')+1:], " \t")
	if strings.HasSuffix(last, `\`) || strings.HasSuffix(last, "^") || strings.HasSuffix(last, "`") {
		return false
	}
	if strings.HasPrefix(strings.TrimSpace(s), "$session") && !strings.Contains(s, "Invoke-WebRequest") {
		return false
	}
	if strings.Contains(s, "Invoke-WebRequest") {
		closed := 0
		for _, line := range strings.Split(s, "\n") {
			if strings.HasPrefix(strings.TrimSpace(line), "}") {
				closed++
			}
		}
		return strings.Count(s, "@{") <= closed
	}
	return true
}

// readPaste reads a paste byte by byte until it is complete (see
// pasteComplete), or until Ctrl-D. Ctrl-C cancels. It expects a terminal in
// raw mode: in canonical mode Linux drops anything past 4095 bytes on a line,
// and a copied cURL line with its cookies easily goes past that.
func readPaste(r io.Reader) (string, error) {
	var b strings.Builder
	buf := make([]byte, 4096)
	for {
		n, err := r.Read(buf)
		for _, c := range buf[:n] {
			switch c {
			case 3: // Ctrl-C
				return "", errPasteCancelled
			case 4: // Ctrl-D
				return b.String(), nil
			case '\r':
				c = '\n'
			}
			b.WriteByte(c)
			if c == '\n' && pasteComplete(b.String()) {
				return b.String(), nil
			}
		}
		if errors.Is(err, io.EOF) {
			return b.String(), nil
		}
		if err != nil {
			return "", err
		}
	}
}

// Whatever is still queued once readPaste stops (the tail of a paste it did
// not recognise, keys typed meanwhile) holds secrets: it must not be left for
// the shell to read once slk exits. A paste reaches the terminal in chunks,
// slower over SSH, so a single flush right away can miss the tail. Input is
// discarded until it has been quiet for drainQuiet, for drainMax at most.
const (
	drainPoll  = 50 * time.Millisecond
	drainQuiet = 500 * time.Millisecond
	drainMax   = 5 * time.Second
)

// drainInput discards pending input until none has arrived for drainQuiet,
// or drainMax has passed. pending, flush and sleep are injected for tests.
func drainInput(pending func() (int, error), flush func() error, sleep func(time.Duration)) {
	quiet := time.Duration(0)
	for total := time.Duration(0); total < drainMax && quiet < drainQuiet; total += drainPoll {
		if n, err := pending(); err == nil && n > 0 {
			_ = flush()
			quiet = 0
		} else {
			quiet += drainPoll
		}
		sleep(drainPoll)
	}
	_ = flush()
}

// readSecret prints prompt and reads a paste without echoing it. From a pipe
// it reads everything.
func readSecret(in *os.File, prompt string) (string, error) {
	fmt.Print(prompt)
	defer fmt.Println()
	fd := int(in.Fd())
	if !term.IsTerminal(fd) {
		b, err := io.ReadAll(in)
		return string(b), err
	}
	old, err := term.MakeRaw(fd)
	if err != nil {
		return "", err
	}
	defer func() { _ = term.Restore(fd, old) }()

	// A signal while waiting for the paste skips the deferred Restore: put
	// the terminal back before going. (Ctrl-C itself arrives as a byte in
	// raw mode; SIGINT here is a kill -INT.)
	sig := make(chan os.Signal, 1)
	done := make(chan struct{})
	signal.Notify(sig, os.Interrupt, syscall.SIGTERM, syscall.SIGHUP)
	defer func() {
		signal.Stop(sig)
		close(done)
	}()
	go func() {
		select {
		case <-sig:
			_ = term.Restore(fd, old)
			os.Exit(1)
		case <-done:
		}
	}()

	s, err := readPaste(in)
	drainInput(func() (int, error) { return inputPending(fd) }, func() error { return flushInput(fd) }, time.Sleep)
	return s, err
}

// browserLogin is the browser-session flow with its I/O injected, so each
// step can be tested without a terminal or Slack.
type browserLogin struct {
	// read prompts for and returns a paste.
	read func(prompt string) (string, error)
	// auth checks the pair with Slack and returns the token to save.
	auth func(token, cookie string) (slackclient.Token, error)
	// save persists it.
	save func(slackclient.Token) error
}

func (b browserLogin) run(st onboardingStyles) error {
	paste, err := b.read("cURL command or xoxc token: ")
	if err != nil {
		return err
	}
	token, cookie := parseBrowserSession(paste)
	if token == "" {
		fmt.Println(st.errorText.Render("  " + errNoToken.Error()))
		return errNoToken
	}
	if cookie == "" {
		paste, err = b.read("d cookie: ")
		if err != nil {
			return err
		}
		if _, cookie = parseBrowserSession(paste); cookie == "" {
			fmt.Println(st.errorText.Render("  " + errNoCookie.Error()))
			return errNoCookie
		}
	}

	fmt.Println(st.step.Render("Connecting..."))
	tok, err := b.auth(token, cookie)
	if err != nil {
		fmt.Println(st.errorText.Render(fmt.Sprintf("  Authentication failed: %v", err)))
		return fmt.Errorf("authentication failed: %w", err)
	}
	return b.save(tok)
}

// authBrowserSession checks the pair with auth.test, which also yields the
// team ID, name and subdomain the token file needs.
func authBrowserSession(token, cookie string) (slackclient.Token, error) {
	client := slackclient.NewClient(token, cookie)
	if err := client.Connect(context.Background()); err != nil {
		return slackclient.Token{}, err
	}
	return slackclient.Token{
		AccessToken: token,
		Cookie:      cookie,
		Domain:      client.TeamSubdomain(),
		TeamID:      client.TeamID(),
		TeamName:    client.TeamName(),
	}, nil
}

// offerBrowserFallback is called when the desktop session cannot be read. On
// a terminal it asks whether to run the browser-session flow instead;
// otherwise, or on a no, it returns the desktop error unchanged. ask and
// browser are injected for tests.
func offerBrowserFallback(interactive bool, ask func() bool, browser func() error, desktopErr error) error {
	if !interactive || !ask() {
		return desktopErr
	}
	return browser()
}

// askBrowserFallback asks whether to sign in with a browser session; yes by
// default, no when the form cannot run.
func askBrowserFallback() bool {
	useBrowser := true
	confirm := huh.NewConfirm().
		Title("Sign in with a browser session instead?").
		Affirmative("Yes").
		Negative("No").
		Value(&useBrowser)
	if err := huh.NewForm(huh.NewGroup(confirm)).WithTheme(huh.ThemeFunc(huh.ThemeDracula)).Run(); err != nil {
		return false
	}
	return useBrowser
}

// addWorkspaceFromBrowser runs the browser-session flow against the terminal
// and Slack.
func addWorkspaceFromBrowser(tokenStore *slackclient.TokenStore, st onboardingStyles) error {
	fmt.Println()
	fmt.Println(browserSessionSteps)
	fmt.Println()

	login := browserLogin{
		read: func(prompt string) (string, error) { return readSecret(os.Stdin, prompt) },
		auth: authBrowserSession,
		save: func(tok slackclient.Token) error { return saveWorkspace(tokenStore, tok, st) },
	}
	if err := login.run(st); err != nil {
		return err
	}

	fmt.Println()
	fmt.Println(st.dim.Render("  This session lasts as long as the browser's. When Slack signs it out, run"))
	fmt.Println(st.dim.Render("  slk --add-workspace --browser again."))
	fmt.Println(st.dim.Render("  Run ") + st.step.Render("slk") + st.dim.Render(" to start."))
	fmt.Println()
	return nil
}

// addWorkspaceArgs reads the arguments of an --add-workspace run, in any
// order: whether to go straight to the browser-session flow. --browser alone,
// or anything else alongside, is an error rather than silently ignored.
func addWorkspaceArgs(args []string) (browser bool, err error) {
	add := false
	for _, a := range args {
		switch a {
		case "--add-workspace":
			add = true
		case "--browser":
			browser = true
		default:
			return false, fmt.Errorf("unexpected argument %q (usage: slk --add-workspace [--browser])", a)
		}
	}
	if !add {
		return false, errors.New("--browser goes with --add-workspace: slk --add-workspace --browser")
	}
	return browser, nil
}

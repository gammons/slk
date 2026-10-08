package main

import (
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/gammons/slk/internal/config"
	slackclient "github.com/gammons/slk/internal/slack"
	"github.com/gammons/slk/internal/slackdesktop"
)

// Shapes of "Copy as cURL" as Chrome and Firefox produce them, trimmed to what
// matters. The values are fake. The cmd and PowerShell ones follow Chrome's
// escaping rules (^" around strings, ^% and %^ around a percent; the cookies
// in a WebRequestSession).
const (
	chromeCurl = `curl 'https://acme.slack.com/api/conversations.history?_x_id=abc&slack_route=T0123' \
  -H 'accept: */*' \
  -b 'b=abc123; d=xoxd-AbC%2Fdef%2BGhi%3D%3D; d-s=1700000000; lc=1700000000' \
  -H 'origin: https://app.slack.com' \
  --data-raw $'------WebKitFormBoundaryXYZ\r\nContent-Disposition: form-data; name="token"\r\n\r\nxoxc-1111-2222-3333-abcdef\r\n------WebKitFormBoundaryXYZ--\r\n'
`
	firefoxCurl = `curl 'https://acme.slack.com/api/client.counts' -X POST -H 'Content-Type: application/x-www-form-urlencoded' -H 'Cookie: d-s=17; d=xoxd-ZZZ%2F%3D; b=x' --data-raw 'token=xoxc-9999-8888-aa&_x_reason=x'
`
	cmdCurl = `curl ^"https://acme.slack.com/api/conversations.history?_x_id=abc^&slack_route=T0123^" ^
  -H ^"accept: */*^" ^
  -b ^"b=abc123; d=xoxd-AbC^%^2Fdef^%^2BGhi^%^3D^%^3D; d-s=1700000000^" ^
  --data-raw ^"token=xoxc-1111-2222-3333-abcdef^&_x_reason=x^"
`
	powershellCurl = "$session = New-Object Microsoft.PowerShell.Commands.WebRequestSession\n" +
		"$session.UserAgent = \"Mozilla/5.0\"\n" +
		"$session.Cookies.Add((New-Object System.Net.Cookie(\"b\", \"abc123\", \"/\", \".slack.com\")))\n" +
		"$session.Cookies.Add((New-Object System.Net.Cookie(\"d\", \"xoxd-AbC%2Fdef\", \"/\", \".slack.com\")))\n" +
		"$session.Cookies.Add((New-Object System.Net.Cookie(\"d-s\", \"1700000000\", \"/\", \".slack.com\")))\n" +
		"Invoke-WebRequest -UseBasicParsing -Uri \"https://acme.slack.com/api/client.counts\" `\n" +
		"-Method \"POST\" `\n" +
		"-WebSession $session `\n" +
		"-Headers @{\n" +
		"\"accept\"=\"*/*\"\n" +
		"  \"origin\"=\"https://app.slack.com\"\n" +
		"} `\n" +
		"-ContentType \"application/x-www-form-urlencoded\" `\n" +
		"-Body \"token=xoxc-9999-8888-aa&_x_reason=x\"\n"
	// The PowerShell form without a $session: the cookie in the headers.
	powershellHeadersCurl = "Invoke-WebRequest -UseBasicParsing -Uri \"https://acme.slack.com/api/client.counts\" `\n" +
		"-Method POST `\n" +
		"-Headers @{\n" +
		"\"Accept\" = \"*/*\"\n" +
		"\"Cookie\" = \"b=x; d=xoxd-ZZZ%2F%3D; d-s=17\"\n" +
		"} `\n" +
		"-Body \"token=xoxc-9999-8888-aa&_x_reason=x\"\n"
)

func TestParseBrowserSession(t *testing.T) {
	tests := []struct {
		name, paste, token, cookie string
	}{
		{"chrome", chromeCurl, "xoxc-1111-2222-3333-abcdef", "xoxd-AbC%2Fdef%2BGhi%3D%3D"},
		{"firefox, d-s before d", firefoxCurl, "xoxc-9999-8888-aa", "xoxd-ZZZ%2F%3D"},
		{"chrome cmd", cmdCurl, "xoxc-1111-2222-3333-abcdef", "xoxd-AbC%2Fdef%2BGhi%3D%3D"},
		{"chrome powershell", powershellCurl, "xoxc-9999-8888-aa", "xoxd-AbC%2Fdef"},
		{"powershell, cookie in the headers", powershellHeadersCurl, "xoxc-9999-8888-aa", "xoxd-ZZZ%2F%3D"},
		{"a cookie named xd before d", `-b 'xd=xoxd-WRONG; d=xoxd-RIGHT' token=xoxc-1`, "xoxc-1", "xoxd-RIGHT"},
		{"a cookie named x-d before d", `-b 'x-d=xoxd-WRONG; d=xoxd-RIGHT' token=xoxc-1`, "xoxc-1", "xoxd-RIGHT"},
		{"bare cookie, decoded", "xoxd-AbC/def+Ghi==\n", "", "xoxd-AbC%2Fdef%2BGhi%3D%3D"},
		{"bare cookie, encoded, kept as is", "xoxd-AbC%2Fdef\n", "", "xoxd-AbC%2Fdef"},
		{"piped: token, then the cookie on its own line", "xoxc-1-2\nxoxd-Q%2F\n", "xoxc-1-2", "xoxd-Q%2F"},
		{"piped, CRLF", "xoxc-1-2\r\n  xoxd-Q%2F\r\n", "xoxc-1-2", "xoxd-Q%2F"},
		{"xoxd inside other text is not a bare cookie", "see xoxd-Q in the docs\n", "", ""},
		{"xoxd followed by other text is not a bare cookie", "xoxd-Q is my cookie\n", "", ""},
		{"bare cookie with its trailing semicolon", "xoxd-Q%2F;\n", "", "xoxd-Q%2F"},
		{"cookie first in the header", `-H 'cookie: d=xoxd-A1; b=2' token=xoxc-1-2`, "xoxc-1-2", "xoxd-A1"},
		{"bare token", "xoxc-1-2-3\n", "xoxc-1-2-3", ""},
		{"bare cookie", "  xoxd-Q%2F\n", "", "xoxd-Q%2F"},
		{"cookie with its name", "d=xoxd-Q\n", "", "xoxd-Q"},
		{"only d-s", `-b 'd-s=xoxd-nope' token=xoxc-1`, "xoxc-1", ""},
		{"the page, not an api call", `curl 'https://app.slack.com/client/T1' -b 'd=xoxd-A'`, "", "xoxd-A"},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			token, cookie := parseBrowserSession(tc.paste)
			if token != tc.token || cookie != tc.cookie {
				t.Errorf("parseBrowserSession = (%q, %q), want (%q, %q)", token, cookie, tc.token, tc.cookie)
			}
		})
	}
}

func TestPasteComplete(t *testing.T) {
	tests := []struct {
		s    string
		want bool
	}{
		{"", false},
		{"\n\n", false},
		{"xoxc-1\n", true},
		{"curl 'x' \\\n", false},
		{"curl 'x' \\  \n", false},
		{"curl 'x' \\\n  -H 'a: b'\n", true},
		{"curl ^\"x^\" ^\n", false},
		{"curl ^\"x^\" ^\n  -H ^\"a: b^\"\n", true},
		{"$session = New-Object X\n", false},
		{"$session = New-Object X\nInvoke-WebRequest -Uri \"x\" `\n", false},
		{"$session = New-Object X\nInvoke-WebRequest -Uri \"x\" `\n-Body \"y\"\n", true},
		{"Invoke-WebRequest -Uri \"x\" `\n-Headers @{\n", false},
		{"Invoke-WebRequest -Uri \"x\" `\n-Headers @{\n\"a\"=\"b\"\n", false},
		{"Invoke-WebRequest -Uri \"x\" `\n-Headers @{\n\"a\"=\"b\"\n} `\n", false},
		{"Invoke-WebRequest -Uri \"x\" `\n-Headers @{\n\"a\"=\"b\"\n} `\n-Body \"y\"\n", true},
	}
	for _, tc := range tests {
		if got := pasteComplete(tc.s); got != tc.want {
			t.Errorf("pasteComplete(%q) = %v, want %v", tc.s, got, tc.want)
		}
	}
}

// A raw-mode terminal sends \r for Enter; a multi-line paste must be read up
// to the line that closes the command, and not past it.
func TestReadPasteMultiLineCurl(t *testing.T) {
	in := strings.ReplaceAll(chromeCurl, "\n", "\r") + "left over"
	got, err := readPaste(strings.NewReader(in))
	if err != nil {
		t.Fatalf("readPaste: %v", err)
	}
	if got != chromeCurl {
		t.Errorf("readPaste = %q, want %q", got, chromeCurl)
	}
}

// A line longer than the 4096-byte read buffer (and than the tty's canonical
// line limit) must come through whole.
func TestReadPasteLongLine(t *testing.T) {
	long := "curl -b 'd=xoxd-" + strings.Repeat("A", 10000) + "'\r"
	got, err := readPaste(strings.NewReader(long))
	if err != nil {
		t.Fatalf("readPaste: %v", err)
	}
	if want := strings.ReplaceAll(long, "\r", "\n"); got != want {
		t.Errorf("readPaste returned %d bytes, want %d", len(got), len(want))
	}
}

func TestReadPasteControlKeys(t *testing.T) {
	if _, err := readPaste(strings.NewReader("xoxc-1\x03")); !errors.Is(err, errPasteCancelled) {
		t.Errorf("Ctrl-C: err = %v, want errPasteCancelled", err)
	}
	got, err := readPaste(strings.NewReader("curl 'x' \\\r\x04more"))
	if err != nil || got != "curl 'x' \\\n" {
		t.Errorf("Ctrl-D: readPaste = (%q, %v), want what was read so far", got, err)
	}
	got, err = readPaste(strings.NewReader("xoxc-1"))
	if err != nil || got != "xoxc-1" {
		t.Errorf("EOF: readPaste = (%q, %v), want what was read so far", got, err)
	}
}

// Every shape must be read whole: a paste readPaste stops on early leaves its
// tail, token and cookie included, for the shell to read.
func TestReadPasteReadsEveryShapeWhole(t *testing.T) {
	for name, paste := range map[string]string{
		"chrome bash": chromeCurl, "firefox": firefoxCurl, "chrome cmd": cmdCurl,
		"chrome powershell": powershellCurl, "powershell, cookie in the headers": powershellHeadersCurl,
	} {
		in := strings.ReplaceAll(paste, "\n", "\r") + "left over"
		got, err := readPaste(strings.NewReader(in))
		if err != nil || got != paste {
			t.Errorf("%s: readPaste = (%q, %v), want the whole paste", name, got, err)
		}
	}
}

// fakeLogin is a browserLogin whose reads come from a list, and which records
// what reached Slack and the token store.
type fakeLogin struct {
	pastes  []string
	reads   int
	authed  bool
	saved   *slackclient.Token
	authErr error
}

func (f *fakeLogin) login() browserLogin {
	return browserLogin{
		read: func(string) (string, error) {
			if f.reads >= len(f.pastes) {
				return "", errors.New("unexpected read")
			}
			f.reads++
			return f.pastes[f.reads-1], nil
		},
		auth: func(token, cookie string) (slackclient.Token, error) {
			f.authed = true
			if f.authErr != nil {
				return slackclient.Token{}, f.authErr
			}
			return slackclient.Token{AccessToken: token, Cookie: cookie, TeamID: "T1", TeamName: "Acme", Domain: "acme"}, nil
		},
		save: func(tok slackclient.Token) error {
			f.saved = &tok
			return nil
		},
	}
}

func TestBrowserLoginRun(t *testing.T) {
	st := newOnboardingStyles()

	t.Run("no token: Slack is never called", func(t *testing.T) {
		f := &fakeLogin{pastes: []string{"curl 'https://app.slack.com/client' -b 'd=xoxd-A'"}}
		if err := f.login().run(st); !errors.Is(err, errNoToken) {
			t.Errorf("err = %v, want errNoToken", err)
		}
		if f.authed || f.saved != nil {
			t.Error("auth or save ran without a token")
		}
	})

	t.Run("bare token: the cookie is asked for next", func(t *testing.T) {
		f := &fakeLogin{pastes: []string{"xoxc-1-2\n", "xoxd-Q%2F\n"}}
		if err := f.login().run(st); err != nil {
			t.Fatalf("run: %v", err)
		}
		if f.reads != 2 || f.saved == nil || f.saved.AccessToken != "xoxc-1-2" || f.saved.Cookie != "xoxd-Q%2F" {
			t.Errorf("reads=%d saved=%+v, want both values saved after two reads", f.reads, f.saved)
		}
	})

	// From a pipe the first read returns everything: both values must be
	// found in it, the second prompt would only see EOF.
	t.Run("piped token and cookie: one read", func(t *testing.T) {
		f := &fakeLogin{pastes: []string{"xoxc-1-2\nxoxd-Q%2F\n"}}
		if err := f.login().run(st); err != nil {
			t.Fatalf("run: %v", err)
		}
		if f.reads != 1 || f.saved == nil || f.saved.Cookie != "xoxd-Q%2F" {
			t.Errorf("reads=%d saved=%+v, want both values from one read", f.reads, f.saved)
		}
	})

	t.Run("no cookie on the second read", func(t *testing.T) {
		f := &fakeLogin{pastes: []string{"xoxc-1-2\n", "not a cookie\n"}}
		if err := f.login().run(st); !errors.Is(err, errNoCookie) {
			t.Errorf("err = %v, want errNoCookie", err)
		}
		if f.authed || f.saved != nil {
			t.Error("auth or save ran without a cookie")
		}
	})

	t.Run("a full cURL saves in one read", func(t *testing.T) {
		f := &fakeLogin{pastes: []string{chromeCurl}}
		if err := f.login().run(st); err != nil {
			t.Fatalf("run: %v", err)
		}
		if f.reads != 1 || f.saved == nil || f.saved.TeamID != "T1" {
			t.Errorf("reads=%d saved=%+v, want one read and the team saved", f.reads, f.saved)
		}
		if f.saved != nil && f.saved.Source != slackclient.TokenSourceBrowser {
			t.Errorf("Source = %q, want it marked as a browser session", f.saved.Source)
		}
	})

	t.Run("Slack refuses: nothing saved", func(t *testing.T) {
		authErr := errors.New("invalid_auth")
		f := &fakeLogin{pastes: []string{chromeCurl}, authErr: authErr}
		if err := f.login().run(st); !errors.Is(err, authErr) {
			t.Errorf("err = %v, want it to wrap the auth error", err)
		}
		if f.saved != nil {
			t.Error("saved a token Slack refused")
		}
	})
}

func TestOfferBrowserFallback(t *testing.T) {
	desktopErr := errors.New("slack desktop app config directory not found")
	browserErr := errors.New("from the browser flow")
	tests := []struct {
		name        string
		interactive bool
		yes         bool
		wantAsked   bool
		want        error
	}{
		// Without a terminal there is nobody to ask: the desktop error
		// comes back as is, not wrapped.
		{"not a terminal", false, true, false, desktopErr},
		{"declined", true, false, true, desktopErr},
		{"accepted", true, true, true, browserErr},
	}
	for _, tc := range tests {
		asked, ran := false, false
		err := offerBrowserFallback(tc.interactive,
			func() bool { asked = true; return tc.yes },
			func() error { ran = true; return browserErr },
			desktopErr)
		if err != tc.want || asked != tc.wantAsked || ran != (tc.want == browserErr) {
			t.Errorf("%s: err=%v asked=%v ran=%v, want err=%v asked=%v", tc.name, err, asked, ran, tc.want, tc.wantAsked)
		}
	}
}

// fakeTerminal is input arriving on a schedule, for drainInput: arrivals[i]
// bytes land before poll i.
type fakeTerminal struct {
	arrivals []int
	poll     int
	queued   int
	flushes  int
	slept    time.Duration
}

func (f *fakeTerminal) pending() (int, error) {
	if f.poll < len(f.arrivals) {
		f.queued += f.arrivals[f.poll]
	}
	f.poll++
	return f.queued, nil
}
func (f *fakeTerminal) flush() error          { f.flushes++; f.queued = 0; return nil }
func (f *fakeTerminal) sleep(d time.Duration) { f.slept += d }

func TestDrainInput(t *testing.T) {
	t.Run("nothing pending: one quiet window", func(t *testing.T) {
		f := &fakeTerminal{}
		drainInput(f.pending, f.flush, f.sleep)
		if f.slept != drainQuiet || f.queued != 0 {
			t.Errorf("slept %v, queued %d, want %v and nothing left", f.slept, f.queued, drainQuiet)
		}
	})
	t.Run("a tail arriving late is still discarded", func(t *testing.T) {
		// 62 bytes 300 ms in, as over a slow SSH link.
		f := &fakeTerminal{arrivals: []int{0, 0, 0, 0, 0, 0, 62}}
		drainInput(f.pending, f.flush, f.sleep)
		if f.queued != 0 || f.flushes < 2 {
			t.Errorf("queued %d after %d flushes, want the tail flushed", f.queued, f.flushes)
		}
		if want := 7*drainPoll + drainQuiet; f.slept != want {
			t.Errorf("slept %v, want %v (a full quiet window after the tail)", f.slept, want)
		}
	})
	t.Run("input that never stops: bounded", func(t *testing.T) {
		arrivals := make([]int, 1000)
		for i := range arrivals {
			arrivals[i] = 1
		}
		f := &fakeTerminal{arrivals: arrivals}
		drainInput(f.pending, f.flush, f.sleep)
		// A literal, not drainMax: the bound itself is what is pinned.
		if limit := 5 * time.Second; f.slept > limit {
			t.Errorf("slept %v, want at most %v", f.slept, limit)
		}
	})
}

func TestAddWorkspaceArgs(t *testing.T) {
	tests := []struct {
		args    []string
		browser bool
		wantErr bool
	}{
		{[]string{"--add-workspace"}, false, false},
		{[]string{"--add-workspace", "--browser"}, true, false},
		{[]string{"--browser", "--add-workspace"}, true, false},
		{[]string{"--browser"}, false, true},
		{[]string{"--add-workspace", "--brower"}, false, true},
	}
	for _, tc := range tests {
		browser, err := addWorkspaceArgs(tc.args)
		if browser != tc.browser || (err != nil) != tc.wantErr {
			t.Errorf("addWorkspaceArgs(%q) = (%v, %v), want (%v, error=%v)", tc.args, browser, err, tc.browser, tc.wantErr)
		}
	}
}

// Running the flow again when the browser session expires must refresh the
// token without writing a second [workspaces] block for the same team, which
// config.Load rejects.
func TestSaveWorkspaceTwiceKeepsConfigLoadable(t *testing.T) {
	dir := t.TempDir()
	t.Setenv("XDG_CONFIG_HOME", dir)
	store := slackclient.NewTokenStore(filepath.Join(dir, "tokens"))
	st := newOnboardingStyles()

	// A real-looking team ID: config.Load rejects one that is not.
	first := slackclient.Token{AccessToken: "xoxc-old", Cookie: "xoxd-old", TeamID: "T0123ABCD", TeamName: "Acme", Domain: "acme"}
	second := first
	second.AccessToken, second.Cookie = "xoxc-new", "xoxd-new"
	for _, tok := range []slackclient.Token{first, second} {
		if err := saveWorkspace(store, tok, st); err != nil {
			t.Fatalf("saveWorkspace: %v", err)
		}
	}

	configPath := filepath.Join(dir, "slk", "config.toml")
	data, err := os.ReadFile(configPath)
	if err != nil {
		t.Fatalf("reading config: %v", err)
	}
	if n := strings.Count(string(data), "[workspaces."); n != 1 {
		t.Errorf("%d workspace blocks, want 1:\n%s", n, data)
	}
	if _, err := config.Load(configPath); err != nil {
		t.Errorf("config.Load: %v", err)
	}
	got, err := store.Load("T0123ABCD")
	if err != nil || got.AccessToken != "xoxc-new" {
		t.Errorf("token = (%+v, %v), want the refreshed one", got, err)
	}
}

// A config.toml that already fails to load (a duplicate an earlier run left)
// is not appended to: one more block cannot fix it.
func TestSaveWorkspaceLeavesUnloadableConfigAlone(t *testing.T) {
	dir := t.TempDir()
	t.Setenv("XDG_CONFIG_HOME", dir)
	configPath := filepath.Join(dir, "slk", "config.toml")
	// Broken by another workspace, so nothing else stops the append.
	broken := "[workspaces.other]\nteam_id = \"T0999ZZZZ\"\n\n[workspaces.other-2]\nteam_id = \"T0999ZZZZ\"\n"
	if err := os.MkdirAll(filepath.Dir(configPath), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(configPath, []byte(broken), 0o644); err != nil {
		t.Fatal(err)
	}
	store := slackclient.NewTokenStore(filepath.Join(dir, "tokens"))
	tok := slackclient.Token{AccessToken: "xoxc-1", Cookie: "xoxd-1", TeamID: "T0123ABCD", TeamName: "Acme", Domain: "acme"}
	if err := saveWorkspace(store, tok, newOnboardingStyles()); err != nil {
		t.Fatalf("saveWorkspace: %v", err)
	}
	if data, _ := os.ReadFile(configPath); string(data) != broken {
		t.Errorf("config.toml changed:\n%s", data)
	}
	if _, err := store.Load("T0123ABCD"); err != nil {
		t.Errorf("token not saved: %v", err)
	}
}

// Adding another workspace through the desktop flow must not replace a
// browser session as a side effect: its row is listed, not pre-selected.
func TestDesktopChoicesLeavesBrowserSessionsUnselected(t *testing.T) {
	ws := []slackdesktop.Workspace{
		{TeamID: "T1", Name: "Acme", Domain: "acme"},
		{TeamID: "T2", Name: "Other", Domain: "other"},
		{TeamID: "T3", Name: "Third", Domain: "third"},
	}
	saved := []slackclient.Token{
		{TeamID: "T1", Source: slackclient.TokenSourceBrowser},
		{TeamID: "T2"}, // from the desktop app: refreshed as before
	}
	choices, chosen := desktopChoices(ws, saved)
	if len(choices) != 3 {
		t.Fatalf("%d rows, want every workspace listed", len(choices))
	}
	if strings.Join(chosen, ",") != "T2,T3" {
		t.Errorf("pre-selected %v, want T2 and T3 only", chosen)
	}
	if !strings.Contains(choices[0].label, "browser session") || strings.Contains(choices[1].label, "browser session") {
		t.Errorf("labels = %q / %q, want only the browser one marked", choices[0].label, choices[1].label)
	}
}

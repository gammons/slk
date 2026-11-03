package slackclient

import (
	"bytes"
	"encoding/json"
	"errors"
	"net/http"
	"strings"
	"testing"
)

const fakeAuthBundle = `{"format":"slk-auth","version":1,"workspace":{"team_id":"T0123ABCD","name":"Example Inc","domain":"example"},"credentials":{"access_token":"xoxc-FAKE-TOKEN-MARKER","cookie":"FAKE-COOKIE-MARKER%2F+=="}}`

func TestAuthBundleRoundTrip(t *testing.T) {
	in := Token{
		TeamID: "T0123ABCD", TeamName: "Example & Co. ☃", Domain: "example-123",
		AccessToken: "xoxc-FAKE-TOKEN-MARKER", Cookie: "FAKE-COOKIE-MARKER%2F+==",
		Source: TokenSourceBrowser, UserID: "U123",
	}
	data, err := EncodeAuthBundle(in)
	if err != nil {
		t.Fatal(err)
	}
	for _, field := range []string{"source", "user_id", "team_name", "browser"} {
		if bytes.Contains(data, []byte(field)) {
			t.Errorf("portable bundle includes local field %s", field)
		}
	}
	got, err := DecodeAuthBundle(append(data, []byte(" \n\t")...))
	if err != nil {
		t.Fatal(err)
	}
	in.Source, in.UserID = "", ""
	if got != in {
		t.Fatal("round trip changed portable fields or retained local policy/identity")
	}
	got, err = DecodeAuthBundle([]byte(fakeAuthBundle))
	if err != nil || got.Cookie != "FAKE-COOKIE-MARKER%2F+==" {
		t.Fatal("decode changed cookie encoding")
	}
}

func TestAuthBundleRejectsInvalidJSON(t *testing.T) {
	for _, tc := range []struct{ name, input string }{
		{"empty", ""},
		{"array", "[]"},
		{"null", "null"},
		{"string", `"FAKE-COOKIE-MARKER"`},
		{"truncated", fakeAuthBundle[:len(fakeAuthBundle)-1]},
		{"trailing document", fakeAuthBundle + `{}`},
		{"trailing junk", fakeAuthBundle + `FAKE-COOKIE-MARKER`},
		{"unknown top key", strings.Replace(fakeAuthBundle, `"format":`, `"FAKE-COOKIE-MARKER":0,"format":`, 1)},
		{"unknown workspace key", strings.Replace(fakeAuthBundle, `"team_id":`, `"FAKE-COOKIE-MARKER":0,"team_id":`, 1)},
		{"unknown credentials key", strings.Replace(fakeAuthBundle, `"cookie":`, `"FAKE-COOKIE-MARKER":0,"cookie":`, 1)},
		{"case variant key", strings.Replace(fakeAuthBundle, `"format"`, `"Format"`, 1)},
		{"duplicate top key", strings.Replace(fakeAuthBundle, `"version":1`, `"version":1,"version":1`, 1)},
		{"duplicate workspace key", strings.Replace(fakeAuthBundle, `"name":"Example Inc"`, `"name":"Example Inc","name":"Example Inc"`, 1)},
		{"duplicate credentials key", strings.Replace(fakeAuthBundle, `"cookie":`, `"cookie":"FAKE-COOKIE-MARKER","cookie":`, 1)},
		{"escaped duplicate key", strings.Replace(fakeAuthBundle, `"version":1`, `"version":1,"\u0076ersion":1`, 1)},
		{"missing format", strings.Replace(fakeAuthBundle, `"format":"slk-auth",`, "", 1)},
		{"wrong format", strings.Replace(fakeAuthBundle, `"slk-auth"`, `"FAKE-COOKIE-MARKER"`, 1)},
		{"wrong version", strings.Replace(fakeAuthBundle, `"version":1`, `"version":2`, 1)},
		{"fractional version", strings.Replace(fakeAuthBundle, `"version":1`, `"version":1.5`, 1)},
		{"boolean version", strings.Replace(fakeAuthBundle, `"version":1`, `"version":true`, 1)},
		{"null format", strings.Replace(fakeAuthBundle, `"slk-auth"`, `null`, 1)},
		{"missing credentials", `{"format":"slk-auth","version":1,"workspace":{"team_id":"T1","name":"Example"}}`},
		{"missing cookie", strings.Replace(fakeAuthBundle, `,"cookie":"FAKE-COOKIE-MARKER%2F+=="`, "", 1)},
		{"missing token", strings.Replace(fakeAuthBundle, `"access_token":"xoxc-FAKE-TOKEN-MARKER",`, "", 1)},
		{"missing team", strings.Replace(fakeAuthBundle, `"team_id":"T0123ABCD",`, "", 1)},
		{"null domain", strings.Replace(fakeAuthBundle, `"domain":"example"`, `"domain":null`, 1)},
		{"local identity", strings.Replace(fakeAuthBundle, `"format":`, `"user_id":"U123","format":`, 1)},
		{"local source", strings.Replace(fakeAuthBundle, `"format":`, `"source":"browser","format":`, 1)},
		{"string version", strings.Replace(fakeAuthBundle, `"version":1`, `"version":"1"`, 1)},
		{"null version", strings.Replace(fakeAuthBundle, `"version":1`, `"version":null`, 1)},
		{"null workspace", strings.Replace(fakeAuthBundle, `{"team_id":"T0123ABCD","name":"Example Inc","domain":"example"}`, `null`, 1)},
		{"null secret", strings.Replace(fakeAuthBundle, `"xoxc-FAKE-TOKEN-MARKER"`, `null`, 1)},
		{"wrong secret type", strings.Replace(fakeAuthBundle, `"xoxc-FAKE-TOKEN-MARKER"`, `[]`, 1)},
		{"invalid utf8", strings.Replace(fakeAuthBundle, "Example Inc", string([]byte{0xff}), 1)},
		{"oversized", fakeAuthBundle + strings.Repeat(" ", AuthBundleLimit)},
	} {
		t.Run(tc.name, func(t *testing.T) {
			got, err := DecodeAuthBundle([]byte(tc.input))
			if !errors.Is(err, ErrInvalidAuthBundle) {
				t.Fatalf("error = %v; want ErrInvalidAuthBundle", err)
			}
			if got != (Token{}) {
				t.Fatal("invalid bundle returned credentials")
			}
			for _, marker := range []string{"FAKE-COOKIE-MARKER", "FAKE-TOKEN-MARKER"} {
				if strings.Contains(err.Error(), marker) {
					t.Fatal("error disclosed a secret marker")
				}
			}
		})
	}
}

func TestAuthBundleFieldValidation(t *testing.T) {
	for _, tc := range []struct {
		name  string
		field string
		value string
	}{
		{"empty token", "access_token", ""},
		{"missing token suffix", "access_token", "xoxc-"},
		{"wrong token prefix", "access_token", "xoxb-FAKE-TOKEN-MARKER"},
		{"token whitespace", "access_token", "xoxc-FAKE TOKEN"},
		{"token control", "access_token", "xoxc-FAKE\x00TOKEN"},
		{"long token", "access_token", "xoxc-" + strings.Repeat("a", 40*1024)},
		{"empty cookie", "cookie", ""},
		{"cookie whitespace", "cookie", " FAKE-COOKIE-MARKER"},
		{"cookie unicode whitespace", "cookie", "FAKE\u00a0COOKIE"},
		{"cookie control", "cookie", "FAKE\x1bCOOKIE"},
		{"cookie quote", "cookie", `FAKE"COOKIE`},
		{"quoted cookie", "cookie", `"FAKE-COOKIE-MARKER"`},
		{"cookie semicolon", "cookie", "FAKE;COOKIE"},
		{"cookie backslash", "cookie", `FAKE\COOKIE`},
		{"cookie comma", "cookie", "FAKE,COOKIE"},
		{"cookie non ascii", "cookie", "FAKEéCOOKIE"},
		{"cookie replacement rune", "cookie", "FAKE\ufffdCOOKIE"},
		{"cookie delete", "cookie", "FAKE\x7fCOOKIE"},
		{"cookie newline", "cookie", "FAKE\r\nCOOKIE"},
		{"long cookie", "cookie", strings.Repeat("a", 40*1024)},
		{"empty team", "team_id", ""},
		{"team prefix", "team_id", "U123"},
		{"short team", "team_id", "T"},
		{"lowercase team", "team_id", "T123abc"},
		{"relative traversal", "team_id", "../T123"},
		{"nested traversal", "team_id", "T123/../T999"},
		{"windows separator", "team_id", `T123\T999`},
		{"absolute path", "team_id", "/T123"},
		{"drive path", "team_id", `T:\T123`},
		{"team control", "team_id", "T123\n"},
		{"long team", "team_id", "T" + strings.Repeat("A", 1000)},
		{"name control", "name", "Example\x1b[31m"},
		{"name newline", "name", "Example\nInc"},
		{"name c1 control", "name", "Example\u009b31m"},
		{"long name", "name", strings.Repeat("a", 4096)},
		{"url domain", "domain", "https://example.slack.com"},
		{"host domain", "domain", "example.slack.com"},
		{"domain path", "domain", "example/path"},
		{"domain control", "domain", "example\n"},
		{"domain leading hyphen", "domain", "-example"},
		{"domain trailing hyphen", "domain", "example-"},
		{"domain underscore", "domain", "example_name"},
		{"domain non ascii", "domain", "éxample"},
		{"domain unicode case fold", "domain", "Klub.enterprise"},
		{"grid unicode suffix", "domain", "example.enterpriſe"},
		{"domain leading dot", "domain", ".example"},
		{"domain trailing dot", "domain", "example."},
		{"grid missing label", "domain", ".enterprise"},
		{"grid leading hyphen", "domain", "-example.enterprise"},
		{"grid trailing hyphen", "domain", "example-.enterprise"},
		{"grid extra label", "domain", "example.other.enterprise"},
		{"grid empty label", "domain", "example..enterprise"},
		{"grid host", "domain", "example.enterprise.slack.com"},
		{"grid url", "domain", "https://example.enterprise.slack.com"},
		{"grid path", "domain", "example.enterprise/path"},
		{"grid port", "domain", "example.enterprise:443"},
		{"grid userinfo", "domain", "user@example.enterprise"},
		{"grid query", "domain", "example.enterprise?other"},
		{"grid fragment", "domain", "example.enterprise#other"},
		{"grid long label", "domain", strings.Repeat("a", 64) + ".enterprise"},
		{"long domain", "domain", strings.Repeat("a", 64)},
	} {
		t.Run(tc.name, func(t *testing.T) {
			var doc map[string]any
			if err := json.Unmarshal([]byte(fakeAuthBundle), &doc); err != nil {
				t.Fatal(err)
			}
			section := "workspace"
			if tc.field == "cookie" || tc.field == "access_token" {
				section = "credentials"
			}
			doc[section].(map[string]any)[tc.field] = tc.value
			data, err := json.Marshal(doc)
			if err != nil {
				t.Fatal(err)
			}
			if _, err := DecodeAuthBundle(data); !errors.Is(err, ErrInvalidAuthBundle) {
				t.Fatalf("decode error = %v; want ErrInvalidAuthBundle", err)
			}
			in, err := DecodeAuthBundle([]byte(fakeAuthBundle))
			if err != nil {
				t.Fatal(err)
			}
			switch tc.field {
			case "access_token":
				in.AccessToken = tc.value
			case "cookie":
				in.Cookie = tc.value
			case "team_id":
				in.TeamID = tc.value
			case "name":
				in.TeamName = tc.value
			case "domain":
				in.Domain = tc.value
			}
			if data, err := EncodeAuthBundle(in); !errors.Is(err, ErrInvalidAuthBundle) || data != nil {
				t.Fatalf("encode error = %v; want ErrInvalidAuthBundle and no bytes", err)
			}
		})
	}
}

func TestAuthBundleDomainRoundTrip(t *testing.T) {
	for _, domain := range []string{
		"", "a", "Example-123", "hackclub.enterprise", "Example-123.enterprise", "Example.ENTERPRISE",
		strings.Repeat("a", 63), strings.Repeat("a", 63) + ".enterprise",
	} {
		t.Run(domain, func(t *testing.T) {
			in, err := DecodeAuthBundle([]byte(fakeAuthBundle))
			if err != nil {
				t.Fatal(err)
			}
			in.Domain = domain
			data, err := EncodeAuthBundle(in)
			if err != nil {
				t.Fatal(err)
			}
			got, err := DecodeAuthBundle(data)
			if err != nil || got != in {
				t.Fatalf("domain round trip changed metadata: %v", err)
			}
		})
	}
}

func TestAuthBundleCookieRoundTrip(t *testing.T) {
	for _, cookie := range []string{
		"FAKE-COOKIE-MARKER%2F+==",
		"FAKE%2f%3B%22%5C%80+==",
		"FAKE!#$%&'()*+-./0123456789:<=>?@[]^_`{|}~",
	} {
		t.Run(cookie, func(t *testing.T) {
			in, err := DecodeAuthBundle([]byte(fakeAuthBundle))
			if err != nil {
				t.Fatal(err)
			}
			in.Cookie = cookie
			data, err := EncodeAuthBundle(in)
			if err != nil {
				t.Fatal(err)
			}
			got, err := DecodeAuthBundle(data)
			if err != nil || got != in {
				t.Fatalf("cookie round trip changed credentials: %v", err)
			}
			req := &http.Request{Header: make(http.Header)}
			req.AddCookie(&http.Cookie{Name: "d", Value: got.Cookie})
			if req.Header.Get("Cookie") != "d="+cookie {
				t.Fatal("HTTP serialization changed cookie bytes")
			}
			parsed, err := req.Cookie("d")
			if err != nil || parsed.Value != cookie {
				t.Fatalf("HTTP parsing changed cookie bytes: %v", err)
			}
		})
	}
}

func TestAuthBundleOptionalMetadataAndSizeBoundary(t *testing.T) {
	input := strings.Replace(fakeAuthBundle, `,"domain":"example"`, "", 1)
	if _, err := DecodeAuthBundle([]byte(input)); err != nil {
		t.Fatalf("optional domain: %v", err)
	}
	data := []byte(fakeAuthBundle + strings.Repeat(" ", AuthBundleLimit-len(fakeAuthBundle)))
	if _, err := DecodeAuthBundle(data); err != nil {
		t.Fatalf("exact size limit: %v", err)
	}
}

func TestTokenUsesDesktopLogin(t *testing.T) {
	for _, source := range []string{"", TokenSourceBrowser, TokenSourceImported, "future-source"} {
		if got := (Token{Source: source}).UsesDesktopLogin(); got != (source == "") {
			t.Errorf("source %q: UsesDesktopLogin = %v", source, got)
		}
	}
}

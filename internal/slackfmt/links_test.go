package slackfmt

import "testing"

func TestStripMarkupLinkSchemes(t *testing.T) {
	cases := map[string]string{
		"<http://a.example/x>":         "http://a.example/x",
		"<ftp://a.example|files>":      "files",
		"<file:///tmp/a.txt|a.txt>":    "a.txt",
		"<tel:+15550100>":              "tel:+15550100",
		"<mailto:a@b.co|a@b.co>":       "a@b.co",
		"<javascript:alert(1)|x>":      "<javascript:alert(1)|x>",
		"<data:text/html;base64,AAAA>": "<data:text/html;base64,AAAA>",
	}
	for in, want := range cases {
		if got := StripMarkup(in, nil); got != want {
			t.Errorf("StripMarkup(%q) = %q, want %q", in, got, want)
		}
	}
}

package linkscheme

import (
	"regexp"
	"testing"
)

func TestAlternationMatchesEveryPrefix(t *testing.T) {
	re := regexp.MustCompile(`^(?:` + Alternation + `)`)
	for _, p := range Prefixes {
		if !re.MatchString(p + "x") {
			t.Errorf("Alternation does not match %q", p)
		}
	}
	for _, s := range []string{"javascript:alert(1)", "data:text/html,x", "vbscript:x", "<#C1|x>", "<@U1>", "httpx://a", "HTTP://a"} {
		if re.MatchString(s) {
			t.Errorf("Alternation matches %q", s)
		}
	}
}

func TestAlternationIsPinned(t *testing.T) {
	want := []string{"http://", "https://", "ftp://", "ftps://", "sftp://", "ssh://", "file:", "mailto:", "tel:"}
	if len(Prefixes) != len(want) {
		t.Fatalf("Prefixes = %q, want %q", Prefixes, want)
	}
	for i := range want {
		if Prefixes[i] != want[i] {
			t.Errorf("Prefixes[%d] = %q, want %q", i, Prefixes[i], want[i])
		}
	}
}

func TestHasPrefix(t *testing.T) {
	for s, want := range map[string]bool{
		"https://a": true, "file:///x": true, "tel:+1": true, "mailto:a@b": true,
		"javascript:x": false, "": false, "www.a.com": false,
	} {
		if got := HasPrefix(s); got != want {
			t.Errorf("HasPrefix(%q) = %v, want %v", s, got, want)
		}
	}
}

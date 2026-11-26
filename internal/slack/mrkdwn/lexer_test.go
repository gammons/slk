package mrkdwn

import (
	"errors"
	"testing"

	"github.com/okneniz/parsec"
	pstrings "github.com/okneniz/parsec/strings"
)

func lexString(src string) ([]mdToken, error) {
	buf := pstrings.Buffer([]rune(src))
	stream := make([]mdToken, 0, 64)

	for {
		tok, err := lex()(&mdState{}, buf)
		if errors.Is(err, parsec.ErrEndOfFile) {
			break
		}
		if err != nil {
			return nil, err
		}

		stream = append(stream, tok)
	}

	return stream, nil
}

func TestLexer(t *testing.T) {
	tests := []struct {
		name  string
		src   string
		kinds []kind
		want  []string
	}{
		{
			name:  "basic",
			src:   "hello, world!",
			kinds: []kind{kindWord, kindPunctuation, kindWhitespace, kindWord, kindPunctuation},
			want:  []string{"hello", ",", " ", "world", "!"},
		},
		{
			name:  "styled",
			src:   "*bold* _italics_ ~strike~",
			kinds: []kind{kindPunctuation, kindWord, kindPunctuation, kindWhitespace, kindPunctuation, kindWord, kindPunctuation, kindWhitespace, kindPunctuation, kindWord, kindPunctuation},
			want:  []string{"*", "bold", "*", " ", "_", "italics", "_", " ", "~", "strike", "~"},
		},
		{
			name:  "newline",
			src:   "*bold*\n_italics_\n~strike~",
			kinds: []kind{kindPunctuation, kindWord, kindPunctuation, kindNewline, kindPunctuation, kindWord, kindPunctuation, kindNewline, kindPunctuation, kindWord, kindPunctuation},
			want:  []string{"*", "bold", "*", "\n", "_", "italics", "_", "\n", "~", "strike", "~"},
		},
		{
			name:  "url",
			src:   "http://example.com/foo.txt",
			kinds: []kind{kindWord, kindPunctuation, kindPunctuation, kindPunctuation, kindWord, kindPunctuation, kindWord, kindPunctuation, kindWord, kindPunctuation, kindWord},
			want:  []string{"http", ":", "/", "/", "example", ".", "com", "/", "foo", ".", "txt"},
		},
		{
			name:  "weird url",
			src:   "http://example.com/résumé.txt",
			kinds: []kind{kindWord, kindPunctuation, kindPunctuation, kindPunctuation, kindWord, kindPunctuation, kindWord, kindPunctuation, kindWord, kindPunctuation, kindWord},
			want:  []string{"http", ":", "/", "/", "example", ".", "com", "/", "résumé", ".", "txt"},
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			tokens, err := lexString(test.src)
			if err != nil {
				t.Fatalf("lex(%q) failed: %v", test.src, err)
			}

			if len(tokens) != len(test.kinds) {
				t.Fatalf("lex(%q) produced %d tokens (%v), want %d", test.src, len(tokens), tokens, len(test.kinds))
			}

			for i, tok := range tokens {
				if string(tok.Lexeme) != test.want[i] {
					t.Errorf("token %d: %q, want %q", i, tok.Lexeme, test.want[i])
				}

				if tok.Kind != test.kinds[i] {
					t.Errorf("token %d (%q): kind %v, want %v", i, tok.Lexeme, tok.Kind, test.kinds[i])
				}
			}
		})
	}
}

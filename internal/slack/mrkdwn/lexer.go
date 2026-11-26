package mrkdwn

import (
	"slices"
	"strings"

	"github.com/okneniz/parsec"
	pstrings "github.com/okneniz/parsec/strings"
	"github.com/okneniz/parsec/tokens"
)

type kind string

type lexeme string

type mdToken = tokens.Token[kind, lexeme]

const (
	kindWhitespace  kind = "whitespace"
	kindPunctuation kind = "punctuation"
	kindWord        kind = "word"
	kindNewline     kind = "newline"
)

func newWhitespace(r []rune) (mdToken, error) {
	return mdToken{Kind: kindWhitespace, Lexeme: lexeme(r)}, nil
}
func newPunctuation(r rune) (mdToken, error) {
	return mdToken{Kind: kindPunctuation, Lexeme: lexeme(r)}, nil
}
func newWord(r []rune) (mdToken, error) {
	return mdToken{Kind: kindWord, Lexeme: lexeme(r)}, nil
}
func newNewline(r rune) (mdToken, error) {
	return mdToken{Kind: kindNewline, Lexeme: lexeme(r)}, nil
}

// Turn the token into a string
func toString(t mdToken) (string, error) {
	return string(t.Lexeme), nil
}

func castToString(p parserToken) parserString {
	return parsec.Cast(p, toString)
}

// Concatenate multiple tokens together into a string
func toStrings(ts []mdToken) (string, error) {
	var s strings.Builder
	for _, t := range ts {
		s.WriteString(string(t.Lexeme))
	}
	return s.String(), nil
}

// Turn a token into its first rune
func toRune(t mdToken) (rune, error) {
	return rune(string(t.Lexeme)[0]), nil
}

// Parse the exact tokens matching a string, regardless of their type
func tokenS(s string) parserString {
	var parsers []parserToken
	for _, char := range s {
		parsers = append(parsers, tokens.Satisfy(lexer, s, func(t mdToken) bool {
			return t.Lexeme == lexeme(char)
		}))
	}
	return parsec.Cast(parsec.Sequence(0, parsers...), toStrings)
}

// Parse the exact word given in the string
func word(s string) parserString {
	return parsec.Cast(tokens.Exact(lexer, kindWord, lexeme(s)), toString)
}

// Parse any word
func anyWord() parserString {
	return parsec.Cast(tokens.OfKind(lexer, kindWord), toString)
}

// Parse any whitespace
var whitespace = parsec.Cast(tokens.OfKind(lexer, kindWhitespace), toString)

// Parse multiple whitespaces
var whitespaces = parsec.Cast(parsec.Many(0, try(whitespace)), concatStrings)

// Parse a single newline
var newline = parsec.Cast(tokens.OfKind(lexer, kindNewline), toString)

// Parse a single given punctuation symbol
func punct(r rune) parserRune {
	return parsec.Cast(tokens.Exact(lexer, kindPunctuation, lexeme(string(r))), toRune)
}

// Parse a single given punctuation symbol, returning it as string
func punctS(r rune) parserString {
	return parsec.Cast(tokens.Exact(lexer, kindPunctuation, lexeme(string(r))), toString)
}

// Parse anything that's not a word (i.e. whitespace or punctuation)
// Useful for determining word boundaries
func nonWord() parserSectionElement {
	return castText(tokens.Satisfy(lexer, "expected non-word",
		func(t mdToken) bool { return t.Kind != kindWord }))
}

// Parse anything that's not whitespace (i.e. word or punctuation)
// Useful for determining word boundaries
func nonWS() parserSectionElement {
	return castText(tokens.Satisfy(lexer, "expected non-whitespace",
		func(t mdToken) bool { return t.Kind != kindWhitespace && t.Kind != kindNewline }))
}

var nonNewline = tokens.Satisfy(lexer, "expected anything other than newline",
	func(t mdToken) bool { return t.Kind != kindNewline })

// Parse a token which is neither whitespace, newline, nor one of the chars in the given string
func tokenNoneOf(chars string) parserString {
	return castToString(tokens.Satisfy(
		lexer,
		"expected part",
		func(t mdToken) bool {
			return t.Kind != kindWhitespace && t.Kind != kindNewline &&
				!slices.Contains([]rune(chars), []rune(t.Lexeme)[0])
		},
	))
}

func lex() tokens.Lexer[kind, lexeme, *mdState] {
	newlineChars := "\n\r"
	newline := try(pstrings.OneOf[*mdState]("expected newline", []rune(newlineChars)...))

	spaceChars := " \t\f"
	space := try(pstrings.OneOf[*mdState]("expected whitespace", []rune(spaceChars)...))
	spaces := parsec.Some(0, "expected run of whitespace", space)

	punctChars := "!\"#$%&'()*+,-./:;<=>?@[\\]^_`{|}~"
	punct := pstrings.OneOf[*mdState]("expected punctuation", []rune(punctChars)...)

	wordChar := try(pstrings.NoneOf[*mdState]("expected word character", []rune(spaceChars+punctChars+newlineChars)...))
	word := parsec.Some(0, "expected word", wordChar)

	token := parsec.Choice("mrkdwn token",
		try(parsec.Cast(newline, newNewline)),
		try(parsec.Cast(punct, newPunctuation)),
		try(parsec.Cast(spaces, newWhitespace)),
		try(parsec.Cast(word, newWord)),
	)

	return func(state *mdState, buf parsec.Buffer[rune, pstrings.Position]) (mdToken, error) {
		if buf.IsEOF() {
			return mdToken{}, parsec.ErrEndOfFile
		}
		tok, err := token(state, buf)
		if err == nil {
			state.prevTokenKind = &tok.Kind
		}
		return tok, err
	}
}

var lexer = lex()

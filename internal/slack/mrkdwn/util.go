package mrkdwn

import (
	"fmt"
	"strings"

	"github.com/okneniz/parsec"
	pstrings "github.com/okneniz/parsec/strings"
	slk "github.com/slack-go/slack"
)

// Merge together adjacent section elements, if possible (same type & style)
//
// Also handles merging only the skin tone swatches into the preceding emoji.
func mergeSectionElements(elements ...slk.RichTextSectionElement) []slk.RichTextSectionElement {
	var new []slk.RichTextSectionElement
	for i, n := range elements {
		if i == 0 {
			new = append(new, n)
			continue
		}
		prev := new[len(new)-1]
		switch t := n.(type) {
		// Merge together text elements of the same style
		case *slk.RichTextSectionTextElement:
			if prevtext, match := prev.(*slk.RichTextSectionTextElement); match && (t.Style == prevtext.Style || t.Style != nil && prevtext.Style != nil && *t.Style == *prevtext.Style) {
				copied := copy(prevtext)
				copied.Text += t.Text
				new[len(new)-1] = copied
				continue
			}
		// Merge the skin-tone swatch emoji with the previous (real) emoji
		case *slk.RichTextSectionEmojiElement:
			if prevemoji, match := prev.(*slk.RichTextSectionEmojiElement); match && strings.HasPrefix(t.Name, "skin-tone-") {
				copied := copy(prevemoji)
				copied.Name += "::" + t.Name
				new[len(new)-1] = copied
				continue
			}
		}
		new = append(new, n)
	}
	return new
}

// Merge together multiple text elements if possible (same type & other properties)
//
// This also merges the underlying section elements with [mergeSectionElements]
func Merge(elements []slk.RichTextElement) []slk.RichTextElement {
	var new []slk.RichTextElement
	var prev slk.RichTextElement
	for i, rte := range elements {
		if i > 0 {
			prev = new[len(new)-1]
		}
		switch t := rte.(type) {
		case *slk.RichTextSection:
			if prevm, match := prev.(*slk.RichTextSection); match {
				copied := copy(prevm)
				copied.Elements = append(append([]slk.RichTextSectionElement{}, copied.Elements...), t.Elements...)
				copied.Elements = mergeSectionElements(copied.Elements...)
				new[len(new)-1] = copied
				continue
			}
		case *slk.RichTextList:
			if prevm, match := prev.(*slk.RichTextList); match {
				if prevm.Style == t.Style && prevm.Border == t.Border && prevm.Indent == t.Indent {
					copied := copy(prevm)
					copied.Elements = append(append([]slk.RichTextElement{}, copied.Elements...), t.Elements...)
					new[len(new)-1] = copied
					continue
				}
			}
		case *slk.RichTextQuote:
			if prevm, match := prev.(*slk.RichTextQuote); match && prevm.Border == t.Border {
				copied := copy(prevm)
				copied.Elements = append(append([]slk.RichTextSectionElement{}, copied.Elements...), slk.NewRichTextSectionTextElement("\n", nil))
				copied.Elements = append(copied.Elements, t.Elements...)
				copied.Elements = mergeSectionElements(copied.Elements...)
				new[len(new)-1] = copied
				continue
			}
		}
		new = append(new, rte)
	}
	return new
}

// Fixes the indents and offsets of all the lists in the given elements
//
// May modify the slice in-place
func fixListIndentsOffsets(elements []slk.RichTextElement) []slk.RichTextElement {
	var prev slk.RichTextElement
	for i, rte := range elements {
		if i > 0 {
			prev = elements[i-1]
		}
		if l, match := rte.(*slk.RichTextList); match {
			prevm, match := prev.(*slk.RichTextList)
			prevIndent := 0
			if match {
				prevIndent = prevm.Indent
			}
			if l.Indent-prevIndent > 1 {
				l.Indent = prevIndent + (l.Indent-prevIndent)/2
			}
		}
	}
	return elements
}

// Return just a single item as a list element. This can never error
func singleton[A any](a A) ([]A, error) {
	return []A{a}, nil
}

// Return just a single item as a list element. This can never error
func castSingleton[T any, P any, O any, S any](p parsec.Combinator[T, P, O, S]) parsec.Combinator[T, P, []O, S] {
	return parsec.Cast(p, singleton)
}

// Same as parsec.try, but also restores the state on failure
func try[T any, P any, O any](c parsec.Combinator[T, P, O, *mdState]) parsec.Combinator[T, P, O, *mdState] {
	var null O

	return func(state *mdState, buffer parsec.Buffer[T, P]) (O, parsec.Error[P]) {
		pos := buffer.Position()
		copied := copy(state)

		result, err := c(state, buffer)
		if err != nil {
			*state = *copied
			if seekErr := buffer.Seek(pos); seekErr != nil {
				return null, parsec.NewParseError(
					buffer.Position(),
					seekErr.Error(),
					err,
				)
			}

			return null, err
		}

		return result, nil
	}
}

// Applies the parser, and then restores the state and position regardless of the parser's outcome.
// Basically, this can "parse into the future".
// Useful to see what the next tokens are to decide if the parse is correct or not, e.g. "is this followed by..."
func lookAhead[T any, P any, O any, S any](c parsec.Combinator[T, P, O, *S]) parsec.Combinator[T, P, O, *S] {
	var null O

	return func(state *S, buffer parsec.Buffer[T, P]) (O, parsec.Error[P]) {
		pos := buffer.Position()

		copied := copy(state)
		result, err := c(state, buffer)
		*state = *copied
		if seekErr := buffer.Seek(pos); seekErr != nil {
			return null, parsec.NewParseError(
				buffer.Position(),
				seekErr.Error(),
				err,
			)
		}

		return result, err
	}
}

// Applies the parser, restores the state and position regardless of the parser's outcome.
// Fails if the parser was successful, otherwise succeds with an "empty" output.
// This expresses a "this is not followed by" logic.
func notFollowedBy[T any, P any, O any, S any](
	step func(state *S, buf parsec.Buffer[T, P]) (O, parsec.Error[P]),
	context string,
) parsec.Combinator[T, P, O, *S] {
	var null O

	return func(state *S, buf parsec.Buffer[T, P]) (O, parsec.Error[P]) {
		pos := buf.Position()

		copied := copy(state)
		result, err := step(state, buf)
		*state = *copied
		if seekErr := buf.Seek(pos); seekErr != nil {
			return null, parsec.NewParseError(
				buf.Position(),
				seekErr.Error(),
				err,
			)
		}
		if err != nil {
			return null, nil
		}

		return null, parsec.NewParseError(pos, fmt.Sprintf("unexpected %v, %s", result, context))
	}
}

// Parse a given parser, but do not fail if it failed due to EOF
// Useful for determining word boundaries
func orEOF[T any, P any, O any, S any](p parsec.Combinator[T, P, O, S]) parsec.Combinator[T, P, O, S] {
	return func(state S, buf parsec.Buffer[T, P]) (O, parsec.Error[P]) {
		var null O
		result, err := p(state, buf)
		if err != nil && buf.IsEOF() {
			return null, nil
		}
		return result, err
	}
}

// Turn a token into RichTextSectionTextElement
func toText(style *slk.RichTextSectionTextStyle) func(mdToken) (*slk.RichTextSectionTextElement, error) {
	return func(t mdToken) (*slk.RichTextSectionTextElement, error) {
		return slk.NewRichTextSectionTextElement(string(t.Lexeme), copy(style)), nil
	}
}

// Cast a token into RichTextSectionTextElement, applying the style from current parser state
// This is the most common way we turn tokens into text
func castText(p parserToken) parserSectionElement {
	return func(state *mdState, buf parsec.Buffer[rune, pstrings.Position]) (slk.RichTextSectionElement, parsec.Error[pstrings.Position]) {
		return parsec.Cast(p, toText(state.style))(state, buf)
	}
}

// Concatenate two slices together
//
// Convenience function for [parsec.Cast]
func concatSlices[A any](a []A, b []A) []A {
	return append(a, b...)
}

// Make a slice from two elements
//
// Convenience function for [parsec.Cast]
func mkSlice[A any](a A, b A) []A {
	return []A{a, b}
}

// Concat a slice of strings into one string
//
// Convenience function for [parsec.Cast]
func concatStrings(s []string) (string, error) {
	return strings.Join(s, ""), nil
}

// Parse a list of strings, then concat into one string.
func castConcatStrings(p parserStrings) parserString {
	return parsec.Cast(p, concatStrings)
}

// Copy a variable behind pointer
// Used to make a copy of the current state style, so future changes do not affect the style of already parsed elements
func copy[A any](a *A) *A {
	if a == nil {
		return nil
	}
	cp := *a
	return &cp
}

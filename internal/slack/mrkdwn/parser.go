package mrkdwn

import (
	"errors"
	"fmt"
	"slices"
	"strconv"

	"strings"

	"github.com/gammons/slk/internal/linkscheme"
	"github.com/okneniz/parsec"
	pstrings "github.com/okneniz/parsec/strings"
	"github.com/okneniz/parsec/tokens"
	"github.com/slack-go/slack"
)

type mdState struct {
	style         *slack.RichTextSectionTextStyle
	prevTokenKind *kind
	parseAngled   bool
}

type parserToken = parsec.Combinator[rune, pstrings.Position, mdToken, *mdState]
type parserRune = parsec.Combinator[rune, pstrings.Position, rune, *mdState]
type parserSectionElement = parsec.Combinator[rune, pstrings.Position, slack.RichTextSectionElement, *mdState]
type parserString = parsec.Combinator[rune, pstrings.Position, string, *mdState]
type parserStrings = parsec.Combinator[rune, pstrings.Position, []string, *mdState]
type parserInline = parsec.Combinator[rune, pstrings.Position, []slack.RichTextSectionElement, *mdState]
type parserElement = parsec.Combinator[rune, pstrings.Position, slack.RichTextElement, *mdState]
type buffer = parsec.Buffer[rune, pstrings.Position]
type mdErr = parsec.Error[pstrings.Position]

// Parse a given delimiter, but only if it "flanks" mrkdwn content from the left;
// That is, it is not preceded by a word and it is succeeded by a non-whitespace
func leftFlankingDelim(delim rune) parserInline {
	return func(state *mdState, buf buffer) ([]slack.RichTextSectionElement, mdErr) {
		// If the previous token is a word, consume a non-word token to ensure we're on a word boundary
		// E.g. foo*bar* should be kept verbatim
		var content []slack.RichTextSectionElement
		if state.prevTokenKind != nil && *state.prevTokenKind == kindWord {
			return content, parsec.NewParseError(buf.Position(), "expected a word separator")
		}
		_, err := parsec.Skip(punct(delim), lookAhead(nonWS()))(state, buf)
		return content, err
	}
}

// Parse a given delimiter, but only if it "flanks" mrkdwn content from the right;
// That is, it is preceded by non-whitespace and is not succeeded by a word
func rightFlankingDelim(delim rune) parserInline {
	return func(state *mdState, buf buffer) ([]slack.RichTextSectionElement, mdErr) {
		// If the previous token is a word, consume a non-word token to ensure we're on a word boundary
		// E.g. foo*bar* should be kept verbatim
		var content []slack.RichTextSectionElement
		if state.prevTokenKind == nil || *state.prevTokenKind == kindWhitespace || *state.prevTokenKind == kindNewline {
			return content, parsec.NewParseError(buf.Position(), "expected a word boundary")
		}
		_, err := parsec.Skip(try(punct(delim)), lookAhead(orEOF(nonWord())))(state, buf)
		return content, err
	}
}

// Parse a given inline parser until a right-flanking delimiter is found.
//
// Instances of the same delimiter which are not flanking the content from the
// right will fail the parser.
func tilRightDelim(parser parserInline, delim rune) parserInline {
	return func(state *mdState, buf buffer) ([]slack.RichTextSectionElement, mdErr) {
		rfd := try(rightFlankingDelim(delim))
		// Do not allow the same delimiter to be exist inside this one
		// This ensures that *foo *bar* goo* => *foo <b>bar</b> goo* (the outer asterisks are not parsed as bold)
		// And *hello*world* stays verbatim
		unexpected := notFollowedBy(punct(delim), "unexpected delim")
		md := try(parsec.And(parsec.Skip(unexpected, parser), tilRightDelim(parser, delim), concatSlices))
		result, err := parsec.Choice("expected mrkdwn or right delim", rfd, md)(state, buf)
		return result, err
	}
}

// A mrkdwn text style delimiter
type styleDelim rune

const (
	boldDelim   styleDelim = '*'
	italicDelim styleDelim = '_'
	strikeDelim styleDelim = '~'
	codeDelim   styleDelim = '`'
)

// Parse internal content after a style delimiter
// Parses anything for code, and mrkdwn sections for others
func styleInternalParser(delim styleDelim) parserInline {
	switch delim {
	case codeDelim:
		return tilRightDelim(castSingleton(castText(nonNewline)), rune(delim))
	default:
		return tilRightDelim(sectionElement(), rune(delim))
	}
}

// Update the slack style correspondingly to the mrkdwn style
func setStyle(delim styleDelim, style slack.RichTextSectionTextStyle) slack.RichTextSectionTextStyle {
	switch delim {
	case boldDelim:
		style.Bold = true
	case italicDelim:
		style.Italic = true
	case strikeDelim:
		style.Strike = true
	case codeDelim:
		style.Code = true
	}

	return style
}

// Parse a run of mrkdwn styled by one of the styles (_italic_, *bold*, ~strikethru~, `code`)
// Instances of the same delimiter inside the run will fail the parser, e.g. *foo*bar* will not parse.
// Failed parse does not consume any tokens (for convenience in [styled])
func styledWith(delim styleDelim) parserInline {
	return func(state *mdState, buf buffer) ([]slack.RichTextSectionElement, mdErr) {
		parser := parsec.Skip(leftFlankingDelim(rune(delim)), styleInternalParser(delim))

		prev := state.style
		if state.style == nil {
			state.style = &slack.RichTextSectionTextStyle{}
		}

		new := setStyle(delim, *state.style)
		state.style = &new

		content, err := try(parser)(state, buf)

		state.style = prev
		return content, err
	}
}

func styled() parserInline {
	return parsec.Choice(
		"expected style",
		styledWith(boldDelim),
		styledWith(italicDelim),
		styledWith(strikeDelim),
		styledWith(codeDelim),
	)
}

// Parse a mrkdwn link, e.g. [this is link text](https://example.com)
func link(state *mdState, buf buffer) (slack.RichTextSectionElement, mdErr) {
	textPart := tokens.Satisfy(lexer, "expected word part", func(t mdToken) bool {
		return t.Kind != kindNewline && t.Lexeme != lexeme("]")
	})
	text := parsec.Cast(parsec.Between(punct('['), parsec.Some(0, "link text", try(textPart)), punct(']')), toStrings)
	url := parsec.Between(punct('('), urlPartHandleMatchingBkts("", "", ")"), punct(')'))
	parser := parsec.And(text, url, func(text string, url string) slack.RichTextSectionElement {
		return slack.NewRichTextSectionLinkElement(url, text, copy(state.style))
	})
	return parser(state, buf)
}

// Parse a token that's neither in never nor sometimes,
// or a token which is in sometimes followed by a (discarded) token which is in neither.
func nonTrailedToken(never string, sometimes string) parserString {
	following := lookAhead(tokenNoneOf(sometimes + never))
	followed := parsec.SkipAfter(following, tokenNoneOf(never))
	return parsec.Or("expected a url token", try(followed), try(tokenNoneOf(sometimes+never)))
}

// Parse a url part, handling matching brackets:
// ")" => no parse
// "foo)" => "foo"
// "foo(bar)" => "foo(bar)"
//
// This is useful because URLs may contain matching brackets, e.g. "https://en.wikipedia.org/wiki/Mercury_(planet)"
// And at the same time URLs can be surrounded by brakets, e.g. "(see https://example.com)"
//
// We must parse the first as a URL with a bracket at the end, and the second without the bracket.
func urlPartHandleMatchingBkts(never string, sometimes string, brackets string) parserString {
	return func(state *mdState, buf buffer) (string, mdErr) {
		maybeEmptyUP := parsec.Optional(urlPartHandleMatchingBkts(never, sometimes, brackets), "")
		withParens := castConcatStrings(parsec.Sequence(0, punctS('('), maybeEmptyUP, punctS(')')))
		withSqBrkt := castConcatStrings(parsec.Sequence(0, punctS('['), maybeEmptyUP, punctS(']')))
		withCurly := castConcatStrings(parsec.Sequence(0, punctS('{'), maybeEmptyUP, punctS('}')))
		return castConcatStrings(
			parsec.Some(0, "expected url parts",
				parsec.Choice("expected url part",
					try(withParens),
					try(withSqBrkt),
					try(withCurly),
					try(nonTrailedToken(never+"`<>"+brackets, sometimes)),
				),
			),
		)(state, buf)
	}
}

// Parse a URL host part (intended to be separated by periods)
var urlHostPart = castConcatStrings(parsec.Some(0, "expected url host part", parsec.Choice("expected url host part", try(anyWord()), try(punctS('-')))))

// Parse a URL host part (without the port)
//
// Please note that IPv6 hosts are not supported, same as with the upstream slack client.
var urlHost = castConcatStrings(
	parsec.Concat(0,
		castSingleton(try(urlHostPart)),
		parsec.Many(0, try(castConcatStrings(parsec.Sequence(0, punctS('.'), urlHostPart)))),
	),
)

// Parse a URL username (e.g. in email or for HTTP authentication)
var urlUsername = castConcatStrings(parsec.Some(0, "expected user name", try(tokenNoneOf("@:[]()<>:;,\"\\´"))))

// Parse a "bare"/"autlinked" URL
// The rules are a bit complicated, but basically:
// 1. Optional protocol
// 2. Required host
// 3. Optional port
// 4. Optional path (potentially containing matching brackets)
// 5. Optional query
// 6. Optional hash
//
// If the protocol is missing, we must either
// 1. Have host be a well-known ccTLD which can not be a file extension
// 2. Have host be a potential-file-extension ccTLD (e.g. .md) and have at least one other URL part (port, path, query, hash)
func bareLinkURL(state *mdState, buf buffer) (slack.RichTextSectionElement, mdErr) {
	var null *slack.RichTextSectionLinkElement
	url := ""
	protocol, protoErr := try(parsec.Sequence(0, anyWord(), tokenS("://")))(state, buf)
	if protoErr == nil {
		url += protocol[0] + protocol[1]
	}
	password := parsec.Optional(try(castConcatStrings(parsec.Sequence(0, punctS(':'), urlUsername))), "")
	auth, authErr := try(parsec.Sequence(0, urlUsername, password, punctS('@')))(state, buf)
	if authErr == nil {
		url += auth[0] + auth[1] + auth[2]
	}
	host, hostErr := urlHost(state, buf)
	if hostErr != nil {
		return null, hostErr
	}
	url += host
	port, portErr := try(parsec.Skip(punct(':'), integer))(state, buf)
	if portErr == nil {
		url += ":" + fmt.Sprintf("%d", port)
	}
	slash, slashErr := try(tokenS("/"))(state, buf)
	if slashErr == nil {
		// FIXME: verify
		path, err := parsec.SepBy(0, try(urlPartHandleMatchingBkts("#?", ",.?!:;*~_", ")]}")), try(punct('/')))(state, buf)
		if err == nil {
			url += slash + strings.Join(path, "/")
		}
		// Optional trailing slash
		slash, err := try(tokenS("/"))(state, buf)
		if err == nil {
			url += slash
		}
	}

	query, queryErr := try(
		parsec.Skip(
			punct('?'),
			parsec.SepBy1(0, "expected query",
				try(urlPartHandleMatchingBkts("#&", ",.?!:;*~_", ")]}")),
				punct('&'),
			),
		),
	)(state, buf)

	if queryErr == nil {
		url += "?" + strings.Join(query, "&")
	}
	anchor, hashErr := try(
		parsec.Skip(
			punct('#'),
			urlPartHandleMatchingBkts("", ",.?!:;*~_", ")]}"),
		),
	)(state, buf)
	if hashErr == nil {
		url += "#" + anchor
	}
	text := url
	if protoErr != nil {
		s := strings.Split(host, ".")
		if len(s) < 2 {
			return null, protoErr
		}
		tld := strings.ToLower(s[len(s)-1])
		if slices.Contains(tldPotentialFileExt, tld) {
			// If the host ends in a TLD that's a potential file extension (e.g. .md), make sure we have some other URL part
			if portErr != nil && slashErr != nil && queryErr != nil && hashErr != nil {
				return null, protoErr
			}
		} else if !slices.Contains(tlds, tld) {
			// If the host is not a ccTLD of any kind, don't match a URL
			return null, protoErr
		}
		// If we don't have a protocol, assume http
		url = "http://" + url
	}
	return &slack.RichTextSectionLinkElement{
		Type:  slack.RTSELink,
		Text:  text,
		URL:   url,
		Style: copy(state.style),
	}, nil
}

func TypesAsBareUrl(url string) bool {
	buf := pstrings.Buffer([]rune(url))
	state := &mdState{}
	_, err := bareLinkURL(state, buf)
	// Check that we have parsed something, and that we have parsed until the end of string
	return err == nil && buf.IsEOF()
}

// Parse a "bare"/"autolinked" URL
func bareLinkEmail(state *mdState, buf buffer) (slack.RichTextSectionElement, mdErr) {
	return parsec.Cast(parsec.Sequence(0, urlUsername, punctS('@'), urlHost), func(s []string) (slack.RichTextSectionElement, error) {
		var null *slack.RichTextSectionLinkElement
		err := parsec.NewParseError(buf.Position(), "expected domain with good ccTLD")
		host := strings.Split(s[2], ".")
		if len(host) < 2 {
			return null, err
		}
		tld := host[len(host)-1]
		if !slices.Contains(tlds, tld) {
			return null, err
		}

		return &slack.RichTextSectionLinkElement{
			Type:  slack.RTSELink,
			Text:  strings.Join(s, ""),
			URL:   "mailto:" + strings.Join(s, ""),
			Style: copy(state.style),
		}, nil
	})(state, buf)
}

// Parse a "bare"/"autolinked" phone number
//
// FIXME: Upstream Slack client is more intelligent, requiring a correct phone number format for the country code.
func bareLinkPhone(state *mdState, buf buffer) (slack.RichTextSectionElement, mdErr) {
	return parsec.Cast(parsec.Skip(punct('+'), integer), func(i int) (slack.RichTextSectionElement, error) {
		var null *slack.RichTextSectionLinkElement
		if i < 1000000 {
			return null, parsec.NewParseError(buf.Position(), "expected a bigger number")
		}

		phone := "+" + fmt.Sprintf("%d", i)

		return &slack.RichTextSectionLinkElement{
			Type:  slack.RTSELink,
			Text:  phone,
			URL:   "tel:" + phone,
			Style: copy(state.style),
		}, nil
	})(state, buf)
}

// Parse an integer
var integer parsec.Combinator[rune, pstrings.Position, int, *mdState] = parsec.Cast(tokens.Satisfy(lexer, "integer", func(t mdToken) bool {
	return t.Kind == kindWord
}), func(t mdToken) (int, error) {
	result, err := strconv.ParseInt(string(t.Lexeme), 10, 0)
	return int(result), err
})

// Parse a shortcode emoji
//
// Note that skin tones are parsed as separate emoji here and are later merged in [mergeSectionElements]
func emoji(state *mdState, buf buffer) (slack.RichTextSectionElement, mdErr) {
	shortcodePart := tokens.Satisfy(lexer, "expected emoji shortcode part", func(t mdToken) bool {
		return t.Kind != kindWhitespace && t.Kind != kindNewline && t.Lexeme != lexeme(":")
	})
	name := parsec.Cast(parsec.Between(punct(':'), parsec.Some(0, "expected shortcode", try(shortcodePart)), punct(':')), toStrings)

	parser := parsec.Cast(name, func(name string) (slack.RichTextSectionElement, error) {
		return &slack.RichTextSectionEmojiElement{
			Type:  slack.RTSEEmoji,
			Name:  name,
			Style: copy(state.style),
		}, nil
	})
	return parser(state, buf)
}

// Parse an "angled" slack token, e.g. <@U123456> or <!here>
//
// This should not be parsed in user-provided mrkdwn, hence we gate behind [mdState.parseAngled]
func angledToken(state *mdState, buf buffer) (slack.RichTextSectionElement, mdErr) {
	if !state.parseAngled {
		var null slack.RichTextSectionElement
		return null, parsec.NewParseError(buf.Position(), "parsing tokens disabled")
	}
	textPart := tokens.Satisfy(lexer, "expected token text part", func(t mdToken) bool {
		return t.Kind != kindNewline && t.Lexeme != lexeme(">")
	})
	text := parsec.Skip(punct('|'), parsec.Cast(parsec.Some(0, "expected token text", try(textPart)), toStrings))
	// N.B. the provided text is simply discarded for all tokens except links
	tokenUser := parsec.Cast(
		parsec.Skip(punct('@'), parsec.SkipAfter(parsec.Optional(try(text), ""), anyWord())),
		func(uid string) (slack.RichTextSectionElement, error) {
			return slack.NewRichTextSectionUserElement(uid, copy(state.style)), nil
		},
	)
	tokenChannel := parsec.Cast(
		parsec.Skip(punct('#'), parsec.SkipAfter(parsec.Optional(try(text), ""), anyWord())),
		func(uid string) (slack.RichTextSectionElement, error) {
			return slack.NewRichTextSectionChannelElement(uid, copy(state.style)), nil
		},
	)
	tokenUserGroup := parsec.Cast(
		parsec.Skip(
			parsec.Sequence(0, punctS('!'), word("subteam"), punctS('^')),
			parsec.SkipAfter(parsec.Optional(try(text), ""), anyWord()),
		),
		func(uid string) (slack.RichTextSectionElement, error) {
			return &slack.RichTextSectionUserGroupElement{Type: slack.RTSEUserGroup, UsergroupID: uid, Style: copy(state.style)}, nil
		},
	)
	validBroadcast := parsec.Choice("expected valid broadcast", word("here"), word("everyone"), word("channel"))
	tokenBroadcast := parsec.Cast(parsec.Skip(punct('!'), validBroadcast), func(uid string) (slack.RichTextSectionElement, error) {
		return slack.NewRichTextSectionBroadcastElement(uid), nil
	})
	urlPart := castToString(tokens.Satisfy(lexer, "expected url part", func(t mdToken) bool {
		return t.Kind != kindNewline && t.Lexeme != lexeme(">") && t.Lexeme != lexeme("|")
	}))
	validLinkScheme := parsec.Cast(
		castConcatStrings(parsec.Sequence(0, anyWord(), punctS(':'), parsec.Optional(try(tokenS("//")), ""))),
		func(scheme string) (string, error) {
			if !linkscheme.HasPrefix(scheme) {
				return "", errors.New("invalid link scheme")
			}
			return scheme, nil
		},
	)
	tokenLink := parsec.And(
		castConcatStrings(parsec.Concat(0,
			castSingleton(validLinkScheme),
			parsec.Some(0, "", try(urlPart))),
		),

		parsec.Optional(try(text), ""),

		func(url string, name string) slack.RichTextSectionElement {
			text := url
			if name != "" {
				text = name
			}
			return slack.NewRichTextSectionLinkElement(url, text, copy(state.style))
		},
	)
	tokens := parsec.Choice("expected slack token", try(tokenUser), try(tokenChannel), try(tokenUserGroup), try(tokenBroadcast), try(tokenLink))
	return parsec.Between(punct('<'), tokens, punct('>'))(state, buf)
}

// Parse a mrkdwn section element
func sectionElement() parserInline {
	return parsec.Choice(
		"expected inline mrkdwn",
		try(styled()),
		castSingleton(try(angledToken)),
		castSingleton(try(bareLinkEmail)),
		castSingleton(try(bareLinkPhone)),
		castSingleton(try(bareLinkURL)),
		castSingleton(try(link)),
		castSingleton(try(emoji)),
		castSingleton(try(castText(nonNewline))),
	)
}

// Parse an entire mrkdwn section until a newline
//
// Note that if there is another plain section after the newline, it will be merged by [Merge]
var section = parsec.Cast(
	parsec.Many(0, sectionElement()),
	func(sections [][]slack.RichTextSectionElement) (slack.RichTextElement, error) {
		var elements []slack.RichTextSectionElement
		for _, sec := range sections {
			elements = append(elements, sec...)
		}
		return slack.NewRichTextSection(mergeSectionElements(elements...)...), nil
	},
)

// Parse a newline as a mrkdwn section
var newlineSection = parsec.Cast(newline, func(nl string) (slack.RichTextElement, error) {
	return slack.NewRichTextSection(slack.NewRichTextSectionTextElement(nl, nil)), nil
})

// Parse a single list item, returning an entire list.
//
// This only parses until a newline;
// If there is another list item on the next line, it will be merged in [Merge].
// Multi-line list items are not supported, in line with upstream Slack behavior.
//
// The offset provided is correct only if this is the first item in the list;
// This is also handled correctly in [Merge].
func listItem(state *mdState, buf buffer) (slack.RichTextElement, mdErr) {
	indent := parsec.Cast(whitespaces, func(s string) (int, error) { return len(s) / 2, nil })
	markerBullet := parsec.SkipAfter(whitespace, parsec.Or("expected bullet", try(punctS('-')), try(punctS('*'))))
	bullet := parsec.And(parsec.SkipAfter(markerBullet, indent), section, func(ind int, s slack.RichTextElement) *slack.RichTextList {
		return &slack.RichTextList{
			Type:     slack.RTEList,
			Elements: []slack.RichTextElement{s},
			Style:    slack.RTEListBullet,
			Indent:   ind,
		}
	})
	markerNumbered := parsec.SkipAfter(whitespace, parsec.SkipAfter(punct('.'), integer))
	numbered := parsec.And(parsec.And(indent, markerNumbered, mkSlice), section, func(indOffset []int, s slack.RichTextElement) *slack.RichTextList {
		return &slack.RichTextList{
			Type:     slack.RTEList,
			Elements: []slack.RichTextElement{s},
			Style:    slack.RTEListOrdered,
			Indent:   indOffset[0],
			Offset:   indOffset[1] - 1, // offset = 0 + first element => 1.
		}
	})
	return parsec.Or("expected list", try(bullet), try(numbered))(state, buf)
}

// Parse a blockquote, until a newline.
//
// If the blockquote continues on the next line, it will be merged in [Merge].
//
// Note that nested quotes and lists are emitted separately, by specifying the correct "Border".
func quote(state *mdState, buf buffer) (slack.RichTextElement, mdErr) {
	_, err := parsec.SkipAfter(parsec.Optional(try(whitespace), ""), punct('>'))(state, buf)
	var null slack.RichTextElement
	if err != nil {
		return null, err
	}
	elem, err := element()(state, buf)

	skipNewline := false

	switch t := elem.(type) {
	case *slack.RichTextSection:
		elem = &slack.RichTextQuote{
			Type:     slack.RTEQuote,
			Elements: t.Elements,
			Border:   0,
		}
		skipNewline = true
	case *slack.RichTextQuote:
		t.Border += 1
	case *slack.RichTextList:
		t.Border += 1
	case *slack.RichTextPreformatted:
		t.Border += 1
	}

	if skipNewline {
		_, _ = parsec.Optional(try(newline), "")(state, buf)
	}

	return elem, err
}

// Parse an optional newline
var optionalNewline = parsec.Optional(try(newline), "")

// Parse a fenced quote block.
//
// This parses everything until the closing fence.
//
// Note that, same as upstream Slack client, you can not specify the language of the code block,
// and it is not possible to escape the fence in any way (i.e. the block may not contain "```")
func fenced(state *mdState, buf buffer) (slack.RichTextElement, mdErr) {
	fence := tokenS("```")
	anything := parsec.Cast(parsec.ManyTill(0, "fenced content", tokens.Satisfy(lexer, "anything", parsec.Anything), lookAhead(fence)), toStrings)
	block := parsec.Cast(anything, func(s string) (*slack.RichTextPreformatted, error) {
		return &slack.RichTextPreformatted{
			Type:     slack.RTEPreformatted,
			Elements: []slack.RichTextSectionElement{slack.NewRichTextSectionTextElement(strings.TrimSuffix(s, "\n"), nil)},
		}, nil
	})
	return parsec.Between(parsec.Skip(fence, optionalNewline), block, fence)(state, buf)
}

// Parse a top-level mrkdwn element.
func element() parserElement {
	return parsec.Choice(
		"expected mrkdwn element",
		try(parsec.SkipAfter(optionalNewline, fenced)),
		try(parsec.SkipAfter(optionalNewline, listItem)),
		try(quote),
		try(section),
	)
}

// Parse a top-level mrkdwn element or a newline.
//
// The newline comes first because element() can succeed without consuming any input.
var elementOrNewline = parsec.Choice(
	"expected mrkdwn element or newline",
	try(newlineSection),
	element(),
)

// Parse the entire mrkdwn block.
var block = parsec.Cast(
	parsec.Some(0, "expected mrkdwn block", elementOrNewline),
	func(elems []slack.RichTextElement) (*slack.RichTextBlock, error) {
		return slack.NewRichTextBlock("", Merge(fixListIndentsOffsets(elems))...), nil
	},
)

// Parse the given string into the slack rich text block.
//
// parseAngled determines whether angled tokens (e.g. <@U12345> or <!here>) will be parsed.
func parseBlock(src string, parseAngled bool) *slack.RichTextBlock {
	buf := pstrings.Buffer([]rune(src))
	blk, _ := block(&mdState{parseAngled: parseAngled}, buf)
	return blk
}

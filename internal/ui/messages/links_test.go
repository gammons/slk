package messages

import (
	"reflect"
	"testing"

	"github.com/gammons/slk/internal/core/blocks"
	"github.com/slack-go/slack"
)

func TestExtractLinks(t *testing.T) {
	cases := []struct {
		name string
		text string
		want []Link
	}{
		{
			name: "no links",
			text: "just some text with <@U123> and <#C123|general>",
			want: nil,
		},
		{
			name: "bare link",
			text: "see <https://example.com/page>",
			want: []Link{{URL: "https://example.com/page"}},
		},
		{
			name: "labeled link",
			text: "see <https://example.com/page|the docs>",
			want: []Link{{URL: "https://example.com/page", Label: "the docs"}},
		},
		{
			name: "mixed, order of appearance",
			text: "<https://a.example/1|first> then <https://b.example/2>",
			want: []Link{
				{URL: "https://a.example/1", Label: "first"},
				{URL: "https://b.example/2"},
			},
		},
		{
			name: "duplicate URLs deduped",
			text: "<https://a.example/1> and again <https://a.example/1|same>",
			want: []Link{{URL: "https://a.example/1"}},
		},
		{
			name: "mailto",
			text: "<mailto:foo@example.com|foo@example.com>",
			want: []Link{{URL: "mailto:foo@example.com", Label: "foo@example.com"}},
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got := ExtractLinks(tc.text)
			if !reflect.DeepEqual(got, tc.want) {
				t.Errorf("ExtractLinks(%q) = %#v, want %#v", tc.text, got, tc.want)
			}
		})
	}
}

func TestMessageLinks(t *testing.T) {
	cases := []struct {
		name string
		msg  MessageItem
		want []Link
	}{
		{
			name: "plain text only",
			msg:  MessageItem{Text: "see <https://example.com/x|docs>"},
			want: []Link{{URL: "https://example.com/x", Label: "docs"}},
		},
		{
			name: "jira post: body link and title_link dedupe to body label",
			msg: MessageItem{LegacyAttachments: []blocks.LegacyAttachment{{
				Text:      ">>>@user - <https://example.atlassian.net/browse/SD-5763|SD-5763 Investigate clients>",
				Title:     "SD-5763 Investigate clients",
				TitleLink: "https://example.atlassian.net/browse/SD-5763",
			}}},
			want: []Link{{
				URL:   "https://example.atlassian.net/browse/SD-5763",
				Label: "SD-5763 Investigate clients",
			}},
		},
		{
			name: "jira post: distinct title_link and body link, both kept in order",
			msg: MessageItem{LegacyAttachments: []blocks.LegacyAttachment{{
				Text:      "see <https://example.com/spec|the spec>",
				Title:     "SD-5763",
				TitleLink: "https://example.atlassian.net/browse/SD-5763",
			}}},
			want: []Link{
				{URL: "https://example.atlassian.net/browse/SD-5763", Label: "SD-5763"},
				{URL: "https://example.com/spec", Label: "the spec"},
			},
		},
		{
			name: "pretext link picked up",
			msg: MessageItem{LegacyAttachments: []blocks.LegacyAttachment{{
				Pretext: "new deploy <https://ci.example/1|run 1>",
				Text:    "plain body text",
			}}},
			want: []Link{{URL: "https://ci.example/1", Label: "run 1"}},
		},
		{
			name: "title_link without title is an unlabeled link",
			msg: MessageItem{LegacyAttachments: []blocks.LegacyAttachment{{
				TitleLink: "https://example.atlassian.net/browse/SD-1",
			}}},
			want: []Link{{URL: "https://example.atlassian.net/browse/SD-1"}},
		},
		{
			name: "jira link-unfurl: link inside attachment's nested blocks",
			msg: MessageItem{LegacyAttachments: []blocks.LegacyAttachment{{
				Blocks: []blocks.Block{
					blocks.SectionBlock{
						Text: "*<https://example.atlassian.net/browse/SD-5763|SD-5763 Investigate clients marked as Awaiting Deletion in UK Matchbox>*",
					},
					blocks.ActionsBlock{},
				},
			}}},
			want: []Link{{
				URL:   "https://example.atlassian.net/browse/SD-5763",
				Label: "SD-5763 Investigate clients marked as Awaiting Deletion in UK Matchbox",
			}},
		},
		{
			name: "top-level blocks: section fields and context links",
			msg: MessageItem{Blocks: []blocks.Block{
				blocks.SectionBlock{Fields: []string{"Link: <https://example.com/f|field link>"}},
				blocks.ContextBlock{Elements: []blocks.ContextElement{
					{Text: "see <https://example.com/c|context>"},
				}},
			}},
			want: []Link{
				{URL: "https://example.com/f", Label: "field link"},
				{URL: "https://example.com/c", Label: "context"},
			},
		},
		{
			name: "top-level rich_text block: link element reconstructed to mrkdwn",
			msg: MessageItem{Blocks: []blocks.Block{
				blocks.RichTextBlock{Elements: []slack.RichTextElement{
					&slack.RichTextSection{Elements: []slack.RichTextSectionElement{
						&slack.RichTextSectionTextElement{Type: slack.RTSEText, Text: "issue "},
						&slack.RichTextSectionLinkElement{
							Type: slack.RTSELink, URL: "https://example.atlassian.net/browse/SD-5763",
							Text: "SD-5763",
						},
					}},
				}},
			}},
			want: []Link{{URL: "https://example.atlassian.net/browse/SD-5763", Label: "SD-5763"}},
		},
		{
			name: "attachment field value link picked up",
			msg: MessageItem{LegacyAttachments: []blocks.LegacyAttachment{{
				Fields: []blocks.LegacyField{{Title: "Link", Value: "<https://example.com/f|the field>"}},
			}}},
			want: []Link{{URL: "https://example.com/f", Label: "the field"}},
		},
		{
			name: "shared message: slack from_url labeled",
			msg: MessageItem{LegacyAttachments: []blocks.LegacyAttachment{{
				FromURL:    "https://myslack.slack.com/archives/C1/p1697000000000000",
				AuthorName: "Alice",
			}}},
			want: []Link{{URL: "https://myslack.slack.com/archives/C1/p1697000000000000", Label: "Message from Alice"}},
		},
		{
			name: "shared message without author gets generic label",
			msg: MessageItem{LegacyAttachments: []blocks.LegacyAttachment{{
				FromURL: "https://myslack.slack.com/archives/C1/p1697000000000000",
			}}},
			want: []Link{{URL: "https://myslack.slack.com/archives/C1/p1697000000000000", Label: "Shared message"}},
		},
		{
			name: "non-slack from_url skipped as unfurl",
			msg: MessageItem{LegacyAttachments: []blocks.LegacyAttachment{{
				FromURL: "https://example.com/page",
			}}},
			want: nil,
		},
		{
			name: "from_url duplicated in body is dropped",
			msg: MessageItem{
				Text: "read <https://myslack.slack.com/archives/C1/p1697000000000000|this>",
				LegacyAttachments: []blocks.LegacyAttachment{{
					FromURL: "https://myslack.slack.com/archives/C1/p1697000000000000",
				}},
			},
			want: []Link{{URL: "https://myslack.slack.com/archives/C1/p1697000000000000", Label: "this"}},
		},
		{
			name: "links across text and attachments deduped, first wins",
			msg: MessageItem{
				Text: "<https://example.com/1|in text>",
				LegacyAttachments: []blocks.LegacyAttachment{{
					Text: "<https://example.com/1|in att> and <https://example.com/2>",
				}},
			},
			want: []Link{
				{URL: "https://example.com/1", Label: "in text"},
				{URL: "https://example.com/2"},
			},
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got := MessageLinks(tc.msg)
			if !reflect.DeepEqual(got, tc.want) {
				t.Errorf("MessageLinks(%#v) = %#v, want %#v", tc.msg, got, tc.want)
			}
		})
	}
}

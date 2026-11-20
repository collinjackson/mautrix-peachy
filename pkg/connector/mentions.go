package connector

// Mentions both ways. On Peachy a mention is an @handle in the words plus who
// it names (the library writes that part from the words). On Matrix it's a
// pill plus m.mentions.

import (
	"context"
	"html"
	"strings"

	"github.com/collinjackson/peachy-go"
	"maunium.net/go/mautrix/bridgev2/networkid"
	"maunium.net/go/mautrix/event"
	"maunium.net/go/mautrix/format"
	"maunium.net/go/mautrix/id"
)

// textContent is a Peachy message's words for Matrix: each @handle it names
// becomes a pill, and m.mentions says who (and @room for @all), so Matrix
// notifies them.
func (c *PeachyClient) textContent(text string, mentions []peachy.Mention, everyone bool) *event.MessageEventContent {
	content := &event.MessageEventContent{MsgType: event.MsgText, Body: text}
	if len(mentions) == 0 && !everyone {
		return content
	}
	content.Mentions = &event.Mentions{Room: everyone}
	var b strings.Builder
	lower := strings.ToLower(text)
	from := 0
	for _, m := range mentions {
		at := strings.Index(lower[from:], "@"+m.Handle)
		if m.Handle == "" || at < 0 {
			continue
		}
		at += from
		end := at + 1 + len(m.Handle)
		mxid := c.mxidOf(m.UserID)
		content.Mentions.UserIDs = append(content.Mentions.UserIDs, mxid)
		b.WriteString(htmlText(text[from:at]))
		b.WriteString(`<a href="` + html.EscapeString(mxid.URI().MatrixToURL()) + `">` + html.EscapeString(text[at:end]) + `</a>`)
		from = end
	}
	if from > 0 {
		b.WriteString(htmlText(text[from:]))
		content.Format, content.FormattedBody = event.FormatHTML, b.String()
	}
	return content
}

func htmlText(s string) string {
	return strings.ReplaceAll(html.EscapeString(s), "\n", "<br>")
}

// mxidOf is the Matrix user a Peachy person is: the bridge's user for
// themselves, a ghost for anyone else.
func (c *PeachyClient) mxidOf(userID string) id.UserID {
	if userID == c.me {
		return c.login.UserMXID
	}
	return c.pc.Bridge.Matrix.GhostIntent(networkid.UserID(userID)).GetMXID()
}

// matrixText is a Matrix message's words for Peachy, with each pill to a
// Peachy person written as their @handle, which Peachy reads as a mention.
// A pill to someone with no handle, or not on Peachy, stays their name.
func (c *PeachyClient) matrixText(ctx context.Context, content *event.MessageEventContent) string {
	if content.Format != event.FormatHTML || !strings.Contains(content.FormattedBody, "matrix.to/#/@") {
		return content.Body
	}
	// Peachy shows words as they are, so bold and the like stay plain
	// rather than becoming Markdown; the body has them that way too.
	plain := func(s string, _ format.Context) string { return s }
	parser := &format.HTMLParser{
		TabsToSpaces:           4,
		Newline:                "\n",
		HorizontalLine:         "\n---\n",
		BoldConverter:          plain,
		ItalicConverter:        plain,
		StrikethroughConverter: plain,
		UnderlineConverter:     plain,
		MonospaceConverter:     plain,
		PillConverter: func(displayname, mxid, _ string, _ format.Context) string {
			if h := c.handleOf(ctx, id.UserID(mxid)); h != "" {
				return "@" + h
			}
			return displayname
		},
	}
	return parser.Parse(content.FormattedBody, format.NewContext(ctx))
}

func (c *PeachyClient) handleOf(ctx context.Context, mxid id.UserID) string {
	var person string
	if mxid == c.login.UserMXID {
		person = c.me
	} else if ghost, ok := c.pc.Bridge.Matrix.ParseGhostMXID(mxid); ok {
		person = string(ghost)
	} else {
		return ""
	}
	people, err := c.device.People(ctx, []string{person})
	if err != nil || len(people) == 0 {
		return ""
	}
	return people[0].Handle
}

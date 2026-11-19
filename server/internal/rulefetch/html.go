package rulefetch

import (
	"html"
	"strings"

	nethtml "golang.org/x/net/html"
)

// pyUnescape is kept for the archived replay dispatcher and website reader.
// HTML entity decoding is owned by the standard library.
func pyUnescape(value string) string { return html.UnescapeString(value) }

// metaContent reads the final meta element's content attribute in source order.
func metaContent(value string) string {
	tokenizer := nethtml.NewTokenizer(strings.NewReader(value))
	content := ""
	for {
		switch tokenizer.Next() {
		case nethtml.ErrorToken:
			return CleanText(content, 200)
		case nethtml.StartTagToken, nethtml.SelfClosingTagToken:
			token := tokenizer.Token()
			if token.Data != "meta" {
				continue
			}
			content = ""
			for _, attr := range token.Attr {
				if attr.Key == "content" {
					content = attr.Val
				}
			}
		}
	}
}

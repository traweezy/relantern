package parsing

import (
	"fmt"
	"net/url"
	"strconv"
	"strings"
	"unicode"

	"golang.org/x/net/html"
	"golang.org/x/net/html/atom"
)

var forbiddenElements = map[string]struct{}{
	"applet": {}, "audio": {}, "button": {}, "canvas": {}, "embed": {},
	"form": {}, "iframe": {}, "input": {}, "math": {}, "noscript": {},
	"object": {}, "script": {}, "select": {}, "style": {}, "svg": {},
	"template": {}, "textarea": {}, "video": {},
}

func cleanUntrustedFragment(value string, anchor string) (string, []Warning) {
	blocks, warnings := extractUntrustedFragmentBlocks(value, anchor)
	parts := make([]string, 0, len(blocks))
	for _, block := range blocks {
		if text := normalizeText(block.Text, block.Kind == "code"); text != "" {
			parts = append(parts, text)
		}
	}
	return normalizeText(strings.Join(parts, "\n\n"), false), warnings
}

func extractUntrustedFragmentBlocks(value string, anchor string) ([]extractedBlock, []Warning) {
	if strings.TrimSpace(value) == "" {
		return nil, nil
	}
	contextNode := &html.Node{Type: html.ElementNode, DataAtom: atom.Div, Data: "div"}
	nodes, err := html.ParseFragment(strings.NewReader(value), contextNode)
	if err != nil {
		return []extractedBlock{{Kind: "paragraph", Anchor: anchor, Text: textOnlyFallback(value)}}, []Warning{{
			Code:   "fragment_parse_failed",
			Detail: "Embedded markup could not be parsed and was reduced to text.",
			Anchor: anchor,
		}}
	}
	root := &html.Node{Type: html.ElementNode, Data: "div"}
	for _, node := range nodes {
		root.AppendChild(node)
	}
	sanitizeTree(root)
	return extractHTMLBlocks(root, anchor), nil
}

func textOnlyFallback(value string) string {
	parsed, err := html.Parse(strings.NewReader(value))
	if err != nil {
		return ""
	}
	sanitizeTree(parsed)
	return textContent(parsed)
}

func sanitizeTree(node *html.Node) {
	for child := node.FirstChild; child != nil; {
		next := child.NextSibling
		remove := child.Type == html.CommentNode
		if child.Type == html.ElementNode {
			_, forbidden := forbiddenElements[strings.ToLower(child.Data)]
			remove = remove || forbidden || hiddenElement(child)
			if !remove {
				child.Attr = safeAttributes(child)
			}
		}
		if remove {
			node.RemoveChild(child)
		} else {
			sanitizeTree(child)
		}
		child = next
	}
}

func hiddenElement(node *html.Node) bool {
	for _, attribute := range node.Attr {
		key := strings.ToLower(attribute.Key)
		value := strings.ToLower(strings.TrimSpace(attribute.Val))
		switch key {
		case "hidden":
			return true
		case "aria-hidden":
			if value == "true" {
				return true
			}
		case "style":
			compact := strings.ReplaceAll(value, " ", "")
			if strings.Contains(compact, "display:none") || strings.Contains(compact, "visibility:hidden") {
				return true
			}
		}
	}
	return false
}

func safeAttributes(node *html.Node) []html.Attribute {
	attributes := make([]html.Attribute, 0, 2)
	for _, attribute := range node.Attr {
		key := strings.ToLower(attribute.Key)
		if strings.HasPrefix(key, "on") {
			continue
		}
		switch key {
		case "id":
			if value := safeAnchor(attribute.Val); value != "" {
				attributes = append(attributes, html.Attribute{Key: "id", Val: value})
			}
		case "href":
			value := strings.TrimSpace(attribute.Val)
			lower := strings.ToLower(value)
			if !strings.HasPrefix(lower, "javascript:") && !strings.HasPrefix(lower, "data:") {
				attributes = append(attributes, html.Attribute{Key: "href", Val: value})
			}
		}
	}
	return attributes
}

func extractHTMLBlocks(root *html.Node, prefix string) []extractedBlock {
	blocks := make([]extractedBlock, 0, 16)
	counts := make(map[string]int)
	var walk func(*html.Node)
	walk = func(node *html.Node) {
		if node.Type == html.ElementNode {
			kind, level, block := blockKind(node.Data)
			if block {
				text := textContent(node)
				if normalizeText(text, kind == "code") != "" {
					counts[kind]++
					anchor := nodeAttribute(node, "id")
					if anchor != "" {
						anchor = "#" + anchor
					} else {
						anchor = fmt.Sprintf("%s/%s/%d", strings.TrimSuffix(prefix, "/"), kind, counts[kind])
					}
					blocks = append(blocks, extractedBlock{Kind: kind, Level: level, Anchor: anchor, Text: text})
				}
				return
			}
		}
		for child := node.FirstChild; child != nil; child = child.NextSibling {
			walk(child)
		}
	}
	walk(root)
	if len(blocks) == 0 {
		if text := normalizeText(textContent(root), false); text != "" {
			blocks = append(blocks, extractedBlock{Kind: "paragraph", Anchor: strings.TrimSuffix(prefix, "/") + "/text/1", Text: text})
		}
	}
	return blocks
}

func blockKind(tag string) (string, int, bool) {
	switch strings.ToLower(tag) {
	case "h1", "h2", "h3", "h4", "h5", "h6":
		level, _ := strconv.Atoi(tag[1:])
		return "heading", level, true
	case "p":
		return "paragraph", 0, true
	case "li":
		return "list_item", 0, true
	case "pre":
		return "code", 0, true
	case "blockquote":
		return "quote", 0, true
	case "tr":
		return "table_row", 0, true
	default:
		return "", 0, false
	}
}

func textContent(node *html.Node) string {
	var builder strings.Builder
	var walk func(*html.Node)
	walk = func(current *html.Node) {
		if current.Type == html.TextNode {
			builder.WriteString(current.Data)
			builder.WriteByte(' ')
		}
		for child := current.FirstChild; child != nil; child = child.NextSibling {
			walk(child)
		}
	}
	walk(node)
	return builder.String()
}

func nodeAttribute(node *html.Node, name string) string {
	for _, attribute := range node.Attr {
		if strings.EqualFold(attribute.Key, name) {
			return attribute.Val
		}
	}
	return ""
}

func safeAnchor(value string) string {
	value = strings.TrimSpace(value)
	if value == "" || len(value) > 200 {
		return ""
	}
	for _, character := range value {
		if !unicode.IsLetter(character) && !unicode.IsDigit(character) && !strings.ContainsRune("-_.:", character) {
			return ""
		}
	}
	return value
}

func sameHostURL(baseURL *url.URL, candidate string) string {
	parsed, err := baseURL.Parse(strings.TrimSpace(candidate))
	if err != nil || parsed.User != nil || (parsed.Scheme != "http" && parsed.Scheme != "https") {
		return ""
	}
	if !strings.EqualFold(parsed.Scheme, baseURL.Scheme) || !strings.EqualFold(parsed.Hostname(), baseURL.Hostname()) || effectivePort(parsed) != effectivePort(baseURL) {
		return ""
	}
	parsed.Fragment = ""
	return parsed.String()
}

func effectivePort(value *url.URL) string {
	if port := value.Port(); port != "" {
		return port
	}
	if strings.EqualFold(value.Scheme, "https") {
		return "443"
	}
	return "80"
}

package app

import (
	"fmt"
	"html"
	"html/template"
	"regexp"
	"strings"
)

type markdownRenderResult struct {
	HTML template.HTML
	TOC  []markdownTocItem
}

type markdownTocItem struct {
	ID    string
	Text  string
	Class string
}

var markdownBoldPattern = regexp.MustCompile(`\*\*([^*]+)\*\*`)

func renderMarkdownDocument(source string) markdownRenderResult {
	lines := strings.Split(source, "\n")
	var out strings.Builder
	var toc []markdownTocItem
	var inCodeBlock bool
	var codeLang string
	var code strings.Builder
	var inList bool
	headingIndex := 0

	closeList := func() {
		if inList {
			out.WriteString("</ul>\n")
			inList = false
		}
	}

	closeCodeBlock := func() {
		out.WriteString(`<pre><code`)
		if codeLang != "" {
			out.WriteString(` data-lang="`)
			out.WriteString(html.EscapeString(codeLang))
			out.WriteString(`"`)
		}
		out.WriteString(`>`)
		out.WriteString(html.EscapeString(strings.TrimRight(code.String(), "\n")))
		out.WriteString("</code></pre>\n")
		code.Reset()
		codeLang = ""
		inCodeBlock = false
	}

	for _, line := range lines {
		trimmed := strings.TrimSpace(line)

		if strings.HasPrefix(trimmed, "```") {
			if inCodeBlock {
				closeCodeBlock()
				continue
			}
			closeList()
			inCodeBlock = true
			codeLang = strings.TrimSpace(strings.TrimPrefix(trimmed, "```"))
			continue
		}

		if inCodeBlock {
			code.WriteString(line)
			code.WriteByte('\n')
			continue
		}

		if trimmed == "" {
			closeList()
			continue
		}

		if level, text, ok := parseMarkdownHeading(trimmed); ok {
			closeList()
			headingIndex++
			id := fmt.Sprintf("section-%d", headingIndex)
			plain := stripMarkdownInline(text)
			toc = append(toc, markdownTocItem{
				ID:    id,
				Text:  plain,
				Class: markdownTocClass(level),
			})
			out.WriteString(fmt.Sprintf(`<h%d id="%s">`, level, id))
			out.WriteString(renderMarkdownInline(text))
			out.WriteString(fmt.Sprintf("</h%d>\n", level))
			continue
		}

		if item, ok := parseMarkdownListItem(trimmed); ok {
			if !inList {
				out.WriteString("<ul>\n")
				inList = true
			}
			out.WriteString("<li>")
			out.WriteString(renderMarkdownInline(item))
			out.WriteString("</li>\n")
			continue
		}

		closeList()
		out.WriteString("<p>")
		out.WriteString(renderMarkdownInline(trimmed))
		out.WriteString("</p>\n")
	}

	closeList()
	if inCodeBlock {
		closeCodeBlock()
	}

	return markdownRenderResult{
		HTML: template.HTML(out.String()),
		TOC:  toc,
	}
}

func parseMarkdownHeading(line string) (int, string, bool) {
	count := 0
	for count < len(line) && line[count] == '#' {
		count++
	}
	if count == 0 || count > 6 || len(line) <= count || line[count] != ' ' {
		return 0, "", false
	}
	return count, strings.TrimSpace(line[count+1:]), true
}

func parseMarkdownListItem(line string) (string, bool) {
	if strings.HasPrefix(line, "* ") || strings.HasPrefix(line, "- ") {
		return strings.TrimSpace(line[2:]), true
	}
	return "", false
}

func markdownTocClass(level int) string {
	switch {
	case level <= 1:
		return "config-doc-toc-level-1"
	case level == 2:
		return "config-doc-toc-level-2"
	default:
		return "config-doc-toc-level-3"
	}
}

func stripMarkdownInline(text string) string {
	text = strings.ReplaceAll(text, "`", "")
	text = strings.ReplaceAll(text, "**", "")
	return strings.TrimSpace(text)
}

func renderMarkdownInline(text string) string {
	parts := strings.Split(text, "`")
	var out strings.Builder
	for i, part := range parts {
		if i%2 == 1 {
			out.WriteString("<code>")
			out.WriteString(html.EscapeString(part))
			out.WriteString("</code>")
			continue
		}
		out.WriteString(renderMarkdownBold(html.EscapeString(part)))
	}
	return out.String()
}

func renderMarkdownBold(escaped string) string {
	return markdownBoldPattern.ReplaceAllString(escaped, "<strong>$1</strong>")
}

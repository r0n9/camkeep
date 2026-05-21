package app

import (
	"strings"
	"testing"
)

func TestRenderMarkdownDocumentBuildsTOCAndContent(t *testing.T) {
	source := "# Title\n\n" +
		"## Section\n\n" +
		"Plain **bold** and `code`.\n\n" +
		"* one\n" +
		"* two\n\n" +
		"```yaml\n" +
		"stream_url: \"rtsp://example/live\"\n" +
		"```\n"
	doc := renderMarkdownDocument(source)

	if len(doc.TOC) != 2 {
		t.Fatalf("expected two toc items, got %d", len(doc.TOC))
	}
	if doc.TOC[0].Text != "Title" || doc.TOC[1].Text != "Section" {
		t.Fatalf("unexpected toc: %+v", doc.TOC)
	}

	html := string(doc.HTML)
	for _, want := range []string{
		`<h1 id="section-1">Title</h1>`,
		`<h2 id="section-2">Section</h2>`,
		`<strong>bold</strong>`,
		`<code>code</code>`,
		`<li>one</li>`,
		`<pre><code data-lang="yaml">stream_url: &#34;rtsp://example/live&#34;</code></pre>`,
	} {
		if !strings.Contains(html, want) {
			t.Fatalf("expected rendered html to contain %q, got %s", want, html)
		}
	}
}

func TestRenderMarkdownDocumentEscapesHTML(t *testing.T) {
	doc := renderMarkdownDocument("## <script>alert(1)</script>\n\n`<unsafe>`")
	html := string(doc.HTML)
	if strings.Contains(html, "<script>") || strings.Contains(html, "<unsafe>") {
		t.Fatalf("expected html to be escaped, got %s", html)
	}
	if !strings.Contains(html, "&lt;script&gt;alert(1)&lt;/script&gt;") {
		t.Fatalf("expected escaped heading content, got %s", html)
	}
}

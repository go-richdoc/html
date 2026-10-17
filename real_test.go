package html

import (
	"os"
	"strings"
	"testing"

	"github.com/go-richdoc/richdoc"
)

// ⛔ A REAL DOCUMENT, before publishing anything. go-pdfkit/extract's table
// detector passed every fixture and then reported 588 tables on one real PDF;
// fixtures say what their author thought of, and a real page says what the web
// actually sends. This one is go-richdoc's own landing page as Hugo minified
// and published it: 30 898 bytes, with an inline stylesheet, inline scripts,
// SVG icons, minified attributes with no quotes, and emoji.
func TestARealPublishedPage(t *testing.T) {
	src, err := os.ReadFile("testdata/real-landing.html")
	if err != nil {
		t.Fatal(err)
	}
	doc, err := Parse(src, Limits{})
	if err != nil {
		t.Fatalf("Parse: %v", err)
	}

	// 1. Nothing that is not content came through. These are strings that
	// appear in the page's <style> and <script> and nowhere in its prose.
	all := richdoc.PlainText(doc)
	for _, forbidden := range []string{
		"prefers-color-scheme", "localStorage", "data-theme", "--brand",
		"box-shadow", "addEventListener", "querySelector",
	} {
		if strings.Contains(all, forbidden) {
			t.Errorf("the document carries %q, which comes from a stylesheet or a script", forbidden)
		}
	}

	// 2. The structure a reader sees is there. The page has one h1 and several
	// h2 and h3; the counts are READ rather than asserted exactly, because the
	// page is maintained elsewhere and an exact count would break on a word.
	var h1, h2plus, paras, links, images int
	for _, b := range doc.Blocks {
		switch v := b.(type) {
		case richdoc.Heading:
			if v.Level == 1 {
				h1++
			} else {
				h2plus++
			}
		case richdoc.Paragraph:
			paras++
		}
	}
	richdoc.Walk(doc, visitor(func(n any) {
		switch n.(type) {
		case richdoc.Link:
			links++
		case richdoc.Image:
			images++
		}
	}))
	if h1 != 1 {
		t.Errorf("h1 count = %d, want exactly one", h1)
	}
	if h2plus < 5 {
		t.Errorf("headings below h1 = %d, want several", h2plus)
	}
	if paras < 10 {
		t.Errorf("top-level paragraphs = %d, want the page's prose", paras)
	}
	if links < 20 {
		t.Errorf("links = %d, want the page's many links", links)
	}
	if images < 1 {
		t.Errorf("images = %d, want the logo", images)
	}

	// 3. The prose is actually there, and reads as prose rather than as
	// runtogetherwords.
	for _, want := range []string{
		"One document model",
		"formats that meet in it",
		"pairwise converters not written",
	} {
		if !strings.Contains(all, want) {
			t.Errorf("the document does not say %q", want)
		}
	}
	if strings.Contains(all, "modelandthe") {
		t.Error("words ran together: the whitespace between inline elements was swallowed")
	}

	// 4. The SVG icons survived as raw content rather than as stray letters.
	var raws int
	richdoc.Walk(doc, visitor(func(n any) {
		if r, ok := n.(richdoc.RawInline); ok && r.Format == "svg" {
			raws++
		}
	}))
	if raws < 1 {
		t.Errorf("inline svg kept = %d, want the button icons", raws)
	}
}

// visitor adapts a function to richdoc.Visitor: it sees every node and never
// prunes, which is what counting needs.
type visitor func(any)

func (v visitor) Enter(n any) bool { v(n); return true }
func (v visitor) Leave(any)        {}

// ⛔ Pathological inputs, from the shapes a hostile or merely broken document
// takes. None of these may panic, hang or allocate without bound; what they
// may do is come back refused.
func TestPathologicalDocuments(t *testing.T) {
	for _, tc := range []struct {
		name string
		src  string
	}{
		{"nothing at all", ""},
		{"only a doctype", "<!doctype html>"},
		{"unclosed tags all the way down", strings.Repeat("<div><p><b>x", 50)},
		{"tags closed in the wrong order", "<b><i>x</b></i>"},
		{"a table inside a table inside a cell", strings.Repeat("<table><tr><td>", 40) + "x"},
		{"a list nested forty deep", strings.Repeat("<ul><li>", 40) + "x"},
		{"an attribute with no value and no end", `<p class=`},
		{"a comment that never closes", "<p>a</p><!-- forever"},
		{"a script that never closes", "<p>a</p><script>var x = '</p>"},
		{"a cell with a thousand columns declared", `<table><tr><td colspan="99999">x</td></tr></table>`},
		{"text that is only whitespace", strings.Repeat(" \t\n", 1000)},
		{"a null byte and a lone surrogate", "<p>a\x00b�c</p>"},
		{"an entity that does not exist", "<p>&notanentity; &amp; &#x110000;</p>"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			doc, err := Parse([]byte(tc.src), Limits{})
			if err != nil {
				// A refusal is a correct answer; it must say which limit.
				if !strings.Contains(err.Error(), "html:") {
					t.Errorf("error = %v, want it to name this package", err)
				}
				return
			}
			// And whatever came back must be a document, not a panic waiting
			// to happen in a writer: walking it must work.
			richdoc.Walk(doc, visitor(func(any) {}))
		})
	}
}

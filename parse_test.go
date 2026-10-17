package html

import (
	"fmt"
	"strings"
	"testing"

	"github.com/go-richdoc/richdoc"
)

// shape is a one-line-per-block rendering, for tests that are about STRUCTURE.
//
// ⛔ It deliberately prints a header row and an ordinary row differently.
// go-richdoc/fb2's test helper printed them the same way, and two mutations
// survived because of it: a test that cannot see the difference cannot defend
// it. Every test about which row is the header asserts Header directly as
// well.
func shape(blocks []richdoc.Block, indent string) string {
	var b strings.Builder
	for _, blk := range blocks {
		switch v := blk.(type) {
		case richdoc.Heading:
			fmt.Fprintf(&b, "%sh%d%s %s\n", indent, v.Level, idOf(v.ID), text(v.Inlines))
		case richdoc.Paragraph:
			fmt.Fprintf(&b, "%sp %s\n", indent, text(v.Inlines))
		case richdoc.CodeBlock:
			fmt.Fprintf(&b, "%scode[%s] %q\n", indent, v.Language, v.Text)
		case richdoc.BlockQuote:
			fmt.Fprintf(&b, "%squote\n", indent)
			b.WriteString(shape(v.Blocks, indent+"  "))
		case richdoc.List:
			fmt.Fprintf(&b, "%slist ordered=%v start=%d tight=%v\n", indent, v.Ordered, v.Start, v.Tight)
			for _, it := range v.Items {
				fmt.Fprintf(&b, "%s  item\n", indent)
				b.WriteString(shape(it.Blocks, indent+"    "))
			}
		case richdoc.Table:
			fmt.Fprintf(&b, "%stable caption=%q\n", indent, text(v.Caption))
			if len(v.Header) > 0 {
				fmt.Fprintf(&b, "%s  header %s\n", indent, cells(v.Header))
			}
			for _, r := range v.Rows {
				fmt.Fprintf(&b, "%s  row %s\n", indent, cells(r))
			}
		case richdoc.ThematicBreak:
			fmt.Fprintf(&b, "%shr\n", indent)
		case richdoc.RawBlock:
			fmt.Fprintf(&b, "%sraw[%s] %s\n", indent, v.Format, v.Text)
		default:
			fmt.Fprintf(&b, "%s?%T\n", indent, blk)
		}
	}
	return b.String()
}

func idOf(id string) string {
	if id == "" {
		return ""
	}
	return "#" + id
}

func cells(cs []richdoc.Cell) string {
	var out []string
	for _, c := range cs {
		s := text(c.Inlines)
		if c.ColSpan > 0 {
			s += fmt.Sprintf("+%dcol", c.ColSpan)
		}
		if c.RowSpan > 0 {
			s += fmt.Sprintf("+%drow", c.RowSpan)
		}
		if len(c.Blocks) > 0 {
			s += fmt.Sprintf("{%d blocks}", len(c.Blocks))
		}
		out = append(out, "["+s+"]")
	}
	return strings.Join(out, " ")
}

// text renders inline content with its markings visible, so a test can tell
// emphasis from plain words.
func text(in []richdoc.Inline) string {
	var b strings.Builder
	for _, i := range in {
		switch v := i.(type) {
		case richdoc.Text:
			b.WriteString(v.Value)
		case richdoc.Emph:
			fmt.Fprintf(&b, "<em>%s</em>", text(v.Inlines))
		case richdoc.Strong:
			fmt.Fprintf(&b, "<strong>%s</strong>", text(v.Inlines))
		case richdoc.Strikethrough:
			fmt.Fprintf(&b, "<del>%s</del>", text(v.Inlines))
		case richdoc.Code:
			fmt.Fprintf(&b, "`%s`", v.Value)
		case richdoc.Link:
			fmt.Fprintf(&b, "[%s](%s)", text(v.Inlines), v.URL)
		case richdoc.Anchor:
			fmt.Fprintf(&b, "{#%s}%s", v.ID, text(v.Inlines))
		case richdoc.Image:
			fmt.Fprintf(&b, "![%s](%s)", v.Alt, v.URL)
		case richdoc.LineBreak:
			b.WriteString("\\n")
		case richdoc.RawInline:
			fmt.Fprintf(&b, "raw[%s]%s", v.Format, v.Text)
		default:
			fmt.Fprintf(&b, "?%T", i)
		}
	}
	return b.String()
}

func parse(t *testing.T, src string) *richdoc.Document {
	t.Helper()
	doc, err := Parse([]byte(src), Limits{})
	if err != nil {
		t.Fatalf("Parse: %v", err)
	}
	return doc
}

func TestBlocks(t *testing.T) {
	for _, tc := range []struct{ name, src, want string }{
		{
			name: "headings keep their level and their anchor",
			src:  `<h1 id="top">One</h1><h2>Two</h2><h3>Three</h3><h4>Four</h4><h5>Five</h5><h6>Six</h6>`,
			want: "h1#top One\nh2 Two\nh3 Three\nh4 Four\nh5 Five\nh6 Six\n",
		},
		{
			name: "a comment is not content, between blocks or inside them",
			src:  `<p>a</p><!-- hidden --><p>b<!-- also hidden -->c</p>`,
			want: "p a\np bc\n",
		},
		{
			// ⛔ h7 does not exist; the parser makes it an unknown element, so
			// its text survives as a paragraph rather than as a heading.
			name: "an element that is not a heading is not one",
			src:  `<h7>Seven</h7>`,
			want: "p Seven\n",
		},
		{
			name: "a div is not a paragraph, and its words are not lost",
			src:  `<div>bare words<p>a real one</p>more bare</div>`,
			want: "p bare words\np a real one\np more bare\n",
		},
		{
			name: "an empty paragraph and an empty heading say nothing",
			src:  `<p>  </p><h2>  </h2><p>text</p>`,
			want: "p text\n",
		},
		{
			name: "a thematic break",
			src:  `<p>a</p><hr><p>b</p>`,
			want: "p a\nhr\np b\n",
		},
		{
			name: "a block quotation nests",
			src:  `<blockquote><p>quoted</p><blockquote><p>deeper</p></blockquote></blockquote>`,
			want: "quote\n  p quoted\n  quote\n    p deeper\n",
		},
		{
			name: "an empty quotation is dropped",
			src:  `<blockquote>   </blockquote><p>a</p>`,
			want: "p a\n",
		},
		{
			name: "a list, tight",
			src:  `<ul><li>one</li><li>two</li></ul>`,
			want: "list ordered=false start=0 tight=true\n  item\n    p one\n  item\n    p two\n",
		},
		{
			name: "a list whose items wrap their text in p is loose",
			src:  `<ul><li><p>one</p></li></ul>`,
			want: "list ordered=false start=0 tight=false\n  item\n    p one\n",
		},
		{
			name: "an ordered list keeps its start",
			src:  `<ol start="7"><li>seven</li></ol>`,
			want: "list ordered=true start=7 tight=true\n  item\n    p seven\n",
		},
		{
			name: "a start that is not a number is ignored",
			src:  `<ol start="seven"><li>x</li></ol>`,
			want: "list ordered=true start=0 tight=true\n  item\n    p x\n",
		},
		{
			name: "an empty item is still an item",
			src:  `<ul><li>one</li><li></li><li>three</li></ul>`,
			want: "list ordered=false start=0 tight=true\n  item\n    p one\n  item\n  item\n    p three\n",
		},
		{
			name: "a list with no items at all is nothing",
			src:  `<ul>   </ul><p>after</p>`,
			want: "p after\n",
		},
		{
			name: "a nested list is a block inside its item",
			src:  `<ul><li>one<ul><li>deeper</li></ul></li></ul>`,
			want: "list ordered=false start=0 tight=true\n  item\n    p one\n    list ordered=false start=0 tight=true\n      item\n        p deeper\n",
		},
		{
			name: "a menu is a list",
			src:  `<menu><li>one</li></menu>`,
			want: "list ordered=false start=0 tight=true\n  item\n    p one\n",
		},
		{
			name: "pre keeps its spaces and drops the newline after the tag",
			src:  "<pre>\nif x:\n    y   z\n</pre>",
			want: "code[] \"if x:\\n    y   z\\n\"\n",
		},
		{
			name: "pre reads its language from a highlighter class",
			src:  `<pre class="language-go">x := 1</pre>`,
			want: "code[go] \"x := 1\"\n",
		},
		{
			name: "pre reads it from the code inside, too",
			src:  `<pre><code class="lang-rust">let x = 1;</code></pre>`,
			want: "code[rust] \"let x = 1;\"\n",
		},
		{
			name: "a class that is not a language is not one",
			src:  `<pre class="chroma"><code class="highlight-sh">ls</code></pre>`,
			want: "code[sh] \"ls\"\n",
		},
		{
			name: "an empty pre is dropped",
			src:  "<pre>\n   \n</pre><p>a</p>",
			want: "p a\n",
		},
		{
			// Written the way a person writes it: a newline between the term
			// and its definition, which is a text node the walk has to skip.
			name: "a definition list keeps both halves",
			src: `<dl>
			  <dt>Term</dt>
			  <dd><p>What it means</p></dd>
			</dl>`,
			want: "p <strong>Term</strong>\nquote\n  p What it means\n",
		},
		{
			name: "an empty term and an empty definition are dropped",
			src:  `<dl><dt> </dt><dd> </dd><dt>t</dt></dl>`,
			want: "p <strong>t</strong>\n",
		},
		{
			name: "svg is kept verbatim, with its format named",
			// The expected value is what the RENDERER writes: x/net/html
			// serialises a void-looking foreign element as a pair of tags, and
			// the fixture says so rather than saying what I first assumed.
			src:  `<p>before</p><svg viewBox="0 0 1 1"><rect/></svg>`,
			want: "p before\nraw[svg] <svg viewBox=\"0 0 1 1\"><rect></rect></svg>\n",
		},
		{
			name: "a figure is transparent and its caption is a paragraph",
			src:  `<figure><img src="a.png" alt="A"><figcaption>The caption</figcaption></figure>`,
			want: "p ![A](a.png)\np The caption\n",
		},
		{
			name: "details and summary are transparent",
			src:  `<details><summary>More</summary><p>hidden</p></details>`,
			want: "p More\np hidden\n",
		},
		{
			name: "an empty details is dropped",
			src:  `<details>  </details><p>a</p>`,
			want: "p a\n",
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			got := shape(parse(t, tc.src).Blocks, "")
			if got != tc.want {
				t.Errorf("shape =\n%s\nwant\n%s", got, tc.want)
			}
		})
	}
}

func TestInlines(t *testing.T) {
	for _, tc := range []struct{ name, src, want string }{
		{"emphasis both spellings", `<p><em>a</em> <i>b</i></p>`, "p <em>a</em> <em>b</em>\n"},
		{"strong both spellings", `<p><strong>a</strong> <b>b</b></p>`, "p <strong>a</strong> <strong>b</strong>\n"},
		{"strikethrough three spellings", `<p><del>a</del><s>b</s><strike>c</strike></p>`, "p <del>a</del><del>b</del><del>c</del>\n"},
		{"code four spellings", "<p><code>a</code><kbd>b</kbd><samp>c</samp><tt>d</tt></p>", "p `a``b``c``d`\n"},
		{"code flattens the markup inside it", `<p><code><b>x</b>()</code></p>`, "p `x()`\n"},
		{"an empty code element is dropped", `<p>a<code> </code></p>`, "p a\n"},
		{"an empty marking is dropped", `<p>a<strong></strong>b</p>`, "p ab\n"},
		{"a link", `<p><a href="/x" title="T">text</a></p>`, "p [text](/x)\n"},
		{"a link with no text shows its URL", `<p><a href="/x"></a></p>`, "p [/x](/x)\n"},
		{"an anchor from id", `<p><a id="sec1">Section</a></p>`, "p {#sec1}Section\n"},
		{"an anchor from name, as HTML 4 wrote it", `<p><a name="sec2"></a>text</p>`, "p {#sec2}text\n"},
		{"an a with neither href nor id passes through", `<p><a>plain</a></p>`, "p plain\n"},
		{"an image", `<p><img src="a.png" alt="A" title="T"></p>`, "p ![A](a.png)\n"},
		{"an image with no src is not a picture", `<p>a<img alt="A"></p>`, "p a\n"},
		{"a line break", `<p>a<br>b</p>`, "p a\\nb\n"},
		{"a word-break opportunity is not a line break", `<p>a<wbr>b</p>`, "p ab\n"},
		{"a span passes its children through", `<p><span class="x">kept</span></p>`, "p kept\n"},
		{"an unknown element passes its children through", `<p><custom-tag>kept</custom-tag></p>`, "p kept\n"},
		{"a script inside a paragraph is not text", `<p>a<script>evil()</script>b</p>`, "p ab\n"},
		{"a style inside a paragraph is not text either", `<p>a<style>p{}</style>b</p>`, "p ab\n"},
		{"sub and sup keep their text", `<p>H<sub>2</sub>O and x<sup>2</sup></p>`, "p H2O and x2\n"},
		{"math is kept verbatim inline", `<p>a <math><mi>x</mi></math></p>`, "p a raw[math]<math><mi>x</mi></math>\n"},
		{"nested markings nest", `<p><strong>bold <em>and</em></strong></p>`, "p <strong>bold <em>and</em></strong>\n"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			got := shape(parse(t, tc.src).Blocks, "")
			if got != tc.want {
				t.Errorf("shape = %q, want %q", got, tc.want)
			}
		})
	}
}

// ⛔ THE DEFECT THIS READER EXISTS NOT TO HAVE. A stylesheet and a script are
// text nodes like any other, and the commonest failure of an HTML-to-text
// converter is a first paragraph full of CSS declarations.
func TestAStylesheetIsNotTheFirstParagraph(t *testing.T) {
	src := `<!doctype html><html><head>
	  <title>A page</title>
	  <style>body { color: red; font-family: "Times"; }</style>
	  <script>var secret = "do not print me";</script>
	</head><body>
	  <noscript>Please enable JavaScript.</noscript>
	  <template><p>a template is not content</p></template>
	  <p>The only paragraph.</p>
	  <script>more();</script>
	</body></html>`
	doc := parse(t, src)
	if got, want := shape(doc.Blocks, ""), "p The only paragraph.\n"; got != want {
		t.Errorf("shape = %q, want %q", got, want)
	}
	all := richdoc.PlainText(doc)
	for _, forbidden := range []string{"color", "Times", "secret", "JavaScript", "template", "more()"} {
		if strings.Contains(all, forbidden) {
			t.Errorf("the document carries %q, which is not content:\n%s", forbidden, all)
		}
	}
}

// ⛔ The whitespace test that matters: the space between two inline elements
// lives in a text node of its own, and a reader that trims each node runs the
// words together.
func TestWhitespaceIsCollapsedAndNotSwallowed(t *testing.T) {
	for _, tc := range []struct{ name, src, want string }{
		{"a space between markings survives", "<p><b>one</b> <i>two</i></p>", "p <strong>one</strong> <em>two</em>\n"},
		{"a newline between words is a space", "<p>one\ntwo</p>", "p one two\n"},
		{"runs of whitespace collapse", "<p>one   \t\n  two</p>", "p one two\n"},
		{"a block's edges are trimmed", "<p>   one   </p>", "p one\n"},
		{"leading space before a marking is dropped", "<p>  <b>one</b></p>", "p <strong>one</strong>\n"},
		{"trailing space after a marking is dropped", "<p><b>one</b>  </p>", "p <strong>one</strong>\n"},
		{"a non-breaking space is a character, not whitespace", "<p>10\u00a0km</p>", "p 10 km\n"},
		{"whitespace-only text between blocks is nothing", "<div><p>a</p>\n  \n<p>b</p></div>", "p a\np b\n"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			if got := shape(parse(t, tc.src).Blocks, ""); got != tc.want {
				t.Errorf("shape = %q, want %q", got, tc.want)
			}
		})
	}
}

func TestMeta(t *testing.T) {
	doc := parse(t, `<!doctype html><html lang="fr"><head>
	  <title> A Title </title>
	  <meta name="AUTHOR" content="Ada Lovelace">
	  <meta name="description" content="what it is about">
	  <meta name="DCTERMS.date" content="2026-10-17">
	  <meta charset="utf-8">
	</head><body><p>x</p></body></html>`)
	want := map[string]string{
		"language":    "fr",
		"title":       "A Title",
		"author":      "Ada Lovelace",
		"description": "what it is about",
		"date":        "2026-10-17",
	}
	if len(doc.Meta) != len(want) {
		t.Fatalf("Meta = %v, want %v", doc.Meta, want)
	}
	for k, v := range want {
		if doc.Meta[k] != v {
			t.Errorf("Meta[%q] = %q, want %q", k, doc.Meta[k], v)
		}
	}
}

func TestADocumentThatSaysNothingAboutItselfHasNoMetadata(t *testing.T) {
	doc := parse(t, `<p>x</p>`)
	if doc.Meta != nil {
		t.Errorf("Meta = %v, want nil: an empty map makes a caller look twice", doc.Meta)
	}
	// An empty title and an empty author are not metadata either.
	doc = parse(t, `<html><head><title>  </title><meta name="author" content=" "></head><body><p>x</p></body></html>`)
	if doc.Meta != nil {
		t.Errorf("Meta = %v, want nil", doc.Meta)
	}
}

func TestAFragmentIsAsWelcomeAsAPage(t *testing.T) {
	// The body of an HTML email, and an EPUB chapter, in the same reader.
	doc := parse(t, `Hello <b>there</b>.`)
	if got, want := shape(doc.Blocks, ""), "p Hello <strong>there</strong>.\n"; got != want {
		t.Errorf("shape = %q, want %q", got, want)
	}
}

func TestLimits(t *testing.T) {
	deep := strings.Repeat("<div>", 300) + "x" + strings.Repeat("</div>", 300)
	if _, err := Parse([]byte(deep), Limits{}); err == nil {
		t.Error("a document 300 elements deep was accepted")
	} else if !strings.Contains(err.Error(), "nested deeper than 256") {
		t.Errorf("error = %v, want it to name the depth", err)
	}
	// And the same document under a limit that allows it.
	if _, err := Parse([]byte(deep), Limits{MaxDepth: 400}); err != nil {
		t.Errorf("MaxDepth=400 refused a 300-deep document: %v", err)
	}

	wide := strings.Repeat("<p>x</p>", 100)
	if _, err := Parse([]byte(wide), Limits{MaxNodes: 10}); err == nil {
		t.Error("a document with 100 paragraphs was accepted under MaxNodes=10")
	} else if !strings.Contains(err.Error(), "more than 10 nodes") {
		t.Errorf("error = %v, want it to name the budget", err)
	}
	if _, err := Parse([]byte(wide), Limits{}); err != nil {
		t.Errorf("the default budget refused 100 paragraphs: %v", err)
	}
}

// ⛔ A limit reached INSIDE a nested structure must still be reported, not
// quietly truncate the document. Each of these spends its budget in a
// different walker, which is the only way to know every one of them checks.
func TestEveryWalkerStopsWhenTheBudgetIsSpent(t *testing.T) {
	for _, tc := range []struct{ name, src string }{
		{"in a list", `<ul><li>a</li><li>b</li><li>c</li></ul>`},
		{"in a definition list", `<dl><dt>a</dt><dd>b</dd><dt>c</dt></dl>`},
		{"in a table section", `<table><tbody><tr><td>a</td><td>b</td></tr></tbody></table>`},
		{"in a bare row", `<table><tr><td>a</td><td>b</td><td>c</td></tr></table>`},
		{"in inline content", `<p>a <b>b</b> <i>c</i> <em>d</em></p>`},
	} {
		t.Run(tc.name, func(t *testing.T) {
			for budget := 1; budget <= 6; budget++ {
				_, err := Parse([]byte(tc.src), Limits{MaxNodes: budget})
				if err == nil {
					return // the whole document fitted; nothing to prove
				}
				if !strings.Contains(err.Error(), "nodes") {
					t.Fatalf("budget %d: error = %v, want the node budget", budget, err)
				}
			}
		})
	}
}

// ⛔ THE MUTATION SWEEP FOUND THIS HOLE. Four `!c.spend(depth)` guards — one
// per inner loop — survived deletion, because every fixture's items had
// CONTENT, and the walk into that content charged the budget one layer down.
// An EMPTY item charges nothing there, so a million of them would have cost
// nothing at all: the guards are what bounds them, and these are the only
// documents that can tell.
func TestAThousandEmptyChildrenAreStillCharged(t *testing.T) {
	for _, tc := range []struct{ name, src string }{
		{"empty list items", "<ul>" + strings.Repeat("<li></li>", 1000) + "</ul>"},
		{"empty terms", "<dl>" + strings.Repeat("<dt></dt>", 1000) + "</dl>"},
		{"empty definitions", "<dl>" + strings.Repeat("<dd></dd>", 1000) + "</dl>"},
		{"empty rows in a section", "<table><tbody>" + strings.Repeat("<tr><td></td></tr>", 1000) + "</tbody></table>"},
		// A table may hold any number of tbody elements, and an empty one
		// charges nothing below itself. This is the only document that reaches
		// the guard in the section loop, and the sweep is what said so.
		{"empty sections in a table", "<table>" + strings.Repeat("<tbody></tbody>", 1000) + "</table>"},
		{"empty cells in a row", "<table><tr>" + strings.Repeat("<td></td>", 1000) + "</tr></table>"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			_, err := Parse([]byte(tc.src), Limits{MaxNodes: 100})
			if err == nil {
				t.Fatal("a thousand empty children cost nothing: the budget is not charged in this loop")
			}
			if !strings.Contains(err.Error(), "more than 100 nodes") {
				t.Errorf("error = %v, want the node budget", err)
			}
		})
	}
}

func TestDepthIsCheckedInsideEveryNestedStructure(t *testing.T) {
	for _, tc := range []struct{ name, src string }{
		{"a list item", `<ul><li>x</li></ul>`},
		{"a definition", `<dl><dt>x</dt></dl>`},
		{"a table", `<table><tr><td>x</td></tr></table>`},
		{"inline content", `<p><b>x</b></p>`},
	} {
		t.Run(tc.name, func(t *testing.T) {
			if _, err := Parse([]byte(tc.src), Limits{MaxDepth: 1}); err == nil {
				t.Errorf("MaxDepth=1 accepted %s", tc.name)
			}
		})
	}
}

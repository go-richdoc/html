// Copyright (c) the go-richdoc authors.
// SPDX-License-Identifier: BSD-3-Clause

package html

import (
	"fmt"
	"reflect"
	"strings"
	"testing"

	"github.com/go-richdoc/richdoc"
)

// writeAndParse renders a document and reads it back with this package's own
// parser, which is the equality oracle: the two halves of one converter must
// agree, and nothing else in reach knows what this model means.
func writeAndParse(t *testing.T, d *richdoc.Document) *richdoc.Document {
	t.Helper()
	b, err := Write(d)
	if err != nil {
		t.Fatalf("writing: %v", err)
	}
	back, err := Parse(b, Limits{})
	if err != nil {
		t.Fatalf("reading back: %v\n%s", err, b)
	}
	return back
}

// same compares two documents and prints both when they differ.
func same(t *testing.T, got, want *richdoc.Document, what string) {
	t.Helper()
	if !reflect.DeepEqual(got, want) {
		t.Errorf("%s\n got: %#v\nwant: %#v", what, got, want)
	}
}

func TestProseSurvivesTheRoundTrip(t *testing.T) {
	in := &richdoc.Document{Blocks: []richdoc.Block{
		richdoc.Heading{Level: 2, ID: "chapter-one", Inlines: runs("Chapter one")},
		richdoc.Paragraph{Inlines: []richdoc.Inline{
			richdoc.Text{Value: "plain, "},
			richdoc.Emph{Inlines: runs("emphasised")},
			richdoc.Text{Value: ", "},
			richdoc.Strong{Inlines: runs("strong")},
			richdoc.Text{Value: ", "},
			richdoc.Strikethrough{Inlines: runs("struck")},
			richdoc.Text{Value: " and "},
			richdoc.Code{Value: "code()"},
			richdoc.Text{Value: "."},
		}},
		richdoc.Paragraph{Inlines: []richdoc.Inline{
			richdoc.Link{URL: "https://example.org/", Title: "a title", Inlines: runs("a link")},
			richdoc.LineBreak{},
			richdoc.Text{Value: "after the break"},
		}},
		richdoc.ThematicBreak{},
		richdoc.BlockQuote{Blocks: []richdoc.Block{
			richdoc.Paragraph{Inlines: runs("quoted")},
		}},
	}}
	same(t, writeAndParse(t, in), in, "prose did not come back")
}

// ⛔ TIGHT AND LOOSE ARE THE SAME MARKUP BUT FOR A <p>, and a writer that
// always wrapped, or never did, would turn every list into the other kind on
// the next read.
func TestAListKeepsItsTightnessAndItsStart(t *testing.T) {
	for _, c := range []struct {
		name string
		list richdoc.List
	}{
		{"tight unordered", richdoc.List{Tight: true, Items: items("one", "two")}},
		{"loose unordered", richdoc.List{Items: items("one", "two")}},
		{"tight ordered", richdoc.List{Ordered: true, Tight: true, Items: items("one", "two")}},
		{"ordered from seven", richdoc.List{Ordered: true, Tight: true, Start: 7, Items: items("one", "two")}},
	} {
		t.Run(c.name, func(t *testing.T) {
			in := &richdoc.Document{Blocks: []richdoc.Block{c.list}}
			same(t, writeAndParse(t, in), in, "the list changed")
		})
	}
}

func TestANestedListStaysNested(t *testing.T) {
	inner := richdoc.List{Tight: true, Items: items("inner")}
	in := &richdoc.Document{Blocks: []richdoc.Block{
		richdoc.List{Tight: true, Items: []richdoc.ListItem{{Blocks: []richdoc.Block{
			richdoc.Paragraph{Inlines: runs("outer")},
			inner,
		}}}},
	}}
	// An item holding a paragraph AND a list is not tight markup, so it comes
	// back loose — the one thing the markup cannot say twice.
	back := writeAndParse(t, in)
	outer, ok := back.Blocks[0].(richdoc.List)
	if !ok || len(outer.Items) != 1 {
		t.Fatalf("the outer list came back as %#v", back.Blocks[0])
	}
	if len(outer.Items[0].Blocks) != 2 {
		t.Fatalf("the item holds %d blocks, want a paragraph and a list", len(outer.Items[0].Blocks))
	}
	if got, ok := outer.Items[0].Blocks[1].(richdoc.List); !ok || len(got.Items) != 1 {
		t.Errorf("the nested list came back as %#v", outer.Items[0].Blocks[1])
	}
}

func TestACodeBlockKeepsItsLanguageAndItsBytes(t *testing.T) {
	in := &richdoc.Document{Blocks: []richdoc.Block{
		richdoc.CodeBlock{Language: "go", Text: "if a < b && c > d {\n\treturn \"x\"\n}\n"},
	}}
	same(t, writeAndParse(t, in), in, "the code block changed")
}

// ⛔ THE ESCAPING TEST IS NOT ABOUT VALIDITY, IT IS ABOUT INJECTION. A writer
// that escapes four of the five characters produces a page that is fine until
// somebody's document holds the fifth, and the result is not a parse error: it
// is somebody else's markup in the page.
func TestTextThatLooksLikeMarkupComesBackAsText(t *testing.T) {
	nasty := `<script>alert("x")</script> & 'quoted' <b>not bold</b>`
	in := &richdoc.Document{Blocks: []richdoc.Block{
		richdoc.Paragraph{Inlines: runs(nasty)},
	}}
	b, err := Write(in)
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(b), "<script>") {
		t.Fatalf("a script tag reached the page:\n%s", b)
	}
	back := writeAndParse(t, in)
	same(t, back, in, "the text came back changed")

	// And the same for a title and a link, which are written as attributes.
	in = &richdoc.Document{Blocks: []richdoc.Block{
		richdoc.Paragraph{Inlines: []richdoc.Inline{
			richdoc.Link{URL: "https://example.org/?a=1&b=2", Title: `he said "no"`, Inlines: runs("x")},
		}},
	}}
	same(t, writeAndParse(t, in), in, "an attribute came back changed")
}

// ⛔⛔ THE DEFECT THE ROUND TRIP CAUGHT, kept as the case it really is. Every
// attribute was written with %q, which is GO quoting: a title holding a double
// quote came out as `he said \"no\"`, and an HTML parser has never heard of a
// backslash escape — it ends the attribute at the second quote and reads the
// rest as markup. On a title that is a truncated tooltip; on any attribute it
// is an injection, because `" onload="…` closes one attribute and opens
// another. The test that found it was comparing a round trip, not looking for
// a vulnerability.
func TestAnAttributeCannotBeClosedByItsOwnValue(t *testing.T) {
	escape := `" onload="alert(1)`
	in := &richdoc.Document{Blocks: []richdoc.Block{
		richdoc.Heading{Level: 1, ID: escape, Inlines: runs("h")},
		richdoc.Paragraph{Inlines: []richdoc.Inline{
			richdoc.Link{URL: escape, Title: escape, Inlines: runs("l")},
			richdoc.Image{URL: escape, Alt: escape, Title: escape, Width: escape},
			richdoc.Anchor{ID: escape, Inlines: runs("a")},
			richdoc.CrossRef{Target: escape, Inlines: runs("r")},
		}},
		richdoc.Paragraph{Classes: []string{escape}, Inlines: runs("p")},
		richdoc.CodeBlock{Language: escape, Text: "x"},
	}}
	b, err := Write(in)
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(b), "onload=\"alert(1)\"") {
		t.Fatalf("an attribute value closed its own attribute:\n%s", b)
	}
	// The parser is the judge: read it back and no node may carry an onload.
	back, err := Parse(b, Limits{})
	if err != nil {
		t.Fatalf("the page does not parse: %v", err)
	}
	if strings.Contains(richdoc.PlainText(back), "alert") {
		t.Errorf("the escape became text in the document: %q", richdoc.PlainText(back))
	}
	// And the values still arrive whole, which is the other half: escaping that
	// mangles the content is not escaping, it is losing.
	link := back.Blocks[1].(richdoc.Paragraph).Inlines[0].(richdoc.Link)
	if link.URL != escape || link.Title != escape {
		t.Errorf("the link came back as %#v", link)
	}
}

func TestATableKeepsItsCaptionHeaderAndSpans(t *testing.T) {
	in := &richdoc.Document{Blocks: []richdoc.Block{
		richdoc.Table{
			Caption: runs("what it holds"),
			Header:  []richdoc.Cell{{Inlines: runs("a")}, {Inlines: runs("b")}},
			Rows: [][]richdoc.Cell{
				{{Inlines: runs("1"), ColSpan: 2}},
				{{Inlines: runs("2"), RowSpan: 2}, {Inlines: runs("3")}},
			},
		},
	}}
	back := writeAndParse(t, in)
	got, ok := back.Blocks[0].(richdoc.Table)
	if !ok {
		t.Fatalf("the table came back as %#v", back.Blocks[0])
	}
	if len(got.Caption) == 0 {
		t.Error("the caption was lost")
	}
	if len(got.Header) != 2 {
		t.Errorf("the header has %d cells, want 2", len(got.Header))
	}
	if len(got.Rows) != 2 || got.Rows[0][0].ColSpan != 2 || got.Rows[1][0].RowSpan != 2 {
		t.Errorf("the spans were lost: %#v", got.Rows)
	}
}

func TestWhatTheDocumentSaysAboutItself(t *testing.T) {
	in := &richdoc.Document{
		Blocks: []richdoc.Block{richdoc.Paragraph{Inlines: runs("body")}},
		Meta: map[string]string{
			"title": "The Report", "author": "Someone",
			"date": "2026-11-05", "description": "what it is about",
			"language": "en",
		},
	}
	same(t, writeAndParse(t, in), in, "the metadata changed")
}

// ⛔ A MAP ITERATED IN GO'S OWN ORDER WRITES A DIFFERENT FILE EVERY RUN, and
// two conversions of one document would differ for no reason anybody can see —
// which is the kind of difference that gets blamed on the document.
func TestTwoWritesOfOneDocumentAreTheSameBytes(t *testing.T) {
	in := &richdoc.Document{
		Blocks: []richdoc.Block{richdoc.Paragraph{Inlines: runs("body")}},
		Meta: map[string]string{
			"title": "t", "author": "a", "date": "d", "description": "x", "language": "en",
		},
	}
	first, err := Write(in)
	if err != nil {
		t.Fatal(err)
	}
	for i := 0; i < 8; i++ {
		again, err := Write(in)
		if err != nil {
			t.Fatal(err)
		}
		if string(again) != string(first) {
			t.Fatalf("write %d differs:\n%s\n---\n%s", i, first, again)
		}
	}
}

func TestAFootnoteBecomesAReferenceAndASection(t *testing.T) {
	in := &richdoc.Document{Blocks: []richdoc.Block{
		richdoc.Paragraph{Inlines: []richdoc.Inline{
			richdoc.Text{Value: "see"},
			richdoc.Footnote{Blocks: []richdoc.Block{
				richdoc.Paragraph{Inlines: runs("the note")},
			}},
		}},
	}}
	b, err := Write(in)
	if err != nil {
		t.Fatal(err)
	}
	for _, want := range []string{`href="#fn1"`, `id="fn1"`, "the note", `class="footnotes"`} {
		if !strings.Contains(string(b), want) {
			t.Errorf("the page does not hold %q:\n%s", want, b)
		}
	}
}

// ⛔ RAW CONTENT IS KEPT ONLY FOR THE FORMATS THIS PAIR UNDERSTANDS. The model
// says a converter may drop a raw node for another format, and dropping it is
// the honest thing: writing LaTeX source into an HTML page would put the
// backslashes on the screen as if the author had typed them.
func TestRawContentIsKeptOrDroppedByItsFormat(t *testing.T) {
	in := &richdoc.Document{Blocks: []richdoc.Block{
		richdoc.RawBlock{Format: "html", Text: "<hr class=\"raw\">"},
		richdoc.RawBlock{Format: "latex", Text: `\newpage`},
		richdoc.Paragraph{Inlines: []richdoc.Inline{
			richdoc.RawInline{Format: "html", Text: "<b>kept</b>"},
			richdoc.RawInline{Format: "latex", Text: `\ldots`},
		}},
	}}
	b, err := Write(in)
	if err != nil {
		t.Fatal(err)
	}
	page := string(b)
	if !strings.Contains(page, `<hr class="raw">`) || !strings.Contains(page, "<b>kept</b>") {
		t.Errorf("html raw content was dropped:\n%s", page)
	}
	if strings.Contains(page, `\newpage`) || strings.Contains(page, `\ldots`) {
		t.Errorf("LaTeX source reached an HTML page:\n%s", page)
	}
}

func TestMathIsWrittenAsItsSourceBecauseHTMLHasNoElementForIt(t *testing.T) {
	in := &richdoc.Document{Blocks: []richdoc.Block{
		richdoc.MathBlock{TeX: `a^2 + b^2 = c^2`},
		richdoc.Paragraph{Inlines: []richdoc.Inline{richdoc.Math{TeX: `x < y`}}},
	}}
	b, err := Write(in)
	if err != nil {
		t.Fatal(err)
	}
	page := string(b)
	if !strings.Contains(page, `\[a^2 + b^2 = c^2\]`) {
		t.Errorf("display maths is not in the page:\n%s", page)
	}
	// And its source is ESCAPED: TeX is full of characters HTML reads.
	if !strings.Contains(page, `\(x &lt; y\)`) {
		t.Errorf("inline maths was not escaped:\n%s", page)
	}
}

// ⛔ The four fields Parse does not read back are WRITTEN all the same, because
// what lays the page out does read them. Dropping them would lose what the
// author asked for, which is the reason the model carries them at all.
func TestAnImageCarriesWhatTheAuthorAskedFor(t *testing.T) {
	in := &richdoc.Document{Blocks: []richdoc.Block{
		richdoc.Paragraph{Inlines: []richdoc.Inline{richdoc.Image{
			URL: "fig.png", Alt: "a figure", Title: "t",
			Width: "200", Height: "100", Scale: 50, Align: richdoc.AlignRight,
		}}},
	}}
	b, err := Write(in)
	if err != nil {
		t.Fatal(err)
	}
	page := string(b)
	for _, want := range []string{`src="fig.png"`, `alt="a figure"`, `title="t"`,
		`width="200"`, `height="100"`, "width: 50%", "float: right"} {
		if !strings.Contains(page, want) {
			t.Errorf("the image does not carry %q:\n%s", want, page)
		}
	}
	// What DOES come back is named in Write's own documentation.
	back := writeAndParse(t, in)
	got := back.Blocks[0].(richdoc.Paragraph).Inlines[0].(richdoc.Image)
	if got.URL != "fig.png" || got.Alt != "a figure" || got.Title != "t" {
		t.Errorf("the image came back as %#v", got)
	}
}

func TestANilDocumentIsAnEmptyPageAndNotNothing(t *testing.T) {
	b, err := Write(nil)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(b), "<!doctype html>") || !strings.Contains(string(b), "</html>") {
		t.Errorf("a nil document did not render a page:\n%s", b)
	}
	if _, err := Parse(b, Limits{}); err != nil {
		t.Errorf("the empty page does not parse: %v", err)
	}
}

// ⛔ A heading level is CLAMPED rather than refused. richdoc says 1..6 and
// nothing enforces it; HTML has h1..h6 and nothing else. A producer that
// counted its own nesting past six should get an h6, not a tag no browser has.
func TestAHeadingLevelOutsideOneToSixIsClamped(t *testing.T) {
	for _, c := range []struct{ level, want int }{{0, 1}, {-3, 1}, {7, 6}, {99, 6}} {
		in := &richdoc.Document{Blocks: []richdoc.Block{
			richdoc.Heading{Level: c.level, Inlines: runs("h")},
		}}
		b, err := Write(in)
		if err != nil {
			t.Fatal(err)
		}
		if !strings.Contains(string(b), fmt.Sprintf("<h%d>h</h%d>", c.want, c.want)) {
			t.Errorf("level %d did not become h%d:\n%s", c.level, c.want, b)
		}
	}
}

// A heading with nothing in it and no anchor is nothing. One with an anchor is
// a target, and a target with no text is still a place to link to.
func TestAnEmptyHeadingIsDroppedUnlessItIsATarget(t *testing.T) {
	empty := &richdoc.Document{Blocks: []richdoc.Block{richdoc.Heading{Level: 2}}}
	b, err := Write(empty)
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(b), "<h2") {
		t.Errorf("an empty heading was written:\n%s", b)
	}
	target := &richdoc.Document{Blocks: []richdoc.Block{richdoc.Heading{Level: 2, ID: "here"}}}
	if b, err = Write(target); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(b), `id="here"`) {
		t.Errorf("a heading that is only an anchor was dropped:\n%s", b)
	}
}

// ⛔ An empty item is still an item: a list of three bullets whose middle one
// is blank has three bullets. What it must not become is an item holding an
// empty paragraph, which renders as a bullet followed by a blank line.
func TestAnEmptyListItemKeepsItsBullet(t *testing.T) {
	in := &richdoc.Document{Blocks: []richdoc.Block{richdoc.List{Tight: true, Items: []richdoc.ListItem{
		{Blocks: []richdoc.Block{richdoc.Paragraph{Inlines: runs("one")}}},
		{},
		{Blocks: []richdoc.Block{richdoc.Paragraph{Inlines: runs("three")}}},
	}}}}
	b, err := Write(in)
	if err != nil {
		t.Fatal(err)
	}
	if n := strings.Count(string(b), "<li>"); n != 3 {
		t.Errorf("%d bullets for three items:\n%s", n, b)
	}
}

// A cell may hold real block content, and Cell.Content is the preference
// order: the blocks when the producer filled them, the flattened inlines when
// it did not.
func TestACellHoldsBlocksAndAnEmptyCellIsEmpty(t *testing.T) {
	in := &richdoc.Document{Blocks: []richdoc.Block{richdoc.Table{
		Rows: [][]richdoc.Cell{{
			{
				Inlines: runs("a b"),
				Blocks: []richdoc.Block{
					richdoc.Paragraph{Inlines: runs("a")},
					richdoc.Paragraph{Inlines: runs("b")},
				},
			},
			{},
		}},
	}}}
	b, err := Write(in)
	if err != nil {
		t.Fatal(err)
	}
	page := string(b)
	if !strings.Contains(page, "<p>a</p>") || !strings.Contains(page, "<p>b</p>") {
		t.Errorf("a cell's blocks were flattened away:\n%s", page)
	}
	if !strings.Contains(page, "<td></td>") {
		t.Errorf("an empty cell is not empty:\n%s", page)
	}
}

func TestAColumnAlignmentIsWrittenAsAStyle(t *testing.T) {
	in := &richdoc.Document{Blocks: []richdoc.Block{richdoc.Table{
		Align: []richdoc.Alignment{
			richdoc.AlignLeft, richdoc.AlignCenter, richdoc.AlignRight, richdoc.AlignDefault,
		},
		Rows: [][]richdoc.Cell{{
			{Inlines: runs("l")}, {Inlines: runs("c")},
			{Inlines: runs("r")}, {Inlines: runs("d")},
			// ⛔ A fifth cell with only four alignments: a shorter Align slice
			// leaves the rest at AlignDefault, which the model says outright.
			{Inlines: runs("x")},
		}},
	}}}
	b, err := Write(in)
	if err != nil {
		t.Fatal(err)
	}
	page := string(b)
	for _, want := range []string{"text-align: left", "text-align: center", "text-align: right"} {
		if !strings.Contains(page, want) {
			t.Errorf("%q is missing:\n%s", want, page)
		}
	}
	if n := strings.Count(page, "text-align"); n != 3 {
		t.Errorf("%d alignments written; the default and the missing one carry none:\n%s", n, page)
	}
}

// A cross-reference with no visible text stands in its target, which is what
// the model says a renderer should supply.
func TestACrossReferenceWithNoTextNamesItsTarget(t *testing.T) {
	in := &richdoc.Document{Blocks: []richdoc.Block{richdoc.Paragraph{Inlines: []richdoc.Inline{
		richdoc.CrossRef{Target: "eq-1"},
	}}}}
	b, err := Write(in)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(b), `<a href="#eq-1">eq-1</a>`) {
		t.Errorf("the target did not stand in for the text:\n%s", b)
	}
}

func TestAnImageCarriesItsAlignmentAndItsClasses(t *testing.T) {
	for _, c := range []struct {
		align richdoc.Alignment
		want  string
	}{
		{richdoc.AlignLeft, "float: left"},
		{richdoc.AlignCenter, "display: block"},
		{richdoc.AlignRight, "float: right"},
	} {
		in := &richdoc.Document{Blocks: []richdoc.Block{richdoc.Paragraph{Inlines: []richdoc.Inline{
			richdoc.Image{URL: "a.png", Align: c.align, Classes: []string{"figure"}},
		}}}}
		b, err := Write(in)
		if err != nil {
			t.Fatal(err)
		}
		if !strings.Contains(string(b), c.want) {
			t.Errorf("alignment %v did not write %q:\n%s", c.align, c.want, b)
		}
		if !strings.Contains(string(b), `class="figure"`) {
			t.Errorf("an image's classes were dropped:\n%s", b)
		}
	}
}

// A quotation of nothing is nothing: reading `<blockquote></blockquote>` back
// gives no block, so writing one would make the second pass differ.
func TestAQuotationOfNothingIsNotWritten(t *testing.T) {
	in := &richdoc.Document{Blocks: []richdoc.Block{
		richdoc.BlockQuote{Blocks: []richdoc.Block{richdoc.Paragraph{Inlines: runs("   ")}}},
		richdoc.Paragraph{Inlines: runs("after")},
	}}
	b, err := Write(in)
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(b), "<blockquote") {
		t.Errorf("an empty quotation was written:\n%s", b)
	}
	if !strings.Contains(string(b), "after") {
		t.Errorf("the block after it was lost:\n%s", b)
	}
}

// ⛔ A list whose item holds a single block that is NOT a paragraph cannot be
// written tight: the block needs its own tag inside the <li>, and reading that
// back marks the whole list loose.
func TestAnItemHoldingOneNonParagraphMakesTheListLoose(t *testing.T) {
	in := &richdoc.Document{Blocks: []richdoc.Block{richdoc.List{
		Tight: true,
		Items: []richdoc.ListItem{
			{Blocks: []richdoc.Block{richdoc.Paragraph{Inlines: runs("a")}}},
			{Blocks: []richdoc.Block{richdoc.CodeBlock{Text: "c"}}},
		},
	}}}
	back := writeAndParse(t, in)
	got, ok := back.Blocks[0].(richdoc.List)
	if !ok {
		t.Fatalf("the list came back as %#v", back.Blocks[0])
	}
	if got.Tight {
		t.Error("a list holding a code block came back tight")
	}
	if len(got.Items) != 2 {
		t.Errorf("%d items, want 2", len(got.Items))
	}
}

// ⛔ THIS TEST REPLACES A DEFAULT BRANCH NOTHING COULD REACH. richdoc's Block
// and Inline are CLOSED sets — their marker methods are unexported — so no
// test outside that package can construct a foreign node, and a default branch
// handling one is a guard no test can enter. Instead: one of EVERY type this
// package knows, and none of them may render to nothing.
//
// A type added to richdoc renders as nothing here until it is added to the
// switch, and this is where that is meant to be noticed.
func TestEveryBlockAndInlineTypeRendersSomething(t *testing.T) {
	blocks := []richdoc.Block{
		richdoc.Heading{Level: 1, Inlines: runs("h")},
		richdoc.Paragraph{Inlines: runs("p")},
		richdoc.List{Tight: true, Items: items("i")},
		richdoc.CodeBlock{Text: "c"},
		richdoc.BlockQuote{Blocks: []richdoc.Block{richdoc.Paragraph{Inlines: runs("q")}}},
		richdoc.Table{Rows: [][]richdoc.Cell{{{Inlines: runs("t")}}}},
		richdoc.ThematicBreak{},
		richdoc.MathBlock{TeX: "m"},
		richdoc.RawBlock{Format: "html", Text: "<hr>"},
	}
	for _, b := range blocks {
		var w writer
		if got := w.block(b, 0); got == "" {
			t.Errorf("a %T renders to nothing", b)
		}
	}

	inlines := []richdoc.Inline{
		richdoc.Text{Value: "t"},
		richdoc.Emph{Inlines: runs("e")},
		richdoc.Strong{Inlines: runs("s")},
		richdoc.Strikethrough{Inlines: runs("d")},
		richdoc.Code{Value: "c"},
		richdoc.Link{URL: "u"},
		richdoc.Image{URL: "i"},
		richdoc.Math{TeX: "m"},
		richdoc.LineBreak{},
		richdoc.RawInline{Format: "html", Text: "<b>b</b>"},
		richdoc.Footnote{Blocks: []richdoc.Block{richdoc.Paragraph{Inlines: runs("f")}}},
		richdoc.Anchor{ID: "a"},
		richdoc.CrossRef{Target: "x"},
	}
	for _, in := range inlines {
		var w writer
		if got := w.inline(in); got == "" {
			t.Errorf("a %T renders to nothing", in)
		}
	}

	// ⛔ And the one case that SHOULD render to nothing, so that "nothing" is
	// a decision here and not an oversight: raw content for a format this
	// cannot put in an HTML page.
	var w writer
	if got := w.block(richdoc.RawBlock{Format: "latex", Text: `\x`}, 0); got != "" {
		t.Errorf("LaTeX raw content rendered as %q", got)
	}
	if got := w.inline(richdoc.RawInline{Format: "latex", Text: `\x`}); got != "" {
		t.Errorf("LaTeX raw inline rendered as %q", got)
	}
}

// ⛔ THE PROPERTY WHEN THERE IS NO GROUND TRUTH: a round trip need not be the
// identity — Write names what it loses — but it must REACH A FIXPOINT. Once a
// document has been through the pair, writing it again must produce the same
// bytes. A writer that kept rewording its own output would make every second
// conversion of one file a different file, and nothing would say which was
// right.
//
// It is fuzzed from arbitrary HTML rather than from documents, because that is
// what the pair is handed: an EPUB's chapter, an email's body, somebody's
// saved page.
func FuzzWritingTwiceChangesNothing(f *testing.F) {
	f.Add([]byte("<h1 id=x>t</h1><p>a <em>b</em> &amp; <code>c</code></p>"))
	f.Add([]byte("<ul><li>a</li><li><p>b</p></li></ul>"))
	f.Add([]byte(`<table><caption>c</caption><tr><th colspan=2>h</th></tr></table>`))
	f.Add([]byte(`<img src="a" alt='b"c'><a href="#x">y</a>`))
	f.Add([]byte("<pre><code class=\"language-go\">x &lt; y</code></pre>"))
	f.Add([]byte("<html lang=fr><head><title>t</title></head><body><p>p</p></body></html>"))
	f.Add([]byte(""))

	f.Fuzz(func(t *testing.T, src []byte) {
		first, err := Parse(src, Limits{})
		if err != nil {
			return // not HTML this package will read; Parse's own fuzz covers that
		}
		once, err := Write(first)
		if err != nil {
			t.Fatalf("writing: %v", err)
		}
		second, err := Parse(once, Limits{})
		if err != nil {
			t.Fatalf("what this wrote does not parse: %v\n%s", err, once)
		}
		twice, err := Write(second)
		if err != nil {
			t.Fatalf("writing again: %v", err)
		}
		if string(twice) != string(once) {
			t.Fatalf("the second pass changed the page\nonce:\n%s\ntwice:\n%s", once, twice)
		}
	})
}

// runs is one run of literal text, which most of these documents are made of.
// (parse_test.go already has a `text`, which reads a node's words.)
func runs(s string) []richdoc.Inline { return []richdoc.Inline{richdoc.Text{Value: s}} }

// items makes one tight list item per string.
func items(ss ...string) []richdoc.ListItem {
	out := make([]richdoc.ListItem, 0, len(ss))
	for _, s := range ss {
		out = append(out, richdoc.ListItem{Blocks: []richdoc.Block{
			richdoc.Paragraph{Inlines: runs(s)},
		}})
	}
	return out
}

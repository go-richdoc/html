// Copyright (c) the go-richdoc authors.
// SPDX-License-Identifier: BSD-3-Clause

package html

import (
	"fmt"
	"html"
	"strconv"
	"strings"

	"github.com/go-richdoc/richdoc"
)

// Write renders a [richdoc.Document] as a complete HTML5 document: a doctype, a
// head carrying what the document says about itself, and a body of the blocks.
//
// The error return is always nil; it is kept for symmetry with [Parse]. A nil
// document renders to an empty page rather than to nothing, because a converter
// handed one still owes its caller a file.
//
// # Why this exists, and why a whole document
//
// ⛔ EVERY READER IN go-richdoc REACHED A DEAD END HERE. Word, Excel and
// PowerPoint (go-ooxml), ODT, ODS, ODP and ODG (go-odf), Markdown, CSV, JSON,
// XML, plain text, EPUB, MOBI, FB2 and email (go-richdoc) all arrive at the
// same model, and the model had no way out to a laid-out page: measured on
// 2026-11-05, every ToPDF in the fleet lives in go-pdfkit/convert and every one
// of them takes pictures, an SVG, a ZIP of pictures or an XPS package. Seventeen
// capabilities were missing for want of one writer.
//
// A complete document and not a fragment, because the thing that lays HTML out
// — go-pdfkit/html2pdf — takes a document, and because [Parse] reads the head:
// the language off <html lang>, the title off <title>, and author, description
// and date off <meta name>. Writing them is what makes the pair round-trip.
//
// # What does not survive a round trip
//
// Named, rather than left to be discovered. [Parse] does not read these back,
// so a document written and re-parsed loses them:
//
//   - [richdoc.Image]'s Width, Height, Scale and Align — the parser reads src,
//     alt and title and no more. They are WRITTEN all the same, because what
//     lays the page out does read them.
//   - Classes everywhere except a code block's language, which survives as
//     class="language-x" because that is the one class the parser looks for.
//   - [richdoc.Math] and [richdoc.MathBlock] carry TeX, and HTML has no element
//     for TeX. They are written as the delimited source every pipeline without a
//     maths renderer shows, and come back as text.
//   - [richdoc.Footnote] becomes a reference and a list at the end; it comes
//     back as a link and a section, which is what it looks like.
//   - [richdoc.CrossRef] becomes a link to its target, and comes back as one.
//   - [richdoc.RawBlock] and [richdoc.RawInline] are written verbatim when they
//     name html, svg or math — the three the parser itself produces — and
//     DROPPED otherwise, which is what the model says a converter may do with a
//     format it cannot render.
func Write(d *richdoc.Document) ([]byte, error) {
	var w writer
	body := w.blocks(blocksOf(d), 0)

	var b strings.Builder
	b.WriteString("<!doctype html>\n<html")
	if lang := metaOf(d, "language"); lang != "" {
		b.WriteString(attrOf("lang", lang))
	}
	b.WriteString(">\n<head>\n<meta charset=\"utf-8\">\n")
	if title := metaOf(d, "title"); title != "" {
		fmt.Fprintf(&b, "<title>%s</title>\n", esc(title))
	}
	// ⛔ In a fixed order, and only the keys Parse reads. A map iterated in Go's
	// own order writes a different file every run, which makes two conversions
	// of one document differ for no reason anybody can see.
	for _, k := range []string{"author", "description", "date"} {
		if v := metaOf(d, k); v != "" {
			fmt.Fprintf(&b, "<meta%s%s>\n", attrOf("name", k), attrOf("content", v))
		}
	}
	b.WriteString("</head>\n<body>\n")
	b.WriteString(body)
	if notes := w.notes(); notes != "" {
		b.WriteString(notes)
	}
	b.WriteString("\n</body>\n</html>\n")
	return []byte(b.String()), nil
}

// blocksOf is a document's blocks, or nothing for a nil document.
func blocksOf(d *richdoc.Document) []richdoc.Block {
	if d == nil {
		return nil
	}
	return d.Blocks
}

// metaOf reads one key of a document's metadata.
func metaOf(d *richdoc.Document, key string) string {
	if d == nil || d.Meta == nil {
		return ""
	}
	return d.Meta[key]
}

// writer holds the footnote bodies collected while the body renders. A footnote
// is written where it is referenced and its text goes to the end, which is what
// a footnote is.
type writer struct {
	footnotes []string
}

// notes renders the collected footnote bodies, numbered in reference order.
//
// The loop is index-based because rendering a note's body may reference further
// notes, which append as it goes.
func (w *writer) notes() string {
	if len(w.footnotes) == 0 {
		return ""
	}
	var b strings.Builder
	b.WriteString("\n<section class=\"footnotes\">\n<ol>\n")
	for i := 0; i < len(w.footnotes); i++ {
		fmt.Fprintf(&b, "<li id=\"fn%d\">%s</li>\n", i+1, w.footnotes[i])
	}
	b.WriteString("</ol>\n</section>\n")
	return b.String()
}

// blocks renders a sequence of blocks, one per line, indented by depth.
func (w *writer) blocks(bs []richdoc.Block, depth int) string {
	var out []string
	for _, b := range bs {
		if s := w.block(b, depth); s != "" {
			out = append(out, s)
		}
	}
	return strings.Join(out, "\n")
}

func (w *writer) block(b richdoc.Block, depth int) string {
	pad := strings.Repeat("  ", depth)
	switch n := b.(type) {
	case richdoc.Heading:
		level := n.Level
		if level < 1 {
			level = 1
		}
		if level > 6 {
			level = 6
		}
		id := ""
		if n.ID != "" {
			id = attrOf("id", n.ID)
		}
		head := w.flow(n.Inlines)
		if head == "" && n.ID == "" {
			return ""
		}
		return fmt.Sprintf("%s<h%d%s>%s</h%d>", pad, level, id, head, level)

	case richdoc.Paragraph:
		// ⛔ A paragraph that renders to nothing but space is not written. The
		// parser drops a block with no content — a document made of a vertical
		// tab parses to a paragraph holding one space, and `<p> </p>` read back
		// is no paragraph at all — so writing it would make the second
		// conversion of a document differ from the first. Found by the fixpoint
		// fuzz, not by reading the parser.
		body := w.flow(n.Inlines)
		if body == "" {
			return ""
		}
		return fmt.Sprintf("%s<p%s>%s</p>", pad, classAttr(n.Classes), body)

	case richdoc.List:
		return w.list(n, depth)

	case richdoc.CodeBlock:
		// ⛔ class="language-x" and not a data attribute: it is the one class
		// Parse looks for, so it is the one spelling that comes back.
		code := "<code"
		if n.Language != "" {
			code += attrOf("class", "language-"+n.Language)
		}
		return fmt.Sprintf("%s<pre%s>%s>%s</code></pre>",
			pad, classAttr(n.Classes), code, esc(n.Text))

	case richdoc.BlockQuote:
		return fmt.Sprintf("%s<blockquote%s>\n%s\n%s</blockquote>",
			pad, classAttr(n.Classes), w.blocks(n.Blocks, depth+1), pad)

	case richdoc.Table:
		return w.table(n, depth)

	case richdoc.ThematicBreak:
		return pad + "<hr>"

	case richdoc.MathBlock:
		// HTML has no element for TeX. This is what every pipeline without a
		// maths renderer shows, and it is legible either way.
		return fmt.Sprintf("%s<p class=\"math display\">\\[%s\\]</p>", pad, esc(n.TeX))

	case richdoc.RawBlock:
		if keepsRaw(n.Format) {
			return pad + n.Text
		}
		return ""
	}
	// ⛔ The block set is closed, so this is unreachable for anything this
	// package knows — and a richdoc that grew a block type would otherwise
	// vanish silently rather than show up as something to handle.
	return fmt.Sprintf("%s<!-- richdoc: a %T that this writer does not know -->", pad, b)
}

// list renders an ordered or unordered list. A loose list wraps each item's
// text in <p>, which is exactly how Parse tells the two apart.
func (w *writer) list(n richdoc.List, depth int) string {
	pad := strings.Repeat("  ", depth)
	tag := "ul"
	start := ""
	if n.Ordered {
		tag = "ol"
		if n.Start > 0 && n.Start != 1 {
			start = " start=\"" + strconv.Itoa(n.Start) + "\""
		}
	}
	var b strings.Builder
	fmt.Fprintf(&b, "%s<%s%s%s>\n", pad, tag, start, classAttr(n.Classes))
	for _, item := range n.Items {
		fmt.Fprintf(&b, "%s  <li>", pad)
		b.WriteString(w.item(item, n.Tight, depth+2))
		b.WriteString("</li>\n")
	}
	fmt.Fprintf(&b, "%s</%s>", pad, tag)
	return b.String()
}

// item renders one list item's blocks. A tight item unwraps a lone paragraph,
// because a <p> inside an <li> is what makes a list loose.
func (w *writer) item(item richdoc.ListItem, tight bool, depth int) string {
	if tight && len(item.Blocks) == 1 {
		if p, ok := item.Blocks[0].(richdoc.Paragraph); ok {
			return w.flow(p.Inlines)
		}
	}
	if len(item.Blocks) == 0 {
		// ⛔ An empty item is still an item. Writing nothing inside the <li>
		// keeps the bullet, which is what an empty bullet looks like; writing a
		// paragraph would put a blank line in the middle of the list.
		return ""
	}
	return "\n" + w.blocks(item.Blocks, depth) + "\n" + strings.Repeat("  ", depth-1)
}

// table renders a table with its caption, header and alignments.
func (w *writer) table(n richdoc.Table, depth int) string {
	pad := strings.Repeat("  ", depth)
	var b strings.Builder
	fmt.Fprintf(&b, "%s<table%s>\n", pad, classAttr(n.Classes))
	if len(n.Caption) > 0 {
		fmt.Fprintf(&b, "%s  <caption>%s</caption>\n", pad, w.flow(n.Caption))
	}
	if len(n.Header) > 0 {
		fmt.Fprintf(&b, "%s  <thead>\n%s", pad, w.row(n.Header, n.Align, "th", depth+2))
		fmt.Fprintf(&b, "%s  </thead>\n", pad)
	}
	if len(n.Rows) > 0 {
		fmt.Fprintf(&b, "%s  <tbody>\n", pad)
		for _, r := range n.Rows {
			b.WriteString(w.row(r, n.Align, "td", depth+2))
		}
		fmt.Fprintf(&b, "%s  </tbody>\n", pad)
	}
	fmt.Fprintf(&b, "%s</table>", pad)
	return b.String()
}

// row renders one row of cells.
func (w *writer) row(cells []richdoc.Cell, align []richdoc.Alignment, tag string, depth int) string {
	pad := strings.Repeat("  ", depth)
	var b strings.Builder
	fmt.Fprintf(&b, "%s<tr>\n", pad)
	for i, c := range cells {
		fmt.Fprintf(&b, "%s  <%s%s%s>", pad, tag, spanAttr(c), alignAttr(align, i))
		b.WriteString(w.cell(c, depth+2))
		fmt.Fprintf(&b, "</%s>\n", tag)
	}
	fmt.Fprintf(&b, "%s</tr>\n", pad)
	return b.String()
}

// cell renders a cell's content, which is [richdoc.Cell.Content]'s answer: the
// blocks when the producer filled them, and the flattened inlines otherwise.
func (w *writer) cell(c richdoc.Cell, depth int) string {
	blocks := c.Content()
	if len(blocks) == 1 {
		if p, ok := blocks[0].(richdoc.Paragraph); ok {
			return w.flow(p.Inlines)
		}
	}
	if len(blocks) == 0 {
		return ""
	}
	return "\n" + w.blocks(blocks, depth) + "\n" + strings.Repeat("  ", depth-1)
}

// spanAttr writes colspan and rowspan, which Parse reads back.
func spanAttr(c richdoc.Cell) string {
	out := ""
	if c.ColSpan > 1 {
		out += " colspan=\"" + strconv.Itoa(c.ColSpan) + "\""
	}
	if c.RowSpan > 1 {
		out += " rowspan=\"" + strconv.Itoa(c.RowSpan) + "\""
	}
	return out
}

// alignAttr writes a column's alignment as a style, which is what lays out.
// Parse does not read it back; see the note on Write.
func alignAttr(align []richdoc.Alignment, col int) string {
	if col >= len(align) {
		return ""
	}
	switch align[col] {
	case richdoc.AlignLeft:
		return " style=\"text-align: left\""
	case richdoc.AlignCenter:
		return " style=\"text-align: center\""
	case richdoc.AlignRight:
		return " style=\"text-align: right\""
	}
	return ""
}

// classAttr writes an author's own presentational names.
func classAttr(classes []string) string {
	if len(classes) == 0 {
		return ""
	}
	return attrOf("class", strings.Join(classes, " "))
}

// keepsRaw says whether a raw node's format is one this writer can put in an
// HTML page unchanged. They are the three Parse itself produces.
func keepsRaw(format string) bool {
	switch strings.ToLower(format) {
	case "html", "svg", "math":
		return true
	}
	return false
}

// flow renders inline content that is a BLOCK's whole content, trimmed.
//
// ⛔ Trimmed because [Parse] trims: its rule is that a block's leading and
// trailing space is dropped, so `<p> 0</p>` reads back as `0` and a writer
// that emitted the space would differ from itself on the next pass. The fuzz
// found this one as `\v0`.
func (w *writer) flow(ins []richdoc.Inline) string {
	return strings.TrimSpace(w.inlines(ins))
}

// inlines renders a sequence of inline nodes.
//
// ⛔⛔ A SPACE AT A JOIN IS NOT WRITTEN TWICE, and collapsing inside each node
// is not enough to prevent it. `0 <a></a> 0` parses to two text runs, "0 " and
// " 0" — the empty element left a space on each side — and each run holds one
// space, so neither is collapsed and the join produces two. HTML collapses them
// at the next read, so the second conversion of the document differed from the
// first, which a fuzz run found in three seconds.
func (w *writer) inlines(ins []richdoc.Inline) string {
	var b strings.Builder
	for _, in := range ins {
		piece := w.inline(in)
		if strings.HasPrefix(piece, " ") && strings.HasSuffix(b.String(), " ") {
			piece = piece[1:]
		}
		b.WriteString(piece)
	}
	return b.String()
}

func (w *writer) inline(in richdoc.Inline) string {
	switch n := in.(type) {
	case richdoc.Text:
		return escFlow(n.Value)
	case richdoc.Emph:
		return "<em>" + w.inlines(n.Inlines) + "</em>"
	case richdoc.Strong:
		return "<strong>" + w.inlines(n.Inlines) + "</strong>"
	case richdoc.Strikethrough:
		return "<del>" + w.inlines(n.Inlines) + "</del>"
	case richdoc.Code:
		// Inline code is flow content: it is not inside <pre>, so its spaces
		// collapse on the way back whatever this writes.
		return fmt.Sprintf("<code%s>%s</code>", classAttr(n.Classes), escFlow(n.Value))
	case richdoc.Link:
		return fmt.Sprintf("<a%s%s>%s</a>",
			attrOf("href", n.URL), titleAttr(n.Title), w.inlines(n.Inlines))
	case richdoc.Image:
		return w.image(n)
	case richdoc.Math:
		return fmt.Sprintf("<span class=\"math inline\">\\(%s\\)</span>", esc(n.TeX))
	case richdoc.LineBreak:
		return "<br>"
	case richdoc.RawInline:
		if keepsRaw(n.Format) {
			return n.Text
		}
		return ""
	case richdoc.Footnote:
		w.footnotes = append(w.footnotes, "")
		at := len(w.footnotes)
		// Rendered AFTER the slot is taken, so a note inside a note numbers
		// after its parent rather than displacing it.
		w.footnotes[at-1] = w.blocks(n.Blocks, 0)
		return fmt.Sprintf("<sup><a href=\"#fn%d\" id=\"fnref%d\">%d</a></sup>", at, at, at)
	case richdoc.Anchor:
		return fmt.Sprintf("<a%s>%s</a>", attrOf("id", n.ID), w.inlines(n.Inlines))
	case richdoc.CrossRef:
		text := w.inlines(n.Inlines)
		if text == "" {
			text = esc(n.Target)
		}
		return fmt.Sprintf("<a%s>%s</a>", attrOf("href", "#"+n.Target), text)
	}
	return ""
}

// image writes an image with everything the author asked for, including the
// four fields Parse does not read back: what lays the page out does read them,
// and dropping them would lose what the author wrote.
func (w *writer) image(n richdoc.Image) string {
	out := "<img" + attrOf("src", n.URL) + attrOf("alt", n.Alt) + titleAttr(n.Title)
	if n.Width != "" {
		out += attrOf("width", n.Width)
	}
	if n.Height != "" {
		out += attrOf("height", n.Height)
	}
	var style []string
	if n.Scale > 0 {
		style = append(style, fmt.Sprintf("width: %d%%", n.Scale))
	}
	switch n.Align {
	case richdoc.AlignLeft:
		style = append(style, "float: left")
	case richdoc.AlignRight:
		style = append(style, "float: right")
	case richdoc.AlignCenter:
		style = append(style, "display: block", "margin: 0 auto")
	}
	if len(style) > 0 {
		out += attrOf("style", strings.Join(style, "; "))
	}
	if len(n.Classes) > 0 {
		out += classAttr(n.Classes)
	}
	return out + ">"
}

// titleAttr writes an optional advisory title.
func titleAttr(title string) string {
	if title == "" {
		return ""
	}
	return attrOf("title", title)
}

// esc escapes text for HTML.
//
// ⛔ Through the standard library, not by hand. A writer that escapes four of
// the five characters produces a file that is valid until somebody's document
// holds the fifth, and the result is not a parse error but a page with somebody
// else's markup in it.
func esc(s string) string { return html.EscapeString(s) }

// escFlow escapes text that lands in FLOW content, collapsing runs of
// whitespace on the way.
//
// ⛔⛔ THE COLLAPSE IS WHAT MAKES THE PAIR REACH A FIXPOINT, and a fuzz run
// found it in three seconds. `0 <a></a> 0` parses to one text run holding TWO
// spaces — the empty element contributed nothing and left both spaces behind —
// so writing it gave `0  0`, and reading THAT back collapsed the pair, because
// collapsing runs of whitespace is what HTML does and what [Parse] documents.
// The second pass therefore produced a different file from the first.
//
// Collapsing here loses nothing that HTML could have carried: a browser, this
// package's own parser and every other HTML reader collapse those spaces at
// the first opportunity. What it buys is that the loss happens ONCE, where it
// can be named, instead of appearing between the first and second conversion
// of the same document and leaving nobody able to say which file was right.
//
// ⛔ Flow content only. A code block's text is inside <pre>, where whitespace
// is kept byte for byte, and an attribute is not flow content at all.
func escFlow(s string) string { return esc(collapseSpace(s)) }

// collapseSpace turns every run of whitespace into one space, keeping a single
// leading or trailing one: `a <em>b</em>` must not lose the space before the
// emphasis, and a block's own leading and trailing space is [Parse]'s to drop.
func collapseSpace(s string) string {
	var b strings.Builder
	b.Grow(len(s))
	space := false
	for _, r := range s {
		if r == ' ' || r == '\t' || r == '\n' || r == '\r' || r == '\f' || r == '\v' {
			space = true
			continue
		}
		if space {
			b.WriteByte(' ')
		}
		space = false
		b.WriteRune(r)
	}
	if space {
		b.WriteByte(' ')
	}
	return b.String()
}

// attr writes one attribute, escaped for an attribute.
//
// ⛔⛔ NOT %q, WHICH IS GO QUOTING AND NOT HTML QUOTING. Every attribute here
// was written with %q at first, and a title holding a double quote came back
// truncated: Go writes `he said \"no\"` and an HTML parser, which has never
// heard of a backslash escape, ends the attribute at the second quote. The
// round-trip test caught it on a title; the same bug on any attribute is an
// injection, because `" onload="…` closes the attribute and opens another.
//
// html.EscapeString turns a quote into &#34;, which is what an attribute wants.
func attrOf(name, value string) string {
	return fmt.Sprintf(" %s=\"%s\"", name, esc(value))
}

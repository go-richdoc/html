package html

import (
	"strings"

	"github.com/go-richdoc/richdoc"
	xhtml "golang.org/x/net/html"
	"golang.org/x/net/html/atom"
)

// inlines turns the children of an element into inline content.
func (c *conv) inlines(n *xhtml.Node, depth int) []richdoc.Inline {
	var out []richdoc.Inline
	for k := n.FirstChild; k != nil; k = k.NextSibling {
		if !c.spend(depth) {
			return out
		}
		switch k.Type {
		case xhtml.TextNode:
			out = append(out, c.text(k.Data)...)
			continue
		case xhtml.ElementNode:
		default:
			continue
		}
		if skipped(k) {
			continue
		}
		out = append(out, c.mark(k, depth+1)...)
	}
	return out
}

// mark wraps a run of inline content in what its element says about it.
//
// ⛔ An element this does not know PASSES ITS CHILDREN THROUGH rather than
// disappearing. A span, a mark, a bdi, a custom element and tomorrow's tag all
// carry text, and a reader built on a list of known tags loses the sentence
// somebody wrapped for a reason that has nothing to do with meaning.
func (c *conv) mark(n *xhtml.Node, depth int) []richdoc.Inline {
	kids := func() []richdoc.Inline { return c.inlines(n, depth) }
	switch n.DataAtom {
	case atom.Br:
		return []richdoc.Inline{richdoc.LineBreak{}}
	case atom.Wbr:
		// A word-break opportunity is typography, not content, and it must not
		// become a line break: it says "you MAY break here".
		return nil
	case atom.Img:
		return image(n)
	case atom.A:
		return c.anchor(n, depth)
	case atom.Em, atom.I, atom.Cite, atom.Var, atom.Dfn:
		return wrap(kids(), func(in []richdoc.Inline) richdoc.Inline {
			return richdoc.Emph{Inlines: in}
		})
	case atom.Strong, atom.B:
		return wrap(kids(), func(in []richdoc.Inline) richdoc.Inline {
			return richdoc.Strong{Inlines: in}
		})
	case atom.Del, atom.S, atom.Strike:
		return wrap(kids(), func(in []richdoc.Inline) richdoc.Inline {
			return richdoc.Strikethrough{Inlines: in}
		})
	case atom.Code, atom.Kbd, atom.Samp, atom.Tt:
		// ⛔ Code carries a STRING and not inlines, so markup inside it is
		// flattened rather than nested: `<code><b>x</b></code>` is the text x,
		// monospaced. The alternative — dropping it — loses the identifier.
		if s := textOf(n); strings.TrimSpace(s) != "" {
			return []richdoc.Inline{richdoc.Code{Value: s}}
		}
		return nil
	case atom.Math, atom.Svg:
		return []richdoc.Inline{richdoc.RawInline{Format: n.Data, Text: render(n)}}
	}
	return kids()
}

// wrap applies a marking, and only when there is something to mark: an empty
// <strong> is a stray tag, and a writer asked to emit emphasis around nothing
// produces `****`.
func wrap(in []richdoc.Inline, f func([]richdoc.Inline) richdoc.Inline) []richdoc.Inline {
	if len(in) == 0 {
		return nil
	}
	return []richdoc.Inline{f(in)}
}

// anchor is <a>: a link when it has an href, a named target when it has an id
// and no href, and plain content when it has neither.
func (c *conv) anchor(n *xhtml.Node, depth int) []richdoc.Inline {
	in := c.inlines(n, depth)
	href := attr(n, "href")
	if href != "" {
		if len(in) == 0 {
			// A link with no text still goes somewhere, and the URL is the
			// only thing a reader can be shown.
			in = []richdoc.Inline{richdoc.Text{Value: href}}
		}
		return []richdoc.Inline{richdoc.Link{URL: href, Title: attr(n, "title"), Inlines: in}}
	}
	// ⛔ `name` as well as `id`: an anchor written for HTML 4 uses name, and
	// every internal link in a converted ebook points at one of those.
	id := attr(n, "id")
	if id == "" {
		id = attr(n, "name")
	}
	if id != "" {
		return []richdoc.Inline{richdoc.Anchor{ID: id, Inlines: in}}
	}
	return in
}

// image is <img>.
func image(n *xhtml.Node) []richdoc.Inline {
	src := attr(n, "src")
	if src == "" {
		// ⛔ A src-less img is markup, not a picture. It happens: a template
		// that failed to fill, or a lazy-loading image whose real URL is in a
		// data attribute this reader is not going to guess at.
		return nil
	}
	return []richdoc.Inline{richdoc.Image{
		URL:   src,
		Alt:   attr(n, "alt"),
		Title: attr(n, "title"),
	}}
}

// text collapses a run of characters the way HTML collapses it: any run of
// spaces, tabs, newlines, carriage returns and form feeds becomes one space.
//
// ⛔ It is NOT trimmed here. The space between `</b>` and `c` in
// "<b>b</b> c" is a text node of its own, and trimming each node separately
// would run the words together — which is the defect that makes converted
// ebooks read as onewordafteranother. Trimming belongs to the block, once its
// whole run is known: see trim.
// collapse gives "" only for an empty string, and the parser emits no empty
// text node, so there is no "nothing came back" branch here to defend.
func (c *conv) text(s string) []richdoc.Inline {
	return []richdoc.Inline{richdoc.Text{Value: collapse(s)}}
}

func collapse(s string) string {
	var b strings.Builder
	b.Grow(len(s))
	space := false
	// ⛔ A text node of nothing but whitespace is ONE SPACE, not nothing. The
	// space between `</b>` and `<i>` in "<b>a</b> <i>b</i>" is exactly such a
	// node, and returning "" for it is how a reader turns two words into one.
	// Measured: this was the first thing the tests caught, against a comment in
	// this very file claiming the defect was avoided.
	//
	// It costs nothing at a block's edges, because trim drops a space that
	// ends up there -- and a whitespace-only node between two blocks therefore
	// does not become a paragraph.
	if s != "" && strings.TrimLeft(s, " \t\n\r\f") == "" {
		return " "
	}
	for _, r := range s {
		switch r {
		case ' ', '\t', '\n', '\r', '\f':
			space = true
			continue
		case ' ':
			// A non-breaking space is a CHARACTER, not whitespace: it is there
			// because an author did not want a break, and collapsing it away
			// changes "10 km" into something a line can split.
			space = false
			b.WriteString(" ")
			continue
		}
		if space && b.Len() > 0 {
			b.WriteByte(' ')
		}
		space = false
		b.WriteRune(r)
	}
	if space && b.Len() > 0 {
		b.WriteByte(' ')
	}
	return b.String()
}

// trim drops the leading and trailing space of a finished run, which is what
// HTML does at a block's edges.
func trim(in []richdoc.Inline) []richdoc.Inline {
	for len(in) > 0 {
		t, ok := in[0].(richdoc.Text)
		if !ok {
			break
		}
		if s := strings.TrimLeft(t.Value, " "); s != "" {
			in[0] = richdoc.Text{Value: s}
			break
		}
		in = in[1:]
	}
	for len(in) > 0 {
		last := len(in) - 1
		t, ok := in[last].(richdoc.Text)
		if !ok {
			break
		}
		if s := strings.TrimRight(t.Value, " "); s != "" {
			in[last] = richdoc.Text{Value: s}
			break
		}
		in = in[:last]
	}
	return in
}

// flatten is a degraded rendering of blocks as one run of inline content, with
// a line break where a block boundary was.
//
// It exists for [richdoc.Cell], whose contract is that a producer setting
// Blocks must also set Inlines "to whatever degraded rendering it would have
// produced before" — so that a consumer written before Cell.Blocks existed
// renders the cell's words instead of an empty cell.
func flatten(blocks []richdoc.Block) []richdoc.Inline {
	var out []richdoc.Inline
	for _, b := range blocks {
		in := inlinesOf(b)
		if len(in) == 0 {
			continue
		}
		if len(out) > 0 {
			out = append(out, richdoc.LineBreak{})
		}
		out = append(out, in...)
	}
	return out
}

// inlinesOf is the inline content of one block, as far as there is any.
func inlinesOf(b richdoc.Block) []richdoc.Inline {
	switch v := b.(type) {
	case richdoc.Paragraph:
		return v.Inlines
	case richdoc.Heading:
		return v.Inlines
	case richdoc.CodeBlock:
		return []richdoc.Inline{richdoc.Code{Value: v.Text}}
	case richdoc.BlockQuote:
		return flatten(v.Blocks)
	case richdoc.List:
		var out []richdoc.Inline
		for _, item := range v.Items {
			if in := flatten(item.Blocks); len(in) > 0 {
				if len(out) > 0 {
					out = append(out, richdoc.LineBreak{})
				}
				out = append(out, in...)
			}
		}
		return out
	}
	// A table inside a cell, a thematic break, raw content: there is no honest
	// one-line rendering, and inventing one would put a table's words in a
	// cell as a sentence.
	return nil
}

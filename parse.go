package html

import (
	"bytes"
	"fmt"
	"strconv"
	"strings"

	"github.com/go-richdoc/richdoc"
	xhtml "golang.org/x/net/html"
	"golang.org/x/net/html/atom"
)

// Limits bound what a document may ask for. A zero field means the default, so
// Limits{} is the default everywhere.
type Limits struct {
	// MaxDepth is how deeply elements may nest before the reader refuses.
	//
	// ⛔ This bounds OUR walk, not the parse. golang.org/x/net/html builds its
	// tree without recursion and happily returns one a thousand elements deep;
	// what would overflow a stack is this package walking it. The guard is
	// therefore where the recursion is, which is the opposite of the rule for
	// encoding/xml — there the decoder recurses and a guard in our own code
	// never runs.
	MaxDepth int

	// MaxNodes is how many nodes the reader will consider in total. A deeply
	// nested document is one shape of abuse; a flat document with four million
	// empty spans is the other, and MaxDepth says nothing about it.
	MaxNodes int

	// MaxInputBytes is how much source this reader will take.
	//
	// ⛔ THE TWO CEILINGS ABOVE BOUND THE WALK AND NOT THE PARSE, and that is
	// not a bound on memory. golang.org/x/net/html builds its whole tree before
	// conv.spend is called once, and it has no ceiling of its own to fall back
	// on: its only limit is Tokenizer.SetMaxBuf, which html.Parse does not set
	// (x/net v0.62.0, token.go:149 and :1278; parse.go and node.go hold no
	// depth or node bound at all). Measured: 100 000 <br> and 800 000, both
	// refused at node 1 001 of a caller's ceiling, cost 12 MB and 96 MB — a
	// ratio of 8.0 for 8x the source. The refusal arrived; the allocation had
	// already happened, and nothing capped it.
	//
	// 64 MiB is what the rest of the family uses, and the population agrees:
	// over 123 369 real HTML files on one machine the biggest is 19.87 MB
	// (Chromium's licence page) and p99.9 is 1.77 MB.
	MaxInputBytes int

	// MaxSourceElements is how many tags the source may contain, counted by the
	// tokenizer before any tree is built, so that a hostile document is refused
	// without being parsed.
	//
	// ⛔ IT IS ITS OWN CEILING AND NOT A PROXY FOR MaxNodes. It cannot be
	// derived from one: tags in <head> are never walked, so a document could
	// hold a million of them and reach MaxNodes at zero — a source-side count
	// is no lower bound on what the walk will count, and a guard that refuses
	// what the thing it protects would accept is worse than no guard. So this
	// bounds the SOURCE, says so, and takes its number from the population:
	// the widest of 123 369 real files holds 98 961 elements, and 1 << 20
	// leaves that an order of magnitude of room.
	MaxSourceElements int
}

const (
	defaultMaxDepth          = 256
	defaultMaxNodes          = 1 << 20
	defaultMaxInputBytes     = 64 << 20
	defaultMaxSourceElements = 1 << 20

	// headingLevels is as many as richdoc's writers have.
	headingLevels = 6
)

func (l Limits) orDefaults() Limits {
	if l.MaxDepth <= 0 {
		l.MaxDepth = defaultMaxDepth
	}
	if l.MaxNodes <= 0 {
		l.MaxNodes = defaultMaxNodes
	}
	if l.MaxInputBytes <= 0 {
		l.MaxInputBytes = defaultMaxInputBytes
	}
	if l.MaxSourceElements <= 0 {
		l.MaxSourceElements = defaultMaxSourceElements
	}
	return l
}

// countSourceElements is how many tags the source holds, stopping at the
// ceiling.
//
// ⛔ IT COUNTS WITH THE TOKENIZER AND NOT WITH A SCAN FOR '<'. A scan counts
// the '<' inside an attribute value, inside a comment and inside a <script>,
// and the three disagree with the parser in different directions — so a guard
// built on one would refuse documents over characters the parser never read as
// markup.
//
// ⛔ And it asks for no TagName. The name is not needed to count a tag, and
// z.TagName() copies one per token: the whole value of refusing before the tree
// is built is lost if the refusal allocates per tag anyway.
func countSourceElements(src []byte, max int) error {
	z := xhtml.NewTokenizer(bytes.NewReader(src))
	n := 0
	for {
		switch z.Next() {
		case xhtml.ErrorToken:
			// ⛔ Whatever the tokenizer stopped for — EOF or a malformed
			// stream — is not this function's business. html.Parse accepts
			// every input the HTML5 algorithm can recover, which is all of
			// them, and inventing a syntax error here would refuse documents
			// the reader has always read.
			return nil
		case xhtml.StartTagToken, xhtml.SelfClosingTagToken:
			n++
			if n > max {
				return fmt.Errorf("html: more than %d elements in the source", max)
			}
		}
	}
}

// Parse reads HTML or XHTML into a document.
//
// A fragment is as welcome as a whole page: the HTML5 algorithm synthesises the
// html, head and body elements a fragment leaves out, which is what makes the
// same reader serve an EPUB chapter and the body of an email.
func Parse(src []byte, lim Limits) (*richdoc.Document, error) {
	// ⛔ The error is dropped, and the reason is a GUARANTEE rather than an
	// assumption: xhtml.Parse reports only what its io.Reader reports, a
	// bytes.Reader never fails, and the HTML5 algorithm returns a document for
	// every input including no bytes at all. A branch no input can reach is a
	// branch no test can defend, and this package's CI refuses an uncovered
	// one. The errors Parse does return are its own: the limits below.
	// ⛔ BEFORE xhtml.Parse, because that is where the memory goes. The ceilings
	// in conv.spend are checked while this package walks the tree, which is
	// after golang.org/x/net/html has built all of it — see Limits.MaxInputBytes
	// for the measurement.
	lim = lim.orDefaults()
	if len(src) > lim.MaxInputBytes {
		return nil, fmt.Errorf("html: %d bytes of source, and the limit is %d",
			len(src), lim.MaxInputBytes)
	}
	if err := countSourceElements(src, lim.MaxSourceElements); err != nil {
		return nil, err
	}

	root, _ := xhtml.Parse(bytes.NewReader(src))
	c := &conv{limits: lim}
	doc := &richdoc.Document{}
	htmlEl, head, body := sections(root)
	doc.Meta = meta(htmlEl, head)
	if body != nil {
		doc.Blocks = c.blocks(body, 1)
	}
	if c.err != nil {
		return nil, c.err
	}
	return doc, nil
}

// sections finds the html element, the head and the body.
//
// ⛔ The html ELEMENT, not the document node the parser hands back: the
// language of a document is `<html lang="fr">`, and reading the attribute off
// the document node finds nothing — which is how the first run of the tests
// lost the language of a French page.
func sections(root *xhtml.Node) (htmlEl, head, body *xhtml.Node) {
	// ⛔ No early return once all three are found. It would be unreachable:
	// the parser emits html, head and body in that order for every input, so
	// the walk always finds the last of them on its way out — and a branch no
	// input can reach is a branch no test can defend.
	elements(root, func(n *xhtml.Node) {
		switch n.DataAtom {
		case atom.Html:
			htmlEl = n
		case atom.Head:
			head = n
		case atom.Body:
			body = n
		}
	})
	return htmlEl, head, body
}

// elements calls fn for every element in a tree, in document order.
//
// ⛔ It is NOT a range-over-func iterator, and that is a measurement rather
// than a preference: written as one, it needed a "stop" result on every step,
// and with no caller that stops, the two early-return branches were lines the
// coverage profile showed no input could reach. The callers here read a whole
// head and a whole document; neither leaves early.
func elements(n *xhtml.Node, fn func(*xhtml.Node)) {
	for c := n.FirstChild; c != nil; c = c.NextSibling {
		if c.Type == xhtml.ElementNode {
			fn(c)
		}
		elements(c, fn)
	}
}

// meta is what the document says about itself, in the keys richdoc's other
// readers use.
func meta(htmlEl, head *xhtml.Node) map[string]string {
	out := map[string]string{}
	if htmlEl != nil {
		if lang := attr(htmlEl, "lang"); lang != "" {
			out["language"] = lang
		}
	}
	if head != nil {
		elements(head, func(n *xhtml.Node) {
			switch n.DataAtom {
			case atom.Title:
				if s := strings.TrimSpace(textOf(n)); s != "" {
					out["title"] = s
				}
			case atom.Meta:
				// ⛔ Matched on the NAME, lowercased: producers write Author,
				// AUTHOR and author, and the attribute is case-insensitive.
				switch strings.ToLower(attr(n, "name")) {
				case "author":
					if s := strings.TrimSpace(attr(n, "content")); s != "" {
						out["author"] = s
					}
				case "description":
					if s := strings.TrimSpace(attr(n, "content")); s != "" {
						out["description"] = s
					}
				case "date", "dcterms.date":
					if s := strings.TrimSpace(attr(n, "content")); s != "" {
						out["date"] = s
					}
				}
			}
		})
	}
	if len(out) == 0 {
		// A document that says nothing about itself has no metadata, not an
		// empty map: a caller testing for a title should not find a map with
		// nothing in it and have to look twice.
		return nil
	}
	return out
}

// a conv carries what the whole walk needs: the limits, and how much of them
// is spent.
type conv struct {
	limits Limits
	nodes  int
	err    error
}

// spend accounts for one node and reports whether the walk may go on.
//
// ⛔ The first check is a FAST PATH and nothing more, which the mutation sweep
// proved: delete it and every test still passes, because the error is already
// recorded and every message a later call could write is the same one. It stays
// so that a refused document stops being walked, not because anything
// observable depends on it — and saying which of the two it is, is the point.
func (c *conv) spend(depth int) bool {
	if c.err != nil {
		return false
	}
	if depth > c.limits.MaxDepth {
		c.err = fmt.Errorf("html: nested deeper than %d elements", c.limits.MaxDepth)
		return false
	}
	c.nodes++
	if c.nodes > c.limits.MaxNodes {
		c.err = fmt.Errorf("html: more than %d nodes", c.limits.MaxNodes)
		return false
	}
	return true
}

// blocks turns the children of one element into blocks.
//
// ⛔ Inline content between blocks becomes a paragraph of its own. HTML lets a
// div hold a sentence, a table and another sentence, and a reader that kept
// only the children it recognised as blocks would drop both sentences.
func (c *conv) blocks(parent *xhtml.Node, depth int) []richdoc.Block {
	b := builder{c: c, depth: depth}
	for n := parent.FirstChild; n != nil; n = n.NextSibling {
		b.add(n)
	}
	return b.done()
}

// a builder accumulates blocks and the run of inline content between them.
type builder struct {
	c      *conv
	depth  int
	out    []richdoc.Block
	inline []richdoc.Inline
}

func (b *builder) add(n *xhtml.Node) {
	if !b.c.spend(b.depth) {
		return
	}
	switch n.Type {
	case xhtml.TextNode:
		b.inline = append(b.inline, b.c.text(n.Data)...)
		return
	case xhtml.ElementNode:
	default:
		// A comment, a doctype, the document node itself: not content.
		return
	}
	if skipped(n) {
		return
	}
	if blk, ok := b.c.block(n, b.depth); ok {
		b.flush()
		b.out = append(b.out, blk...)
		return
	}
	// ⛔ mark, NOT inlines. inlines(n) walks n's CHILDREN and so loses what n
	// itself said: `Hello <b>there</b>` came back as plain words, and
	// `<figure><img></figure>` came back empty, because an img has no children
	// at all. Measured by the tests on the first run.
	b.inline = append(b.inline, b.c.mark(n, b.depth+1)...)
}

// flush turns the pending inline run into a paragraph, if it says anything.
func (b *builder) flush() {
	in := trim(b.inline)
	b.inline = nil
	if len(in) > 0 {
		b.out = append(b.out, richdoc.Paragraph{Inlines: in})
	}
}

func (b *builder) done() []richdoc.Block {
	b.flush()
	return b.out
}

// skipped reports whether an element is not content at all.
//
// ⛔ THIS IS THE LIST THAT MATTERS. A stylesheet and a script are text nodes in
// the tree like any other, and a reader that walks the tree without this list
// emits a page of CSS declarations as the document's first paragraph. It is the
// commonest defect in HTML-to-text converters and it is one switch away.
func skipped(n *xhtml.Node) bool {
	switch n.DataAtom {
	case atom.Script, atom.Style, atom.Noscript, atom.Template,
		atom.Head, atom.Title, atom.Base, atom.Link, atom.Meta,
		atom.Iframe, atom.Object, atom.Audio, atom.Video, atom.Canvas,
		atom.Map, atom.Area, atom.Param, atom.Source, atom.Track:
		return true
	}
	return false
}

// block converts an element that is a block, and says whether it was one.
func (c *conv) block(n *xhtml.Node, depth int) ([]richdoc.Block, bool) {
	switch n.DataAtom {
	case atom.H1, atom.H2, atom.H3, atom.H4, atom.H5, atom.H6:
		return c.heading(n, depth), true
	case atom.P:
		if in := trim(c.inlines(n, depth+1)); len(in) > 0 {
			return []richdoc.Block{richdoc.Paragraph{Inlines: in}}, true
		}
		return nil, true
	case atom.Pre:
		return c.pre(n), true
	case atom.Blockquote, atom.Figcaption, atom.Details, atom.Fieldset:
		if in := c.blocks(n, depth+1); len(in) > 0 {
			if n.DataAtom == atom.Blockquote {
				return []richdoc.Block{richdoc.BlockQuote{Blocks: in}}, true
			}
			return in, true
		}
		return nil, true
	case atom.Ul, atom.Ol, atom.Menu:
		return c.list(n, depth), true
	case atom.Dl:
		return c.definitions(n, depth), true
	case atom.Table:
		return c.table(n, depth), true
	case atom.Hr:
		return []richdoc.Block{richdoc.ThematicBreak{}}, true
	case atom.Svg, atom.Math:
		return c.raw(n), true
	case atom.Div, atom.Section, atom.Article, atom.Aside, atom.Main,
		atom.Header, atom.Footer, atom.Nav, atom.Figure, atom.Hgroup,
		atom.Address, atom.Form, atom.Body, atom.Html, atom.Center,
		atom.Dialog, atom.Summary, atom.Caption, atom.Colgroup, atom.Col:
		// Transparent containers: their children are the content, and the
		// element itself says something about layout rather than about the
		// document. A div is not a paragraph.
		return c.blocks(n, depth+1), true
	}
	return nil, false
}

// heading is one h1..h6, with the id attribute as its anchor.
func (c *conv) heading(n *xhtml.Node, depth int) []richdoc.Block {
	in := trim(c.inlines(n, depth+1))
	if len(in) == 0 {
		// An empty heading is a layout device, not a section: emitting one
		// would put a nameless entry in every table of contents.
		return nil
	}
	return []richdoc.Block{richdoc.Heading{
		Level:   level(n),
		ID:      attr(n, "id"),
		Inlines: in,
	}}
}

// level is the number in the tag name.
func level(n *xhtml.Node) int {
	switch n.DataAtom {
	case atom.H1:
		return 1
	case atom.H2:
		return 2
	case atom.H3:
		return 3
	case atom.H4:
		return 4
	case atom.H5:
		return 5
	}
	return headingLevels
}

// pre is a code block, kept byte for byte.
//
// ⛔ A leading newline right after <pre> is DROPPED, as the HTML parser's own
// rule says: authors write the opening tag on its own line, and a code block
// that starts with a blank line where the source has none is wrong in every
// writer downstream.
func (c *conv) pre(n *xhtml.Node) []richdoc.Block {
	text := strings.TrimPrefix(textOf(n), "\n")
	if strings.TrimSpace(text) == "" {
		return nil
	}
	return []richdoc.Block{richdoc.CodeBlock{
		Language: language(n),
		Text:     text,
	}}
}

// language reads a highlighter's class, on the pre or on the code inside it.
// "language-go" and "lang-go" are both written; so is a bare "go".
func language(n *xhtml.Node) string {
	nodes := []*xhtml.Node{n}
	for c := n.FirstChild; c != nil; c = c.NextSibling {
		if c.Type == xhtml.ElementNode && c.DataAtom == atom.Code {
			nodes = append(nodes, c)
		}
	}
	for _, m := range nodes {
		for _, class := range strings.Fields(attr(m, "class")) {
			for _, prefix := range []string{"language-", "lang-", "highlight-"} {
				if s, ok := strings.CutPrefix(class, prefix); ok && s != "" {
					return s
				}
			}
		}
	}
	return ""
}

// list is ul or ol.
//
// ⛔ Tight or loose is read from the MARKUP rather than guessed: a list whose
// items wrap their text in <p> is the loose one, which is exactly the
// distinction CommonMark makes and the reason a Markdown writer can round-trip
// what came from HTML.
func (c *conv) list(n *xhtml.Node, depth int) []richdoc.Block {
	out := richdoc.List{Ordered: n.DataAtom == atom.Ol, Tight: true}
	if out.Ordered {
		if s := attr(n, "start"); s != "" {
			if v, err := strconv.Atoi(s); err == nil {
				out.Start = v
			}
		}
	}
	for li := n.FirstChild; li != nil; li = li.NextSibling {
		if li.Type != xhtml.ElementNode || li.DataAtom != atom.Li {
			continue
		}
		if !c.spend(depth) {
			return nil
		}
		if hasChild(li, atom.P) {
			out.Tight = false
		}
		// ⛔ An empty <li> is still an ITEM, with no blocks in it: a list of
		// three bullets whose middle one is blank has three bullets. What it
		// must not become is an item holding an empty paragraph, which a
		// writer renders as a bullet followed by a blank line.
		out.Items = append(out.Items, richdoc.ListItem{Blocks: c.blocks(li, depth+1)})
	}
	if len(out.Items) == 0 {
		return nil
	}
	return []richdoc.Block{out}
}

// definitions is dl, dt and dd.
//
// ⛔ richdoc has no definition list, so the shape is SAID rather than invented:
// each term becomes a paragraph in bold and its definitions become a block
// quotation under it. That is what a reader sees in every renderer, and it
// keeps both halves — a mapping that dropped the terms would read as a document
// of answers with no questions.
func (c *conv) definitions(n *xhtml.Node, depth int) []richdoc.Block {
	var out []richdoc.Block
	for d := n.FirstChild; d != nil; d = d.NextSibling {
		if d.Type != xhtml.ElementNode {
			continue
		}
		if !c.spend(depth) {
			return nil
		}
		switch d.DataAtom {
		case atom.Dt:
			if in := trim(c.inlines(d, depth+1)); len(in) > 0 {
				out = append(out, richdoc.Paragraph{
					Inlines: []richdoc.Inline{richdoc.Strong{Inlines: in}},
				})
			}
		case atom.Dd:
			if blocks := c.blocks(d, depth+1); len(blocks) > 0 {
				out = append(out, richdoc.BlockQuote{Blocks: blocks})
			}
		}
	}
	return out
}

// raw keeps an element this model has no word for, verbatim, with its format
// named — svg and math, which a writer that knows them can still render.
// The markup is never empty — an element serialises to at least its own tags —
// so there is no "nothing came back" branch to defend.
func (c *conv) raw(n *xhtml.Node) []richdoc.Block {
	return []richdoc.Block{richdoc.RawBlock{Format: n.Data, Text: render(n)}}
}

// render writes an element back out as markup. It is how raw passthrough keeps
// what it cannot model.
//
// ⛔ The error is DROPPED, deliberately and with one consequence stated:
// xhtml.Render fails on a tree it cannot serialise, which a tree it parsed is
// not, and on a Writer that fails, which a strings.Builder does not. Keeping
// the branch would be keeping a line no input can reach — and losing one <svg>
// must not refuse the chapter it sits in, so whatever was written is what gets
// kept.
func render(n *xhtml.Node) string {
	var b strings.Builder
	_ = xhtml.Render(&b, n)
	return b.String()
}

// hasChild reports whether an element has a direct child of this kind.
func hasChild(n *xhtml.Node, a atom.Atom) bool {
	for c := n.FirstChild; c != nil; c = c.NextSibling {
		if c.Type == xhtml.ElementNode && c.DataAtom == a {
			return true
		}
	}
	return false
}

// attr is an attribute's value, or "".
func attr(n *xhtml.Node, name string) string {
	for _, a := range n.Attr {
		if strings.EqualFold(a.Key, name) {
			return a.Val
		}
	}
	return ""
}

// textOf is every character inside an element, markup ignored and whitespace
// untouched. It is what <pre> and <title> want.
func textOf(n *xhtml.Node) string {
	var b strings.Builder
	var walk func(*xhtml.Node)
	walk = func(n *xhtml.Node) {
		for c := n.FirstChild; c != nil; c = c.NextSibling {
			if c.Type == xhtml.TextNode {
				b.WriteString(c.Data)
			}
			walk(c)
		}
	}
	walk(n)
	return b.String()
}

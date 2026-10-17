package html

import (
	"strconv"

	"github.com/go-richdoc/richdoc"
	xhtml "golang.org/x/net/html"
	"golang.org/x/net/html/atom"
)

// a row is a table row and whether every one of its cells was a th.
//
// ⛔ The th-ness travels BESIDE the cells because richdoc.Cell has no field for
// it: a cell is content, and which row is the heading is the table's business.
// Carrying it here rather than inventing a field is the cheaper half of
// [speculative API] -- the information is needed for exactly one decision, two
// functions apart.
type row struct {
	cells []richdoc.Cell
	allTH bool
}

// table turns an HTML table into a richdoc one.
//
// ⛔ The parser has already done the hard part. HTML's tree construction
// inserts the tbody a document omits and moves a stray element out of a table,
// so the rows are reachable the same way whatever the source looked like --
// which is why this walks thead, tbody, tfoot AND tr children without caring
// which of them the author wrote.
func (c *conv) table(n *xhtml.Node, depth int) []richdoc.Block {
	out := richdoc.Table{}
	var rows []row
	var fromHead int

	for sec := n.FirstChild; sec != nil; sec = sec.NextSibling {
		if sec.Type != xhtml.ElementNode {
			continue
		}
		if !c.spend(depth) {
			return nil
		}
		switch sec.DataAtom {
		case atom.Caption:
			out.Caption = trim(c.inlines(sec, depth+1))
		case atom.Thead:
			got := c.rows(sec, depth+1)
			fromHead += len(got)
			rows = append(rows, got...)
		case atom.Tbody, atom.Tfoot:
			rows = append(rows, c.rows(sec, depth+1)...)
		}
		// ⛔ There is deliberately NO `case atom.Tr` here, and that is a fact
		// about the parser rather than a gap: HTML tree construction inserts
		// the tbody a document omits, so a `<table><tr>` arrives as
		// table > tbody > tr and a branch for a bare row is one no input can
		// reach. Measured: the branch was written first and the coverage
		// profile showed it never ran.
	}

	if len(rows) == 0 {
		// A table with a caption and no rows is a caption; a table with
		// neither is nothing at all.
		if len(out.Caption) > 0 {
			return []richdoc.Block{richdoc.Paragraph{Inlines: out.Caption}}
		}
		return nil
	}

	// ⛔ ONE header row, because that is what richdoc.Table has. A thead with
	// several rows keeps the FIRST as the header and leaves the others as
	// ordinary rows, rather than the last one quietly replacing the first --
	// the defect go-richdoc/fb2's table reader was built to avoid, and the one
	// a long table with repeated headings walks into.
	if fromHead > 0 || rows[0].allTH {
		out.Header = rows[0].cells
		rows = rows[1:]
	}
	for _, r := range rows {
		out.Rows = append(out.Rows, r.cells)
	}
	return []richdoc.Block{out}
}

// rows is every tr inside a section.
func (c *conv) rows(sec *xhtml.Node, depth int) []row {
	var out []row
	for tr := sec.FirstChild; tr != nil; tr = tr.NextSibling {
		if tr.Type != xhtml.ElementNode || tr.DataAtom != atom.Tr {
			continue
		}
		if r, ok := c.row(tr, depth+1); ok {
			out = append(out, r)
		}
	}
	return out
}

// row is one tr, and false when it holds no cells at all.
func (c *conv) row(tr *xhtml.Node, depth int) (row, bool) {
	out := row{allTH: true}
	for td := tr.FirstChild; td != nil; td = td.NextSibling {
		if td.Type != xhtml.ElementNode {
			continue
		}
		if td.DataAtom != atom.Td && td.DataAtom != atom.Th {
			continue
		}
		if !c.spend(depth) {
			return row{}, false
		}
		if td.DataAtom != atom.Th {
			out.allTH = false
		}
		out.cells = append(out.cells, c.cell(td, depth+1))
	}
	return out, len(out.cells) > 0
}

// cell is one td or th.
//
// ⛔ A cell with real block content fills BOTH fields, which richdoc's contract
// demands: Blocks for a consumer that can render them, Inlines for one written
// before Blocks existed -- and the two must say the same thing, so Inlines is
// derived from Blocks rather than read a second time.
func (c *conv) cell(td *xhtml.Node, depth int) richdoc.Cell {
	out := richdoc.Cell{
		ColSpan: span(td, "colspan"),
		RowSpan: span(td, "rowspan"),
	}
	blocks := c.blocks(td, depth)
	switch {
	case len(blocks) == 0:
		// An empty cell is a cell: dropping it would shift every cell after it
		// one column to the left.
	case len(blocks) == 1 && isParagraph(blocks[0]):
		out.Inlines = blocks[0].(richdoc.Paragraph).Inlines
	default:
		out.Blocks = blocks
		out.Inlines = flatten(blocks)
	}
	return out
}

func isParagraph(b richdoc.Block) bool {
	_, ok := b.(richdoc.Paragraph)
	return ok
}

// span reads colspan or rowspan.
//
// ⛔ Zero is left as zero rather than written as one: richdoc says the default
// means "spans nothing extra", so a table of ordinary cells carries no span
// fields at all and compares equal to one built by hand. A span below one is a
// producer's mistake and is ignored rather than honoured, since a cell
// spanning zero columns is not a thing a grid can hold.
// ⛔ There is no "the attribute is absent" branch, because strconv.Atoi("")
// already refuses it. The mutation sweep is what found the branch: deleting it
// changed nothing any test could see, which is the definition of a line that
// is not holding anything.
func span(n *xhtml.Node, name string) int {
	v, err := strconv.Atoi(attr(n, name))
	if err != nil || v <= 1 {
		return 0
	}
	return v
}

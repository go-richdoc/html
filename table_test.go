package html

import (
	"strings"
	"testing"

	"github.com/go-richdoc/richdoc"
)

func TestTables(t *testing.T) {
	for _, tc := range []struct{ name, src, want string }{
		{
			name: "a thead is the header and a tbody is the rows",
			src: `<table><caption>Sales</caption>
			  <thead><tr><th>Region</th><th>Units</th></tr></thead>
			  <tbody><tr><td>North</td><td>12</td></tr></tbody></table>`,
			want: "table caption=\"Sales\"\n  header [Region] [Units]\n  row [North] [12]\n",
		},
		{
			name: "a first row of th is the header with no thead at all",
			src:  `<table><tr><th>a</th><th>b</th></tr><tr><td>1</td><td>2</td></tr></table>`,
			want: "table caption=\"\"\n  header [a] [b]\n  row [1] [2]\n",
		},
		{
			name: "a first row of td is not a header",
			src:  `<table><tr><td>1</td><td>2</td></tr></table>`,
			want: "table caption=\"\"\n  row [1] [2]\n",
		},
		{
			// ⛔ A row that mixes th and td is a row. A stub cell in the corner
			// of a cross-tabulation is a th, and taking that row as the header
			// would lose a line of data.
			name: "a row that mixes th and td is not a header",
			src:  `<table><tr><th>corner</th><td>1</td></tr></table>`,
			want: "table caption=\"\"\n  row [corner] [1]\n",
		},
		{
			name: "a tfoot's rows are rows",
			src: `<table><tbody><tr><td>1</td></tr></tbody>
			  <tfoot><tr><td>total</td></tr></tfoot></table>`,
			want: "table caption=\"\"\n  row [1]\n  row [total]\n",
		},
		{
			name: "spans are kept",
			src:  `<table><tr><td colspan="2" rowspan="3">wide</td><td>x</td></tr></table>`,
			want: "table caption=\"\"\n  row [wide+2col+3row] [x]\n",
		},
		{
			name: "a span of one, of zero and of nonsense are all no span",
			src:  `<table><tr><td colspan="1">a</td><td colspan="0">b</td><td colspan="lots">c</td></tr></table>`,
			want: "table caption=\"\"\n  row [a] [b] [c]\n",
		},
		{
			name: "an empty cell holds its place",
			src:  `<table><tr><td>a</td><td></td><td>c</td></tr></table>`,
			want: "table caption=\"\"\n  row [a] [] [c]\n",
		},
		{
			name: "anything that is not a row or a section is ignored",
			src:  `<table><colgroup><col></colgroup><tr><td>a</td></tr></table>`,
			want: "table caption=\"\"\n  row [a]\n",
		},
		{
			name: "a row with no cells is not a row",
			src:  `<table><tr></tr><tr><td>a</td></tr></table>`,
			want: "table caption=\"\"\n  row [a]\n",
		},
		{
			// ⛔ The two things that really do appear inside a <tr> and are not
			// cells. Text and most elements are FOSTER-PARENTED out of a table
			// by the parser, so they never reach this walk — but a comment
			// stays where it was written, and <template> is allowed in a row.
			name: "a comment and a template inside a row are not cells",
			src:  `<table><tr><!-- note --><template><td>ignored</td></template><td>a</td></tr></table>`,
			want: "table caption=\"\"\n  row [a]\n",
		},
		{
			name: "a table with a caption and no rows is its caption",
			src:  `<table><caption>Just a caption</caption></table>`,
			want: "p Just a caption\n",
		},
		{
			name: "a table with nothing in it is nothing",
			src:  `<table></table><p>after</p>`,
			want: "p after\n",
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			if got := shape(parse(t, tc.src).Blocks, ""); got != tc.want {
				t.Errorf("shape =\n%s\nwant\n%s", got, tc.want)
			}
		})
	}
}

// ⛔ Asserted DIRECTLY, not through the shape helper: a helper that prints a
// header and a first row the same way cannot defend the difference, which is
// exactly how two mutations survived in go-richdoc/fb2.
func TestOnlyTheFirstRowOfAMultiRowTheadIsTheHeader(t *testing.T) {
	doc := parse(t, `<table><thead>
	  <tr><th>first</th></tr>
	  <tr><th>repeated partway down</th></tr>
	</thead><tbody><tr><td>data</td></tr></tbody></table>`)
	tbl, ok := doc.Blocks[0].(richdoc.Table)
	if !ok {
		t.Fatalf("Blocks[0] = %T, want a Table", doc.Blocks[0])
	}
	if len(tbl.Header) != 1 || text(tbl.Header[0].Inlines) != "first" {
		t.Errorf("Header = %v, want the FIRST thead row", cells(tbl.Header))
	}
	if len(tbl.Rows) != 2 {
		t.Fatalf("Rows = %v, want the second thead row kept as a row", tbl.Rows)
	}
	if got := text(tbl.Rows[0][0].Inlines); got != "repeated partway down" {
		t.Errorf("Rows[0] = %q, want the repeated heading as an ordinary row", got)
	}
}

// ⛔ richdoc.Cell's contract: a producer that sets Blocks MUST also set
// Inlines, and the two must say the same thing.
func TestACellWithBlockContentFillsBothFields(t *testing.T) {
	doc := parse(t, `<table><tr><td>
	  <p>first paragraph</p>
	  <ul><li>one</li><li>two</li></ul>
	</td></tr></table>`)
	tbl := doc.Blocks[0].(richdoc.Table)
	cell := tbl.Rows[0][0]
	if len(cell.Blocks) != 2 {
		t.Fatalf("Blocks = %d, want the paragraph and the list", len(cell.Blocks))
	}
	if len(cell.Inlines) == 0 {
		t.Fatal("Inlines is empty: a consumer written before Cell.Blocks existed would render an empty cell")
	}
	flat := text(cell.Inlines)
	for _, want := range []string{"first paragraph", "one", "two"} {
		if !strings.Contains(flat, want) {
			t.Errorf("the flattened cell does not say %q: %q", want, flat)
		}
	}
	// And Content() -- the one call that expresses the contract -- gives the
	// blocks back.
	if got := len(cell.Content()); got != 2 {
		t.Errorf("Content() = %d blocks, want 2", got)
	}
}

// Every block kind that can sit in a cell, flattened: this is the degraded
// rendering the contract asks for, and the kinds with no honest one line are
// left out rather than invented.
func TestFlattenCoversEveryBlockKind(t *testing.T) {
	for _, tc := range []struct{ name, src, want string }{
		{"two paragraphs", `<td><p>a</p><p>b</p></td>`, "a\\nb"},
		{"a heading", `<td><h3>title</h3><p>a</p></td>`, "title\\na"},
		{"a code block", `<td><pre>x := 1</pre><p>a</p></td>`, "`x := 1`\\na"},
		{"a quotation", `<td><blockquote><p>q</p></blockquote><p>a</p></td>`, "q\\na"},
		{"a list", `<td><ul><li>one</li><li>two</li></ul><p>a</p></td>`, "one\\ntwo\\na"},
		{"a nested table has no one-line rendering", `<td><table><tr><td>x</td></tr></table><p>a</p></td>`, "a"},
		{"a thematic break has none either", `<td><hr><p>a</p></td>`, "a"},
		{"an empty block contributes nothing", `<td><p></p><p>a</p></td>`, "a"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			doc := parse(t, "<table><tr>"+tc.src+"</tr></table>")
			tbl := doc.Blocks[0].(richdoc.Table)
			if got := text(tbl.Rows[0][0].Inlines); got != tc.want {
				t.Errorf("flattened = %q, want %q", got, tc.want)
			}
		})
	}
}

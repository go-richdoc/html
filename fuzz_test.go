// Copyright (c) the go-richdoc authors.
// SPDX-License-Identifier: BSD-3-Clause

package html

import (
	"fmt"
	"strings"
	"testing"

	"github.com/go-richdoc/richdoc"
)

// FuzzParse points this reader at bytes nobody wrote. It reads HTML, which
// arrives from a page, a clipboard or a mail part, and go-richdoc/epub hands
// it every chapter of every publication — so a tree shape that breaks this
// breaks that too, and HTML is plain text, which is the kind of input a
// fuzzer explores cheaply. This target does in minutes what the EPUB one
// cannot: that container refuses most mutations before any of its own code
// runs.
//
// Five properties:
//
//   - Parse does not panic.
//   - its two results are exclusive: a document OR an error, never neither.
//     A caller reads one of them, and "nil, nil" is the shape that gets
//     dereferenced.
//   - Parse is DETERMINISTIC. The reader keeps attributes in a map and
//     builds Meta from one; a single range reaching the output would make a
//     document depend on nothing the page said.
//   - the NODE limit holds, measured on the document rather than trusted.
//     MaxNodes exists because "a flat document with four million siblings is
//     another shape of abuse", and a count is the only thing that says so.
//   - the DEPTH limit holds. Limits.MaxDepth bounds this package's own walk,
//     because x/net/html builds its tree without recursion and would hand
//     back one a thousand elements deep; the returned tree must therefore
//     respect it, and that is an invariant about the OUTPUT, which no unit
//     case had asserted.
func FuzzParse(f *testing.F) {
	seeds := []string{
		"<html><body><p>hello</p></body></html>",
		"<p>one</p><p>two</p>",
		// Nesting, which is what MaxDepth is for, and a flat run, which is
		// what MaxNodes is for.
		strings.Repeat("<div>", 300) + "x" + strings.Repeat("</div>", 300),
		strings.Repeat("<p>x</p>", 500),
		// Tables, the construct with its own file here.
		"<table><tr><th>h</th></tr><tr><td>a</td><td rowspan=2>b</td></tr></table>",
		"<table><tbody><tr><td><table><tr><td>nested</td></tr></table></td></tr></tbody></table>",
		// Lists, quotes, headings, code.
		"<ul><li>a<ul><li>b</li></ul></li></ul><ol start=3><li>c</li></ol>",
		"<blockquote><p>q</p><footer>who</footer></blockquote><h1>t</h1><pre><code>x</code></pre>",
		// Inline markup, links, images, and an anchor.
		`<p><em>e</em><strong>s</strong><code>c</code><a href="/x">l</a><img src="i.png" alt="a"><a id="anchor"></a></p>`,
		// Meta, which becomes Document.Meta.
		`<html><head><title>T</title><meta name="author" content="A"></head><body><p>b</p></body></html>`,
		// Malformed, unclosed and misnested, which the HTML5 algorithm repairs
		// in ways no hand-written case would guess.
		"<b><i>x</b></i><table><td>stray<p>",
		"<!doctype html><html><body><p>a<div>b</p></div>",
		"<svg><foreignObject><p>in svg</p></foreignObject></svg>",
		"<!-- comment --><script>var x = '<p>not markup</p>'</script><style>p{}</style>",
		"",
	}
	for _, s := range seeds {
		f.Add([]byte(s))
	}

	f.Fuzz(func(t *testing.T, src []byte) {
		// Small limits on purpose: a bound is only tested near its edge.
		lim := Limits{MaxDepth: 32, MaxNodes: 2048}
		doc, err := Parse(src, lim)
		if err != nil {
			if doc != nil {
				t.Fatalf("both a document and an error: %v", err)
			}
			return
		}
		if doc == nil {
			t.Fatal("neither a document nor an error")
		}

		again, err2 := Parse(src, lim)
		if err2 != nil || again == nil {
			t.Fatalf("the second Parse disagreed with the first: doc=%v err=%v", again != nil, err2)
		}
		if first, second := fmt.Sprintf("%#v", doc), fmt.Sprintf("%#v", again); first != second {
			t.Fatalf("two parses of the same bytes differ\nfirst:\n%.400s\nsecond:\n%.400s", first, second)
		}

		if n := fuzzCountNodes(doc.Blocks); n > lim.MaxNodes {
			t.Errorf("the document holds %d nodes, over the MaxNodes of %d", n, lim.MaxNodes)
		}
		if d := fuzzTreeDepth(doc.Blocks); d > lim.MaxDepth {
			t.Errorf("the document nests %d deep, over the MaxDepth of %d", d, lim.MaxDepth)
		}
	})
}

func fuzzCountNodes(bs []richdoc.Block) int {
	n := 0
	for _, b := range bs {
		n++
		for _, child := range fuzzChildBlocks(b) {
			n += fuzzCountNodes(child)
		}
		n += len(fuzzInlinesOf(b))
	}
	return n
}

func fuzzTreeDepth(bs []richdoc.Block) int {
	deepest := 0
	for _, b := range bs {
		for _, child := range fuzzChildBlocks(b) {
			if d := fuzzTreeDepth(child); d > deepest {
				deepest = d
			}
		}
	}
	if len(bs) == 0 {
		return 0
	}
	return deepest + 1
}

// fuzzChildBlocks is every block-holding slice a block owns, which is the only
// thing the two walks above need from richdoc's shapes.
func fuzzChildBlocks(b richdoc.Block) [][]richdoc.Block {
	switch v := b.(type) {
	case richdoc.BlockQuote:
		return [][]richdoc.Block{v.Blocks}
	case richdoc.List:
		out := make([][]richdoc.Block, 0, len(v.Items))
		for _, it := range v.Items {
			out = append(out, it.Blocks)
		}
		return out
	case richdoc.Table:
		var out [][]richdoc.Block
		for _, row := range append([][]richdoc.Cell{v.Header}, v.Rows...) {
			for _, c := range row {
				if len(c.Blocks) > 0 {
					out = append(out, c.Blocks)
				}
			}
		}
		return out
	}
	return nil
}

func fuzzInlinesOf(b richdoc.Block) []richdoc.Inline {
	switch v := b.(type) {
	case richdoc.Paragraph:
		return v.Inlines
	case richdoc.Heading:
		return v.Inlines
	}
	return nil
}

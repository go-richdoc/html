# html

**HTML and XHTML into [richdoc](https://github.com/go-richdoc/richdoc).**
Pure Go, CGO-free, including `GOOS=js`.

```go
doc, err := html.Parse(src, html.Limits{})
```

This is the piece the ebook formats were missing. EPUB's content documents are
XHTML, MOBI's text is HTML, and the body of nearly every HTML email is HTML —
one reader serves all three, which is the whole argument for having a document
model at all.

## The parse is delegated, as it should be

To [`golang.org/x/net/html`](https://pkg.go.dev/golang.org/x/net/html), the
reference Go implementation of the HTML5 parsing algorithm — exactly as
[`markdown`](https://github.com/go-richdoc/markdown) delegates to goldmark and
[`rst`](https://github.com/go-richdoc/rst) to go-docutils. Nothing here
hand-rolls a tokenizer.

[go-webengine/engine](https://github.com/go-webengine/engine) builds its own
DOM from the same package, and that DOM **stays there**: it exists to carry CSS
and layout, which a document model has no use for. Checked before writing a
line of this, because the house rule is to extract from the engine rather than
to duplicate it — here there was nothing to extract, only a parser both of us
should be using.

⛔ **Parsing XHTML with the HTML parser is also the safe choice**, not a
shortcut. An XML parser reads a DOCTYPE, and a DOCTYPE is where entity
expansion and external entity references live. The HTML5 algorithm has a fixed
entity table and no DTD at all, so an EPUB cannot ask this reader to fetch a
file or to expand a billion laughs.

## What it maps onto

| | |
|---|---|
| `h1`…`h6` | a heading, with the `id` attribute as its anchor |
| `p` | a paragraph |
| `ul`, `ol`, `li` | a list; `ol`'s `start` is kept, and **tight or loose is read from the markup** — a list whose items wrap their text in `<p>` is the loose one |
| `dl`, `dt`, `dd` | a paragraph in bold, then a block quotation |
| `pre` | a code block, language from `class="language-x"`, `lang-x` or `highlight-x` |
| `blockquote` | a block quotation |
| `table` | a table: `caption`, one header row, `colspan` and `rowspan` |
| `hr` | a thematic break |
| `img` | an image |
| `a` | a link, or an **anchor** when it has an `id`/`name` and no `href` |
| `em`, `i`, `strong`, `b` | emphasis and strong |
| `del`, `s`, `strike` | strikethrough |
| `code`, `kbd`, `samp`, `tt` | inline code |
| `br` | a line break |
| `svg`, `math` | kept **verbatim** as raw content, with the format named |
| `div`, `section`, `article`, `figure`, … | transparent: the children are the content |

## ⛔ What it drops, which is the interesting part

**`script`, `style`, `noscript`, `template` and the head's own contents are
dropped completely.** They are the reason naive converters emit a page of CSS
declarations as the document's first paragraph: a stylesheet is text in the
tree like any other text, and it is not content. The test for this reads a
**real published page** — 30 898 bytes of minified Hugo output — and fails if
the word `prefers-color-scheme` or `addEventListener` reaches the document.

**Whitespace is collapsed the way HTML collapses it.** Any run of spaces, tabs
and newlines becomes one space; a block's leading and trailing space goes.

⛔ A text node of nothing but whitespace is **one space, not nothing**. The
space between `</b>` and `<i>` in `<b>a</b> <i>b</i>` is exactly such a node,
and returning nothing for it is how a reader turns two words into one. This
file's own comment claimed the defect was avoided while the code had it; the
first test run said otherwise.

⛔ **And that defect had a second half, which lived here much longer.** A text
node that *begins* with whitespace and then has content — the ` two` after
`</b>` in `<b>one</b> two` — had its leading space dropped, because the
collapser only emitted a space once it had already written something. So
`<b>one</b> two <b>three</b> four` came out as **`onetwo threefour`**: every
paragraph with an `<i>`, a `<b>`, a `<span>` or a link lost a space at each of
those boundaries, under [`epub`](https://github.com/go-richdoc/epub),
[`email`](https://github.com/go-richdoc/email) and
[`mobi`](https://github.com/go-richdoc/mobi) alike.

Nothing failed. The words were all there and two of them were stuck together,
which no block count and no substring test can see. What saw it was a **second
rendering of the same text**: Project Gutenberg's MOBI of *Pride and Prejudice*
against Gutenberg's own plain text of the same book — 125 816 words against
126 630, and 86.3% of its long sentences found verbatim. One line later:
**126 512 words, and 97.9%** (4 125 of 4 215). The table in
`TestWhitespaceIsCollapsedAndNotSwallowed` now has a case on *each* side of a
marking, because every case it had put the space in a node of its own.

**What richdoc has no word for is kept where keeping it is honest.** `svg` and
`math` become `RawBlock`/`RawInline` with the format named. `sub`, `sup`,
`mark` and `span` keep their text and lose their marking — losing the 2 of
`H2O` to say "there was a subscript here" is the worse trade.

## ⛔ The limits are the security surface

```go
html.Limits{MaxDepth: 256, MaxNodes: 1 << 20} // the defaults
```

Two shapes of abuse, two numbers. A document a thousand elements deep is one;
a flat document with four million empty spans is the other, and `MaxDepth`
says nothing about it.

The depth guard is **where our own recursion is**, which is the opposite of the
rule for `encoding/xml`: `x/net/html` builds its tree without recursing, so a
deep document costs it nothing and costs this package a stack. (In
[go-odf/odf](https://github.com/go-odf/odf) the decoder was the one that
recursed, and a guard in the caller never ran.)

Thirteen pathological documents are in the suite — unclosed tags fifty deep,
tags closed in the wrong order, forty nested tables, a comment that never
closes, a null byte, `&#x110000;` — and each must come back as a document or
as a refusal that names this package, never as a panic.

## How it is checked

- **100% of statements, no uncovered block**, race detector on, `gofmt` and
  `go vet` clean, and it builds for ten targets including `js/wasm`.
- ⛔ Three branches were **removed rather than covered**, and the reason is in
  the code beside each: the parser's own guarantees made them unreachable — a
  bare `<tr>` never arrives without its implied `tbody`, `xhtml.Parse` cannot
  fail on a `bytes.Reader`, and an element always serialises to at least its
  own tags. A branch no input can reach is a branch no test can defend.
- A **real published page** as a fixture, not only hand-written snippets:
  go-pdfkit's table detector passed every fixture and then found 588 tables in
  one real PDF.
- **A fuzz target** (`FuzzParse`, v0.1.1), which this package earns more than
  most: it reads HTML, HTML is plain text, and a fuzzer explores plain text
  cheaply — 1.5M executions produced **490 interesting inputs**, against 32 for
  the same budget on `go-richdoc/epub`'s checksummed container. Since that
  package hands this one every chapter of every publication, a tree shape that
  breaks this breaks that too.
  It asserts no panic, a document OR an error and never neither, determinism —
  the reader keeps attributes in a map, and Go randomises map iteration on
  purpose — and **both limits measured on the OUTPUT**: the node count, and the
  nesting DEPTH, which is an invariant about the returned tree that no unit case
  had stated. Nothing found.

## The rest of the family

[**richdoc**](https://github.com/go-richdoc/richdoc) is the model ·
[**markdown**](https://github.com/go-richdoc/markdown) ·
[**rst**](https://github.com/go-richdoc/rst) ·
[**latex**](https://github.com/go-richdoc/latex) ·
[**fb2**](https://github.com/go-richdoc/fb2) ·
[**data**](https://github.com/go-richdoc/data)

[**go-pdfkit**](https://github.com/go-pdfkit) composes a richdoc document into
a PDF, which is what makes HTML → PDF a two-step with no format-specific code
in between.

## Licence

BSD-3-Clause.

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
html.Limits{
    MaxDepth:          256,       // our walk of the tree
    MaxNodes:          1 << 20,   // our walk of the tree
    MaxInputBytes:     64 << 20,  // the source, before it is parsed  (v0.2.0)
    MaxSourceElements: 1 << 20,   // the source, before it is parsed  (v0.2.0)
}
```

Two shapes of abuse, two numbers — then a third thing the first two cannot see.
A document a thousand elements deep is one shape; a flat document with four
million empty spans is the other, and `MaxDepth` says nothing about it.

The depth guard is **where our own recursion is**, which is the opposite of the
rule for `encoding/xml`: `x/net/html` builds its tree without recursing, so a
deep document costs it nothing and costs this package a stack. (In
[go-odf/odf](https://github.com/go-odf/odf) the decoder was the one that
recursed, and a guard in the caller never ran.)

### ⛔ Those two bound the walk, and the walk is not where the memory goes

`x/net/html` has built the **entire tree** before `conv.spend` is called once,
and it has no ceiling of its own to fall back on: its only limit is
`Tokenizer.SetMaxBuf`, which `html.Parse` does not set (x/net **v0.62.0**, the
version this module pins and the version these numbers mean: `token.go:149` and
`:1278`; `parse.go` and `node.go` hold no depth or node bound at all).

The control is two documents refused by the **same ceiling at the same
element**, the second eight times the source of the first — so nothing after
that byte should have cost anything:

| | small | 8× the source | ratio |
| --- | --- | --- | --- |
| `MaxNodes` alone | 12 063 584 | 96 063 568 | **8.0** |
| `MaxSourceElements` (v0.2.0) | 7 096 | **7 096** | **1.0** |

Refusing a hostile 3.2 MB document went from **96 MB** to **7 096 bytes**, and
the figure no longer moves with the file's size at all. One byte of source buys
**56.3** bytes of heap at the cheapest shape, 16.5 on a page of prose — so
before `MaxInputBytes` there was no bound on memory whatsoever.

⛔ **`MaxSourceElements` is its own ceiling and not a proxy for `MaxNodes`**, and
it cannot be derived from one: tags in `<head>` are never walked, so a document
can hold a million of them and reach `MaxNodes` at nearly zero. A source-side
count is therefore no lower bound on what the walk will count — and a guard that
refuses what the thing it protects would accept is worse than no guard. A test
asserts both answers to that document, because both are right: they answer two
questions.

⛔ **It counts with the tokenizer, not with a scan for `<`.** A scan counts the
`<` inside an attribute value, inside a comment and inside a `<script>`, and the
three disagree with the parser in different directions; a guard built on one
would refuse documents over their punctuation. Four such documents are in the
suite.

### ⛔ A bomb test must not be able to take the machine with it

The expensive refusal above holds about **114 MB** from the operating system, and
it runs on every CI job of every target. It now runs under a **256 MiB** heap cap
(`debug.SetMemoryLimit`, which is `GOMEMLIMIT` by another name), chosen as 2.2×
the measured figure, so a regression fails here rather than on somebody's
machine.

The shape comes from an incident in a sibling project: a test handed an
unclamped viewBox to a rasterizer, asked for about **40 GB**, and OOM-killed the
linux/amd64 CI runner — while darwin **overcommitted lazily and hid it** on the
machine it was written on. A ten-target cross-build compiles everywhere and
*runs* the tests on one target, so that did not catch it either.

⛔ **`ulimit -v` is a no-op on darwin** (RLIMIT_AS is ignored), so the shell half
of that guard protects CI and nothing locally — backwards from where a bomb gets
written. What works in both places is Go's own soft limit plus a figure read back
from `runtime.MemStats`. And the soft limit is a **GC target, not an allocation
refusal**: it holds against incremental growth (a million small nodes, which is
every bomb here) and does *nothing* against one enormous request, which only
RLIMIT_AS stops.

⛔ **And a gate that protects by avoidance hides what it avoids**, so the cap is
the *weaker* of two assertions. Under a soft limit a reader whose ceiling
regressed does not reserve more memory — it makes the collector work harder to
stay inside the limit, so the failure arrives as a test that takes minutes, and
a slow test reads as a slow machine rather than as a broken bound.

What *names* the regression is what the call asked the allocator for:
`TotalAlloc` is cumulative, so no amount of thrashing hides it, and its delta is
order-independent. Per case, measured:

| | asks for | ceiling | |
| --- | --- | --- | --- |
| the walk ceiling alone | 96 063 696 | 192 MiB | a sanity bound on the test's own input |
| the source ceiling | **7 096** | **1 MiB** | tight: a four-order jump if the pre-scan stops refusing first |

⛔ Two more things the tests say out loud, because both were wrong first:

- **A cap nobody has seen trip is not a cap.** A separate test allocates 64 MiB
  live under an 8 MiB cap and asserts the helper reports past it — without that,
  the cap on the real bombs is a claim that the comparison works, not evidence.
- **The figure is absolute, not a delta.** `Sys` never shrinks, so
  `after - before` reports what a call *added* to whatever an earlier test grew:
  the same bomb measured **94 736 400** bytes run first and **40 386 560** run
  second. Same work, two numbers, decided by test order. A cap is about what the
  process holds, so it is compared against what the process holds — while the
  per-call comparison between readers stays in the file that measures
  `TotalAlloc`, which is order-independent.

### The defaults come from 123 369 real files

Measured with `htmlcorpus/htmlsizeprobe --reader`, which asks **this reader**
for its verdict on every file rather than trusting a percentile:

| | p50 | p90 | p99 | p99.9 | max |
| --- | --- | --- | --- | --- | --- |
| bytes | 6 824 | 63 322 | 179 074 | 1 765 776 | **19 873 471** |
| elements | 91 | 1 218 | 3 812 | 9 794 | **98 961** |
| tokenizer depth | 7 | 15 | 33 | 329 | **3 140** |

**Every one of the 123 369 is accepted — 100.0000%.** The biggest is Chromium's
licence page at 19.87 MB, the widest is the ODF 1.3 specification at 98 961
elements, and the deepest is GNU's `bash` manual.

⛔ That last column is why the probe asks the reader. It tokenizes **3 140**
levels deep against a `MaxDepth` of **256** — so the percentile says 0.1% of
real documents are past the default, and the truth is **zero**: unclosed tags
make the tokenizer's nesting climb where the HTML5 algorithm closes them. A
default "corrected" from that column would have loosened a ceiling that was
never tight. The probe's own documentation carries the 12× gap.

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

// Package html reads HTML and XHTML into the richdoc document model.
//
// It is the piece the ebook formats were missing: EPUB's content documents are
// XHTML, MOBI's text is HTML, and the body of nearly every HTML email is HTML.
// One reader serves all three, which is the whole argument for a document model
// — see [github.com/go-richdoc/richdoc].
//
// # The parse is delegated
//
// To golang.org/x/net/html, the reference Go implementation of the HTML5
// parsing algorithm, exactly as [github.com/go-richdoc/markdown] delegates to
// goldmark and [github.com/go-richdoc/rst] to go-docutils. Nothing here
// hand-rolls a tokenizer. go-webengine/engine builds its own DOM from the same
// package, and its DOM stays there: that tree exists to carry CSS and layout,
// which a document model has no use for.
//
// ⛔ Parsing XHTML with the HTML parser is also the SAFE choice, not a
// shortcut. An XML parser reads a DOCTYPE, and a DOCTYPE is where entity
// expansion and external entity references live; the HTML5 algorithm has a
// fixed entity table and no DTD at all, so an EPUB cannot ask this reader to
// fetch a file or to expand a billion laughs.
//
// # What it maps onto
//
//	h1..h6                a heading, with the id attribute as its anchor
//	p                     a paragraph
//	ul, ol, li            a list; ol's start attribute is kept
//	dl, dt, dd            a paragraph in bold, then a block quotation
//	pre                   a code block, language from class="language-x"
//	blockquote            a block quotation
//	table                 a table: caption, header row, colspan and rowspan
//	hr                    a thematic break
//	img                   an image
//	a                     a link, or an anchor when it has an id and no href
//	em, i, strong, b      emphasis and strong
//	del, s, strike        strikethrough
//	code, kbd, samp, tt   inline code
//	br                    a line break
//	svg, math             kept VERBATIM as raw content, with its format named
//
// # ⛔ What it drops, and why that is the interesting part
//
// script, style, noscript, template and the head's own contents are dropped
// COMPLETELY. They are the reason naive converters emit a page of CSS
// declarations as the document's first paragraph: a stylesheet is text in the
// tree and is not content. Comments go the same way.
//
// Whitespace is collapsed the way HTML collapses it — any run of spaces, tabs
// and newlines becomes one space, and a block's leading and trailing space is
// dropped — except inside pre, where it is kept byte for byte.
//
// What richdoc has no word for is kept where keeping it is honest: svg and
// math become [github.com/go-richdoc/richdoc.RawBlock] or RawInline with the
// format named, so a writer that knows them can render them and one that does
// not can say so. sub, sup, mark and span keep their text and lose their
// marking, because losing the 2 of H2O to say "there was a subscript here"
// is the worse trade.
//
// Nothing here is CGO, and it builds for GOOS=js.
package html

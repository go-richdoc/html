// Copyright (c) the go-richdoc authors.
// SPDX-License-Identifier: BSD-3-Clause

package html

import (
	"strings"
	"testing"
)

// TestTheInputCeilingRefusesAndNamesItself.
func TestTheInputCeilingRefusesAndNamesItself(t *testing.T) {
	src := []byte(strings.Repeat("x", 101))
	_, err := Parse(src, Limits{MaxInputBytes: 100})
	if err == nil {
		t.Fatal("101 bytes against a ceiling of 100 was accepted")
	}
	if !strings.Contains(err.Error(), "101 bytes of source") ||
		!strings.Contains(err.Error(), "limit is 100") {
		t.Errorf("the error does not say what was measured against what: %v", err)
	}
	// ⛔ The control: exactly at the ceiling is inside it. An off-by-one here
	// refuses a document of precisely the permitted size, and nothing else in
	// the suite would notice.
	if _, err := Parse([]byte(strings.Repeat("x", 100)), Limits{MaxInputBytes: 100}); err != nil {
		t.Errorf("100 bytes against a ceiling of 100: %v", err)
	}
}

// TestTheSourceElementCeilingRefusesBeforeTheTree.
func TestTheSourceElementCeilingRefusesBeforeTheTree(t *testing.T) {
	// ⛔ Five <br> and a ceiling of four. <br> is counted and opens no level,
	// which is the case a depth-based guard would miss entirely.
	_, err := Parse([]byte(strings.Repeat("<br>", 5)), Limits{MaxSourceElements: 4})
	if err == nil {
		t.Fatal("five elements against a ceiling of four was accepted")
	}
	if !strings.Contains(err.Error(), "more than 4 elements in the source") {
		t.Errorf("the error does not name the source ceiling: %v", err)
	}
	if _, err := Parse([]byte(strings.Repeat("<br>", 4)), Limits{MaxSourceElements: 4}); err != nil {
		t.Errorf("four elements against a ceiling of four: %v", err)
	}
}

// TestTheSourceCountIsTheTokenizersAndNotAScanForLessThan.
//
// ⛔ Each of these holds characters a scan for '<' would count as a tag and the
// parser does not read as markup. A guard built on such a scan would refuse
// documents over their punctuation.
func TestTheSourceCountIsTheTokenizersAndNotAScanForLessThan(t *testing.T) {
	for _, src := range []string{
		`<p title="a<b<c">x</p>`,     // two in an attribute value
		`<p>1 < 2 and 3 < 4</p>`,     // two in text
		"<!-- <div><div><div> -->",   // three in a comment
		"<script>if(a<b){}</script>", // one in a script
	} {
		// One real tag at most in each (the comment has none), so a ceiling of
		// two must accept every one of them.
		if _, err := Parse([]byte(src), Limits{MaxSourceElements: 2}); err != nil {
			t.Errorf("%s was refused over a '<' the parser never read as a tag: %v", src, err)
		}
	}
}

// TestTheSourceCeilingIsNotAProxyForMaxNodes, said out loud as a test because
// the comment on Limits.MaxSourceElements claims it and a claim in a comment is
// not checked.
//
// ⛔ A thousand tags in <head> reach the walk's node count at nearly nothing:
// the walk starts at <body>. So a source-side count is NO lower bound on what
// the walk will count, which is exactly why this ceiling is its own and why it
// must never be derived from MaxNodes.
func TestTheSourceCeilingIsNotAProxyForMaxNodes(t *testing.T) {
	head := "<html><head>" + strings.Repeat(`<meta name="a" content="b">`, 1_000) +
		"</head><body><p>one</p></body></html>"

	// The walk is happy with a tiny node ceiling: the body holds almost nothing.
	if _, err := Parse([]byte(head), Limits{MaxNodes: 8, MaxSourceElements: 1 << 20}); err != nil {
		t.Fatalf("a thousand <meta> and a three-node body, against MaxNodes 8: %v", err)
	}
	// And the source ceiling refuses the same document, because it is counting
	// something else. Both answers are right; they are answers to two questions.
	if _, err := Parse([]byte(head), Limits{MaxSourceElements: 100}); err == nil {
		t.Error("a thousand tags in the source passed a source ceiling of 100")
	}
}

// TestTheDefaultsLeaveTheRealPopulationAlone records what the corpus said, so a
// change to a default has to face the measurement rather than a memory of it.
//
// Measured 2026-10-31 with htmlcorpus/htmlsizeprobe --reader over 123 369 real
// HTML files: every one accepted, 100.0000%. The biggest was 19 873 471 bytes,
// the widest held 98 961 elements, and the deepest tokenized 3 140 levels —
// which the reader accepts, because the HTML5 algorithm closes the unclosed
// tags that make the tokenizer's nesting climb.
func TestTheDefaultsLeaveTheRealPopulationAlone(t *testing.T) {
	const (
		biggestRealFile  = 19_873_471
		widestRealFile   = 98_961
		deepestTokenized = 3_140
	)
	d := Limits{}.orDefaults()
	if d.MaxInputBytes < biggestRealFile {
		t.Errorf("MaxInputBytes %d is below the biggest real file measured, %d",
			d.MaxInputBytes, biggestRealFile)
	}
	if d.MaxSourceElements < widestRealFile {
		t.Errorf("MaxSourceElements %d is below the widest real file measured, %d",
			d.MaxSourceElements, widestRealFile)
	}
	// ⛔ NOT a check that MaxDepth exceeds 3 140. It does not, and it does not
	// need to: that figure is the TOKENIZER's nesting and the ceiling bounds
	// this package's walk of the parsed tree. The probe's own documentation
	// carries the 12x gap between them. Asserting the wrong one here would
	// force a default to a number nothing measured.
	if d.MaxDepth != defaultMaxDepth || d.MaxNodes != defaultMaxNodes {
		t.Errorf("the walk ceilings moved: depth %d, nodes %d", d.MaxDepth, d.MaxNodes)
	}
}

// TestAMalformedStreamIsNotTheCountersBusiness: html.Parse reads every input the
// HTML5 algorithm can recover, which is all of them, so the pre-count must not
// invent a syntax error.
func TestAMalformedStreamIsNotTheCountersBusiness(t *testing.T) {
	for _, src := range []string{
		"<p",           // a tag that never ends
		"<!--",         // a comment that never ends
		`<p title="`,   // an attribute that never ends
		"<script>a<b",  // raw text that never ends
		"\x00\xff\xfe", // bytes that are not text
		"",             // nothing at all
	} {
		if err := countSourceElements([]byte(src), 1<<20); err != nil {
			t.Errorf("%q: the counter refused it, and syntax is not its business: %v", src, err)
		}
		if _, err := Parse([]byte(src), Limits{}); err != nil {
			t.Errorf("%q: Parse refused an input the HTML5 algorithm recovers: %v", src, err)
		}
	}
}

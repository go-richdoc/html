// Copyright (c) the go-richdoc authors.
// SPDX-License-Identifier: BSD-3-Clause

// What a document costs before the ceilings can see it.
//
// ⛔ THE LIMITS IN Limits BOUND OUR WALK, AND THE COMMENT ON THEM SAYS SO. That
// is right about what it is about — a stack overflow comes from this package
// recursing, not from golang.org/x/net/html, which builds its tree without
// recursion. It is silent about ALLOCATION, and allocation is paid by
// xhtml.Parse before conv.spend is ever called once.
//
// golang.org/x/net/html v0.62.0 — the version this module pins, which is the
// version these numbers mean — has no ceiling of its own to fall back on. Its
// only limit is Tokenizer.SetMaxBuf, which html.Parse does not set (token.go:149
// and :1278; parse.go and node.go hold no depth or node bound at all).
//
// ⛔ The first run of this file was taken against v0.61.0, on a branch that had
// been squash-merged while main moved on to v0.62.0. Same finding, and
// SetMaxBuf had moved two lines. A citation is to a version or it is to nothing.
//
// ⛔ Every figure here is a count of BYTES from runtime.MemStats. Exact, and
// independent of what else the machine is doing — these were taken at a load
// average above 50. There is no timing claim in this file; a ratio of durations
// would measure the neighbours.
package html

import (
	"runtime"
	"strings"
	"testing"

	"github.com/go-richdoc/richdoc"
)

// allocatedBy is how many bytes one call asks the allocator for in total.
//
// ⛔ TotalAlloc, not HeapAlloc: a reader that asks for a gigabyte and frees all
// but a megabyte has still asked for a gigabyte, and that is the number a
// machine serving many such calls pays. And retained is SIGNED, because the
// heap can end LOWER than it started once a refusal frees the parser's tree —
// as a uint64 subtraction that reads as eighteen quintillion.
func allocatedBy(t *testing.T, f func() (*richdoc.Document, error)) (total uint64, retained int64, err error) {
	t.Helper()
	runtime.GC()
	var before, after runtime.MemStats
	runtime.ReadMemStats(&before)

	doc, err := f()

	runtime.ReadMemStats(&after)
	total = after.TotalAlloc - before.TotalAlloc
	runtime.GC()
	var held runtime.MemStats
	runtime.ReadMemStats(&held)
	retained = int64(held.HeapAlloc) - int64(before.HeapAlloc)
	runtime.KeepAlive(doc)
	return total, retained, err
}

// flatNodes is n siblings of four bytes each: the cheapest node HTML has.
func flatNodes(n int) []byte {
	return []byte(strings.Repeat("<br>", n))
}

// nestedDivs is n levels of five bytes each on the way in.
func nestedDivs(n int) []byte {
	return []byte(strings.Repeat("<div>", n) + strings.Repeat("</div>", n))
}

// TestWhatOneByteOfHtmlBuys reports the amplification at the cheapest shapes.
func TestWhatOneByteOfHtmlBuys(t *testing.T) {
	const ceiling = 400 // bytes allocated per byte of source

	for _, c := range []struct {
		name string
		src  []byte
	}{
		{"100 000 flat nodes", flatNodes(100_000)},
		{"200 levels of div", nestedDivs(200)},
		{"a page of text", []byte(strings.Repeat("<p>one two three four five</p>", 20_000))},
	} {
		t.Run(c.name, func(t *testing.T) {
			total, retained, err := allocatedBy(t, func() (*richdoc.Document, error) {
				return Parse(c.src, Limits{})
			})
			if err != nil {
				t.Fatalf("%d bytes: %v", len(c.src), err)
			}
			perByte := float64(total) / float64(len(c.src))
			t.Logf("%d bytes of source -> %d allocated (%.1f per byte), %d retained",
				len(c.src), total, perByte, retained)
			if perByte > ceiling {
				t.Errorf("allocates %.1f bytes per byte of source, past the ceiling of %d",
					perByte, ceiling)
			}
		})
	}
}

// The experiment both tests below run: two documents refused by the SAME
// ceiling at the SAME item, the second eight times the source of the first. The
// reader was supposed to stop at the same item in both, so nothing after that
// byte should have cost anything. If allocation scales with the source instead,
// the ceiling is checked after the work it exists to prevent.
//
// ⛔ LOW, CALLER-CHOSEN CEILINGS, so it costs megabytes instead of gigabytes.
// The defaults would need a 64 MB document to exceed by 8x, and the shape of
// the answer does not depend on the constant.
const (
	expCeiling = 1_000
	expSmall   = 100_000
	expLarge   = 800_000
)

func ratioOverEightTimesTheSource(t *testing.T, lim func(int) Limits) float64 {
	t.Helper()
	cost := func(n int) uint64 {
		src := flatNodes(n)
		total, _, err := allocatedBy(t, func() (*richdoc.Document, error) {
			return Parse(src, lim(n))
		})
		if err == nil {
			t.Fatalf("%d elements against a ceiling of %d was accepted", n, expCeiling)
		}
		t.Logf("%d elements (%d bytes), refused: %d bytes allocated", n, len(src), total)
		return total
	}
	s, l := cost(expSmall), cost(expLarge)
	r := float64(l) / float64(s)
	t.Logf("same ceiling, 8x the source: %d -> %d bytes allocated, ratio %.1f", s, l, r)
	return r
}

// TestTheWalkCeilingAloneDoesNotBoundTheWork is why MaxSourceElements exists,
// kept as an assertion rather than a sentence in a comment.
//
// ⛔ It asserts the ratio is LARGE. That reads backwards until you see what it
// watches: golang.org/x/net/html building its whole tree before our first node
// is counted. If a future version of that package gains a ceiling of its own,
// this test fails — and that failure is the news that the source-side ceiling
// may no longer be needed. A dependency's absent bound is a fact about the
// dependency, and this is where we keep it.
func TestTheWalkCeilingAloneDoesNotBoundTheWork(t *testing.T) {
	r := ratioOverEightTimesTheSource(t, func(int) Limits {
		// Only the walk ceiling, and the source ceiling left wide open.
		return Limits{MaxNodes: expCeiling, MaxSourceElements: 1 << 30}
	})
	if r < 4 {
		t.Errorf("8x the source cost only %.1fx the allocation — x/net/html now bounds "+
			"something it did not, and MaxSourceElements may be removable", r)
	}
}

// TestTheSourceCeilingBoundsTheWork is the fix's witness: refusing before the
// tree is built costs the same whatever the file's size.
func TestTheSourceCeilingBoundsTheWork(t *testing.T) {
	r := ratioOverEightTimesTheSource(t, func(int) Limits {
		return Limits{MaxSourceElements: expCeiling}
	})
	if r > 2 {
		t.Errorf("the source ceiling does not bound the work: 8x the source costs %.1fx "+
			"the allocation", r)
	}
}

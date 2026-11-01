// Copyright (c) the go-richdoc authors.
// SPDX-License-Identifier: BSD-3-Clause

// A bomb test must not be able to take the machine with it.
//
// ⛔ THE INCIDENT THIS SHAPE COMES FROM is in a sibling project: a test handed
// an unclamped viewBox to a rasterizer, asked for about 40 GB, and OOM-KILLED
// the linux/amd64 CI runner — while darwin/arm64 overcommitted lazily and hid
// it on the machine it was written on. A cross-build matrix compiles for ten
// targets and RUNS the tests on one, so it did not catch it either.
//
// ⛔ AND `ulimit -v` IS A NO-OP ON DARWIN: RLIMIT_AS is ignored there. So the
// shell half of the guard protects CI and nothing locally, which is precisely
// backwards from where a bomb gets written. What works in both places is Go's
// own soft limit, [debug.SetMemoryLimit] — GOMEMLIMIT by another name — plus a
// figure read back from [runtime.MemStats].
//
// ⛔ WHAT THE SOFT LIMIT DOES AND DOES NOT DO, said out loud so nobody trusts
// it further than it goes. It is a GC TARGET, not an allocation refusal:
//
//   - against INCREMENTAL growth — a million nodes, each small, which is every
//     bomb in this package — it holds, because the collector is told to work
//     harder as the live heap approaches it;
//   - against ONE enormous request — `make([]byte, 40e9)` — it does nothing at
//     all. Only RLIMIT_AS stops that, and RLIMIT_AS is the thing darwin
//     ignores. A test that can make one giant request needs a subprocess and a
//     Linux runner, and must say so.
//
// ⛔ THIS FILE AND bytestoheap_test.go MEASURE DIFFERENT THINGS ON PURPOSE, and
// confusing them is how a figure gets quoted for the wrong claim:
//
//   - there, TotalAlloc per call — what one conversion asked the allocator for,
//     order-independent, and therefore the figure to COMPARE two readers with
//     (12 MB against 7 096 bytes for the same document);
//   - here, Sys after the call — what the whole process holds, which is what an
//     OOM killer counts and therefore the figure to BOUND. It includes the
//     arena every earlier test grew, so it is a property of the test binary and
//     not of one call: a cap trips on whatever ballooned, which is the point.
package html

import (
	"runtime"
	"runtime/debug"
	"testing"
)

// underHeapCap runs f with the Go heap capped at limit bytes and restores the
// previous limit afterwards. It returns [runtime.MemStats.Sys] after f: how much
// the process holds from the operating system in total.
//
// ⛔ Sys and not HeapAlloc: HeapAlloc is what is live at the moment the test
// happens to look, and the question a cap answers is how much the process has
// ASKED THE MACHINE FOR — which is what an OOM killer counts. It is the nearest
// thing to peak RSS available without leaving the process.
//
// ⛔ AND IT IS THE ABSOLUTE FIGURE, NOT A DELTA, which the first draft got
// wrong in a way worth keeping written down. Sys never shrinks: the runtime
// does not return its arena. So `after.Sys - before.Sys` reports what this call
// ADDED to whatever an earlier test already grew, and the same bomb measured
// 94 736 400 bytes run first and 40 386 560 run second — the same work, two
// numbers, decided by test order. A cap is about what the process holds, so it
// is compared against what the process holds.
// It also returns what f asked the allocator for, which is the figure that
// NAMES a regression — see TestTheBombsRunUnderACapAndStayWellInsideIt.
func underHeapCap(t *testing.T, limit int64, f func()) (held, asked uint64) {
	t.Helper()

	// ⛔ The limit is process-wide, so it is restored even if f panics, and no
	// test that sets it may run in parallel with another. None here do.
	previous := debug.SetMemoryLimit(limit)
	t.Cleanup(func() { debug.SetMemoryLimit(previous) })

	runtime.GC()
	var before runtime.MemStats
	runtime.ReadMemStats(&before)

	f()

	var after runtime.MemStats
	runtime.ReadMemStats(&after)
	// ⛔ TotalAlloc's delta is sound where Sys's is not: it is a cumulative
	// counter of bytes handed out, so the difference is exactly what f asked
	// for, whatever ran before it.
	return after.Sys, after.TotalAlloc - before.TotalAlloc
}

// TestTheCapCanActuallyFire is the instrument's self-comparison, and it comes
// first because every figure below depends on it.
//
// ⛔ A CAP NOBODY HAS SEEN TRIP IS NOT A CAP. The assertion in the next test
// has never fired — the readers stay far inside it — so on its own it is a
// claim that the comparison works, not evidence. Here the work is a known
// allocation well past a deliberately small cap, and the helper has to report
// a peak above it. If this stops failing-by-design, the next test is decoration.
//
// ⛔ It allocates 64 MiB and keeps it live, which is what makes the peak real;
// the cap is 8 MiB, so this is also the only place in the package where the
// soft limit is deliberately exceeded, and it is exceeded by a FIXED amount
// that cannot grow with a future change.
func TestTheCapCanActuallyFire(t *testing.T) {
	const (
		tinyCap = 8 << 20
		live    = 64 << 20
	)
	var kept [][]byte
	peak, _ := underHeapCap(t, tinyCap, func() {
		for range 64 {
			kept = append(kept, make([]byte, 1<<20))
		}
	})
	runtime.KeepAlive(kept)

	if peak <= tinyCap {
		t.Errorf("the process holds %d bytes for %d bytes of live data under a %d-byte "+
			"cap: the comparison in TestTheBombsRunUnderACapAndStayWellInsideIt cannot fire",
			peak, live, tinyCap)
	}
	t.Logf("%d bytes of live data under a %d-byte cap: the process holds %d, so the cap trips",
		live, tinyCap, peak)
}

// TestTheBombsRunUnderACapAndStayWellInsideIt.
//
// ⛔ TWO ASSERTIONS, AND THE CAP IS THE WEAKER ONE. A gate that protects by
// AVOIDANCE hides what it avoids: under a soft limit a reader whose ceiling
// regressed does not reserve more memory, it makes the collector work harder to
// stay inside the limit — so the failure arrives as a test that takes minutes,
// and a slow test reads as a slow machine, not as a broken bound. The cap is
// the host's safety net and nothing else.
//
// What NAMES the regression is `asked`: TotalAlloc is a cumulative counter, so
// the collector cannot hold it down and no amount of thrashing hides it. Its
// ceiling per case is measured, and it fails immediately with both numbers in
// the message.
//
// ⛔ The per-case ceilings are the point. The walk ceiling legitimately asks for
// about 96 MB to refuse this document — that is the defect it characterises —
// so 192 MiB there is only a sanity bound on the test's own input. The source
// ceiling asks for 7 096 bytes, so 1 MiB there is TIGHT: if the pre-scan ever
// stops refusing before the tree is built, that case jumps four orders of
// magnitude and says so in one line.
func TestTheBombsRunUnderACapAndStayWellInsideIt(t *testing.T) {
	const cap256 = 256 << 20

	for _, c := range []struct {
		name     string
		lim      Limits
		src      []byte
		maxAsked uint64
	}{
		{
			// The expensive one: the walk ceiling alone, which lets
			// x/net/html build the whole tree first.
			"the walk ceiling alone, 800 000 elements",
			Limits{MaxNodes: expCeiling, MaxSourceElements: 1 << 30},
			flatNodes(expLarge),
			192 << 20, // it asks for ~96 MB; a sanity bound
		},
		{
			// And the cheap one, whose figure is the one worth watching.
			"the source ceiling, 800 000 elements",
			Limits{MaxSourceElements: expCeiling},
			flatNodes(expLarge),
			1 << 20, // it asks for ~7 KB; tight on purpose
		},
	} {
		t.Run(c.name, func(t *testing.T) {
			var err error
			peak, asked := underHeapCap(t, cap256, func() {
				_, err = Parse(c.src, c.lim)
			})

			// ⛔ A BOMB TEST ASSERTS A REFUSAL. Without this the test would
			// pass just as happily on a reader that accepted the document and
			// returned a gigabyte of nodes, which is the failure it exists to
			// catch.
			if err == nil {
				t.Fatalf("%d bytes was accepted", len(c.src))
			}
			t.Logf("%d bytes refused (%v); asked for %d bytes, process holds %d",
				len(c.src), err, asked, peak)

			if asked > c.maxAsked {
				t.Errorf("asked the allocator for %d bytes to refuse %d bytes of source, "+
					"past the %d-byte ceiling: the bound is no longer refusing before "+
					"the work", asked, len(c.src), c.maxAsked)
			}
			if peak > cap256 {
				t.Errorf("the process holds %d bytes, past the %d-byte cap",
					peak, cap256)
			}
		})
	}
}

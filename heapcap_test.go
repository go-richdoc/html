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
func underHeapCap(t *testing.T, limit int64, f func()) uint64 {
	t.Helper()

	// ⛔ The limit is process-wide, so it is restored even if f panics, and no
	// test that sets it may run in parallel with another. None here do.
	previous := debug.SetMemoryLimit(limit)
	t.Cleanup(func() { debug.SetMemoryLimit(previous) })

	runtime.GC()
	f()

	var after runtime.MemStats
	runtime.ReadMemStats(&after)
	return after.Sys
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
	var held [][]byte
	peak := underHeapCap(t, tinyCap, func() {
		for range 64 {
			held = append(held, make([]byte, 1<<20))
		}
	})
	runtime.KeepAlive(held)

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
// ⛔ The cap is 256 MiB, chosen from the measurement below and not from taste:
// the largest refusal in this package reserves about 100 MB from the operating
// system, so a cap two and a half times that leaves the legitimate case room
// and still stops a regression — eight times the source at the same ceiling —
// from growing without bound. It fails here instead of on somebody's machine.
func TestTheBombsRunUnderACapAndStayWellInsideIt(t *testing.T) {
	const cap256 = 256 << 20

	for _, c := range []struct {
		name string
		lim  Limits
		src  []byte
	}{
		{
			// The expensive one: the walk ceiling alone, which lets
			// x/net/html build the whole tree first.
			"the walk ceiling alone, 800 000 elements",
			Limits{MaxNodes: expCeiling, MaxSourceElements: 1 << 30},
			flatNodes(expLarge),
		},
		{
			// And the cheap one, for contrast in the same units.
			"the source ceiling, 800 000 elements",
			Limits{MaxSourceElements: expCeiling},
			flatNodes(expLarge),
		},
	} {
		t.Run(c.name, func(t *testing.T) {
			var err error
			peak := underHeapCap(t, cap256, func() {
				_, err = Parse(c.src, c.lim)
			})

			// ⛔ A BOMB TEST ASSERTS A REFUSAL. Without this the test would
			// pass just as happily on a reader that accepted the document and
			// returned a gigabyte of nodes, which is the failure it exists to
			// catch.
			if err == nil {
				t.Fatalf("%d bytes was accepted", len(c.src))
			}
			t.Logf("%d bytes refused (%v); the process holds %d bytes", len(c.src), err, peak)

			if peak > cap256 {
				t.Errorf("the process holds %d bytes, past the %d-byte cap",
					peak, cap256)
			}
		})
	}
}

// Copyright (c) the go-richdoc authors.
// SPDX-License-Identifier: BSD-3-Clause

// A bomb test must not be able to take the machine with it.
//
// ⛔ THE GUARDS LIVE IN [github.com/go-richdoc/richdoc/bombtest] NOW, not here.
// This file used to hold its own copy, and the copy is how the mistakes got
// made: a Sys delta that was order-dependent, a cap nobody had seen trip, a
// refusal assertion that was easy to leave out. Three of the four readers in
// this family needed the same guards, so the questions moved into a package
// that will not run a bomb which has not answered them — `Bound` has no
// defaults and `Run` refuses before the bomb allocates.
//
// What is left here is this package's own numbers: how much each refusal
// legitimately costs, which is the one thing the shared gate cannot know.
//
// ⛔ The incident the shape comes from is a sibling project's test that handed
// an unclamped viewBox to a rasterizer, asked for about 40 GB, and OOM-killed
// the linux/amd64 CI runner — while darwin overcommitted lazily and hid it on
// the machine it was written on. `ulimit -v` would have caught it there and is
// a NO-OP on darwin (RLIMIT_AS is ignored), which is why the bound is Go-side.
//
// ⛔ THIS FILE AND bytestoheap_test.go MEASURE DIFFERENT THINGS ON PURPOSE:
// there, TotalAlloc per call, order-independent, the figure to COMPARE two
// readers with (12 MB against 7 096 bytes for the same document); here, the
// bounds each refusal is held to. Quoting one for the other's claim is how a
// figure goes wrong.
package html

import (
	"testing"

	"github.com/go-richdoc/richdoc/bombtest"
)

// TestTheBombsStayInsideWhatTheyDeclare.
//
// ⛔ The ceilings are measured, not chosen. The walk ceiling legitimately asks
// for about 96 MB to refuse this document — that is the defect it
// characterises — so 192 MiB there is only a sanity bound on the test's own
// input. The source ceiling asks for 7 096 bytes, so 1 MiB there is TIGHT: if
// the pre-scan ever stops refusing before the tree is built, that case jumps
// four orders of magnitude and the gate says so with both numbers in it.
func TestTheBombsStayInsideWhatTheyDeclare(t *testing.T) {
	for _, c := range []struct {
		name  string
		lim   Limits
		src   []byte
		bound bombtest.Bound
	}{
		{
			// The expensive one: the walk ceiling alone, which lets
			// x/net/html build the whole tree first.
			"the walk ceiling alone, 800 000 elements",
			Limits{MaxNodes: expCeiling, MaxSourceElements: 1 << 30},
			flatNodes(expLarge),
			bombtest.Bound{
				Why:        "MaxNodes refuses a flat document, after x/net/html has built its tree",
				AsksAtMost: 192 << 20,
				HeldAtMost: 256 << 20,
			},
		},
		{
			// And the cheap one, whose figure is the one worth watching.
			"the source ceiling, 800 000 elements",
			Limits{MaxSourceElements: expCeiling},
			flatNodes(expLarge),
			bombtest.Bound{
				Why:        "MaxSourceElements refuses before x/net/html builds anything",
				AsksAtMost: 1 << 20,
				HeldAtMost: 256 << 20,
			},
		},
	} {
		t.Run(c.name, func(t *testing.T) {
			r := bombtest.Run(t, c.bound, func() error {
				_, err := Parse(c.src, c.lim)
				return err
			})
			t.Logf("%d bytes: %s", len(c.src), r)
		})
	}
}

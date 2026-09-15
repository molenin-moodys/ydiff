package ui

import "testing"

// TestBrowserScreen_DividerAt_WideTier verifies both dividers are found at
// their exact geometry (per the design's Technical Details table) in the
// three-column tier, and that everything else — content columns, border
// rows, out-of-bounds rows — misses.
func TestBrowserScreen_DividerAt_WideTier(t *testing.T) {
	b := browserScreen{width: 120, height: 40, widths: [3]int{15, 35, 50}}
	ph := b.paneContentHeight()

	available := max(b.width-6, 0)
	cells := distributeWidths(available, []int{15, 35, 50})
	div0Cols := [2]int{cells[0] + 1, cells[0] + 2}
	div1Cols := [2]int{cells[0] + cells[1] + 3, cells[0] + cells[1] + 4}

	for _, x := range div0Cols {
		if got := b.dividerAt(x, ph/2+1); got != 0 {
			t.Fatalf("dividerAt(%d, mid) = %d, want 0", x, got)
		}
	}
	for _, x := range div1Cols {
		if got := b.dividerAt(x, ph/2+1); got != 1 {
			t.Fatalf("dividerAt(%d, mid) = %d, want 1", x, got)
		}
	}

	// ordinary content column: misses.
	if got := b.dividerAt(div0Cols[0]-2, ph/2+1); got != -1 {
		t.Fatalf("dividerAt(content column) = %d, want -1", got)
	}

	// row bounds: divider spans [1, ph+2] inclusive; outside is a miss.
	if got := b.dividerAt(div0Cols[0], 0); got != -1 {
		t.Fatalf("dividerAt(row 0, path header) = %d, want -1", got)
	}
	if got := b.dividerAt(div0Cols[0], ph+3); got != -1 {
		t.Fatalf("dividerAt(row ph+3, past bottom border) = %d, want -1", got)
	}
	if got := b.dividerAt(div0Cols[0], 1); got != 0 {
		t.Fatalf("dividerAt(top border row) = %d, want 0", got)
	}
	if got := b.dividerAt(div0Cols[0], ph+2); got != 0 {
		t.Fatalf("dividerAt(bottom border row) = %d, want 0", got)
	}
}

// TestBrowserScreen_DividerAt_MediumTier verifies only divider 1 exists once
// the parent column is dropped.
func TestBrowserScreen_DividerAt_MediumTier(t *testing.T) {
	b := browserScreen{width: 70, height: 40, widths: [3]int{15, 35, 50}}
	ph := b.paneContentHeight()

	available := max(b.width-4, 0)
	cells := distributeWidths(available, []int{35, 50})
	divCols := [2]int{cells[0] + 1, cells[0] + 2}

	for _, x := range divCols {
		if got := b.dividerAt(x, ph/2+1); got != 1 {
			t.Fatalf("dividerAt(%d, mid) = %d, want 1", x, got)
		}
	}

	// no divider 0 exists in this tier.
	if got := b.dividerAt(0, ph/2+1); got != -1 {
		t.Fatalf("dividerAt(x=0) = %d, want -1 (no parent column in medium tier)", got)
	}
}

// TestBrowserScreen_DividerAt_NarrowTier verifies no divider exists below
// mediumTierWidth, where a single pane fills the whole screen.
func TestBrowserScreen_DividerAt_NarrowTier(t *testing.T) {
	b := browserScreen{width: 40, height: 40, widths: [3]int{15, 35, 50}}
	ph := b.paneContentHeight()
	for x := 0; x < 40; x++ {
		if got := b.dividerAt(x, ph/2+1); got != -1 {
			t.Fatalf("dividerAt(%d, mid) = %d, want -1 in narrow tier", x, got)
		}
	}
}

// TestBrowserScreen_ResizeDividerTo_WideTierDrag exercises a plain drag left
// and right on both wide-tier dividers and checks the exact resulting cell
// widths, plus that distributeWidths on the new widths reproduces those
// dragged-to cells — the property that makes cell counts usable directly as
// proportions.
func TestBrowserScreen_ResizeDividerTo_WideTierDrag(t *testing.T) {
	b := browserScreen{width: 120, height: 40, widths: [3]int{15, 35, 50}}
	available := max(b.width-6, 0) // 114

	// drag divider 0 (parent|current) right by 10 cells.
	origCells := distributeWidths(available, []int{15, 35, 50})
	newX := origCells[0] + 1 + 10 // leftEdge=0 for divider 0, +1 offset per the formula
	changed := b.resizeDividerTo(0, newX)
	if !changed {
		t.Fatal("resizeDividerTo(0, ...) reported no change on a real drag")
	}
	wantParent := origCells[0] + 10
	if b.widths[0] != wantParent {
		t.Fatalf("widths[0] = %d, want %d", b.widths[0], wantParent)
	}
	if b.widths[0]+b.widths[1] != origCells[0]+origCells[1] {
		t.Fatalf("pair total drifted: got %d, want %d", b.widths[0]+b.widths[1], origCells[0]+origCells[1])
	}
	gotCells := distributeWidths(available, []int{b.widths[0], b.widths[1], b.widths[2]})
	if gotCells[0] != b.widths[0] || gotCells[1] != b.widths[1] || gotCells[2] != b.widths[2] {
		t.Fatalf("distributeWidths(widths) = %v, want widths %v to round-trip exactly", gotCells, b.widths)
	}

	// drag divider 1 (current|changed) left by 10 cells from the freshly
	// dragged widths.
	b2 := browserScreen{width: 120, height: 40, widths: [3]int{15, 35, 50}}
	cells := distributeWidths(available, []int{15, 35, 50})
	leftEdge1 := cells[0] + 2
	newX2 := leftEdge1 + 1 + (cells[1] - 10)
	changed2 := b2.resizeDividerTo(1, newX2)
	if !changed2 {
		t.Fatal("resizeDividerTo(1, ...) reported no change on a real drag")
	}
	wantCurrent := cells[1] - 10
	if b2.widths[1] != wantCurrent {
		t.Fatalf("widths[1] = %d, want %d", b2.widths[1], wantCurrent)
	}
	if b2.widths[1]+b2.widths[2] != cells[1]+cells[2] {
		t.Fatalf("pair total drifted: got %d, want %d", b2.widths[1]+b2.widths[2], cells[1]+cells[2])
	}
}

// TestBrowserScreen_ResizeDividerTo_ClampsAtMinColumnWidth verifies a drag
// past either end of the pair is clamped to minColumnWidth rather than
// producing a degenerate or negative width.
func TestBrowserScreen_ResizeDividerTo_ClampsAtMinColumnWidth(t *testing.T) {
	b := browserScreen{width: 120, height: 40, widths: [3]int{15, 35, 50}}
	available := max(b.width-6, 0)
	cells := distributeWidths(available, []int{15, 35, 50})
	pairTotal := cells[0] + cells[1]

	// drag far to the left of the box (x <= leftEdge): clamps to minColumnWidth.
	if !b.resizeDividerTo(0, -1000) {
		t.Fatal("expected a change (clamped, not a no-op)")
	}
	if b.widths[0] != minColumnWidth {
		t.Fatalf("widths[0] = %d, want clamped to %d", b.widths[0], minColumnWidth)
	}
	if b.widths[1] != pairTotal-minColumnWidth {
		t.Fatalf("widths[1] = %d, want %d", b.widths[1], pairTotal-minColumnWidth)
	}

	// drag far to the right: clamps the other way.
	b2 := browserScreen{width: 120, height: 40, widths: [3]int{15, 35, 50}}
	if !b2.resizeDividerTo(0, 100000) {
		t.Fatal("expected a change (clamped, not a no-op)")
	}
	if b2.widths[1] != minColumnWidth {
		t.Fatalf("widths[1] = %d, want clamped to %d", b2.widths[1], minColumnWidth)
	}
	if b2.widths[0] != pairTotal-minColumnWidth {
		t.Fatalf("widths[0] = %d, want %d", b2.widths[0], pairTotal-minColumnWidth)
	}
}

// TestBrowserScreen_ResizeDividerTo_NoRoomIsNoOp verifies a divider whose
// pair does not have 2*minColumnWidth to give is rejected outright.
func TestBrowserScreen_ResizeDividerTo_NoRoomIsNoOp(t *testing.T) {
	// a very narrow wide-tier width (just over 100) leaves little room; force
	// a pair with less than 2*minColumnWidth by giving one side almost all
	// the proportion.
	b := browserScreen{width: 100, height: 40, widths: [3]int{1, 1, 1000}}
	available := max(b.width-6, 0)
	cells := distributeWidths(available, []int{1, 1, 1000})
	if cells[0]+cells[1] >= 2*minColumnWidth {
		t.Skipf("fixture no longer produces a starved pair: cells=%v", cells)
	}
	before := b.widths
	if b.resizeDividerTo(0, 50) {
		t.Fatal("resizeDividerTo reported a change with no room to give")
	}
	if b.widths != before {
		t.Fatalf("widths mutated despite reporting no change: %v -> %v", before, b.widths)
	}
}

// TestBrowserScreen_ResizeDividerTo_UnchangedIsFalse verifies a drag that
// lands back on the same cell widths reports no change. Widths are seeded
// already in cell-count units (summing exactly to available) so
// distributeWidths reproduces them with no rounding drift — otherwise even a
// same-position drag would appear to "change" the stored proportions from
// their original (possibly rounding-lossy) scale into exact cell counts.
func TestBrowserScreen_ResizeDividerTo_UnchangedIsFalse(t *testing.T) {
	b := browserScreen{width: 120, height: 40, widths: [3]int{17, 39, 58}} // sums to available (114)
	available := max(b.width-6, 0)
	cells := distributeWidths(available, []int{17, 39, 58})
	// x reproducing the exact current divider-0 position: leftEdge=0, so
	// newLeft = x - 1 must equal cells[0].
	x := cells[0] + 1
	if b.resizeDividerTo(0, x) {
		t.Fatalf("resizeDividerTo reported a change for a no-op drag: widths now %v", b.widths)
	}
}

// TestBrowserScreen_ResizeDividerTo_MediumTierPreservesParentShare verifies
// a medium-tier drag (only divider 1 exists) rescales the hidden parent
// column to keep its old proportion of the total, rather than leaving it
// stuck at its old absolute value.
func TestBrowserScreen_ResizeDividerTo_MediumTierPreservesParentShare(t *testing.T) {
	b := browserScreen{width: 70, height: 40, widths: [3]int{15, 35, 50}}
	available := max(b.width-4, 0)
	cells := distributeWidths(available, []int{35, 50})

	// drag divider 1 right by 5 cells.
	x := cells[0] + 1 + 5
	if !b.resizeDividerTo(1, x) {
		t.Fatal("expected a change on a real medium-tier drag")
	}
	wantCurrent := cells[0] + 5
	if b.widths[1] != wantCurrent {
		t.Fatalf("widths[1] = %d, want %d", b.widths[1], wantCurrent)
	}
	// parent share preserved: old parent(15) * new pair total / old pair total.
	oldPairTotal := 35 + 50
	newPairTotal := b.widths[1] + b.widths[2]
	wantParent := max(1, 15*newPairTotal/oldPairTotal)
	if b.widths[0] != wantParent {
		t.Fatalf("widths[0] = %d, want %d (parent share preserved)", b.widths[0], wantParent)
	}

	// zero-denominator guard: a pair with zero old proportion falls back to
	// the old parent value instead of dividing by zero.
	b2 := browserScreen{width: 70, height: 40, widths: [3]int{15, 0, 0}}
	cells2 := distributeWidths(max(b2.width-4, 0), []int{0, 0})
	x2 := cells2[0] + 1 + 3
	if !b2.resizeDividerTo(1, x2) {
		t.Fatal("expected a change even with a zero-proportion pair")
	}
	if b2.widths[0] != 15 {
		t.Fatalf("widths[0] = %d, want 15 (unchanged, zero-denominator guard)", b2.widths[0])
	}
}

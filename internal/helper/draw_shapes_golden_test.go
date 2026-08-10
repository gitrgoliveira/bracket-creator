package helper

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"slices"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// This file freezes the CURRENT pool-to-knockout draw behaviour (bc-draw
// Phase 1) into testdata/draw_shapes.json.
//
// IT PINS DEFECTS ON PURPOSE. Several captured values violate the rules the
// draw is meant to follow (see the per-field comments on drawShapeCase and the
// `_comment` block written into the golden file). Do NOT "fix" a value here:
// the golden is the diff instrument for the later phases, so a behaviour-
// preserving refactor must show a ZERO diff against it and the algorithm
// change that follows must show a reviewable one.
//
// Regeneration (same convention as the file itself documents):
//
//	UPDATE_GOLDEN=1 go test ./internal/helper/ -run TestDrawShapesGolden
//
// Regeneration is deterministic: no shuffling, no map iteration and no clock
// reaches the output, so two consecutive runs produce a byte-identical file.

// drawGoldenPoolSize is the pool size handed to CreatePools in "max" mode for
// every sweep case. Combined with drawGoldenRosterSize it yields pools of 4 and
// 3 players in the same competition.
const drawGoldenPoolSize = 4

// drawGoldenByeMarker is what a round-1 pairing prints for an empty leaf.
const drawGoldenByeMarker = "(bye)"

// The sweep required by bc-draw Phase 1. Nothing is skipped: a combination that
// errors records the error string as the case's value.
var (
	drawSweepPoolWinners = []int{1, 2, 3, 4}
	drawSweepPoolCounts  = []int{2, 3, 4, 5, 6, 7, 8, 9, 10, 11, 12}
	drawSweepCourts      = []int{1, 2, 4}
)

// drawShapesGolden is the whole golden file. Field order here is the field
// order in the JSON (encoding/json emits struct fields in declaration order and
// sorts map keys), which is what keeps the file stable and diff-friendly.
type drawShapesGolden struct {
	Comment []string                 `json:"_comment"`
	Sweep   drawShapesSweep          `json:"sweep"`
	Cases   map[string]drawShapeCase `json:"cases"`
}

type drawShapesSweep struct {
	PoolWinners []int  `json:"poolWinners"`
	PoolCounts  []int  `json:"poolCounts"`
	Courts      []int  `json:"courts"`
	KeyFormat   string `json:"keyFormat"`
}

// drawShapeCase is one (poolCount, poolWinners, courts) combination.
type drawShapeCase struct {
	// Error is set (and every other field left zero) when the combination
	// cannot be built at all. Recorded rather than skipped so the sweep's
	// coverage is visible in the file itself.
	Error string `json:"error,omitempty"`

	// RosterSize / PoolNames / PoolSizes describe the pools the draw is built
	// from. Sizes are deliberately mixed (see drawGoldenRosterSize): R6's
	// "oversized pool" bye criterion keys on them. Today they feed NOTHING -
	// the draw is derived from pool NAMES alone - so a size change must not
	// move any other field in this file.
	RosterSize int      `json:"rosterSize"`
	PoolNames  []string `json:"poolNames"`
	PoolSizes  []int    `json:"poolSizes"`

	// PoolToCourt is AssignPoolsToCourts' output: pool index -> court index.
	// Contiguous blocks, which is why GenerateFinals' adjacent-pool pairing
	// puts both sides of a round-1 match on the SAME shiaijo.
	PoolToCourt []int `json:"poolToCourt"`

	NumEntrants int `json:"numEntrants"`

	// Leaves is the leaf array after
	// GenerateFinals -> CreateBalancedTree -> ApplyPoolAdjustments -> TreeToLeafArray
	// (the engine's draw pipeline). "" is a structural bye slot.
	Leaves []string `json:"leaves"`

	// Round1 pairs Leaves[2i] against Leaves[2i+1]; byes print as
	// drawGoldenByeMarker so they are never silently absent from the diff.
	Round1 []string `json:"round1"`

	// Byes are the placeholders that receive a bye: the non-empty side of a
	// round-1 pair whose other side is empty, in leaf order.
	//
	// DEFECT PINNED: at 3+ qualifiers per pool these are frequently 2nd and
	// 3rd places while pool WINNERS play a round-1 match, which R6 forbids
	// (byes are meant to go to a region's home 1st places, seeded first). See
	// TestTreeAdjustmentByeAllocation for the named per-pool-count assertion.
	Byes []string `json:"byes"`

	// NumPages is TreePageLayout(numEntrants, courts, false) - always a power
	// of two. NumPagesRendered is len(SubdivideTree(tree, NumPages)), what the
	// workbook actually gets.
	//
	// DEFECT PINNED: the two disagree whenever the tree is shallower than the
	// requested page count, because SubdivideTree cannot split a tree it has
	// run out of levels for and falls back to appending the WHOLE TREE as an
	// extra page.
	NumPages         int `json:"numPages"`
	NumPagesRendered int `json:"numPagesRendered"`

	Pages []drawShapePage `json:"pages"`

	// PageCourtMismatchCount is the number of pages whose roster overlay
	// claims a different pool set than the page's bracket actually contains.
	// A scalar so the defect's scale is visible without reading the detail.
	PageCourtMismatchCount int `json:"pageCourtMismatchCount"`

	// PageCourtMismatch details ONLY the mismatching pages (every page's
	// claims and contents are in Pages regardless).
	//
	// DEFECT PINNED - this is the single most important field in the file.
	// Each tree page is titled "Shiaijo <label>" (SubtreeCourtIndex) and gets
	// a roster overlay for PoolBoundsForSubtree's pool slice, but the bracket
	// printed on it comes from SubdivideTree, which splits the draw by tree
	// position. Because a court's qualifiers are scattered across both halves
	// of the draw, the title and the roster describe one shiaijo while the
	// bracket shows another's competitors. Violates R3/R8.
	PageCourtMismatch []drawPageMismatch `json:"pageCourtMismatch"`
}

type drawShapePage struct {
	Page       int    `json:"page"`
	CourtLabel string `json:"courtLabel"`
	// PoolStart/PoolEnd are PoolBoundsForSubtree's [start, end) into PoolNames.
	PoolStart int `json:"poolStart"`
	PoolEnd   int `json:"poolEnd"`
	// ClaimedPools is the roster overlay printed on the page (PoolNames[start:end]).
	ClaimedPools []string `json:"claimedPools"`
	// PresentPools is the pools whose qualifiers actually appear in this
	// page's leaves, sorted and de-duplicated.
	PresentPools []string `json:"presentPools"`
	LeafCount    int      `json:"leafCount"`
}

// drawPageMismatch is one page whose title/roster overlay disagrees with the
// bracket printed on it. Summary restates both sides in a single line so the
// defect is legible without cross-referencing Pages (which holds the full
// claimed/present sets for EVERY page, mismatching or not).
type drawPageMismatch struct {
	Page       int    `json:"page"`
	CourtLabel string `json:"courtLabel"`
	// ClaimedButAbsent: overlaid on the page, no qualifier of theirs on it.
	ClaimedButAbsent []string `json:"claimedButAbsent"`
	// PresentButUnclaimed: competitors printed on a page titled with another
	// shiaijo, i.e. the operator-visible half of the defect.
	PresentButUnclaimed []string `json:"presentButUnclaimed"`
	Summary             string   `json:"summary"`
}

// drawGoldenRosterSize returns the synthetic roster size that makes
// CreatePools(roster, drawGoldenPoolSize, true) produce EXACTLY numPools pools
// with MIXED sizes: max(1, numPools-3) pools of 4 players and the rest of 3.
//
// Derivation: in "max" mode CreatePools makes ceil(roster/poolSize) pools and
// then levels them (base = roster/pools, the first `rem` pools get one extra).
// With poolSize 4, a roster of 3n+k gives exactly n pools - k of size 4 and n-k
// of size 3 - for any k in [max(1, n-3), n-1]. Taking the LOWER bound keeps the
// roster small while still guaranteeing at least one oversized pool and one
// smallest pool at every pool count in the sweep, which is the input R6's
// "oversized pool" bye criterion keys on.
//
// Note this makes poolWinners=4 degenerate for the 3-player pools (nobody can
// finish 4th). That is deliberate and harmless: the current pipeline builds the
// draw from pool NAMES only, so "Pool X-4th" placeholders are emitted for every
// pool regardless of its size and the captured shape is exactly what the code
// produces. Real competitions reject the configuration elsewhere.
func drawGoldenRosterSize(numPools int) int {
	extra := numPools - 3
	if extra < 1 {
		extra = 1
	}
	return 3*numPools + extra
}

// drawGoldenRoster builds the synthetic roster. Every player gets a UNIQUE
// dojo so CreatePools' dojo-conflict avoidance never fires and placement is a
// pure round-robin fill: no randomness anywhere in this file.
func drawGoldenRoster(numPools int) []Player {
	size := drawGoldenRosterSize(numPools)
	players := make([]Player, size)
	for i := range players {
		players[i] = Player{
			Name: fmt.Sprintf("P%03d", i+1),
			Dojo: fmt.Sprintf("Dojo %03d", i+1),
		}
	}
	return players
}

func drawShapeKey(numPools, poolWinners, courts int) string {
	return fmt.Sprintf("P%02d-W%d-C%d", numPools, poolWinners, courts)
}

// sortedUniquePoolNames extracts the pool name of every non-empty leaf value,
// de-duplicated and sorted, so page contents never depend on traversal order.
func sortedUniquePoolNames(leaves []string) []string {
	seen := map[string]bool{}
	out := []string{}
	for _, l := range leaves {
		if l == "" {
			continue
		}
		name := leafPool(l)
		if name == "" || seen[name] {
			continue
		}
		seen[name] = true
		out = append(out, name)
	}
	slices.Sort(out)
	return out
}

// missingFrom returns the members of want that are absent from have.
func missingFrom(want, have []string) []string {
	out := []string{}
	for _, w := range want {
		if !slices.Contains(have, w) {
			out = append(out, w)
		}
	}
	return out
}

// buildDrawShapeCase runs the real production pipeline for one combination.
// Everything it records comes from exported helper functions, so the golden
// tracks the shipped code rather than a re-implementation of it.
func buildDrawShapeCase(numPools, poolWinners, courts int) drawShapeCase {
	pools, err := CreatePools(drawGoldenRoster(numPools), drawGoldenPoolSize, true)
	if err != nil {
		return drawShapeCase{Error: "CreatePools: " + err.Error()}
	}
	if len(pools) != numPools {
		return drawShapeCase{Error: fmt.Sprintf("CreatePools produced %d pools, want %d", len(pools), numPools)}
	}

	c := drawShapeCase{RosterSize: drawGoldenRosterSize(numPools)}
	for _, p := range pools {
		c.PoolNames = append(c.PoolNames, p.PoolName)
		c.PoolSizes = append(c.PoolSizes, len(p.Players))
	}

	// NOTE: the engine does NOT call ReorderPoolsForCourts (bc-draw Phase 2a),
	// so the golden does not either. AssignPoolsToCourts therefore sees the
	// raw pool order, which is what makes the court blocks contiguous.
	assignments, err := AssignPoolsToCourts(numPools, courts)
	if err != nil {
		return drawShapeCase{Error: "AssignPoolsToCourts: " + err.Error()}
	}
	c.PoolToCourt = assignments

	// The live draw pipeline, verbatim (engine/knockout.go ResolveQualifiedPools
	// and engine/bracket.go both run exactly these four calls).
	finals := GenerateFinals(pools, poolWinners)
	tree := CreateBalancedTree(finals)
	ApplyPoolAdjustments(tree)
	c.Leaves = TreeToLeafArray(tree)
	c.NumEntrants = numPools * poolWinners

	c.Round1 = []string{}
	c.Byes = []string{}
	for i := 0; i+1 < len(c.Leaves); i += 2 {
		left, right := c.Leaves[i], c.Leaves[i+1]
		leftLabel, rightLabel := left, right
		if left == "" {
			leftLabel = drawGoldenByeMarker
		}
		if right == "" {
			rightLabel = drawGoldenByeMarker
		}
		c.Round1 = append(c.Round1, leftLabel+" vs "+rightLabel)
		switch {
		case left != "" && right == "":
			c.Byes = append(c.Byes, left)
		case left == "" && right != "":
			c.Byes = append(c.Byes, right)
		}
	}

	numPages, err := TreePageLayout(c.NumEntrants, courts, false)
	if err != nil {
		return drawShapeCase{Error: "TreePageLayout: " + err.Error()}
	}
	c.NumPages = numPages

	// RenderTreePages drives both court labelling and the roster overlay off
	// len(subtrees), NOT off the requested page count, so the golden does too.
	subtrees := SubdivideTree(tree, numPages)
	c.NumPagesRendered = len(subtrees)

	c.Pages = []drawShapePage{}
	c.PageCourtMismatch = []drawPageMismatch{}
	for i, subtree := range subtrees {
		label := CourtLabel(SubtreeCourtIndex(len(subtrees), courts, i))
		start, end := PoolBoundsForSubtree(numPools, courts, len(subtrees), i)
		claimed := append([]string{}, c.PoolNames[start:end]...)
		pageLeaves := collectOrderedLeaves(subtree)
		present := sortedUniquePoolNames(pageLeaves)

		c.Pages = append(c.Pages, drawShapePage{
			Page:         i + 1,
			CourtLabel:   label,
			PoolStart:    start,
			PoolEnd:      end,
			ClaimedPools: claimed,
			PresentPools: present,
			LeafCount:    len(pageLeaves),
		})

		absent := missingFrom(claimed, present)
		unclaimed := missingFrom(present, claimed)
		if len(absent) == 0 && len(unclaimed) == 0 {
			continue
		}
		c.PageCourtMismatch = append(c.PageCourtMismatch, drawPageMismatch{
			Page:                i + 1,
			CourtLabel:          label,
			ClaimedButAbsent:    absent,
			PresentButUnclaimed: unclaimed,
			Summary: fmt.Sprintf("page %d titled %q overlays %v but its bracket contains %v",
				i+1, "Shiaijo "+label, claimed, present),
		})
	}
	c.PageCourtMismatchCount = len(c.PageCourtMismatch)

	return c
}

func buildDrawShapesGolden() drawShapesGolden {
	g := drawShapesGolden{
		Comment: []string{
			"bc-draw Phase 1 characterization golden. Generated by",
			"internal/helper/draw_shapes_golden_test.go; regenerate with:",
			"",
			"    UPDATE_GOLDEN=1 go test ./internal/helper/ -run TestDrawShapesGolden",
			"",
			"THIS FILE PINS BEHAVIOUR WE BELIEVE IS WRONG. It is the diff",
			"instrument for the pool-to-knockout draw rewrite: the behaviour-",
			"preserving refactor phase must produce a ZERO diff against it, and",
			"the algorithm change after it must produce a reviewable one. Do not",
			"hand-edit a value to make it look correct.",
			"",
			"What each case captures, for one (pool count, qualifiers per pool,",
			"shiaijo count) combination, from the live pipeline",
			"GenerateFinals -> CreateBalancedTree -> ApplyPoolAdjustments ->",
			"TreeToLeafArray plus TreePageLayout/SubdivideTree page geometry.",
			"Pools come from helper.CreatePools over a synthetic roster with one",
			"dojo per player, sized so every case mixes 4-player and 3-player",
			"pools (see drawGoldenRosterSize).",
			"",
			"KNOWN DEFECTS PINNED HERE:",
			"",
			"1. pageCourtMismatch - a tree page is titled \"Shiaijo X\" and gets",
			"   the roster overlay for X's pools, but the bracket printed on it",
			"   is whatever SubdivideTree's positional split handed over. With 4",
			"   pools x 2 qualifiers x 2 courts, page 1 says Shiaijo A and",
			"   overlays Pool A and Pool B while its bracket holds Pool C-1st and",
			"   Pool D-2nd. Every court's qualifiers are scattered across both",
			"   halves of the draw, so no page can honestly claim one shiaijo.",
			"",
			"2. byes - at 3+ qualifiers per pool the bye repeatedly lands on a",
			"   2nd or 3rd place while pool WINNERS play a round-1 match. The",
			"   two-node local swap in treeAdjustment cannot see far enough to",
			"   place a bye correctly once more than two ranks are in play.",
			"",
			"3. numPages vs numPagesRendered - TreePageLayout only ever returns a",
			"   power of two, and SubdivideTree cannot honour a page count deeper",
			"   than the tree: it appends the WHOLE TREE as a trailing page, so a",
			"   small draw on 4 courts renders one page that duplicates the",
			"   entire bracket.",
			"",
			"4. round1 - poolToCourt shows contiguous court blocks and",
			"   GenerateFinals pairs each pool with its ADJACENT pool, so at 2",
			"   qualifiers both sides of a round-1 match are normally on the SAME",
			"   shiaijo instead of crossing to a partner court.",
			"",
			"Cases that cannot be built record `error` and nothing else; none do",
			"today, and a case gaining an error is itself a reportable change.",
		},
		Sweep: drawShapesSweep{
			PoolWinners: drawSweepPoolWinners,
			PoolCounts:  drawSweepPoolCounts,
			Courts:      drawSweepCourts,
			KeyFormat:   "P<poolCount, 2 digits>-W<qualifiers per pool>-C<courts>",
		},
		Cases: map[string]drawShapeCase{},
	}
	for _, numPools := range drawSweepPoolCounts {
		for _, poolWinners := range drawSweepPoolWinners {
			for _, courts := range drawSweepCourts {
				g.Cases[drawShapeKey(numPools, poolWinners, courts)] = buildDrawShapeCase(numPools, poolWinners, courts)
			}
		}
	}
	return g
}

func drawShapesGoldenPath() string {
	return filepath.Join("testdata", "draw_shapes.json")
}

func encodeDrawShapesGolden(t *testing.T, g drawShapesGolden) []byte {
	t.Helper()
	// MarshalIndent sorts map keys and emits struct fields in declaration
	// order, so the encoding is stable run to run and one changed leaf shows
	// up as a one-line diff.
	encoded, err := json.MarshalIndent(g, "", "  ")
	require.NoError(t, err)
	return append(encoded, '\n')
}

// TestDrawShapesGolden is the characterization gate for the pool-to-knockout
// draw. It fails naming the exact cases whose shape moved.
func TestDrawShapesGolden(t *testing.T) {
	got := buildDrawShapesGolden()
	encoded := encodeDrawShapesGolden(t, got)
	path := drawShapesGoldenPath()

	if os.Getenv("UPDATE_GOLDEN") != "" {
		require.NoError(t, os.MkdirAll("testdata", 0o750))
		require.NoError(t, os.WriteFile(path, encoded, 0o600))
		t.Logf("regenerated %s with %d cases", path, len(got.Cases))
		return
	}

	raw, err := os.ReadFile(path) // #nosec G304 -- fixed testdata path
	require.NoError(t, err, "golden file missing; regenerate with: UPDATE_GOLDEN=1 go test ./internal/helper/ -run TestDrawShapesGolden")
	if string(raw) == string(encoded) {
		return
	}

	var want drawShapesGolden
	require.NoError(t, json.Unmarshal(raw, &want), "golden file is not valid JSON")

	keys := map[string]bool{}
	for k := range want.Cases {
		keys[k] = true
	}
	for k := range got.Cases {
		keys[k] = true
	}
	ordered := make([]string, 0, len(keys))
	for k := range keys {
		ordered = append(ordered, k)
	}
	slices.Sort(ordered)

	var changed []string
	for _, k := range ordered {
		w, inWant := want.Cases[k]
		g, inGot := got.Cases[k]
		if inWant && inGot && assert.ObjectsAreEqual(w, g) {
			continue
		}
		changed = append(changed, k)
		// Print a readable field-level diff for the first few, then stop:
		// 132 cases of full-struct dumps is unreadable.
		if len(changed) <= 5 {
			switch {
			case !inWant:
				t.Errorf("case %s is NEW (not in the golden file)", k)
			case !inGot:
				t.Errorf("case %s DISAPPEARED (present in the golden file, no longer generated)", k)
			default:
				assert.Equal(t, w, g, "draw shape changed for case %s", k)
			}
		}
	}

	if len(changed) == 0 {
		// Byte difference with no case difference: formatting or the header
		// block moved. Still a change the golden must record.
		t.Errorf("golden %s differs byte-for-byte but every case matches (header/formatting change). Regenerate with: UPDATE_GOLDEN=1 go test ./internal/helper/ -run TestDrawShapesGolden", path)
		return
	}

	t.Errorf("THE DRAW CHANGED: %d of %d cases differ from %s: %v\n"+
		"If this is intentional, regenerate with: UPDATE_GOLDEN=1 go test ./internal/helper/ -run TestDrawShapesGolden\n"+
		"and review the resulting diff - it IS the behaviour change.",
		len(changed), len(ordered), path, changed)
}

// TestDrawShapesGoldenIsDeterministic proves the generator has no shuffle, map
// iteration or clock in it: two builds in the same process must encode
// identically. (The regeneration-twice check is a manual step; this makes the
// property a permanent gate.)
func TestDrawShapesGoldenIsDeterministic(t *testing.T) {
	first := encodeDrawShapesGolden(t, buildDrawShapesGolden())
	second := encodeDrawShapesGolden(t, buildDrawShapesGolden())
	assert.Equal(t, string(first), string(second), "draw shape generation is not deterministic")
}

// TestDrawGoldenRosterSizesAreMixed pins the roster-sizing formula's whole
// point: every sweep pool count must yield both oversized (4) and smallest (3)
// pools, because R6's second bye criterion keys on that difference. If this
// goes red the golden's poolSizes stopped exercising the criterion.
func TestDrawGoldenRosterSizesAreMixed(t *testing.T) {
	for _, numPools := range drawSweepPoolCounts {
		t.Run(fmt.Sprintf("%d_pools", numPools), func(t *testing.T) {
			pools, err := CreatePools(drawGoldenRoster(numPools), drawGoldenPoolSize, true)
			require.NoError(t, err)
			require.Len(t, pools, numPools, "roster formula must produce exactly the requested pool count")

			sizes := map[int]int{}
			for _, p := range pools {
				sizes[len(p.Players)]++
			}
			assert.Greater(t, sizes[4], 0, "expected at least one oversized (4-player) pool, got sizes %v", sizes)
			assert.Greater(t, sizes[3], 0, "expected at least one smallest (3-player) pool, got sizes %v", sizes)
			assert.Len(t, sizes, 2, "only sizes 3 and 4 should occur, got %v", sizes)
		})
	}
}

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

// This file captures the pool-to-knockout draw's shape into
// testdata/draw_shapes.json (bc-draw).
//
// It is the diff instrument for the draw. It used to pin DEFECTS on purpose,
// which is why the golden file's `_comment` block still names them: reading the
// two revisions side by side is how the rewrite is reviewed. What it records
// now is the shipped behaviour, so a diff here means the draw moved and the
// reason has to be stated. Do NOT hand-edit a value.
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

// The sweep required by bc-draw. Nothing is skipped: a combination that errors
// records the error string as the case's value.
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
	// "oversized pool" bye criterion keys on them, so a size change here CAN
	// move a bye (it could not before, when the draw was derived from pool
	// names alone).
	RosterSize int      `json:"rosterSize"`
	PoolNames  []string `json:"poolNames"`
	PoolSizes  []int    `json:"poolSizes"`

	// DrawCourts is the shiaijo count the draw actually used. It is the
	// requested count clamped by EffectiveDrawCourts, which never allocates
	// more courts than pools (a court with no pools would own an empty region)
	// and, when it does clamp, steps down to the largest POWER OF TWO that fits
	// (LargestShiaijoCountAtMost), never merely to an even count: 8 shiaijo over
	// 7 pools gives 4, not 6, because R9 rejects 6.
	DrawCourts int `json:"drawCourts"`

	// PoolToCourt is AssignPoolsToCourts' output over DrawCourts: pool index ->
	// court index. Contiguous blocks - this IS the R3 allocation, and each
	// block's pools own exactly one region of the bracket.
	PoolToCourt []int `json:"poolToCourt"`

	NumEntrants int `json:"numEntrants"`

	// Leaves is TreeToLeafArray over the court-first draw's root: the pow2 slot
	// array the engine's bracket is built from. "" is a structural bye slot.
	Leaves []string `json:"leaves"`

	// Round1 pairs Leaves[2i] against Leaves[2i+1]; byes print as
	// drawGoldenByeMarker so they are never silently absent from the diff.
	Round1 []string `json:"round1"`

	// Byes are the placeholders that receive a NAMED round-1 bye: the non-empty
	// side of a round-1 pair whose other side is empty, in leaf order. Under D4
	// a region of q occupants grants exactly q mod 2 of them, to its
	// highest-precedence occupant under R6 (seeded pools' winners, then
	// oversized pools' winners, then remaining winners, then crossed-in ranks).
	Byes []string `json:"byes"`

	// NumPages is what RenderKnockoutPages prints: DrawCourts x {1,2,4}.
	// NumPagesRendered is len(SubdivideRegions(...)), what the workbook
	// actually gets. R8 makes them equal in every case; a case where they
	// diverge is a bug, not a recorded quirk.
	NumPages         int `json:"numPages"`
	NumPagesRendered int `json:"numPagesRendered"`

	Pages []drawShapePage `json:"pages"`

	// PageCourtMismatchCount is the number of pages whose title disagrees with
	// the bracket printed on them, i.e. that print a pool WINNER belonging to
	// another shiaijo. R3/R8/R4a make this ZERO in every case; a non-zero value
	// is a regression, not a pinned defect.
	PageCourtMismatchCount int `json:"pageCourtMismatchCount"`

	// PageCourtMismatch details ONLY the mismatching pages (every page's
	// claims and contents are in Pages regardless).
	//
	// What counts as a mismatch is foreignHomeWinners: a pool WINNER printed on
	// a shiaijo other than the one its pool ran on, which R4a forbids outright.
	// Crossing is deliberately NOT one, because a page legitimately prints the
	// partner court's runners-up - that is what R4b routes there.
	//
	// It used to carry a second field, claimedButAbsent ("the page overlays a
	// pool's roster but holds no qualifier of that pool" - the old draw's
	// headline defect, a page titled "Shiaijo A" overlaying pools A and B while
	// its bracket printed Pool C-1st and Pool D-2nd). It was DROPPED because it
	// had become unfalsifiable: PageRosterPools now derives the claim BY
	// filtering the block down to the pools present on the page, so a claim this
	// file recomputes with it cannot name an absent pool whatever the renderer
	// does. The zero it reported was arithmetic, not evidence. The property it
	// stood for is now asserted where it can actually fail, against a rendered
	// workbook: TestTreePageHomePoolsAlwaysPresent
	// (draw_court_mapping_test.go). Dropping it changed no bytes in the golden,
	// because pageCourtMismatch is empty in every case.
	PageCourtMismatch []drawPageMismatch `json:"pageCourtMismatch"`
}

type drawShapePage struct {
	Page       int    `json:"page"`
	CourtLabel string `json:"courtLabel"`
	// PoolStart/PoolEnd are PoolBoundsForSubtree's [start, end) into PoolNames:
	// the whole pool block of the page's shiaijo.
	PoolStart int `json:"poolStart"`
	PoolEnd   int `json:"poolEnd"`
	// ClaimedPools is the roster overlay actually printed on the page, i.e. that
	// block narrowed by PageRosterPools to the pools the page prints.
	ClaimedPools []string `json:"claimedPools"`
	// PresentPools is the pools whose qualifiers actually appear in this
	// page's leaves, sorted and de-duplicated. It is normally a SUPERSET of
	// ClaimedPools: the page's own pools plus the partner court's pools whose
	// runners-up crossed in under R4b.
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
	// ForeignHomeWinners: pool winners printed on a shiaijo other than the one
	// their pool ran on (R4a). This is the genuine cross-check in this file -
	// the page's contents against AssignPoolsToCourts, two independent sources.
	ForeignHomeWinners []string `json:"foreignHomeWinners"`
	Summary            string   `json:"summary"`
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

	// The live draw pipeline, verbatim: engine/bracket.go
	// generatePoolPreviewBracket runs exactly this one call. (So did
	// ResolveQualifiedPools, until it stopped recomputing the placeholder
	// template on every pool completion and started reading the labels the draw
	// persisted on each match instead.)
	draw := BuildKnockoutDraw(pools, poolWinners, courts)
	if draw == nil {
		return drawShapeCase{Error: "BuildKnockoutDraw returned no draw"}
	}
	c.Leaves = TreeToLeafArray(draw.Root)
	c.NumEntrants = numPools * poolWinners
	c.DrawCourts = draw.NumCourts()

	// NOTE: the engine does NOT call ReorderPoolsForCourts (bc-draw Phase 2a),
	// so the golden does not either. AssignPoolsToCourts therefore sees the
	// raw pool order, which is what makes the court blocks contiguous. It is
	// read back with the draw's OWN court count, which is what the draw
	// allocated against.
	assignments, err := AssignPoolsToCourts(numPools, c.DrawCourts)
	if err != nil {
		return drawShapeCase{Error: "AssignPoolsToCourts: " + err.Error()}
	}
	c.PoolToCourt = assignments

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

	c.NumPages = len(KnockoutPageSubtrees(draw, false))

	// RenderTreePages drives both court labelling and the roster overlay off
	// len(subtrees), NOT off the requested page count, so the golden does too.
	// The two now always agree (R8), which is itself a recorded property.
	subtrees := SubdivideRegions(draw.Regions, KnockoutPagesPerCourt(draw.Regions))
	c.NumPagesRendered = len(subtrees)

	c.Pages = []drawShapePage{}
	c.PageCourtMismatch = []drawPageMismatch{}
	for i, subtree := range subtrees {
		label := CourtLabel(SubtreeCourtIndex(len(subtrees), c.DrawCourts, i))
		start, end := PoolBoundsForSubtree(numPools, c.DrawCourts, len(subtrees), i)
		// Exactly what RenderTreePages overlays: the page's shiaijo block,
		// narrowed to the pools it actually prints a qualifier of.
		claimed := []string{}
		for _, p := range PageRosterPools(pools[start:end], subtree) {
			claimed = append(claimed, p.PoolName)
		}
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

		// A pool WINNER on a page belonging to another shiaijo is an R4a
		// violation; a crossed-in runner-up is not, so only rank 1 counts.
		// This compares the page's LEAVES with AssignPoolsToCourts, which is why
		// it is the mismatch this file can still detect: neither side is derived
		// from the other. (The retired claimedButAbsent compared the overlay
		// helper's output with the input it filtered on - see drawPageMismatch.)
		foreign := []string{}
		for _, l := range pageLeaves {
			if leafRank(l) != 1 {
				continue
			}
			pi := slices.Index(c.PoolNames, leafPool(l))
			if pi >= 0 && assignments[pi] != SubtreeCourtIndex(len(subtrees), c.DrawCourts, i) {
				foreign = append(foreign, l)
			}
		}
		if len(foreign) == 0 {
			continue
		}
		c.PageCourtMismatch = append(c.PageCourtMismatch, drawPageMismatch{
			Page:               i + 1,
			CourtLabel:         label,
			ForeignHomeWinners: foreign,
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
			"bc-draw characterization golden for the pool-to-knockout draw.",
			"Generated by internal/helper/draw_shapes_golden_test.go; regenerate:",
			"",
			"    UPDATE_GOLDEN=1 go test ./internal/helper/ -run TestDrawShapesGolden",
			"",
			"It is the diff instrument for the draw: any change to placement, byes",
			"or pagination shows up here as a reviewable diff, and a change that",
			"was not intended is a regression. Do not hand-edit a value.",
			"",
			"What each case captures, for one (pool count, qualifiers per pool,",
			"shiaijo count) combination, from the live pipeline",
			"BuildKnockoutDraw -> TreeToLeafArray plus the TreePageLayout /",
			"SubdivideRegions page geometry. Pools come from helper.CreatePools",
			"over a synthetic roster with one dojo per player, sized so every case",
			"mixes 4-player and 3-player pools (see drawGoldenRosterSize).",
			"",
			"IT USED TO PIN FOUR DEFECTS. They are recorded here because the file",
			"is read side by side with its predecessor:",
			"",
			"1. pageCourtMismatch - a tree page was titled \"Shiaijo X\" and got the",
			"   roster overlay for X's pools, but the bracket printed on it was",
			"   whatever the positional split handed over. With 4 pools x 2",
			"   qualifiers x 2 shiaijo, page 1 said Shiaijo A and overlaid Pool A",
			"   and Pool B while its bracket held Pool C-1st and Pool D-2nd. The",
			"   draw is now built court-first, so a page IS one shiaijo's region",
			"   (R3/R8) and this field is empty in every case.",
			"",
			"2. byes - at 3+ qualifiers per pool a bye repeatedly landed on a 2nd or",
			"   3rd place while pool WINNERS played a round-1 match. Byes are now",
			"   allocated inside each region by R6 precedence: a seeded pool's",
			"   winner, then an oversized pool's winner (D1), then the remaining",
			"   home winners, and only then a crossed-in rank. A non-winner bye",
			"   survives in exactly one shape - a 2-pool, 3-qualifier draw, where a",
			"   region holds one home winner plus BOTH of the other pool's lower",
			"   qualifiers and the bye has to move to keep them out of a round-1",
			"   match against each other (R5 outranks R6).",
			"",
			"3. numPages vs numPagesRendered - the page count was a power of two",
			"   unrelated to the tree, and the splitter, out of levels, appended the",
			"   WHOLE TREE as a trailing page that reprinted the whole bracket. The",
			"   count is now shiaijo x {1,2,4} and the two always agree.",
			"",
			"4. round1 - contiguous court blocks plus adjacent-pool pairing meant",
			"   both sides of a 2-qualifier round-1 match were normally on the SAME",
			"   shiaijo. Every 2nd place now crosses to the partner court (R4b), so",
			"   a round-1 match pairs a home winner against a crossed-in runner-up.",
			"   Two crossed-in qualifiers of the same source court CAN still meet",
			"   when a region is short of home winners; that is R4f (structure beats",
			"   preference) and the reference draw does it too.",
			"",
			"Cases that cannot be built record `error` and nothing else; none do,",
			"and a case gaining an error is itself a reportable change.",
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

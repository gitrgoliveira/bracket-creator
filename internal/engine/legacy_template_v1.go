// Package engine — FROZEN v1 pool-to-knockout placement.
//
// # WHAT THIS FILE IS
//
// A byte-for-behaviour copy of the pool-to-knockout draw as it stood before the
// bc-draw Phase 4 rewrite ("v1"), reduced to the only thing the resolver needs
// from it: which pool-origin placeholder ("Pool A-1st", …) each knockout slot
// held at draw time.
//
// It exists for ONE reason. A mixed competition's knockout bracket is written at
// draw time with placeholder sides, and ResolveQualifiedPools overwrites those
// strings in place as each pool finishes, so a bracket drawn before this change
// no longer records which placeholder a slot started with. New brackets persist
// that on each match (state.BracketMatch.PlaceholderA/B/Winner). A bracket drawn
// BEFORE those fields existed has no record at all, and their absence is exactly
// how such a file is detected. This is the one-time reconstruction for it, after
// which the fields are backfilled and saved and this code is never consulted for
// that competition again.
//
// RULES
//
//  1. NEVER "refactor" this to call helper.GenerateFinals / CreateBalancedTree /
//     ApplyPoolAdjustments / TreeToLeafArray, or engine.buildBracketFromDraw.
//     Delegating to the live pipeline would make this file track the algorithm it
//     exists to be independent OF, and a legacy bracket would then be resolved
//     with the NEW placement, writing qualifiers into the wrong slots of a live
//     knockout, silently. That is the entire failure this file prevents.
//  2. The frozen algorithm below the "migration glue" section is not a library.
//     Live code reaches it through exactly two entry points, both in that
//     section, and nothing else here may be called, shared or "usefully" reused.
//     It is dead weight by design.
//  3. DELETE THIS FILE (and its test) one release after Phase 4 ships. By then
//     every bracket in the field has been drawn with, or backfilled to, persisted
//     placeholders. It is bounded on purpose: there is no v2 frozen copy, because
//     from Phase 4 onward the resolver never depends on the draw algorithm again.
//
// # FAITHFULNESS
//
// TestLegacyPlaceholderTemplateV1_MatchesLivePipeline pins this file's output
// against the LIVE pipeline over a matrix of pool/qualifier counts, proving the
// copy started out faithful. When Phase 4 changes the live pipeline that test is
// expected to be converted to a STATIC expectation (record today's values as
// literals) — NOT deleted. Deleting it would leave this file unverified for the
// release in which it still matters.
package engine

import (
	"fmt"
	"strconv"
	"strings"

	"github.com/gitrgoliveira/bracket-creator/internal/state"
)

// --- migration glue ---------------------------------------------------------
//
// Everything from here to the end of the "frozen copy" sections is the ONLY
// bridge between the frozen builder and live code, kept in this file so the
// whole migration is one deletable unit: when this file goes, the sole edit left
// elsewhere is dropping the two-line legacy branch in ResolveQualifiedPools.

// bracketHasDrawPlaceholders reports whether a bracket records its draw-time slot
// labels. The absence of them IS the version marker for a pre-Phase-4 file: there
// is no version field, and none is needed. Any single non-empty placeholder
// proves the bracket was written by a builder that records them; a pool-fed
// knockout always has at least one (its round-0 leaves are pool placeholders by
// construction), and a bracket with none is either legacy or not pool-fed at all
// (in which case there is nothing to resolve and the backfill produces nothing).
func bracketHasDrawPlaceholders(b *state.Bracket) bool {
	if b == nil {
		return false
	}
	has := func(m *state.BracketMatch) bool {
		return m.PlaceholderA != "" || m.PlaceholderB != "" || m.PlaceholderWinner != ""
	}
	for ri := range b.Rounds {
		for mi := range b.Rounds[ri] {
			if has(&b.Rounds[ri][mi]) {
				return true
			}
		}
	}
	return b.ThirdPlaceMatch != nil && has(b.ThirdPlaceMatch)
}

// backfillDrawPlaceholdersV1 reconstructs a legacy bracket's draw-time labels
// with the frozen v1 builder and writes them onto the matches. Reports whether
// anything was written, so the caller knows to persist a call that resolved
// nothing but still learned something.
//
// Position IS the mapping here — the one place it still has to be, because a
// legacy bracket kept no other link between a slot and its label. That is safe
// precisely because the template comes from the FROZEN v1 pipeline, the one that
// drew this bracket, rather than from whatever the live pipeline does now. It is
// also the last time position is used: from the backfill on, the label rides on
// the match.
//
// Geometry mismatches (hand-edited or truncated bracket.json) are skipped rather
// than guessed at, mirroring the bounds guards the positional resolver used.
func backfillDrawPlaceholdersV1(b *state.Bracket, poolNames []string, poolWinners int) bool {
	if b == nil {
		return false
	}
	tpl := legacyPlaceholderTemplateV1(poolNames, poolWinners)
	if len(tpl) == 0 {
		return false
	}
	wrote := false
	for ri := range b.Rounds {
		if ri >= len(tpl) {
			break
		}
		for mi := range b.Rounds[ri] {
			if mi >= len(tpl[ri]) {
				break
			}
			m := &b.Rounds[ri][mi]
			t := tpl[ri][mi]
			m.PlaceholderA = t.SideA
			m.PlaceholderB = t.SideB
			m.PlaceholderWinner = t.Winner
			if t.SideA != "" || t.SideB != "" || t.Winner != "" {
				wrote = true
			}
		}
	}
	return wrote
}

// v1PlaceholderMatch is one knockout slot's draw-time labels, in the same
// [round][match] geometry as state.Bracket.Rounds.
type v1PlaceholderMatch struct {
	SideA  string
	SideB  string
	Winner string
	// completed mirrors the v1 builder's auto-resolved-bye status. Internal to
	// the reconstruction (it gates winner propagation); the caller only reads
	// the three labels.
	completed bool
}

// legacyPlaceholderTemplateV1 reconstructs the draw-time placeholder labels for
// a mixed competition drawn under v1, given the pool names IN DRAW ORDER and the
// qualifiers-per-pool count. Returns rounds[roundIdx][matchIdx], aligned with
// state.Bracket.Rounds. Nil when there is nothing to place.
//
// Pool NAMES rather than helper.Pool: this file must not depend on a live type
// whose shape can move under it. Names in draw order are all v1 ever read.
//
// The bronze (3rd-place) match is deliberately absent: in v1 it is created after
// the bracket is built and its sides are filled from semifinal losers, so it
// never held a pool placeholder.
func legacyPlaceholderTemplateV1(poolNames []string, poolWinners int) [][]v1PlaceholderMatch {
	finals := v1GenerateFinals(poolNames, poolWinners)
	if len(finals) == 0 {
		return nil
	}
	tree := v1CreateBalancedTree(finals)
	v1ApplyPoolAdjustments(tree)
	return v1BuildRounds(v1TreeToLeafArray(tree))
}

// --- frozen copy of helper.GenerateFinals ----------------------------------

// v1GenerateFinals is helper.GenerateFinals: one full pass over the pools per
// rank-rotation round r, pool p contributing the finisher of rank (p+r)%poolWinners.
func v1GenerateFinals(poolNames []string, poolWinners int) []string {
	if poolWinners <= 0 || len(poolNames) == 0 {
		return nil
	}

	finalists := make([][]string, len(poolNames))
	for i := 0; i < len(poolNames); i++ {
		for j := 0; j < poolWinners; j++ {
			finalists[i] = append(finalists[i], fmt.Sprintf("%s-%s", poolNames[i], v1GetOrdinal(j+1)))
		}
	}

	matches := make([]string, 0, len(poolNames)*poolWinners)
	for r := 0; r < poolWinners; r++ {
		for p := 0; p < len(poolNames); p++ {
			pos := (p + r) % poolWinners
			matches = append(matches, finalists[p][pos])
		}
	}
	return matches
}

// v1GetOrdinal is helper.GetOrdinal.
func v1GetOrdinal(n int) string {
	if n <= 0 {
		return strconv.Itoa(n)
	}
	switch n % 100 {
	case 11, 12, 13:
		return strconv.Itoa(n) + "th"
	}
	switch n % 10 {
	case 1:
		return strconv.Itoa(n) + "st"
	case 2:
		return strconv.Itoa(n) + "nd"
	case 3:
		return strconv.Itoa(n) + "rd"
	default:
		return strconv.Itoa(n) + "th"
	}
}

// --- frozen copy of helper's tree build + pool placement --------------------

// v1Node is helper.Node reduced to the fields the placement pass reads.
type v1Node struct {
	leafNode bool
	leafVal  string
	left     *v1Node
	right    *v1Node
}

// v1CreateBalancedTree is helper.CreateBalancedTree.
func v1CreateBalancedTree(leafValues []string) *v1Node {
	if len(leafValues) == 0 {
		return nil
	}
	mid := len(leafValues) / 2
	node := &v1Node{}
	if len(leafValues) == 1 {
		node.leafVal = leafValues[0]
		node.leafNode = true
		return node
	}
	node.left = v1CreateBalancedTree(leafValues[:mid])
	node.right = v1CreateBalancedTree(leafValues[mid:])
	return node
}

// v1ApplyPoolAdjustments is helper.ApplyPoolAdjustments: a pre-order
// v1TreeAdjustment traversal over the WHOLE tree.
func v1ApplyPoolAdjustments(node *v1Node) {
	if node == nil || node.leafNode {
		return
	}
	v1TreeAdjustment(node)
	v1ApplyPoolAdjustments(node.left)
	v1ApplyPoolAdjustments(node.right)
}

// v1TreeAdjustment is helper.treeAdjustment: lift the better-placed finisher to
// the top of a pairing, and into the bye slot. The nil guards are defensive
// only; v1CreateBalancedTree gives every internal node both children.
func v1TreeAdjustment(node *v1Node) {
	if node.left == nil || node.right == nil {
		return
	}
	if node.left.leafNode && node.right.leafNode {
		leftPos := v1PoolRank(node.left.leafVal)
		rightPos := v1PoolRank(node.right.leafVal)
		if leftPos > rightPos {
			node.left, node.right = node.right, node.left
		}
	}
	if node.left.leafNode && !node.right.leafNode && node.right.left != nil {
		leftPos := v1PoolRank(node.left.leafVal)
		rightPos := v1PoolRank(node.right.left.leafVal)
		if leftPos > rightPos {
			node.left, node.right.left = node.right.left, node.left
		}
	}
}

// v1PoolRank is helper.splitPoolNameAndRank plus an ordinal parse: the numeric rank in
// a "Pool A-1st" label, 0 when there is none.
func v1PoolRank(val string) int64 {
	idx := strings.LastIndex(val, "-")
	if idx == -1 {
		return 0
	}
	s := val[idx+1:]
	if s == "" {
		return 0
	}
	if len(s) > 2 {
		s = s[:len(s)-2]
	}
	pos, _ := strconv.ParseInt(s, 10, 64)
	return pos
}

// v1TreeToLeafArray is helper.TreeToLeafArray: flatten to a power-of-two leaf
// array, padding each side of a junction so structural byes land where the
// tree's asymmetry puts them.
func v1TreeToLeafArray(node *v1Node) []string {
	if node == nil {
		return nil
	}
	if node.leafNode {
		return []string{node.leafVal}
	}
	left := v1TreeToLeafArray(node.left)
	right := v1TreeToLeafArray(node.right)
	target := v1NextPow2(max(len(left), len(right)))
	for len(left) < target {
		left = append(left, "")
	}
	for len(right) < target {
		right = append(right, "")
	}
	return append(left, right...)
}

// v1NextPow2 is helper.NextPow2.
func v1NextPow2(n int) int {
	if n <= 1 {
		return 1
	}
	p := 1
	for p < n {
		p <<= 1
	}
	return p
}

// --- frozen copy of the engine's leaves-to-rounds derivation ----------------

// v1WinnerOfFormat is engine.winnerOfFormat. Frozen with the rest: the label is
// how a slot names its feeder, and a resolver reading persisted placeholders
// must reproduce the exact string the old draw wrote.
const v1WinnerOfFormat = "Winner of r%d-m%d"

// v1BuildRounds is the side/winner half of buildBracketFromDraw: pair the pow2
// leaves into round 0, name each later round's sides after their feeders,
// auto-resolve byes, and propagate those bye winners upward.
//
// It reproduces sides and winners ONLY. Court assignment, IDs, scheduling,
// display metadata and match numbering are all irrelevant to "which placeholder
// owned this slot", and the one remaining step of the live builder — marking
// latent byes Completed — runs after all propagation and changes no label.
func v1BuildRounds(leaves []string) [][]v1PlaceholderMatch {
	pow2 := v1NextPow2(len(leaves))
	leafValues := make([]string, pow2)
	copy(leafValues, leaves)

	numRounds := 0
	for n := pow2; n > 1; n >>= 1 {
		numRounds++
	}
	if numRounds == 0 {
		return nil // a single leaf has no match
	}

	rounds := make([][]v1PlaceholderMatch, numRounds)
	for rIdx := 0; rIdx < numRounds; rIdx++ {
		count := pow2 >> (rIdx + 1)
		rounds[rIdx] = make([]v1PlaceholderMatch, count)
		for i := 0; i < count; i++ {
			m := &rounds[rIdx][i]
			if rIdx == 0 {
				m.SideA = leafValues[i*2]
				m.SideB = leafValues[i*2+1]
			} else {
				// Depth is 1-based from the final, matching parseWinnerOf.
				depth := numRounds + 1 - rIdx
				m.SideA = fmt.Sprintf(v1WinnerOfFormat, depth, i*2)
				m.SideB = fmt.Sprintf(v1WinnerOfFormat, depth, i*2+1)
			}
			switch {
			case m.SideA == "" && m.SideB != "":
				m.Winner = m.SideB
				m.completed = true
			case m.SideA != "" && m.SideB == "":
				m.Winner = m.SideA
				m.completed = true
			case m.SideA == "" && m.SideB == "":
				m.completed = true
			}
		}
	}

	for rIdx := 0; rIdx < len(rounds)-1; rIdx++ {
		for mIdx := range rounds[rIdx] {
			if rounds[rIdx][mIdx].completed {
				v1PropagateWinner(rounds, rIdx, mIdx)
			}
		}
	}
	return rounds
}

// v1PropagateWinner is engine.propagateBracketWinner reduced to label movement
// (the bronze branch is absent: no bronze exists while the bracket is built).
func v1PropagateWinner(rounds [][]v1PlaceholderMatch, rIdx, mIdx int) {
	if rIdx >= len(rounds)-1 {
		return
	}
	m := rounds[rIdx][mIdx]
	nextIdx := mIdx / 2
	next := &rounds[rIdx+1][nextIdx]

	if mIdx%2 == 0 {
		next.SideA = m.Winner
	} else {
		next.SideB = m.Winner
	}

	if strings.HasPrefix(next.SideA, "Winner of") {
		r, mm := v1ParseWinnerOf(next.SideA, len(rounds))
		if r >= 0 && r < len(rounds) && mm >= 0 && mm < len(rounds[r]) && rounds[r][mm].completed {
			next.SideA = rounds[r][mm].Winner
		}
	}
	if strings.HasPrefix(next.SideB, "Winner of") {
		r, mm := v1ParseWinnerOf(next.SideB, len(rounds))
		if r >= 0 && r < len(rounds) && mm >= 0 && mm < len(rounds[r]) && rounds[r][mm].completed {
			next.SideB = rounds[r][mm].Winner
		}
	}

	switch {
	case next.SideA != "" && next.SideB == "" && !strings.HasPrefix(next.SideA, "Winner of"):
		next.Winner = next.SideA
		next.completed = true
		v1PropagateWinner(rounds, rIdx+1, nextIdx)
	case next.SideA == "" && next.SideB != "" && !strings.HasPrefix(next.SideB, "Winner of"):
		next.Winner = next.SideB
		next.completed = true
		v1PropagateWinner(rounds, rIdx+1, nextIdx)
	case next.SideA == "" && next.SideB == "":
		next.completed = true
		v1PropagateWinner(rounds, rIdx+1, nextIdx)
	}
}

// v1ParseWinnerOf is engine.parseWinnerOf.
func v1ParseWinnerOf(s string, numRounds int) (int, int) {
	var depth, matchIdx int
	if _, err := fmt.Sscanf(s, v1WinnerOfFormat, &depth, &matchIdx); err != nil {
		return -1, -1
	}
	return numRounds - depth, matchIdx
}

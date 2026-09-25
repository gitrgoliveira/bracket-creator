package state

import (
	"errors"
	"fmt"
	"sort"
)

// BracketMatchLeafSlot is the LEFTMOST first-round slot that match (roundIdx,
// matchIdx) is rooted above, in a pow2-padded Bracket.Rounds: match (r, m)
// covers leaves [m*2^(r+1), (m+1)*2^(r+1)).
//
// Two things derive from it and they MUST agree: a match's court (the region
// owning that leaf, engine.buildBracketFromDraw) and the tie-break inside a
// DisplayRound when numbering matches (NumberMatches). They are the same
// quantity, so they are one function -- numbering a bout by one rule and
// placing it by another is precisely how the printed sheet's "Match 12" and the
// app's "Match 12" became different bouts.
func BracketMatchLeafSlot(roundIdx, matchIdx int) int {
	return matchIdx * (1 << (roundIdx + 1))
}

// numbered reports whether m is a real bout, one that carries a round and a
// match number: not a Hidden structural bye and not a both-sides-empty dead
// match. NumberMatches numbers exactly these, and RestampRoundsFromFeeders
// requires its walk to reach every one of them, so the two cannot disagree
// about which matches count.
func (m *BracketMatch) numbered() bool {
	return !m.Hidden && (m.SideA != "" || m.SideB != "")
}

// NumberMatches sets MatchNumber on every real (non-Hidden, non-empty) bracket
// match. It is the ONE body of the web API's numbering: engine.buildBracketFromDraw
// runs it when a draw is generated, and RestampRoundsFromFeeders runs it again
// when a stored bracket's rounds are brought up to date on load. The Excel
// renderer has a SEPARATE implementation, helper.AssignMatchNumbers, which
// operates on []*Node instead of *Bracket. The two are NOT a literally-shared
// function (the types differ), they are kept equal-by-contract so the on-screen
// "Match N" always equals the printed Excel "Match N".
//
// Ordering, CRITICAL for byes: the Excel sheet numbers via eliminationMatchRounds,
// which groups matches by DEPTH-FROM-ROOT (the unbalanced tree's deepest matches
// come first), NOT by raw Rounds index. With a non-power-of-two roster the
// pow2-padded Rounds order diverges from that depth grouping, so numbering in raw
// Rounds order drifts (e.g. 5 entrants: the lone deep first-round bout must be
// Match 1, not the shallow slot-0 bout). DisplayRound already encodes the Excel
// depth grouping (verified by TestBracketDisplayMetadata_MatchesExcelRounds), so
// matches are numbered by descending DisplayRound (deepest/earliest round first).
//
// The tie-break inside a DisplayRound is the match's LEFTMOST FIRST-ROUND SLOT,
// BracketMatchLeafSlot -- the same function engine.buildBracketFromDraw uses to
// find a match's court. It has to be, because Excel's TraverseRounds walks each
// depth level LEFT TO RIGHT across the whole tree, and one effective round can
// draw its matches from several pow2 rounds at once: a shallow region's first
// bout and a deep region's second bout share a DisplayRound while sitting in
// Rounds 0 and 1. Tie-breaking on the within-round position alone (the old rule)
// then interleaves them by an index that means different things in the two
// rounds, and the printed "Match 12" and the app's "Match 12" become different
// bouts. Measured on a pool-fed draw of 8 pools x 3 qualifiers: the sheet
// numbered Pool G-3rd v Pool H-3rd 12 while the app numbered the E-1st/A-2nd v
// F-1st/B-2nd bout 12 (bc-draw Phase 5). The leaf slot orders them the way the
// sheet prints them, because a node's slot range is contiguous and left-to-right
// IS increasing slot.
//
// Skip rule (matches the Excel nil-node skip): Hidden (structural-bye) matches and
// both-sides-empty dead matches are excluded and do not consume a number.
//
// The printed Excel sheet is authoritative. The contract is enforced by
// TestMatchNumberingParity_ExcelVsWeb (internal/engine,
// match_numbering_parity_test.go) for knockout brackets and by
// TestExcelWorkbookMatchesEngineBracket_Mixed (excel_draw_parity_test.go) for
// pool-fed ones, the latter by reading the numbers back out of a rendered
// workbook. If they ever diverge, fix THIS path to match the Excel one -- and the
// JS buildDisplayModel matchNumById ordering with it (web-mobile/js/bracket.jsx),
// which is the third implementation of this walk.
//
// Must run AFTER Hidden and DisplayRound are set: at generation by
// engine.computeBracketDisplayMetadata and engine.applySlotDisplayRounds, on
// load by RestampRoundsFromFeeders.
func (b *Bracket) NumberMatches() {
	type ref struct {
		m *BracketMatch
		// leafSlot is the first-round slot this match's subtree starts at.
		leafSlot int
	}
	var real []ref
	for ri := range b.Rounds {
		for mi := range b.Rounds[ri] {
			m := &b.Rounds[ri][mi]
			if !m.numbered() {
				continue
			}
			real = append(real, ref{m: m, leafSlot: BracketMatchLeafSlot(ri, mi)})
		}
	}
	// Descending DisplayRound (deepest/earliest round first), then left to right
	// across the whole tree, mirrors the Excel eliminationMatchRounds walk.
	sort.SliceStable(real, func(i, j int) bool {
		if real[i].m.DisplayRound != real[j].m.DisplayRound {
			return real[i].m.DisplayRound > real[j].m.DisplayRound
		}
		return real[i].leafSlot < real[j].leafSlot
	})
	for i, r := range real {
		r.m.MatchNumber = i + 1
	}
}

// ErrBracketNoFeeders reports a bracket whose matches carry no Feeders at all:
// one generated before the display metadata existed (mp-7f2w). Its rounds
// cannot be walked, and it also predates the round classification
// RestampRoundsFromFeeders corrects, so there is nothing to correct.
var ErrBracketNoFeeders = errors.New("bracket carries no feeder metadata")

// ErrBracketFeedersUnwalkable reports stored Feeders that do not form the tree
// generation writes, so no round can be derived from them with confidence.
var ErrBracketFeedersUnwalkable = errors.New("bracket feeders cannot be walked from the final")

// BracketRoundChange is one match whose stored round or match number
// RestampRoundsFromFeeders changed.
type BracketRoundChange struct {
	ID        string
	OldRound  int
	NewRound  int
	OldNumber int
	NewNumber int
}

// RestampRoundsFromFeeders recomputes every real match's DisplayRound from the
// bracket's own stored Feeders, renumbers the matches with NumberMatches, and
// returns what moved (nil when nothing did, so a second call is a no-op).
//
// Why it exists: DisplayRound and MatchNumber are stamped once, when the draw is
// generated, and nothing recomputed them afterwards. v2.0.0 and v2.1.0 put a
// pair drawn beside an empty pair one round early (P1 v P2 of a five-entrant
// knockout was a quarterfinal rather than a semifinal), so a bracket drawn by
// either release kept those rounds and the numbers ordered by them, while the
// Excel export rebuilds the tree, numbers it with the corrected walk, and lays
// scores and courts onto the printed numbers. Store.EnsureLegacyUpgraded calls
// this once per load so such a bracket agrees with the export again.
//
// The rule: a match's round is its distance from the final. The final (the sole
// match of the last round) is round 1 and every match named in a match's
// Feeders is one round deeper. Generation reaches the same rounds by another
// route, the draw tree's own walk (engine.applySlotDisplayRounds), and
// TestRestampRoundsFromFeeders_LeavesFreshBracketsUnchanged (internal/engine)
// pins that the two agree on freshly generated knockout and pool-fed brackets.
// Feeders and Hidden are written at generation and never rewritten when results
// resolve the sides, so a bracket in play walks exactly like a fresh one.
//
// Only matches the walk reaches move. Hidden matches keep their stored values,
// the bronze (ThirdPlaceMatch, DisplayRound -1) is neither read nor written, and
// pairings, results, courts and scheduled times are never touched.
//
// When the bracket cannot be walked safely it changes NOTHING, numbers included,
// and says why: ErrBracketNoFeeders when no match carries Feeders, and
// ErrBracketFeedersUnwalkable when the last round is not a single final, a
// feeder names a match that is missing, not real, or already reached, or a real
// match is never reached from the final.
func (b *Bracket) RestampRoundsFromFeeders() ([]BracketRoundChange, error) {
	if b == nil || len(b.Rounds) == 0 {
		return nil, nil
	}
	byID := make(map[string]*BracketMatch)
	hasFeeders := false
	for ri := range b.Rounds {
		for mi := range b.Rounds[ri] {
			m := &b.Rounds[ri][mi]
			if _, dup := byID[m.ID]; dup {
				return nil, fmt.Errorf("%w: match id %q appears twice", ErrBracketFeedersUnwalkable, m.ID)
			}
			byID[m.ID] = m
			if len(m.Feeders) > 0 {
				hasFeeders = true
			}
		}
	}
	if !hasFeeders {
		return nil, ErrBracketNoFeeders
	}
	last := b.Rounds[len(b.Rounds)-1]
	if len(last) != 1 {
		return nil, fmt.Errorf("%w: the last round holds %d matches, not one final", ErrBracketFeedersUnwalkable, len(last))
	}

	// Walk from the final, breadth first, recording each match's round before
	// anything is written, so a refusal leaves the bracket exactly as it was.
	// A final that is not a real bout (fewer than two entrants) roots no walk.
	round := make(map[string]int)
	if final := &last[0]; final.numbered() {
		round[final.ID] = 1
		queue := []*BracketMatch{final}
		for len(queue) > 0 {
			m := queue[0]
			queue = queue[1:]
			for _, fid := range m.Feeders {
				if fid == "" {
					continue // a seeded entrant or a bye: no match feeds this side
				}
				f, ok := byID[fid]
				if !ok {
					return nil, fmt.Errorf("%w: %s names feeder %q, which is not in the bracket", ErrBracketFeedersUnwalkable, m.ID, fid)
				}
				if !f.numbered() {
					return nil, fmt.Errorf("%w: %s names feeder %s, which is not a real match", ErrBracketFeedersUnwalkable, m.ID, fid)
				}
				if _, seen := round[fid]; seen {
					return nil, fmt.Errorf("%w: %s is reached twice", ErrBracketFeedersUnwalkable, fid)
				}
				round[fid] = round[m.ID] + 1
				queue = append(queue, f)
			}
		}
	}
	for ri := range b.Rounds {
		for mi := range b.Rounds[ri] {
			m := &b.Rounds[ri][mi]
			if _, reached := round[m.ID]; m.numbered() && !reached {
				return nil, fmt.Errorf("%w: %s is not reached from the final", ErrBracketFeedersUnwalkable, m.ID)
			}
		}
	}

	type stamp struct{ round, number int }
	before := make(map[string]stamp, len(byID))
	for id, m := range byID {
		before[id] = stamp{m.DisplayRound, m.MatchNumber}
	}
	for id, r := range round {
		byID[id].DisplayRound = r
	}
	b.NumberMatches()

	var changes []BracketRoundChange
	for ri := range b.Rounds {
		for mi := range b.Rounds[ri] {
			m := &b.Rounds[ri][mi]
			if old := before[m.ID]; old.round != m.DisplayRound || old.number != m.MatchNumber {
				changes = append(changes, BracketRoundChange{
					ID:        m.ID,
					OldRound:  old.round,
					NewRound:  m.DisplayRound,
					OldNumber: old.number,
					NewNumber: m.MatchNumber,
				})
			}
		}
	}
	return changes, nil
}

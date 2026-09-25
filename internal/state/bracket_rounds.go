package state

import (
	"errors"
	"fmt"
	"sort"
	"time"
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
// match. NumberMatches numbers exactly these, and StampRoundsFromFeeders
// requires its walk to reach every one of them, so the two cannot disagree
// about which matches count. At generation it equals !Hidden
// (engine.computeBracketDisplayMetadata hides every match missing a side), and
// in every supported flow it stays so, because no write after the draw
// empties a side of a match in Rounds: a result moves a name in, retracting it
// restores a "Winner of" placeholder, and only the bronze, outside Rounds, is
// ever blanked. The one exception is data no draw produces: a pool with fewer
// ranked finishers than the places it sends (hand-edited or imported files)
// has each missing place resolved to an empty side (engine.qualifierResolver).
// Should that empty both sides of a real match, the walk from the final can
// no longer pass through it, so StampRoundsFromFeeders refuses with
// ErrBracketFeedersUnwalkable and the load-time repair leaves the bracket as
// stored and logs why. The side check mirrors the Excel numbering's nil-node
// skip.
func (m *BracketMatch) numbered() bool {
	return !m.Hidden && (m.SideA != "" || m.SideB != "")
}

// NumberMatches sets MatchNumber on every real (non-Hidden, non-empty) bracket
// match. It is the ONE body of the web API's numbering, run by
// StampRoundsFromFeeders right after it stamps the rounds, both when
// engine.buildBracketFromDraw generates a draw and when
// RestampRoundsFromFeeders brings a stored bracket up to date on load. The Excel
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
// Must run AFTER Hidden (engine.computeBracketDisplayMetadata, at generation)
// and DisplayRound (StampRoundsFromFeeders) are set.
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
// one generated before the display metadata existed (mp-7f2w, v0.17.0). Its
// rounds cannot be walked, so RestampRoundsFromFeeders leaves it as stored.
var ErrBracketNoFeeders = errors.New("bracket carries no feeder metadata")

// ErrBracketFeedersUnwalkable reports stored Feeders that do not form the tree
// generation writes, so no round can be derived from them with confidence.
var ErrBracketFeedersUnwalkable = errors.New("bracket feeders cannot be walked from the final")

// ScheduledAtLayout is how a match's ScheduledAt reads: HH:MM, 24-hour. The
// engine's scheduler writes it in this layout (scheduleClockLayout).
const ScheduledAtLayout = "15:04"

// BracketRoundChange is one match whose stored round, match number or
// scheduled time RestampRoundsFromFeeders changed.
type BracketRoundChange struct {
	ID             string
	OldRound       int
	NewRound       int
	OldNumber      int
	NewNumber      int
	OldScheduledAt string
	NewScheduledAt string
}

// RestampRoundsFromFeeders brings a STORED bracket's DisplayRound and
// MatchNumber up to the rule generation applies, StampRoundsFromFeeders, puts
// an unstarted old bracket's times in match-number order, and returns what
// moved (nil when nothing did, so a second call is a no-op). It may also set
// TimesSettled with no match moving, which the caller must save too.
// Store.EnsureLegacyUpgraded calls it once per load.
//
// Why it exists: DisplayRound and MatchNumber are stamped once, when the draw
// is generated, and nothing recomputed them afterwards, while the Excel export
// rebuilds the tree, numbers it with today's walk, and lays scores and courts
// onto the printed numbers. So it recomputes any stored bracket whose rounds or
// numbers differ from the rule. In practice that is two kinds of bracket:
//
//   - drawn by v2.0.0 or v2.1.0, which put a pair drawn beside an empty pair
//     one round early (P1 v P2 of a five-entrant knockout was a quarterfinal
//     rather than a semifinal) and numbered by those rounds;
//   - drawn by an older release (Feeders are stored, and the rounds derived
//     from them, since v0.17.0), whose rounds are therefore already right
//     but which broke a tie inside a round by the match's position in its
//     own pow2 round rather than by its first-round slot
//     (BracketMatchLeafSlot, v2.0.0). Where one round draws its bouts from
//     two pow2 rounds, which takes byes, the two orders differ and the
//     bracket is renumbered (a nine-entrant v1.1.0 knockout swaps its
//     Matches 4 and 5).
//
// Times: every release before this one scheduled a court in storage order, not
// match-number order, so on a bracket with byes the court queue (ordered by
// time) could list Match 2 before Match 1, renumbered or not. This is repaired
// ONCE per bracket, on the first load of a bracket not yet TimesSettled (one
// written by an older release; this release's draw sets it). If nobody has
// started it (no real match started, scored or decided), a court whose times
// still rise in storage order has them handed out again in match-number
// order, the order a fresh draw is scheduled in; see
// scheduleNumberedInNumberOrder. If any real match has been touched the times
// stay as stored, and a court whose times do not rise in storage order (a
// time the operator moved) keeps them. Either way the bracket is then
// TimesSettled and its times are never looked at again, so a move the
// operator makes later, which may well leave a court rising in storage
// order, is not mistaken for the old scheduling.
//
// Only real matches move. Hidden matches keep their stored values, the bronze
// (ThirdPlaceMatch, DisplayRound -1) is neither read nor written, and
// pairings, results and courts are never touched.
//
// When the bracket cannot be walked safely it changes NOTHING, numbers and
// times included, and says why: ErrBracketNoFeeders when no match carries
// Feeders, and ErrBracketFeedersUnwalkable as StampRoundsFromFeeders
// describes.
func (b *Bracket) RestampRoundsFromFeeders() ([]BracketRoundChange, error) {
	if b == nil || len(b.Rounds) == 0 {
		return nil, nil
	}
	hasFeeders := false
	for ri := range b.Rounds {
		for mi := range b.Rounds[ri] {
			if len(b.Rounds[ri][mi].Feeders) > 0 {
				hasFeeders = true
			}
		}
	}
	if !hasFeeders {
		return nil, ErrBracketNoFeeders
	}

	type stamp struct {
		round, number int
		at            string
	}
	before := make(map[*BracketMatch]stamp)
	for ri := range b.Rounds {
		for mi := range b.Rounds[ri] {
			m := &b.Rounds[ri][mi]
			before[m] = stamp{m.DisplayRound, m.MatchNumber, m.ScheduledAt}
		}
	}
	if err := b.StampRoundsFromFeeders(); err != nil {
		return nil, err
	}
	if !b.TimesSettled {
		if !b.anyNumberedMatchTouched() {
			b.scheduleNumberedInNumberOrder()
		}
		b.TimesSettled = true
	}

	var changes []BracketRoundChange
	for ri := range b.Rounds {
		for mi := range b.Rounds[ri] {
			m := &b.Rounds[ri][mi]
			old := before[m]
			if old.round != m.DisplayRound || old.number != m.MatchNumber || old.at != m.ScheduledAt {
				changes = append(changes, BracketRoundChange{
					ID:             m.ID,
					OldRound:       old.round,
					NewRound:       m.DisplayRound,
					OldNumber:      old.number,
					NewNumber:      m.MatchNumber,
					OldScheduledAt: old.at,
					NewScheduledAt: m.ScheduledAt,
				})
			}
		}
	}
	return changes, nil
}

// StampRoundsFromFeeders is the ONE producer of DisplayRound and MatchNumber
// on a bracket's real matches: each one's round is its distance from the
// final along the stored Feeders (the final, the sole match of the last
// round, is round 1; every match named in a match's Feeders is one round
// deeper), and the matches are then numbered by NumberMatches.
// engine.buildBracketFromDraw calls it once Hidden and Feeders are stamped,
// and RestampRoundsFromFeeders calls it on load, so a freshly generated
// bracket and a restamped one cannot disagree about a round.
//
// It reads Feeders and numbered() (Hidden, and whether both sides are empty),
// never a side's contents, so it walks a bracket in play exactly as it walked
// the fresh one: nothing after the draw rewrites Feeders or Hidden, and
// numbered() says why its side check does not change either, and what
// happens in the one case, outside any supported flow, where it can.
//
// Hidden matches and the bronze (ThirdPlaceMatch) are neither read nor
// written. A bracket with no real match (fewer than two entrants) is left as
// it is. When the Feeders cannot be walked it changes NOTHING and returns
// ErrBracketFeedersUnwalkable: the last round is not a single final, a match
// id repeats, a feeder names a match that is missing, not real, or already
// reached, or a real match is never reached from the final.
func (b *Bracket) StampRoundsFromFeeders() error {
	if b == nil || len(b.Rounds) == 0 {
		return nil
	}
	rounds, err := b.feederRounds()
	if err != nil {
		return err
	}
	for m, r := range rounds {
		m.DisplayRound = r
	}
	b.NumberMatches()
	return nil
}

// feederRounds walks the Feeders from the final and returns every real
// match's round, writing nothing, so a refusal leaves the bracket exactly as
// it was. See StampRoundsFromFeeders for the rule and the refusals.
func (b *Bracket) feederRounds() (map[*BracketMatch]int, error) {
	byID := make(map[string]*BracketMatch)
	for ri := range b.Rounds {
		for mi := range b.Rounds[ri] {
			m := &b.Rounds[ri][mi]
			if _, dup := byID[m.ID]; dup {
				return nil, fmt.Errorf("%w: match id %q appears twice", ErrBracketFeedersUnwalkable, m.ID)
			}
			byID[m.ID] = m
		}
	}
	last := b.Rounds[len(b.Rounds)-1]
	if len(last) != 1 {
		return nil, fmt.Errorf("%w: the last round holds %d matches, not one final", ErrBracketFeedersUnwalkable, len(last))
	}

	// Breadth first from the final. A final that is not a real bout (fewer
	// than two entrants) roots no walk.
	round := make(map[*BracketMatch]int)
	if final := &last[0]; final.numbered() {
		round[final] = 1
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
				if _, seen := round[f]; seen {
					return nil, fmt.Errorf("%w: %s is reached twice", ErrBracketFeedersUnwalkable, fid)
				}
				round[f] = round[m] + 1
				queue = append(queue, f)
			}
		}
	}
	for ri := range b.Rounds {
		for mi := range b.Rounds[ri] {
			m := &b.Rounds[ri][mi]
			if _, reached := round[m]; m.numbered() && !reached {
				return nil, fmt.Errorf("%w: %s is not reached from the final", ErrBracketFeedersUnwalkable, m.ID)
			}
		}
	}
	return round, nil
}

// anyNumberedMatchTouched reports whether any real match of the bracket has
// been started or carries anything a result writes: a status other than
// scheduled, a winner, a score, sub-bouts, a decision, overtime, or a write
// stamp (a match started and put back in the queue keeps its stamp). Byes and
// Hidden matches are resolved by the draw itself and do not count, and the
// bronze cannot be played before the semifinals.
func (b *Bracket) anyNumberedMatchTouched() bool {
	for ri := range b.Rounds {
		for mi := range b.Rounds[ri] {
			m := &b.Rounds[ri][mi]
			if !m.numbered() {
				continue
			}
			if m.Status != MatchStatusScheduled || m.Winner != "" || m.WinnerID != "" ||
				len(m.IpponsA) > 0 || len(m.IpponsB) > 0 || m.HansokuA != 0 || m.HansokuB != 0 ||
				len(m.SubResults) > 0 || m.Decision != "" || m.Encho != nil || m.ModifiedAt != 0 {
				return true
			}
		}
	}
	return false
}

// scheduleNumberedInNumberOrder gives each court's real matches that court's
// OWN scheduled times, earliest time to the lowest match number, so the court
// plays in match-number order as a fresh draw does
// (engine.assignBracketMatchSlots). No time is invented or dropped: a court's
// set of times is unchanged, only who holds which. Hidden matches and the
// bronze keep theirs.
//
// It runs only on a bracket written by an older release (not TimesSettled,
// see RestampRoundsFromFeeders), and only touches a court whose times still
// carry the signature of the old scheduler: every release before match-number
// scheduling handed a court its times in STORAGE order (Rounds, then
// position), so they rise along it. A court whose times do not (a time the
// operator moved by hand) is left as it is, and so is one where a real match
// carries no time or one that does not read as a clock time. A court whose
// storage and number orders agree comes back unchanged.
func (b *Bracket) scheduleNumberedInNumberOrder() {
	byCourt := make(map[string][]*BracketMatch)
	var courts []string
	for ri := range b.Rounds {
		for mi := range b.Rounds[ri] {
			m := &b.Rounds[ri][mi]
			if !m.numbered() {
				continue
			}
			if _, seen := byCourt[m.Court]; !seen {
				courts = append(courts, m.Court)
			}
			byCourt[m.Court] = append(byCourt[m.Court], m)
		}
	}
	for _, court := range courts {
		ms := byCourt[court] // storage order: Rounds, then position
		times := make([]string, 0, len(ms))
		storageOrdered := true
		var prev time.Time
		for i, m := range ms {
			clock, err := time.Parse(ScheduledAtLayout, m.ScheduledAt)
			if err != nil || (i > 0 && clock.Before(prev)) {
				storageOrdered = false
				break
			}
			prev = clock
			times = append(times, m.ScheduledAt)
		}
		if !storageOrdered {
			continue
		}
		// times is ascending already (it was checked to be), so the earliest
		// goes to the lowest match number.
		sort.SliceStable(ms, func(i, j int) bool { return ms[i].MatchNumber < ms[j].MatchNumber })
		for i, m := range ms {
			m.ScheduledAt = times[i]
		}
	}
}

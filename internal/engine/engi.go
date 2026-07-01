// Package engine, engi.go owns the ENTIRE Engi-kyogi (kata demonstration / flag
// scoring) vertical slice. Engi is a second scoring paradigm: bouts are decided
// by referee flag counts (FlagsA/FlagsB) instead of ippon waza letters, and
// standings rank by wins then accumulated own-side flags.
//
// HARD SEPARATION PRINCIPLE (user directive): engi logic MUST NOT be mixed into
// the kendo scoring code. There are no `if comp.Engi` branches sprinkled through
// computeStandingsFrom, writeMatchResult, recordBracketMatchResult, or the
// shared tie-break logic. The kendo functions are BRANCHED AROUND at single
// dispatch seams (RecordMatchResultWithIneligibility(+Tx) and computeStandings)
// that delegate here; they are never edited internally. The only shared seam is
// the additive persistence DTO fields (MatchResult.FlagsA/FlagsB,
// PlayerStanding.Flags, Competition.Engi).
//
// Reusing the PURE helper propagateBracketWinner is allowed: it only advances a
// decided winner's name forward and computes no score, so it is not kendo
// scoring logic.
package engine

import (
	"fmt"
	"sort"

	"github.com/gitrgoliveira/bracket-creator/internal/helper"
	"github.com/gitrgoliveira/bracket-creator/internal/state"
)

// engiValidTotal reports whether a flag pair is a valid engi result. Valid
// totals are {1, 3, 5}: odd (so there is always a strict majority and never a
// draw) and at most 5 (the hard cap, there are never more than 5 referees on an
// official panel). The oddness of the total is what guarantees a strict winner:
// an equal split can only sum to an even number, so a {1, 3, 5} total already
// implies flagsA != flagsB and the winner derivation below is total.
func engiValidTotal(flagsA, flagsB int) bool {
	if flagsA < 0 || flagsB < 0 {
		return false
	}
	t := flagsA + flagsB
	return t == 1 || t == 3 || t == 5
}

// engiWinnerSide returns "A" or "B" for the side with more flags. Callers MUST
// have validated via engiValidTotal first (which guarantees flagsA != flagsB).
func engiWinnerSide(flagsA, flagsB int) string {
	if flagsA > flagsB {
		return "A"
	}
	return "B"
}

// recordEngiMatchResult records a completed engi bout (POOL or BRACKET), keyed
// by competition + match id and the two flag counts. It is the engi twin of the
// kendo record path and does NOT route through writeMatchResult /
// recordBracketMatchResult. Validation ({1,3,5}, no draw) lives here.
//
// Pool match: updates the pool-match record in place (winner from flag majority,
// flag counts stored, status completed).
//
// Bracket match (including the "m-bronze" 3rd-place playoff): sets
// Winner/FlagsA/FlagsB on the stored match, then calls the pure
// propagateBracketWinner to advance the decided winner (no advancement out of
// bronze).
//
// correctionReason is the operator-supplied audit note when overwriting a
// previously completed match. It mirrors the kendo path's CorrectionReason
// so the audit trail is preserved for engi competitions.
//
// Returns the persisted MatchResult so the handler can echo / broadcast it.
func (e *Engine) recordEngiMatchResult(compID, matchID string, flagsA, flagsB int, correctionReason string) (*state.MatchResult, error) {
	return e.recordEngiMatch(compID, matchID, flagsA, flagsB, correctionReason,
		e.withPoolMatch,
		e.store.UpdateBracket,
	)
}

// recordEngiMatchResultTx is the transaction-aware twin. It writes through the
// supplied StoreTx so the engi dispatch from RecordMatchResultWithIneligibilityTx
// runs inside the caller's single per-comp lock acquire (calling e.store
// directly from inside a held tx would deadlock the non-reentrant mutex).
func (e *Engine) recordEngiMatchResultTx(tx state.StoreTx, compID, matchID string, flagsA, flagsB int, correctionReason string) (*state.MatchResult, error) {
	return e.recordEngiMatch(compID, matchID, flagsA, flagsB, correctionReason,
		func(cID, mID string, mutate func(*state.MatchResult)) error {
			return e.withPoolMatchTx(tx, cID, mID, mutate)
		},
		tx.UpdateBracket,
	)
}

// recordEngiMatch is the shared record core for both the tx and non-tx paths.
// poolUpdate and bracketUpdate abstract the persistence layer so the same logic
// runs against either e.store (non-tx) or a StoreTx (tx).
func (e *Engine) recordEngiMatch(
	compID, matchID string,
	flagsA, flagsB int,
	correctionReason string,
	poolUpdate func(compID, matchID string, mutate func(*state.MatchResult)) error,
	bracketUpdate func(compID string, mutate func(*state.Bracket) error) error,
) (*state.MatchResult, error) {
	if !engiValidTotal(flagsA, flagsB) {
		return nil, validationErrorf(
			"engi: flag total %d+%d=%d is invalid; total must be in {1,3,5} with flagsA != flagsB",
			flagsA, flagsB, flagsA+flagsB,
		)
	}
	winnerSide := engiWinnerSide(flagsA, flagsB)

	// Try the pool stage first.
	var out *state.MatchResult
	err := poolUpdate(compID, matchID, func(r *state.MatchResult) {
		applyEngiToMatchResult(r, flagsA, flagsB, winnerSide, correctionReason)
		cp := *r
		out = &cp
	})
	if err == nil {
		return out, nil
	}
	if err != errMatchNotFound {
		return nil, err
	}

	// Fall through to the bracket stage (rounds + bronze).
	var result *state.MatchResult
	updateErr := bracketUpdate(compID, func(b *state.Bracket) error {
		for rIdx, round := range b.Rounds {
			for mIdx := range round {
				if b.Rounds[rIdx][mIdx].ID != matchID {
					continue
				}
				bm := &b.Rounds[rIdx][mIdx]
				if !bracketMatchPlayable(bm) {
					return validationErrorf("knockout match %s is not ready to score: a feeder pool or match has not finished", matchID)
				}
				result = applyEngiToBracketMatch(bm, flagsA, flagsB, winnerSide, correctionReason)
				e.propagateBracketWinner(b, rIdx, mIdx)
				return nil
			}
		}
		if b.ThirdPlaceMatch != nil && b.ThirdPlaceMatch.ID == matchID {
			bm := b.ThirdPlaceMatch
			if !bracketMatchPlayable(bm) {
				return validationErrorf("knockout match %s is not ready to score: a feeder pool or match has not finished", matchID)
			}
			result = applyEngiToBracketMatch(bm, flagsA, flagsB, winnerSide, correctionReason)
			// No propagation out of bronze.
			return nil
		}
		return notFoundErrorf("bracket match %s not found", matchID)
	})
	if updateErr != nil {
		return nil, updateErr
	}
	return result, nil
}

// applyEngiToMatchResult writes a flag-decided result into a pool MatchResult.
// correctionReason is the operator audit note for overwrites; it is persisted
// only when non-empty, mirroring the kendo path's CorrectionReason semantics.
// Also sets WinnerID from SideAID/SideBID (when present) so same-name
// participants from different dojos remain distinguishable downstream (e.g.
// computeEngiStandings), mirroring the non-engi scoring path.
func applyEngiToMatchResult(r *state.MatchResult, flagsA, flagsB int, winnerSide, correctionReason string) {
	if winnerSide == "A" {
		r.Winner = r.SideA
		r.WinnerID = r.SideAID
	} else {
		r.Winner = r.SideB
		r.WinnerID = r.SideBID
	}
	r.WinnerSide = winnerSide
	r.FlagsA = flagsA
	r.FlagsB = flagsB
	r.Status = state.MatchStatusCompleted
	if correctionReason != "" {
		r.CorrectionReason = correctionReason
	}
}

// applyEngiToBracketMatch writes a flag-decided result into a BracketMatch and
// returns the equivalent MatchResult for the caller to echo / broadcast.
// correctionReason is persisted on the bracket match when non-empty.
func applyEngiToBracketMatch(bm *state.BracketMatch, flagsA, flagsB int, winnerSide, correctionReason string) *state.MatchResult {
	if winnerSide == "A" {
		bm.Winner = bm.SideA
	} else {
		bm.Winner = bm.SideB
	}
	bm.FlagsA = flagsA
	bm.FlagsB = flagsB
	bm.Status = state.MatchStatusCompleted
	if correctionReason != "" {
		bm.CorrectionReason = correctionReason
	}
	return &state.MatchResult{
		ID:               bm.ID,
		SideA:            bm.SideA,
		SideB:            bm.SideB,
		Winner:           bm.Winner,
		WinnerSide:       winnerSide,
		FlagsA:           flagsA,
		FlagsB:           flagsB,
		Status:           state.MatchStatusCompleted,
		Court:            bm.Court,
		ScheduledAt:      bm.ScheduledAt,
		CorrectionReason: correctionReason,
	}
}

// engiPlayerKey returns the stable key used to index playerStandings by
// participant identity. Same-name participants from different dojos are
// explicitly allowed (CheckDuplicateEntriesByNameDojo only rejects same-name
// AND same-dojo), so keying by name alone silently merges distinct
// competitors into one standings row. Prefers the participant UUID (id);
// falls back to name for legacy data that predates SideAID/SideBID/player.ID
// (empty for old CSVs, so behavior for that data is unchanged).
func engiPlayerKey(id, name string) string {
	if id != "" {
		return "id:" + id
	}
	return "name:" + name
}

// engiStandingsLoader is the minimal read surface computeEngiStandings
// requires. It is narrower than poolStandingsLoader (which also mandates
// LoadCompetition); both *state.Store and state.StoreTx satisfy it.
type engiStandingsLoader interface {
	LoadPools(compID string) ([]helper.Pool, error)
	LoadPoolMatches(compID string) ([]state.MatchResult, error)
}

// computeEngiStandings is the engi standings core, fully independent of the
// kendo computeStandingsFrom. It ranks each pool by (1) total Wins, then
// (2) total accumulated OWN-SIDE flags across every completed bout (the winner
// accrues their flags AND the loser accrues theirs, so a 3-2 bout adds +3 to
// the winner and +2 to the loser toward the tiebreaker).
//
// Works for BOTH pool and league formats because the dispatch seam in
// computeStandings sits above the pool/league split: a league competition
// stores all its bouts as pool matches under its single league pool, so the
// same per-pool aggregation applies.
func (e *Engine) computeEngiStandings(loader engiStandingsLoader, compID string) (map[string][]state.PlayerStanding, error) {
	pools, err := loader.LoadPools(compID)
	if err != nil {
		return nil, err
	}
	results, err := loader.LoadPoolMatches(compID)
	if err != nil {
		return nil, err
	}

	poolResults := make(map[string][]state.MatchResult)
	for _, r := range results {
		if pn, ok := poolNameFromMatchID(r.ID); ok {
			poolResults[pn] = append(poolResults[pn], r)
		}
	}

	allStandings := make(map[string][]state.PlayerStanding)
	for _, p := range pools {
		matches := poolResults[p.PoolName]

		playerStandings := make(map[string]*state.PlayerStanding)
		for _, player := range p.Players {
			playerStandings[engiPlayerKey(player.ID, player.Name)] = &state.PlayerStanding{Player: player}
		}

		for _, m := range matches {
			if m.Status != state.MatchStatusCompleted {
				continue
			}
			// Supplementary bouts (TB/DH) don't count toward engi standings.
			if IsTiebreakerMatchID(m.ID) || IsPoolDaihyosenMatchID(m.ID) {
				continue
			}
			sA := playerStandings[engiPlayerKey(m.SideAID, m.SideA)]
			sB := playerStandings[engiPlayerKey(m.SideBID, m.SideB)]
			if sA == nil || sB == nil {
				continue
			}
			// Win/loss by flag majority. Engi has no draws (odd flag total).
			// Resolve the winning side by WinnerID when available (unambiguous
			// even when both sides share a display name); fall back to the
			// Winner name for legacy data recorded before WinnerID was set.
			winnerIsA := (m.WinnerID != "" && m.WinnerID == m.SideAID) || (m.WinnerID == "" && m.Winner == m.SideA)
			winnerIsB := (m.WinnerID != "" && m.WinnerID == m.SideBID) || (m.WinnerID == "" && m.Winner == m.SideB)
			switch {
			case winnerIsA:
				sA.Wins++
				sB.Losses++
			case winnerIsB:
				sB.Wins++
				sA.Losses++
			}
			// Own-side flag accrual: winner AND loser both accumulate the flags
			// raised for their own side.
			sA.Flags += m.FlagsA
			sB.Flags += m.FlagsB
		}

		sorted := make([]state.PlayerStanding, 0, len(playerStandings))
		for _, s := range playerStandings {
			s.ScoreSummary = fmt.Sprintf("W:%d Flags:%d", s.Wins, s.Flags)
			sorted = append(sorted, *s)
		}

		// Stable sort: more Wins first, then more accumulated own-side Flags,
		// then by name so the order is deterministic for fully-tied
		// competitors. Points is left at its zero value: engi has no points
		// metric, so an honest 0 reaches the wire rather than a packed sort key.
		sort.SliceStable(sorted, func(i, j int) bool {
			if sorted[i].Wins != sorted[j].Wins {
				return sorted[i].Wins > sorted[j].Wins
			}
			if sorted[i].Flags != sorted[j].Flags {
				return sorted[i].Flags > sorted[j].Flags
			}
			return sorted[i].Player.Name < sorted[j].Player.Name
		})

		for i := range sorted {
			sorted[i].Rank = i + 1
		}
		allStandings[p.PoolName] = sorted
	}
	return allStandings, nil
}

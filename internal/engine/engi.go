// Package engine, engi.go owns the ENTIRE Engi-kyogi (kata competition / flag
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

	"github.com/gitrgoliveira/bracket-creator/internal/domain"
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
func (e *Engine) recordEngiMatchResult(h state.StoreTx, compID, matchID string, flagsA, flagsB int, correctionReason string) (*state.MatchResult, error) {
	return e.recordEngiMatch(h, compID, matchID, flagsA, flagsB, correctionReason)
}

// backfillEngiResult copies the engine-derived identity from a recorded engi
// MatchResult (rec) onto the caller's result so the handler's SSE
// match_updated broadcast carries the winner. The engi score client submits
// only flag counts and status, never a winner, so without this the bracket
// card / scoreboard would show the match completed but with no winner
// highlight until the next background refetch. Winner/WinnerSide are set by
// engiWinnerSide; WinnerID is populated for pool bouts (from SideAID/SideBID)
// and empty for bracket bouts (BracketMatch has no per-side IDs), matching the
// authoritative on-disk state either way.
func backfillEngiResult(result, rec *state.MatchResult) {
	if result == nil || rec == nil {
		return
	}
	result.Winner = rec.Winner
	result.WinnerSide = rec.WinnerSide
	result.WinnerID = rec.WinnerID
	result.Status = rec.Status
}

// recordEngiMatch is the shared record core. The store handle h abstracts the
// persistence layer: *state.Store satisfies state.StoreTx, so the same body
// runs against either the store itself (each call locks) or a live transaction
// (the caller's per-comp lock is already held) — see writeToPoolOrBracket for
// the handle convention. This used to inject two closures per persistence op;
// the handle carries both.
func (e *Engine) recordEngiMatch(
	h state.StoreTx,
	compID, matchID string,
	flagsA, flagsB int,
	correctionReason string,
) (*state.MatchResult, error) {
	if !engiValidTotal(flagsA, flagsB) {
		return nil, validationErrorf(
			"engi: flag total %d+%d=%d is invalid; total must be odd and in {1,3,5} (3- or 5-referee panel, no draw possible)",
			flagsA, flagsB, flagsA+flagsB,
		)
	}
	winnerSide := engiWinnerSide(flagsA, flagsB)

	// Try the pool stage first.
	var out *state.MatchResult
	err := e.withPoolMatch(h, compID, matchID, func(r *state.MatchResult) error {
		applyEngiToMatchResult(r, flagsA, flagsB, winnerSide, correctionReason)
		cp := *r
		out = &cp
		return nil
	})
	if err == nil {
		return out, nil
	}
	if err != errMatchNotFound {
		return nil, err
	}

	// Fall through to the bracket stage (rounds + bronze).
	var result *state.MatchResult
	updateErr := h.UpdateBracket(compID, func(b *state.Bracket) error {
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

// standingsPlayerKey returns the id-based or name-based encoding used by
// registerStandingsPlayer / lookupStandingsPlayer below. Same-name
// participants from different dojos are explicitly allowed
// (CheckDuplicateEntriesByNameDojo only rejects same-name AND same-dojo), so
// keying by name alone silently merges distinct competitors into one
// standings row. NOT used by Swiss (SwissStandings / computeEngiSwissStandings):
// Swiss matches persist no per-side UUIDs at all (buildSwissMatches never
// sets SideAID/SideBID, and the SPA only echoes an id it already received),
// so an id-preferring key there would build "id:<uuid>" roster entries that
// no match lookup (always "name:...", since the match side id is always
// empty) could ever find -- silently dropping every Swiss tally rather than
// fixing the same-name case. See computeEngiSwissStandings's doc comment.
func standingsPlayerKey(id, name string) string {
	if id != "" {
		return "id:" + id
	}
	return "name:" + name
}

// registerStandingsPlayer indexes a fresh *state.PlayerStanding for player
// into m under BOTH its name key AND (when player.ID is non-empty) its id
// key, and returns the standing so the caller can keep populating it.
//
// Registering both, rather than only the id-preferred single key, is what
// makes lookupStandingsPlayer resilient to a roster whose participants carry
// real ids while some of the MATCHES that reference them don't (a
// competition drawn before per-side ids existed on a given match shape, or a
// TB/DH row generated before generateTiebreakerMatches /
// generatePoolDaihyosenMatches started stamping SideAID/SideBID): the id key
// simply goes unused for those matches and lookup falls through to the name
// key, exactly as it always did before ids existed. A single symmetric key
// (id-preferred on BOTH sides) does not degrade this way: if the roster
// entry resolves via "id:<uuid>" but the match carries no id for that side,
// the match's key is "name:<name>", which never equals the roster's key, and
// EVERY match missing a side id -- not just the same-name case -- silently
// stops contributing to standings. That regression was caught by
// internal/export's hand-built fixtures (real participant ids, no
// SideAID/SideBID on the match) before it could reach production data drawn
// the same way (e.g. a mid-tournament upgrade whose TB/DH rows predate the
// id-stamping fix).
//
// The name key is last-write-wins on a genuine collision (two roster entries
// sharing a name with no id on either side to disambiguate): that is the
// SAME degraded behavior standings always had before ids existed, not a new
// gap -- it only matters when a match referencing one of them ALSO carries
// no id, at which point there is no data left to disambiguate correctly.
func registerStandingsPlayer(m map[string]*state.PlayerStanding, player domain.Player) *state.PlayerStanding {
	st := &state.PlayerStanding{Player: player}
	m[standingsPlayerKey("", player.Name)] = st
	if player.ID != "" {
		m[standingsPlayerKey(player.ID, "")] = st
	}
	return st
}

// lookupStandingsPlayer resolves a match side (id, name) to the
// *state.PlayerStanding registered by registerStandingsPlayer. Prefers the
// id key when id is non-empty AND that key was actually registered
// (unambiguous even when two roster entries share a display name); falls
// back to the name key otherwise -- when the match carries no id for this
// side, or carries one that doesn't match any registered roster id (stale/
// foreign data; degrading to name is preferable to resolving nothing).
func lookupStandingsPlayer(m map[string]*state.PlayerStanding, id, name string) *state.PlayerStanding {
	if id != "" {
		if st, ok := m[standingsPlayerKey(id, "")]; ok {
			return st
		}
	}
	return m[standingsPlayerKey("", name)]
}

// engiScoreSummary renders the human-readable score cell for an engi
// standing. One format definition shared by the pool/league and Swiss engi
// standings so the two tables can never drift.
func engiScoreSummary(s *state.PlayerStanding) string {
	return fmt.Sprintf("W:%d Flags:%d", s.Wins, s.Flags)
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
// It takes the same poolStandingsLoader as computeStandingsFrom (it only calls
// LoadPools + LoadPoolMatches, never LoadCompetition), so both *state.Store and
// state.StoreTx satisfy it.
func (e *Engine) computeEngiStandings(loader poolStandingsLoader, compID string) (map[string][]state.PlayerStanding, error) {
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
		// order holds one *PlayerStanding per player, in roster order: the
		// output slice below ranges over THIS, not over playerStandings'
		// values, because registerStandingsPlayer indexes the SAME pointer
		// under two keys (id and name) so a range over the map's values
		// would visit -- and append -- each player twice.
		order := make([]*state.PlayerStanding, 0, len(p.Players))
		for _, player := range p.Players {
			order = append(order, registerStandingsPlayer(playerStandings, player))
		}

		for _, m := range matches {
			if m.Status != state.MatchStatusCompleted {
				continue
			}
			// Supplementary bouts (TB/DH) don't count toward engi standings.
			if IsTiebreakerMatchID(m.ID) || IsPoolDaihyosenMatchID(m.ID) {
				continue
			}
			sA := lookupStandingsPlayer(playerStandings, m.SideAID, m.SideA)
			sB := lookupStandingsPlayer(playerStandings, m.SideBID, m.SideB)
			if sA == nil || sB == nil {
				continue
			}
			// Win by flag majority. Engi has no draws (odd flag total) and does
			// not record losses: ranking is Wins then accumulated own-side Flags.
			// Resolve the winning side by WinnerID when available (unambiguous
			// even when both sides share a display name); fall back to the
			// Winner name for legacy data recorded before WinnerID was set.
			winnerIsA := (m.WinnerID != "" && m.WinnerID == m.SideAID) || (m.WinnerID == "" && m.Winner == m.SideA)
			winnerIsB := (m.WinnerID != "" && m.WinnerID == m.SideBID) || (m.WinnerID == "" && m.Winner == m.SideB)
			switch {
			case winnerIsA:
				sA.Wins++
			case winnerIsB:
				sB.Wins++
			}
			// Own-side flag accrual: winner AND loser both accumulate the flags
			// raised for their own side.
			sA.Flags += m.FlagsA
			sB.Flags += m.FlagsB
		}

		sorted := make([]state.PlayerStanding, 0, len(order))
		for _, s := range order {
			s.ScoreSummary = engiScoreSummary(s)
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

// computeEngiSwissStandings is the engi twin of SwissStandings: the
// flag-scored standings core for a Swiss competition. It is the delegate the
// kendo SwissStandings branches to at its engi dispatch seam, so engi's
// flag ranking stays out of the kendo tally (engi.go hard-separation
// principle). It mirrors SwissStandings' cumulative-across-rounds structure
// (one flat group, byes are auto-wins, head-to-head is the final tiebreak
// before name) but ranks by (1) Wins then (2) accumulated OWN-SIDE flags,
// exactly like the pool/league computeEngiStandings.
//
// Identity is keyed by display NAME, exactly like the kendo SwissStandings it
// twins: Swiss matches persist no per-side UUIDs (SideAID/SideBID/WinnerID are
// empty in pool-matches.csv), and the Swiss pairing pipeline (rematch
// avoidance, win/bye tracking, rank ordering) already treats names as the
// pairing identity end to end -- see the correction in swissFieldNamesFromMatches's
// doc comment: the actual duplicate-name rule is name+dojo
// (CheckDuplicateEntriesByNameDojo), not the whole-row CheckDuplicateEntries,
// so this is a known, accepted gap for a same-name pair in Swiss, not a rule.
// Keying by standingsPlayerKey would build "id:<uuid>" roster keys that never
// match the name-only lookups the empty-ID matches produce, tallying nothing.
func (e *Engine) computeEngiSwissStandings(participants []domain.Player, matches []state.MatchResult) ([]state.PlayerStanding, error) {
	byName := make(map[string]*state.PlayerStanding, len(participants))
	for _, p := range participants {
		byName[p.Name] = &state.PlayerStanding{Player: p}
	}

	headToHead := make(map[string]map[string]string) // sideA → sideB → who won
	for _, m := range matches {
		if _, ok := parseSwissMatchRound(m.ID); !ok {
			continue
		}
		// Bye: SideA wins, no flags accrued, no head-to-head.
		if m.SideB == "" {
			if sA := byName[m.SideA]; sA != nil {
				sA.Wins++
			}
			continue
		}
		if m.Status != state.MatchStatusCompleted {
			continue
		}
		sA := byName[m.SideA]
		sB := byName[m.SideB]
		if sA == nil || sB == nil {
			continue
		}
		// Engi has no draws (odd flag total) and records no losses: ranking is
		// Wins then accumulated own-side Flags. Winner resolves by name (Swiss
		// stores no WinnerID); a completed engi bout always names a winner.
		switch m.Winner {
		case m.SideA:
			sA.Wins++
			recordHeadToHead(headToHead, m.SideA, m.SideB, m.SideA)
		case m.SideB:
			sB.Wins++
			recordHeadToHead(headToHead, m.SideA, m.SideB, m.SideB)
		}
		// Own-side flag accrual: winner AND loser both accumulate the flags
		// raised for their own side.
		sA.Flags += m.FlagsA
		sB.Flags += m.FlagsB
	}

	standings := make([]state.PlayerStanding, 0, len(byName))
	for _, s := range byName {
		s.ScoreSummary = engiScoreSummary(s)
		standings = append(standings, *s)
	}
	sort.SliceStable(standings, func(i, j int) bool {
		a, b := standings[i], standings[j]
		if a.Wins != b.Wins {
			return a.Wins > b.Wins
		}
		if a.Flags != b.Flags {
			return a.Flags > b.Flags
		}
		// Head-to-head: if a beat b directly, a ranks higher.
		if winner, ok := lookupH2H(headToHead, a.Player.Name, b.Player.Name); ok {
			if winner == a.Player.Name {
				return true
			}
			if winner == b.Player.Name {
				return false
			}
		}
		return a.Player.Name < b.Player.Name
	})
	for i := range standings {
		standings[i].Rank = i + 1
	}
	return standings, nil
}

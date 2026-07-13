// Package engine, kachinuki "winner-stays-on" team match advancement.
//
// FR-044, data-model §4.1.
//
// Kachinuki is a team-match format where:
//
//   - Only the first bout is scheduled up front.
//   - After each bout the winner stays on the court and faces the next
//     un-retired player from the losing team.
//   - On a hikiwake (draw) BOTH players retire and the next pair from
//     each remaining roster advance.
//   - The team match ends when one side has no remaining un-retired
//     players, the other side wins by exhaustion
//     (domain.DecisionKachinukiExhaustion).
//
// AdvanceKachinuki encapsulates the pure decision logic. Callers
// (typically a score handler, see handlers_match.go) pass a snapshot
// of the just-completed bout plus the remaining un-retired roster per
// side, and the engine returns either the next bout to schedule or a
// MatchEnded sentinel.
package engine

import (
	"errors"
	"fmt"
	"log"
	"sort"

	"github.com/gitrgoliveira/bracket-creator/internal/domain"
	"github.com/gitrgoliveira/bracket-creator/internal/state"
)

// ErrKachinukiPrematureCompletion is returned by
// CheckKachinukiPrematureCompletion when a completed-status write would
// finalize a kachinuki match that still has players remaining on both
// teams and carries no daihyosen resolution. The score handler maps it
// to HTTP 409.
var ErrKachinukiPrematureCompletion = errors.New("kachinuki match cannot be completed while both teams still have players remaining")

// AdvanceKachinukiInput is the minimal snapshot AdvanceKachinuki needs.
// The engine deliberately does NOT load the full match, callers pass
// the completed bout plus the un-retired roster per team so this
// function stays free of I/O and trivially unit-testable.
//
//   - LastBout: the bout that just completed. Decision-or-Winner
//     determines the advancement path. SideA / SideB names on the bout
//     identify which physical player just played for each team.
//   - SideA, SideB: remaining un-retired competitors per team in the
//     order they will take the court. SHOULD NOT include the players
//     that just played in LastBout (callers strip retired players
//     before passing the snapshot). The team names themselves are
//     carried on the parent MatchResult, not here.
type AdvanceKachinukiInput struct {
	LastBout state.SubMatchResult
	SideA    []string
	SideB    []string
}

// AdvanceKachinukiResult is the engine's verdict.
//
//   - Next: when non-nil, the next bout to schedule. Position is set to
//     LastBout.Position + 1; SideA/SideB carry the next pair of player
//     names. Other fields are left zero, the score handler will fill
//     them as the bout is played.
//   - MatchEnded: true when one team has no remaining un-retired
//     players. Next is nil. WinningSide is "A" or "B"; Decision is
//     domain.DecisionKachinukiExhaustion. Callers should mark the
//     parent MatchResult completed with these values.
//   - BothExhausted: true when a hikiwake retired the last player on
//     both teams simultaneously. MatchEnded is false; the caller
//     decides the outcome by phase (pool/league draw, bracket daihyosen).
type AdvanceKachinukiResult struct {
	Next        *state.SubMatchResult
	MatchEnded  bool
	WinningSide string // "A" or "B" when MatchEnded; "" otherwise
	Decision    string // domain.DecisionKachinukiExhaustion when MatchEnded

	// BothExhausted is true only when a hikiwake retired the last player on
	// BOTH teams at once (no winner determinable). AdvanceKachinuki cannot
	// pick a winner; the caller decides the outcome by phase: a pool/league
	// encounter is finalized as a draw, a bracket encounter stays running
	// until the operator resolves the tie with a daihyosen.
	BothExhausted bool
}

// AdvanceKachinuki computes the post-bout transition.
//
// Branches:
//
//  1. LastBout.Winner names the SideA player → SideA stays on; we pair
//     them against the head of input.SideB.
//  2. LastBout.Winner names the SideB player → SideB stays on; we pair
//     them against the head of input.SideA.
//  3. LastBout is a hikiwake (Decision == domain.DecisionHikiwake or
//     Winner == "" with a recorded decision) → both retire; pair the
//     heads of input.SideA and input.SideB.
//  4. One side's queue empty → MatchEnded=true, the non-empty side wins
//     by exhaustion. BOTH empty (simultaneous exhaustion after a
//     hikiwake) → BothExhausted=true and no winner; the caller finalizes
//     a pool/league encounter as a draw or keeps a bracket encounter
//     running for a daihyosen.
//
// The function is pure: no I/O, no logging on the happy path. Unusual
// inputs (Winner not matching either side) log a warning so
// live-tournament operators get a breadcrumb when something downstream
// silently degraded. Simultaneous exhaustion (BothExhausted) logs a
// breadcrumb to trace the phase-dispatch flow.
func AdvanceKachinuki(in AdvanceKachinukiInput) AdvanceKachinukiResult {
	last := in.LastBout
	// Hikiwake: explicit "hikiwake" decision is the canonical signal.
	// We deliberately don't treat "empty Winner + any decision" as a
	// draw because a non-hikiwake decision (kiken, fusenpai, …) should
	// have a Winner assigned by the score handler, an empty Winner
	// there is malformed input, not a draw.
	hikiwake := state.IsDraw(last.Decision)

	switch {
	case hikiwake:
		return advanceAfterHikiwake(in)
	case last.Winner == last.SideA && last.SideA != "":
		return advanceWinnerStays(last.SideA, last.Position, in.SideB, "A")
	case last.Winner == last.SideB && last.SideB != "":
		return advanceWinnerStays(last.SideB, last.Position, in.SideA, "B")
	default:
		// Unexpected: Winner is set but doesn't match either bout
		// side. Treat as a no-op (no advancement) so callers fall
		// back to manual scheduling instead of silently producing a
		// wrong pairing.
		log.Printf("engine.AdvanceKachinuki: unrecognized bout outcome, winner=%q sideA=%q sideB=%q decision=%q; no advancement",
			last.Winner, last.SideA, last.SideB, last.Decision)
		return AdvanceKachinukiResult{}
	}
}

// advanceWinnerStays builds the next-bout descriptor when one side's
// player stays on. The opposing side's queue (`oppQueue`) must contain
// the next un-retired opponent at index 0. The `winnerSide` param is
// just for the exhaustion-end path's WinningSide field.
func advanceWinnerStays(stayingName string, lastPos int, oppQueue []string, winnerSide string) AdvanceKachinukiResult {
	if len(oppQueue) == 0 {
		// Opposing team is exhausted, current side wins.
		return AdvanceKachinukiResult{
			MatchEnded:  true,
			WinningSide: winnerSide,
			Decision:    string(domain.DecisionKachinukiExhaustion),
		}
	}
	nextOpp := oppQueue[0]
	// Preserve the canonical SideA/SideB role from the previous bout:
	// when SideA's player stays, they remain SideA in the new bout;
	// when SideB's player stays, they remain SideB.
	var sideA, sideB string
	if winnerSide == "A" {
		sideA, sideB = stayingName, nextOpp
	} else {
		sideA, sideB = nextOpp, stayingName
	}
	return AdvanceKachinukiResult{
		Next: &state.SubMatchResult{
			Position: lastPos + 1,
			SideA:    sideA,
			SideB:    sideB,
		},
	}
}

// advanceAfterHikiwake builds the next-bout descriptor when both
// previous-bout players retire. Pairs the heads of each remaining
// queue. Either side empty → opposing side wins by exhaustion; both
// empty → BothExhausted (caller finalizes a pool draw or keeps a bracket running for daihyosen).
func advanceAfterHikiwake(in AdvanceKachinukiInput) AdvanceKachinukiResult {
	switch {
	case len(in.SideA) == 0 && len(in.SideB) == 0:
		// Both teams ran out simultaneously after a draw. The engine cannot
		// determine a winner; flag BothExhausted and let the caller decide by
		// phase (pool/league finalize as a draw, bracket stays running for a
		// daihyosen). See MaybeAdvanceKachinuki.
		log.Printf("engine.AdvanceKachinuki: hikiwake exhausted both teams simultaneously at position %d; caller resolves by phase (pool draw / bracket daihyosen)",
			in.LastBout.Position)
		return AdvanceKachinukiResult{BothExhausted: true}
	case len(in.SideA) == 0:
		return AdvanceKachinukiResult{
			MatchEnded:  true,
			WinningSide: "B",
			Decision:    string(domain.DecisionKachinukiExhaustion),
		}
	case len(in.SideB) == 0:
		return AdvanceKachinukiResult{
			MatchEnded:  true,
			WinningSide: "A",
			Decision:    string(domain.DecisionKachinukiExhaustion),
		}
	}
	return AdvanceKachinukiResult{
		Next: &state.SubMatchResult{
			Position: in.LastBout.Position + 1,
			SideA:    in.SideA[0],
			SideB:    in.SideB[0],
		},
	}
}

// RetiredPlayersFromBoutLog walks a bout log and returns, per side,
// the set of player names that have retired (lost or hikiwake'd out)
// up to and including the supplied log. The returned maps key off
// player name; presence == retired.
//
// Helper for callers building AdvanceKachinukiInput.{SideA,SideB} from
// a roster, they subtract retired names from the initial roster to
// derive the remaining un-retired queue.
//
// teamAName / teamBName are the parent MatchResult.SideA / SideB
// (the team names), used to disambiguate which side won each bout.
func RetiredPlayersFromBoutLog(boutLog []state.SubMatchResult, teamAName, teamBName string) (retiredA, retiredB map[string]struct{}) {
	retiredA = map[string]struct{}{}
	retiredB = map[string]struct{}{}
	for _, b := range boutLog {
		if b.Position == state.DaihyosenSubPosition {
			// The daihyosen (rep bout) is not a kachinuki bout: its side
			// names are the representatives (often the team names), not
			// roster players, so it must not retire anyone.
			continue
		}
		hikiwake := state.IsDraw(b.Decision)
		if hikiwake {
			if b.SideA != "" {
				retiredA[b.SideA] = struct{}{}
			}
			if b.SideB != "" {
				retiredB[b.SideB] = struct{}{}
			}
			continue
		}
		// Map per-bout winner to the team side. A team-name match on
		// the parent (b.Winner == teamAName) is the legacy synth path
		// from quick-score; the per-player path keys on the bout's
		// SideA/SideB names.
		switch b.Winner {
		case b.SideA, teamAName:
			// SideA player stays; SideB player retires.
			if b.SideB != "" {
				retiredB[b.SideB] = struct{}{}
			}
		case b.SideB, teamBName:
			if b.SideA != "" {
				retiredA[b.SideA] = struct{}{}
			}
		}
	}
	return retiredA, retiredB
}

// FilterRemaining returns roster entries that are NOT present in the
// retired set, preserving original order. Helper for callers building
// AdvanceKachinukiInput.{SideA,SideB} from a roster and a retired set
// produced by RetiredPlayersFromBoutLog.
func FilterRemaining(roster []string, retired map[string]struct{}) []string {
	out := make([]string, 0, len(roster))
	for _, name := range roster {
		if _, gone := retired[name]; gone {
			continue
		}
		out = append(out, name)
	}
	return out
}

// describeKachinukiResult is a stringer used by the handler when
// logging an advancement decision. Pure helper, no behaviour.
func describeKachinukiResult(r AdvanceKachinukiResult) string {
	if r.MatchEnded {
		return fmt.Sprintf("MatchEnded winningSide=%s decision=%s", r.WinningSide, r.Decision)
	}
	if r.Next != nil {
		return fmt.Sprintf("Next position=%d sideA=%q sideB=%q", r.Next.Position, r.Next.SideA, r.Next.SideB)
	}
	return "no-op"
}

// appendNextKachinukiBout appends the engine-produced next bout to a
// bracket match's log, mirroring the pool mutate closure (GAP 4): the
// encounter stays running with no match-level winner or decision.
// Shared by the rounds loop and the bronze (3rd-place) branch.
func appendNextKachinukiBout(bm *state.BracketMatch, next state.SubMatchResult) {
	next.Position = len(bm.SubResults) + 1
	bm.SubResults = append(bm.SubResults, next)
	bm.Status = state.MatchStatusRunning
	bm.Winner = ""
	bm.Decision = ""
}

// MaybeAdvanceKachinuki runs the post-score side effect for a
// kachinuki team match.
//
// The score endpoint (handlers_match.go) calls this AFTER
// RecordMatchResult* has persisted the operator's bout. Steps:
//
//  1. Load the competition; bail out as no-op if it's not a kachinuki
//     team competition.
//  2. Load the just-recorded MatchResult; bail if its last SubResults
//     entry has no final outcome (still in progress).
//  3. Build the remaining-roster snapshot per side from the saved
//     TeamLineup (GAP 1/2a), falling back to the unique player names
//     seen in the bout log when no lineup is saved. The exhaustion-end
//     and hikiwake-after-empty-queue cases run off this roster data.
//  4. Pass to AdvanceKachinuki. When it returns Next, append the bout
//     to SubResults and persist (status stays Running). When it returns
//     MatchEnded, finalize the parent match (Status=Completed,
//     Decision=kachinuki-exhaustion, Winner=team-name).
//
// Returns (changed, error). `changed` indicates whether SubResults or
// the parent match was mutated, handler uses it to decide whether to
// emit an additional match-updated SSE event with the freshly-derived
// bout list.
//
// FR-044, T135, T137.
func (e *Engine) MaybeAdvanceKachinuki(compID, matchID string) (bool, error) {
	comp, err := e.store.LoadCompetition(compID)
	if err != nil {
		return false, err
	}
	if comp == nil || comp.TeamSize < 2 || comp.TeamMatchType != state.TeamMatchTypeKachinuki {
		return false, nil
	}

	// Locate the parent match in either the pool or bracket store:
	// advancement runs in both (bracket bouts append via
	// appendNextKachinukiBout, with propagateBracketWinner on
	// exhaustion).
	parent, isBracket, roundIdx, err := e.findTeamMatch(compID, matchID)
	if err != nil {
		return false, err
	}
	if parent == nil || len(parent.SubResults) == 0 {
		return false, nil
	}
	// A completed match is final: corrections re-submit the bout log of a
	// finished match and must never re-run advancement (which would append
	// a phantom next bout onto the completed result). Defense in depth on
	// top of the handler's kachinukiBoutFinal gating.
	if parent.Status == state.MatchStatusCompleted {
		return false, nil
	}

	// Advancement is driven by the last NUMBERED bout. A daihyosen sub-result
	// (Position == DaihyosenSubPosition) is not a kachinuki bout: a bracket
	// encounter that reaches simultaneous exhaustion stays open until the
	// operator adds a daihyosen, and mergeKachinukiSubResults orders that row
	// last, so keying off the final slice element would advance off the rep
	// bout. Scan from the end past any daihyosen placeholder to the real bout.
	lastIdx := -1
	for i := len(parent.SubResults) - 1; i >= 0; i-- {
		if parent.SubResults[i].Position != state.DaihyosenSubPosition {
			lastIdx = i
			break
		}
	}
	if lastIdx < 0 {
		// Only the daihyosen placeholder is present: nothing to advance off.
		return false, nil
	}
	last := parent.SubResults[lastIdx]
	// Only act when the last bout has a final outcome. A bout written
	// with no Winner AND no Decision is still being scored; bail.
	hasOutcome := last.Winner != "" || last.Decision != ""
	if !hasOutcome {
		return false, nil
	}
	// Identity guard: retirement math needs to know WHO fought. A bout
	// carrying an outcome but no side names (e.g. a client that could not
	// resolve the lineup submitted a nameless hikiwake) retires nobody,
	// and advancing off it would append a wrong pairing and shift the
	// whole sequence by one. Refuse loudly and leave the match untouched
	// so the operator can correct the bout.
	if last.SideA == "" && last.SideB == "" {
		log.Printf("engine.MaybeAdvanceKachinuki compId=%s matchId=%s: last bout (position %d) has an outcome but no side names; skipping advancement", compID, matchID, last.Position)
		return false, nil
	}

	// Build remaining-roster snapshot. When a TeamLineup has been saved
	// for the team, use the full ordered roster filtered by bout-log
	// retirements (A2, GAP 1 / GAP 2a). Without a lineup the function
	// degrades to the bout-log-only heuristic so existing competitions
	// without lineups continue to work.
	remainingA, remainingB, rosterAvailable := e.kachinukiRemainingRoster(compID, matchID, comp, parent, roundIdx)

	out := AdvanceKachinuki(AdvanceKachinukiInput{
		LastBout: last,
		SideA:    remainingA,
		SideB:    remainingB,
	})
	log.Printf("engine.MaybeAdvanceKachinuki compId=%s matchId=%s rosterAvailable=%t result=%s",
		compID, matchID, rosterAvailable, describeKachinukiResult(out))

	// Simultaneous exhaustion: a pool or league encounter is a legitimate
	// draw (daihyosen is knockout-only), so finalize it as a hikiwake here.
	// A bracket encounter falls through to the running-state guard below and
	// stays open until the operator adds a daihyosen (scoring.go rejects a
	// winnerless bracket completion, AMENDMENT 2).
	if out.BothExhausted && !isBracket {
		out = AdvanceKachinukiResult{MatchEnded: true, Decision: state.DecisionDraw}
	}

	if !out.MatchEnded && out.Next == nil {
		return false, nil
	}

	// Persist via the matching atomic primitive.
	mutate := func(parent *state.MatchResult) {
		if out.MatchEnded {
			parent.Status = state.MatchStatusCompleted
			parent.Decision = out.Decision
			switch out.WinningSide {
			case "A":
				parent.Winner = parent.SideA
			case "B":
				parent.Winner = parent.SideB
			}
			return
		}
		// Append the next bout. The handler's broadcast carries the
		// updated subResults so SSE consumers see the new pairing.
		// Appending means the encounter continues: the parent match must
		// stay running with no match-level winner/decision.
		out.Next.Position = len(parent.SubResults) + 1
		parent.SubResults = append(parent.SubResults, *out.Next)
		parent.Status = state.MatchStatusRunning
		parent.Winner = ""
		parent.Decision = ""
	}

	if isBracket {
		if err := e.store.UpdateBracket(compID, func(bracket *state.Bracket) error {
			if bracket == nil {
				return notFoundErrorf("bracket not found for competition %s", compID)
			}
			for rIdx := range bracket.Rounds {
				for mIdx := range bracket.Rounds[rIdx] {
					if bracket.Rounds[rIdx][mIdx].ID == matchID {
						bm := &bracket.Rounds[rIdx][mIdx]
						if out.MatchEnded {
							// Finalize the bracket match and propagate the winner
							// to the next round so downstream SideA/SideB slots
							// are populated without a manual reload (GAP 4).
							bm.Status = state.MatchStatusCompleted
							bm.Decision = out.Decision
							switch out.WinningSide {
							case "A":
								bm.Winner = bm.SideA
							case "B":
								bm.Winner = bm.SideB
							}
							e.propagateBracketWinner(bracket, rIdx, mIdx)
						} else if out.Next != nil {
							appendNextKachinukiBout(bm, *out.Next)
						}
						return nil
					}
				}
			}
			// The Naginata 3rd-place (bronze) match is a sibling of
			// bracket.Rounds, not an element of it, so the loop above never
			// reaches it. Bronze is a terminal match: no propagation needed.
			if bm := bracket.ThirdPlaceMatch; bm != nil && bm.ID == matchID {
				if out.MatchEnded {
					bm.Status = state.MatchStatusCompleted
					bm.Decision = out.Decision
					switch out.WinningSide {
					case "A":
						bm.Winner = bm.SideA
					case "B":
						bm.Winner = bm.SideB
					}
				} else if out.Next != nil {
					appendNextKachinukiBout(bm, *out.Next)
				}
				return nil
			}
			return notFoundErrorf("bracket match %s not found", matchID)
		}); err != nil {
			return false, err
		}
		return true, nil
	}

	if found, err := e.store.UpdatePoolMatchByID(compID, matchID, mutate); err != nil {
		return false, err
	} else if !found {
		return false, nil
	}
	return true, nil
}

// applyKachinukiMerge merges an incoming kachinuki bout log into the stored
// prior log by position via mergeKachinukiSubResults. No-op for individual,
// fixed-format, or missing competitions. Shared by the locked and tx scoring
// paths so the merge guard cannot drift between them.
func applyKachinukiMerge(comp *state.Competition, prior, result *state.MatchResult) {
	if comp == nil || comp.TeamSize < 2 || comp.TeamMatchType != state.TeamMatchTypeKachinuki {
		return
	}
	var stored []state.SubMatchResult
	if prior != nil {
		stored = prior.SubResults
	}
	result.SubResults = mergeKachinukiSubResults(stored, result.SubResults)
}

// mergeKachinukiSubResults merges an incoming kachinuki bout log into
// the stored one BY POSITION (ACID: a client whose local log is behind
// the server, a stale modal, a debounced autosave, or a second operator,
// must never destroy server-appended bouts). Incoming entries overwrite
// the stored entry at the same position; stored entries absent from the
// incoming patch are preserved, whether they are unplayed placeholders
// appended by MaybeAdvanceKachinuki, completed bouts, or the position -1
// daihyosen. Output order: numbered positions ascending, daihyosen last,
// matching the append order the advancement logic relies on (the LAST
// entry drives AdvanceKachinuki).
func mergeKachinukiSubResults(stored, incoming []state.SubMatchResult) []state.SubMatchResult {
	byPos := make(map[int]state.SubMatchResult, len(stored)+len(incoming))
	for _, s := range stored {
		byPos[s.Position] = s
	}
	for _, s := range incoming {
		byPos[s.Position] = s
	}
	numbered := make([]int, 0, len(byPos))
	hasDaihyosen := false
	for p := range byPos {
		if p == state.DaihyosenSubPosition {
			hasDaihyosen = true
			continue
		}
		numbered = append(numbered, p)
	}
	sort.Ints(numbered)
	out := make([]state.SubMatchResult, 0, len(byPos))
	for _, p := range numbered {
		out = append(out, byPos[p])
	}
	if hasDaihyosen {
		out = append(out, byPos[state.DaihyosenSubPosition])
	}
	return out
}

// CheckKachinukiPrematureCompletion is the score handler's pre-write
// safety net (ACID: no silent drops, no silent acceptance of a bogus
// final). A status=completed write on a kachinuki team match is only
// legitimate when one of these holds:
//
//   - the write is a correction (the stored match is already completed),
//   - the patch carries a daihyosen sub-result (position -1), the
//     sanctioned tied-after-exhaustion resolution,
//   - the match-level decision is a withdrawal/default
//     (kiken*/fusenpai/fusensho), which finalizes without playing out
//     the roster,
//   - the roster snapshot derived from the incoming bout log says at
//     least one side is exhausted.
//
// Otherwise it returns ErrKachinukiPrematureCompletion (handler: 409).
// Non-kachinuki competitions and non-completed writes always pass.
// Must be called OUTSIDE the score transaction: the store loads here
// acquire the per-comp lock themselves.
func (e *Engine) CheckKachinukiPrematureCompletion(compID, matchID string, result *state.MatchResult) error {
	if result == nil || result.Status != state.MatchStatusCompleted {
		return nil
	}
	comp, err := e.store.LoadCompetition(compID)
	if err != nil {
		return err
	}
	if comp == nil || comp.TeamSize < 2 || comp.TeamMatchType != state.TeamMatchTypeKachinuki {
		return nil
	}
	// Withdrawals and defaults finalize a match without exhausting the
	// roster; they are legitimate completions.
	if domain.IsKikenDecisionStr(result.Decision) || result.Decision == string(domain.DecisionFusenpai) || result.Decision == string(domain.DecisionFusensho) {
		return nil
	}
	// A daihyosen sub-result is the sanctioned tie resolution, but only once
	// it carries a winner: an empty/unscored Position=-1 placeholder must not
	// let a premature completion (players still remaining) slip past. This
	// mirrors deriveDaihyosenWinner, which also requires sub.Winner != "".
	for _, sub := range result.SubResults {
		if sub.Position == state.DaihyosenSubPosition && sub.Winner != "" {
			return nil
		}
	}
	parent, _, roundIdx, err := e.findTeamMatch(compID, matchID)
	if err != nil {
		return err
	}
	if parent == nil {
		return nil // unknown match: the write path owns the 404
	}
	if parent.Status == state.MatchStatusCompleted {
		return nil // correction of a finished result
	}
	// Judge exhaustion from the MERGED bout log, mirroring the write path
	// (applyKachinukiMerge): the incoming bouts are overlaid onto the stored
	// log BY POSITION, so a partial or stale client log (one that omits
	// earlier server-appended bouts) cannot make this pre-check see fewer
	// retirements than the committed result will, and thus cannot falsely
	// 409 a legitimate exhaustion completion.
	probe := *parent
	if len(result.SubResults) > 0 {
		probe.SubResults = mergeKachinukiSubResults(parent.SubResults, result.SubResults)
	}
	remainingA, remainingB, _ := e.kachinukiRemainingRoster(compID, matchID, comp, &probe, roundIdx)
	if len(remainingA) == 0 || len(remainingB) == 0 {
		return nil // one side exhausted: a legitimate completion
	}
	return ErrKachinukiPrematureCompletion
}

// findTeamMatch locates a match by ID, returning the parent record (a
// copy), a flag indicating whether it was found in the bracket store
// rather than the pool store, and the bracket round index (0 for pool
// matches, rIdx for bracket matches, len(Rounds) for the ThirdPlaceMatch
// so round-scoped lineup resolution prefers the bronze's own stage,
// matching the client's derivedBracket.rounds.length).
func (e *Engine) findTeamMatch(compID, matchID string) (*state.MatchResult, bool, int, error) {
	poolMatches, err := e.store.LoadPoolMatches(compID)
	if err == nil {
		for i := range poolMatches {
			if poolMatches[i].ID == matchID {
				m := poolMatches[i]
				return &m, false, 0, nil
			}
		}
	}
	bracket, err := e.store.LoadBracket(compID)
	if err == nil && bracket != nil {
		for rIdx, round := range bracket.Rounds {
			for _, bm := range round {
				if bm.ID == matchID {
					return bracketMatchToTeamResult(bm), true, rIdx, nil
				}
			}
		}
		// The Naginata 3rd-place (bronze) match is a sibling of
		// bracket.Rounds, not an element of it; look it up here. Its
		// effective round index is len(Rounds) (one past the final round),
		// mirroring the client's derivedBracket.rounds.length so a
		// round-scoped lineup saved for the bronze stage resolves ahead of
		// an earlier round's lineup.
		if bm := bracket.ThirdPlaceMatch; bm != nil && bm.ID == matchID {
			return bracketMatchToTeamResult(*bm), true, len(bracket.Rounds), nil
		}
	}
	return nil, false, 0, nil
}

// bracketMatchToTeamResult projects a BracketMatch into the *MatchResult shape
// findTeamMatch returns for kachinuki lookups. It carries Court + ScheduledAt
// (unlike bracketMatchAsResult in bracket_result.go, which omits them and adds
// decision/encho/flag detail for the eligibility/rollback paths), so the two
// projections are deliberately distinct.
//
// SubResults is carried through by reference, same as bracketMatchAsResult:
// e.store.LoadBracket / tx.LoadBracket already deep-copy every BracketMatch
// (including SubResults, via Store.copyBracket) before handing the bracket
// back, so this projection is never aliased to the on-disk store cache. Only
// the FINDTEAMMATCH POOL branch's caller (MaybeAdvanceKachinuki's mutate
// closure) appends to a returned result's SubResults in place, and that
// closure only ever runs against the independently-loaded pool MatchResult
// from UpdatePoolMatchByID, never against a bracket-sourced result from this
// helper (the bracket branch mirrors Winner/Status directly onto the
// BracketMatch instead). If a future caller appends in place to a
// bracket-sourced result here, copy SubResults first (mirrors
// handlers_daihyosen.go's daihyosenBracketResult, which does exactly that for
// its own in-place-append call site).
func bracketMatchToTeamResult(bm state.BracketMatch) *state.MatchResult {
	return &state.MatchResult{
		ID:          bm.ID,
		SideA:       bm.SideA,
		SideB:       bm.SideB,
		Winner:      bm.Winner,
		Status:      bm.Status,
		Court:       bm.Court,
		ScheduledAt: bm.ScheduledAt,
		Decision:    bm.Decision,
		SubResults:  bm.SubResults,
	}
}

// kachinukiRemainingRoster derives the remaining un-retired roster per side
// for a team match. Returns (sideA, sideB, rosterAvailable). rosterAvailable
// is true when at least one side's roster was resolved from a saved TeamLineup;
// false means both sides fell back to the bout-log-only heuristic.
//
// Priority per side (AMENDMENT 1 / GAP 1 / GAP 2a):
//  1. Match-scoped lineup for this matchID.
//  2. Round-scoped lineup: highest round <= roundIdx.
//  3. Round-scoped lineup: highest round overall (fallback).
//  4. Bout-log-only heuristic (anyone who appeared in a bout, minus retired).
//
// The full ordered roster (from lineup.OrderedRoster) is filtered by
// RetiredPlayersFromBoutLog to produce the remaining queue.
func (e *Engine) kachinukiRemainingRoster(compID, matchID string, comp *state.Competition, parent *state.MatchResult, roundIdx int) ([]string, []string, bool) {
	retiredA, retiredB := RetiredPlayersFromBoutLog(parent.SubResults, parent.SideA, parent.SideB)

	// Attempt lineup-based roster resolution.
	lineups, err := e.store.LoadTeamLineups(compID)
	if err != nil {
		log.Printf("engine.kachinukiRemainingRoster compId=%s matchId=%s: lineup load error: %v; falling back to bout-log-only", compID, matchID, err)
		lineups = nil
	}

	// The lineup editor keys lineups by the team PARTICIPANT ID
	// (player.id, a UUID) while match sides carry the team display NAME,
	// so translate each side name to its participant ID and try both keys
	// ("match on id OR name"). A participant load failure only degrades
	// the lookup to name-only; the bout-log fallback below still applies.
	var participants []domain.Player
	if len(lineups) > 0 {
		participants, err = e.store.LoadParticipants(compID, comp.EffectiveWithZekkenName())
		if err != nil {
			log.Printf("engine.kachinukiRemainingRoster compId=%s matchId=%s: participant load error: %v; lineup lookup degrades to name-only", compID, matchID, err)
			participants = nil
		}
	}
	teamKeys := func(teamName string) []string {
		// Participant ID FIRST, then the display name. The lineup editor's
		// current storage key is the participant ID, so an id-keyed lineup
		// must win a same-round tie over a legacy name-keyed one:
		// FindBestLineupAny resolves same-tier ties by slice order. The name
		// stays as a fallback for lineups saved under it (older data, or a
		// team name that is not a participant id). Mirrors the id-first order
		// in kachinuki_export.go's teamKeys.
		var keys []string
		for _, p := range participants {
			if p.Name == teamName && p.ID != "" && p.ID != teamName {
				keys = append(keys, p.ID)
			}
		}
		return append(keys, teamName)
	}

	resolveRoster := func(teamName string, retired map[string]struct{}) ([]string, bool) {
		if lineups != nil {
			if lineup, found := state.FindBestLineupAny(lineups, teamKeys(teamName), matchID, roundIdx); found {
				full := lineup.OrderedRoster(comp.TeamSize)
				return FilterRemaining(full, retired), true
			}
		}
		// Preserve first-appearance order from the bout log: AdvanceKachinuki
		// treats this slice as an ordered queue (index 0 is the next fighter
		// in), so a map-iteration order would make the next pairing
		// nondeterministic when a kachinuki match runs without saved lineups.
		seen := map[string]struct{}{}
		out := make([]string, 0)
		isA := teamName == parent.SideA
		for _, b := range parent.SubResults {
			if b.Position == state.DaihyosenSubPosition {
				continue // rep bout, not a roster player (see RetiredPlayersFromBoutLog)
			}
			name := b.SideB
			if isA {
				name = b.SideA
			}
			if name == "" {
				continue
			}
			if _, dup := seen[name]; dup {
				continue
			}
			seen[name] = struct{}{}
			if _, gone := retired[name]; gone {
				continue
			}
			out = append(out, name)
		}
		return out, false
	}

	remainingA, foundA := resolveRoster(parent.SideA, retiredA)
	remainingB, foundB := resolveRoster(parent.SideB, retiredB)
	return remainingA, remainingB, foundA || foundB
}

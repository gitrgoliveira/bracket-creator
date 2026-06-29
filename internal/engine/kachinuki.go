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
	"fmt"
	"log"

	"github.com/gitrgoliveira/bracket-creator/internal/domain"
	"github.com/gitrgoliveira/bracket-creator/internal/state"
)

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
type AdvanceKachinukiResult struct {
	Next        *state.SubMatchResult
	MatchEnded  bool
	WinningSide string // "A" or "B" when MatchEnded; "" otherwise
	Decision    string // domain.DecisionKachinukiExhaustion when MatchEnded
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
//  4. Either side's queue is empty → MatchEnded=true, the non-empty
//     side wins by exhaustion. If BOTH are empty (no one left to
//     advance after a hikiwake), Side A is treated as the winner
//     defensively, log + return, but this is an unusual path because
//     the caller should have detected the previous-bout exhaustion
//     first.
//
// The function is pure: no I/O, no logging on the happy path. Unusual
// inputs (Winner not matching either side, all-empty queues after a
// hikiwake) log a warning so live-tournament operators get a breadcrumb
// when something downstream silently degraded.
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
// empty → defensively flag side A and log.
func advanceAfterHikiwake(in AdvanceKachinukiInput) AdvanceKachinukiResult {
	switch {
	case len(in.SideA) == 0 && len(in.SideB) == 0:
		// Both teams ran out simultaneously after a draw. The team
		// match ends; without a tiebreak in scope here, default to
		// SideA winning and log, operators can review and override.
		log.Printf("engine.AdvanceKachinuki: hikiwake exhausted both teams simultaneously at position %d; defaulting WinningSide=A",
			in.LastBout.Position)
		return AdvanceKachinukiResult{
			MatchEnded:  true,
			WinningSide: "A",
			Decision:    string(domain.DecisionKachinukiExhaustion),
		}
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
//  3. Build the remaining-roster snapshot per side. The Slice-7.C
//     first-cut leaves the FULL roster lookup to the lineup slice,
//     for now we treat the (currently-unset) Player.Side rosters as
//     unavailable and only act on the per-player winner path that
//     keeps the previous bout's stayer on for the next position. The
//     exhaustion-end and hikiwake-after-empty-queue cases need real
//     roster data and short-circuit with a no-op + log line.
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
// TODO(slice-7.B/D): once team-lineup persistence (state/team_lineup.go)
// + scheduling lands, replace the roster shortcut with a real lookup
// so the exhaustion-end branch works without operator intervention.
// FR-044, T135, T137.
func (e *Engine) MaybeAdvanceKachinuki(compID, matchID string) (bool, error) {
	comp, err := e.store.LoadCompetition(compID)
	if err != nil {
		return false, err
	}
	if comp == nil || comp.TeamSize < 2 || comp.TeamMatchType != state.TeamMatchTypeKachinuki {
		return false, nil
	}

	// Locate the parent match. Kachinuki is currently only meaningful
	// for pool matches (round-robin team matches), but check the
	// bracket too so a future playoffs integration doesn't silently
	// skip, the lookup is cheap.
	parent, isBracket, err := e.findTeamMatch(compID, matchID)
	if err != nil {
		return false, err
	}
	if parent == nil || len(parent.SubResults) == 0 {
		return false, nil
	}

	last := parent.SubResults[len(parent.SubResults)-1]
	// Only act when the last bout has a final outcome. A bout written
	// with no Winner AND no Decision is still being scored; bail.
	hasOutcome := last.Winner != "" || last.Decision != ""
	if !hasOutcome {
		return false, nil
	}

	// Build remaining-roster snapshot. Without TeamLineup data we
	// cannot enumerate the full team roster, see the TODO. The
	// first-cut behaviour: skip when we don't have rosters to feed
	// AdvanceKachinuki. The handler's responsibility is to detect this
	// and surface a log line; live competitions with kachinuki will
	// need the lineup slice landed first.
	//
	// We still proceed with EMPTY remaining rosters so exhaustion
	// detection works for cases where the bout log itself reveals
	// "B-Senpo lost in bout 1, B-Jiho lost in bout 2, …" with no
	// players left. AdvanceKachinuki sees empty queues → MatchEnded.
	// In practice this is the common signal until lineup integration
	// arrives.
	remainingA, remainingB, rosterAvailable := e.kachinukiRemainingRoster(compID, comp, parent)

	out := AdvanceKachinuki(AdvanceKachinukiInput{
		LastBout: last,
		SideA:    remainingA,
		SideB:    remainingB,
	})
	log.Printf("engine.MaybeAdvanceKachinuki compId=%s matchId=%s rosterAvailable=%t result=%s",
		compID, matchID, rosterAvailable, describeKachinukiResult(out))

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
		out.Next.Position = len(parent.SubResults) + 1
		parent.SubResults = append(parent.SubResults, *out.Next)
	}

	if isBracket {
		if err := e.store.UpdateBracket(compID, func(bracket *state.Bracket) error {
			if bracket == nil {
				return notFoundErrorf("bracket not found for competition %s", compID)
			}
			for rIdx := range bracket.Rounds {
				for mIdx := range bracket.Rounds[rIdx] {
					if bracket.Rounds[rIdx][mIdx].ID == matchID {
						// BracketMatch carries no SubResults; the
						// authoritative bout log lives on the parent
						// MatchResult side of the world. For kachinuki
						// finals we mirror Winner/Decision only.
						bm := &bracket.Rounds[rIdx][mIdx]
						if out.MatchEnded {
							bm.Status = state.MatchStatusCompleted
							bm.Decision = out.Decision
							switch out.WinningSide {
							case "A":
								bm.Winner = bm.SideA
							case "B":
								bm.Winner = bm.SideB
							}
						}
						return nil
					}
				}
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

// findTeamMatch locates a match by ID, returning the parent record (a
// copy) and a flag indicating whether it was found in the bracket
// store rather than the pool store.
func (e *Engine) findTeamMatch(compID, matchID string) (*state.MatchResult, bool, error) {
	poolMatches, err := e.store.LoadPoolMatches(compID)
	if err == nil {
		for i := range poolMatches {
			if poolMatches[i].ID == matchID {
				m := poolMatches[i]
				return &m, false, nil
			}
		}
	}
	bracket, err := e.store.LoadBracket(compID)
	if err == nil && bracket != nil {
		for _, round := range bracket.Rounds {
			for _, bm := range round {
				if bm.ID == matchID {
					return &state.MatchResult{
						ID:          bm.ID,
						SideA:       bm.SideA,
						SideB:       bm.SideB,
						Winner:      bm.Winner,
						Status:      bm.Status,
						Court:       bm.Court,
						ScheduledAt: bm.ScheduledAt,
						Decision:    bm.Decision,
					}, true, nil
				}
			}
		}
	}
	return nil, false, nil
}

// kachinukiRemainingRoster derives the remaining un-retired roster per
// side for a team match. Returns (sideA, sideB, available); `available`
// is false when the roster source is not yet wired in (Slice 7.C
// first-cut, see TODO in MaybeAdvanceKachinuki).
//
// The current implementation derives "remaining" purely from the bout
// log: anyone who has played and lost (or hikiwake'd) is retired; the
// remaining set is the set of bout SideA/SideB names that haven't
// played yet or are still standing. Without the full team roster
// (lineup integration), this won't include players that haven't yet
// been scheduled into a bout, so exhaustion detection will trip early
// in practice. The TODO points at the lineup slice fixing this.
func (e *Engine) kachinukiRemainingRoster(_ string, _ *state.Competition, parent *state.MatchResult) ([]string, []string, bool) {
	retiredA, retiredB := RetiredPlayersFromBoutLog(parent.SubResults, parent.SideA, parent.SideB)
	// Without a full roster source, surface the bout-log player set as
	// the "known names" universe per side. The handler logs
	// rosterAvailable=false so operators know the result may be
	// approximate.
	knownA := map[string]struct{}{}
	knownB := map[string]struct{}{}
	for _, b := range parent.SubResults {
		if b.SideA != "" {
			knownA[b.SideA] = struct{}{}
		}
		if b.SideB != "" {
			knownB[b.SideB] = struct{}{}
		}
	}
	collect := func(known, retired map[string]struct{}) []string {
		out := make([]string, 0, len(known))
		for name := range known {
			if _, gone := retired[name]; gone {
				continue
			}
			out = append(out, name)
		}
		return out
	}
	return collect(knownA, retiredA), collect(knownB, retiredB), false
}

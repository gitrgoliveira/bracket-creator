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
//   - The encounter can be won two ways: by EXHAUSTION (one side has no
//     remaining un-retired players) or by the TAISHO-DEFEATED rule (the
//     taisho -- always the last fighter -- loses, so their team loses).
//     Team sizes are unregulated and lineup vacancies are not enforced,
//     so the app's roster snapshot is advisory: the shiaijo OPERATOR
//     declares the end ("End match"), the engine never auto-finalizes
//     (mp-gmcg). Both win rules persist as
//     domain.DecisionKachinukiExhaustion.
//   - A tied final bout is a drawn encounter in pools/league; a knockout
//     tie is resolved by encho on that same bout (daihyosen does not
//     exist in kachinuki).
//
// AdvanceKachinuki encapsulates the pure decision logic. Callers
// (typically a score handler, see handlers_match.go) pass a snapshot
// of the just-completed bout plus the remaining un-retired roster per
// side, and the engine returns either the next bout to schedule or a
// MatchEnded sentinel (advisory, logged only).
package engine

import (
	"errors"
	"fmt"
	"log"
	"sort"
	"strings"
	"time"

	"github.com/gitrgoliveira/bracket-creator/internal/domain"
	"github.com/gitrgoliveira/bracket-creator/internal/state"
)

// kachinukiFighter names one un-retired fighter in an advancement queue: a
// display NAME (always present, exactly as before bc-tmid) plus the squad
// MEMBER id (empty for the bout-log-only heuristic, which has no lineup to
// draw an id from at all, or for a slot a legacy-repair has not yet
// reached). AdvanceKachinuki stamps whichever of these travels onto the
// next appended bout (appendNextKachinukiBout's Next), so a fighter's
// IDENTITY, not just their current name, rides forward onto the row that
// will eventually retire them -- see RetiredPlayersFromBoutLog /
// IsMemberRetired, the readers this exists to feed.
type kachinukiFighter struct {
	Name     string
	MemberID string
}

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
	SideA    []kachinukiFighter
	SideB    []kachinukiFighter
}

// AdvanceKachinukiResult is the engine's verdict. Under the operator-led
// contract (mp-gmcg) the verdict is ADVISORY: completion only ever happens
// through an explicit operator score write, never from this result.
//
//   - Next: when non-nil, the next bout to schedule. Position is set to
//     LastBout.Position + 1; SideA/SideB carry the next pair of player
//     names. Other fields are left zero, the score handler will fill
//     them as the bout is played. After a hikiwake that leaves exactly
//     ONE side without a replacement, Next keeps THE FIGHTER WHO JUST
//     TIED on that side (under the taisho rule they stay on; the
//     operator never re-types the name) paired against the surviving
//     side's next fighter. Under plain exhaustion that fighter is
//     actually out: the operator gives the survivor the per-bout
//     fusensho and Ends on that point — the walkover that expresses the
//     surviving team's win (spec 006 decision 2).
//   - MatchEnded: true when the last bout had a WINNER and the loser's
//     ADVISORY roster is empty (the decisive point already exists, so no
//     walkover slot is appended). Next is nil. WinningSide is "A" or
//     "B"; Decision is domain.DecisionKachinukiExhaustion. Team sizes
//     are unregulated, so the snapshot may be wrong; MaybeAdvanceKachinuki
//     logs this and simply appends nothing — it never finalizes the match.
//   - BothExhausted: true when a hikiwake retired the last SNAPSHOT player
//     on both teams simultaneously. MatchEnded is false. Also advisory:
//     the operator ends the encounter (pool/league tie → drawn encounter;
//     a knockout tie continues via a further bout or encho on the final
//     bout — daihyosen does not exist in kachinuki).
type AdvanceKachinukiResult struct {
	Next        *state.SubMatchResult
	MatchEnded  bool
	WinningSide string // "A" or "B" when MatchEnded; "" otherwise
	Decision    string // domain.DecisionKachinukiExhaustion when MatchEnded

	// BothExhausted is true only when a hikiwake retired the last player on
	// BOTH teams at once (no winner determinable) in the ADVISORY snapshot.
	// AdvanceKachinuki cannot pick a winner and MaybeAdvanceKachinuki takes
	// no action beyond logging; the operator decides how the encounter ends
	// (drawn in pools/league, continued via next bout or encho in a
	// knockout).
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
//  4. After a WIN, the loser's queue empty → MatchEnded=true, the winner's
//     side wins by exhaustion in the ADVISORY snapshot (the decisive point
//     already exists; nothing is appended). After a HIKIWAKE, exactly one
//     queue empty → append a slot keeping the FIGHTER WHO JUST TIED on
//     the replacement-less side, against the surviving side's next
//     fighter (taisho-rule continue with nothing to re-type; plain
//     exhaustion turns it into the walkover via per-bout fusensho —
//     spec 006 decision 2). BOTH empty → BothExhausted=true and no
//     winner. In every case the caller (MaybeAdvanceKachinuki) never
//     finalizes — the match stays running until the operator ends it
//     with an explicit completed score write (mp-gmcg operator-led
//     contract).
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

	// Which side stayed on is decided by the ONE owner of "which side won
	// this bout" (domain.SubBoutAttribution + AttributeWinnerSide): the
	// row's member ids first, its names second. A fighter picked by squad
	// number before being named (bc-dnst) has an empty name and a member
	// id, and the client records such a winner as its TEAM name plus the
	// member id, so a name comparison alone could never see it stay on.
	if hikiwake {
		return advanceAfterHikiwake(in)
	}
	side := domain.AttributeWinnerSide(domain.SubBoutAttribution(last.Attribution()))
	if side == domain.MatchSideNone {
		// The same residue RetiredPlayersFromBoutLog keeps, for the same
		// reason: a row the ids cannot settle (no ids, two fighters sharing
		// a name) still answers by name, side A first, because that
		// function has just RETIRED the loser on that answer and a queue
		// whose head never clears is a stuck encounter, while an arbitrary
		// pairing is a wrong-but-recoverable one the operator can correct.
		switch {
		case last.Winner != "" && last.Winner == last.SideA:
			side = domain.MatchSideA
		case last.Winner != "" && last.Winner == last.SideB:
			side = domain.MatchSideB
		}
	}
	switch side {
	case domain.MatchSideA:
		return advanceWinnerStays(kachinukiFighter{Name: last.SideA, MemberID: last.SideAMemberID}, last.Position, in.SideB, "A")
	case domain.MatchSideB:
		return advanceWinnerStays(kachinukiFighter{Name: last.SideB, MemberID: last.SideBMemberID}, last.Position, in.SideA, "B")
	default:
		// Unexpected: Winner is set but doesn't match either bout
		// side by id or by name. Treat as a no-op (no advancement) so
		// callers fall back to manual scheduling instead of silently
		// producing a wrong pairing.
		log.Printf("engine.AdvanceKachinuki: unrecognized bout outcome, winner=%q sideA=%q sideB=%q decision=%q; no advancement",
			last.Winner, last.SideA, last.SideB, last.Decision)
		return AdvanceKachinukiResult{}
	}
}

// advanceWinnerStays builds the next-bout descriptor when one side's
// player stays on. The opposing side's queue (`oppQueue`) must contain
// the next un-retired opponent at index 0. The `winnerSide` param is
// just for the exhaustion-end path's WinningSide field. `staying` carries
// the member id alongside the name (bc-tmid pass 2) so the appended row
// keeps the fighter's IDENTITY, not just the name they currently answer to.
func advanceWinnerStays(staying kachinukiFighter, lastPos int, oppQueue []kachinukiFighter, winnerSide string) AdvanceKachinukiResult {
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
	var sideA, sideB kachinukiFighter
	if winnerSide == "A" {
		sideA, sideB = staying, nextOpp
	} else {
		sideA, sideB = nextOpp, staying
	}
	return AdvanceKachinukiResult{
		Next: &state.SubMatchResult{
			Position:      lastPos + 1,
			SideA:         sideA.Name,
			SideAMemberID: sideA.MemberID,
			SideB:         sideB.Name,
			SideBMemberID: sideB.MemberID,
		},
	}
}

// advanceAfterHikiwake builds the next-bout descriptor after a tie.
// Both sides have a replacement → both retire, pair the heads of each
// remaining queue. Exactly one side without a replacement → the fighter
// who just tied STAYS on the slot (under the taisho rule a drawing
// Taisho continues; the operator never re-types the name), paired
// against the surviving side's next fighter — under plain exhaustion
// the operator gives the survivor the per-bout fusensho and Ends on
// that point (spec 006 decision 2: the extra bout IS how the win is
// expressed). Both empty → BothExhausted (advisory: the operator ends
// the encounter, the engine never finalizes — mp-gmcg).
func advanceAfterHikiwake(in AdvanceKachinukiInput) AdvanceKachinukiResult {
	switch {
	case len(in.SideA) == 0 && len(in.SideB) == 0:
		// Both teams ran out simultaneously after a draw, per the ADVISORY
		// roster snapshot. The engine cannot determine a winner; flag
		// BothExhausted and append nothing. The operator ends the encounter:
		// drawn in pools/league, continued (next bout or encho) in a
		// knockout. See MaybeAdvanceKachinuki.
		log.Printf("engine.AdvanceKachinuki: hikiwake exhausted both teams simultaneously at position %d (advisory); operator ends the encounter",
			in.LastBout.Position)
		return AdvanceKachinukiResult{BothExhausted: true}
	case len(in.SideA) == 0:
		// SideA's ADVISORY roster has no replacement but SideB does: the
		// fighter who just tied STAYS ON THE SLOT (operator ruling: under
		// the taisho rule a drawing Taisho continues, and the operator must
		// not have to re-type the name), paired against SideB's next
		// fighter. The slot is advisory like every append and serves both
		// modes: under plain exhaustion the tied fighter is actually out,
		// so the operator gives the surviving fighter the per-bout fusensho
		// and Ends on that point (the walkover); under the taisho rule the
		// pairing is fought as-is. An abandoned trailing unscored slot is
		// stripped on the completed write.
		log.Printf("engine.AdvanceKachinuki: hikiwake left side A without a replacement at position %d (advisory); pairing %s against %s",
			in.LastBout.Position, in.LastBout.SideA, in.SideB[0].Name)
		return AdvanceKachinukiResult{
			Next: &state.SubMatchResult{
				Position:      in.LastBout.Position + 1,
				SideA:         in.LastBout.SideA,
				SideAMemberID: in.LastBout.SideAMemberID,
				SideB:         in.SideB[0].Name,
				SideBMemberID: in.SideB[0].MemberID,
			},
		}
	case len(in.SideB) == 0:
		log.Printf("engine.AdvanceKachinuki: hikiwake left side B without a replacement at position %d (advisory); pairing %s against %s",
			in.LastBout.Position, in.SideA[0].Name, in.LastBout.SideB)
		return AdvanceKachinukiResult{
			Next: &state.SubMatchResult{
				Position:      in.LastBout.Position + 1,
				SideA:         in.SideA[0].Name,
				SideAMemberID: in.SideA[0].MemberID,
				SideB:         in.LastBout.SideB,
				SideBMemberID: in.LastBout.SideBMemberID,
			},
		}
	}
	return AdvanceKachinukiResult{
		Next: &state.SubMatchResult{
			Position:      in.LastBout.Position + 1,
			SideA:         in.SideA[0].Name,
			SideAMemberID: in.SideA[0].MemberID,
			SideB:         in.SideB[0].Name,
			SideBMemberID: in.SideB[0].MemberID,
		},
	}
}

// RetiredMemberSet records everyone who has retired on ONE side of a
// kachinuki encounter, keyed BOTH ways at once (bc-tmid pass 2): by squad
// member id, when the retiring bout row carries one, and by display name,
// always, when the row names one. Both are populated from the SAME
// retirement event, never independently, so they can never disagree about
// WHO retired -- only about which key a later lookup can use to find them.
// IsMemberRetired is the one place that decides which key wins.
type RetiredMemberSet struct {
	IDs   map[string]struct{}
	Names map[string]struct{}
	// nameOnly holds the names of retirements that carried NO member id, so
	// Count can tally distinct fighters as ids plus id-less names: a fighter
	// fielded by squad number before being named (bc-dnst) retires under an
	// id and an empty name, and a count of Names alone would miss them.
	nameOnly map[string]struct{}
	// namedByID holds every name that retired ALONGSIDE a member id. It exists
	// only so Count can tell a second, id-less row for a fighter ALREADY
	// counted through their id from a genuinely different id-less fighter.
	namedByID map[string]struct{}
}

func newRetiredMemberSet() RetiredMemberSet {
	return RetiredMemberSet{
		IDs: map[string]struct{}{}, Names: map[string]struct{}{},
		nameOnly: map[string]struct{}{}, namedByID: map[string]struct{}{},
	}
}

// Count is the number of distinct fighters retired: every id-carrying
// retirement, plus every name-only retirement for a fighter no id already
// counted.
//
// The second clause is why this is not len(IDs)+len(nameOnly). One fighter can
// be recorded BOTH ways -- reopen an encounter and re-score a bout through a
// path that omits the member id while the original row still carries it -- and
// the naive sum then reported two eliminations for a team that lost one
// fighter, straight into the exported Kachinuki Detail sheet.
//
// Two teammates genuinely sharing a display name, one retiring with an id and
// one without, still collapse to one. That is the same name ambiguity
// ambiguousFighterNames exists for, and under-counting it is the safe
// direction: a tally that is short never ends an encounter early.
func (r RetiredMemberSet) Count() int {
	n := len(r.IDs)
	for name := range r.nameOnly {
		if _, alsoByID := r.namedByID[name]; alsoByID {
			continue
		}
		n++
	}
	return n
}

// retire records one retirement: name (when non-empty) into Names,
// memberID (when non-empty) into IDs. A row missing its member id (an
// unrepaired legacy row, or one the bout-log-only heuristic synthesised)
// simply contributes nothing to IDs -- IsMemberRetired's name fallback is
// what still finds it.
func (r RetiredMemberSet) retire(name, memberID string) {
	if name != "" {
		r.Names[name] = struct{}{}
	}
	if memberID != "" {
		r.IDs[memberID] = struct{}{}
		if name != "" {
			r.namedByID[name] = struct{}{}
		}
	} else if name != "" {
		r.nameOnly[name] = struct{}{}
	}
}

// ambiguousFighterNames returns the names carried by MORE THAN ONE entry of
// roster. A retirement recorded under such a name cannot say WHICH of them
// retired, so IsMemberRetired refuses to act on it. Members of one team may
// legally share a display name -- the uniqueness rule is grandfathered, so
// rosters written before it exist and still load.
//
// Deliberately not helper.DuplicateNamesWithKeys, which answers a nearby
// question differently: it folds names through NormalizeParticipantName and
// reports the FIRST-SEEN label of each duplicate group. Both differences
// break this use. The set it gates (RetiredMemberSet.Names) holds the raw
// names bout rows store, and the lookup compares a fighter's own raw name
// against it, so a normalised set would be keyed differently from the thing
// it guards; and a first-seen label misses the other spellings, which is the
// silent-miss direction. Reusing it would mean normalising the retirement
// comparison too, a change to what that set means rather than a tidy-up.
func ambiguousFighterNames(roster []kachinukiFighter) map[string]struct{} {
	count := make(map[string]int, len(roster))
	for _, f := range roster {
		if f.Name != "" {
			count[f.Name]++
		}
	}
	out := make(map[string]struct{})
	for name, n := range count {
		if n > 1 {
			out[name] = struct{}{}
		}
	}
	return out
}

// IsMemberRetired reports whether fighter f -- named, and possibly
// id-stamped, exactly like a bout-log side or a lineup slot -- has retired
// per `retired`. ambiguousNames is the set of names more than one member of
// f's OWN roster carries (ambiguousFighterNames); pass nil when there is no
// roster to compute it from.
//
// The member id wins whenever f carries one (operator ruling bc-pnum, "a
// record that carries an id field is resolved by id only"): a renamed
// member's slot keeps ITS id, so a retirement recorded under the member's
// OLD name is still found -- this is the mechanism that closes the rename
// defect this pass exists for.
//
// An id MISS then falls through to the name, and that tier is not a
// weakening of the id rule but the only way to read a row that has no id to
// be resolved by. The two keys live on opposite ends of one comparison:
// f's id comes from a LINEUP SLOT, which the load-time repair fills, while
// the retirement was recorded by a BOUT ROW, which the same repair can only
// fill when that row's name resolves to exactly one squad member. A row it
// could not resolve -- or one written after it ran, by a client too old to
// send ids -- contributes a name and nothing else, so an id-only check
// finds nothing and puts a fighter who has already lost back in the queue,
// ahead of the reserve who has not. That is the defect this tier exists for,
// and it is the same defect the id rule was introduced to fix, arriving
// through the other door.
//
// The gate is what keeps it honest: the fallback applies only when f's name
// belongs to f alone on this roster, because a shared name cannot say which
// teammate the row meant. One case stays wrong and cannot be fixed from
// stored data: a member renamed AFTER losing, whose old name a teammate now
// carries, retires that teammate instead. Nothing links the old name to the
// id, so every rule guesses there; this one guesses in the direction that
// keeps a beaten fighter out of the queue.
func IsMemberRetired(f kachinukiFighter, retired RetiredMemberSet, ambiguousNames map[string]struct{}) bool {
	if f.MemberID != "" {
		if _, ok := retired.IDs[f.MemberID]; ok {
			return true
		}
		if _, ambiguous := ambiguousNames[f.Name]; ambiguous {
			return false
		}
	}
	_, ok := retired.Names[f.Name]
	return ok
}

// RetiredPlayersFromBoutLog walks a bout log and returns, per side, the set
// of fighters that have retired (lost or hikiwake'd out) up to and
// including the supplied log.
//
// Helper for callers building AdvanceKachinukiInput.{SideA,SideB} from a
// roster; they filter the initial roster by IsMemberRetired against the
// returned sets to derive the remaining un-retired queue. BOTH branches of
// kachinukiRemainingRoster do that through filterRemainingFighters -- the
// lineup-resolved one and the bout-log-only heuristic alike -- because the
// name tier needs the whole roster as context and cannot judge one fighter
// at a time. The heuristic branch simply supplies fighters whose MemberID
// is empty.
//
// teamAName / teamBName are the parent MatchResult.SideA / SideB
// (the team names), used to disambiguate which side won each bout.
func RetiredPlayersFromBoutLog(boutLog []state.SubMatchResult, teamAName, teamBName string) (retiredA, retiredB RetiredMemberSet) {
	retiredA, retiredB = newRetiredMemberSet(), newRetiredMemberSet()
	for _, b := range boutLog {
		if b.Position == state.DaihyosenSubPosition {
			// The daihyosen (rep bout) is not a kachinuki bout: its side
			// names are the representatives (often the team names), not
			// roster players, so it must not retire anyone.
			continue
		}
		hikiwake := state.IsDraw(b.Decision)
		if hikiwake {
			retiredA.retire(b.SideA, b.SideAMemberID)
			retiredB.retire(b.SideB, b.SideBMemberID)
			continue
		}
		// Map per-bout winner to the team side. MEMBER IDS FIRST (operator
		// ruling bc-pnum): who won decides who stays on, so the same
		// same-name hazard that used to hand an individual victory to the
		// wrong team also used to retire the wrong fighter here, which is
		// worse -- it changes who fights next. The ids settle it whenever
		// the row carries them, which every bout scored since the editor
		// began stamping the winner's member id does.
		//
		// The name switch below still answers for a row they cannot settle,
		// aka-first on a same-name pair exactly as before. That residue is
		// deliberately NOT converted into "nobody retires": a kachinuki
		// queue whose head never clears would re-offer the same pairing,
		// turning a wrong-but-recoverable answer into a stuck encounter. A
		// team-name match on the parent (b.Winner == teamAName) is the
		// legacy synth path from quick-score; the per-player path keys on
		// the bout's own side names.
		switch domain.AttributeWinnerSide(domain.SubBoutAttribution(b.Attribution())) {
		case domain.MatchSideA:
			retiredB.retire(b.SideB, b.SideBMemberID)
			continue
		case domain.MatchSideB:
			retiredA.retire(b.SideA, b.SideAMemberID)
			continue
		}
		if b.Winner == "" {
			// No outcome yet (the pending pairing the engine appended, or a
			// bout still being scored) retires nobody. Without this the
			// name switch below matched an empty Winner against an empty
			// side NAME, which a fighter fielded by squad number and not
			// yet named (bc-dnst) now legitimately has, and retired that
			// row's opponent before the bout was fought.
			continue
		}
		switch b.Winner {
		case b.SideA, teamAName:
			// SideA player stays; SideB player retires.
			retiredB.retire(b.SideB, b.SideBMemberID)
		case b.SideB, teamBName:
			retiredA.retire(b.SideA, b.SideAMemberID)
		}
	}
	return retiredA, retiredB
}

// filterRemainingFighters filters a slice of kachinukiFighter (name plus a
// possible member id) via IsMemberRetired, preserving order. It is the ONE
// filter kachinukiRemainingRoster uses, on both branches: the
// lineup-resolved one passes fighters carrying member ids, the bout-log-only
// heuristic passes fighters whose MemberID is empty and lets the name tier
// decide. Routing the heuristic through here too is deliberate and is the
// fix for the shared-name defect its call site describes; there is no
// name-only twin left to drift from.
func filterRemainingFighters(roster []kachinukiFighter, retired RetiredMemberSet) []kachinukiFighter {
	// The roster IS the context IsMemberRetired's name tier needs: whether a
	// name belongs to one member of this team or to several is a fact about
	// this slice, computed once here rather than re-derived per fighter.
	ambiguous := ambiguousFighterNames(roster)
	out := make([]kachinukiFighter, 0, len(roster))
	for _, f := range roster {
		if IsMemberRetired(f, retired, ambiguous) {
			continue
		}
		out = append(out, f)
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
	bm.WinnerID = "" // bc-brid: the verdict's id half, cleared with the name.
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
//     seen in the bout log when no lineup is saved.
//  4. Pass to AdvanceKachinuki. When it returns Next, append the bout
//     to SubResults and persist (status stays Running). It NEVER
//     finalizes the parent match: completion is operator-led (mp-gmcg,
//     the out.Next == nil path below). A MatchEnded/BothExhausted verdict
//     is advisory only (team sizes are unregulated, so the roster snapshot
//     may be incomplete); the operator ends the encounter with an explicit
//     completed score write from the score editor.
//
// Reports (advanced, postLog, err): `advanced` is whether SubResults or the
// parent match was mutated (the handler uses it to decide whether to emit an
// extra match-updated SSE event), and `postLog` is the FULL bout log AFTER the
// append when advanced is true (nil otherwise). Returning the log lets the
// caller echo the appended pairing to the open editor without re-reading the
// match from the store — the read this replaced was ~the 9th store read on a
// request already doing several, once per advancing bout, live (mp-gmcg review
// E1).
//
// FR-044, T135, T137.
func (e *Engine) MaybeAdvanceKachinuki(compID, matchID string) (bool, []state.SubMatchResult, error) {
	comp, err := e.store.LoadCompetition(compID)
	if err != nil {
		return false, nil, err
	}
	if !comp.IsKachinuki() {
		return false, nil, nil
	}

	// Locate the parent match in either the pool or bracket store:
	// advancement runs in both (bracket bouts append via
	// appendNextKachinukiBout, with propagateBracketWinner on
	// exhaustion).
	parent, isBracket, roundIdx, err := e.findTeamMatch(compID, matchID)
	if err != nil {
		return false, nil, err
	}
	if parent == nil || len(parent.SubResults) == 0 {
		return false, nil, nil
	}
	// A completed match is final: corrections re-submit the bout log of a
	// finished match and must never re-run advancement (which would append
	// a phantom next bout onto the completed result). Defense in depth on
	// top of the handler's kachinukiBoutFinal gating.
	if parent.Status == state.MatchStatusCompleted {
		return false, nil, nil
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
		return false, nil, nil
	}
	last := parent.SubResults[lastIdx]
	// Only act when the last bout has a final outcome. A bout written
	// with no Winner AND no Decision is still being scored; bail.
	hasOutcome := last.Winner != "" || last.Decision != ""
	if !hasOutcome {
		return false, nil, nil
	}
	// Identity guard: retirement math needs to know WHO fought. A bout
	// carrying an outcome but no side identity at all (e.g. a client that
	// could not resolve the lineup submitted a nameless hikiwake) retires
	// nobody, and advancing off it would append a wrong pairing and shift
	// the whole sequence by one. Refuse loudly and leave the match
	// untouched so the operator can correct the bout. A side is identified
	// by its name OR its member id: a fighter picked by squad number and
	// not yet named (bc-dnst) carries only the id, and that is enough.
	if last.SideA == "" && last.SideAMemberID == "" && last.SideB == "" && last.SideBMemberID == "" {
		log.Printf("engine.MaybeAdvanceKachinuki compId=%s matchId=%s: last bout (position %d) has an outcome but no side names; skipping advancement", compID, matchID, last.Position)
		return false, nil, nil
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

	// Operator-led completion (mp-gmcg): the engine NEVER auto-finalizes a
	// kachinuki encounter. Team sizes are unregulated and lineup vacancies
	// are not enforced, so the roster snapshot above is advisory: a side
	// that looks exhausted may still have fighters the app has never seen.
	// MatchEnded/BothExhausted are logged (breadcrumb above) but not acted
	// on; the shiaijo operator ends the match explicitly from the score
	// editor ("End match"), which arrives as a normal completed score write
	// (winner from the last decisive bout, covering both win rules -- full
	// exhaustion AND taisho-defeated -- or hikiwake in pools/league; a
	// knockout tie is resolved by encho on the final bout, never a draw).
	if out.Next == nil {
		return false, nil, nil
	}

	// postLog captures the FULL bout log AFTER the append, so the caller can
	// echo it without re-reading the match (E1). A fresh slice header keeps it
	// independent of the store's parse buffer.
	var postLog []state.SubMatchResult

	if isBracket {
		// UpdateBracketMatchByID owns the rounds → bronze-sibling walk, so the
		// append site no longer re-implements it (and can't forget the bronze).
		found, err := e.store.UpdateBracketMatchByID(compID, matchID, func(bm *state.BracketMatch) {
			appendNextKachinukiBout(bm, *out.Next)
			postLog = append([]state.SubMatchResult(nil), bm.SubResults...)
		})
		if err != nil {
			return false, nil, err
		}
		if !found {
			return false, nil, notFoundErrorf("bracket match %s not found", matchID)
		}
		return true, postLog, nil
	}

	found, err := e.store.UpdatePoolMatchByID(compID, matchID, func(parent *state.MatchResult) error {
		// Append the next bout. Appending means the encounter continues: the
		// parent match must stay running with no match-level winner/decision.
		out.Next.Position = len(parent.SubResults) + 1
		parent.SubResults = append(parent.SubResults, *out.Next)
		parent.Status = state.MatchStatusRunning
		parent.Winner = ""
		parent.WinnerID = "" // bc-brid: the verdict's id half, cleared with the name.
		parent.Decision = ""
		postLog = append([]state.SubMatchResult(nil), parent.SubResults...)
		return nil
	})
	if err != nil {
		return false, nil, err
	}
	if !found {
		return false, nil, nil
	}
	return true, postLog, nil
}

// Sentinel errors for the operator-led reopen path (mp-gmcg, spec 006
// decision 4). Both map to HTTP 409 in the handler, as does the third
// reopen conflict — a busy court — which deliberately reuses the existing
// ErrCourtBusy / *CourtBusyError pair (eligibility.go) rather than minting
// a reopen-specific twin, so the "court already has a running match"
// condition has exactly ONE sentinel and one wire shape across the score
// and reopen paths.
var (
	// ErrReopenNotCompleted: only a COMPLETED match can be reopened; a
	// running match needs no reopen and a scheduled one has nothing to
	// reopen.
	ErrReopenNotCompleted = errors.New("match is not completed; only a completed match can be reopened")
	// ErrReopenDownstreamFought is retractPropagatedWinner's own defensive
	// backstop re-check at the actual mutation point (mp-gmcg review), not
	// the primary interactive refusal: an operator reopening through the
	// normal doors sees *DownstreamKnockoutRunningError instead (bc-cse),
	// raised earlier in the same transaction by reopenBracketDownstreamCheck.
	// A downstream knockout match CLOSED with a result of its own is not
	// this error either: that one is the operator's to confirm
	// (DownstreamKnockoutPlayedError, see reopenBracketDownstreamCheck), and
	// one a bye resolved is unwound, not refused. What reaches a client
	// through this sentinel is only a downstream row no write path produces
	// (see reopenBracketDownstreamCheck's last paragraph).
	ErrReopenDownstreamFought = errors.New("cannot reopen: a downstream knockout match has already started or recorded a result")
	// ErrRemoveBoutNotRunning: bouts are removed from a RUNNING encounter only.
	// A completed kachinuki match already had its trailing unscored bouts
	// stripped on the End-match write, so there is nothing to remove; edit a
	// finished result via reopen/correction instead.
	ErrRemoveBoutNotRunning = errors.New("match is not running; reopen it before removing a bout")
	// ErrNoRemovableBout: the encounter has no trailing UNSCORED bout to drop —
	// the current bout is already scored, or there are no bouts. Surfaced so the
	// operator learns the empty-bout undo had nothing to act on rather than
	// getting a silent no-op.
	ErrNoRemovableBout = errors.New("no unscored bout to remove; only an empty appended bout can be removed")
)

// ReopenMatch is the sanctioned "Reopen match" path for a COMPLETED match
// that is either a kachinuki team match (mp-gmcg, spec 006 decision 4) or
// any match, team or individual, decided by a WITHDRAWAL or a DEFAULT WIN
// (domain.IsDefaultWinDecisionStr: kiken/kiken-voluntary/kiken-injury,
// fusenpai, or fusensho -- operator ruling 2026-09-24: "Everything should be
// able to be fixed, in case of a wrong entry"; widened to fusensho by
// bc-cse, "Clear default win and reopen"): status back to running, the
// match-level verdict cleared, the bouts already fought kept, so the
// operator scores what is left and finishes the match again through the
// normal score path. For a withdrawal or default win that is the whole
// point: the opponent received the default score, so removing one means the
// match was never decided. The gate is reopenResultPreconditionTx.
//
// `reason` is an OPTIONAL audit justification, persisted as the match's
// CorrectionReason when supplied. Reopening is the only way to rewrite a
// finalized kachinuki result without going through the score path's
// correction gate (which requires a correctionReason of its own), so the
// justification cannot simply be dropped — but demanding it HERE was too
// much friction: an operator who ended a match by mistake, at a shiaijo,
// mid-session, had to compose a reason before they could get back in.
// Reopen is therefore one tap, and ending the match again asks for no
// reason either (operator ruling 2026-09-25: a match can be reopened without
// any reason, and nothing is gated on that). When no reason is given the
// match is flagged ReopenPending, which only lets a reason sent with the next
// completion be kept as its correction reason; that completion clears it.
//
// The flag is persisted rather than held client-side because the score
// editor mounts per match: navigating away and back would lose it.
//
// A supplied reason is trimmed here so a padded reason can never be
// persisted; the caller (the reopen handler) rejects an oversized one.
//
// COURT GATE. Reopening puts the match back in the RUNNING state, and
// court exclusivity keys purely on `status == running`
// (courtOccupied / checkCourtExclusivityTx). Reopening a match
// whose court already has a running match would therefore leave TWO
// running matches on one court, and the exclusivity check then rejects
// BOTH of them: the re-End of the reopened match AND every further score
// write to the genuinely live bout — i.e. reopening a past match would
// stop the operator scoring the match actually being fought. So a busy
// court REJECTS the reopen (*CourtBusyError, HTTP 409 court_busy).
// Semantically that is also the right answer: a reopen means "we need to
// fight more bouts", which really does take the court. The common case —
// a plain completed -> completed correction — is unaffected: it never
// re-enters the running state and the score handler's isCorrection skip
// already lets it through on a busy court. DO NOT remove this guard as a
// redundant-looking check.
//
// The one reopen that does NOT take the court is a match-level fusensho whose
// barred competitor is still barred: it goes back to SCHEDULED
// (reopenTargetStatus), so neither gate applies to it. It used to be refused
// court_busy whenever another bout was running on its court, and the only
// remedy offered (requeue the court's occupant) wiped that live bout to free
// a court the reopen never takes. Whether the reopen lands scheduled is read
// before the cross-competition gate (checkTargetReopenable) and HELD inside
// the tx, so a reinstatement landing in between cannot turn a gate-skipped
// reopen into a running match; that competitor's match simply starts from
// the queue the normal way.
//
// The cross-competition half runs BEFORE WithTransaction
// (CheckCrossCompCourtBusy takes read locks on other competitions, so
// calling it while holding this competition's write lock risks a
// circular wait); the same-competition half runs inside the tx off the
// same courtFreeInCompTxWith the score path reaches via
// checkCourtExclusivityTx. Both are skipped when the match has no court
// assigned.
//
// Any other completed match (not kachinuki, not decided by a withdrawal or
// default win) returns a *ValidationError (HTTP 400): the correction path
// (completed -> completed with a correctionReason) remains its sanctioned
// edit. The score
// path's stale-write guard (a plain running write against a completed match
// silently no-ops) is intentionally untouched; reopen is explicit and
// separate.
//
// Bracket matches: the completed result may already have been propagated
// downstream (propagateBracketWinner fills the next round's slot, and a
// semifinal feeds its loser to the bronze match). When the downstream slot
// is merely filled but unfought, the slot is reset the same way generation
// fills it: the next-round side returns to its "Winner of rX-mY" placeholder
// (the exact string propagateBracketWinner re-resolves on the next
// completion) and the bronze side to empty. A next-round match a BYE resolved
// off this winner (nobody fought it) is unwound the same way, back to the
// completed-with-no-winner shape generation gave it, and so is every further
// bye the winner was passed through; the match past them answers as the next
// round does here (propagatedDownstreamOf). A downstream match CLOSED with a
// result of its own is the operator's call, exactly as on a knockout
// correction (bc-kcdg, operator ruling 2026-09-24: "The operator just needs
// to be aware of the consequences"): refused with
// *DownstreamKnockoutPlayedError naming it, and on a retry with
// opts.Force it is reopened for re-entry (forceReopenDownstreamChain, one
// hop past any byes) and its id reported through opts.Reopened. A downstream
// match that is RUNNING is still refused outright -- through the
// operator-facing doors that is *DownstreamKnockoutRunningError, raised by
// reopenBracketDownstreamCheck earlier in the same transaction;
// ErrReopenDownstreamFought is only the defensive re-check at the actual
// mutation point (see its own doc comment above), not what a client normally
// receives.
//
// ELIGIBILITY. A reopen clears the match's decision, so reopening a match a
// withdrawal (kiken, fusenpai) ended removes that withdrawal, and the
// eligibility record follows the ruling exactly as on the score path
// (restoreIfWithdrawalRemoved, operator ruling 2026-09-24: "Everything should
// be able to be fixed, in case of a wrong entry"): the competitor it barred is
// restored in the same transaction as the reopen, and the restored status is
// returned so the handler broadcasts competitor_status_updated. nil when the
// reopened result was not a withdrawal -- which now INCLUDES a match-level
// fusensho (domain.IsWithdrawalDecisionStr, scoring_tx.go, deliberately
// excludes it): recording a fusensho never wrote a CompetitorStatus for
// anyone, so there is nothing to restore, and reopening one correctly
// restores nobody's eligibility either. A downstream match force-reopened
// with it gets the same treatment (restoreForceReopened), reported on its
// opts.Reopened entry's Restored.
//
// STAMP. The reopen sets ModifiedAt to the server's now (reopenPoolMatch,
// reopenBracketMatch), so a write stamped before it cannot win
// last-write-wins against it and complete the running match again.
func (e *Engine) ReopenMatch(compID, matchID, reason string, opts ...ForceOptions) (*domain.CompetitorStatus, error) {
	fo := firstForceOptions(opts)
	comp, err := e.store.LoadCompetition(compID)
	if err != nil {
		return nil, err
	}

	// Court exclusivity is held HERE, inside the engine, so the
	// cross-competition pre-check and the same-competition in-transaction check
	// are atomic BY CONSTRUCTION — not by every caller remembering to wrap the
	// call in WithCourtExclusivityLock (mp-gmcg review A2). Without it a
	// concurrent match-start in ANOTHER competition could pass its own
	// cross-comp check and commit between our two halves, re-creating the
	// two-running-matches-on-one-court wedge the reopen court gate exists to
	// prevent. e.store owns the lock; nothing below re-takes it (non-reentrant),
	// and the ordering is courtCheckMu → per-comp lock, the same as the score
	// path. The LoadCompetition/validation above stay outside: they touch no
	// court state, so a bad-input rejection need not serialize on the lock.
	var restored *domain.CompetitorStatus
	err = e.store.WithCourtExclusivityLock(func() error {
		// Read-only RESULT preconditions BEFORE the court gates, so a plain reopen
		// of a permanently-unreopenable target (not completed, or its result fed a
		// fought downstream) reports THAT — not a transient court_busy. The admin
		// remedy panel turns court_busy into an offer to requeue the court's
		// occupant (applyReopenFailure branches on code=="court_busy",
		// admin_scoring_team.jsx); without this pre-check a cross-comp court hold
		// (CheckCrossCompCourtBusy runs before the tx) would surface court_busy
		// first and steer the operator into that dead-end remedy for a target that
		// can never reopen (mp-gmcg review). checkTargetReopenable is the SAME
		// read-only pre-check the requeue path runs before its revert, and the
		// mutation below re-checks — so this only reorders which 409 wins. It lives
		// at this plain-reopen entry rather than in the shared body because the
		// requeue path already pre-checks before its revert and need not re-run it
		// (the check is read-only, so a second run would be wasted work, not a
		// hazard).
		landsScheduled, verr := e.checkTargetReopenable(compID, comp, matchID, fo.Force)
		if verr != nil {
			return verr
		}
		var rerr error
		restored, rerr = e.reopenUnderCourtLock(compID, comp, matchID, reason, fo, landsScheduled)
		return rerr
	})
	if err != nil {
		return nil, err
	}
	return restored, nil
}

// reopenUnderCourtLock runs the reopen body assuming the store's
// court-exclusivity lock is ALREADY held by the caller (mp-gmcg review A4), so a
// preceding blocker requeue and this reopen can share ONE lock section
// (RequeueBlockerAndReopen) with no window for another match to grab
// the freed court. It MUST NOT take the court lock itself (the mutex is
// non-reentrant). comp is the target competition the caller already loaded;
// whether this match may be reopened at all is reopenResultPreconditionTx's
// call, made below under the tx. This is the SINGLE shared consumer of
// `reason`, so it trims it here (mp-gmcg review): a padded reason can never
// reach reopenPending / CorrectionReason regardless of which entry point
// called in. fo carries the operator's confirmation for a downstream match
// closed with its own result (see ReopenMatch). landsScheduled is the
// caller's read-only pre-read (checkTargetReopenable) that this reopen goes
// back to the queue rather than onto the court; it skips the cross-competition
// gate, which cannot wait for the tx to decide (see the COURT GATE note on
// ReopenMatch), and holds the reopen to scheduled.
func (e *Engine) reopenUnderCourtLock(compID string, comp *state.Competition, matchID, reason string, fo ForceOptions, landsScheduled bool) (*domain.CompetitorStatus, error) {
	reason = strings.TrimSpace(reason)
	// Cross-competition court gate, deliberately OUTSIDE the transaction (see
	// the doc comment). Its own *NotFoundError is now only a backstop: both entry
	// points surface an unknown match earlier (the plain entry via
	// checkTargetReopenable, the requeue entry via requireBlockerHoldsCourt), so
	// this gate's 404 is reachable only if the competition is deleted in the gap.
	// Skipped for a reopen that lands scheduled: it takes no court.
	if !landsScheduled {
		if err := e.CheckCrossCompCourtBusy(compID, matchID); err != nil {
			return nil, err
		}
	}

	var opErr error
	var restored *domain.CompetitorStatus
	txErr := e.store.WithTransaction(compID, func(tx state.StoreTx) error {
		// courtGate is the same-competition half of the reopen court gate (see
		// the COURT GATE note above): a reopen that flips the match back to
		// running on a court that already has a running match would leave it
		// with two, wedging the exclusivity check for BOTH. DO NOT remove it as
		// a redundant-looking check. It runs only for a reopen that lands
		// RUNNING: one that lands scheduled takes no court. It reuses the pool
		// matches + bracket findMatchHome already loaded under this tx for the
		// same-comp scan, instead of the re-load a nil/nil call would do
		// (mp-gmcg review E4); a nil one (the bracket on a pool home) reloads.
		courtGate := func(h matchHome, court string) error {
			return courtFreeInCompTxWith(tx, compID, matchID, court, h.PoolMatches, h.BracketRoot)
		}

		found, ferr := findMatchHome(tx, compID, matchID, func(h matchHome) error {
			// Read-only RESULT preconditions (completed + downstream-not-fought),
			// the SAME check checkTargetReopenable runs, shared via
			// reopenResultPreconditionTx so neither path can add one the other
			// misses (mp-gmcg review). A pool target has no downstream check:
			// reopening it moves no knockout slot, and the write that finishes
			// it answers for any qualifier it moves (requalifyAfterPoolWrite
			// measures that against the bracket, so the reopen dropping the
			// match out of the standings cannot hide it). These precede the SAME-competition court
			// gate below; the cross-comp gate (CheckCrossCompCourtBusy) already ran
			// before this tx, and the plain-reopen entry (ReopenMatch)
			// pre-checks these preconditions before THAT — so an unreopenable target
			// is not masked by a transient court_busy on either gate, EXCEPT in the
			// same accepted race the requeue path documents: a /decision or
			// /bulk-score completing a downstream between the entry pre-check's tx
			// close and CheckCrossCompCourtBusy can still surface court_busy for a
			// now-unreopenable target (a retry then reports the permanent 409).
			if rerr := e.reopenResultPreconditionTx(tx, compID, comp, matchID, h, fo.Force); rerr != nil {
				opErr = rerr
				return nil
			}
			// Where the reopen lands decides whether it needs the court, so
			// it is settled BEFORE the court gate. A pre-read of scheduled
			// holds: the cross-competition gate was skipped on its word, so
			// this reopen must not become running (a competitor reinstated in
			// between simply gets a scheduled match to start the normal way).
			targetStatus := state.MatchStatusScheduled
			if !landsScheduled {
				targetStatus = e.reopenTargetStatusOfHome(tx, compID, matchID, h)
			}
			if targetStatus == state.MatchStatusRunning {
				court := ""
				if h.Pool != nil {
					court = h.Pool.Court
				} else {
					court = h.Bracket.Court
				}
				if cerr := courtGate(h, court); cerr != nil {
					opErr = cerr
					return nil
				}
			}
			if h.Pool != nil {
				prior := h.Pool.Decision
				fight, single := singleBoutFightOf(h.Pool.SubResults, h.Pool.IpponsA, h.Pool.IpponsB, h.Pool.HansokuA, h.Pool.HansokuB, h.Pool.Encho)
				// Who fought a team -DH-/-TB- rep bout is a fact of that bout
				// too; a pool match is the only home that names them.
				fight.RepPlayerA, fight.RepPlayerB = h.Pool.RepPlayerA, h.Pool.RepPlayerB
				reopenPoolMatch(h.Pool, reason, targetStatus)
				if single {
					h.Pool.IpponsA, h.Pool.IpponsB = fight.IpponsA, fight.IpponsB
					h.Pool.HansokuA, h.Pool.HansokuB = fight.HansokuA, fight.HansokuB
					h.Pool.Encho = fight.Encho
					h.Pool.RepPlayerA, h.Pool.RepPlayerB = fight.RepPlayerA, fight.RepPlayerB
				}
				// SavePoolMatches funnels through the normal save chokepoint,
				// so standings caches invalidate via the usual version bump.
				if serr := h.Save(); serr != nil {
					return serr
				}
				// The reopen cleared the decision: a withdrawal it removed
				// bars nobody (see ELIGIBILITY on ReopenMatch).
				restored = e.restoreIfWithdrawalRemoved(tx, compID, matchID, prior, h.Pool.Decision, nil)
				return nil
			}
			// A bracket ROUND winner may already be propagated downstream,
			// through any byes it resolved (propagatedDownstreamOf); the bronze
			// (3rd-place) match is a sibling with no downstream, so it needs
			// no retraction. The precondition above already refused a
			// downstream being fought, and one closed with its own result
			// unless the operator confirmed it (fo.Force). That confirmed one
			// is reopened for re-entry FIRST, exactly as a forced knockout
			// correction does, which leaves it an untouched scheduled slot;
			// retractPropagatedWinner then re-checks the targets as its
			// documented check-before-mutate contract (a no-op here), unwinds
			// the byes and does the retraction.
			var reopenedDownstream []ReopenedMatch
			if !h.Bronze {
				if fo.Force {
					reopenedDownstream = forceReopenDownstreamChain(h.BracketRoot, h.RIdx, h.MIdx, matchID)
				}
				if derr := retractPropagatedWinner(h.BracketRoot, h.RIdx, h.MIdx); derr != nil {
					opErr = derr
					return nil
				}
			}
			prior := h.Bracket.Decision
			fight, single := singleBoutFightOf(h.Bracket.SubResults, h.Bracket.IpponsA, h.Bracket.IpponsB, h.Bracket.HansokuA, h.Bracket.HansokuB, h.Bracket.Encho)
			reopenBracketMatch(h.Bracket, reason, targetStatus)
			if single {
				h.Bracket.IpponsA, h.Bracket.IpponsB = fight.IpponsA, fight.IpponsB
				h.Bracket.HansokuA, h.Bracket.HansokuB = fight.HansokuA, fight.HansokuB
				h.Bracket.Encho = fight.Encho
			}
			if serr := h.Save(); serr != nil {
				return serr
			}
			e.restoreForceReopened(tx, compID, reopenedDownstream)
			if fo.Reopened != nil {
				*fo.Reopened = reopenedDownstream
			}
			restored = e.restoreIfWithdrawalRemoved(tx, compID, matchID, prior, h.Bracket.Decision, nil)
			return nil
		})
		if ferr != nil {
			return ferr
		}
		if !found {
			opErr = notFoundErrorf("match %s not found", matchID)
		}
		return nil
	})
	if txErr != nil {
		return nil, txErr
	}
	if opErr != nil {
		return nil, opErr
	}
	return restored, nil
}

// RequeueBlockerAndReopen atomically frees a court and reopens a
// kachinuki match onto it: under ONE hold of the court-exclusivity lock it
// requeues the blocking match and then reopens the target. A court hosts
// matches from ANY competition, and one competition spreads its matches across
// SEVERAL courts, so the blocker holding this match's court may be in the same
// competition (on this same court, a sibling of the target) or in a different
// one — either way it is the match occupying THIS court. Holding the lock
// across both steps closes the race the two-call client flow had (another
// operator could take the freed court between the requeue and the reopen —
// mp-gmcg review A4). RevertMatchToQueue takes only the blocker's
// per-competition lock, so calling it under the court lock keeps the
// courtCheckMu → per-comp ordering.
//
// The blocker id is CLIENT-SUPPLIED and RevertMatchToQueue is destructive (it
// clears the match's score), so requireBlockerHoldsCourt gates it FIRST: the
// named match must be running on the target's court, else the call is rejected
// without touching anything (mp-gmcg review R1). Without that gate a wrongly
// named bystander on a different court would be wiped AND the reopen would then
// fail on the court's real occupant, leaving the wipe committed but invisible
// behind a "court busy" response. For the same reason a target that reopens
// to SCHEDULED (see the COURT GATE note on ReopenMatch) is refused before the
// revert with a *ValidationError pointing at the plain reopen: it needs no
// court, so there is nothing to free.
//
// The returned status is the target's, exactly as ReopenMatch
// returns it (see ELIGIBILITY there); the blocker was running, so its requeue
// removes no withdrawal.
func (e *Engine) RequeueBlockerAndReopen(targetComp, targetMatch, blockerComp, blockerMatch, reason string, opts ...ForceOptions) (*domain.CompetitorStatus, error) {
	fo := firstForceOptions(opts)
	comp, err := e.store.LoadCompetition(targetComp)
	if err != nil {
		return nil, err
	}
	var restored *domain.CompetitorStatus
	err = e.store.WithCourtExclusivityLock(func() error {
		if verr := e.requireBlockerHoldsCourt(targetComp, targetMatch, blockerComp, blockerMatch); verr != nil {
			return verr
		}
		// Pre-check the TARGET's RESULT preconditions (completed +
		// downstream-not-fought) read-only BEFORE the destructive revert, so a
		// target that cannot be reopened — because its result already fed a
		// fought knockout — does not cost the blocker its live on-court score
		// (mp-gmcg review). The court half is deliberately NOT checked here: the
		// revert is what frees the court, so the reopen's own court gate is the
		// authoritative one. This NARROWS the wipe window but does NOT close it:
		// PUT /score takes the court lock we hold, but POST /decision and POST
		// /bulk-score complete a match under the per-comp lock alone (see the
		// ErrMatchAlreadyCompleted case in the requeue handler), so a downstream
		// write landing between
		// this pre-check's tx close and the reopen's own in-tx re-check can still
		// make the reopen fail AFTER the revert — costing the blocker its score.
		// The reopen's re-check is the backstop that preserves bracket integrity
		// in that race regardless; closing the residual window deterministically
		// would need the pre-check, revert, and reopen under one target-comp
		// transaction, which the cross-comp revert (it takes the BLOCKER comp's
		// lock) cannot provide.
		landsScheduled, perr := e.checkTargetReopenable(targetComp, comp, targetMatch, fo.Force)
		if perr != nil {
			return perr
		}
		// A target that reopens to SCHEDULED (a default win whose barred
		// competitor is still barred) takes no court, so requeuing the
		// blocker would wipe its live score to free a court nobody needs.
		// Refused before the revert, naming the one step that works: the
		// plain reopen, which no busy court refuses for such a target.
		if landsScheduled {
			return validationErrorf("%s goes back to the queue when reopened, not onto the court, so %s does not need to be sent back to the queue. Reopen it directly.",
				SentenceCase(e.operatorMatchLabel(targetComp, targetMatch)), e.operatorMatchLabel(blockerComp, blockerMatch))
		}
		if rerr := e.RevertMatchToQueue(blockerComp, blockerMatch); rerr != nil {
			return rerr
		}
		var oerr error
		restored, oerr = e.reopenUnderCourtLock(targetComp, comp, targetMatch, reason, fo, false)
		return oerr
	})
	if err != nil {
		return nil, err
	}
	return restored, nil
}

// reopenResultPreconditionTx runs the read-only RESULT preconditions a reopen
// requires for one already-located match home, in this order:
//
//  1. the match must be COMPLETED (ErrReopenNotCompleted);
//  2. it must be one a reopen is FOR: a kachinuki team match, or any match
//     decided by a withdrawal OR a default win (domain.IsDefaultWinDecisionStr:
//     kiken/kiken-voluntary/kiken-injury, fusenpai, or fusensho -- operator
//     ruling 2026-09-24, widened to fusensho by bc-cse so "Clear default win
//     and reopen" works on a match-level fusensho too; a fusensho reopen
//     restores nobody's eligibility, since recording one never barred anyone
//     -- see restoreIfWithdrawalRemoved, scoring_tx.go, which stays scoped to
//     domain.IsWithdrawalDecisionStr for exactly that reason). Anything else
//     is a *ValidationError (HTTP 400); its sanctioned edit is the correction
//     path. This is the reopen's one entry gate, so both doors (ReopenMatch,
//     RequeueBlockerAndReopen) apply it, and it reads the STORED decision
//     under the tx, never a caller's claim about it;
//  3. for a bracket round, its result must not have fed a downstream it
//     cannot unwind (reopenBracketDownstreamCheck), where force is the
//     operator's confirmation for a downstream closed with its own result. A
//     pool match has no such check: reopening it moves no knockout slot, and
//     the write that finishes it answers for any qualifier it moves.
//
// It EXCLUDES the court gate (the requeue path frees the court itself; the
// plain reopen checks it separately) and performs NO mutation.
// checkTargetReopenable and reopenUnderCourtLock both run it, so a RESULT
// precondition added to one path can't be missed by the other — the drift
// that reopens the wipe-for-nothing hazard (mp-gmcg review).
func (e *Engine) reopenResultPreconditionTx(tx state.StoreTx, compID string, comp *state.Competition, matchID string, h matchHome, force bool) error {
	var status state.MatchStatus
	var decision string
	if h.Pool != nil {
		status, decision = h.Pool.Status, h.Pool.Decision
	} else {
		status, decision = h.Bracket.Status, h.Bracket.Decision
	}
	if status != state.MatchStatusCompleted {
		return ErrReopenNotCompleted
	}
	if !comp.IsKachinuki() && !domain.IsDefaultWinDecisionStr(decision) {
		// bc-cse item 14: no "(correctionReason)" jargon -- that named the
		// internal API field, not anything the operator sees on the score
		// editor's own correction-reason box.
		return validationErrorf("reopen is only for kachinuki team matches and for matches decided by a withdrawal or default win (kiken, kiken-injury, fusenpai, or fusensho); correct other results via the score editor instead")
	}
	if h.Pool != nil {
		// A pool reopen changes no knockout slot: the pool is incomplete
		// while the match is open, and an incomplete pool is never
		// re-resolved. What the reopen leads to is decided when it is
		// finished: that write runs the requalification rule
		// (requalifyAfterPoolWrite), which is silent for the same result and
		// warns, naming the knockout matches, for a different one.
		return nil
	}
	// Bronze is a sibling of Rounds with no downstream, so it can never be
	// downstream-fought (matches reopenUnderCourtLock's !Bronze).
	if h.Bronze {
		return nil
	}
	return reopenBracketDownstreamCheck(h.BracketRoot, h.RIdx, h.MIdx, force)
}

// reopenBracketDownstreamCheck decides, for a bracket ROUND match being
// reopened, what its already-propagated result means for the matches it fed:
// its bronze when it is a semifinal, and the first next-round match past any
// byes the winner was passed through (propagatedDownstreamOf; the byes themselves
// are unwound by the reopen, never refused, since nobody fought them). THREE
// cases, the terminal one checked FIRST so the operator is never asked to
// confirm a reopen that would then be refused anyway:
//
//   - a target that is RUNNING: refused outright with
//     *DownstreamKnockoutRunningError (bc-cse; the SAME 409
//     downstream_knockout_running shape the pool-requalification refusal
//     already uses). Someone is fighting it now; finish it or send it back to
//     the queue, then reopen;
//   - a target CLOSED with a result of its own (propagatedDownstream.played,
//     the same rule a knockout correction uses): without force, *DownstreamKnockoutPlayedError
//     naming it (HTTP 409 downstream_knockout_played), so the operator is told
//     the consequence and may proceed; with force it passes, and
//     reopenUnderCourtLock reopens it for re-entry before retracting;
//   - otherwise (a merely filled, unfought slot): nil.
//
// A target that is SCHEDULED yet carries stray result data, or COMPLETED with
// no result of its own and not in a bye's shape (hand-edited or pre-migration
// files; no write path produces either), is confidently none of these, so it
// passes here and retractPropagatedWinner's backstop refuses it with
// ErrReopenDownstreamFought.
func reopenBracketDownstreamCheck(bracket *state.Bracket, rIdx, mIdx int, force bool) error {
	d := propagatedDownstreamOf(bracket, rIdx, mIdx)
	var running []ReopenedMatch
	for _, t := range []*state.BracketMatch{d.bronze, d.next} {
		if t != nil && t.Status == state.MatchStatusRunning {
			running = append(running, bracketMatchRef(t))
		}
	}
	matchID := bracket.Rounds[rIdx][mIdx].ID
	if len(running) > 0 {
		return &DownstreamKnockoutRunningError{MatchID: matchID, Running: running, Reopening: true}
	}
	if played := d.played(); len(played) > 0 && !force {
		return newDownstreamKnockoutPlayedError(&bracket.Rounds[rIdx][mIdx], played, d.displacedSlot(played, mIdx))
	}
	return nil
}

// checkTargetReopenable runs the read-only reopen RESULT preconditions (mp-gmcg
// review): it opens a target-competition tx and reports whether the match is
// completed and its result has not fed a fought downstream, WITHOUT any court
// check and WITHOUT any mutation. Two callers run it before their court gates:
// the requeue-and-reopen path (before its destructive revert, so in the common
// path a target that can't reopen doesn't cost the blocker its score) and the
// plain-reopen entry (before CheckCrossCompCourtBusy, so an unreopenable target
// reports that rather than a transient court_busy). The preconditions live in
// reopenResultPreconditionTx, which reopenUnderCourtLock also runs, so
// the pre-check and the reopen share ONE rule set and cannot DRIFT — though a
// /decision or /bulk-score racing the gap can still make the reopen reject after
// the pre-check passed (the accepted window the requeue comment documents).
//
// It also reports whether the reopen lands SCHEDULED (reopenTargetStatus: a
// match-level fusensho whose decisionBy side is still barred), read in the same
// tx. Such a reopen takes no court, so reopenUnderCourtLock skips both court
// gates for it and holds it to scheduled even if the bar lifts in between (see
// the COURT GATE note on ReopenMatch).
func (e *Engine) checkTargetReopenable(compID string, comp *state.Competition, matchID string, force bool) (bool, error) {
	var checkErr error
	landsScheduled := false
	txErr := e.store.WithTransaction(compID, func(tx state.StoreTx) error {
		found, ferr := findMatchHome(tx, compID, matchID, func(h matchHome) error {
			checkErr = e.reopenResultPreconditionTx(tx, compID, comp, matchID, h, force)
			if checkErr == nil {
				landsScheduled = e.reopenTargetStatusOfHome(tx, compID, matchID, h) == state.MatchStatusScheduled
			}
			return nil
		})
		if ferr != nil {
			return ferr
		}
		if !found {
			checkErr = notFoundErrorf("match %s not found", matchID)
		}
		return nil
	})
	if txErr != nil {
		return false, txErr
	}
	return landsScheduled, checkErr
}

// requireBlockerHoldsCourt verifies the client-named blocker is a RUNNING match
// on targetMatch's court, the precondition RequeueBlockerAndReopen's
// destructive requeue depends on (mp-gmcg review R1). It rejects — WITHOUT
// touching the blocker — when the target has no court, when the blocker sits on
// a different court (the bystander-wipe case), or when the blocker is not
// running (a completed/scheduled match is not what is wedging the court; retry
// the plain reopen). Each check returns its OWN operator-facing message, which
// is why this validates the client's claim directly rather than folding into a
// single store.RunningMatchOnCourt occupant lookup (that would name the true
// occupant but collapse the three messages into one).
//
// The two lookupMatchCourt reads go through the COPYING LoadPoolMatches/
// LoadBracket; MatchStatusByID is the no-copy one. The path is operator-
// initiated and rare, so the extra copies don't matter, but the doc should not
// claim they aren't made. The caller holds only the court lock, not a per-comp
// write lock, so these reads match CheckCrossCompCourtBusy's pre-tx discipline.
func (e *Engine) requireBlockerHoldsCourt(targetComp, targetMatch, blockerComp, blockerMatch string) error {
	targetCourt, err := e.lookupMatchCourt(targetComp, targetMatch)
	if err != nil {
		return err
	}
	if targetCourt == "" {
		return validationErrorf("%s has no court assigned, so no match can be blocking it.", SentenceCase(e.operatorMatchLabel(targetComp, targetMatch)))
	}
	blockerCourt, err := e.lookupMatchCourt(blockerComp, blockerMatch)
	if err != nil {
		return err
	}
	if blockerCourt != targetCourt {
		// The second label is mid-sentence ("not %s's Shiaijo"), so only the
		// first (sentence-initial) one is case-corrected.
		return validationErrorf("%s is on Shiaijo %s, not %s's Shiaijo %s. Requeuing it would not free the court.",
			SentenceCase(e.operatorMatchLabel(blockerComp, blockerMatch)), blockerCourt, e.operatorMatchLabel(targetComp, targetMatch), targetCourt)
	}
	status, found, err := e.store.MatchStatusByID(blockerComp, blockerMatch)
	if err != nil {
		return err
	}
	if !found {
		// lookupMatchCourt above already 404s an unknown id, so on the normal
		// path found is true here. This still fires if blockerComp is DELETED
		// between the two cached reads (DeleteCompetition takes the per-comp
		// lock, not the court lock we hold) — a clearer message than the
		// "not running (status \"\")" the check below would otherwise give.
		// No label to resolve: the match is gone, so there is nothing to name
		// it by beyond "it".
		return notFoundErrorf("the blocking match could not be found; it may have been deleted. Retry the reopen.")
	}
	if status != state.MatchStatusRunning {
		return validationErrorf("%s is not running now, so it is not blocking the court. Retry the reopen.", SentenceCase(e.operatorMatchLabel(blockerComp, blockerMatch)))
	}
	return nil
}

// RemoveTrailingKachinukiBout drops the SINGLE trailing UNSCORED bout from a
// RUNNING kachinuki encounter — the explicit operator undo for a pairing
// appended by mistake ([Record bout] / [Add next bout]). Its own `strip`
// closure (below), not stripTrailingUnscoredKachinukiBouts, defines
// "removable" here: never the last remaining bout, and never more than one
// per call (review F3) — a DIFFERENT, narrower rule than the completed-write
// strip's "drop the whole trailing run", which is safe there only because a
// completed match always has a scored bout to stop on. Position <= 0 is
// still refused, so a non-numbered sub is never touched — daihyosen does not
// exist in kachinuki and is not involved.
//
// Walks the same three match homes as ReopenMatch (pool → bracket
// rounds → bronze) via the shared findMatchHome visitor (review F6), so a
// fourth match home or a lookup-order change can't be forgotten in just one
// of the two. Returns the updated match for the caller to broadcast. No
// court gate: the match is (and stays) running, so removing an empty bout
// changes no court occupancy.
func (e *Engine) RemoveTrailingKachinukiBout(compID, matchID string) (*state.MatchResult, error) {
	comp, err := e.store.LoadCompetition(compID)
	if err != nil {
		return nil, err
	}
	if !comp.IsKachinuki() {
		return nil, validationErrorf("removing a bout is only supported for kachinuki team matches")
	}

	var (
		updated *state.MatchResult
		opErr   error
	)
	txErr := e.store.WithTransaction(compID, func(tx state.StoreTx) error {
		// strip is the SINGLE precondition+mutation every match home applies:
		// the encounter must be RUNNING and must carry a trailing unscored bout.
		// One closure so pool/round/bronze cannot drift (same reasoning as
		// reopenResultPreconditionTx, the reopen's single result-precondition helper).
		strip := func(status state.MatchStatus, subs []state.SubMatchResult) ([]state.SubMatchResult, error) {
			if status != state.MatchStatusRunning {
				return nil, ErrRemoveBoutNotRunning
			}
			// Remove ONLY the single trailing bout, and never the last remaining
			// one: the operator's "× Remove this bout" is singular, and a RUNNING
			// kachinuki encounter must always keep at least one bout (mp-gmcg
			// review F3). stripTrailingUnscoredKachinukiBouts drops the whole
			// trailing run with no floor — correct for the completed-write caller
			// (a completed match always carries a scored bout, so it never
			// empties) but not here, where bout 1 could be unscored, or two
			// "Add next bout manually" pairings could be appended and one tap
			// must not strip both.
			n := len(subs)
			if n <= 1 {
				return nil, ErrNoRemovableBout
			}
			last := subs[n-1]
			if last.Position <= 0 || !isUnscoredKachinukiBout(last) {
				return nil, ErrNoRemovableBout
			}
			return subs[:n-1], nil
		}

		found, ferr := findMatchHome(tx, compID, matchID, func(h matchHome) error {
			if h.Pool != nil {
				stripped, serr := strip(h.Pool.Status, h.Pool.SubResults)
				if serr != nil {
					opErr = serr
					return nil
				}
				h.Pool.SubResults = stripped
				if werr := h.Save(); werr != nil {
					return werr
				}
				u := *h.Pool
				updated = &u
				return nil
			}
			stripped, serr := strip(h.Bracket.Status, h.Bracket.SubResults)
			if serr != nil {
				opErr = serr
				return nil
			}
			h.Bracket.SubResults = stripped
			if werr := h.Save(); werr != nil {
				return werr
			}
			updated = bracketMatchToTeamResult(*h.Bracket)
			return nil
		})
		if ferr != nil {
			return ferr
		}
		if !found {
			opErr = notFoundErrorf("match %s not found", matchID)
		}
		return nil
	})
	if txErr != nil {
		return nil, txErr
	}
	if opErr != nil {
		return nil, opErr
	}
	return updated, nil
}

// matchHome is the store location that owns a match ID, handed to a
// findMatchHome visitor. Exactly one of Pool / Bracket is non-nil. For a
// bracket ROUND match, Bracket points into BracketRoot.Rounds[RIdx][MIdx] and
// Bronze is false; for the 3rd-place match, Bracket is BracketRoot.ThirdPlaceMatch,
// Bronze is true, and RIdx/MIdx are meaningless (the bronze is a SIBLING of
// Rounds, not an element — forgetting it is the class of bug this shared walk
// exists to make impossible). Save persists the store the match lives in.
type matchHome struct {
	Pool        *state.MatchResult
	Bracket     *state.BracketMatch
	BracketRoot *state.Bracket
	RIdx, MIdx  int
	Bronze      bool
	Save        func() error
	// PoolMatches / BracketRoot are the FULL slices findMatchHome already
	// loaded under this tx, exposed so a visitor's court check can reuse them
	// instead of re-loading (mp-gmcg review E4). A court check treats a nil
	// PoolMatches as "reload", so it never skips pool matches. BracketRoot is
	// nil for a pool home (findMatchHome returns before loading the bracket).
	PoolMatches []state.MatchResult
}

// findMatchHome walks the three homes a match ID can have — pool matches, then
// bracket rounds, then the bronze (3rd-place) match — in that FIXED order, and
// invokes visit for the owning home. It is the MUTATING, in-transaction walk,
// and the engine's only copy of it, so a new caller cannot drop the bronze
// branch by hand-copying the ~60-line skeleton (mp-gmcg review F6).
// found=false with a nil error means the ID is in neither store. A LOAD error
// from either store is returned as it is, never read as "not in this store": a
// missing file already loads as empty, so an error is a file that exists and
// cannot be read, and swallowing the pool one answered a corrupt
// pool-matches.csv with a 404 "not found" instead of the corrupt-file 500 that
// names the file. Every caller already returns the error. visit's own error
// propagates.
func findMatchHome(tx state.StoreTx, compID, matchID string, visit func(matchHome) error) (bool, error) {
	poolMatches, lerr := tx.LoadPoolMatches(compID)
	if lerr != nil {
		return false, lerr
	}
	for i := range poolMatches {
		if poolMatches[i].ID == matchID {
			return true, visit(matchHome{
				Pool:        &poolMatches[i],
				PoolMatches: poolMatches,
				Save:        func() error { return tx.SavePoolMatches(compID, poolMatches) },
			})
		}
	}

	bracket, berr := tx.LoadBracket(compID)
	if berr != nil {
		return false, berr
	}
	if bracket != nil {
		for rIdx := range bracket.Rounds {
			for mIdx := range bracket.Rounds[rIdx] {
				if bracket.Rounds[rIdx][mIdx].ID == matchID {
					return true, visit(matchHome{
						Bracket:     &bracket.Rounds[rIdx][mIdx],
						BracketRoot: bracket,
						PoolMatches: poolMatches,
						RIdx:        rIdx,
						MIdx:        mIdx,
						Save:        func() error { return tx.SaveBracket(compID, bracket) },
					})
				}
			}
		}
		if bm := bracket.ThirdPlaceMatch; bm != nil && bm.ID == matchID {
			return true, visit(matchHome{
				Bracket:     bm,
				BracketRoot: bracket,
				PoolMatches: poolMatches,
				Bronze:      true,
				Save:        func() error { return tx.SaveBracket(compID, bracket) },
			})
		}
	}

	return false, nil
}

// reopenPoolMatch is reopenBracketMatch's twin for a pool/league match. Same
// rule, same field set: MatchResult and BracketMatch both carry the
// scoreline as IpponsA/IpponsB (+ HansokuA/B) and, since bc-brid, the winner
// id as WinnerID. This twin adds the rep-bout nominations, which have no
// BracketMatch counterpart (a bracket daihyosen is a numbered sub-bout, not
// a team rep-player nomination). The hantei verdict needs no clear of its
// own: it is the domain.HanteiMark entry inside the scoreline being cleared.
// See reopenBracketMatch for why each of these is verdict rather than bout
// record.
//
// RepPlayerA/B name who fought a pool daihyosen. On a team encounter that
// bout's own record lives in SubResults like any other, and these two fields
// are the discarded verdict's nomination for it, so they go with the rest of
// the verdict. On a match that IS the rep bout (a -DH-/-TB- match with no bout
// rows) they are who fought it, which reopenUnderCourtLock puts back
// (singleBoutFight) with the letters struck.
//
// The reopen stamps ModifiedAt with the server's now, the same revert fence
// requeueBracketMatch sets (mp-y3nk): a write stamped before the reopen, such
// as an offline-queued Save correction replayed afterwards, is older than the
// reopen and is refused as superseded instead of completing the match again.
// reopenTargetStatus decides RUNNING vs SCHEDULED for a reopen (bc-cse item
// 9). Every reopen goes to RUNNING, as it always has (the operator tapped
// Reopen to fight on), except a default win whose barred side is barred by
// ANOTHER match. The first such case was a match-level FUSENSHO
// (domain.DecisionFusensho) recorded to close a barred competitor's
// remaining match with a default win for the opponent -- fusensho itself
// never writes a CompetitorStatus (recordIneligibilityFromDecision only
// fires for domain.IsWithdrawalDecisionStr, which deliberately excludes it;
// see ReopenMatch's ELIGIBILITY doc), so the bar this match's decisionBy
// side carries, if any, was recorded by an EARLIER withdrawal elsewhere and
// this reopen restores nothing. The second is a FUSENPAI chained onto such a
// bar (bc-kfup, alreadyBarredRefusal): it records the default loss and no
// status of its own, so the same holds. If that earlier bar still holds,
// the competitor still cannot fight: reopening to RUNNING would only let
// StartMatchTx refuse every later write on this match with no way forward,
// so it goes to SCHEDULED instead, which re-shows the barred-match notice
// (annotateIneligibleSides) exactly as it did before the default win was
// ever recorded, and, taking no court, is not refused for a busy one (see
// the COURT GATE note on ReopenMatch). Once the competitor is reinstated or
// the earlier withdrawal is itself cleared, the SAME reopen goes to RUNNING
// like any other. An ordinary kiken or fusenpai is unaffected: the status
// it recorded names THIS match, which BarredSides' undo-path exemption (a
// status recorded BY matchID never bars it) ignores, and which the reopen
// restores.
//
// decisionBy names the WITHDRAWING side (recordDecisionTx's own convention:
// "aka" -> sideA lost, "shiro" -> sideB lost), so it is read against
// BarredSides' A/B return the same way. statuses/matchID/sideAID/sideBID are
// the reopened match's OWN identity, read before reopenPoolMatch/
// reopenBracketMatch clear Decision/DecisionBy.
func reopenTargetStatus(statuses map[string]domain.CompetitorStatus, matchID, decision, decisionBy, sideAID, sideBID string) state.MatchStatus {
	if !domain.IsDefaultWinDecisionStr(decision) {
		return state.MatchStatusRunning
	}
	a, b := BarredSides(statuses, matchID, sideAID, sideBID)
	stillBarred := (decisionBy == "aka" && a != nil) || (decisionBy == "shiro" && b != nil)
	if stillBarred {
		return state.MatchStatusScheduled
	}
	return state.MatchStatusRunning
}

// reopenTargetStatusTx is reopenTargetStatus's tx-aware wrapper: it loads
// CompetitorStatus LAZILY, only when decision is a default win (the one case
// reopenTargetStatus needs it for), through the live transaction handle. A
// load failure is logged rather than discarded and defaults to RUNNING
// (today's behaviour), since a read the operator cannot diagnose must never
// silently trade one stuck state for another.
func (e *Engine) reopenTargetStatusTx(tx state.StoreTx, compID, matchID, decision, decisionBy, sideAID, sideBID string) state.MatchStatus {
	if !domain.IsDefaultWinDecisionStr(decision) {
		return state.MatchStatusRunning
	}
	statuses, err := tx.LoadCompetitorStatus(compID)
	if err != nil {
		log.Printf("engine: ReopenMatch: LoadCompetitorStatus compId=%s matchId=%s: %v (defaulting to running)", compID, matchID, err)
		return state.MatchStatusRunning
	}
	return reopenTargetStatus(statuses, matchID, decision, decisionBy, sideAID, sideBID)
}

// reopenTargetStatusOfHome is reopenTargetStatusTx for a located match home,
// reading the stored result's own decision and sides, so the read-only
// pre-check (checkTargetReopenable) and the reopen itself
// (reopenUnderCourtLock) ask the one question the same way.
func (e *Engine) reopenTargetStatusOfHome(tx state.StoreTx, compID, matchID string, h matchHome) state.MatchStatus {
	if h.Pool != nil {
		return e.reopenTargetStatusTx(tx, compID, matchID, h.Pool.Decision, h.Pool.DecisionBy, h.Pool.SideAID, h.Pool.SideBID)
	}
	return e.reopenTargetStatusTx(tx, compID, matchID, h.Bracket.Decision, h.Bracket.DecisionBy, h.Bracket.SideAID, h.Bracket.SideBID)
}

func reopenPoolMatch(m *state.MatchResult, reason string, targetStatus state.MatchStatus) {
	m.Status = targetStatus
	m.Winner = ""
	m.WinnerID = ""
	m.IpponsA = nil
	m.IpponsB = nil
	m.HansokuA = 0
	m.HansokuB = 0
	m.Decision = ""
	m.DecisionBy = ""
	m.DecisionReason = ""
	m.Encho = nil
	m.ResultSource = ""
	m.RepPlayerA = ""
	m.RepPlayerB = ""
	m.CorrectionReason = reason
	m.ReopenPending = reopenPending(reason)
	m.ModifiedAt = time.Now().UnixMilli()
}

// reopenBracketMatch discards the ENCOUNTER-LEVEL VERDICT and keeps the BOUT
// LOG. Those are separate things in the data model, which is what makes this
// safe: every fact about a bout that was actually fought — who fought it
// (SideA/SideB), what they struck, hansoku, hantei, whether it went to encho —
// lives on the SubMatchResult for that bout (state/models.go). SubResults is
// therefore untouched here, and all of it survives a reopen.
//
// Everything cleared below is the discarded verdict ABOUT those bouts, not a
// record OF them: the encounter winner, the match-level default-win scoreline
// (the FIK Art. 32 maru fill a kiken/fusenpai writes at match level), the
// match-level hansoku count, the decision, the encho/hantei state describing
// the bout in progress when the operator ended it, and the provenance stamps
// for a result that no longer exists (IsOverridden, ResultSource). Leaving
// those behind produced a match that was running yet still advertised a
// winner, a scoreline, and a manual-override badge inherited from the result
// the reopen threw away.
//
// This clears the verdict fields RevertMatchToQueue clears (engine/scoring.go)
// that a kachinuki result can actually carry, MINUS SubResults (which requeue
// drops and reopen exists to preserve) and CorrectionReason/ReopenPending
// (which the reopen is itself setting). Match-level HansokuA/B ARE mirrored
// here, even though today's team-editor wire path never sets them on a
// kachinuki match: NormalizeLegacy (state/legacy_hantei.go) folds a legacy
// ScoreA/ScoreB string into IpponsA/HansokuA on every bracket read, and
// MatchResult.HansokuA/B are first-class pool-matches.csv columns any client
// payload can set (state/pools.go), so a pre-migration file or an
// out-of-band write can leave a genuine count here. Before bc-bmsc, the old
// ScoreA = "" clear wiped this count too, because it rode inside the
// rendered string's "(HN)" suffix; leaving it behind now would let
// applyHansokuIppons fold a stale count into the H ippons of a scoreline the
// reopen just cleared. FlagsA/B remain unmirrored: they are engi-only, and
// engi has no kachinuki, so neither reopen path ever sees them populated.
// ModifiedAt is left at the completion stamp on purpose — it still
// fences any stale pre-completion offline write via ApplyByTimestamp and
// never blocks the re-End. If you add a match-level verdict field a
// kachinuki result CAN carry, clear it here too.
func reopenBracketMatch(bm *state.BracketMatch, reason string, targetStatus state.MatchStatus) {
	bm.Status = targetStatus
	bm.Winner = ""
	bm.WinnerID = "" // bc-brid: the verdict's id half, cleared with the name.
	bm.IpponsA = nil
	bm.IpponsB = nil
	bm.HansokuA = 0
	bm.HansokuB = 0
	bm.Decision = ""
	bm.DecisionBy = ""
	bm.DecisionReason = ""
	bm.Encho = nil
	bm.IsOverridden = false
	bm.ResultSource = ""
	bm.CorrectionReason = reason
	bm.ReopenPending = reopenPending(reason)
	// Revert fence, as reopenPoolMatch's doc explains: every reopen door
	// (ReopenMatch, RequeueBlockerAndReopen, forceReopenDownstreamChain) ends
	// here, so a write stamped before the reopen is refused as superseded.
	bm.ModifiedAt = time.Now().UnixMilli()
}

// reopenPending reports whether a reopen was made without an audit reason:
// true when none was supplied, false when the operator gave one. Nothing is
// refused for it (see state.MatchResult.ReopenPending). One helper rather than a `reason == ""` test
// inlined at each of its two call sites (reopenPoolMatch, reopenBracketMatch
// — the latter already covers both the bracket-round and bronze homes), so a
// third caller can't drift from the rule — the same reason the reopen keeps its
// result preconditions in a single reopenResultPreconditionTx.
func reopenPending(reason string) bool {
	return reason == ""
}

// singleBoutFight is what a reopen keeps at MATCH level of a match that is a
// single bout: an individual match, or a team competition's -DH-/-TB- rep
// bout. On such a match the match-level scoreline is not a verdict ABOUT
// bouts (reopenBracketMatch's reason for clearing it), it IS the bout, so the
// letters actually struck, the fouls and the overtime are facts of the fight
// the reopen promises to keep. RepPlayerA/B name who fought a team
// competition's rep bout; only a pool match carries them (a bracket match has
// no rep nomination), so the pool branch fills them and the bracket branch
// leaves them empty.
type singleBoutFight struct {
	IpponsA, IpponsB       []string
	HansokuA, HansokuB     int
	Encho                  *state.EnchoMetadata
	RepPlayerA, RepPlayerB string
}

// singleBoutFightOf reports whether a match about to be reopened is a single
// bout (it carries no bout rows) and, if so, what of its fight survives the
// reopen. The scoreline goes through struckIppons, which is the whole point:
// a withdrawal's scoreline is the winner's default-win maru plus the letters
// the withdrawing side had struck (preserveLoserScore, FIK Art. 32), so the
// maru (the discarded verdict) goes and the struck letters stay. The winner's
// own letters from before the withdrawal are NOT recoverable here: the
// withdrawal replaced them with the maru when it was recorded.
//
// Called by reopenUnderCourtLock for the match being reopened ONLY. The
// primitives reopenPoolMatch/reopenBracketMatch stay "discard the verdict"
// and nothing more, because forceReopenDownstreamChain also uses
// reopenBracketMatch on a DOWNSTREAM match whose sides were just repainted,
// where every letter belongs to a pairing no longer in it.
func singleBoutFightOf(subs []state.SubMatchResult, ipponsA, ipponsB []string, hansokuA, hansokuB int, encho *state.EnchoMetadata) (singleBoutFight, bool) {
	if len(subs) > 0 {
		return singleBoutFight{}, false
	}
	return singleBoutFight{
		IpponsA:  struckIppons(ipponsA),
		IpponsB:  struckIppons(ipponsB),
		HansokuA: hansokuA,
		HansokuB: hansokuB,
		Encho:    encho,
	}, true
}

// downstreamTargets returns the bracket matches this result was propagated into:
// next (the next-round slot, which received the WINNER) and bronze (the 3rd-place
// match, which received the semifinal LOSER, and only when this is a semifinal
// that feeds it). Either may be nil. This is the SINGLE derivation of downstream
// LOCATION on the REOPEN side (propagatedDownstreamOf composes it hop by hop), so the
// read-only reopenBracketDownstreamCheck and the destructive
// retractPropagatedWinner mutation cannot drift on WHERE a result went — the drift that would split "check passes" from "mutation misses"
// and wipe a blocker's live score for a reopen that then fails (mp-gmcg review).
// propagateBracketWinner (scoring.go) independently encodes the same slot rule
// (the mIdx/2 next-round index and the bronze-feeding round index), so a
// location-rule change must still land there too — as retractPropagatedWinner's
// body comment already flags ("Mirror propagateBracketWinner's positional
// assignment").
func downstreamTargets(bracket *state.Bracket, rIdx, mIdx int) (bronze, next *state.BracketMatch) {
	if bracket.ThirdPlaceMatch != nil && rIdx == len(bracket.Rounds)-2 {
		bronze = bracket.ThirdPlaceMatch
	}
	if rIdx+1 < len(bracket.Rounds) {
		next = &bracket.Rounds[rIdx+1][mIdx/2]
	}
	return bronze, next
}

// bracketPos is a bracket ROUND match's coordinates in Bracket.Rounds.
type bracketPos struct{ R, M int }

// resolvedByByeFrom reports whether bm is a match propagateBracketWinner's bye
// auto-resolution closed off the winner fed into it from slot feedM: completed,
// the fed side holding a competitor, the other side the empty bye, a winner,
// and no result of its own (bracketMatchCarriesOwnResult). That is the whole of
// what the auto-resolution writes (Winner, WinnerID, Status) onto a match
// generation left completed with no winner (buildBracketFromDraw's latent-bye
// pass: a feeder placeholder against an empty side), so nobody fought it and
// unresolveBye can take it back exactly.
func resolvedByByeFrom(bm *state.BracketMatch, feedM int) bool {
	fed, other := bm.SideB, bm.SideA
	if feedM%2 == 0 {
		fed, other = bm.SideA, bm.SideB
	}
	return bm.Status == state.MatchStatusCompleted &&
		bm.Winner != "" &&
		other == "" &&
		!isUnresolvedBracketSide(fed) &&
		!bracketMatchCarriesOwnResult(bm)
}

// unresolveBye returns a match resolvedByByeFrom accepts to the shape
// generation gave it, once the caller has put its fed side back to the
// feeder's placeholder: still completed, with no winner. Completed is
// deliberate, not an oversight. It is what the latent-bye pass writes so the
// match is never counted as one to play, and a SCHEDULED one would be: the
// queue numbers every scheduled match on a court (annotateBracketQueuePositions),
// so a hidden bye would push "N before yours" up by one. The feeder's next
// winner re-resolves it through propagateBracketWinner exactly as the first
// one did.
func unresolveBye(bm *state.BracketMatch) {
	bm.Winner = ""
	bm.WinnerID = ""
}

// propagatedDownstream is everything the result of a bracket ROUND match was
// propagated into, which is what each door that takes that result back or
// changes it answers for: bronze is the 3rd-place match this semifinal's loser
// went to (nil otherwise); byes are the matches a bye resolved off the winner,
// nearest first, each passing the winner one round further; next is the first
// match past them (nil past the final), fed by the match at feed (the last
// bye, or the match itself when there is none).
//
// A bye passes a winner on without being fought, so it is never the match a
// door answers to: next is, the first one a person could have fought. A
// REOPEN unwinds the byes (retractPropagatedWinner, and retractIntoUntouched
// for a match a correction reopened) rather than being refused by them:
// there is nothing to finish, requeue or confirm on a match nobody
// fought, and refusing left no way at all to remove a wrongly recorded
// withdrawal whose winner went through one, since a correction keeps the
// withdrawal (KeepsWithdrawalRuling). A CORRECTION re-resolves them with its
// new winner (propagateBracketWinner), and without looking past them it
// repainted a played match beyond a bye with no warning at all, the very
// defect its guard exists for.
//
// A bye never feeds the bronze: the empty side it beat is its loser, and
// propagateBracketWinner feeds no empty loser. So only the match's own bronze
// is followed, never a bye's.
type propagatedDownstream struct {
	bronze *state.BracketMatch
	byes   []bracketPos
	feed   bracketPos
	next   *state.BracketMatch
}

// propagatedDownstreamOf locates the propagatedDownstream of the ROUND match
// at (rIdx, mIdx), off downstreamTargets, the one owner of where a winner goes.
func propagatedDownstreamOf(bracket *state.Bracket, rIdx, mIdx int) propagatedDownstream {
	bronze, _ := downstreamTargets(bracket, rIdx, mIdx)
	d := propagatedDownstream{bronze: bronze, feed: bracketPos{rIdx, mIdx}}
	for {
		_, next := downstreamTargets(bracket, d.feed.R, d.feed.M)
		if next == nil || !resolvedByByeFrom(next, d.feed.M) {
			d.next = next
			return d
		}
		d.feed = bracketPos{d.feed.R + 1, d.feed.M / 2}
		d.byes = append(d.byes, d.feed)
	}
}

// played returns the matches ONE HOP down that the result reached and that
// are closed with a result of their own (bracketMatchCarriesOwnResult): next,
// and for a semifinal the bronze it also feeds, bronze first. Nil when
// neither is closed. Every door that would displace someone from a played
// match names these (reopenBracketDownstreamCheck,
// guardDownstreamKnockoutCorrection, guardOverrideDownstreamKnockoutCorrection)
// and forceReopenDownstreamChain reopens exactly these, so the refusal and the
// confirmation cannot disagree.
//
// ONE HOP, not the whole chain (operator ruling 2026-09-19): "if a correction
// is applied then that match is completed and reopens the next one, if that
// one is also completed". The deeper rounds are not this write's business and
// are not silently unwound behind one confirmation. They are reached in their
// own turn: once the operator re-fights the reopened match and enters THAT
// result, the write propagates a round further, meets this same check against
// the round after it, and asks again. One decision per round, each one the
// operator's, instead of a single dialog quietly clearing three matches. A bye
// is not a round anyone decided, so the hop is counted past it.
func (d propagatedDownstream) played() []*state.BracketMatch {
	var blocking []*state.BracketMatch
	// Bronze first: it is the slot the operator forgets, and naming it first
	// keeps the order stable for the dialog and for the tests.
	if d.bronze != nil && bracketMatchCarriesOwnResult(d.bronze) {
		blocking = append(blocking, d.bronze)
	}
	if d.next != nil && bracketMatchCarriesOwnResult(d.next) {
		blocking = append(blocking, d.next)
	}
	return blocking
}

// displacedSlot is the feeder position newDownstreamKnockoutPlayedError reads
// the displaced competitor's slot by, for blocking = d.played(): the match
// itself (at mIdx) for its own bronze, and feed for next, which past a bye is
// the last bye rather than the match.
func (d propagatedDownstream) displacedSlot(blocking []*state.BracketMatch, mIdx int) int {
	if blocking[0] == d.bronze {
		return mIdx
	}
	return d.feed.M
}

// retractPropagatedWinner undoes what propagateBracketWinner did for the
// match at (rIdx, mIdx): every bye resolved off its winner is unresolved
// (its fed side back to the feeder's "Winner of rX-mY" placeholder, no
// winner), the first match past them gets the same placeholder in the slot
// the winner reached, and, for a semifinal feeding a bronze match, the bronze
// slot returns to empty. The targets are CHECKED before anything is mutated,
// so a rejection leaves the bracket untouched: a bronze or next-round match
// that has started, recorded bouts, or completed rejects the reopen with
// ErrReopenDownstreamFought -- THIS function's own return, not what an operator
// sees through the normal reopen doors, which are refused earlier in the
// transaction by reopenBracketDownstreamCheck (see ErrReopenDownstreamFought's
// own doc comment). Reaching this rejection is therefore the defensive-backstop
// case: the earlier check missed it.
func retractPropagatedWinner(bracket *state.Bracket, rIdx, mIdx int) error {
	d := propagatedDownstreamOf(bracket, rIdx, mIdx)
	if (d.bronze != nil && bracketMatchStartedOrScored(d.bronze)) ||
		(d.next != nil && bracketMatchStartedOrScored(d.next)) {
		return ErrReopenDownstreamFought
	}
	clearPropagatedSlots(bracket, rIdx, mIdx, d.bronze, nil)
	d.unwindChain(bracket, rIdx, mIdx)
	return nil
}

// unwindChain is the mutation both retractions share (retractPropagatedWinner
// and retractIntoUntouched): it undoes what propagateBracketWinner wrote along
// the chain past the ROUND match at (rIdx, mIdx). Every bye in d.byes is
// unresolved, nearest first (its fed side back to its feeder's "Winner of ..."
// placeholder, then unresolveBye), and the slot the last of them fed in d.next
// (the match itself when there is no bye) goes back to that feeder's
// placeholder. The bronze is not on the chain, since a bye never feeds one,
// so each caller clears it by its own rule.
func (d propagatedDownstream) unwindChain(bracket *state.Bracket, rIdx, mIdx int) {
	feed := bracketPos{rIdx, mIdx}
	for _, bye := range d.byes {
		bm := &bracket.Rounds[bye.R][bye.M]
		clearPropagatedSlots(bracket, feed.R, feed.M, nil, bm)
		unresolveBye(bm)
		feed = bye
	}
	clearPropagatedSlots(bracket, feed.R, feed.M, nil, d.next)
}

// retractIntoUntouched is retractPropagatedWinner for a match a correction
// elsewhere has just reopened (applyRequalification for a pool correction,
// forceReopenDownstreamChain for a knockout one or a reopen): each target the old
// winner reached is cleared back to its feeder placeholder ONLY if nobody has
// touched it since, judged per target rather than all-or-nothing, so an
// untouched final is not left advertising a pairing because the 3rd-place
// match beside it was played. A target that was played is left exactly as it
// is: it is one hop further than this correction reaches, and it gets its own
// warning when the reopened match is fought again and its new result
// propagates (the operator's one-decision-per-round ruling).
//
// The targets are the ones propagatedDownstreamOf names, so the hop is counted
// past any bye the old winner was passed through, as every other door counts
// it: next is the first match past the byes, the one a person could have
// fought. An untouched next has the whole chain unwound (unwindChain): each
// bye unresolved and next's slot back to its placeholder. Judging the bye
// itself instead read it as played (generation completes it, and the
// auto-resolution gives it a winner), so a pool correction left the displaced
// qualifier seated past the bye, in a match that could then be started with
// both sides named. A played next keeps the byes before it as well: they carry
// the winner it shows, and re-fighting the reopened match warns about it
// through them.
func retractIntoUntouched(bracket *state.Bracket, rIdx, mIdx int) {
	d := propagatedDownstreamOf(bracket, rIdx, mIdx)
	kept := func(t *state.BracketMatch) bool {
		if t == nil || !bracketMatchStartedOrScored(t) {
			return false
		}
		log.Printf("engine: bracket match %s keeps the winner propagated into it: it is %s, so the reopened match %s does not retract it", t.ID, t.Status, bracket.Rounds[rIdx][mIdx].ID)
		return true
	}
	if !kept(d.bronze) {
		clearPropagatedSlots(bracket, rIdx, mIdx, d.bronze, nil)
	}
	if !kept(d.next) {
		d.unwindChain(bracket, rIdx, mIdx)
	}
}

// clearPropagatedSlots is the mutation half of retractPropagatedWinner, shared
// with retractIntoUntouched: it returns the slot (rIdx, mIdx) fed in each
// given target to what it held before propagateBracketWinner wrote there. A
// nil target is left alone.
func clearPropagatedSlots(bracket *state.Bracket, rIdx, mIdx int, bronze, next *state.BracketMatch) {
	if bronze != nil {
		// Mirror propagateBracketWinner's positional assignment: semifinal
		// mIdx 0 feeds the bronze SideA, mIdx 1 feeds SideB. The id clears
		// alongside the name (bc-brid): propagateBracketWinner set both
		// together, so undoing it must clear both together too, or the
		// bronze slot would keep a stale id pointing at a name it no longer
		// carries.
		if mIdx%2 == 0 {
			bronze.SideA = ""
			bronze.SideAID = ""
		} else {
			bronze.SideB = ""
			bronze.SideBID = ""
		}
	}
	if next != nil {
		// winnerOfPlaceholder (bracket.go) is the ONE producer this now shares
		// with generation and parseWinnerOf: depth is 1-based from the final,
		// so the source match at round rIdx is depth len(Rounds)-rIdx.
		placeholder := winnerOfPlaceholder(len(bracket.Rounds)-rIdx, mIdx)
		// Same id-follows-name rule as the bronze clear above: a "Winner of
		// ..." placeholder is not a resolved competitor, so its slot must
		// carry no id (bc-brid).
		if mIdx%2 == 0 {
			next.SideA = placeholder
			next.SideAID = ""
		} else {
			next.SideB = placeholder
			next.SideBID = ""
		}
	}
}

// bracketMatchStartedOrScored reports whether a downstream bracket match
// is anything other than an untouched scheduled slot: running/completed
// status, a winner, recorded bouts, or a scoreline all block a reopen.
func bracketMatchStartedOrScored(bm *state.BracketMatch) bool {
	return bm.Status == state.MatchStatusRunning ||
		bm.Status == state.MatchStatusCompleted ||
		bm.Winner != "" ||
		len(bm.SubResults) > 0 ||
		len(bm.IpponsA) > 0 ||
		len(bm.IpponsB) > 0
}

// applyKachinukiMerge merges an incoming kachinuki bout log into the stored
// prior log by position via mergeKachinukiSubResults. No-op for individual,
// fixed-format, or missing competitions. Shared by the locked and tx scoring
// paths (RecordMatchResultTx/RecordMatchResult AND, via
// RecordMatchResultWithIneligibilityTx, RecordDecisionTx) so the merge guard
// cannot drift between them.
//
// On a COMPLETED write (the operator's explicit "End match", mp-gmcg) it
// additionally strips trailing UNSCORED bouts after the merge:
// MaybeAdvanceKachinuki auto-appends the next pairing after every scored
// bout, so ending the match leaves an abandoned empty pairing at the tail.
// Because the merge preserves stored entries by position, a client simply
// omitting that row cannot remove it, the server must strip it here so it
// never reaches standings/exports. It then derives the winner from the
// cleaned bout log (deriveKachinukiWinner) so a plain bout-driven End write
// cannot carry an outcome the log itself does not support.
func applyKachinukiMerge(comp *state.Competition, prior, result *state.MatchResult) error {
	if !comp.IsKachinuki() {
		return nil
	}
	var stored []state.SubMatchResult
	if prior != nil {
		stored = prior.SubResults
	}
	result.SubResults = mergeKachinukiSubResults(stored, result.SubResults)
	if result.Status == state.MatchStatusCompleted {
		result.SubResults = stripTrailingUnscoredKachinukiBouts(result.SubResults)
		return deriveKachinukiWinner(result)
	}
	return nil
}

// deriveKachinukiWinner authoritatively derives result.Winner from the
// merged bout log's LAST bout carrying a recorded outcome, mirroring
// deriveKachinukiEndOutcome (admin_scoring_team.jsx): OPERATOR INPUT
// DETERMINES THE BOUT OUTCOME. This is the server-side twin of that client
// rule (mp-gmcg review C3): before this, the client simply HID the generic
// "Save correction" button that would have let an operator pick an
// unsupported winner, but nothing stopped a bulk /scores write, an offline
// flush, or any future caller from completing a kachinuki match with a
// winner the bout log does not support — the invariant was enforced by not
// rendering a button, which is not enforcement.
//
// Scoped STRICTLY to decision == kachinuki-exhaustion: that value is UNIQUE
// to the plain bout-driven End-match write (buildPatch("completed",
// {endOutcome}) in admin_scoring_team.jsx always sends it for a decisive
// end, "hikiwake" for a drawn one). kiken/fusenpai/fusensho/daihyosen
// completions reach this SAME merge point (RecordDecisionTx ->
// RecordMatchResultWithIneligibilityTx -> applyKachinukiMerge) with THEIR
// OWN decision values and their own winner rule — the non-withdrawing side,
// already set on result.Winner before this runs (see RecordDecisionTx) —
// and re-deriving from the bout log there would silently overturn a
// legitimate walkover, which is exactly the "loser kept its struck ippons"
// case FIK Art. 32 protects. "hikiwake" and "" carry no winner to derive.
//
// It also REJECTS a kachinuki-exhaustion write whose last scored bout is a DRAW
// (mp-gmcg review R2): exhaustion means the final pairing was decisive and the
// loser had no replacement, so a tied last bout contradicts the decision. C3
// closed "wrong winner when the log names one"; this closes the residue where
// the log names NO winner yet the client still sends one (for a bracket match
// validateBracketCompletion only checks the winner is non-empty, so it would
// otherwise pass). A genuine tie is "hikiwake" in pools, or goes to encho in a
// knockout — which gives the bout a winner. A completed write with NO scored
// bout at all leaves the client's winner untouched (a separate degenerate case,
// out of scope here).
func deriveKachinukiWinner(result *state.MatchResult) error {
	if result == nil || result.Decision != string(domain.DecisionKachinukiExhaustion) {
		return nil
	}
	var last *state.SubMatchResult
	for i := range result.SubResults {
		s := &result.SubResults[i]
		if s.Position <= 0 || isUnscoredKachinukiBout(*s) {
			continue
		}
		if last == nil || s.Position > last.Position {
			last = s
		}
	}
	if last == nil {
		return nil
	}
	if last.Winner == "" {
		return validationErrorf("a kachinuki match ending on a tied bout is not a decisive win: record the last bout's winner, take it to overtime, or end the encounter as a draw (hikiwake)")
	}
	// MEMBER IDS FIRST (operator ruling bc-pnum). The deciding bout names its
	// winner by the FIGHTER's name, and two opposing fighters may legally
	// share one, so the name comparison below cannot tell them apart and its
	// case order silently handed the ENCOUNTER to side A. That is the same
	// coin flip as the individual victory, one level up and far more
	// expensive: it decides who advances. The score editor stamps all three
	// member ids on the row it writes, so this settles every bout scored
	// through it.
	switch domain.AttributeWinnerSide(domain.SubBoutAttribution(last.Attribution())) {
	case domain.MatchSideA:
		result.Winner = result.SideA
		return nil
	case domain.MatchSideB:
		result.Winner = result.SideB
		return nil
	}
	// No ids, and the two fighters share a name. The name comparison below
	// MUST NOT run: both of its cases match, and the first one wins, which
	// is how the encounter used to be handed to side A. The only evidence
	// left is the operator's own verdict, already on result.Winner from the
	// payload, so keep it -- rather than overturning it by case order, and
	// rather than rejecting the write, which would leave a legitimate
	// encounter on legacy data impossible to finish at all. A verdict naming
	// neither team is still refused: the relaxation trusts the operator's
	// CHOICE OF SIDE, not any string they send.
	if last.SideA != "" && last.SideA == last.SideB {
		if result.Winner == result.SideA || result.Winner == result.SideB {
			return nil
		}
		return validationErrorf("a kachinuki match's deciding bout was fought between two competitors with the same name (%q) and carries no member ids, so the winner cannot be derived: record the encounter winner from the score editor, which knows which side scored", last.SideA)
	}
	switch {
	case isWinForSide(last.Winner, result.SideA, last.SideA):
		result.Winner = result.SideA
	case isWinForSide(last.Winner, result.SideB, last.SideB):
		result.Winner = result.SideB
	default:
		// The deciding bout names a winner that matches NEITHER side (mp-gmcg
		// review, R2 residue). A kachinuki bout persists the PLAYER name as its
		// winner (resolveKachinukiBoutSides never writes the team name), so the
		// only thing this can match is last.SideA/last.SideB. A payload carrying
		// a sub winner + ippons but omitting the sub's sideA/sideB — a bulk
		// PUT /scores, an offline flush, any non-editor caller — reaches here,
		// and without this guard result.Winner would keep whatever the client
		// sent (for a bracket match validateBracketCompletion only checks it is
		// non-empty). Reject the unattributable winner rather than accept it; the
		// score editor always sends the sub sides.
		return validationErrorf("a kachinuki match's deciding bout names a winner (%q) that is neither competitor: send the bout's sideA/sideB so the encounter winner can be derived", last.Winner)
	}
	return nil
}

// isUnscoredKachinukiBout reports whether a bout carries no recorded
// outcome or score at all: no winner, no decision, no real ippons on
// either side, no hansoku, no hantei, no encho marker. Such a row is the
// placeholder pairing MaybeAdvanceKachinuki appends for a bout that was
// never fought. The encho test uses the canonical EnchoMetadata.On()
// predicate (the same one validation and the (E) label share), so a
// degenerate zero-period block is NOT mistaken for a real marker that would
// wedge an unscored placeholder into a completed record.
func isUnscoredKachinukiBout(s state.SubMatchResult) bool {
	return s.Winner == "" &&
		s.Decision == "" &&
		countScoringIppons(s.IpponsA) == 0 &&
		countScoringIppons(s.IpponsB) == 0 &&
		s.HansokuA == 0 &&
		s.HansokuB == 0 &&
		!s.HanteiDecided() &&
		!s.Encho.On()
}

// stripTrailingUnscoredKachinukiBouts drops TRAILING unscored bouts from a
// kachinuki bout log (highest positions downward), stopping at the first
// scored row. Interior unscored rows are never touched (stripping them
// would renumber history), and the walk stops entirely on any row with
// Position <= 0 so a legacy daihyosen sentinel (Position -1, ordered last
// by mergeKachinukiSubResults) is never deleted or skipped over.
func stripTrailingUnscoredKachinukiBouts(subs []state.SubMatchResult) []state.SubMatchResult {
	end := len(subs)
	for end > 0 {
		s := subs[end-1]
		if s.Position <= 0 || !isUnscoredKachinukiBout(s) {
			break
		}
		end--
	}
	return subs[:end]
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
//
// Member ids (bc-tmid pass 2): a bout the engine itself appended
// (appendNextKachinukiBout, drawn from a resolved lineup slot) already
// carries its two fighters' SideAMemberID/SideBMemberID before the
// operator ever scores it. The score editor does not yet round-trip them
// on the wire (pass three's change), so its write for the SAME position
// would otherwise silently overwrite them with nothing on every "overwrite
// the stored entry at the same position" above -- the exact
// verdict-silence problem preserveSubHantei solves for the daihyosen mark,
// here for the member id instead. preserveKachinukiMemberIDs inherits the
// stored ids onto an incoming row that is silent about them (empty) AND
// still names the same fighter on that side, and SubMatchResult.
// ResolveMemberWinnerID then derives WinnerMemberID from whichever side
// ids are now known -- the SAME derivation the legacy-load repair uses
// (state.resolveSubMemberIDs), so the rule has one owner between the two
// call sites.
func mergeKachinukiSubResults(stored, incoming []state.SubMatchResult) []state.SubMatchResult {
	storedByPos := make(map[int]state.SubMatchResult, len(stored))
	byPos := make(map[int]state.SubMatchResult, len(stored)+len(incoming))
	for _, s := range stored {
		storedByPos[s.Position] = s
		byPos[s.Position] = s
	}
	for _, s := range incoming {
		preserveKachinukiMemberIDs(storedByPos, &s)
		s.ResolveMemberWinnerID()
		byPos[s.Position] = s
	}
	numbered := make([]int, 0, len(byPos))
	hasDaihyosen := false
	for p := range byPos {
		if p == state.DaihyosenSubPosition {
			hasDaihyosen = true
			continue
		}
		if p < 0 {
			// Malformed negative position (not the daihyosen sentinel). Drop it
			// rather than preserve-and-sort-it-first, mirroring the defensive
			// Position <= DaihyosenSubPosition skip used by every aggregate
			// (state.TeamResultFrom etc.): real bouts are non-negative.
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

// preserveKachinukiMemberIDs inherits storedByPos[in.Position]'s member
// ids onto in when in is silent about them (empty) AND the two rows still
// name the SAME fighter on that side -- the same side-matching guard
// preserveLoserScore/preserveSubHantei use, so a position genuinely
// re-used for a different pairing (only reachable via a hand-edited file;
// a real pairing is never re-used once it has fought) is never
// mis-attributed to the wrong fighter.
func preserveKachinukiMemberIDs(storedByPos map[int]state.SubMatchResult, in *state.SubMatchResult) {
	prior, ok := storedByPos[in.Position]
	if !ok {
		return
	}
	if in.SideAMemberID == "" && prior.SideAMemberID != "" && in.SideA == prior.SideA {
		in.SideAMemberID = prior.SideAMemberID
	}
	if in.SideBMemberID == "" && prior.SideBMemberID != "" && in.SideB == prior.SideB {
		in.SideBMemberID = prior.SideBMemberID
	}
}

// findTeamMatch locates a match by ID, returning the parent record (a
// copy), a flag indicating whether it was found in the bracket store
// rather than the pool store, and the bracket round index (0 for pool
// matches, rIdx for bracket matches, len(Rounds) for the ThirdPlaceMatch
// so round-scoped lineup resolution prefers the bronze's own stage,
// matching the client's derivedBracket.rounds.length).
func (e *Engine) findTeamMatch(compID, matchID string) (*state.MatchResult, bool, int, error) {
	// A load error is returned, never read as "no such match" (see
	// findMatchHome): a nil parent is reserved for an id in neither store.
	poolMatches, err := e.store.LoadPoolMatches(compID)
	if err != nil {
		return nil, false, 0, err
	}
	for i := range poolMatches {
		if poolMatches[i].ID == matchID {
			m := poolMatches[i]
			return &m, false, 0, nil
		}
	}
	bracket, err := e.store.LoadBracket(compID)
	if err != nil {
		return nil, false, 0, err
	}
	if bracket != nil {
		for rIdx, round := range bracket.Rounds {
			for _, bm := range round {
				if bm.ID == matchID {
					return bracketMatchToTeamResult(bm), true, rIdx, nil
				}
			}
		}
		// The single-3rd-place (bronze) match is a sibling of
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
		ID:    bm.ID,
		SideA: bm.SideA,
		SideB: bm.SideB,
		// SideAID/SideBID carry the bracket match's own id-only side
		// resolution (bc-brid) through this read-only projection, so a
		// caller that needs to resolve a bracket-origin bout's fighter to a
		// squad member (kachinuki_export.go's team-number/label lookup) can
		// do so without a second, parallel read of the raw BracketMatch.
		// This function is a read-only projection used by callers that only
		// ever inspect the result (findTeamMatch, MaybeAdvanceKachinuki's
		// advancement math, the kachinuki detail export); it is never fed
		// into a match-write policy (matchWriteForward/matchWriteRestore),
		// so adding fields here carries none of the "omitted field means
		// clear" risk that governs bracketMatchAsResult's own projection in
		// bracket_result.go.
		SideAID:     bm.SideAID,
		SideBID:     bm.SideBID,
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
// The full ordered roster (from lineup.OrderedMembers, bc-tmid pass 2) is
// filtered by IsMemberRetired against RetiredPlayersFromBoutLog's sets to
// produce the remaining queue, so a lineup-resolved fighter's member id
// (when the slot has one) rather than their possibly-stale name decides
// whether they have retired -- falling back to the name only where the
// retiring bout row carried no id to match against and the name belongs to
// one member of this roster alone. See IsMemberRetired for why both tiers
// are needed and what the gate protects.
func (e *Engine) kachinukiRemainingRoster(compID, matchID string, comp *state.Competition, parent *state.MatchResult, roundIdx int) ([]kachinukiFighter, []kachinukiFighter, bool) {
	retiredA, retiredB := RetiredPlayersFromBoutLog(parent.SubResults, parent.SideA, parent.SideB)

	// Attempt lineup-based roster resolution.
	lineupFor := e.lineupInForce(compID, matchID, comp, roundIdx)

	resolveRoster := func(teamName string, retired RetiredMemberSet) ([]kachinukiFighter, bool) {
		if lineup, found := lineupFor(teamName); found {
			full := lineup.OrderedMembers(comp.TeamSize)
			fighters := make([]kachinukiFighter, len(full))
			for i, slot := range full {
				fighters[i] = kachinukiFighter{Name: slot.Name, MemberID: slot.MemberID}
			}
			return filterRemainingFighters(fighters, retired), true
		}
		// Preserve first-appearance order from the bout log: AdvanceKachinuki
		// treats this slice as an ordered queue (index 0 is the next fighter
		// in), so a map-iteration order would make the next pairing
		// nondeterministic when a kachinuki match runs without saved lineups.
		// A row's fighter is keyed by member id when it carries one (a
		// fighter picked by squad number on a match with no saved lineup
		// has an id and no name, bc-dnst) and by name otherwise, so a
		// nameless pick is still a queue entry and IsMemberRetired settles
		// each by the same id-then-name order the lineup branch uses.
		seen := map[string]struct{}{}
		out := make([]kachinukiFighter, 0)
		isA := teamName == parent.SideA
		for _, b := range parent.SubResults {
			if b.Position == state.DaihyosenSubPosition {
				continue // rep bout, not a roster player (see RetiredPlayersFromBoutLog)
			}
			f := kachinukiFighter{Name: b.SideB, MemberID: b.SideBMemberID}
			if isA {
				f = kachinukiFighter{Name: b.SideA, MemberID: b.SideAMemberID}
			}
			key := f.MemberID
			if key == "" {
				key = f.Name
			}
			if key == "" {
				continue
			}
			if _, dup := seen[key]; dup {
				continue
			}
			seen[key] = struct{}{}
			out = append(out, f)
		}
		// Filtered through the SHARED helper, in a second pass, for the reason
		// its own doc gives: the roster IS the context the name tier needs, so
		// it cannot be judged one fighter at a time while the roster is still
		// being built. Filtering inline passed a nil ambiguity set, which let
		// the name tier fire for a fighter whose OWN member id proves they have
		// not retired: two teammates sharing a display name (grandfathered
		// pre-uniqueness data) meant one losing retired the other, who was then
		// never fielded. The lineup branch above has always used this helper.
		return filterRemainingFighters(out, retired), false
	}

	remainingA, foundA := resolveRoster(parent.SideA, retiredA)
	remainingB, foundB := resolveRoster(parent.SideB, retiredB)
	return remainingA, remainingB, foundA || foundB
}

// lineupInForce loads a competition's saved lineups once and returns the
// resolver for "which lineup is in force for this side of matchID": a
// match-scoped lineup first, else the round-scoped one for roundIdx, per
// state.FindBestLineupAny's tiers. The resolver answers false when the side
// has no saved lineup, including when the lineups could not be loaded (the
// error is logged under the name of its one caller, kachinukiRemainingRoster).
func (e *Engine) lineupInForce(compID, matchID string, comp *state.Competition, roundIdx int) func(teamName string) (domain.TeamLineup, bool) {
	lineups, err := e.store.LoadTeamLineups(compID)
	if err != nil {
		log.Printf("engine.kachinukiRemainingRoster compId=%s matchId=%s: lineup load error: %v; resolving no lineup", compID, matchID, err)
		lineups = nil
	}

	// The lineup editor keys lineups by the team PARTICIPANT ID
	// (player.id, a UUID) while match sides carry the team display NAME,
	// so translate each side name to its participant ID and try both keys
	// ("match on id OR name"). A participant load failure only degrades
	// the lookup to name-only.
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
	return func(teamName string) (domain.TeamLineup, bool) {
		if lineups == nil {
			return domain.TeamLineup{}, false
		}
		return state.FindBestLineupAny(lineups, teamKeys(teamName), matchID, roundIdx)
	}
}

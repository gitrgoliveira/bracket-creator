package engine

import (
	"log"
	"slices"

	"github.com/gitrgoliveira/bracket-creator/internal/domain"
	"github.com/gitrgoliveira/bracket-creator/internal/state"
)

// preserveSubHantei enforces the operator ruling "all results must be
// recorded into storage" at the store boundary: a writer that says NOTHING
// about the daihyosen verdict (DecidedByHantei nil) must not erase a
// recorded one. The verdict travels (flag + winner) onto the incoming
// daihyosen row only when that row is verdict-silent, names no winner of its
// own, carries a decision hantei can coexist with, and is still tied (an
// untied row cannot carry a hantei). An EXPLICIT false and a named winner
// both pass through untouched. The preserveLoserScore precedent, one bout
// deeper. The silence test itself is KeepsStoredDaihyosenVerdict, which the
// self-run guard shares.
//
// NIL vs EMPTY on IpponsA/IpponsB is the other half of "verdict-silent":
// scoreline-silence is judged on whether the writer sent an ippon array AT
// ALL (nil, the key absent from the payload), never on whether the array is
// empty of SCORING ippons. The two are not the same thing.
//
// WHO ACTUALLY SENDS NIL. The team editor (web-mobile/js/admin_scoring_team.jsx
// buildPatch) is the one client this engine has to reason about, and it is
// NOT silent by default: it states the daihyosen row's ippons explicitly
// (built from the bout's own point totals, then round-tripped through
// api_serializers.jsx's toBackendMatchResult, which folds the editor's
// decidedByHantei flag into the ippons and never forwards the flag itself
// onto the wire) whenever the operator has touched that row THIS session, or
// the row's stored verdict/score/overtime is already known locally (an
// editor that mounted after a verdict existed re-states what it was shown,
// same behaviour as before). buildPatch omits ipponsA/ipponsB entirely -
// the genuine-silence shape this function relies on - ONLY when BOTH hold:
// the row is untouched (identical to its mount-time seed, including the
// hantei arm/pick and the daihyosen encho counter) AND nothing about it is
// known locally (no recorded verdict, no scored points/fouls/draw, no
// overtime). That is exactly the stale-editor case this preserve exists for
// (an SSE gap or an offline-queue replay landing after a verdict was
// recorded elsewhere): the editor genuinely has nothing to say about the
// row, so it says nothing, and this function restores the stored verdict.
// A deliberate 0-0 withdrawal is the row-is-known-locally branch: it still
// sends an explicit `ipponsA: []` for both sides - a 0-0 scoreline the
// writer DID state, same as a 1-1 withdrawal states "D"/"T" - so it is never
// mistaken for silence.
//
// quick-score (handlers_match.go) is a SEPARATE, already-documented gap, not
// a nil-ippons case: it synthesises positions 1..N only and never emits a
// position -1 row at all, so this function's per-row loop simply never
// matches it (see the SCOPE note below on the delete contract).
//
// Testing scoring-ippon COUNT instead of nil-ness could not tell a silent
// write apart from a 0-0 withdrawal: an empty slice and a nil slice both
// count zero scoring ippons, so a 0-0 withdrawal would read as silence and
// the verdict would be copied right back onto the row the operator just
// cleared. See countScoringIppons below for the (correct, separate) job
// that helper still does: tallying points, not detecting silence.
//
// SCOPE, deliberately narrow in two directions:
//
//   - It PATCHES an existing position -1 row; it never RE-APPENDS one the
//     incoming payload dropped. DELETE /daihyosen removes that row on purpose
//     (handlers_daihyosen.go) and writes through this same path, so
//     resurrecting a missing row would make an unscored rep bout undeletable.
//     A writer that drops the row entirely (quick-score synthesises positions
//     1..N only) therefore still discards it - that is the delete contract, not
//     an oversight.
//   - It runs in the engine, AFTER the HTTP validator, so a row it mutates is
//     never re-validated. Every field it writes must land on a row that would
//     have passed validateSubBout, which is why the decision allow-list below
//     mirrors that validator exactly: without it a verdict-silent
//     `decision:"kiken-voluntary"` row would be stamped into a bout that is
//     simultaneously a withdrawal and a judges' decision, and SideMarksLR
//     would then emit both `Kiken` and `Ht`.
//
// hanteiStillHolds reports whether a MATCH-level hantei verdict is still valid
// for the incoming result. It is the engine-side re-application of the
// DecidedByHantei block in ScoreRequest.Validate, and it must enforce that
// block's conditions IN FULL, because this runs after validation and its output
// is never re-checked: whatever it stamps is persisted unexamined.
//
// All four conditions, and what each one closed:
//
//   - A NAMED WINNER. A hantei declares one; the validator refuses the pairing
//     without it. Omitting it here let a `{status:"running", winner:null}`
//     reopen inherit the verdict, and a mark with no winner is unattributable
//     to either side — stripInvalidHantei then strips it (drop-never-guess,
//     the same policy an unattributable winner-less verdict gets everywhere
//     else in the scoring code).
//   - COMPLETED (or unstated, which applyBracketMatchResult defaults to
//     completed). A match still running has not been decided by anyone.
//   - A COMPATIBLE DECISION, through the MATCH-level predicate. Using the
//     sub-bout one let a verdict-silent `decision:"daihyosen"` write inherit a
//     match-level verdict, claiming the encounter itself was judged.
//   - A TIED SCORELINE (FIK 7-5 / 29-6), the rule the verdict rests on.
//   - NO DecisionBy / DecisionReason. Those two are the withdrawal audit trail
//     (who called it, and why), so a result carrying either is recording a
//     decision someone MADE, not a tied bout the referees judged. The validator
//     refuses the pairing; this twin was missing both, so "IN FULL" above was an
//     overclaim until they were added. Reachable only through the paths that do
//     not run ScoreRequest.Validate, which is precisely why this function exists.
func hanteiStillHolds(r *state.MatchResult) bool {
	return r.Winner != "" &&
		(r.Status == "" || r.Status == state.MatchStatusCompleted) &&
		domain.IsMatchHanteiCompatibleDecisionStr(r.Decision) &&
		domain.HanteiTiedScoreline(r.IpponsA, r.IpponsB) &&
		r.DecisionBy == "" && r.DecisionReason == ""
}

// stripInvalidHantei enforces the mark's validity on a FORWARD write from the
// paths that do not run ScoreRequest.Validate. The verdict rides in the
// winner's ippon slice, so a re-score replaces it atomically with the
// scoreline it rests on and there is no separate flag to carry. What remains
// is the CONTRADICTION guard preserveMatchHantei used to provide: a mark that
// no longer satisfies its own preconditions (hanteiStillHolds), or that sits
// on a side that is not the winner's, is stripped. The points stay, the
// verdict goes.
//
// WHERE AN INVALID MARK ACTUALLY COMES FROM. Not from the decision twins:
// preserveLoserScore filters the inherited slice through struckIppons, and
// domain.IsScoringIppon drops the mark, so a kiken over a stored hantei
// arrives with the mark ALREADY gone (pinned by
// TestPreserveLoserScoreDropsTheHanteiMark). This comment used to claim that
// path as the reason this function exists; it was wrong, and the test that
// pinned the guard hand-built a payload the decision path cannot produce.
//
// The reachable producer is a handler that copies the STORED match wholesale
// and changes something the verdict depended on. handlers_daihyosen.go's add
// path does exactly that - `u := *match`, then Status flips to running - so a
// stored match-level verdict arrives on a result whose status alone now fails
// hanteiStillHolds. (Its delete path strips the mark itself before writing;
// the add path relies on this guard.)
//
// WHY THIS STRIPS RATHER THAN REJECTS, and must keep doing so: the mark it
// removes is one the write INHERITED, not one the caller sent. Returning a
// validation error here would answer an operator's daihyosen with a 400 for a
// mark already on disk that their request never carried, and no endpoint could
// repair it - the same failure applyHansokuIppons' fold scoping exists to
// avoid, and the same rule: a write answers for what it introduces, not for
// what it inherited. A mark the CLIENT sent is a different matter and is
// rejected, at the boundary that can still tell whose fault it is, by
// mobileapp.validateHanteiMarkPlacement.
//
// The strip is logged. It discards an operator-visible verdict on an otherwise
// successful write, so a silent version leaves a 200 with the verdict gone and
// nothing to investigate.
//
// Forward only. matchWriteRestore replays a trusted snapshot verbatim;
// re-testing it there would let this rewrite the state being restored.
func stripInvalidHantei(result *state.MatchResult) {
	if !result.HanteiDecided() {
		return
	}
	if !hanteiStillHolds(result) {
		result.IpponsA = domain.StripHantei(result.IpponsA)
		result.IpponsB = domain.StripHantei(result.IpponsB)
		logStrippedHantei(result, "its preconditions no longer hold")
		return
	}
	// Winner-side pin: the mark names the competitor the referees chose.
	// Attribution goes through domain.AttributeWinnerSide, the one owner of
	// "which side won" (ids win over names, so a same-name pair - legal:
	// two participants from different dojos may share a name - can't send
	// the mark to the wrong side just because names alone couldn't tell
	// them apart).
	side := domain.AttributeWinnerSide(domain.WinnerAttribution{
		WinnerID: result.WinnerID, SideAID: result.SideAID, SideBID: result.SideBID,
		Winner: result.Winner, SideA: result.SideA, SideB: result.SideB,
	})
	if domain.ContainsHantei(result.IpponsA) && side != domain.MatchSideA {
		result.IpponsA = domain.StripHantei(result.IpponsA)
		logStrippedHantei(result, "it sat on a side that is not the winner's")
	}
	if domain.ContainsHantei(result.IpponsB) && side != domain.MatchSideB {
		result.IpponsB = domain.StripHantei(result.IpponsB)
		logStrippedHantei(result, "it sat on a side that is not the winner's")
	}
}

// logStrippedHantei records a discarded verdict. Every field a reader needs to
// tell an inherited mark from a client-sent one is in the line: which match,
// which precondition failed, and the winner/status/decision the mark was
// judged against.
func logStrippedHantei(result *state.MatchResult, why string) {
	log.Printf("engine: dropped the hantei mark on match %q because %s (winner=%q status=%q decision=%q); the points were kept",
		result.ID, why, result.Winner, result.Status, result.Decision)
}

// KeepsStoredDaihyosenVerdict reports whether a write whose representative
// bout row is in keeps the hantei verdict of prior, the stored row (nil when
// there is none): prior carries the verdict and the winner it names, and in
// says nothing about either, so preserveSubHantei carries prior's verdict and
// scoreline onto it. It is the one owner of that silence test. The self-run
// judge asks it too (mobileapp.holdSelfReportedWriteUnderTx), so a
// participant's write that would erase an organiser's verdict is told apart
// from one that keeps it by the same test the write itself applies.
func KeepsStoredDaihyosenVerdict(prior, in *state.SubMatchResult) bool {
	if prior == nil || !prior.HanteiDecided() || prior.Winner == "" {
		return false
	}
	if in.Winner != "" || in.DecidedByHantei != nil ||
		domain.ContainsHantei(in.IpponsA) || domain.ContainsHantei(in.IpponsB) {
		return false // the writer addressed the verdict: its word stands
	}
	// The SAME predicate validateSubBout enforces, shared via domain so the
	// two cannot drift (the restore runs after validation and is never
	// re-checked).
	if !domain.IsSubBoutHanteiCompatibleDecisionStr(in.Decision) {
		return false
	}
	// Side-guarded, like the preserveLoserScore precedent: IpponsA/IpponsB
	// are POSITIONAL, so copying them onto a row whose sides are named in
	// the opposite order mirrors the letters and credits each side with the
	// other's points. reconcileSides normalises at MATCH level only, never
	// per sub-bout, so a drifted or hand-built payload can reach here
	// swapped. An unnamed incoming row (the common stale-snapshot shape)
	// inherits the names along with the scoreline.
	//
	// ABANDON on a mismatch rather than skipping only the copy. Winner is a
	// NAME, so it needs no positional guard of its own - but it names one of
	// the STORED pair, and stamping it onto a row naming a different pair
	// attributes the verdict to neither competitor present. The tie check in
	// preserveSubHantei cannot catch that: with the copy skipped the incoming
	// row is still empty, so it compares 0 against 0 and passes vacuously.
	// deriveDaihyosenWinner then matches no side and leaves the encounter
	// with a hantei-decided rep bout and no winner at all, which a bracket
	// completion rejects and pool standings score as a draw for both teams.
	if (in.SideA != "" || in.SideB != "") &&
		(in.SideA != prior.SideA || in.SideB != prior.SideB) {
		return false
	}
	// Two outstanding fouls on a side fold into an "H" for the other side
	// before a write is stored (applyHansokuIppons), which gives a row
	// without ippons an array of its own. Such a row is not silent, so the
	// answer is the same asked before the fold (the self-run judge) as
	// after it (preserveSubHantei).
	if in.HansokuA >= 2 || in.HansokuB >= 2 {
		return false
	}
	// A row that supplies NEITHER ippon array said nothing about the
	// SCORELINE either, so the stored one travels with the verdict it
	// rests on. Without this the verdict lands on an all-empty row and the
	// struck ippons, the outstanding fouls, the overtime marker and the
	// sub-decision are all lost: a 1-1 hantei would persist as 0-0, which
	// moves the `Ht` to the other slot (resultSlot fills outside-to-inside)
	// and drops the `(E)`.
	//
	// A row that DOES supply an ippon array — even one that is EMPTY, and
	// even a stale second-device replay whose own scoreline happens to
	// still read tied, e.g. a markless 1-1 offline-queue replay — has
	// spoken for the scoreline itself, and the verdict must not travel
	// onto it: the mark lives IN the copied ippons, so stamping the winner
	// without also copying the mark-carrying scoreline would split the two
	// permanently. The referees' record would be gone from disk while its
	// consequence, the winner, survived — and once this write lands as the
	// new stored row, a future preserve has no mark left to re-attach.
	// ABANDON here, before any field is touched, rather than letting the
	// tie check decide: that check answers "is this scoreline still tied",
	// not "did the writer supply it", so it cannot distinguish an own tied
	// scoreline from the copied one.
	//
	// This is a NIL check, deliberately not a scoring-ippon COUNT: an
	// explicit `[]` (the team editor's 0-0 daihyosen withdrawal - see
	// preserveSubHantei's doc) and an omitted key (a genuinely silent stale
	// snapshot) both count zero scoring ippons, but only the second one
	// is silence. countScoringIppons cannot tell them apart; nil-ness
	// can, because Go's JSON decoder only produces nil for an absent key
	// (SubMatchResult.IpponsA/IpponsB carry no `omitempty`, so a present
	// `[]` always decodes to a non-nil empty slice). A bug fixed here:
	// treating an explicit 0-0 withdrawal as silence resurrected the
	// verdict the operator had just cleared, because the copy would then
	// run and copy the stored Ht mark straight back.
	return in.IpponsA == nil && in.IpponsB == nil
}

func preserveSubHantei(stored, incoming []state.SubMatchResult) {
	var prior *state.SubMatchResult
	if i := state.DaihyosenSubIndex(stored); i >= 0 {
		prior = &stored[i]
	}
	i := state.DaihyosenSubIndex(incoming)
	if i < 0 {
		return
	}
	in := &incoming[i]
	if !KeepsStoredDaihyosenVerdict(prior, in) {
		return
	}
	if in.SideA == "" && in.SideB == "" {
		in.SideA, in.SideB = prior.SideA, prior.SideB
	}
	in.IpponsA = append([]string(nil), prior.IpponsA...)
	in.IpponsB = append([]string(nil), prior.IpponsB...)
	// Hansoku travels WITH the ippons, not separately: an outstanding
	// foul is part of the same scoreline, and the two are coupled
	// (every second one discharges into an "H" ippon for the opponent,
	// applyHansokuIppons). Restoring the letters but not the counts
	// left a coherent stored pair as an incoherent restored one -
	// prior's discharged H's beside the incoming zero - so the
	// referee's outstanding ▲ vanished from every scoreboard and the
	// next foul on that side no longer discharged.
	in.HansokuA, in.HansokuB = prior.HansokuA, prior.HansokuB
	in.Encho = prior.Encho.Clone()
	if in.Decision == "" {
		in.Decision = prior.Decision
	}
	if !domain.HanteiTiedScoreline(in.IpponsA, in.IpponsB) {
		return // untied now: the verdict cannot stand on this scoreline
	}
	// The verdict itself travelled with the copied scoreline: prior's
	// ippons carry the domain.HanteiMark entry, so there is no flag left
	// to raise — only the winner the mark names. Reached ONLY when the
	// scoreline above was copied wholesale from prior (the silence test
	// guards it): the mark and the winner it names move as one atomic
	// unit, never separately.
	in.Winner = prior.Winner
	// Neither id is trusted, the stored one included: the representatives are
	// their own change, so even the stored id may disagree with the picks this
	// row now holds. The reconcile derives the id again from the winner's NAME
	// and those picks, so it is exactly the winning side's pick or empty
	// (SubMatchResult.ReconcileWinnerMemberID).
	in.ReconcileWinnerMemberID()
}

// preserveDaihyosenOutcome is the call every forward SubResults replacement
// makes: preserveSubHantei restores the bout-level verdict, then
// deriveDaihyosenWinner re-reads it into the ENCOUNTER's winner.
//
// Both halves are required, and the order is why they are bundled here rather
// than left as two calls. deriveDaihyosenWinner already runs earlier in each
// writer, but at that point the incoming daihyosen row is still verdict-silent
// and winner-less, so it finds nothing and leaves Winner empty; the preserve
// then stamps the row back. Without this second pass the stored state
// contradicts itself - the rep bout says "Kyoto won by hantei" while the match
// records no winner - and computeStandingsFrom, which keys W/L/T off
// MatchResult.Winner, credits BOTH teams a draw. It is idempotent: an explicit
// winner short-circuits deriveDaihyosenWinner, so a writer that genuinely
// names one is never overridden.
func preserveDaihyosenOutcome(stored []state.SubMatchResult, result *state.MatchResult) {
	if result == nil {
		return
	}
	preserveSubHantei(stored, result.SubResults)
	deriveDaihyosenWinner(result)
}

// isWinForSide reports whether subWinner indicates a win for the given
// match-level side. It checks both the canonical match side name and the
// sub-result-level side name (which may differ when the operator used a
// player name instead of the team name). The subSide != "" guard prevents
// "" == "" false-positives when sub-bout sides are unset (quick-score).
func isWinForSide(subWinner, matchSide, subSide string) bool {
	return subWinner == matchSide || (subSide != "" && subWinner == subSide)
}

// deriveDaihyosenWinner fills result.Winner from a completed daihyosen
// sub-result (Position == -1) when the caller has not set it explicitly.
// Knockout team matches end in daihyosen when IV and PW are tied; the
// operator scores a single representative bout whose winner becomes the
// team match winner. The sub-result Winner may be the representative
// player's name or the team name; this function maps it back to the
// canonical team name (result.SideA / result.SideB) using the same
// side-matching logic as computeStandings.
func deriveDaihyosenWinner(result *state.MatchResult) {
	if result == nil || result.Winner != "" {
		return
	}
	for _, sub := range result.SubResults {
		if sub.Position != state.DaihyosenSubPosition || sub.Winner == "" {
			continue
		}
		sideAWin := isWinForSide(sub.Winner, result.SideA, sub.SideA)
		sideBWin := isWinForSide(sub.Winner, result.SideB, sub.SideB)
		switch {
		case sideAWin:
			result.Winner = result.SideA
		case sideBWin:
			result.Winner = result.SideB
		}
		return
	}
}

// preserveLoserScore implements FIK Regulations Article 32 ("Any point
// scored by the shiai-funo-sha shall remain valid") on a default-win
// result. The winner keeps the maru default-win fill already set on
// `result`; this only touches the WITHDRAWING side, which keeps whatever it
// had struck, and preserves the encounter's prior sub-bouts so a team
// withdrawal never wipes the sub-bouts already fought (both teams' results
// stand and continue to count in IV/PW standings via accrueTeamSubResults).
//
// prior is the match state before the decision. When either record carries
// a side id (CarriesSideIDs -- a pool match, or, since bc-brid, a stamped
// bracket row), both records' ids must be present and equal or nothing is
// preserved -- a drifted or re-oriented prior must not mis-attribute
// points, so a mismatch is a non-match, not a guess. Otherwise (an
// id-less record: a legacy pool row, or an unstamped bracket row -- a bye,
// an unresolved feeder, or an unrepaired legacy row) SideA/SideB are
// compared by name instead.
//
// decisionBy names the WITHDRAWING side ("shiro" = SideB/Shiro, "aka" =
// SideA/Aka). Shared by the two RecordDecision twins.
func preserveLoserScore(result, prior *state.MatchResult, decisionBy string) {
	if prior == nil {
		return
	}
	if prior.CarriesSideIDs() || result.CarriesSideIDs() {
		if prior.SideAID == "" || prior.SideBID == "" || result.SideAID == "" || result.SideBID == "" {
			return
		}
		if prior.SideAID != result.SideAID || prior.SideBID != result.SideBID {
			return
		}
	} else if prior.SideA != result.SideA || prior.SideB != result.SideB {
		return
	}
	result.SubResults = prior.SubResults
	// Preserve only points the loser actually STRUCK (Art. 32 says "any point
	// scored"): strip the maru marker so a prior default-win decision's ○○
	// fill is never carried forward as if it were struck points. This matters
	// on the re-decision path when decisionBy flips — the side that was
	// the prior winner holds maru, not real points, and must not inherit it as
	// the new loser.
	if decisionBy == "shiro" {
		result.IpponsB = struckIppons(prior.IpponsB) // loser = SideB (Shiro)
	} else {
		result.IpponsA = struckIppons(prior.IpponsA) // loser = SideA (Aka)
	}
}

// withdrawalRuling is the stored half of preserveWithdrawalRuling: the fields
// of a match-level withdrawal (kiken, fusenpai) that RecordDecisionTx writes
// and a bout-row correction must not undo. It is a value projection, like
// storedSides, so the pool and bracket owners share ONE merge body even
// though one stores a MatchResult and the other a BracketMatch.
//
// The unit, field by field, is exactly what recordDecisionTx sets:
//   - Decision, DecisionBy, DecisionReason: the ruling itself, which side
//     withdrew and the operator's note on why.
//   - Winner, WinnerID: the side the ruling names as winner. A bout
//     correction derives a winner from the bouts, which says nothing about a
//     withdrawal and may name the other team.
//   - IpponsA, IpponsB: the winner's default-win maru (domain.DefaultWinIppons)
//     plus the points the withdrawing side had struck (preserveLoserScore).
//     Only the maru is ruling; keptWithdrawalScoreline decides what a
//     correction keeps of each side.
//   - Encho: whether the withdrawal came in overtime, which is what decided
//     one maru rather than two.
//
// Not in it: HansokuA/B (the decision path never sets them, so they are not
// part of the ruling), SubResults, CorrectionReason and ModifiedAt (the
// correction's own), and Status (the condition requires both to be
// completed).
type withdrawalRuling struct {
	Status         state.MatchStatus
	Decision       string
	DecisionBy     string
	DecisionReason string
	Winner         string
	WinnerID       string
	IpponsA        []string
	IpponsB        []string
	Encho          *state.EnchoMetadata
}

func rulingOfMatch(m *state.MatchResult) withdrawalRuling {
	return withdrawalRuling{
		Status: m.Status, Decision: m.Decision, DecisionBy: m.DecisionBy, DecisionReason: m.DecisionReason,
		Winner: m.Winner, WinnerID: m.WinnerID, IpponsA: m.IpponsA, IpponsB: m.IpponsB, Encho: m.Encho,
	}
}

func rulingOfBracketMatch(bm *state.BracketMatch) withdrawalRuling {
	return withdrawalRuling{
		Status: bm.Status, Decision: bm.Decision, DecisionBy: bm.DecisionBy, DecisionReason: bm.DecisionReason,
		Winner: bm.Winner, WinnerID: bm.WinnerID, IpponsA: bm.IpponsA, IpponsB: bm.IpponsB, Encho: bm.Encho,
	}
}

// KeepsWithdrawalRuling reports whether a write keeps the match-level
// withdrawal already recorded on the match (operator ruling 2026-09-24, bc-tmfn:
// "Save correction should just save what the operator enters"). It is scoped
// by the WRITE's shape, and every clause is load-bearing:
//   - stored completed with a default-win decision (domain.
//     IsDefaultWinDecisionStr: any kiken, fusenpai, or fusensho): there is a
//     ruling to keep. Widened from IsWithdrawalDecisionStr (bc-tmfn follow-
//     up) to also keep a stored FUSENSHO ruling on a correction: fusensho is
//     a per-bout default win within a team encounter, not a withdrawal of
//     the match it is recorded on, so it stays out of IsWithdrawalDecisionStr
//     and its eligibility side effect (recordIneligibilityFromDecision is
//     keyed on IsWithdrawalDecisionStr specifically, unaffected by this
//     widening) -- but the RULING itself must survive a score correction
//     exactly as a kiken/fusenpai's does, or a correction silently replaced
//     a stored fusensho with an empty decision (the whole-struct overwrite
//     in applyPoolWrite/applyBracketResultIn has nothing else to fall back
//     on once this reports false);
//   - incoming completed: a start, an autosave or a requeue never inherits;
//   - incoming not marked ClearsWithdrawal: the operator removed the ruling
//     in the editor and this write is the real result (operator ruling
//     2026-10-03: a fix leaves the match resolved), so it replaces the ruling
//     whatever decision it carries, a draw included;
//   - incoming decision "" or "hikiwake", and nothing else: that is a score
//     sheet's correction, and no score sheet can state a decision (the team
//     sheet's rows say nothing about the encounter, the individual sheet's
//     "" or its draw toggle say nothing about a withdrawal), so the write
//     corrects the scores and leaves the ruling. Every other decision IS the
//     operator ruling on the match: a withdrawal re-decides it or names the
//     other side, and fusensho, daihyosen, kachinuki-exhaustion or "fought"
//     replace it. An ALLOWLIST, not a denylist: a decision added later is a
//     replacement until someone decides it keeps the ruling, never the
//     reverse (a denylist missed fusensho and daihyosen, which POST
//     /decision sends with the prior bout rows attached by preserveLoserScore:
//     the kiken was kept and the default arm of recordDecisionTx still
//     restored the loser, leaving a stored kiken with an eligible loser).
//
// A withdrawal recorded by mistake is removed in one of two ways: this
// write with ClearsWithdrawal, which stores the real result and leaves the
// match finished (restoreIfWithdrawalRemoved lifts the bar), or a reopen
// (ReopenMatch), which puts the match back to running with its fought bouts
// kept, for a match that still has fighting left in it.
//
// Exported because the team finish gate (mobileapp.refuseUnfinishedTeamFinish)
// must exempt exactly the writes this keeps the ruling for, and a second copy
// of the condition there would drift from this one.
func KeepsWithdrawalRuling(storedStatus state.MatchStatus, storedDecision string, incoming *state.MatchResult) bool {
	return storedStatus == state.MatchStatusCompleted &&
		domain.IsDefaultWinDecisionStr(storedDecision) &&
		incoming.Status == state.MatchStatusCompleted &&
		!incoming.ClearsWithdrawal &&
		(incoming.Decision == "" || incoming.Decision == string(domain.DecisionHikiwake))
}

// preserveWithdrawalRuling is the mirror of preserveLoserScore: that one
// carries the stored bout rows onto a decision write, this one carries the
// stored RULING (withdrawalRuling) onto a score sheet's correction, so a
// correction to the bouts (or, on a single-bout match, the struck letters)
// fought before a withdrawal saves them and leaves the ruling and its
// consequences exactly as recorded. Forward writes only: a
// matchWriteRestore replays a trusted snapshot and inherits nothing.
//
// It reports whether it inherited, and the caller must then skip the
// eligibility side effect (recordIneligibilityFromDecision): the result now
// carries the withdrawal again, and re-recording it would overwrite the
// competitor's status (RecordedAt, and a kiken-injury reinstatement) for a
// ruling nobody changed. Both forward-merge owners call it BEFORE anything
// reads result.Winner for this write: applyPoolWrite before identity
// backfill, applyBracketResultIn before the downstream-correction guard.
func preserveWithdrawalRuling(stored withdrawalRuling, result *state.MatchResult, policy matchWritePolicy) bool {
	if policy != matchWriteForward || !KeepsWithdrawalRuling(stored.Status, stored.Decision, result) ||
		displacesStoredResult(result) {
		return false
	}
	result.Decision = stored.Decision
	result.DecisionBy = stored.DecisionBy
	result.DecisionReason = stored.DecisionReason
	result.Winner = stored.Winner
	result.WinnerID = stored.WinnerID
	// WinnerSide is a transient per-write hint the client never sends; clear
	// it so no handler-set hint can outvote the inherited WinnerID.
	result.WinnerSide = ""
	result.IpponsA, result.IpponsB = keptWithdrawalScoreline(stored, result)
	result.Encho = nil
	if stored.Encho != nil {
		encho := *stored.Encho
		result.Encho = &encho
	}
	return true
}

// displacesStoredResult reports whether the write's merge took the stored
// result out of the match: the stored result was a rival of this one, stamped
// after it by a device that had not seen it (rivalResult), so the first
// recorded result is the write's and there is no stored ruling left to keep.
// A correction that saw the ruling displaces nothing and keeps it (bc-tmfn).
func displacesStoredResult(result *state.MatchResult) bool {
	if result.Merge == nil {
		return false
	}
	return slices.ContainsFunc(result.Merge.Displaced, func(d state.DisplacedChange) bool {
		_, ok := d.Values[state.GroupResult]
		return ok
	})
}

// keptWithdrawalScoreline is the match-level scoreline a correction that keeps
// a withdrawal stores. The ruling's scoreline is two different things, one per
// side (recordDecisionTx): the WINNER's side is the default-win maru, which is
// the ruling itself and is kept; the WITHDRAWING side's is the letters it had
// struck (preserveLoserScore, FIK Art. 32), which are scores, so a correction
// saves what the operator entered for them (operator ruling 2026-09-24: "Save
// correction should just save what the operator enters"), passed through
// struckIppons so no maru or hantei mark the sheet echoed back can be filed
// as a strike.
//
// Two shapes keep the stored scoreline whole instead:
//   - a write with bout rows (a team encounter): its match level is not a
//     score sheet at all, the team editor sends [] there, and the ruling's
//     match-level scoreline is only the verdict's maru;
//   - a ruling with no DecisionBy (written before the side was recorded):
//     nothing says which side withdrew, so there is no loser side to take.
//
// A withdrawing side the write OMITS (nil, not []) is a writer with nothing
// to say, and keeps the stored letters: the same silence rule the wire uses
// for ippons elsewhere. An explicit [] is the operator clearing them.
func keptWithdrawalScoreline(stored withdrawalRuling, result *state.MatchResult) (ipponsA, ipponsB []string) {
	ipponsA = append([]string(nil), stored.IpponsA...)
	ipponsB = append([]string(nil), stored.IpponsB...)
	if len(result.SubResults) > 0 {
		return ipponsA, ipponsB
	}
	switch stored.DecisionBy {
	case "shiro": // Shiro (SideB) withdrew.
		if result.IpponsB != nil {
			ipponsB = struckIppons(result.IpponsB)
		}
	case "aka": // Aka (SideA) withdrew.
		if result.IpponsA != nil {
			ipponsA = struckIppons(result.IpponsA)
		}
	}
	return ipponsA, ipponsB
}

// struckIppons returns the real struck ippon letters from a slice, dropping
// empty entries, the "•" UI placeholder, the domain.DefaultWinIppon maru
// (an awarded default win, not a struck point) and the domain.HanteiMark
// (a verdict, not a strike — and one that cannot survive onto a withdrawal
// anyway; stripInvalidHantei would remove it downstream, this just keeps the
// preserved slice honest at the source). Distinct from countScoringIppons,
// which counts the maru as a scoring ippon by design.
//
// Composed from domain.IsScoringIppon (which already drops "", the
// placeholder and HanteiMark) plus the one deliberate extra exclusion, the
// maru: this keeps struckIppons in lockstep with the domain definition by
// construction, so a future non-scoring token added there (as HanteiMark
// itself once was) does not also need remembering here.
func struckIppons(ippons []string) []string {
	return domain.StruckIppons(ippons)
}

// countScoringIppons is the package-local spelling of domain.CountScoringIppons
// (real ippon marks, ignoring empties and the "•" placeholder; the default-win
// maru counts like any struck ippon). The rule itself lives in domain so the
// store's TeamResultFrom and the HTTP validator share this exact count.
func countScoringIppons(ippons []string) int {
	return domain.CountScoringIppons(ippons)
}

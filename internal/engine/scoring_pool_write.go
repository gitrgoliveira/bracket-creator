package engine

import (
	"fmt"
	"maps"

	"github.com/gitrgoliveira/bracket-creator/internal/domain"
	"github.com/gitrgoliveira/bracket-creator/internal/state"
)

// applyPoolWrite folds the stored match into an incoming result and then
// performs the whole-struct overwrite. It reports whether the write was
// ABANDONED, in which case the stored match is left untouched. That happens two
// ways, mirroring applyBracketMatchResult: the payload names a pairing that is
// not this match's (a client error the caller maps to 409), or the timestamp
// guard drops it as stale (a newer result simply stands, nobody is at fault).
// They are reported on SEPARATE returns because they are different verdicts
// about different actors, and both have to reach the operator: a drop reported
// as plain success is what bc-lww1 was.
//
// It does the `*stored = *result` assignment itself rather than leaving it to
// each caller, so that the overwrite is unreachable without the merge. This is
// ONE function because the hand-copied version drifted twice, in the direction
// that hurts: preserveDaihyosenOutcome reached three of the four writers and
// missed the live /score path, and the CorrectionReason inherit existed only in
// the Tx forward writer, so a team quick-score landing after a kachinuki reopen
// blanked the operator's audit justification through the other two.
//
// SCOPE of "one home", stated precisely because the previous wording
// overclaimed: this owns the ENGINE-side preservation. Server-owned flags a
// client must not be able to clear (ReopenPending) are re-stamped at the HTTP
// boundary instead - see applyCorrectionReason (mobileapp) - so a new field of that kind still
// needs a decision about which layer preserves it.
//
// A THIRD abandonment reason joined the two above (bc-idfx): backfillMatchIdentity
// rejects a client-supplied WinnerID that names neither side once the row's
// own ids are known. It is reported through `err`, not through mismatch,
// because it is a different verdict again -- not "wrong pairing" (mismatch)
// and not "stale" (superseded), but "this winner doesn't correspond to
// either competitor in this match" -- and the caller maps it to 400, not 409.
func applyPoolWrite(stored, result *state.MatchResult, policy matchWritePolicy, comp *state.Competition) (mismatch, superseded, inherited bool, err error) {
	return applyPoolWriteLog(stored, result, policy, comp, nil)
}

// applyPoolWriteLog is applyPoolWrite with the match's score event log, which
// a scoring snapshot is replayed onto (mergeByReplay).
func applyPoolWriteLog(stored, result *state.MatchResult, policy matchWritePolicy, comp *state.Competition, lines []state.ScoreLogLine) (mismatch, superseded, inherited bool, err error) {
	// reconcileSides BACKFILLS omitted sides as a side effect and only reports
	// the mismatch, so it must run under both policies; hoisted out of the
	// condition below because folding it into a short-circuit would let a later
	// tidy (cheap comparison first) silently drop the backfill.
	sidesDisagree := reconcileSides(result, storedSides{A: stored.SideA, B: stored.SideB, AID: stored.SideAID, BID: stored.SideBID})
	// Match identity is fixed at generation; a score must not rewrite it. The
	// restore policy replays sides captured from this same match, so a mismatch
	// there is not a client error.
	if sidesDisagree && policy == matchWriteForward {
		return true, false, false, nil
	}
	// The merge (bc-mrgc), the SAME owner and the same call the bracket branch
	// makes: each group the write changes applies only if its stamp is not
	// older than that group's stored stamp, a group it does not change keeps
	// the stored value, and a write whose every change is held leaves the
	// match untouched (its history entry is written by the caller). A pool's
	// whole-struct write has always replaced the stored bouts with a nil
	// list, so the default keeps that (nilSubsClear).
	if mergeMatchWrite(stored, result, policy, mergeCtx{comp: comp, nilSubsClear: true, lines: lines}).Superseded() {
		return false, true, false, nil
	}
	// A bout-row correction over a recorded withdrawal keeps the ruling
	// (bc-tmfn). Before the identity backfill, so the winner id it resolves
	// and validates is the inherited one.
	inherited = preserveWithdrawalRuling(rulingOfMatch(stored), result, policy)
	// Preserve generation-time participant ids + resolve winner id across the
	// overwrite: score requests carry side NAMES only. See backfillMatchIdentity.
	if berr := backfillMatchIdentity(result, stored, policy); berr != nil {
		return false, false, false, berr
	}
	// The stamps (ModifiedAt and GroupStamps) were settled by the merge: an
	// unstamped write keeps the stored ones, and a restore replays the
	// snapshot's exactly, as the bracket twin always did.
	if result.Court == "" {
		result.Court = stored.Court
	}
	if result.ScheduledAt == "" {
		result.ScheduledAt = stored.ScheduledAt
	}
	result.Round = stored.Round
	// Everything below is forward-only; matchWriteRestore inherits nothing more.
	// Nor does the score replay's derived write (bc-evts): the replay owns
	// its hantei marks and its rows, and a change made here would be a match
	// its own log no longer replays to.
	if policy == matchWriteForward && result.WriteDoor != DoorEvent {
		// Set-if-empty: see the drift note on this function.
		if result.CorrectionReason == "" {
			result.CorrectionReason = stored.CorrectionReason
		}
		// The MATCH-level verdict travels inside the ippons the writer sent
		// (or inherited through preserveLoserScore on the decision paths);
		// what needs enforcing here is only that an inherited mark cannot
		// contradict the incoming result. See stripInvalidHantei.
		stripInvalidHantei(result)
		// A pool/league team encounter can hold a daihyosen (findMatchForDaihyosen
		// accepts pool matches). Without this a verdict-silent write from a stale
		// second editor erases a recorded hantei, which in accrueTeamSubResults
		// flips the bout from a win into a draw and so moves the team's IV/IL/IT
		// tie-break figures.
		preserveDaihyosenOutcome(stored.SubResults, result)
	}
	// The unparsed sub-bout cell is FILE PROVENANCE, not client data: it exists
	// so a malformed hand edit survives until someone repairs it, and the
	// whole-struct overwrite below would drop it on any write to this match --
	// including one that says nothing about sub-bouts at all, such as a court
	// reassignment. Assigned unconditionally rather than set-if-empty, under
	// BOTH policies, so a client that echoes the derived flag back cannot
	// raise it on a match that reads perfectly well.
	//
	// A write that CARRIES sub-bouts clears both: the operator has re-entered
	// the encounter, so the retained bytes are superseded and the warning has
	// been answered. That is the repair path, and it is the only thing that
	// takes the flag down.
	if len(result.SubResults) == 0 {
		result.SubResultsRaw = stored.SubResultsRaw
		result.SubResultsUnreadable = stored.SubResultsUnreadable
	} else {
		result.SubResultsRaw = ""
		result.SubResultsUnreadable = false
	}
	prior := derivedScoreOf(stored.IpponsA, stored.IpponsB, stored.MarkIDs, stored.SubResults, stored.ScoreVersion, stored.EventSeqs)
	priorTurnedAway := stored.TurnedAway
	*stored = *result
	// The write's own merge inputs and report stay on the incoming result for
	// the caller; the stored copy must not carry them to a later writer.
	stored.ClearRequestFields()
	stored.GroupStamps = state.CloneGroupStamps(result.GroupStamps)
	// The mark ids and the event-log version are the replay's alone
	// (bc-evts): only its derived write (DoorEvent) sets them, and its
	// version only grows. Any other write is built by a client (or from
	// one), so what it carries is never stored: it keeps the stored ids of
	// each scoreline it left as it was, and the stored version
	// (derivedScore.carry). A restore puts back the snapshot's exactly.
	switch {
	case policy == matchWriteRestore:
		stored.MarkIDs = result.MarkIDs.Clone()
		stored.EventSeqs = state.CloneGroupStamps(result.EventSeqs)
		stored.TurnedAway = maps.Clone(result.TurnedAway)
	case result.WriteDoor == DoorEvent:
		stored.MarkIDs = result.MarkIDs.Clone()
		stored.ScoreVersion = max(prior.version, result.ScoreVersion)
		stored.EventSeqs = mergeEventSeqs(prior.eventSeqs, result.EventSeqs)
		stored.TurnedAway = maps.Clone(result.TurnedAway)
	default:
		stored.MarkIDs = prior.carry(stored.IpponsA, stored.IpponsB, stored.SubResults)
		stored.ScoreVersion = prior.version
		stored.EventSeqs = state.CloneGroupStamps(prior.eventSeqs)
		stored.TurnedAway = maps.Clone(priorTurnedAway)
		result.MarkIDs, result.ScoreVersion = stored.MarkIDs.Clone(), stored.ScoreVersion
		result.EventSeqs = state.CloneGroupStamps(stored.EventSeqs)
		result.TurnedAway = maps.Clone(stored.TurnedAway)
	}
	return false, false, inherited, nil
}

// applyHansokuIppons auto-awards ippons from accumulated hansoku counts per
// FIK Article 20: every 2 hansoku on one side grants 1 ippon to the opponent.
// Compares the count of 'H' entries a side already carries against what its
// opponent's hansoku count implies and awards ONLY the shortfall, via
// domain.AppendIppon (the derived H is a struck point, so it takes the next
// free slot before growing the slice, and a wire-legal row holding a "•"
// placeholder stays wire-legal after the award). A row carrying at least the
// implied count is left entirely alone, positions included — which is also
// why re-folding an already-folded row reports no change. See applyOneSide
// for why a surplus H is the operator's own strike and must never be
// removed.
//
// The fold and the post-fold guard used to be two steps - applyHansokuIppons
// returning a hansokuFold recording which slices it actually REWROTE, and a
// separate checkHansokuHanteiConflict taking that fold - with all three call
// sites required to chain them by hand. That is now inside this one function,
// so a new caller cannot fold without checking: there is no fold value left
// to forget to check.
//
// The check answers for what THIS fold rewrote, not for whatever was already
// on disk: a row whose award was already folded in on a previous write is
// not touched at all and counts as UNCHANGED. That scoping matters because
// the decision twins inherit the encounter's stored sub-bouts wholesale
// (preserveLoserScore) before the fold runs, so without it an operator's
// kiken would be rejected for an over-cap row that a much earlier write
// persisted and that the kiken payload never carried, with no endpoint able
// to repair it. Same reasoning as applyKachinukiMerge running AFTER the
// check.
//
// It rejects two things the auto-award can do to a scoreline that the wire
// validator, which only ever sees the payload as the client sent it, cannot
// have checked:
//
//   - The award UNTIES a scoreline a hantei mark is still riding on. A
//     payload can arrive tied (e.g. ipponsA:[M,Ht], ipponsB:[K],
//     hansokuB:2) and pass ScoreRequest.Validate's tied-scoreline
//     precondition, then the fold appends an "H" to ipponsA and the RESULT
//     is untied while the mark still stands. Falling through silently hands
//     that row to stripInvalidHantei, which discards the mark with no error
//     reaching the caller, so the operator's hansoku award would quietly
//     overrule a verdict the same payload also carried.
//   - The award pushes a side past the best-of-3 cap. A row with both slots
//     already holding entries plus a folded "H" exceeds it (a free "•"
//     placeholder is filled, not grown past), and nothing downstream rejects it, so
//     the write lands on disk and every subsequent echo save of that match
//     400s at mobileapp.validateIppons - wedging the editor on a row that was
//     never rejected at write time.
//
// Both checks apply at MATCH level and to every SubResults row, because the
// fold applies to both (a daihyosen representative bout carries its own
// hansoku counts and its own mark) and neither stripInvalidHantei nor
// preserveSubHantei runs on the sub level. The returned *ValidationError
// surfaces the same 400 the client would have gotten had it pre-computed the
// award itself and sent the post-award scoreline directly.
//
// A payload that ALREADY carries an over-cap or untied-marked row (pre-folded
// by the caller) is damage the wire validator can see for itself, and
// mobileapp.validateIppons rejects it at both levels before the engine is
// reached; what only the engine can see is the state AFTER the award, which
// is why this check lives here and covers exactly that.
//
// A no-op when the fold changed nothing, and when a changed row carries
// neither a mark nor an overflow: this must never change behaviour for the
// overwhelming majority of hansoku awards, which carry neither.
func applyHansokuIppons(result *state.MatchResult) error {
	if result == nil {
		return nil
	}
	applyOneSide := func(hansoku int, ippons *[]string) bool {
		// hansoku/2 derives the ippons a CUMULATIVE foul count implies — the
		// convention of clients that only count fouls upward and never strike
		// the "H" themselves (reconcileFoulsAtOpen in the JS editors is this
		// same derivation, run at editor-open over legacy stored counts).
		// Today's editors keep the OUTSTANDING count instead — the strike
		// resets the counter to 0 and puts the H in the opponent's slots
		// (applyFoulIncrement) — so for their payloads expected is 0 and
		// every H the row carries is the operator's own strike.
		expected := hansoku / 2
		existing := 0
		for _, v := range *ippons {
			if v == "H" {
				existing++
			}
		}
		// Award only the SHORTFALL, and never remove a surplus H: under the
		// outstanding convention every editor payload carries "more H than
		// the count explains", so a removal branch (tried once, as a
		// "hansoku reduction" feature) silently deleted the struck point on
		// every save — the editor's screen kept showing it while standings,
		// viewers and the export lost it. This restores the fold's original
		// contract: never duplicate existing H entries, never remove them.
		// An operator undoing a mistaken award edits the slots directly, and
		// the row they send IS the row that persists.
		if existing >= expected {
			return false
		}
		out := append([]string{}, *ippons...)
		for ; existing < expected; existing++ {
			// A derived hansoku ippon is a struck point, so it takes the
			// next free slot before growing the slice (domain.AppendIppon,
			// the same rule the editors and AppendHantei follow). Filling
			// rather than appending keeps a legal two-slot row with a "•"
			// placeholder legal after the award; a blind append pushed it
			// to three entries, past validateIppons' structural cap, so
			// the row 400'd here (or, before checkFoldedRow existed,
			// wedged on disk and 400'd every echo save).
			out = domain.AppendIppon(out, "H")
		}
		*ippons = out
		return true
	}
	// HansokuA awards the OPPONENT (side B) an ippon, and vice versa, so each
	// count's "changed" bool belongs to the slice it rewrote.
	matchB := applyOneSide(result.HansokuA, &result.IpponsB)
	matchA := applyOneSide(result.HansokuB, &result.IpponsA)
	if err := checkFoldedRow("", matchA, matchB, result.HanteiDecided(), result.IpponsA, result.IpponsB); err != nil {
		return err
	}
	for i := range result.SubResults {
		sr := &result.SubResults[i]
		subB := applyOneSide(sr.HansokuA, &sr.IpponsB)
		subA := applyOneSide(sr.HansokuB, &sr.IpponsA)
		if err := checkFoldedRow(fmt.Sprintf("subResults[%d]: ", i), subA, subB, sr.HanteiDecided(), sr.IpponsA, sr.IpponsB); err != nil {
			return err
		}
	}
	return nil
}

// checkFoldedRow applies both post-fold checks to ONE scoring row (the match
// itself, or one sub-bout), skipping whatever the fold left alone. Shared by
// the two levels so they cannot drift: the match level used to carry the
// hantei check only, which let the identical fold wedge an over-cap match-level
// slice on disk while the sub level rejected it.
//
// The hantei check runs first: a row that trips both (the illustrative case -
// a mark riding on a scoreline the fold both unties AND pushes over the cap)
// surfaces the more actionable message naming the verdict conflict, rather
// than a bare count error that leaves the operator to work out why.
func checkFoldedRow(prefix string, changedA, changedB, hanteiDecided bool, ipponsA, ipponsB []string) error {
	if (changedA || changedB) && hanteiDecided && !domain.HanteiTiedScoreline(ipponsA, ipponsB) {
		// The tie test spans BOTH sides, so a change to either side can be
		// what untied it.
		return validationErrorf("%shantei requires a tied scoreline after hansoku ippon award", prefix)
	}
	if (changedA && len(ipponsA) > domain.MaxIpponsPerSide) || (changedB && len(ipponsB) > domain.MaxIpponsPerSide) {
		return validationErrorf("%sat most %d ippons per side (best-of-3) after hansoku ippon award, got %d/%d", prefix, domain.MaxIpponsPerSide, len(ipponsA), len(ipponsB))
	}
	return nil
}

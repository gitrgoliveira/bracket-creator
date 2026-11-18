package mobileapp

import (
	"fmt"
	"log"

	"github.com/gitrgoliveira/bracket-creator/internal/engine"
	"github.com/gitrgoliveira/bracket-creator/internal/state"
)

// correctionCheck is applyCorrectionReason's verdict.
//
//   - StoredStatus is the match's status as stored, returned so the caller's
//     stale-after-complete guard doesn't have to look the match up again.
//   - Reject is non-nil when the write must be refused; the caller renders it
//     in its own error shape (a 400 on the single-score path, a
//     partial-success entry on the bulk path).
//   - ClearBracketReopenPending asks the caller to run
//     dischargeReopenPendingUnderTx AFTER a successful engine write. Only bracket
//     matches need it: the pool write is a whole-struct overwrite that carries
//     r.ReopenPending straight through.
type correctionCheck struct {
	StoredStatus state.MatchStatus
	// StoredDecision is the stored match-level decision, read in the same
	// snapshot as StoredStatus (see teamFinishRefusalUnderTx).
	StoredDecision            string
	Reject                    *ValidationError
	ClearBracketReopenPending bool
	// Corrects is true for a completed -> completed write that carries its
	// reason and so lands as a correction: not a finish older than the stored
	// result, not the resend of the one recorded, not a reason-less finish from
	// a device that never saw it (the replay keeps those in the history). The
	// finished-league check asks it (refuseFinishedLeagueCorrection).
	Corrects bool
}

// applyCorrectionReason applies the correction-reason audit policy to r over
// the match's STORED snapshot and returns the verdict with the stored status.
// The snapshot is read inside WithTransaction (the per-comp lock is held), so
// the is-completed read is race-free against a concurrent score write.
//
// The policy is shared by the single-score and bulk-score paths so the audit
// rule cannot drift between them (same reason applyKachinukiMerge is shared
// by the locked and tx scoring paths); each caller wraps the Reject verdict
// in its own error shape.
//
// completed -> completed is a CORRECTION: overwriting an already-finalized
// result requires a non-empty CorrectionReason for traceability. This applies
// to ANY decision type, including a withdrawal (kiken/fusenpai) submitted via
// /score; exempting those would let a finalized result be overwritten with no
// audit reason. Reject reports a missing reason and r is left otherwise
// untouched; on acceptance the supplied reason is kept.
//
// A match reopened without a reason (ReopenPending) is NOT asked for one
// when it is ended again: a match can be reopened without any reason, and
// nothing is gated on that (operator ruling 2026-09-25, which retired the
// mp-gmcg rule that collected the reopen's reason here). Ending it works like
// ending any match; the one difference is that a reason the client does send
// is kept, since the completion replaces a result the reopen discarded, and
// ReopenPending is discharged.
//
// Anything else (a genuine first finalization, or a running/scheduled write)
// is not a rewrite. A client-supplied reason is meaningless there and is
// dropped, but the reason STORED on the match must survive: the kachinuki
// reopen path records the operator's justification there, and blanking it
// would erase the audit trail at the very write it was recorded to justify.
// Carrying it on the non-completed writes too is what keeps it alive for POOL
// matches, whose write is a whole-struct overwrite (`*stored = *result` in
// the engine's applyPoolWrite) rather than the bracket path's set-if-non-empty.
//
// ReopenPending is SERVER-OWNED and is re-stamped from the stored value on
// every write, before any of the above. state.MatchResult binds straight from
// the request body, so a client could otherwise plant the flag on an unrelated
// match or clear it by sending `reopenPending: false`. Re-stamping is also
// what keeps the flag alive across a pool match's running writes, which would
// otherwise blank it through the same whole-struct overwrite.
//
// Returning the stored status spares the caller a second match lookup:
// StoreTx loads read through the parsed-file cache (state.LoadPoolMatchesLocked
// / loadBracketLocked), so on a hit a walk is a stat and a copy of the whole
// body (every pool match, or the whole bracket), still made while the per-comp
// WRITE lock is held, and a miss adds a full parse. The score handler
// reads the snapshot ONCE for every guard of the write (the self-run judge or
// the pick judge, then this), and the start gate after it reads the returned
// status instead.
// seenResultCheck lets applyCorrectionReason ask the match's score log whether a
// reason-less finish's basis holds the line of the result in force at its stamp (engine.ResultInForceAt).
// The score door passes a full one. Bulk-score passes a probeOnly one: it is an
// organiser's tool and is never the rival of a result, so it keeps asking every
// overwrite for its reason, but its exact-resend probe replays the match's real
// log, as the engine's own probe does (bc-evts review round 12).
type seenResultCheck struct {
	stx             state.StoreTx
	compID, matchID string
	// probeOnly holds the log for the exact-resend probe alone: the
	// reason-less rival bypass is the score door's.
	probeOnly bool
	// The match's score log, read once for every judge of the write that
	// asks it (log): the exact-resend probe and the reason check read the
	// same lines the engine's merge does.
	read  bool
	lines []state.ScoreLogLine
	err   error
}

// log reads the match's score log once. A nil check (bulk-score, or a judge
// called with none) holds no log: nil lines.
func (c *seenResultCheck) log() ([]state.ScoreLogLine, error) {
	if c == nil {
		return nil, nil
	}
	if !c.read {
		c.read = true
		c.lines, c.err = c.stx.LoadScoreLog(c.compID, c.matchID)
	}
	return c.lines, c.err
}

// probeLines is the log for the exact-resend probe: a log that cannot be read
// is probed as empty, as the engine's own read (snapshotScoreLog) does. Only a
// write stamped exactly as the stored result can be such a resend, so no
// other write pays the read (the probe declines it before reading lines).
func (c *seenResultCheck) probeLines(snap matchSnapshot, r *state.MatchResult) []state.ScoreLogLine {
	if r.ModifiedAt <= 0 || r.ModifiedAt != snap.ResultStamp {
		return nil
	}
	lines, err := c.log()
	if err != nil {
		return nil
	}
	return lines
}

// sawResult reports whether basis holds a recorded result. A log that cannot be
// read answers false: the caller judges the write as one whose basis did not
// see the result (the engine reads that log as empty too), so it reaches the
// engine and ends in the match's history. Every read error is logged here,
// naming the file and line when the error carries them.
// readable says whether the log could be read at all, from the same one read.
func (c *seenResultCheck) sawResult(stored *state.MatchResult, comp *state.Competition, knockout bool, stamp int64, basis *state.ScoreBasis) (saw, readable bool) {
	lines, err := c.log()
	if err != nil {
		log.Printf("mobileapp: ERROR: match %s/%s: score log not read: %v", c.compID, c.matchID, err)
		return false, false
	}
	r := engine.ResultInForceAt(stored, lines, comp, knockout, stamp, basis)
	return r.InForce && r.Saw, true
}

func applyCorrectionReason(snap matchSnapshot, r *state.MatchResult, comp *state.Competition, score *seenResultCheck) correctionCheck {
	r.ReopenPending = snap.ReopenPending
	if r.Status == state.MatchStatusCompleted && snap.Status == state.MatchStatusCompleted {
		// A finish made BEFORE the stored result (a queued Finish replayed
		// after the match was finished on another device) is no correction:
		// the replay orders it by its stamp (its verdict is the older of two
		// results and is held, the groups it names land or are held by their
		// own stamps) and keeps what loses in the match's history. Demanding
		// a reason here refused it before it got there, and it was lost
		// (bc-mrgc review).
		if r.ModifiedAt > 0 && r.ModifiedAt < snap.ResultStamp {
			return correctionCheck{StoredStatus: snap.Status, StoredDecision: snap.Decision}
		}
		// An equal stamp with nothing it changes differing from what is
		// stored is the finish already recorded, sent again because its
		// answer was lost: the engine answers it as recorded and writes
		// nothing (engine.ExactScoreReplay, bc-xrpl). A group changed since
		// (a later point from another board) is turned away by the closed-match
		// rule and lands nothing, so it is not the resend's to differ in. A same-stamp
		// write that differs where it would land is a correction, and still
		// needs its reason. comp tells the probe a kachinuki or engi write,
		// which the write path shapes before the merge.
		if engine.ExactScoreReplay(snap.Stored, r, comp, !snap.InBracket, score.probeLines(snap, r)) {
			return correctionCheck{StoredStatus: snap.Status, StoredDecision: snap.Decision}
		}
		if r.CorrectionReason == "" {
			// A finish from a device that never saw the recorded result is no
			// correction either, and asking it for a reason would be refusing
			// it with a 400 a queued replay could never answer (operator
			// decision 2026-10-16, I1): the first recorded result stands, and
			// the replay turns the finish away as match_closed and keeps it in
			// the history. Only a write that names a clearWithdrawal, or whose
			// basis holds the result, claims to have seen it. One stamped no
			// later than the result (an equal stamp may be the same board
			// changing its own finish) or with no stamp of its own (the
			// server's, which the replay never turns away) still needs its
			// reason.
			if score != nil && !score.probeOnly && r.ModifiedAt > snap.ResultStamp && !r.ClearsWithdrawal {
				// A log that cannot be read is judged as one whose basis did
				// not see the result: the engine reads it as empty too, and the
				// write then ends in the match's history, which is a separate
				// file, instead of being dropped before it gets there.
				if saw, readable := score.sawResult(snap.Stored, comp, snap.InBracket, r.ModifiedAt, r.Basis); !saw {
					if !readable {
						// The engine reads this log as empty, so a basis naming lines of
						// it means nothing: the write is judged, and logged, as one that
						// sent none.
						r.Basis = nil
					}
					return correctionCheck{StoredStatus: snap.Status, StoredDecision: snap.Decision}
				}
			}
			return correctionCheck{StoredStatus: snap.Status, StoredDecision: snap.Decision, Reject: missingCorrectionReasonError()}
		}
		return correctionCheck{StoredStatus: snap.Status, StoredDecision: snap.Decision, Corrects: true}
	}
	if r.Status == state.MatchStatusCompleted && snap.ReopenPending {
		// Ended again after a reason-less reopen: never refused, the
		// client's reason (if any) kept, the flag discharged.
		r.ReopenPending = false
		return correctionCheck{
			StoredStatus:              snap.Status,
			StoredDecision:            snap.Decision,
			ClearBracketReopenPending: snap.InBracket,
		}
	}
	// Non-completing write: pin the reason to the STORED one. This is not just a
	// carry-forward — it also refuses a client-supplied reason on a running
	// write, so the audit note can only ever change through the completing
	// correction branch above. The engine side (applyPoolWrite in
	// engine: `if result.CorrectionReason == "" { result.CorrectionReason
	// = stored.CorrectionReason }`)
	// independently carries the stored reason across its whole-struct overwrite
	// for callers that DON'T pass through here; the two are deliberately kept
	// separate (this one is stricter) — do not collapse one into the other
	// without re-checking that a running write still cannot rewrite the note
	// (mp-gmcg review F5).
	r.CorrectionReason = snap.CorrectionReason
	return correctionCheck{StoredStatus: snap.Status, StoredDecision: snap.Decision}
}

// applyCorrectionReasonUnderTx reads the match's snapshot itself, applies
// applyCorrectionReason to it and hands the snapshot back: the entry point of
// bulk-score, one read per entry, whose later guard (the landed picks'
// judge) compares the match against it. The score handler reads the snapshot
// once for all its guards and asks applyCorrectionReason directly.
func applyCorrectionReasonUnderTx(stx state.StoreTx, compID, matchID string, r *state.MatchResult, comp *state.Competition) (correctionCheck, matchSnapshot, error) {
	// matchSnapshotOrErr fails CLOSED on a load error: the correction-reason
	// gate must not pass on an assumed-running status, silently overwriting a
	// finalized result without the mandatory reason.
	snap, _, err := matchSnapshotOrErr(stx, compID, matchID, "correction-reason")
	if err != nil {
		return correctionCheck{}, matchSnapshot{}, err
	}
	// A fighter name the entry types on a bout row is stored title-cased, as
	// /score stores it, judged against the stored rows so an echo of a bout
	// already fought is left as recorded, before the reason and exact-replay
	// checks below compare the rows with the stored ones.
	titleCaseTypedFighters(r.SubResults, snap.Stored)
	probe := &seenResultCheck{stx: stx, compID: compID, matchID: matchID, probeOnly: true}
	return applyCorrectionReason(snap, r, comp, probe), snap, nil
}

// missingCorrectionReasonError is the refusal for overwriting a completed
// result without a correction reason.
func missingCorrectionReasonError() *ValidationError {
	return &ValidationError{Field: "correctionReason", Message: "correcting a completed match result requires a non-empty correctionReason"}
}

// dischargeReopenPendingUnderTx clears a match's stored ReopenPending flag
// after a completion, and, when reason is non-empty, records it in
// CorrectionReason. Runs inside the
// caller's WithTransaction, AFTER the engine write has succeeded: the
// finalizing handlers commit the transaction even when the engine rejects the
// write (engErr is surfaced afterwards), so discharging beforehand would
// persist a discharge for a write that never landed.
//
// BOTH match homes, because both finalizing endpoints reach both:
//
//   - PUT /score passes reason "" and is gated on
//     correctionCheck.ClearBracketReopenPending, so only a BRACKET match ever
//     arrives from that path. Its reason is already stored (the engine write
//     copies CorrectionReason set-if-non-empty) and a POOL match is already
//     handled by the whole-struct overwrite (`*stored = *result`, applyPoolWrite) carrying the
//     cleared r.ReopenPending.
//   - POST /decision passes the operator's decisionReason and needs BOTH arms.
//     RecordDecisionTx builds its own result struct, so the pool overwrite
//     blanks ReopenPending with no audit record (a SILENT discharge) while the
//     bracket write, being field-by-field, never copies the flag and leaves it
//     stuck on a completed match. Same rule, two opposite failures; one
//     explicit pass fixes both.
//
// CorrectionReason is set-if-empty so the "" caller cannot blank a stored
// reason, and so a reason already recorded by the engine write wins over a
// re-statement here.
//
// recordBracketMatchResult* deliberately does NOT copy ReopenPending from the
// payload — that field is client-supplied and a score write must not be able
// to move a server-owned audit flag — which is why the flag needs this
// explicit pass rather than riding along with the result.
func dischargeReopenPendingUnderTx(stx state.StoreTx, compID, matchID, reason string, inBracket bool) error {
	// inBracket lets a caller that already knows the match's home (from the
	// snapshot it read) skip the pool probe: the score paths reach here only
	// behind ClearBracketReopenPending (== snap.InBracket && snap.ReopenPending),
	// so the pool arm would be a guaranteed miss — a full uncached
	// pool-matches.csv parse for nothing (mp-gmcg review). The /decision caller
	// passes snap.InBracket, so a genuine pool match still takes the pool arm.
	if !inBracket {
		// Pool first, mirroring lookupMatchSnapshot's order. UpdatePoolMatchByID
		// reports found=false for a bracket match, which is the fall-through.
		found, err := stx.UpdatePoolMatchByID(compID, matchID, func(r *state.MatchResult) error {
			r.ReopenPending = false
			if reason != "" && r.CorrectionReason == "" {
				r.CorrectionReason = reason
			}
			return nil
		})
		if err != nil {
			return fmt.Errorf("discharge reopen flag: pool match: %w", err)
		}
		if found {
			return nil
		}
	}
	// Bracket arm: UpdateBracketMatchByID owns the rounds → bronze-sibling walk
	// (mp-gmcg review), so the easy-to-forget bronze branch is no longer
	// hand-written here. A miss (bracket match not found either) saves nothing.
	_, err := stx.UpdateBracketMatchByID(compID, matchID, func(bm *state.BracketMatch) {
		bm.ReopenPending = false
		if reason != "" && bm.CorrectionReason == "" {
			bm.CorrectionReason = reason
		}
	})
	if err != nil {
		return fmt.Errorf("discharge reopen flag: bracket match: %w", err)
	}
	return nil
}

package mobileapp

import (
	"fmt"
	"log"
	"net/http"

	"github.com/gin-gonic/gin"
	"github.com/gitrgoliveira/bracket-creator/internal/domain"
	"github.com/gitrgoliveira/bracket-creator/internal/engine"
	"github.com/gitrgoliveira/bracket-creator/internal/state"
)

// isMatchFinalized reports whether the given stored status represents a
// concluded match. Any completed match is finalized, anonymous callers must
// not overwrite it regardless of whether a winner was explicitly recorded.
func isMatchFinalized(status state.MatchStatus) bool {
	return status == state.MatchStatusCompleted
}

// matchStores is the minimal read surface a match lookup needs: the two
// files a match can live in. BOTH state.StoreTx (in-transaction, per-comp
// lock already held) and CompetitionStore (plain loads, each taking the lock
// itself) satisfy it, so the transactional and the non-transactional lookups
// share ONE traversal instead of a hand-rolled walk per projection.
type matchStores interface {
	LoadPoolMatches(compID string) ([]state.MatchResult, error)
	LoadBracket(compID string) (*state.Bracket, error)
}

// matchSnapshot is the stored state of a match as the match-write guards
// (applyCorrectionReason, holdSelfReportedWriteUnderTx and
// registerScoreHandler) read it: Status drives the correction / stale-write / finalized gates,
// CorrectionReason carries the kachinuki reopen justification forward (see
// applyCorrectionReason), and ReopenPending says a reason-less reopen
// has not been ended again.
//
// It carries only the fields the guards actually read. The post-advance
// kachinuki bout log rides back to the editor on MaybeAdvanceKachinuki's
// postLog return (result.SubResults), not through this snapshot, and no field
// here deep-clones the stored sub-bouts (mp-gmcg review: that was a write-only
// value, and an expensive one, under the per-comp write lock). Stored is the
// one reader of the stored match's rows, and it SHARES the loaded copy rather
// than cloning it: the member judge (landedMembersRefusal) only reads it, as the
// match stood before the write, and each load hands out a copy of its own, so
// the engine's write never reaches it.
//
// InBracket is the match's HOME rather than its content: the pool write is a
// whole-struct overwrite that carries ReopenPending itself, while the bracket
// write copies field by field and deliberately never reads the flag off the
// client-supplied payload, so only a bracket match needs the separate
// dischargeReopenPendingUnderTx pass. lookupMatchSnapshot already knows which
// store it found the match in, so reporting it costs nothing and saves the
// clear path a second traversal.
type matchSnapshot struct {
	Status           state.MatchStatus
	CorrectionReason string
	ReopenPending    bool
	InBracket        bool
	// Decision is the stored match-level decision, which the team finish
	// gate's exemption reads under the same lock as the write it gates
	// (engine.KeepsWithdrawalRuling, via teamFinishRefusalUnderTx).
	Decision string
	// RepBout is a copy of the stored representative-bout row (position -1),
	// nil when the match has none: the self-run judge compares a
	// participant's write against it, and writes back a deep copy of it once
	// the organiser has decided it (holdSelfReportedWriteUnderTx); the picks the
	// write lands are judged against it, for what they introduce
	// (landedMembersRefusal). One row, which a writer deep-clones (Stored
	// carries the whole bout log).
	RepBout *state.SubMatchResult
	// Pairing is the stored match's attribution (its sides, their ids and its
	// winner), which the self-run judge reads a participant's winner against
	// rather than the sides the write sends.
	Pairing domain.WinnerAttribution
	// ResultStamp is the stored stamp of the match's result group (when its
	// verdict was last changed), which the correction-reason check reads to
	// tell a stale replay of a finish from a correction (bc-mrgc review).
	ResultStamp int64
	// Stored is the stored match itself, as the pool-shaped record the merge
	// reads (a pool row as loaded; a bracket match through
	// engine.BracketMatchAsResult). It is what the judges ask about, and it
	// shares the loaded copy's rows, so a reader clones before it changes
	// anything. Read before the write it is the baseline the member judge
	// compares the landed match against, row by position; read again after the
	// write (the same lookupMatchSnapshot, through the same transaction) it is
	// what landed (landedMembersRefusal). nil only on a zero snapshot (no match
	// found).
	Stored *state.MatchResult
}

// repBoutOf returns a copy of the first representative-bout row in subs, nil
// when there is none.
func repBoutOf(subs []state.SubMatchResult) *state.SubMatchResult {
	i := state.DaihyosenSubIndex(subs)
	if i < 0 {
		return nil
	}
	row := subs[i]
	return &row
}

// lookupMatchSnapshot is mobileapp's READ-side traversal of the three homes a
// match ID can have — the pool-matches CSV, the bracket rounds, and the
// bracket's ThirdPlaceMatch — searched in that order. Lookup is by ID equality,
// not prefix: match-ID shapes vary across formats and fixtures. It reads a
// snapshot for a handler holding only an id; the fail-closed audit wrapper is
// matchSnapshotOrErr (mp-gmcg review).
//
// The bronze (3rd-place) match is a SIBLING of Rounds, not an element of it,
// so a rounds-only loop never reaches it. Forgetting that branch is the bug
// this single traversal exists to make impossible: MaybeAdvanceKachinuki
// appends bouts to the bronze like any other bracket match, and a per-caller
// copy of the walk that omitted it left a kachinuki bronze "Record bout"
// echoing the pre-advance log, with the appended pairing never reaching the
// open editor.
//
// found=false means the ID is in neither store; callers treat an unknown
// match as "nothing recorded yet" (the engine rejects it via errMatchNotFound
// on the actual score write, so there is nothing to fail here). err is the
// FIRST load failure seen, and the walk continues past it so a best-effort
// caller can still find a bracket match when the pool CSV is unreadable.
// Callers do not open-code this: a fail-closed audit gate goes through
// matchSnapshotOrErr (surfaces err); MatchStatusByID is the no-copy status-only
// reader for the pre-tx correction check (review E5).
func lookupMatchSnapshot(s matchStores, compID, matchID string) (matchSnapshot, bool, error) {
	var loadErr error
	poolMatches, err := s.LoadPoolMatches(compID)
	if err != nil {
		loadErr = fmt.Errorf("load pool matches: %w", err)
	}
	for i := range poolMatches {
		if poolMatches[i].ID == matchID {
			return matchSnapshot{
				Status:           poolMatches[i].Status,
				CorrectionReason: poolMatches[i].CorrectionReason,
				ReopenPending:    poolMatches[i].ReopenPending,
				Decision:         poolMatches[i].Decision,
				RepBout:          repBoutOf(poolMatches[i].SubResults),
				Pairing:          poolMatches[i].Attribution(),
				ResultStamp:      poolMatches[i].GroupStamp(state.GroupResult),
				Stored:           &poolMatches[i],
			}, true, loadErr
		}
	}
	bracket, err := s.LoadBracket(compID)
	if err != nil && loadErr == nil {
		loadErr = fmt.Errorf("load bracket: %w", err)
	}
	if bracket != nil {
		for _, round := range bracket.Rounds {
			for i := range round {
				if round[i].ID == matchID {
					return bracketMatchSnapshot(&round[i]), true, loadErr
				}
			}
		}
		if bracket.ThirdPlaceMatch != nil && bracket.ThirdPlaceMatch.ID == matchID {
			return bracketMatchSnapshot(bracket.ThirdPlaceMatch), true, loadErr
		}
	}
	return matchSnapshot{}, false, loadErr
}

// bracketMatchSnapshot projects a BracketMatch into the same shape as a pool
// MatchResult so lookupMatchSnapshot returns one type across both stores.
func bracketMatchSnapshot(bm *state.BracketMatch) matchSnapshot {
	return matchSnapshot{
		Status:           bm.Status,
		CorrectionReason: bm.CorrectionReason,
		ReopenPending:    bm.ReopenPending,
		InBracket:        true,
		Decision:         bm.Decision,
		RepBout:          repBoutOf(bm.SubResults),
		Pairing:          bm.Attribution(),
		ResultStamp:      bm.GroupStamp(state.GroupResult),
		Stored:           engine.BracketMatchAsResult(bm),
	}
}

// matchSnapshotOrErr is the fail-CLOSED reader over lookupMatchSnapshot: it
// SURFACES a load error (wrapped as "<guardLabel> guard: …") instead of
// swallowing it, so an in-transaction audit gate aborts rather than acting on an
// empty snapshot. A fail-closed gate must reach for THIS — reading a match's
// ReopenPending/status under a dropped load error is the exact hole mp-gmcg
// closed. The caller keeps its own found/err handling (the return shapes
// legitimately differ: a sentinel, a bare error, a (correctionCheck{}, err));
// this owns only the shared load-and-wrap.
func matchSnapshotOrErr(s matchStores, compID, matchID, guardLabel string) (matchSnapshot, bool, error) {
	snap, found, err := lookupMatchSnapshot(s, compID, matchID)
	if err != nil {
		return matchSnapshot{}, false, fmt.Errorf("%s guard: %w", guardLabel, err)
	}
	return snap, found, nil
}

// respondSuperseded writes the shared body for a write every change of which the
// merge held (engine.ErrMatchSuperseded, bc-lww1; bc-mrgc): a NEWER change to
// each group it changes is already stored, so nothing of it was applied to the
// match. It is not lost: the held values are in the match's history, the
// write's one footprint, and heldGroups names them.
//
// 200 with applied:false, never a 4xx/5xx. A superseded write is not a fault and
// can never win a retry, while the SPA's offline write queue retries 5xx forever
// (the mp-q8c6 poisoned-queue pattern) — so an error status here would wedge the
// queue behind a write that is by definition unwinnable. The shape mirrors the
// override endpoint, which already answers this exact condition with
// {"applied": false} and whose client already keys on that field.
//
// ONE place for the same reason respondCourtBusy is one place: the condition is
// mapped on five handlers (score, quick-score, /decision and both daihyosen
// paths), and a body hand-copied per handler is how the client's single branch
// quietly stops matching one of them. /score, /decision and both daihyosen
// paths reach it: each carries the client's stamp (mp-jnvl added the decision
// one, bc-dhas the daihyosen ones), so the timestamp guard compares rather than
// taking its unstamped bypass. Quick-score still builds its MatchResult without
// a client stamp; the merge stamps it with the server's time, never older than
// the stored result (engine writeStamp), so it always applies. It is mapped
// defensively because its default arm is a 500 the SPA's write queue would
// retry forever.
//
// bulk-score is deliberately NOT in that list: it reports per-entry failures in
// its own errors[] array inside an overall 200, so a superseded entry is already
// excluded from `successful` and cannot poison a queue.
//
// heldReason is the merge's code for why (engine.HeldReasonOf): "" for a newer
// stored change, "match_closed" when a finish or a decision stamped before the
// write had already closed the match (the closed-match rule). It is still not
// lost and still never wins a retry, so it keeps the same applied:false
// superseded shape, with heldReason and its own message (messageForHeldReason)
// telling the operator the match is finished and to tap Correct.
//
// stored is the match as it is stored once the write was held, read inside the
// write's own transaction (storedMatchForAnswer) and projected as the door's
// applied answer is. It is answered as `match`, so the device that is told
// "Not applied" shows the stored match in the same render as the note and the
// two never disagree. nil when it could not be read: the answer then carries
// no `match`, and the device keeps the copy it had until the next push.
func respondSuperseded(c *gin.Context, stored *state.MatchResult, heldGroups []string, heldReason string, from ...error) {
	// LOGGED because this is the one successful-looking response whose work
	// an operator does not see on the match. Since bc-mrgc the held values are
	// in the match's history, but the line still names the request, which is
	// what someone asking "my score vanished" correlates against. Same reason
	// stripInvalidHantei logs its drop.
	log.Printf("mobileapp: %s %s: write superseded, a newer change is already recorded; kept in the match history (held %v)",
		c.Request.Method, c.Request.URL.Path, heldGroups)
	body := gin.H{
		"applied": false,
		"reason":  "superseded",
		"message": SupersededMessage,
	}
	if len(heldGroups) > 0 {
		body["heldGroups"] = heldGroups
	}
	if heldReason != "" {
		body["heldReason"] = heldReason
		body["message"] = messageForHeldReason(heldReason)
	}
	if stored != nil {
		body["match"] = stored
	}
	// The match's turned-away taps as the log stands after the write (a held
	// rival result is still logged), so the writing device is told of its own
	// on this answer and not only on the push (stage 6 review 6).
	for _, err := range from {
		if away, version := engine.TurnedAwayOf(err); len(away) > 0 {
			body["turnedAway"] = away
			if version > 0 {
				body["scoreVersion"] = version
			}
		}
	}
	c.JSON(http.StatusOK, body)
}

// storedMatchForAnswer reads the match as it is stored, for the answer to a
// write the merge held (respondSuperseded's `match`, bulk-score's per-entry
// `match`). It is read through the write's own transaction after the engine
// answered engine.ErrMatchSuperseded, so it is exactly what the write lost to,
// under the same lock, with nothing landing between the hold and the read.
//
// It is projected as the doors' applied answers are: Rev and RevSession
// zeroed (write-ordering metadata the answer never echoes), a bracket match's
// court and schedule filled in (engine.BracketMatchAsResult omits them, and a
// landed write's answer carries them), and the withdrawal stamp the push
// carries (stampWithdrawnStatus). public is true for a caller answered as
// the broadcast is (an anonymous self-run participant on /score, every caller
// of the representative bout's add and remove): the audit notes are then
// cleared too (matchForBroadcast). nil when the match cannot be read; the
// answer then simply carries no match, as it did before.
func storedMatchForAnswer(stx state.StoreTx, compID, matchID string, public bool) *state.MatchResult {
	snap, found, err := lookupMatchSnapshot(stx, compID, matchID)
	if err != nil {
		log.Printf("mobileapp: %s/%s: read the stored match for a superseded answer: %v", compID, matchID, err)
	}
	if !found || snap.Stored == nil {
		return nil
	}
	m := *snap.Stored
	if snap.InBracket {
		if bracket, berr := stx.LoadBracket(compID); berr == nil {
			m.Court, m.ScheduledAt = bracketSlot(bracket, matchID)
		} else {
			log.Printf("mobileapp: %s/%s: read the bracket slot for a superseded answer: %v", compID, matchID, berr)
		}
	}
	m.Rev, m.RevSession = 0, ""
	stampWithdrawnStatus(stx, compID, &m)
	if public {
		m = matchForBroadcast(m)
	}
	return &m
}

// bracketSlot is the court and the schedule the bracket match matchID holds,
// a round match or the bronze, empty when the bracket holds no such match.
func bracketSlot(bracket *state.Bracket, matchID string) (court, scheduledAt string) {
	if bracket == nil {
		return "", ""
	}
	for _, round := range bracket.Rounds {
		for i := range round {
			if round[i].ID == matchID {
				return round[i].Court, round[i].ScheduledAt
			}
		}
	}
	if bm := bracket.ThirdPlaceMatch; bm != nil && bm.ID == matchID {
		return bm.Court, bm.ScheduledAt
	}
	return "", ""
}

// SupersededMessage is the one sentence a write every change of which was
// held is answered with: nothing of it was applied, and it is kept in the
// match's history (bc-mrgc), not lost.
const SupersededMessage = "Not applied: a newer change to this match is already recorded. This change was kept in the match's history."

// MatchClosedMessage is the sentence a write is answered with when its
// scoring was turned away because a finish or a decision stamped before it had
// already closed the match (the closed-match rule, operator ruling
// 2026-10-15). The score-events door answers a tap with the same words; the
// engine owns them.
const MatchClosedMessage = engine.MatchClosedMessage

// messageForHeldReason is the one place a heldReason code is turned into the
// operator sentence that goes with it: respondSuperseded and the bulk-score
// path (which cannot share its JSON body) both read it, so the two can never
// say something different about the same code.
func messageForHeldReason(heldReason string) string {
	switch heldReason {
	case state.HeldReasonMatchClosed:
		return MatchClosedMessage
	case state.HeldReasonDownstreamPlayed:
		return ResultLockedPlayedMessage
	case state.HeldReasonDownstreamRunning:
		return ResultLockedRunningMessage
	}
	return SupersededMessage
}

// ResultLockedPlayedMessage and ResultLockedRunningMessage are the sentences a
// finish or decision is answered with when the next knockout match was already
// played or started, so the recorded result is locked (operator decision K1,
// 2026-10-16). The write is kept in the match's history.
const (
	ResultLockedPlayedMessage  = "Not applied: the next round was already fought, so the recorded result stands. This result was kept in the match's history. To change the result, tap Correct."
	ResultLockedRunningMessage = "Not applied: the next round has already started, so the recorded result stands. This result was kept in the match's history. To change the result, tap Correct."
)

// writesOverFinished reports whether a write of this status, over a finished
// match, is a correction to it rather than a start: a completed correction, or
// a running or scheduled write that the closed-match rule turns away (it is
// answered match_closed, not refused for the court). Neither takes the court.
func writesOverFinished(status state.MatchStatus) bool {
	return status == state.MatchStatusCompleted || status == state.MatchStatusRunning || status == state.MatchStatusScheduled
}

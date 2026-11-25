package mobileapp

import (
	"errors"
	"fmt"
	"io"
	"log"
	"net/http"
	"strings"

	"github.com/gin-gonic/gin"
	"github.com/gitrgoliveira/bracket-creator/internal/domain"
	"github.com/gitrgoliveira/bracket-creator/internal/engine"
	"github.com/gitrgoliveira/bracket-creator/internal/state"
)

// bulkScorePushEachMax is the largest bulk-score batch that pushes one
// match_updated per landed entry; a larger one pushes one naming only the
// competition.
const bulkScorePushEachMax = 50

// RegisterMatchHandlers wires up the score / quick-score / bulk-score /
// court / time endpoints under the admin group.
//
// The score endpoint (registerScoreHandler) is the
// Slice 0 / NFR-002 demonstration of the interface-based dependency
// injection pattern (T017): it consumes `ScoringEngine` and `Broadcaster`
// rather than the concrete `*engine.Engine` and `*Hub`, plus the
// `ScoreRequest.Validate()` pattern (T015 / NFR-004) for request-shape
// validation.
//
// The endpoints registered below still hold concrete pointers (the
// function signature accepts the concrete `*engine.Engine` for methods not
// yet on the interface). Later slices migrate those one at a time; the
// concrete `*engine.Engine` remains a drop-in implementation of
// `ScoringEngine`, so the score endpoint (and `tryAutoCompletePools`, in
// pool_auto_complete.go) already accept the interface today.
func RegisterMatchHandlers(r *gin.RouterGroup, eng *engine.Engine, store CompetitionStore, tx CompetitionTransactor, hub *Hub, verifier PasswordVerifier, tl TournamentLoader) {
	// The court move and the time change are ordered by arrival: a late
	// duplicate of one is answered as recorded (writeOnce, Y8).
	once := newWriteOnce()
	r.POST("/competitions/:id/matches/bulk-score", func(c *gin.Context) {
		id, ok := requireValidCompID(c)
		if !ok {
			return
		}
		// bulkScoreEntry wraps state.MatchResult with a transient, per-entry
		// field that binds from the JSON body but is never part of
		// state.MatchResult itself -- mirrors scoreRequestBody's wrapping of
		// ScoreRequest below for the single-match /score endpoint.
		type bulkScoreEntry struct {
			state.MatchResult
			// ForceDownstreamReopen bypasses bc-kcdg's downstream-knockout-
			// correction guard for THIS entry, same field name and contract as
			// every other knockout-correction write (see
			// scoreRequestBody.ForceDownstreamReopen).
			ForceDownstreamReopen bool `json:"forceDownstreamReopen"`
			// ConfirmFinished lets THIS entry's correction of a finished
			// league's result land (see scoreRequestBody.ConfirmFinished);
			// without it the entry fails with reason "league_finished".
			ConfirmFinished bool `json:"confirmFinished"`
			// ConfirmLots is the organiser's "Replace the lots": a write that gives a drawn
			// tie-breaker bout a winner, or reopens it, over lots they entered lands (409
			// lots_replaced without it). Request-only, never stored.
			ConfirmLots bool `json:"confirmLots"`
			// ChangedGroups is this entry's `changed`, the groups it changes
			// (bc-mrgc), with the same meaning and default as /score's.
			ChangedGroups []string `json:"changed"`
			// writeTag is the entry's optional writeId and basis (bc-evts).
			writeTag
		}
		var results []bulkScoreEntry
		if err := c.ShouldBindJSON(&results); err != nil {
			c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
			return
		}

		// Defense-in-depth: bulk-score writes straight to disk via
		// RecordMatchResult, bypassing ScoreRequest.Validate's length
		// caps. Reuse the same caps here so a 1MB sideA/winner can't
		// land. Per-result rejection keeps the partial-success semantics
		// (good entries still succeed, bad ones surface in `errors`).
		type scoreError struct {
			MatchID string `json:"matchId"`
			Error   string `json:"error"`
			// Reason is a machine-readable discriminator, set only where the
			// failure is a known verdict rather than an arbitrary error. Values
			// today: "superseded" and "clock_skew" from the timestamp guard (a
			// newer result for this match is already stored, or this entry's
			// modifiedAt is implausibly far in the future so it can be neither
			// trusted nor zeroed, see modifiedAtRefuseSkewMs); "corrupt_overrides"
			// when a corrupt overrides.json blocks the standings read the pool
			// requalification check needs; "downstream_knockout_played"
			// (bc-kcdg) when correcting this entry would repaint a downstream
			// bracket match that has recorded its own result, or, for a pool
			// entry in a mixed competition, would move a qualifier the knockout
			// already played (resolved by retrying the SAME entry with
			// forceDownstreamReopen:true once the operator confirms); and
			// "downstream_knockout_running" when that pool entry would move a
			// qualifier out of a knockout match being fought now, or a knockout
			// entry would change a side of one (terminal: finish or requeue
			// that match, then retry); and "team_member_not_in_team" when a
			// representative pick the entry lands is not on its side's team
			// (the entry stores nothing; /score answers it with a 400); and
			// "team_members_unreadable" when the competition's team members
			// cannot be read to judge such a pick (the entry stores nothing;
			// /score answers it with a 409); and "league_finished" when the
			// entry corrects a finished league's result without
			// confirmFinished (the entry stores nothing; /score answers it with
			// a 409).
			//
			// It matters because the single-match endpoints answer those
			// conditions with a distinct body ({"applied": false, "reason":
			// ...}), while this one folds every per-entry failure
			// into a free-text string. An importer could therefore not tell a
			// benign supersede from a genuine rejection, and re-running the
			// import to "fix" it would re-stamp every entry and overwrite the
			// newer results it was just told about. Omitted for every other
			// failure, so its presence always means the same thing.
			Reason string `json:"reason,omitempty"`
			// HeldGroups names, on a "superseded" entry, every group it
			// changes: each was kept in the match's history (bc-mrgc).
			HeldGroups []string `json:"heldGroups,omitempty"`
			// HeldReason is "match_closed" when a finish or a decision
			// stamped before the entry had already closed the match, or "" for
			// a newer stored change.
			HeldReason string `json:"heldReason,omitempty"`
		}
		var errs []scoreError
		// partlyHeld reports each entry that was applied in part: its matchId
		// and what withHeldGroups adds (heldGroups, always present on an
		// entry, empty when its only change was a move to the history).
		var partlyHeld []gin.H
		// warnings reports each applied entry whose write a carried lineup
		// could not follow: its matchId and the operator's sentence, as /score
		// answers `warning` (stage 6 review 10, m3).
		var warnings []gin.H
		// Only successfully-recorded results go into the SSE broadcast so
		// clients never patch with values the engine rejected.
		var successful []state.MatchResult
		// Collect eligibility changes so we can broadcast
		// EventCompetitorStatusUpdated for every kiken/fusenpai result in
		// the batch, mirrors the single-score handler (T085/T092).
		var eligibilityUpdates []*domain.CompetitorStatus

		// Kachinuki relaxes the numbered-bout encho gate. Every result in the
		// batch belongs to the same competition, so the gate is resolved ONCE
		// (allowNumberedEnchoFromStore also keeps the fail-closed behaviour and
		// the load-error log): scan the batch first so an import that carries no
		// bout-level encho at all never pays the store read.
		batchHasNumberedEncho := false
		for i := range results {
			if anyNumberedBoutHasEncho(results[i].SubResults) {
				batchHasNumberedEncho = true
				break
			}
		}
		allowNumberedEncho := allowNumberedEnchoFromStore(store, id, batchHasNumberedEncho)

		// bc-cse finding 13: the team finish gate's competition record is
		// the SAME for every entry in this batch (one compID), so it is
		// loaded ONCE here rather than once per entry the way a call
		// through refuseUnfinishedTeamFinish would (mirrors the
		// allowNumberedEncho resolve-once just above). A load failure is
		// deterministic (same store, same id) so it would have failed
		// identically for every entry under the old per-entry call too;
		// compErr is propagated to each entry that reaches the gate below,
		// exactly as an independent per-entry failure would have.
		comp, compErr := store.LoadCompetition(id)

		for i := range results {
			// A stamp implausibly far in the future is refused per-entry, before
			// this entry reaches the engine (see modifiedAtRefuseSkewMs). The
			// batch shape means it lands in errs[] rather than as a whole-body
			// response, with the same machine-readable Reason the single-match
			// endpoints put in their body, and the entry is excluded from
			// `successful` so the discarded payload is never broadcast.
			if err := changedGroupsError(results[i].ChangedGroups); err != nil {
				errs = append(errs, scoreError{MatchID: results[i].ID, Error: err.Error()})
				continue
			}
			results[i].Changed = results[i].ChangedGroups
			results[i].WriteDoor = engine.DoorBulkScore
			// Mark ids and the event-log version are the replay's, never a body's.
			results[i].ClearDerivedScoreFields()
			if err := results[i].validateTag(); err != nil {
				errs = append(errs, scoreError{MatchID: results[i].ID, Error: err.Error()})
				continue
			}
			results[i].tagOnto(&results[i].MatchResult)
			if _, aheadMs, refuse := clientClockSkew(results[i].ModifiedAt); refuse {
				logClockSkewRefusal(c, aheadMs, results[i].ID)
				errs = append(errs, scoreError{
					MatchID: results[i].ID,
					Error:   "not saved: this device's clock appears to be ahead of the server; resync the clock and re-submit",
					Reason:  "clock_skew",
				})
				continue
			}
			// Reject a hostile/buggy negative client timestamp so it cannot
			// freeze the match against later legitimate writes (mp-y3nk).
			results[i].ModifiedAt = clampClientModifiedAt(results[i].ModifiedAt)

			// bc-qual/bc-dmsr: same identity backfill as the single-score
			// path, ahead of the legacy-hantei fold and the hantei
			// mark-placement check inside validateBulkScoreLengths. See
			// backfillMatchIdentityForHantei.
			backfillMatchIdentityForHantei(store, id, results[i].ID, &results[i].MatchResult)
			if err := validateBulkScoreLengths(&results[i].MatchResult, allowNumberedEncho); err != nil {
				errs = append(errs, scoreError{MatchID: results[i].ID, Error: err.Error()})
				continue
			}

			// mp-62vr: rep-player names belong only on a pool daihyosen/tiebreaker
			// rep bout. Strip them from regular matches so a crafted bulk payload
			// can't persist stale rep metadata (mirrors the single-score path).
			if !state.IsPoolSupplementaryMatchID(results[i].ID) {
				results[i].RepPlayerA = ""
				results[i].RepPlayerB = ""
			}

			// bc-tmfn: the team finish gate, per entry, exactly as on PUT
			// /score (see refuseUnfinishedTeamFinish for its scope): computed
			// here, applied under the entry's transaction below. A refused
			// entry is a validation failure like validateBulkScoreLengths':
			// it lands in errs naming the bouts, nothing is written for it,
			// and the rest of the batch is unaffected (this endpoint is
			// always 200, partial success).
			//
			// bc-cse finding 13: routed through refuseUnfinishedTeamFinishForComp
			// against the ONE comp loaded above the loop, rather than a call
			// through refuseUnfinishedTeamFinish that would reload it for
			// every entry. compErr mirrors what an independent per-entry
			// LoadCompetition failure would have done (same store, same id,
			// so a deterministic failure recurs identically for every entry).
			if compErr != nil {
				errs = append(errs, scoreError{MatchID: results[i].ID, Error: compErr.Error()})
				continue
			}
			// The kachinuki win decision belongs to a kachinuki competition
			// only, judged against the same one competition record.
			if refusal := refuseKachinukiDecisionForComp(comp, &results[i].MatchResult); refusal != nil {
				errs = append(errs, scoreError{MatchID: results[i].ID, Error: refusal.Error()})
				continue
			}
			finishRefusal := refuseUnfinishedTeamFinishForComp(comp, results[i].ID, &results[i].MatchResult)

			// mp-ic5b: the correction-reason gate and the write run under the
			// same per-comp lock so the status read is race-free against a
			// concurrent PUT /score. Per-result transactions preserve the
			// existing {succeeded, errors[]} partial-success response shape.
			// Court exclusivity (mp-95mg) is intentionally NOT enforced here:
			// bulk-score is an admin batch-import/correction tool, not a live
			// match-start path. It also bypasses StartMatchTx for the same
			// reason, admin corrections are meant to override normal flow.
			results[i].CorrectionReason = strings.TrimSpace(results[i].CorrectionReason)
			var capturedStatus *domain.CompetitorStatus
			var reopenedThisItem []engine.ReopenedMatch
			var supersededThisItem error
			txErr := tx.WithTransaction(id, func(stx state.StoreTx) error {
				// Correction-reason audit policy (require a reason for a
				// completed -> completed overwrite, otherwise carry the STORED
				// reason forward; ending a match reopened without a reason is
				// never refused). The rule itself lives in
				// applyCorrectionReason, shared with the single-score path (which
				// calls it over its own snapshot) so the two cannot drift; only
				// the error SHAPE differs here (partial-success entries carry a
				// plain message).
				check, snap, snapErr := applyCorrectionReasonUnderTx(stx, id, results[i].ID, &results[i].MatchResult, comp)
				if snapErr != nil {
					return snapErr
				}
				if check.Reject != nil {
					return errors.New(check.Reject.Message)
				}
				if refusal := teamFinishRefusalUnderTx(finishRefusal, check, &results[i].MatchResult); refusal != nil {
					return errors.New(refusal.Error())
				}
				if err := refuseFinishedLeagueCorrection(stx, tl, id, results[i].ID, check.Corrects, results[i].ConfirmFinished); err != nil {
					return err
				}
				// The entry's representative picks are judged on what the write
				// landed, as /score's are (landedMembersRefusal), decided here,
				// before the engine rewrites the entry. Its numbered rows are
				// not: this is the organiser's tooling.
				judgePicks := introducesJudgedMember(results[i].SubResults, snap, false)
				status, err := eng.RecordMatchResultWithIneligibilityTx(stx, id, results[i].ID, &results[i].MatchResult, engine.ForceOptions{
					Force:       results[i].ForceDownstreamReopen,
					ConfirmLots: results[i].ConfirmLots,
					Reopened:    &reopenedThisItem,
				})
				if errors.Is(err, engine.ErrMatchSuperseded) {
					// Committed, not aborted: the entry's history record is
					// its one footprint (bc-mrgc).
					supersededThisItem = err
					return nil
				}
				if err != nil {
					return err
				}
				if judgePicks {
					// A refusal aborts this entry's transaction: it stores
					// nothing, history line included, and the entries beside
					// it are unaffected.
					if err := landedMembersRefusal(stx, id, results[i].ID, snap, false); err != nil {
						return err
					}
				}
				capturedStatus = status
				if check.ClearBracketReopenPending {
					// ClearBracketReopenPending is snap.InBracket && snap.ReopenPending,
					// so the match is known to be in the bracket: skip the pool probe.
					return dischargeReopenPendingUnderTx(stx, id, results[i].ID, "", true)
				}
				return nil
			})
			if txErr == nil && supersededThisItem != nil {
				txErr = supersededThisItem
			}
			if err := txErr; err != nil {
				bulkErr := scoreError{MatchID: results[i].ID, Error: err.Error()}
				var downstreamPlayedErr *engine.DownstreamKnockoutPlayedError
				var downstreamRunningErr *engine.DownstreamKnockoutRunningError
				var alreadyIneligErr *engine.AlreadyIneligibleError
				var landedRefusal *landedWriteRefusal
				var leagueRefusal *leagueFinishedRefusal
				var lotsReplaced *engine.LotsReplacedError
				switch {
				case errors.As(err, &leagueRefusal):
					// A correction of a finished league's result, refused
					// until the entry carries confirmFinished: the entry
					// stores nothing, the rest of the batch is unaffected.
					bulkErr.Reason = leagueFinishedCode
					bulkErr.Error = LeagueFinishedMessage
				case errors.As(err, &lotsReplaced):
					// A result that would replace the lots the organiser entered
					// for a drawn tie-breaker, refused until the entry carries
					// confirmLots: the entry stores nothing.
					bulkErr.Reason = lotsReplacedCode
					bulkErr.Error = lotsReplaced.Msg
				case errors.Is(err, engine.ErrTiebreakNeedsWinner):
					// A league tie-breaker finished with no winner: the code rides in
					// Reason (as the 400 body's code on the other doors), the sentence in Error.
					bulkErr.Reason = engine.ErrTiebreakNeedsWinner.Error()
				case errors.As(err, &landedRefusal):
					// A pick the write landed that the side's team does not
					// hold: the refusal's code rides in Reason and its
					// sentence, the page's own, in Error.
					bulkErr.Reason = landedRefusal.refusal.code
					bulkErr.Error = landedRefusal.refusal.message
				case errors.Is(err, engine.ErrMatchSuperseded):
					bulkErr.Reason = "superseded"
					bulkErr.Error = SupersededMessage
					bulkErr.HeldGroups = engine.HeldGroupsOf(err)
					bulkErr.HeldReason = engine.HeldReasonOf(err)
					if bulkErr.HeldReason != "" {
						bulkErr.Error = messageForHeldReason(bulkErr.HeldReason)
					}
				case errors.As(err, &alreadyIneligErr):
					// bc-rawm/bc-cse: this batch shape has no dedicated 409 to
					// answer with, so the operator sentence rides in Error
					// itself (same class of fix as the single-match 409 body):
					// without this, Error was err.Error()'s raw
					// `competitor "<uuid>" already ineligible (match ...)`,
					// naming an internal id and no remedy. Only
					// *AlreadyIneligibleError is reachable here, never
					// *IneligibleCompetitorError: bulk-score never calls
					// StartMatchTx/startSidesGateTx (the only
					// producers of that type besides the simultaneity gate,
					// which this write path also never reaches), only the K2/K3
					// concurrent-withdrawal checks inside
					// RecordMatchResultWithIneligibilityTx, which return this
					// type alone.
					bulkErr.Error = reasonHumanForBarredCompetitor(store, id, results[i].ID, alreadyIneligErr.PlayerID, alreadyIneligErr.Reason, alreadyIneligErr.MatchID, alreadyIneligErr.Decision)
				case errors.Is(err, state.ErrCorruptOverrides):
					// The pool requalification check reaches computeStandingsFrom,
					// which loads overrides.json -- reachable per-entry here the
					// same way it is on the single-match write endpoints. This
					// endpoint never fails the whole request (always 200, partial
					// success), so there is no terminal-422 status to give this
					// entry the way respondIfCorruptOverrides does elsewhere;
					// the Reason still lets an importer tell this apart from an
					// arbitrary rejection and route the operator to
					// DELETE .../overrides rather than retrying the same bytes.
					bulkErr.Reason = "corrupt_overrides"
				case errors.As(err, &downstreamPlayedErr):
					// bc-kcdg: correcting THIS entry would repaint a downstream
					// match that already carries its own result. Unlike the
					// single-match endpoints, a batch entry has no dedicated 409
					// to answer with (this endpoint is always 200, partial
					// success), so the discriminator rides in Reason instead: the
					// operator can retry the same entry with
					// forceDownstreamReopen:true once they've confirmed the
					// override, exactly as /score's 409 body prompts them to.
					bulkErr.Reason = "downstream_knockout_played"
				case errors.As(err, &downstreamRunningErr):
					// Re-scoring THIS entry would move a qualifier out of, or
					// change a side of, a knockout match being fought now. Same
					// batch-Reason treatment as downstream_knockout_played
					// above, but NOT resolvable with forceDownstreamReopen:
					// the operator finishes or requeues that match first.
					bulkErr.Reason = "downstream_knockout_running"
				}
				errs = append(errs, bulkErr)
				continue
			}
			successful = append(successful, results[i].MatchResult)
			if rep := results[i].Merge; len(rep.HeldGroups())+len(rep.DisplacedGroups()) > 0 {
				partlyHeld = append(partlyHeld, withHeldGroups(gin.H{"matchId": results[i].ID, "heldGroups": []string{}}, rep))
			}
			if results[i].LineupWarning != "" {
				warnings = append(warnings, gin.H{"matchId": results[i].ID, "warning": results[i].LineupWarning})
			}
			if capturedStatus != nil {
				eligibilityUpdates = append(eligibilityUpdates, capturedStatus)
			}
			// bc-kcdg: each reopened match is distinct from the entry just
			// corrected; broadcast it too so a client watching only that
			// court/match learns its verdict was cleared (mirrors /score).
			broadcastReopenedDownstream(hub, store, id, reopenedThisItem)
		}

		if len(successful) > 0 {
			// Each push is built from the match as stored when it is sent
			// (freshMatchPush, which stamps the withdrawn status itself). A
			// large batch sends one push naming only the competition instead,
			// which every client answers with one refetch: a push per entry
			// would queue hundreds of frames on a slow subscriber.
			if len(successful) > bulkScorePushEachMax {
				hub.Broadcast(EventMatchUpdated, gin.H{"competitionId": id})
			} else {
				for i := range successful {
					broadcastMatchNow(hub, store, id, successful[i].ID, gin.H{
						"competitionId": id,
						"matchId":       successful[i].ID,
						"result":        matchPtrForBroadcast(&successful[i]),
					})
				}
			}
			tryAutoCompletePoolsAfterWrite(c, eng, hub, id, successful...)
		}
		for _, status := range eligibilityUpdates {
			hub.Broadcast(EventCompetitorStatusUpdated, gin.H{
				"competitionId": id,
				"status":        status,
			})
		}
		body := gin.H{"succeeded": len(successful), "errors": errs}
		if len(partlyHeld) > 0 {
			body["heldGroups"] = partlyHeld
		}
		if len(warnings) > 0 {
			body["warnings"] = warnings
		}
		c.JSON(http.StatusOK, body)
	})

	r.PUT("/competitions/:id/matches/:mid/quick-score", func(c *gin.Context) {
		id, ok := requireValidCompID(c)
		if !ok {
			return
		}
		mid := c.Param("mid")
		var req struct {
			SideA     string `json:"sideA"`
			SideB     string `json:"sideB"`
			TeamAWins int    `json:"teamAWins"`
			TeamBWins int    `json:"teamBWins"`
			Draws     int    `json:"draws"`
			// ForceDownstreamReopen bypasses bc-kcdg's downstream-knockout-
			// correction guard, same field name and contract as the score-write
			// endpoint's (see scoreRequestBody.ForceDownstreamReopen).
			ForceDownstreamReopen bool `json:"forceDownstreamReopen"`
			// ConfirmFinished lets a correction of a finished league's result
			// land, same field name and contract as the score-write
			// endpoint's (see scoreRequestBody.ConfirmFinished).
			ConfirmFinished bool `json:"confirmFinished"`
			// ConfirmLots is the organiser's "Replace the lots": a write that gives a drawn
			// tie-breaker bout a winner, or reopens it, over lots they entered lands (409
			// lots_replaced without it). Request-only, never stored.
			ConfirmLots bool `json:"confirmLots"`
			// writeTag is the optional writeId and basis (bc-evts).
			writeTag
		}
		if err := c.ShouldBindJSON(&req); err != nil {
			c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
			return
		}
		if req.SideA == "" || req.SideB == "" {
			c.JSON(http.StatusBadRequest, gin.H{"error": "sideA and sideB are required"})
			return
		}
		if err := req.validateTag(); err != nil {
			c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
			return
		}
		if err := validateMaxLen("sideA", req.SideA, MaxLenMatchSide); err != nil {
			c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
			return
		}
		if err := validateMaxLen("sideB", req.SideB, MaxLenMatchSide); err != nil {
			c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
			return
		}
		const maxBouts = 100
		if req.TeamAWins < 0 || req.TeamBWins < 0 || req.Draws < 0 {
			c.JSON(http.StatusBadRequest, gin.H{"error": "win/draw counts must be non-negative"})
			return
		}
		if req.TeamAWins > maxBouts || req.TeamBWins > maxBouts || req.Draws > maxBouts {
			c.JSON(http.StatusBadRequest, gin.H{"error": "individual bout count exceeds maximum"})
			return
		}
		total := req.TeamAWins + req.TeamBWins + req.Draws
		if total > maxBouts {
			c.JSON(http.StatusBadRequest, gin.H{"error": "total bout count exceeds maximum"})
			return
		}

		// Engi competitions use flag-based scoring, not ippon tallies.
		// Quick-score builds an ippon-style result, which bypasses the
		// engi dispatch and would corrupt standings for flag-scored bouts.
		comp, err := store.LoadCompetition(id)
		if err != nil {
			internalError(c, err)
			return
		}
		if comp != nil && comp.Engi {
			c.JSON(http.StatusBadRequest, gin.H{"error": "quick-score is not supported for engi competitions"})
			return
		}
		// Kachinuki matches are scored one bout at a time via the score
		// endpoint (server-appended winner-stays bout log). Quick-score
		// synthesises a positional log and writes it wholesale through
		// RecordMatchResultWithIneligibility, which merges a kachinuki bout
		// log BY POSITION (applyKachinukiMerge) rather than replacing it, so
		// a synthesised wholesale write from this endpoint would still
		// corrupt a live winner-stays sequence built up bout-by-bout
		// elsewhere. Same incompatibility class as engi: reject.
		// comp.IsKachinuki(), not a bare TeamMatchType test: the sequence this
		// guard protects only exists when the engine actually runs kachinuki
		// advancement, which requires TeamSize >= 2 (MaybeAdvanceKachinuki
		// returns early below that), matching the engine's dispatch gate.
		if comp.IsKachinuki() {
			c.JSON(http.StatusBadRequest, gin.H{"error": "quick-score is not supported for kachinuki competitions; score bouts individually"})
			return
		}

		// Determine team winner per kendo rules: most individual wins wins.
		// winnerSide records the WINNING SIDE (not just the name) so the
		// engine can stamp WinnerID even when both sides share a name,
		// the name alone can't tell two same-name participants apart.
		winner := ""
		winnerSide := ""
		switch {
		case req.TeamAWins > req.TeamBWins:
			winner = req.SideA
			winnerSide = "A"
		case req.TeamBWins > req.TeamAWins:
			winner = req.SideB
			winnerSide = "B"
		}

		// Synthesise SubResults so standings IV/IL/IT counts are correct.
		// Sub-bout SideA/SideB are left empty, individual bout sides are
		// unknown in quick-score mode (no lineup). Winner attribution in
		// computeStandings uses `sub.Winner == m.SideA` (the match-level
		// name); the `sub.Winner == sub.SideA` fallback is guarded against
		// the "" == "" false-positive.
		subResults := make([]state.SubMatchResult, 0, total)
		pos := 1
		for range req.TeamAWins {
			subResults = append(subResults, state.SubMatchResult{Position: pos, Winner: req.SideA})
			pos++
		}
		for range req.TeamBWins {
			subResults = append(subResults, state.SubMatchResult{Position: pos, Winner: req.SideB})
			pos++
		}
		// A draw row is a Tie: it carries the hikiwake decision, so it has a
		// result (state.SubMatchResult.HasResult). Without one it read as an
		// unfought bout, which the standings do not count as IT and the team
		// finish gate refuses on a later Save correction.
		for range req.Draws {
			subResults = append(subResults, state.SubMatchResult{Position: pos, Decision: state.DecisionDraw})
			pos++
		}

		result := state.MatchResult{
			ID:         mid,
			SideA:      req.SideA,
			SideB:      req.SideB,
			Winner:     winner,
			WinnerSide: winnerSide,
			Status:     state.MatchStatusCompleted,
			SubResults: subResults,
			WriteDoor:  engine.DoorQuickScore,
		}
		req.tagOnto(&result)
		// The team finish gate (every bout of a non-kachinuki team match has a
		// result or a decision) sees the rows this endpoint is about to store,
		// as it does for /score and bulk-score, with the same exemption for a
		// correction that keeps a recorded withdrawal (teamFinishRefusalUnderTx,
		// read under the lock the write holds).
		finishRefusal := refuseUnfinishedTeamFinishForComp(comp, mid, &result)
		// bc-kcdg: reopenedDownstream collects the IDs of any downstream
		// bracket match reopened by a forced correction, populated only when
		// req.ForceDownstreamReopen actually unblocked one. Switched from the
		// plain RecordMatchResult (which hard-refuses the downstream-knockout-
		// correction guard with no override, see writeMatchResult's doc
		// comment) to RecordMatchResultWithIneligibilityTx, the same primitive
		// /score and bulk-score already use, so this endpoint can honour the
		// force flag at all.
		var (
			reopenedDownstream []engine.ReopenedMatch
			engStatus          *domain.CompetitorStatus
			refusal            *ValidationError
		)
		txErr := tx.WithTransaction(id, func(stx state.StoreTx) error {
			snap, _, snapErr := matchSnapshotOrErr(stx, id, mid, "quick-score")
			if snapErr != nil {
				return snapErr
			}
			if finishRefusal != nil {
				check := correctionCheck{StoredStatus: snap.Status, StoredDecision: snap.Decision}
				if refusal = teamFinishRefusalUnderTx(finishRefusal, check, &result); refusal != nil {
					return nil
				}
			}
			// Quick-score is always a completed write, so over a completed
			// match it is a correction: on a finished league it is warned
			// about first. The resend of a write the match's score log already
			// holds (its writeId) lands nothing and is answered as recorded, so
			// there is nothing to warn about.
			corrects := snap.Status == state.MatchStatusCompleted && !eng.WriteIDLogged(stx, id, mid, req.WriteID)
			if lerr := refuseFinishedLeagueCorrection(stx, tl, id, mid, corrects, req.ConfirmFinished); lerr != nil {
				var league *leagueFinishedRefusal
				if errors.As(lerr, &league) {
					err = league
					return nil
				}
				return lerr
			}
			// Quick-score replaces the whole encounter it builds: every group,
			// and every bout row the stored match holds that it does not
			// rebuild (bc-mrgc).
			changed, cerr := eng.WholeMatchChangedGroups(stx, id, mid, &result)
			if cerr != nil {
				err = cerr
				return nil
			}
			result.Changed = changed
			engStatus, err = eng.RecordMatchResultWithIneligibilityTx(stx, id, mid, &result, engine.ForceOptions{
				Force:       req.ForceDownstreamReopen,
				ConfirmLots: req.ConfirmLots,
				Reopened:    &reopenedDownstream,
			})
			return nil
		})
		if txErr != nil {
			internalError(c, txErr)
			return
		}
		if refusal != nil {
			c.JSON(http.StatusBadRequest, gin.H{"error": refusal.Error()})
			return
		}
		if err != nil {
			if errors.Is(err, engine.ErrMatchSuperseded) {
				// bc-lww1. Unreachable from today's SPA (this endpoint's payload
				// carries no modifiedAt, and an unstamped write never loses the
				// timestamp guard), but mapped anyway: the alternative default is
				// a 500, which the offline write queue treats as transient and
				// retries forever against a write that can never win.
				respondSuperseded(c, result.Merge.HeldGroups(), result.Merge.HeldReason(), err)
				return
			}
			if respondIfLeagueFinished(c, err) || respondIfLotsReplaced(c, err) {
				return
			}
			if errors.Is(err, engine.ErrMatchSideMismatch) {
				respondSideMismatch(c)
				return
			}
			// bc-kcdg: correcting this match would change an already-propagated
			// winner while a downstream match carries a result of its own.
			// Fixed wire contract, shared with every other knockout-correction
			// write (see respondIfDownstreamKnockoutPlayed's doc comment).
			if respondIfDownstreamKnockoutPlayed(c, err) {
				return
			}
			// Re-scoring this match would move a qualifier out of, or change
			// a side of, a knockout match being fought now: terminal, not confirmable (see
			// respondIfDownstreamKnockoutRunning's doc comment). A generic 500
			// here would be retried forever by the offline write queue.
			if respondIfDownstreamKnockoutRunning(c, err) {
				return
			}
			// A tied quick-score on a bracket team match produces a
			// Completed write with no winner, which validateBracketCompletion
			// rejects as *engine.ValidationError; map it to 400 so the caller
			// sees "resolve via daihyosen first" instead of a generic 500.
			if respondIfValidationError(c, err) {
				return
			}
			internalError(c, err)
			return
		}

		broadcastMatchNow(hub, store, id, mid, gin.H{"competitionId": id, "matchId": mid})
		// bc-kcdg: each reopened match is distinct from the one just corrected;
		// broadcast it too so a client watching only that court/match learns
		// its verdict was cleared (mirrors /score).
		broadcastReopenedDownstream(hub, store, id, reopenedDownstream)
		if engStatus != nil {
			hub.Broadcast(EventCompetitorStatusUpdated, gin.H{
				"competitionId": id,
				"status":        engStatus,
			})
		}
		tryAutoCompletePoolsAfterWrite(c, eng, hub, id, result)
		c.JSON(http.StatusOK, scoreResponseWithReopened(&result, reopenedDownstream))
	})

	// Score endpoint, Slice 0 demonstration of the interface-DI +
	// Validate() pattern (T015 / T017 / NFR-002 / NFR-004). Calls go
	// through ScoringEngine and Broadcaster (the consumer-boundary
	// interfaces from deps.go) rather than the concrete types, and the
	// request body is validated via ScoreRequest.Validate() before any
	// engine call. The closure captures `*engine.Engine` / `*Hub` and
	// adapts them to the interfaces at the call boundary, same wire
	// behaviour as before. T156 added the CompetitionTransactor `tx`
	// parameter so the match-write + ineligibility-write + lineup-freeze
	// commit under one per-comp lock acquire.
	registerScoreHandler(r, eng, store, tx, hub, verifier, tl)

	r.PUT("/competitions/:id/matches/:mid/court", func(c *gin.Context) {
		id, ok := requireValidCompID(c)
		if !ok {
			return
		}
		mid := c.Param("mid")

		var req struct {
			Court string `json:"court"`
			// StopsRunning: the operator saw the match running and confirmed
			// stopping it there and putting it first on the new court.
			StopsRunning bool `json:"stopsRunning"`
			// The client's id for the move (Y8): one already applied is
			// answered as recorded, never applied again.
			WriteID string `json:"writeId,omitempty"`
		}
		if err := c.ShouldBindJSON(&req); err != nil {
			c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
			return
		}
		// Cap defensively, the tournament-level validateCourtLabels
		// enforces single-char labels but per-match court strings have
		// historically accepted longer values in engine tests (e.g.
		// "Court Z"). 32 is generous enough not to break any real
		// caller while rejecting abusive payloads.
		if err := validateMaxLen("court", req.Court, MaxLenMatchCourt); err != nil {
			c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
			return
		}
		if err := validWriteID(req.WriteID); err != nil {
			c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
			return
		}

		applied := false
		ran := once.do("match\x1f"+id+"\x1f"+mid, req.WriteID, func() bool {
			if err := eng.UpdateMatchCourt(id, mid, req.Court, engine.CourtMove{
				StopsRunning: req.StopsRunning,
				WriteID:      req.WriteID,
			}); err != nil {
				if respondIfCourtMoveRefused(c, err) {
					return false
				}
				// A match the competition does not hold is a 404, never a 500:
				// the outbox retries a 5xx forever and holds the match's later
				// writes behind it (bc-evts stage 3 review 2, R3).
				respondEngineError(c, err)
				return false
			}
			applied = true
			return true
		})
		if !ran {
			// A late duplicate of a move already applied: recorded, nothing
			// moves and nothing is announced.
			c.Status(http.StatusOK)
			return
		}
		if !applied {
			return
		}

		payload := gin.H{
			"competitionId": id,
			"matchId":       mid,
			"court":         req.Court,
		}
		if stored, found, err := readStoredMatch(store, id, mid); err != nil {
			log.Printf("mobileapp: court move %s/%s: read stored match: %v", id, mid, err)
		} else if found && stored != nil {
			// Name the court the store holds: a resent writeId already
			// in the match's score log leaves the store unchanged, and a
			// concurrent move can also make the stored court differ from req.Court.
			payload["court"] = stored.Court
			payload["result"] = matchPtrForBroadcast(stored)
		}
		broadcastMatchNow(hub, store, id, mid, payload)
		// A pool bout moved onto a court may move that court's knockout
		// later (bc-kosc), so the schedule is announced as the time edit's is.
		hub.Broadcast(EventScheduleUpdated, nil)

		c.Status(http.StatusOK)
	})

	// POST /competitions/:id/matches/:mid/revert-to-queue
	// Reverts a running match back to the scheduled (queued) state, keeping its
	// score: starting it again carries on from it (bc-sbq). Idempotent
	// for already-scheduled matches. Completed matches return 409 (use the score
	// editor to correct a recorded result); an unknown match id returns 404.
	//
	// Intentionally NOT in isSelfRunMainGatedConfigRoute: revert-to-queue is
	// operational play (same as start-match), not organiser configuration, so it
	// stays accessible to court operators in self-run mode.
	r.POST("/competitions/:id/matches/:mid/revert-to-queue", func(c *gin.Context) {
		id, ok := requireValidCompID(c)
		if !ok {
			return
		}
		mid := c.Param("mid")
		if err := validateMaxLen("matchId", mid, MaxLenMatchID); err != nil {
			c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
			return
		}

		// The optional {writeId, basis} (bc-evts F1): a resend of a send back
		// the match's score event log holds is answered as recorded.
		tag, ok := bindOptionalWriteTag(c)
		if !ok {
			return
		}
		if err := eng.RevertMatchToQueue(id, mid, tag.tagInto(engine.ForceOptions{})); err != nil {
			if errors.Is(err, engine.ErrMatchAlreadyCompleted) {
				c.JSON(http.StatusConflict, gin.H{"error": "match_completed", "message": err.Error()})
				return
			}
			var notFoundErr *engine.NotFoundError
			if errors.As(err, &notFoundErr) {
				c.JSON(http.StatusNotFound, gin.H{"error": err.Error()})
				return
			}
			internalError(c, err)
			return
		}

		broadcastMatchNow(hub, store, id, mid, gin.H{
			"competitionId": id,
			"matchId":       mid,
		})

		c.Status(http.StatusOK)
	})

	// GET /competitions/:id/matches/:mid/start-check (bc-stpf): the court
	// console's check before it sends the running bout back for another
	// start. Not in isSelfRunMainGatedConfigRoute, like revert-to-queue
	// above; see registerStartCheckHandler.
	registerStartCheckHandler(r, eng, store)

	// POST /competitions/:id/matches/:mid/reopen
	// Sanctioned "Reopen match" for a COMPLETED kachinuki team match
	// (mp-gmcg, spec 006 decision 4), and since bc-tmfn (widened to a
	// match-level fusensho by bc-cse) for any completed match decided by a
	// withdrawal or default win (domain.IsDefaultWinDecisionStr: kiken,
	// kiken-injury, fusenpai, or fusensho), team or individual: the way to
	// clear one recorded by mistake (engine.ReopenMatch).
	// Status back to running, match-level winner/decision cleared, fought
	// bouts kept, so the operator scores what is left and finishes again. A
	// dedicated endpoint rather than a score-write flag so the score path's
	// stale-write guard (a plain running write against a completed match
	// silently no-ops) stays fully intact.
	//
	// Body: {"reason": "<why>", "forceDownstreamReopen": bool} — both
	// OPTIONAL. forceDownstreamReopen is the operator's confirmation after a
	// 409 downstream_knockout_played (the same wire contract as a knockout
	// correction's, respondIfDownstreamKnockoutPlayed): the knockout match
	// this one fed already has a result of its own, and confirming reopens it
	// too, its winner cleared and its points kept. The response then names every match that was
	// reopened that way ({"reopenedMatches": [{id, number, label}]}).
	//
	// The reason is OPTIONAL, and an absent body is equally
	// valid, so reopening is ONE TAP. Requiring the justification here was
	// too much friction for the case it is most needed in: an operator who
	// ended a match by mistake, at a shiaijo, mid-session, had to compose a
	// reason before they could get back in. Ending the match again asks for
	// no reason either (operator ruling 2026-09-25: a match can be reopened
	// without any reason, and nothing is gated on that). A reason-less reopen
	// sets state.MatchResult.ReopenPending, which only lets a reason sent with
	// the next completion be kept (see applyCorrectionReason).
	//
	// A completed match that is neither kachinuki, decided by a withdrawal,
	// nor a pool tie-break or daihyosen bout finished as a draw (no winner)
	// gets 400 (its sanctioned edit of a finished result remains the
	// correction path), as does an over-long reason; a non-completed match, a
	// match whose result feeds a downstream match in progress (or resolved by
	// a bye), a downstream match with its own result and no confirmation, or
	// a BUSY COURT gets 409.
	r.POST("/competitions/:id/matches/:mid/reopen", func(c *gin.Context) {
		id, ok := requireValidCompID(c)
		if !ok {
			return
		}
		mid := c.Param("mid")
		if err := validateMaxLen("matchId", mid, MaxLenMatchID); err != nil {
			c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
			return
		}
		var body struct {
			Reason                string `json:"reason"`
			ForceDownstreamReopen bool   `json:"forceDownstreamReopen"`
			// ForceCourt is the organiser's "Reopen anyway" (bc-adna): a
			// reopen into running on a court that already runs a bout skips
			// both court gates. The route is main-gated in self-run, so no
			// anonymous caller reaches it.
			ForceCourt bool `json:"forceCourt"`
			// ConfirmFinished is the organiser's "Change it anyway" after a 409
			// league_finished (request-only, never stored): the reopen puts a
			// finished league back into match play.
			ConfirmFinished bool `json:"confirmFinished"`
			// ConfirmLots is the organiser's "Replace the lots": a write that gives a drawn
			// tie-breaker bout a winner, or reopens it, over lots they entered lands (409
			// lots_replaced without it). Request-only, never stored.
			ConfirmLots bool `json:"confirmLots"`
			// The optional writeId and basis (bc-evts F1).
			writeTag
		}
		// io.EOF is an EMPTY body, which is the one-tap case: the client sends
		// no JSON at all. Only a malformed body is a 400 — an operator tapping
		// Reopen must never be blocked on request shape.
		if err := c.ShouldBindJSON(&body); err != nil && !errors.Is(err, io.EOF) {
			c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
			return
		}
		if err := body.validateTag(); err != nil {
			c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
			return
		}
		reason := strings.TrimSpace(body.Reason)
		// Only a SUPPLIED reason is length-checked; empty is the sanctioned
		// one-tap shape and is handled by the ReopenPending flag instead.
		if reason != "" {
			if err := validateMaxLen("reason", reason, MaxLenCorrectionReason); err != nil {
				c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
				return
			}
		}

		// Self-run mode: reopening a finalized result is an organiser action
		// (anonymous participants are blocked from overwriting finalized
		// results on the score path via holdSelfReportedWriteUnderTx; an ungated
		// reopen would be a trivial bypass). The gate lives in the CENTRAL
		// allowlist, isSelfRunMainGatedConfigRoute (middleware.go) - NOT hand-rolled here, so it shares
		// AuthMiddleware's verification mechanics including the F4
		// empty-stored-password fail-closed branch. Pinned by
		// TestSelfRun_ReopenRequiresMainPassword.

		// A match of a finished league is warned about first, as a
		// correction of its result is.
		if lerr := refuseFinishedLeagueReopen(tx, tl, id, mid, body.ConfirmFinished); lerr != nil {
			if !respondIfLeagueFinished(c, lerr) {
				internalError(c, lerr)
			}
			return
		}

		// ReopenMatch holds the court-exclusivity lock itself now
		// (mp-gmcg review A2), so its cross-competition pre-check and in-tx
		// same-comp check are atomic by construction — the handler no longer
		// has to remember to wrap the call.
		var reopenedDownstream []engine.ReopenedMatch
		restored, err := eng.ReopenMatch(id, mid, reason, body.tagInto(engine.ForceOptions{
			Force:       body.ForceDownstreamReopen,
			ConfirmLots: body.ConfirmLots,
			Reopened:    &reopenedDownstream,
			ForceCourt:  body.ForceCourt,
		}))
		if err != nil {
			var notFoundErr *engine.NotFoundError
			var validationErr *engine.ValidationError
			var courtBusyErr *engine.CourtBusyError
			switch {
			case errors.Is(err, engine.ErrReopenNotCompleted):
				// bc-rawm/bc-cse: operator sentence naming the TARGET match
				// by its label, not the raw sentinel text.
				c.JSON(http.StatusConflict, gin.H{"error": fmt.Sprintf("%s is not completed yet, so there is nothing to reopen.", engine.SentenceCase(matchLabelOrID(store, id, mid)))})
			case respondIfDownstreamKnockoutRunning(c, err):
				// bc-cse: someone is fighting the downstream match right
				// now -- 409 downstream_knockout_running, the SAME shape
				// the pool-requalification refusal already uses.
			case respondIfDownstreamKnockoutPlayed(c, err):
				// A downstream knockout match has its own result: the
				// operator is told and may retry with forceDownstreamReopen.
			case respondIfLotsReplaced(c, err):
				// The bout is a drawn tie-breaker the organiser's lots
				// settled: warned about, and reopened on confirmLots.
			case errors.Is(err, engine.ErrReopenDownstreamFought):
				// Defensive backstop only (retractPropagatedWinner's
				// re-check at the mutation point); the two typed errors
				// above already raise for every case reachable through this
				// door in practice. Kept mapped to a 409, not internalError,
				// so a genuine internal inconsistency still fails the write
				// rather than surfacing as a 500 the offline queue retries
				// forever (mp-q8c6).
				c.JSON(http.StatusConflict, gin.H{"error": err.Error()})
			case errors.As(err, &courtBusyErr):
				// Same 409 shape as the score path so clients have one
				// court_busy branch to handle (mp-gmcg).
				respondCourtBusy(c, store, courtBusyErr, courtBusyRemedyReopen)
			case errors.As(err, &notFoundErr):
				c.JSON(http.StatusNotFound, gin.H{"error": err.Error()})
			case errors.As(err, &validationErr):
				c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
			default:
				internalError(c, err)
			}
			return
		}

		broadcastMatchNow(hub, store, id, mid, gin.H{
			"competitionId": id,
			"matchId":       mid,
		})
		// A confirmed reopen also reopened the downstream match(es) it named;
		// each is a distinct match whose verdict was cleared (mirrors /score).
		broadcastReopenedDownstream(hub, store, id, reopenedDownstream)
		// Reopening a match a withdrawal ended removes the withdrawal, and
		// the engine restored the competitor it barred; broadcast that
		// exactly as /score does.
		if restored != nil {
			hub.Broadcast(EventCompetitorStatusUpdated, gin.H{
				"competitionId": id,
				"status":        restored,
			})
		}
		// A match reopened on a finished league puts the league back into
		// match play (the engine's leagueBackInPlay), which only the pass
		// does; it reports a start, broadcast as competition_started.
		tryAutoCompletePoolsAfterWrite(c, eng, hub, id)

		c.JSON(http.StatusOK, gin.H{"reopenedMatches": blockedMatchesPayload(reopenedDownstream)})
	})

	// POST /competitions/:id/matches/:mid/requeue-blocker-and-reopen
	//
	// mp-gmcg (review A4): the ATOMIC court-busy remedy. When a plain reopen of
	// a finished kachinuki match is refused because ANOTHER match holds its
	// court, this requeues that blocking match AND reopens the target under ONE
	// hold of the court-exclusivity lock, so no peer can grab the freed court
	// between the two steps the old two-call client flow made. The blocker may
	// be in the same competition (competitions run across several courts, so a
	// sibling match can hold this one's court) or in a different one — the body
	// carries whichever competition owns it. It reopens a recorded result (the
	// blocker keeps its score), so it is main-password-gated in self-run mode via the
	// central allowlist, the same class as reopen.
	// Body: {blockerCompId, blockerMatchId, reason?, forceDownstreamReopen?} —
	// reason and forceDownstreamReopen optional, exactly like reopen (an
	// absent reason leaves the target ReopenPending). The target may be any
	// match reopen accepts (engine.ReopenMatch).
	r.POST("/competitions/:id/matches/:mid/requeue-blocker-and-reopen", func(c *gin.Context) {
		id, ok := requireValidCompID(c)
		if !ok {
			return
		}
		mid := c.Param("mid")
		if err := validateMaxLen("matchId", mid, MaxLenMatchID); err != nil {
			c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
			return
		}
		var body struct {
			BlockerCompID         string `json:"blockerCompId"`
			BlockerMatchID        string `json:"blockerMatchId"`
			Reason                string `json:"reason"`
			ForceDownstreamReopen bool   `json:"forceDownstreamReopen"`
			// ConfirmFinished: as on reopen, for the target's league.
			ConfirmFinished bool `json:"confirmFinished"`
			// ConfirmLots is the organiser's "Replace the lots": a write that gives a drawn
			// tie-breaker bout a winner, or reopens it, over lots they entered lands (409
			// lots_replaced without it). Request-only, never stored.
			ConfirmLots bool `json:"confirmLots"`
			// The optional writeId and basis (bc-evts F1), the target's.
			writeTag
		}
		if err := c.ShouldBindJSON(&body); err != nil && !errors.Is(err, io.EOF) {
			c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
			return
		}
		if err := body.validateTag(); err != nil {
			c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
			return
		}
		if err := state.ValidateCompetitionID(body.BlockerCompID); err != nil {
			c.JSON(http.StatusBadRequest, gin.H{"error": "blockerCompId: " + err.Error()})
			return
		}
		if body.BlockerMatchID == "" {
			c.JSON(http.StatusBadRequest, gin.H{"error": "blockerMatchId is required"})
			return
		}
		if err := validateMaxLen("blockerMatchId", body.BlockerMatchID, MaxLenMatchID); err != nil {
			c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
			return
		}
		reason := strings.TrimSpace(body.Reason)
		if reason != "" {
			if err := validateMaxLen("reason", reason, MaxLenCorrectionReason); err != nil {
				c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
				return
			}
		}

		// The target's league is warned about before the blocker is requeued.
		if lerr := refuseFinishedLeagueReopen(tx, tl, id, mid, body.ConfirmFinished); lerr != nil {
			if !respondIfLeagueFinished(c, lerr) {
				internalError(c, lerr)
			}
			return
		}

		var reopenedDownstream []engine.ReopenedMatch
		restored, err := eng.RequeueBlockerAndReopen(id, mid, body.BlockerCompID, body.BlockerMatchID, reason, body.tagInto(engine.ForceOptions{
			Force:       body.ForceDownstreamReopen,
			ConfirmLots: body.ConfirmLots,
			Reopened:    &reopenedDownstream,
		}))
		if err != nil {
			var notFoundErr *engine.NotFoundError
			var validationErr *engine.ValidationError
			var courtBusyErr *engine.CourtBusyError
			switch {
			case errors.Is(err, engine.ErrMatchAlreadyCompleted):
				// The BLOCKER finished in the race window between
				// requireBlockerHoldsCourt's read and RevertMatchToQueue.
				// requireBlockerHoldsCourt runs under the court lock, but that
				// does not exclude the completion paths that win the race: POST
				// /decision (kiken/fusenpai) and POST /bulk-score complete a match
				// under the per-comp lock alone, never the court lock (mp-gmcg
				// review; the cached pre-tx status read widens the window
				// further). The court is now FREE (occupancy keys on status ==
				// running), so the remedy is to retry the plain reopen, NOT the
				// sentinel's "correct a completed bout" (which names no match and
				// points at the wrong fix). Still a 409 — a raw fall-through would
				// 500, as the sentinel is not a ValidationError/NotFound/CourtBusy.
				// bc-rawm/bc-cse: operator sentence naming the blocker by
				// its label, no internal id, no em-dash.
				blockerLabel := engine.SentenceCase(matchLabelOrID(store, body.BlockerCompID, body.BlockerMatchID))
				c.JSON(http.StatusConflict, gin.H{"error": fmt.Sprintf("%s finished on its own, so its court is now free. Retry the reopen.", blockerLabel)})
			case errors.Is(err, engine.ErrReopenNotCompleted):
				// Names a bad TARGET (not completed). The requeue-and-reopen
				// path pre-checks this read-only BEFORE the destructive
				// revert (checkTargetReopenable), so in the common path the
				// blocker keeps its live score. A /decision or /bulk-score
				// write racing between that pre-check and the reopen's own
				// in-tx re-check can still surface this post-revert (see the
				// NARROWS comment in engine's RequeueBlockerAndReopen) --
				// i.e. with the blocker already requeued; rare, and bracket
				// integrity still holds via the re-check. bc-rawm/bc-cse:
				// operator sentence naming the target by its label.
				c.JSON(http.StatusConflict, gin.H{"error": fmt.Sprintf("%s is not completed yet, so there is nothing to reopen.", engine.SentenceCase(matchLabelOrID(store, id, mid)))})
			case respondIfDownstreamKnockoutRunning(c, err):
				// bc-cse: same pre-check timing note as ErrReopenNotCompleted
				// above; someone is fighting the downstream match right now.
			case errors.Is(err, engine.ErrReopenDownstreamFought):
				// Defensive backstop only; see the plain-reopen handler's
				// identical case for why this should not be reachable
				// through this door in practice.
				c.JSON(http.StatusConflict, gin.H{"error": err.Error()})
			case respondIfDownstreamKnockoutPlayed(c, err):
				// Refused BEFORE the revert (checkTargetReopenable runs first),
				// so the blocker still holds its score; the operator may retry
				// with forceDownstreamReopen once they have confirmed.
			case respondIfLotsReplaced(c, err):
				// Refused BEFORE the revert too (the same pre-check): the
				// blocker keeps its score until confirmLots is sent.
			case errors.As(err, &courtBusyErr):
				// A match OTHER than the one we requeued still holds the court.
				respondCourtBusy(c, store, courtBusyErr, courtBusyRemedyReopen)
			case errors.As(err, &notFoundErr):
				c.JSON(http.StatusNotFound, gin.H{"error": err.Error()})
			case errors.As(err, &validationErr):
				c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
			default:
				internalError(c, err)
			}
			return
		}

		// Target re-entered running; blocker went back to scheduled: broadcast
		// both matches.
		broadcastMatchNow(hub, store, id, mid, gin.H{"competitionId": id, "matchId": mid})
		broadcastMatchNow(hub, store, body.BlockerCompID, body.BlockerMatchID, gin.H{"competitionId": body.BlockerCompID, "matchId": body.BlockerMatchID})
		broadcastReopenedDownstream(hub, store, id, reopenedDownstream)
		if restored != nil {
			hub.Broadcast(EventCompetitorStatusUpdated, gin.H{
				"competitionId": id,
				"status":        restored,
			})
		}
		// The target's league, if finished, is back in match play (see the
		// plain reopen above).
		tryAutoCompletePoolsAfterWrite(c, eng, hub, id)

		c.JSON(http.StatusOK, gin.H{"reopenedMatches": blockedMatchesPayload(reopenedDownstream)})
	})

	// DELETE /competitions/:id/matches/:mid/kachinuki-bout
	//
	// mp-gmcg: remove a trailing UNSCORED kachinuki bout appended by mistake
	// ([Record bout] / [Add next bout]). This is the explicit operator undo for
	// the empty appended pairing that otherwise only vanishes implicitly on the
	// End-match strip. It targets a regular numbered bout, NOT a daihyosen
	// (which does not exist in kachinuki); the engine reuses the same
	// trailing-unscored strip the completed write applies, so the removable set
	// is identical in both places.
	//
	// Not court-gated (a running match stays running). It IS self-run
	// main-gated (isSelfRunMainGatedConfigRoute, middleware.go): removing a
	// bout is an organiser correction, the same class as reopen,
	// NOT the participant score path — which self-gates via enforceSelfRunPolicy
	// (this handler has no such in-handler check), so without the allowlist
	// entry a self-run pass-through would let an anonymous spectator delete a
	// live pairing (mp-gmcg review F2).
	r.DELETE("/competitions/:id/matches/:mid/kachinuki-bout", func(c *gin.Context) {
		id, ok := requireValidCompID(c)
		if !ok {
			return
		}
		mid := c.Param("mid")
		if err := validateMaxLen("matchId", mid, MaxLenMatchID); err != nil {
			c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
			return
		}

		// The optional {writeId, basis} (bc-evts F1): a resend of a removal
		// the match's score event log holds is answered as recorded.
		tag, ok := bindOptionalWriteTag(c)
		if !ok {
			return
		}
		updated, err := eng.RemoveTrailingKachinukiBout(id, mid, tag.tagInto(engine.ForceOptions{}))
		if err != nil {
			var notFoundErr *engine.NotFoundError
			var validationErr *engine.ValidationError
			switch {
			case errors.Is(err, engine.ErrNoRemovableBout):
				// bc-rawm/bc-cse: operator sentence naming the match by its
				// label, not the raw sentinel text.
				c.JSON(http.StatusConflict, gin.H{"error": fmt.Sprintf("%s has no unscored bout to remove; only an empty appended bout can be removed.", engine.SentenceCase(matchLabelOrID(store, id, mid)))})
			case errors.Is(err, engine.ErrRemoveBoutNotRunning):
				c.JSON(http.StatusConflict, gin.H{"error": fmt.Sprintf("%s is not running; reopen it before removing a bout.", engine.SentenceCase(matchLabelOrID(store, id, mid)))})
			case errors.As(err, &notFoundErr):
				c.JSON(http.StatusNotFound, gin.H{"error": err.Error()})
			case errors.As(err, &validationErr):
				c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
			default:
				internalError(c, err)
			}
			return
		}

		broadcastMatchNow(hub, store, id, mid, gin.H{
			"competitionId": id,
			"matchId":       mid,
			"result":        matchPtrForBroadcast(updated),
		})

		// Project the 200 body the same way as the broadcast: the caller only
		// needs the (shortened) subResults to re-sync its local log, not the
		// audit free-text (correctionReason/decisionReason) or another client's
		// revSession (mp-gmcg review F2).
		c.JSON(http.StatusOK, gin.H{"result": matchPtrForBroadcast(updated)})
	})

	r.PUT("/competitions/:id/matches/:mid/time", func(c *gin.Context) {
		id, ok := requireValidCompID(c)
		if !ok {
			return
		}
		mid := c.Param("mid")
		var req struct {
			ScheduledAt string `json:"scheduledAt"`
			// The client's id for the change (Y8), as the court move's.
			WriteID string `json:"writeId,omitempty"`
		}
		if err := c.ShouldBindJSON(&req); err != nil {
			c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
			return
		}
		if err := validateMaxLen("scheduledAt", req.ScheduledAt, MaxLenMatchScheduledAt); err != nil {
			c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
			return
		}
		if err := validWriteID(req.WriteID); err != nil {
			c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
			return
		}

		applied := false
		ran := once.do("match\x1f"+id+"\x1f"+mid, req.WriteID, func() bool {
			if err := eng.UpdateMatchTime(id, mid, req.ScheduledAt); err != nil {
				respondEngineError(c, err) // 404 for an unknown match, as the court move (R3)
				return false
			}
			applied = true
			return true
		})
		if !ran {
			// A late duplicate of a change already applied: recorded.
			c.Status(http.StatusOK)
			return
		}
		if !applied {
			return
		}

		hub.Broadcast(EventScheduleUpdated, nil)
		c.Status(http.StatusOK)
	})
}

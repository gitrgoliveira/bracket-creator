package mobileapp

import (
	"encoding/json"
	"errors"
	"fmt"
	"log"
	"net/http"
	"strings"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/gitrgoliveira/bracket-creator/internal/domain"
	"github.com/gitrgoliveira/bracket-creator/internal/engine"
	"github.com/gitrgoliveira/bracket-creator/internal/state"
)

// anyNumberedBoutHasEncho reports whether any NUMBERED sub-result (a real
// team bout, not the position -1 daihyosen) carries an encho marker. Used
// by the score endpoints to decide whether the kachinuki numbered-bout
// encho exception needs the competition loaded at all: ordinary payloads
// carry no numbered-bout encho and must not pay the store read (a
// LoadCompetition that deep-copies the Players slice, per score write).
//
// Shares subBoutNeedsNumberedEnchoAllowance with validateSubBout's gate, so
// this pre-scan and the enforcement it front-runs can never diverge — the
// fragility the prior open-coded copy carried (mp-gmcg review).
func anyNumberedBoutHasEncho(subResults []state.SubMatchResult) bool {
	for i := range subResults {
		if subBoutNeedsNumberedEnchoAllowance(&subResults[i]) {
			return true
		}
	}
	return false
}

// allowNumberedEnchoFromStore resolves the numbered-bout encho gate for
// compID: it loads the competition and applies comp.IsKachinuki, but ONLY when
// the payload actually carries a numbered-bout encho (hasNumberedEncho, from
// anyNumberedBoutHasEncho). An ordinary payload carries none and must not pay
// the store read on the hot scoring path.
//
// This is the single decision point for the kachinuki bout-level encho
// exception (mp-gmcg): a tied kachinuki pairing may be fought on in overtime
// on that same bout, so the daihyosen-only encho gate in validateSubBout is
// relaxed for kachinuki competitions — in EVERY phase. Whether the final
// pairing must produce a result (e.g. the taisho must be defeated) is
// OPERATOR DISCRETION, never derivable from pool-vs-bracket (operator ruling,
// 2026-08-01, reverting an earlier bracket-only scoping that hard-coded the
// rule by phase — the exact thing spec 006's problem statement forbids). The
// encounter-level no-draw rule for brackets lives in validateBracketCompletion,
// not here. Both the single-score and bulk-score paths must route through
// this helper rather than re-deriving the rule.
//
// FAIL CLOSED: any load failure keeps the STRICT daihyosen-only gate. The
// error is logged rather than swallowed (errcheck) and rather than returned:
// a load failure must not surface to the operator as a 400 blaming the
// payload ("encho is only valid for the daihyosen representative bout
// (position -1)"), which is what an ignored error produced.
func allowNumberedEnchoFromStore(store CompetitionStore, compID string, hasNumberedEncho bool) bool {
	if !hasNumberedEncho {
		return false
	}
	comp, err := store.LoadCompetition(compID)
	if err != nil {
		log.Printf("LoadCompetition(%s) for the numbered-bout encho gate: %v; keeping the strict gate", compID, err)
		return false
	}
	return comp.IsKachinuki()
}

// hanteiAttributionNeedsBackfill reports whether a score payload needs its
// stored pairing consulted ahead of hantei validation: either the deprecated
// match-level decidedByHantei flag is set, or the payload already carries
// the "Ht" mark itself (the modern shape - no flag required, per
// specs/openapi.yaml). Shared by backfillMatchIdentityForHantei below (the
// single function that replaced two hand-copied twins,
// backfillMatchLevelSidesForLegacyHantei and
// backfillMatchLevelIDsForHanteiAttribution) so the gate is now structurally
// single and cannot drift into two different gates again: that exact drift
// (this function once gated on the flag alone, while the ids twin had
// already widened to `legacyFlagged ||
// req.HanteiDecided()`) is what let a modern minimal hantei payload
// (winner + ipponsA/ipponsB carrying the mark, no sides, no ids, no legacy
// flag - legal per the OpenAPI contract, since the engine's reconcileSides
// backfills sides/ids from the stored match anyway) 400 with "the hantei
// mark belongs in the winner's ippon list", while the identical payload plus
// decidedByHantei:true was backfilled and accepted (bc-dmsr review).
func hanteiAttributionNeedsBackfill(req *state.MatchResult) bool {
	legacyFlagged := req.DecidedByHantei != nil && *req.DecidedByHantei
	return legacyFlagged || req.HanteiDecided()
}

// backfillMatchIdentityForHantei backfills an omitted sideA/sideB and/or
// sideAID/sideBID on a score payload from the STORED pairing, but only when
// the payload would otherwise be unattributable for hantei (see
// hanteiAttributionNeedsBackfill). This is the single function that replaced
// two hand-copied twins, backfillMatchLevelSidesForLegacyHantei (names) and
// backfillMatchLevelIDsForHanteiAttribution (ids): both filled from the same
// underlying call, state.Store.MatchSidesByID, which already returns all
// four values (sideA, sideB, sideAID, sideBID) in one pass, so keeping them
// separate cost two store reads per result instead of one and forced every
// call site to invoke both in lockstep anyway (there is no site that wants
// one half without the other).
//
// Why this backfill exists at all: the request-boundary fold (state.
// MatchResult.NormalizeLegacyHantei, called from ScoreRequest.
// validateWithOptions / validateBulkScoreLengths) attributes the "Ht" mark by
// comparing the payload's Winner against its SideA/SideB (name path) or its
// WinnerID/SideAID/SideBID (id path, validateHanteiMarkPlacement /
// domain.AttributeWinnerSide, whose id branch names a side as soon as the
// winner id equals the id ONE side carries, even if the other carries none;
// it was all-three-or-nothing before bc-dnst, and the backfill below is still
// required because the SPA sends a winner id and NEITHER side id). Per
// specs/openapi.yaml, MatchResult.sideA/sideB are NOT
// required on a score write — the engine's reconcileSides (internal/engine/
// scoring.go) backfills an omitted side from the stored match, exactly so a
// minimal payload (winner + ippons only) still writes cleanly. But that
// backfill runs AFTER the request-boundary fold, so:
//
//   - (bc-qual review) A client sending {winner, ipponsA/ipponsB carrying the
//     mark} with no sides used to have its verdict correctly attributed once
//     the pre-array bracket_result.go path read the flag against stored
//     sides at RENDER time (sides are always populated by then); the
//     array-mark representation now needs the sides at FOLD time, before
//     they exist on the wire payload, so foldLegacyHantei's
//     drop-never-guess default silently erases the verdict with a 200 — or,
//     for a payload with no legacy flag at all, 400s at validation instead
//     (this gate, hanteiAttributionNeedsBackfill, used to be narrower and
//     only covered the flagged case before it was widened).
//   - (bc-dmsr review, two threads sharing one root cause) The SPA sends
//     winnerId (state.MatchResult.WinnerID) but never sideAId/sideBId -
//     web-mobile/js/api_serializers.jsx's toBackendMatchResult computes both
//     locally to place the "Ht" mark itself, then discards them. With two of
//     the three id-branch fields always missing on the wire, validation fell
//     back to the name comparison on every request, with two consequences:
//     (1) a same-name pair's mark, placed correctly BY ID on the client,
//     could be rejected outright by the server's name-only re-check - the
//     operator could not record a same-name-pair hantei through the SPA at
//     all, exactly the scenario the id work was built for; and (2) a mark
//     placed by NAME (a stale client, or an offline-queue replay serialized
//     before this fix) could be ACCEPTED by validation's name fallback and
//     then silently stripped later by the engine's stripInvalidHantei,
//     which runs AFTER this backfill has filled in the real stored ids and
//     so attributes by id - a 200 response with the operator's verdict
//     quietly gone.
//
// Running this backfill ahead of validation makes the validator see the SAME
// name pair and id triple the engine will eventually use, so the two can no
// longer disagree: the sides-less legacy payload and the same-name-pair case
// are now accepted, and the silently-stripped-mark case now 400s at
// validation instead of silently losing the mark at write time.
//
// This backfill runs BEFORE validateWithOptions/validateBulkScoreLengths so
// the fold sees the real pairing. It only runs when
// hanteiAttributionNeedsBackfill is true and at least one field is empty, so
// the hot ordinary-score path (the overwhelming majority of writes, which
// carries no mark at all) never pays a store read.
//
// It only fills an EMPTY field (never overwrites a client-supplied one), the
// same "backfill, never overwrite" rule reconcileSides itself applies, so a
// genuine sideA/sideB or sideAID/sideBID mismatch is still caught downstream
// (by reconcileSides, or by stripInvalidHantei) exactly as before; this
// cannot weaken those guards, only let attribution see what they were always
// going to fill in anyway. A bracket match's stored ids are "" only when its
// own row is unstamped (a bye, an unresolved feeder, or an unrepaired legacy
// row); since bc-brid a stamped row's real ids backfill here too, exactly
// like a pool match, and only the unstamped residue still falls back to the
// name path for bracket hantei.
//
// Store-read failure and the one deliberate behaviour change from the merge:
// the two twins had DIFFERENT store-error policies before this merge — the
// sides twin swallowed a MatchSidesByID error silently (documented as a
// deliberate fail-closed: a name-attribution 400 is preferable to guessing a
// side), while the ids twin logged it (documented reason: a transient error
// here would leave the validator on the name fallback while
// stripInvalidHantei, running after this backfill has since filled in the
// real ids, attributes by id - exactly the 200-with-verdict-gone divergence
// the ids twin existed to close, so a silent divergence between validator
// and engine attribution deserved a trace). Both then returned without
// filling anything and let the request proceed on the name fallback. Merged,
// this single function LOGS on any MatchSidesByID error. That is a strict
// improvement, not a behaviour change to the write path: the resulting
// behaviour (fill nothing, proceed on the name fallback) is identical to
// before in every case, only the diagnostic is now also emitted in the one
// extra case (a sides-only miss) where previously only the ids twin would
// have logged. Do not "restore" the silent variant for that case.
func backfillMatchIdentityForHantei(store CompetitionStore, compID, matchID string, req *state.MatchResult) {
	// bc-pnum ruling 1d: also runs whenever the payload names a WinnerID.
	// validateWinnerIDMatchesSide now rejects a WinnerID naming neither side
	// as soon as AT LEAST ONE side id is known (domain.WinnerIDAcceptable's
	// only exemption is BOTH ids unknown together; the old broader "only
	// when both side ids are already on the wire" tolerance is gone), so it
	// needs the real stored ids to check against -- the SPA sends winnerId
	// alone (api_serializers.jsx), never sideAId/sideBId, so without this the
	// wire payload would otherwise carry no side ids at all and every
	// ordinary completed-match write would fail that check. Narrowed to the
	// two id fields specifically (PR #416 finding 7), not sideA/sideB too:
	// once both ids are already on the wire, validateWinnerIDMatchesSide has
	// everything it needs without a store round-trip, and the engine's own
	// gate inside the transaction remains the authoritative check either
	// way. Only a WRITE THAT NAMES A WINNER pays the extra read at all (a
	// "running"/in-progress update carries no WinnerID), so the hot
	// in-progress-score path stays free of it, matching the original
	// hantei-only rationale below for that path specifically.
	if !hanteiAttributionNeedsBackfill(req) && (req.WinnerID == "" || (req.SideAID != "" && req.SideBID != "")) {
		return
	}
	if req.SideA != "" && req.SideB != "" && req.SideAID != "" && req.SideBID != "" {
		return
	}
	storedA, storedB, storedAID, storedBID, found, err := store.MatchSidesByID(compID, matchID)
	if err != nil {
		log.Printf("MatchSidesByID(%s, %s) for hantei attribution: %v; falling back to name attribution", compID, matchID, err)
		return
	}
	if !found {
		return
	}
	if req.SideA == "" {
		req.SideA = storedA
	}
	if req.SideB == "" {
		req.SideB = storedB
	}
	// An id is filled in only beside the name it belongs to. The engine reads
	// a side id equal to the stored one as proof that the side is the same
	// competitor under an old name (adoptCurrentSideName, a write queued
	// before a rename), so an id copied beside a DIFFERENT name would vouch
	// for a payload naming someone else, and the write would be accepted
	// instead of refused as a side mismatch (bc-mrgc review F2). A rename
	// is proven only by an id the client itself sent.
	if req.SideAID == "" && req.SideA == storedA {
		req.SideAID = storedAID
	}
	if req.SideBID == "" && req.SideB == storedB {
		req.SideBID = storedBID
	}
}

// registerScoreHandler wires the `PUT /competitions/:id/matches/:mid/score`
// endpoint via the consumer-boundary interfaces (T014/T017) instead of
// the concrete `*engine.Engine` / `*Hub`. This is the Slice 0
// demonstration of the interface-DI pattern (NFR-002): handler tests
// can drive this code path with a stub ScoringEngine + Broadcaster, no
// temp dirs, no real engine wiring.
//
// Behaviour is identical to the pre-Slice-0 version except for the new
// ScoreRequest.Validate() call, which surfaces a 400 with the field
// name when the body is malformed against its own shape rules
// (Status outside the documented enum, Winner not naming either side).
// The engine's preserve-on-empty-side fallback continues to handle the
// "client sends scoring fields only" case.
//
// T156: the match-write + ineligibility-check-and-set + (T128) lineup-
// freeze now run inside one Store.WithTransaction so they all commit
// under a single per-comp lock acquire. The kachinuki advance + auto-
// complete-pools post-writes deliberately run AFTER the tx, both
// reach for other per-comp locked operations (UpdatePoolMatchByID,
// UpdateCompetitionChanged) which would deadlock inside the tx, and
// they're already structured as best-effort side effects with their
// own non-fatal failure-handling. Bulk-score handler is intentionally
// NOT migrated: the partial-success error array semantics need a
// per-result tx (or a different commit shape) and that's out of scope
// for this slice.
// scoreRequestBody wraps ScoreRequest with transient, request-only
// fields. These bind from the JSON body but are NOT part of
// state.MatchResult, so they can never be persisted.
type scoreRequestBody struct {
	ScoreRequest
	// writeTag is the optional writeId and basis (bc-evts F1): a resend of
	// a write the match's score event log holds answers as recorded.
	writeTag
	// KachinukiBoutFinal marks an explicit operator "record bout" submit
	// on a kachinuki team match. Only a flagged write triggers
	// MaybeAdvanceKachinuki; unflagged running writes (autosave fires
	// mid-bout, where a 1-0 lead already sets the sub winner) and
	// completed writes (corrections, daihyosen finishes) skip
	// advancement entirely.
	KachinukiBoutFinal bool `json:"kachinukiBoutFinal"`
	// ForceDownstreamReopen bypasses bc-kcdg's downstream-knockout-correction
	// guard (engine.DownstreamKnockoutPlayedError, HTTP 409
	// downstream_knockout_played): correcting a completed bracket match whose
	// already-propagated winner fed a downstream match that has since
	// recorded its own result is refused by default. Setting this applies
	// the correction and reopens the one downstream match that
	// carries its own result (see forceReopenDownstreamChain). The same
	// refusal and the same confirmation cover a POOL correction in a mixed
	// competition that moves a qualifier the knockout already played
	// (engine.requalifyAfterPoolWrite): confirmed, those knockout matches are
	// reopened and the new qualifier is seated.
	ForceDownstreamReopen bool `json:"forceDownstreamReopen"`
	// StartOnly marks a write that only starts the match (the SPA's
	// startPatch): the score the stored match holds is kept rather than
	// replaced by the payload's empty one (engine.ForceOptions.StartOnly).
	StartOnly bool `json:"startOnly"`
	// ForceCourt is the organiser's "Start anyway" (bc-adna): a start on a
	// court that already runs a bout skips both court gates and lands; the
	// sides gate still runs. Request-only, like StartOnly, and dropped for
	// an anonymous self-run caller (enforceSelfRunPolicy).
	ForceCourt bool `json:"forceCourt"`
	// ConfirmFinished is the organiser's "Change it anyway": a correction of
	// a finished league's result lands (refuseFinishedLeagueCorrection, 409
	// league_finished without it). Request-only, like ForceCourt. A participant
	// never reaches it: their write over a finished match is refused first
	// (result_finalized).
	ConfirmFinished bool `json:"confirmFinished"`
	// ConfirmLots is the organiser's "Replace the lots": a write that gives a drawn
	// tie-breaker bout a winner, or reopens it, over lots they entered lands (409
	// lots_replaced without it). Request-only, never stored. A participant never
	// reaches it, as with ConfirmFinished: a drawn bout is a finished match, so
	// their write over it is refused first (result_finalized).
	ConfirmLots bool `json:"confirmLots"`
	// ClearWithdrawal marks a completed correction that removes a withdrawal
	// or default win recorded by mistake: the payload is the real result and
	// replaces the ruling (state.MatchResult.ClearsWithdrawal,
	// engine.KeepsWithdrawalRuling), where a correction without it keeps the
	// ruling. The match stays finished, so this fix never takes the court.
	ClearWithdrawal bool `json:"clearWithdrawal"`
	// Changed names the groups this write changes (bc-mrgc; the names are
	// state/match_groups.go's: points, result, encho, flags, rep,
	// bout:<position>). Absent means every group the payload carries, which
	// is what every client sends today. See engine.mergeMatchWrite.
	Changed []string `json:"changed"`
}

// changedGroupsError refuses a `changed` list naming something that is not a
// group, so a typo cannot silently turn a write into a no-op.
func changedGroupsError(changed []string) error {
	for _, g := range changed {
		if !state.ValidGroup(g) {
			return fmt.Errorf("changed: %q is not a match group (%s, bout:<position>)", g, strings.Join(state.ScalarGroups, ", "))
		}
		// A server group (the kachinuki recorded marks) is never a client's to
		// change: refused rather than ignored, so a client that names one is
		// told rather than silently written around.
		if state.IsServerGroup(g) {
			return fmt.Errorf("changed: %q is written by the server alone and cannot be named in a write", g)
		}
	}
	return nil
}

// scoreResponseWithReopened is the score write's reply: the stored
// MatchResult exactly as it always serialized, plus the ids of any match this
// write reopened.
//
// The ids have to come from the SERVER. The client could guess them from the
// refusal it was just shown, and did, but that is the server's intention
// rather than its outcome: a report saying "m-r2-0 was reopened" because the
// refusal named it, whether or not anything was.
//
// It marshals the result and splices the key in, rather than embedding
// MatchResult in a wrapper struct. state.MatchResult has its own MarshalJSON
// (team_result.go), which an embedded field PROMOTES to the wrapper: the
// wrapper then serializes as just the match and the extra key vanishes with no
// error anywhere. That is exactly what happened here, and it survived the unit
// tests because they assert on the handler's Go-side behaviour; only a live
// request showed the field missing from the body.
func scoreResponseWithReopened(result *state.MatchResult, reopened []engine.ReopenedMatch) any {
	if result == nil {
		return result
	}
	held := result.Merge.HeldGroups()
	if len(reopened) == 0 && len(held) == 0 && len(result.Merge.DisplacedGroups()) == 0 && result.LineupWarning == "" {
		return result
	}
	// Both errors below are RETURNED BY THE API and cannot be discarded
	// (errcheck), not defences against a state anyone expects: a MatchResult
	// that gin is about to marshal anyway does not fail here. Falling back to
	// the bare result keeps the receipt honest -- the write is already stored,
	// so the worst case is the operator not being told what was reopened,
	// rather than a stored result reported as a failure.
	raw, err := json.Marshal(result)
	if err != nil {
		return result
	}
	var merged gin.H
	if err := json.Unmarshal(raw, &merged); err != nil {
		return result
	}
	// Identify each match the way the OPERATOR does: by its match number, the
	// label on the score sheet, the bracket and the Excel tree. The id rides
	// along for anything addressing the match, but nothing shows it to a
	// person (operator ruling 2026-09-19: "that's how the matches should be
	// identified to the user").
	if len(reopened) > 0 {
		merged["reopenedMatches"] = blockedMatchesPayload(reopened)
	}
	// A kachinuki start whose kept lineup a later match's carried lineup
	// could not follow: the start stands, and the operator is told (G1), as
	// the lineup doors tell it (lineupAnswer).
	if result.LineupWarning != "" {
		merged["warning"] = result.LineupWarning
	}
	return withHeldGroups(merged, result.Merge)
}

// withHeldGroups adds to a write's answer what the merge did not apply or
// moved (bc-mrgc): heldGroups, the groups kept in the match's history because
// a newer change to each is stored (or, with heldReason "match_closed",
// because a finish or a decision stamped before them had closed the match);
// displacedGroups, stored scoring stamped after this write's result that it
// closed out into the history (answered with heldReason "match_closed"), or a
// representative pick stamped after the representative bout's removal that the
// removal moved (answered with NO heldReason); and heldReason itself. The
// ONE owner of those three keys: the score answer, the
// representative-bout add and remove (handlers_daihyosen.go) and each
// bulk-score entry build theirs here. The rest of the answer is applied, so
// it still reads as applied.
func withHeldGroups(body gin.H, rep *state.MergeReport) gin.H {
	if held := rep.HeldGroups(); len(held) > 0 {
		body["heldGroups"] = held
	}
	if displaced := rep.DisplacedGroups(); len(displaced) > 0 {
		body["displacedGroups"] = displaced
	}
	if reason := rep.HeldReason(); reason != "" {
		body["heldReason"] = reason
	}
	return body
}

func registerScoreHandler(r *gin.RouterGroup, eng ScoringEngine, store CompetitionStore, tx CompetitionTransactor, hub Broadcaster, verifier PasswordVerifier, tl TournamentLoader) {
	// C3: coalesce high-frequency "running" match_updated broadcasts to ≤4/s
	// per match. Completed writes always proceed (isRunning=false).
	clock := time.Now
	if h, ok := hub.(*Hub); ok && h.pushClock != nil {
		clock = h.pushClock
	}
	coalescer := newMatchBroadcastCoalescerWithClock(clock)
	// The decision, requeue, reopen, start and representative-bout broadcasts
	// reach it through the hub to discard a push it still holds.
	if h, ok := hub.(*Hub); ok {
		h.heldPushes = coalescer
	}
	// The taps' door shares the coalescer (bc-evts).
	registerScoreEventsHandler(r, eng, store, tx, hub, verifier, tl, coalescer)

	r.PUT("/competitions/:id/matches/:mid/score", func(c *gin.Context) {
		id, ok := requireValidCompID(c)
		if !ok {
			return
		}
		mid := c.Param("mid")
		if err := validateMaxLen("matchId", mid, MaxLenMatchID); err != nil {
			// Use the shared validateMaxLen helper for a consistent
			// ValidationError-style body ("matchId: must be <= N characters")
			// that includes the limit, matching the other mobileapp handlers.
			c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
			return
		}
		// The broadcast coalescer's key.
		matchKey := id + ":" + mid

		var body scoreRequestBody
		if err := c.ShouldBindJSON(&body); err != nil {
			c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
			return
		}
		req := body.ScoreRequest
		req.ClearsWithdrawal = body.ClearWithdrawal
		if err := changedGroupsError(body.Changed); err != nil {
			c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
			return
		}
		req.Changed = body.Changed
		req.WriteDoor = engine.DoorScore
		// Mark ids and the event-log version are the replay's, never a body's.
		(*state.MatchResult)(&req).ClearDerivedScoreFields()
		if err := body.validateTag(); err != nil {
			c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
			return
		}
		body.tagOnto((*state.MatchResult)(&req))
		// A start changes the status alone: the score the stored match holds
		// is kept (keepQueuedScore), so the merge must not apply the
		// payload's empty one either.
		if body.StartOnly {
			req.Changed = []string{state.GroupResult}
		}
		// startOnly keeps the stored score in place of the payload's, after
		// the payload is validated, so on anything but a start the verdict
		// would rest on a scoreline nothing checked it against.
		if body.StartOnly && req.Status != state.MatchStatusRunning {
			c.JSON(http.StatusBadRequest, gin.H{"error": "startOnly is only for a write that starts the match (status running)"})
			return
		}
		// Kachinuki exception (mp-gmcg): a tied kachinuki pairing may be
		// fought on in overtime on that same bout, in ANY phase — whether the
		// final pairing must produce a result is operator discretion
		// (allowNumberedEnchoFromStore), so the daihyosen-only encho gate in
		// validateSubBout is relaxed for kachinuki competitions. The
		// competition is loaded only when the payload actually carries a
		// bout-level encho, keeping the hot scoring path free of the read.
		// Fail closed: a load failure keeps the strict gate (400 below) and is
		// logged by allowNumberedEnchoFromStore, which the bulk-score path
		// shares so the gate and its diagnostics stay identical.
		allowNumberedEncho := allowNumberedEnchoFromStore(store, id, anyNumberedBoutHasEncho(req.SubResults))
		// bc-qual/bc-dmsr: backfill omitted sides and sideAID/sideBID from
		// the stored pairing BEFORE validation folds a legacy
		// decidedByHantei flag / checks hantei mark-placement, so a
		// sides-less legacy payload's verdict is attributable instead of
		// silently dropped, and the validator attributes the mark by the
		// SAME id triple the engine will use. See
		// backfillMatchIdentityForHantei.
		backfillMatchIdentityForHantei(store, id, mid, (*state.MatchResult)(&req))
		if err := req.validateWithOptions(allowNumberedEncho); err != nil {
			// Map ValidationError → 400 with the validator's message.
			// Engine errors below remain 500 (they surface I/O / state
			// failures, not request-shape errors).
			var verr *ValidationError
			if errors.As(err, &verr) {
				c.JSON(http.StatusBadRequest, gin.H{"error": verr.Error()})
				return
			}
			c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
			return
		}
		// The kachinuki win decision belongs to a kachinuki competition only
		// (refuseKachinukiDecisionForComp); validation above has no
		// competition to judge it by.
		kachinukiRefusal, kachinukiErr := refuseKachinukiDecision(store, id, (*state.MatchResult)(&req))
		if kachinukiErr != nil {
			internalError(c, kachinukiErr)
			return
		}
		if kachinukiRefusal != nil {
			c.JSON(http.StatusBadRequest, gin.H{"error": kachinukiRefusal.Error()})
			return
		}
		// bc-tmfn: a team match cannot be finished while a numbered bout has
		// no result (every bout is fought). This handler is the door every
		// score editor finishes through; see refuseUnfinishedTeamFinish for
		// what is out of scope and why. The refusal is computed here and
		// applied inside the transaction below (teamFinishRefusalUnderTx),
		// where the stored decision its one exemption reads is race-free.
		finishRefusal, finishErr := refuseUnfinishedTeamFinish(store, id, mid, (*state.MatchResult)(&req))
		if finishErr != nil {
			internalError(c, finishErr)
			return
		}

		// mp-ba3: self-run decision allowlist + result provenance. Runs
		// after Validate() so the request is structurally valid.
		resultSource, ok := enforceSelfRunPolicy(c, tl, verifier, &req, &body.ForceCourt)
		if !ok {
			return
		}

		// mp-62vr: rep-player names are meaningful only on a pool
		// daihyosen/tiebreaker rep bout (the frontend gates the dropdowns on
		// the same id shape via repIsTeam). Strip them from any regular match
		// so a crafted authenticated payload can't persist stale rep metadata
		// onto a normal pool/bracket result.
		if !state.IsPoolSupplementaryMatchID(mid) {
			req.RepPlayerA = ""
			req.RepPlayerB = ""
		}

		result := req.AsMatchResult()
		// A stamp implausibly far in the future is refused outright, BEFORE any
		// engine call and before the rev-guard records a high-water mark for
		// this request: neither honouring it nor zeroing it is safe (see
		// modifiedAtRefuseSkewMs). Nothing is written and nothing is broadcast,
		// exactly as the superseded arm below skips both.
		if serverNowMs, aheadMs, refuse := clientClockSkew(result.ModifiedAt); refuse {
			respondClockSkew(c, serverNowMs, aheadMs)
			return
		}
		// Reject a hostile/buggy negative client timestamp so it cannot freeze
		// the match against later legitimate writes (mp-y3nk).
		result.ModifiedAt = clampClientModifiedAt(result.ModifiedAt)
		result.ResultSource = resultSource
		// Normalize the audit reason once, before validation and the engine
		// write, so a whitespace-only reason can't satisfy the correction gate
		// and the persisted value never carries leading/trailing whitespace.
		result.CorrectionReason = strings.TrimSpace(result.CorrectionReason)
		// A correctionReason is meaningful only on a correction (an overwrite of
		// an already-completed result). A non-completed write can never be one,
		// so don't persist a client-supplied reason there.
		if result.Status != state.MatchStatusCompleted {
			result.CorrectionReason = ""
		}

		// NOTE (mp-gmcg): kachinuki completion is operator-led. A completed
		// write on a kachinuki match is always an explicit operator action
		// ("End match") and is accepted even when the roster snapshot shows
		// players remaining: team sizes are unregulated and the
		// taisho-defeated rule legitimately ends a match early, so the app
		// cannot second-guess the shiaijo. The former premature-completion
		// 409 gate was removed with the engine's auto-finalize.

		// A previous build's running write older than one its own board
		// already sent (a lower rev of the same revSession) is held whole by
		// the engine against the score event log (engine
		// olderRevisionThanLogged, bc-evts stage 2b), which replaced the
		// in-memory rev guard here: it survives a restart and needs no
		// clearing when the match leaves running.

		isWithdrawal := domain.IsWithdrawalDecisionStr(result.Decision)
		// isCorrection: a completed -> completed overwrite (the operator is
		// fixing an already-finished result, e.g. via the shiaijo console's or
		// the Scores page's "Correct" affordance). Such a write never places or
		// keeps the match on the court, so BOTH court-exclusivity gates below
		// must skip it; otherwise correcting a completed match while ANOTHER
		// match runs on the same court is wrongly rejected with court_busy,
		// which blocks the operator from going back to correct any match. The
		// pre-tx store read is a benign TOCTOU (see matchStatusFromStore). A
		// first finalization (running -> completed) is NOT a correction, so the
		// start gate still runs there and its eligibility/simultaneity checks
		// are preserved.
		//
		// A running or scheduled write over a finished match takes no court
		// either: the closed-match rule turns it away, so it must reach the
		// merge to be answered match_closed. That puts the store
		// read on the live-scoring path, where it is a cached read. A stale
		// read there can only say "completed" for a match a reopen has just put
		// back to running, and that match already holds its court.
		isCorrection := writesOverFinished(result.Status) &&
			matchStatusFromStore(store, id, mid) == state.MatchStatusCompleted

		// The pre-tx isCorrection is read by the cross-comp gate only (StartMatchTx
		// re-derives it in the transaction, mp-gmcg review). The stale read is
		// benign because a correction, or any write over a finished match
		// (writesOverFinished: completed, running or scheduled writes over a
		// finished match), takes no court, so a misclassified first finalization
		// skips a court check its own effect would have made a no-op. Separately,
		// bc-adna skips the gate for forceCourt and, inside
		// CheckCrossCompCourtBusyOnStart, for any write that does not move the match
		// into running or that goes to a match already stored running.

		// FR-035: WithCourtExclusivityLock serializes the cross-competition
		// court-busy check + per-competition write under a tournament-level
		// mutex so two concurrent match-starts on the same court in different
		// competitions can't both pass the cross-comp check before either
		// commits (TOCTOU). Withdrawal decisions skip the court gate, operators
		// must record kiken/fusenpai regardless of court state.
		var (
			engStatus *domain.CompetitorStatus
			engErr    error
			// reopenedDownstream (bc-kcdg) collects the IDs of any downstream
			// bracket match reopened by a forced correction, populated only
			// when body.ForceDownstreamReopen actually unblocked one. Each
			// gets its own match_updated broadcast below, alongside the
			// corrected match's own.
			reopenedDownstream []engine.ReopenedMatch
		)
		txErr := tx.WithCourtExclusivityLock(func() error {
			// bc-adna: the court is judged only on a write that moves the
			// match into running (CheckCrossCompCourtBusyOnStart reads the
			// stored status here, pre-tx under the court lock), and not at
			// all for the organiser's forceCourt.
			if !isWithdrawal && !isCorrection && !body.ForceCourt {
				if err := eng.CheckCrossCompCourtBusyOnStart(id, mid, result.Status); err != nil {
					return err
				}
			}
			// T156: run the score write + ineligibility update + lineup-freeze
			// inside a single per-comp lock acquire via WithTransaction. The
			// engine's RecordMatchResultWithIneligibilityTx dispatches every
			// store call through `stx`, so no internal call re-acquires the
			// lock (non-reentrant; nesting would deadlock).
			//
			// FR-035: intra-competition eligibility and court gate. Checks that
			// no OTHER match in compID's own pool/bracket is running on the same
			// court, plus participant ineligibility. Withdrawal decisions bypass
			// so operators can record kiken on matches with ineligible participants.
			return tx.WithTransaction(id, func(stx state.StoreTx) error {
				// The stored match is read ONCE, here, for every guard below:
				// nothing writes it between them (the self-run judge rewrites
				// result only), and each walk of it is a real file read while
				// the per-comp write lock is held. Fails CLOSED on a load error
				// (the correction-reason gate must not pass on an assumed status).
				snap, found, snapErr := matchSnapshotOrErr(stx, id, mid, "score write")
				if snapErr != nil {
					return snapErr
				}
				// A fighter name the write types on a bout row is stored
				// title-cased, judged against the stored rows so an echo of
				// a bout already fought is left as recorded, before any judge
				// below reads the rows.
				titleCaseTypedFighters(result.SubResults, snap.Stored)
				// The competition tells the exact-replay probe (the self-run
				// judge's and applyCorrectionReason's) a kachinuki or engi
				// write (bc-xrpl).
				comp, compErr := stx.LoadCompetition(id)
				if compErr != nil {
					return compErr
				}
				// mp-ba3, bc-dhas: the self-run judge runs under the per-comp
				// lock, so concurrent anonymous submissions cannot race it. It
				// may rewrite result's representative bout before the write.
				selfReported := resultSource == "self-reported"
				// The score log, read at most once for the judges below.
				score := &seenResultCheck{stx: stx, compID: id, matchID: mid}
				if selfReported {
					if err := holdSelfReportedWriteUnderTx(id, mid, snap, found, result, comp, body.StartOnly, score); err != nil {
						engErr = err
						return nil
					}
				}
				// Who the write seats is judged after the engine has written
				// it, on what it landed (landedMembersRefusal), and decided
				// here, before the engine rewrites result: a participant's
				// numbered rows and the representative bout's picks, the
				// organiser's picks alone. A start keeps the stored score
				// whatever it sends, so the row it carries is no pick.
				judgeLanded := found && !body.StartOnly && introducesJudgedMember(result.SubResults, snap, selfReported)
				// Correction audit: a completed -> completed overwrite requires a
				// non-empty CorrectionReason for traceability. Ending a match
				// reopened without a reason is never refused (operator ruling
				// 2026-09-25). A genuine first finalization needs no reason but
				// must carry the STORED one forward. All of that rule lives in
				// applyCorrectionReason, shared with the bulk-score path. It
				// runs inside the tx so the is-completed read is race-free (same
				// lock), over the snapshot read above, and the status it returns
				// is reused by the start gate below.
				check := applyCorrectionReason(snap, result, comp, score)
				existingStatus := check.StoredStatus
				if check.Reject != nil {
					engErr = check.Reject
					return nil
				}
				// bc-tmfn: the team finish gate's in-tx half (see the
				// refusal computed before the court lock above).
				if refusal := teamFinishRefusalUnderTx(finishRefusal, check, result); refusal != nil {
					engErr = refusal
					return nil
				}
				// A correction of a finished league's result is warned about
				// first: after the self-run judge above, so a
				// participant is still answered result_finalized.
				if err := refuseFinishedLeagueCorrection(stx, tl, id, mid, check.Corrects, body.ConfirmFinished); err != nil {
					var refusal *leagueFinishedRefusal
					if errors.As(err, &refusal) {
						engErr = refusal
						return nil
					}
					return err
				}
				// A running- or scheduled-status write over a finished match
				// never reverts it: such a write never carries the result group
				// (engine.runningOverFinished), so the match stays finished.
				// Its scoring changes are turned away when they were made after
				// the result was recorded (the closed-match rule: they are kept
				// in the history and answered match_closed), and are ordered by
				// their stamp when made before it, e.g. an autosave queued before
				// Finish and flushed afterwards. There is still no
				// sanctioned way to send a COMPLETED match back to the queue.
				// The StartMatchTx eligibility/simultaneity gate keys off the
				// RACE-FREE in-tx status, not the pre-tx cache read: `isCorrection`
				// is derived from matchStatusFromStore -> MatchStatusByID ->
				// loadCached, which validates on mtime alone, so a completed match
				// requeued within the same ~1ms kernel-clock tick still reads
				// `completed` and would misclassify a genuine FIRST finalization as
				// a correction — skipping the ineligible-competitor (409) and
				// simultaneous-match checks (CLAUDE.md: never key on mtime alone;
				// mp-gmcg review). existingStatus comes from the same tx/lock as
				// this write, so completed->completed corrections still skip the
				// gate while a scheduled/running stored status runs it.
				isCorrectionInTx := writesOverFinished(result.Status) &&
					existingStatus == state.MatchStatusCompleted
				if !isWithdrawal && !isCorrectionInTx {
					// bc-adna: the in-competition court gate judges the same
					// move into running, from the snapshot; a forced start runs
					// the sides gate alone.
					skipCourtGate := body.ForceCourt ||
						result.Status != state.MatchStatusRunning ||
						existingStatus == state.MatchStatusRunning
					if err := eng.StartMatchTx(stx, id, mid, skipCourtGate); err != nil {
						engErr = err
						return nil
					}
				}
				engStatus, engErr = eng.RecordMatchResultWithIneligibilityTx(stx, id, mid, result, engine.ForceOptions{
					Force:       body.ForceDownstreamReopen,
					ConfirmLots: body.ConfirmLots,
					Reopened:    &reopenedDownstream,
					StartOnly:   body.StartOnly,
					ForceCourt:  body.ForceCourt,
					// A participant's exact resend of a finish changes nothing,
					// so it leaves no history line to loop.
					NoReplayHistory: selfReported,
				})
				// engErr is a normal application-level signal (AlreadyIneligible
				// → 409, validation/not-found → other codes); we surface it
				// after the tx returns. The score-write inside the tx already
				// includes the K3 rollback for the AlreadyIneligible path,
				// returning nil here commits whatever final state the engine
				// settled on.
				//
				// The one exception is a refusal of what the write landed:
				// it is RETURNED, which aborts the transaction, so nothing of
				// the write survives (no match write, no history line, no
				// eligibility record). It is judged only for a write the
				// engine accepted: a superseded or rolled-back one must still
				// commit its history and its restore.
				if engErr == nil && judgeLanded {
					if err := landedMembersRefusal(stx, id, mid, snap, selfReported); err != nil {
						return err
					}
				}
				if engErr == nil && check.ClearBracketReopenPending {
					// Only after the write actually landed: this branch commits
					// even when engErr is set, so discharging any earlier would
					// clear the flag for a rejected write.
					// ClearBracketReopenPending implies InBracket, so skip the pool probe.
					return dischargeReopenPendingUnderTx(stx, id, mid, "", true)
				}
				return nil
			})
		})
		// A refusal of what the write landed aborted the transaction
		// (landedMembersRefusal) and is answered like any other refusal below.
		var landed *landedWriteRefusal
		if errors.As(txErr, &landed) {
			engErr, txErr = landed.refusal, nil
		}
		if txErr != nil {
			// txErr carries errors from CheckCrossCompCourtBusy (cross-comp
			// court conflict or match-not-found) or from the WithTransaction
			// infrastructure itself (WAL commit failure, etc.).
			var courtBusyErr *engine.CourtBusyError
			if errors.As(txErr, &courtBusyErr) {
				respondCourtBusy(c, store, courtBusyErr, courtBusyRemedyStart)
				return
			}
			var notFoundErr *engine.NotFoundError
			if errors.As(txErr, &notFoundErr) {
				c.JSON(http.StatusNotFound, gin.H{"error": txErr.Error()})
				return
			}
			internalError(c, txErr)
			return
		}
		if engErr != nil {
			if errors.Is(engErr, engine.ErrMatchSuperseded) {
				// bc-lww1: the timestamp guard dropped this write because a NEWER
				// result is already stored. Nothing was persisted, so everything
				// below is skipped — most importantly the broadcast, which used to
				// push the DISCARDED payload to every viewer, board and OBS feed
				// and made the losing operator's screen agree with itself.
				//
				// 200, never 4xx/5xx: a superseded write is not a fault and can
				// never win a retry, and the SPA's offline queue retries 5xx
				// forever (the mp-q8c6 poisoned-queue pattern). `applied:false` is
				// the shape the override endpoint already uses for this exact
				// condition, and the client keys on it.

				respondSuperseded(c, result.Merge.HeldGroups(), result.Merge.HeldReason(), engErr)
				return
			}
			var refusal *selfRunRefusal
			if errors.As(engErr, &refusal) {
				c.JSON(refusal.status, refusal.body())
				return
			}
			if respondIfLeagueFinished(c, engErr) || respondIfLotsReplaced(c, engErr) {
				return
			}
			if errors.Is(engErr, engine.ErrMatchSideMismatch) {
				// The payload named competitors that differ from the stored
				// pairing, refuse rather than rewrite match identity.
				c.JSON(http.StatusConflict, gin.H{
					"error":   "side_mismatch",
					"message": "The submitted competitors don't match this match's pairing. Check the score sheet and try again.",
				})
				return
			}
			var ineligErr *engine.IneligibleCompetitorError
			if errors.As(engErr, &ineligErr) {
				respondStartRefusal(c, store, id, mid, ineligErr)
				return
			}
			var alreadyIneligErr *engine.AlreadyIneligibleError
			if errors.As(engErr, &alreadyIneligErr) {
				c.JSON(http.StatusConflict, gin.H{
					"error":       "already_ineligible",
					"playerId":    alreadyIneligErr.PlayerID,
					"matchId":     alreadyIneligErr.MatchID,
					"reason":      alreadyIneligErr.Reason,
					"reasonHuman": reasonHumanForBarredCompetitor(store, id, mid, alreadyIneligErr.PlayerID, alreadyIneligErr.Reason, alreadyIneligErr.MatchID, alreadyIneligErr.Decision),
				})
				return
			}
			var courtBusyErr *engine.CourtBusyError
			if errors.As(engErr, &courtBusyErr) {
				respondCourtBusy(c, store, courtBusyErr, courtBusyRemedyStart)
				return
			}
			// A pool correction that would move a qualifier out of a knockout
			// match being fought now, or a knockout correction that would
			// change a side of one: terminal, not confirmable.
			if respondIfDownstreamKnockoutRunning(c, engErr) {
				return
			}
			// bc-kcdg: correcting this match would change an already-propagated
			// winner while a downstream match carries a result of its own. Fixed
			// wire contract, shared with every other knockout-correction write
			// (see respondIfDownstreamKnockoutPlayed's doc comment).
			if respondIfDownstreamKnockoutPlayed(c, engErr) {
				return
			}
			var notFoundEngErr *engine.NotFoundError
			if errors.As(engErr, &notFoundEngErr) {
				c.JSON(http.StatusNotFound, gin.H{"error": engErr.Error()})
				return
			}
			var valErr *ValidationError
			if errors.As(engErr, &valErr) {
				c.JSON(http.StatusBadRequest, gin.H{"error": valErr.Error()})
				return
			}
			// engine.ValidationError is a DISTINCT type from the handler-layer
			// ValidationError above; without this mapping an engine precondition
			// rejection (e.g. validateBracketCompletion's "cannot mark completed
			// with no winner; resolve via daihyosen first") surfaced as a 500,
			// which the client write-queue treats as transient and retries
			// (mp-q8c6 poisoned-queue pattern) instead of showing the message.
			// A corrupt overrides.json (computeStandingsFrom, reached here via
			// the pool requalification check) is the other case
			// respondIfEngineWriteError maps, to the same terminal 422 the
			// decision/daihyosen/league-tiebreak/chusen-candidates handlers use;
			// DELETE .../overrides (handlers_competition.go) is the repair door.
			if respondIfEngineWriteError(c, engErr) {
				return
			}
			internalError(c, engErr)
			return
		}

		// Broadcast match update with the full (post-merge) result so
		// SSE consumers see the same payload they'd see on a re-fetch.
		// C3: coalesce high-frequency running-status broadcasts (first-wins
		// within 250ms); completed writes always broadcast unconditionally.
		isRunning := result.Status == state.MatchStatusRunning
		// The payload is built now: a push held to the window's end must show
		// this write's match, not whatever the handler does to it afterwards.
		stampWithdrawnStatus(store, id, result)
		matchUpdated := gin.H{
			"competitionId": id,
			"matchId":       mid,
			"result":        matchPtrForBroadcast(result),
		}
		coalescer.Submit(matchKey, isRunning, runningPushSender(hub, store, id, mid, matchUpdated))
		// bc-kcdg: a forced correction may have reopened downstream bracket
		// matches whose SideA/SideB the correction's propagation just
		// repainted (forceReopenDownstreamChain). Each is a distinct match
		// from the one the operator corrected, so each gets its own
		// match_updated broadcast; a client watching only that court/match
		// would otherwise never learn its verdict was cleared.
		broadcastReopenedDownstream(hub, store, id, reopenedDownstream)
		// T085/T092, when a kiken or fusenpai is recorded, the engine
		// persisted a CompetitorStatus for the losing player; surface
		// it so admin clients can invalidate cached match lists.
		if engStatus != nil {
			hub.Broadcast(EventCompetitorStatusUpdated, gin.H{
				"competitionId": id,
				"status":        engStatus,
			})
		}
		// T135, kachinuki post-score advancement. Gated on the transient
		// kachinukiBoutFinal request flag: only an explicit operator
		// "record bout" submit may advance. Unflagged running writes are
		// mid-bout autosaves (a 1-0 lead already sets the sub winner, so
		// advancing on them would fire while the bout is still being
		// fought) and completed writes are corrections/daihyosen finishes.
		// Runs OUTSIDE the tx because MaybeAdvanceKachinuki calls
		// UpdatePoolMatchByID / UpdateBracket which acquire the per-comp
		// lock themselves; nesting under our tx would deadlock. A
		// non-fatal error here doesn't fail the request: the operator's
		// bout score is already on disk; surfacing a 500 would lead them
		// to retry and double-record. Mirrors the recordIneligibility
		// non-fatal pattern.
		if body.KachinukiBoutFinal {
			// The advance also marks the bout Record bout closed as recorded
			// (bc-kfnl), appending a pairing or not: post is non-nil whenever
			// it wrote, so the answer and the broadcast carry the mark at a
			// side's exhaustion too, where nothing is appended.
			if _, post, kerr := eng.MaybeAdvanceKachinuki(id, mid); kerr != nil {
				log.Printf("engine.MaybeAdvanceKachinuki(%s, %s): %v", id, mid, kerr)
			} else if post != nil {
				// Echo the POST-advance bout log so the open score editor can
				// render the appended pairing immediately (Record bout →
				// next bout appears, mp-gmcg): the `result` echoed below
				// predates the append, and SSE only refreshes the match
				// LIST, not the host's open-match snapshot. MaybeAdvanceKachinuki
				// hands back the post-append log directly, so no store re-read
				// (mp-gmcg review E1), and the stamps the advance left, so
				// the editor's next tap on the new bout is floored by them
				// (bc-hlck) rather than by the pre-advance stamp.
				result.SubResults = post.BoutLog
				result.ModifiedAt = post.ModifiedAt
				result.GroupStamps = post.GroupStamps
				broadcastMatchNow(hub, store, id, mid, gin.H{
					"competitionId": id,
					"matchId":       mid,
					"result":        matchPtrForBroadcast(result),
				})
			}
		}
		tryAutoCompletePoolsAfterWrite(c, eng, hub, id, *result)

		// Don't echo internal write-ordering metadata back in the response.
		result.Rev = 0
		result.RevSession = ""
		c.JSON(http.StatusOK, scoreResponseWithReopened(result, reopenedDownstream))
	})
}

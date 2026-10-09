// Package mobileapp, handlers_daihyosen.go owns the
// `POST /api/competitions/:cid/matches/:mid/daihyosen` endpoint that
// validates a tied knockout-stage team match and appends a daihyosen
// (representative-bout) placeholder to its SubResults (T140, FR-046,
// CHK026).
//
// The endpoint is structurally a sibling of `/decision` and `/score`:
// the operator's UI calls this when both teams finish a knockout match
// with equal IV+PW; the engine returns the placeholder SubMatchResult
// (Position=-1, Decision="daihyosen") which the handler persists onto
// the parent match. The operator then fills the rep player names +
// ippon via the standard score path.
//
// Eligibility integration (CHK026): before validating the tie, the
// handler counts each team's eligible competitors via the competitor-
// status store. When either side has zero eligible competitors the
// engine returns ErrInsufficientEligibility and we respond 409, the
// caller MUST then forfeit the encounter to the opposing team via the
// standard score endpoint (this handler intentionally does NOT
// auto-record the forfeit so the operator confirms it).
package mobileapp

import (
	"errors"
	"io"
	"log"
	"net/http"

	"github.com/gin-gonic/gin"
	"github.com/gitrgoliveira/bracket-creator/internal/domain"
	"github.com/gitrgoliveira/bracket-creator/internal/engine"
	"github.com/gitrgoliveira/bracket-creator/internal/state"
)

// respondIfSuperseded maps a transaction error that is engine.ErrMatchSuperseded
// onto the shared 200 {"applied": false} response and reports that it handled
// it, so the caller can return without falling through to its 500 arm. Both
// daihyosen endpoints (add and remove) end their WithTransaction the same way,
// and the branch below was the same nineteen lines twice.
//
// bc-lww1, reachable from both since bc-dhas: each write carries the client's
// stamp (daihyosenWriteStamp), so one older than the stored result loses the
// timestamp guard. An unstamped request still applies: the bracket projection
// carries no ModifiedAt (the unstamped bypass), and a pool match's copy carries
// the stored stamp, which ApplyByTimestamp's >= comparison accepts.
func respondIfSuperseded(c *gin.Context, err error) bool {
	if !errors.Is(err, engine.ErrMatchSuperseded) {
		return false
	}
	respondSuperseded(c, engine.HeldGroupsOf(err), engine.HeldReasonOf(err), engine.HeldDecisionOf(err))
	return true
}

// daihyosenWriteStamp reads the client's server-relative write stamp from the
// optional {"modifiedAt": N} body both daihyosen endpoints take, and judges it
// exactly as /decision judges its own: a stamp more than
// modifiedAtRefuseSkewMs ahead of the server is refused with the clock_skew
// body before anything is read or written, and a negative one is clamped to
// the unstamped bypass. An empty body is an unstamped request (an older
// client), which always applies. Reports false when it has answered.
//
// The written match carries the stamp, so an add or remove competes on
// timestamps like a score write, and a copy of the match read before it is
// older than the one it returns, which is how a stale refetch is told apart
// from it (keepNewerMatches, patch.jsx).
func daihyosenWriteStamp(c *gin.Context) (int64, bool) {
	var body struct {
		ModifiedAt int64 `json:"modifiedAt"`
	}
	if err := c.ShouldBindJSON(&body); err != nil && !errors.Is(err, io.EOF) {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return 0, false
	}
	if serverNowMs, aheadMs, refuse := clientClockSkew(body.ModifiedAt); refuse {
		respondClockSkew(c, serverNowMs, aheadMs)
		return 0, false
	}
	return clampClientModifiedAt(body.ModifiedAt), true
}

// respondIfValidationError maps a transaction error that is
// *engine.ValidationError onto HTTP 400 and reports that it handled it, so
// the caller can return without falling through to its internalError (500)
// arm. Both daihyosen endpoints share this the same way they already share
// respondIfSuperseded: a write this handler forwards to the engine (e.g. via
// RecordMatchResultWithIneligibilityTx) can fail a downstream engine
// precondition -- backfillMatchIdentity's forward-write WinnerID check
// (bc-idfx finding 10) being the motivating case, reachable when a stored
// match this handler operates on carries an inherited/legacy identity
// mismatch -- and without this branch that 400-shaped client/data error
// surfaced as an opaque 500 the client's write-queue retries forever
// (mp-q8c6 poisoned-queue pattern), exactly the class of bug the other
// score-writing handlers already guard against.
//
// Currently unreachable from either call site (verified, not asserted):
// both handlers clear u.WinnerID/u.WinnerSide before the write they forward
// (bc-idfx finding 3, round 2), so backfillMatchIdentity's specific
// WinnerID-mismatch check can never fire from here -- there is no non-empty
// WinnerID left on the payload for it to reject. Kept anyway as defensive
// depth: it costs nothing, and it is the SAME engine precondition every
// other score-writing handler already guards, so a future daihyosen code
// path that stops clearing the id (or a new ValidationError this engine
// call starts returning for some other reason) fails closed with a 400
// instead of silently regressing to the opaque-500 class of bug this
// existed to fix in the first place.
func respondIfValidationError(c *gin.Context, err error) bool {
	var verr *engine.ValidationError
	if !errors.As(err, &verr) {
		return false
	}
	c.JSON(http.StatusBadRequest, gin.H{"error": verr.Error()})
	return true
}

// DaihyosenEngine is the consumer-boundary view of *engine.Engine used
// by the daihyosen handler. Mirrors engine.Engine.AddDaihyosen +
// RecordMatchResultWithIneligibilityTx + MaybeAutoCompletePoolsAfterWrite.
//
// Defined as a named local interface (rather than reusing ScoringEngine)
// because AddDaihyosen is not on the existing ScoringEngine interface,
// and broadening that surface for one new endpoint would expose the
// method to every other handler family. The write goes through the *Tx
// variant so the read-modify-write runs under the same per-comp lock the
// read used (see RegisterDaihyosenHandlers).
type DaihyosenEngine interface {
	AddDaihyosen(compID, matchID string, sideA, sideB engine.TeamSummary, isPool bool, sideAEligible, sideBEligible int) (*state.SubMatchResult, error)
	RecordMatchResultWithIneligibilityTx(tx state.StoreTx, compID, matchID string, result *state.MatchResult, opts ...engine.ForceOptions) (*domain.CompetitorStatus, error)
	MaybeAutoCompletePoolsAfterWrite(compID string, written ...state.MatchResult) (engine.AutoCompleteOutcome, error)
}

// DaihyosenStore is the consumer-boundary view of *state.Store used by
// the daihyosen handler. Both endpoints do a read-check-write on a single
// match, so they run entirely inside WithTransaction and read via the
// supplied StoreTx (LoadPoolMatches/LoadBracket for the match, plus
// LoadCompetition/LoadParticipants/LoadCompetitorStatus for the CHK026
// eligibility count), keeping the read and the write atomic under one
// acquire of the per-comp lock. Calling the public Store.Load* methods
// inside the closure would deadlock (the lock is non-reentrant), so the
// store surface here is just the transaction entry point.
type DaihyosenStore interface {
	WithTransaction(compID string, fn func(tx state.StoreTx) error) error
}

// selfRunDaihyosenRefusal is the refusal an anonymous self-run add or remove
// of the representative bout gets, nil when the match is running. A
// participant runs the representative bout of the match being fought: one that
// has finished is the organiser's to correct (errResultFinalized, as on the
// score path), and one not started yet is started first. notStarted is the
// sentence for that case. The organiser keeps no such rule: an add to a
// finished fixed-order knockout match may be their correction, since no reopen
// exists for one.
func selfRunDaihyosenRefusal(status state.MatchStatus, notStarted string) *selfRunRefusal {
	switch {
	case status == state.MatchStatusRunning:
		return nil
	case isMatchFinalized(status):
		return errResultFinalized
	default:
		return &selfRunRefusal{status: http.StatusConflict, code: "match_not_running", message: notStarted}
	}
}

// RegisterDaihyosenHandlers wires the POST and DELETE /daihyosen endpoints.
// The caller in server.go passes `*engine.Engine` and `*state.Store` which
// satisfy the local interfaces by structural match. Both routes are public in
// self-run; tl and verifier tell an anonymous caller from the organiser
// (selfRunAnonymous).
//
// T140, FR-046.
func RegisterDaihyosenHandlers(r *gin.RouterGroup, eng DaihyosenEngine, store DaihyosenStore, hub Broadcaster, tl TournamentLoader, verifier PasswordVerifier) {
	r.DELETE("/competitions/:id/matches/:mid/daihyosen", func(c *gin.Context) {
		id, ok := requireValidCompID(c)
		if !ok {
			return
		}
		mid := c.Param("mid")
		// The caller first, so a wrong password is 401 whatever the body says.
		anonymous, ok := selfRunAnonymous(c, tl, verifier)
		if !ok {
			return
		}
		stamp, ok := daihyosenWriteStamp(c)
		if !ok {
			return
		}

		// The whole read-guard-filter-write runs under ONE acquire of the
		// per-comp lock: re-read the match inside the transaction so the
		// DH-unscored guard is evaluated against, and the write applied to,
		// the same locked snapshot. Reading outside the lock and overwriting the
		// whole match (the pre-fix shape) could revert a concurrent bout score
		// or delete a daihyosen that was scored in the read→write window.
		var (
			updated    state.MatchResult
			resp       *txResponse // the answer when nothing is written, decided under the lock
			haveResult bool
			// supersededErr is a write whose every change was held: the
			// transaction commits (its history entry lands) and the answer is
			// applied:false (bc-mrgc).
			supersededErr error
		)
		txErr := store.WithTransaction(id, func(stx state.StoreTx) error {
			match, found, err := findMatchForDaihyosenTx(stx, id, mid)
			if err != nil {
				return err
			}
			if !found {
				resp = &txResponse{status: http.StatusNotFound, body: gin.H{"error": "match not found"}}
				return nil
			}
			if anonymous {
				if refusal := selfRunDaihyosenRefusal(match.Status, "This match has not started. Start it before removing its representative bout."); refusal != nil {
					resp = refusal.response()
					return nil
				}
			}
			dhIdx := state.DaihyosenSubIndex(match.SubResults)
			if dhIdx < 0 {
				resp = &txResponse{status: http.StatusNotFound, body: gin.H{"error": "no_daihyosen"}}
				return nil
			}
			// Guard (re-checked under the lock): refuse removal once the DH
			// bout carries any score. "Scored" means more than the initial
			// placeholder: ippons, a winner, a hantei flag, recorded hansoku
			// penalties, OR a sub-Decision that is no longer the bare
			// "daihyosen" placeholder (e.g. a withdrawal recorded on the rep
			// bout), validateSubBout does not validate sub.Decision, so an
			// acted-on bout can carry a decision without a winner.
			dh := match.SubResults[dhIdx]
			if len(dh.IpponsA) > 0 || len(dh.IpponsB) > 0 || dh.Winner != "" || dh.HanteiDecided() ||
				dh.HansokuA > 0 || dh.HansokuB > 0 ||
				(dh.Decision != "" && dh.Decision != string(domain.DecisionDaihyosen)) {
				resp = &txResponse{status: http.StatusConflict, body: gin.H{"error": "daihyosen_scored"}}
				return nil
			}
			// Build an updated match with the DH sub filtered out.
			filtered := make([]state.SubMatchResult, 0, len(match.SubResults)-1)
			for i := range match.SubResults {
				if i != dhIdx {
					filtered = append(filtered, match.SubResults[i])
				}
			}
			u := *match
			u.SubResults = filtered
			// Unstamped, the copy's own stamp stands (see respondIfSuperseded).
			if stamp > 0 {
				u.ModifiedAt = stamp
			}
			// No court check runs here (mp-95mg). A participant's remove reaches
			// only a running match (selfRunDaihyosenRefusal), which already
			// holds its court. The organiser's reaches a match in any status,
			// and puts a finished or queued one back to running without the
			// court and eligibility checks a start runs; that is theirs to do.
			u.Status = state.MatchStatusRunning
			// Clear ALL DH-derived match-level result/decision metadata so the
			// match returns to a clean running state: the verdict
			// (clearMatchVerdict), and the overtime, which a removed daihyosen
			// must not leave behind either. WinnerID/WinnerSide go with Winner:
			// the organiser's remove also reaches a finished match, whose winner
			// a decision may have recorded beside an unfought representative
			// bout, and a stored POOL match that has picked up a
			// legacy/hand-edited Position=-1 sub CAN carry a stale WinnerID left
			// over from an unrelated prior result, which would otherwise fail
			// backfillMatchIdentity's forward-write validation (bc-idfx finding
			// 10) as an inherited 500, not this handler's own fault.
			clearMatchVerdict(&u)
			u.Encho = nil
			// What the remove changes (bc-mrgc): the representative bout (its
			// stamp stays as a tombstone, so an older write still carrying the
			// row cannot bring it back), its representatives (they go with the
			// row, and their stamp dates the removal so a pick made before it
			// is held, not applied onto nothing), the verdict, the overtime, and
			// the scoreline when clearing the verdict took a hantei mark out of it.
			u.Changed = daihyosenChangedGroups(match, &u, state.GroupResult, state.GroupEncho, state.GroupRepPicks)
			u.WriteDoor = engine.DoorDaihyosenDel
			if _, err := eng.RecordMatchResultWithIneligibilityTx(stx, id, mid, &u); err != nil {
				if errors.Is(err, engine.ErrMatchSuperseded) {
					// Committed, not aborted: the write's history entry is
					// its one footprint and must land.
					supersededErr = err
					return nil
				}
				return err
			}
			updated = u
			haveResult = true
			return nil
		})
		if txErr != nil {
			if respondIfEngineWriteError(c, txErr) {
				return
			}
			internalError(c, txErr)
			return
		}
		if supersededErr != nil {
			respondIfSuperseded(c, supersededErr)
			return
		}
		if resp != nil {
			c.JSON(resp.status, resp.body)
			return
		}
		if !haveResult {
			c.JSON(http.StatusInternalServerError, gin.H{"error": "daihyosen removal produced no result"})
			return
		}

		hub.Broadcast(EventMatchUpdated, gin.H{
			"competitionId": id,
			"matchId":       mid,
			"result":        matchForBroadcast(updated),
		})

		// Public in self-run, so answered as the broadcast is, without the audit notes.
		c.JSON(http.StatusOK, withHeldGroups(gin.H{"result": matchForBroadcast(updated)}, updated.Merge))
	})

	r.POST("/competitions/:id/matches/:mid/daihyosen", func(c *gin.Context) {
		id, ok := requireValidCompID(c)
		if !ok {
			return
		}
		mid := c.Param("mid")
		// The caller first, so a wrong password is 401 whatever the body says.
		anonymous, ok := selfRunAnonymous(c, tl, verifier)
		if !ok {
			return
		}
		stamp, ok := daihyosenWriteStamp(c)
		if !ok {
			return
		}

		// Read-check-write under ONE acquire of the per-comp lock: the tie that
		// gates AddDaihyosen is recomputed from the match's PERSISTED SubResults,
		// so the match must be read under the same lock that writes the appended
		// daihyosen, otherwise a concurrent bout score between read and write
		// would be reverted, or the tie computed from a stale snapshot.
		var (
			updated    state.MatchResult
			subOut     *state.SubMatchResult
			resp       *txResponse // the answer when nothing is written, decided under the lock
			haveResult bool
			// supersededErr: see the remove handler's.
			supersededErr error
		)
		txErr := store.WithTransaction(id, func(stx state.StoreTx) error {
			// Engi competitions decide bouts by referee flag counts; a
			// representative daihyosen bout has no meaning there. Reject with
			// 400 (mirrors the quick-score / override / decision guards)
			// rather than letting the tie-detection path fail with a 500.
			comp, err := stx.LoadCompetition(id)
			if err != nil {
				return err
			}
			if comp != nil && comp.Engi {
				resp = &txResponse{status: http.StatusBadRequest, body: gin.H{"error": "engi competitions do not support daihyosen; use flag scoring instead"}}
				return nil
			}
			// Daihyosen does not exist in kachinuki (mp-gmcg): a tied final
			// bout is a drawn encounter in pools/league, and a knockout tie
			// is resolved by ENCHO on the taisho bout itself, never by a
			// separate representative bout. comp.IsKachinuki() is the engine's
			// own dispatch gate (TeamSize >= 2 + kachinuki type): a
			// competition the engine refuses to advance as kachinuki
			// (MaybeAdvanceKachinuki returns early below TeamSize 2) must not
			// be told it is kachinuki here either.
			if comp.IsKachinuki() {
				resp = &txResponse{status: http.StatusBadRequest, body: gin.H{"error": "daihyosen does not exist in kachinuki; a tied final bout is a draw in pools/league and goes to encho in a knockout"}}
				return nil
			}
			// A representative bout breaks a tie between TEAMS. An individual
			// match has no bouts, so the tie below would read it as level at
			// nothing each and append one. Team is TeamSize >= 2
			// (ValidateCompetitionTeamSize keeps Kind == "team" in step).
			if comp != nil && comp.TeamSize < 2 {
				resp = &txResponse{status: http.StatusBadRequest, body: gin.H{"error": "individual competitions do not support daihyosen; a representative bout breaks a tie between teams"}}
				return nil
			}

			match, found, err := findMatchForDaihyosenTx(stx, id, mid)
			if err != nil {
				return err
			}
			if !found {
				resp = &txResponse{status: http.StatusNotFound, body: gin.H{"error": "match not found"}}
				return nil
			}
			if anonymous {
				if refusal := selfRunDaihyosenRefusal(match.Status, "This match has not started. Start it before adding a representative bout."); refusal != nil {
					resp = refusal.response()
					return nil
				}
			}
			// One representative bout per encounter, for every caller. The tie
			// below counts no representative bout, so a second add would pass
			// it, sit beside the first, and on a finished match put it back to
			// running and bury the verdict recorded on the first (bc-dhas).
			// The score sheet never offers it; two devices adding at once, or a
			// crafted request, would.
			if state.DaihyosenSubIndex(match.SubResults) >= 0 {
				resp = &txResponse{status: http.StatusConflict, body: gin.H{"error": "daihyosen_exists"}}
				return nil
			}

			// credit is state.DefaultWinCreditSide's answer for THIS match
			// (bc-cse: ComputeTeamSummary's credit is no longer optional). For a
			// participant's add it is domain.MatchSideNone: they reach only a
			// running match (above), and DefaultWinCreditSide credits nobody
			// unless the match is completed. The organiser's add reaches a
			// completed one too, where a default-win ruling's credit counts
			// towards the tie, exactly as every other TeamResult reader counts
			// it.
			credit := state.DefaultWinCreditSide(match.Status, match.Decision, match.DecisionBy, match.Attribution())
			sideASummary, sideBSummary := engine.ComputeTeamSummary(match.SubResults, match.SideA, match.SideB, credit)

			// Whether each side can field a representative, read under the
			// same lock (CHK026): a side another match's withdrawal barred
			// cannot, whoever else in the competition is still eligible.
			sideAEligible, sideBEligible, err := countEligibleForSidesTx(stx, id, mid, match.SideAID, match.SideBID)
			if err != nil {
				return err
			}

			sub, err := eng.AddDaihyosen(id, mid, sideASummary, sideBSummary, engine.IsPoolMatchID(mid), sideAEligible, sideBEligible)
			if err != nil {
				switch {
				case errors.Is(err, engine.ErrNotTied):
					resp = &txResponse{status: http.StatusBadRequest, body: gin.H{"error": "not_tied"}}
					return nil
				case errors.Is(err, engine.ErrPoolMatch):
					resp = &txResponse{status: http.StatusBadRequest, body: gin.H{"error": "pool_match"}}
					return nil
				case errors.Is(err, engine.ErrInsufficientEligibility):
					resp = &txResponse{status: http.StatusConflict, body: gin.H{"error": "insufficient_eligibility"}}
					return nil
				default:
					return err
				}
			}

			// Append the placeholder to the match's SubResults and persist via
			// the Tx score path so the append commits under the held lock.
			// No court check runs here (mp-95mg). A participant's add reaches
			// only a running match (selfRunDaihyosenRefusal), whose court slot
			// was committed when it was started through the score endpoint,
			// which holds WithCourtExclusivityLock. The organiser's reaches a
			// match in any status the tie allows, a 0-0 one not started yet or
			// a finished one included, and puts it to running without the
			// court and eligibility checks a start runs; that is theirs to do.
			u := *match
			// AddDaihyosen only succeeds against ErrPoolMatch's rejection when
			// engine.IsPoolMatchID(mid) is false, so `match` here is ALWAYS the
			// bracket projection (daihyosenBracketResult). The organiser's add
			// to a finished match reopens its result, so the verdict it
			// recorded goes (clearMatchVerdict) rather than standing beside the
			// running status until the next score write. A recorded withdrawal
			// or default win stays: removing one is its own action (Clear
			// withdrawal and reopen), and the competitor status it wrote would
			// otherwise name a match that no longer records it. Only its
			// WinnerID and WinnerSide go, as they always have.
			if domain.IsDefaultWinDecisionStr(u.Decision) {
				u.WinnerID = ""
				u.WinnerSide = ""
			} else {
				clearMatchVerdict(&u)
			}
			if stamp > 0 {
				u.ModifiedAt = stamp
			}
			u.SubResults = append(append([]state.SubMatchResult{}, match.SubResults...), *sub)
			u.Status = state.MatchStatusRunning // daihyosen bout in progress
			// What the add changes (bc-mrgc): the new representative bout and
			// the verdict, and the scoreline when clearing the verdict took a
			// hantei mark out of it. The new row is built with no
			// representatives, and the add names state.GroupRepPicks all the
			// same, as the remove does: it dates the (empty) picks of THIS
			// bout, so a pick made on the previous representative bout, stamped
			// between the remove and the add, is older than the add and is held
			// rather than landing on the new bout through the remove's
			// tombstone stamp.
			u.Changed = daihyosenChangedGroups(match, &u, state.GroupResult, state.GroupRepPicks)
			u.WriteDoor = engine.DoorDaihyosenAdd
			if _, err := eng.RecordMatchResultWithIneligibilityTx(stx, id, mid, &u); err != nil {
				if errors.Is(err, engine.ErrMatchSuperseded) {
					supersededErr = err
					return nil
				}
				return err
			}
			updated = u
			subOut = sub
			haveResult = true
			return nil
		})
		if txErr != nil {
			if respondIfEngineWriteError(c, txErr) {
				return
			}
			internalError(c, txErr)
			return
		}
		if supersededErr != nil {
			respondIfSuperseded(c, supersededErr)
			return
		}
		if resp != nil {
			c.JSON(resp.status, resp.body)
			return
		}
		if !haveResult {
			c.JSON(http.StatusInternalServerError, gin.H{"error": "daihyosen add produced no result"})
			return
		}

		hub.Broadcast(EventMatchUpdated, gin.H{
			"competitionId": id,
			"matchId":       mid,
			"result":        matchForBroadcast(updated),
		})

		// Inline auto-complete check (same pattern as
		// tryAutoCompletePoolsAfterWrite). A rep bout is only ever added to a
		// knockout match, so in knockout status this skips the pool pass.
		outcome, autoErr := eng.MaybeAutoCompletePoolsAfterWrite(id, updated)
		switch {
		case autoErr != nil:
			log.Printf("MaybeAutoCompletePools(%s) after daihyosen: %v", id, autoErr)
			c.Header(AutoCompleteErrorHeader, AutoCompleteErrorValue)
		case outcome == engine.AutoCompleteTransitioned:
			hub.Broadcast(EventCompetitionCompleted, gin.H{"competitionId": id})
		case outcome == engine.AutoCompleteTiebreakInjected:
			hub.Broadcast(EventMatchUpdated, gin.H{"competitionId": id})
			hub.Broadcast(EventScheduleUpdated, nil)
		case outcome == engine.AutoCompleteStarted:
			// The added rep bout left the match running, which is a recorded
			// result, so a still-draw-ready competition was started by it
			// (bc-prow). Same events as tryAutoCompletePools' own branch.
			hub.Broadcast(EventCompetitionStarted, gin.H{"competitionId": id})
			hub.Broadcast(EventScheduleUpdated, nil)
		}

		c.JSON(http.StatusOK, withHeldGroups(gin.H{"subResult": subOut, "result": matchForBroadcast(updated)}, updated.Merge))
	})
}

// findMatchForDaihyosenTx looks up the target match by ID UNDER THE
// TRANSACTION LOCK (via the supplied StoreTx), searching pool matches when
// the ID has the "Pool " prefix and otherwise the bracket. Returns
// (match, true, nil) on success; (nil, false, nil) when neither store
// holds the ID; (nil, false, err) on a store-level I/O failure. Reading
// through tx (not the public Store) keeps the read atomic with the
// subsequent write in the same WithTransaction closure.
func findMatchForDaihyosenTx(tx state.StoreTx, compID, matchID string) (*state.MatchResult, bool, error) {
	if engine.IsPoolMatchID(matchID) {
		poolMatches, err := tx.LoadPoolMatches(compID)
		if err != nil {
			return nil, false, err
		}
		for i := range poolMatches {
			if poolMatches[i].ID == matchID {
				return &poolMatches[i], true, nil
			}
		}
		return nil, false, nil
	}
	bracket, err := tx.LoadBracket(compID)
	if err != nil {
		return nil, false, err
	}
	if bracket == nil {
		return nil, false, nil
	}
	for _, round := range bracket.Rounds {
		for i := range round {
			if round[i].ID == matchID {
				// Re-shape into MatchResult so we can drive the same
				// scoring path as pool matches. SubResults must be copied
				// so TeamSummary is computed from recorded sub-bouts and
				// the daihyosen append doesn't overwrite existing data.
				return daihyosenBracketResult(&round[i]), true, nil
			}
		}
	}
	if bm := bracket.ThirdPlaceMatch; bm != nil && bm.ID == matchID {
		return daihyosenBracketResult(bm), true, nil
	}
	return nil, false, nil
}

// daihyosenBracketResult projects a stored BracketMatch into the MatchResult
// shape the daihyosen scoring path consumes. It carries Court / ScheduledAt
// (the daihyosen append re-runs the score path, which needs the slot) on top
// of the fields the engine's bracketMatchAsResult projects, so it deliberately
// does NOT reuse that engine helper.
//
// SubResults is copied (not aliased) so an in-place append on the returned
// MatchResult can never reach into bm's backing array via shared capacity: the
// caller's own comment ("SubResults must be copied ... the daihyosen append
// doesn't overwrite existing data") depends on that being true at the source,
// not as an incidental side effect of how the one current call site happens to
// build its own copy before appending.
func daihyosenBracketResult(bm *state.BracketMatch) *state.MatchResult {
	return &state.MatchResult{
		ID:     bm.ID,
		SideA:  bm.SideA,
		SideB:  bm.SideB,
		Winner: bm.Winner,
		// SideAID/SideBID/WinnerID (bc-brid): bm may carry none (an
		// unresolved knockout match) or its stamped pairing, and a finished
		// match the organiser adds to or removes from carries its winner's
		// id too; projected faithfully either way, matching
		// bracketMatchAsResult's rule.
		SideAID:        bm.SideAID,
		SideBID:        bm.SideBID,
		WinnerID:       bm.WinnerID,
		Status:         bm.Status,
		Court:          bm.Court,
		ScheduledAt:    bm.ScheduledAt,
		Decision:       bm.Decision,
		DecisionBy:     bm.DecisionBy,
		DecisionReason: bm.DecisionReason,
		Encho:          bm.Encho,
		SubResults:     append([]state.SubMatchResult(nil), bm.SubResults...),
		// BracketMatch persists ippon arrays natively (the same shape as
		// MatchResult), so the scoreline and the judges'-decision mark
		// (an ippon entry) are direct field copies.
		IpponsA:  append([]string(nil), bm.IpponsA...),
		IpponsB:  append([]string(nil), bm.IpponsB...),
		HansokuA: bm.HansokuA,
		HansokuB: bm.HansokuB,
	}
}

// countEligibleForSidesTx reports, for EACH side of match matchID, whether
// that side can field a representative: 1 when it can, 0 when it cannot, the
// count AddDaihyosen refuses on (CHK026). A side is the participant its id
// names (in a team competition, the team), and eligibility is recorded on
// that participant, so the question is the one the start gate asks: is this
// side barred by a withdrawal another match recorded? engine.BarredSides is
// its one owner, so this reads the same answer StartMatchTx does, with the
// same two consequences:
//
//   - a side is resolved by its id only. An empty id (a bye, an unresolved
//     "Winner of ..." feeder, a legacy row not yet repaired) has no status to
//     bar it, and counts as able, as the start gate treats it;
//   - a status THIS match recorded does not count against it (the undo-path
//     exemption), so a match may be re-scored past its own withdrawal.
//
// It used to count the competition's whole roster as one number returned for
// both sides, so a side whose own team was barred still got a representative
// bout while any other entrant was eligible.
func countEligibleForSidesTx(tx state.StoreTx, compID, matchID, sideAID, sideBID string) (int, int, error) {
	statuses, err := tx.LoadCompetitorStatus(compID)
	if err != nil {
		return 0, 0, err
	}
	eligible := func(barred *domain.CompetitorStatus) int {
		if barred != nil {
			return 0
		}
		return 1
	}
	a, b := engine.BarredSides(statuses, matchID, sideAID, sideBID)
	return eligible(a), eligible(b), nil
}

// clearMatchVerdict clears a match's result so it reads as undecided: the
// winner, the decision, and the judges'-decision mark, which travels IN the
// ippons, so stripping it clears the verdict on both store branches alike
// (the bracket write copies the slices onto BracketMatch.IpponsA/B, the pool
// write stores them as the cells). MatchResult.Decision has no omitempty, so a
// decision left set would still read as decided while the match is running.
// Adding and removing a representative bout both put a match back to running.
func clearMatchVerdict(u *state.MatchResult) {
	u.Winner = ""
	u.WinnerID = ""
	u.WinnerSide = ""
	u.IpponsA = domain.StripHantei(u.IpponsA)
	u.IpponsB = domain.StripHantei(u.IpponsB)
	u.Decision = ""
	u.DecisionBy = ""
	u.DecisionReason = ""
}

// daihyosenChangedGroups is what a representative-bout add or remove changes:
// the representative bout, the groups the caller names, and the scoreline
// when clearMatchVerdict took a hantei mark out of it. Diffing u against the
// stored match is safe here, unlike on a client payload: u was built from
// that very match under the same lock (bc-mrgc).
func daihyosenChangedGroups(stored, u *state.MatchResult, groups ...string) []string {
	out := append([]string{state.BoutGroup(state.DaihyosenSubPosition)}, groups...)
	if state.GroupDiffers(stored, u, state.GroupPoints) {
		out = append(out, state.GroupPoints)
	}
	return out
}

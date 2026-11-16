package mobileapp

import (
	"fmt"
	"log"
	"net/http"

	"github.com/gin-gonic/gin"
	"github.com/gitrgoliveira/bracket-creator/internal/engine"
	"github.com/gitrgoliveira/bracket-creator/internal/state"
)

// tryAutoCompletePools runs the auto-complete check after a successful score
// write. The score itself has already been recorded, so we don't fail the
// request when the auto-complete check errors; instead we log full details
// server-side and set AutoCompleteErrorHeader to a generic sentinel so
// clients can detect the failure (and refresh) without us leaking
// internal store details. Broadcasts EventCompetitionCompleted when the
// transition actually happens, and EventCompetitionStarted for the two
// outcomes that start something (AutoCompleteStarted, where this result
// started a draw-ready competition, and AutoCompleteKnockoutStarted, where
// the last pool moved a mixed competition into its knockout).
//
// Takes the consumer-boundary interfaces (T014) so handler tests can
// stub the engine + hub without spinning up the full state/engine
// stack. Production code passes `*engine.Engine` and `*Hub` which
// satisfy the interfaces by structural match.
func tryAutoCompletePools(c *gin.Context, eng ScoringEngine, hub Broadcaster, compID string) {
	outcome, err := eng.MaybeAutoCompletePools(compID)
	answerAutoComplete(c, hub, compID, outcome, err)
}

// answerCompleteRefusedByIndividualLeagueTie answers POST .../complete for an
// individual league whose places are still tied. Such a league has no
// tie-breaker control, so the scan adds the tie-break bouts, and the scan's
// outcome words the answer: 409 saying the bouts were added only when they were,
// 409 saying they could not be when the scan failed or changed nothing, and 200
// with the competition when a racing score settled the tie and the scan finished
// the league, in which case the completion asked for has happened.
func answerCompleteRefusedByIndividualLeagueTie(c *gin.Context, eng ScoringEngine, hub Broadcaster, store interface {
	LoadCompetition(id string) (*state.Competition, error)
}, compID string, refusal gin.H) {
	outcome, err := eng.MaybeAutoCompletePools(compID)
	answerAutoComplete(c, hub, compID, outcome, err)
	switch {
	case err == nil && outcome == engine.AutoCompleteTransitioned:
		finished, lerr := store.LoadCompetition(compID)
		if lerr != nil || finished == nil {
			internalError(c, fmt.Errorf("reload finished competition %s: %v", compID, lerr))
			return
		}
		hub.Broadcast(EventTournamentUpdated, nil)
		c.JSON(http.StatusOK, finished)
	case err != nil || outcome != engine.AutoCompleteTiebreakInjected:
		c.JSON(http.StatusConflict, gin.H{"code": refusal["code"], "error": "Some places are still tied, and their tie-break bouts could not be added. Reload the page and try again."})
	default:
		c.JSON(http.StatusConflict, refusal)
	}
}

// tryAutoCompletePoolsAfterWrite is tryAutoCompletePools for a door that has
// just written the given matches (a score, a decision, a bulk batch): it asks
// engine.MaybeAutoCompletePoolsAfterWrite, which skips the knockout-status
// pool pass for a write that cannot move a qualifier, and answers the outcome
// the same way.
func tryAutoCompletePoolsAfterWrite(c *gin.Context, eng ScoringEngine, hub Broadcaster, compID string, written ...state.MatchResult) {
	outcome, err := eng.MaybeAutoCompletePoolsAfterWrite(compID, written...)
	answerAutoComplete(c, hub, compID, outcome, err)
}

// answerAutoComplete is both doors' shared tail: the sanitized error header,
// or the broadcasts the outcome calls for.
func answerAutoComplete(c *gin.Context, hub Broadcaster, compID string, outcome engine.AutoCompleteOutcome, err error) {
	if err != nil {
		log.Printf("MaybeAutoCompletePools(%s): %v", compID, err)
		c.Header(AutoCompleteErrorHeader, AutoCompleteErrorValue)
		return
	}
	switch outcome {
	case engine.AutoCompleteTransitioned:
		hub.Broadcast(EventCompetitionCompleted, gin.H{"competitionId": compID})
	case engine.AutoCompleteTiebreakInjected:
		hub.Broadcast(EventMatchUpdated, gin.H{"competitionId": compID})
		hub.Broadcast(EventScheduleUpdated, nil)
	case engine.AutoCompleteKnockoutStarted:
		// The LAST pool was just seeded → status moved pools → knockout (only
		// knockout matches remain). Tell clients to reload the now-fully-live
		// competition.
		hub.Broadcast(EventCompetitionStarted, gin.H{"competitionId": compID})
		hub.Broadcast(EventScheduleUpdated, nil)
	case engine.AutoCompleteStarted:
		// A draw-ready competition received its first result (e.g. scored from
		// the shiaijo operator view) and was started by it: same event as
		// POST .../start so every surface reloads the now-running competition.
		hub.Broadcast(EventCompetitionStarted, gin.H{"competitionId": compID})
		hub.Broadcast(EventScheduleUpdated, nil)
	case engine.AutoCompletePoolsResolved:
		// Some (not all) pools were seeded into the knockout, and/or tiebreakers
		// were injected. The bracket/schedule changed and newly-playable knockout
		// matches may now be live, refresh without a full status change.
		hub.Broadcast(EventMatchUpdated, gin.H{"competitionId": compID})
		hub.Broadcast(EventScheduleUpdated, nil)
	case engine.AutoCompleteAwaitingLeagueTiebreak:
		// All regular team-league pool matches are complete but consequential ties
		// remain, the operator must generate tie-breaker matches via the
		// league-tiebreak endpoints (Phase 3b). Broadcast both
		// EventMatchUpdated (reload standings) and EventScheduleUpdated (display
		// the "tie-breaker required" banner) as documented on AutoCompleteAwaitingLeagueTiebreak.
		hub.Broadcast(EventMatchUpdated, gin.H{"competitionId": compID})
		hub.Broadcast(EventScheduleUpdated, nil)
	}
}

package mobileapp

import (
	"errors"
	"fmt"
	"net/http"

	"github.com/gin-gonic/gin"
	"github.com/gitrgoliveira/bracket-creator/internal/engine"
	"github.com/gitrgoliveira/bracket-creator/internal/state"
)

// enforceSelfRunPolicy applies the self-run decision allowlist when the
// tournament is in self-run mode and the request's password is empty, as the
// public page sends it (selfRunAnonymous; a password sent but wrong is a 401
// there). Returns the resultSource string ("admin" or "self-reported")
// and true on success; writes the HTTP error response and returns "",
// false when the request should be rejected.
//
// Nothing that reads the stored match is checked here (the finished-match
// refusal, the representative bout's rules): that runs inside WithTransaction,
// so concurrent anonymous submissions cannot race it. See
// holdSelfReportedWriteUnderTx.
//
// Called after ScoreRequest.Validate() so the request is structurally valid.
//
// In officiated mode this is a pass-through that returns "admin", true.
// On LoadTournament error the function fails closed (500).
func enforceSelfRunPolicy(c *gin.Context, tl TournamentLoader, verifier PasswordVerifier, req *ScoreRequest, forceCourt *bool) (string, bool) {
	anonymous, ok := selfRunAnonymous(c, tl, verifier)
	if !ok {
		return "", false
	}
	if !anonymous {
		return "admin", true
	}

	// Anonymous caller in self-run mode: enforce decision allowlist on
	// the top-level decision AND every sub-result decision.
	if !IsSelfRunReportableDecision(req.Decision, (*state.MatchResult)(req).HanteiDecided()) {
		c.JSON(http.StatusBadRequest, gin.H{"error": "decision type not allowed in self-run mode without admin password"})
		return "", false
	}
	for i := range req.SubResults {
		sub := &req.SubResults[i]
		if !IsSelfRunReportableSubDecision(sub.Decision, sub.Position) {
			c.JSON(http.StatusBadRequest, gin.H{"error": fmt.Sprintf("subResults[%d]: decision type not allowed in self-run mode without admin password", i)})
			return "", false
		}
	}

	// Anonymous self-run writes are treated as unversioned: clear any client-
	// supplied rev/revSession so they don't engage the same-session rev-guard at
	// all and simply apply (last-write-wins among peers). A participant has no
	// need to order their own writes, and this keeps a crafted revSession from
	// interfering with another session's rev ordering.
	req.Rev = 0
	req.RevSession = ""
	// bc-adna: forcing a start on a busy court is the organiser's alone; a
	// participant's forceCourt is dropped, so their start is court_busy.
	*forceCourt = false
	return "self-reported", true
}

// selfRunRefusal is a refusal of a score write the handler judged under the
// write's lock: an anonymous self-run write, or the organiser's representative
// pick (landedMembersRefusal, which answers the same refusal as a
// participant's). It carries the status of the response, its code (the body's
// error) and the sentence the page shows as it is (the body's message). The
// sentence is written where the refusal is decided, so the handler that sends
// it never works the case out again.
type selfRunRefusal struct {
	status  int
	code    string
	message string
}

// respondSideMismatch answers a write that named other competitors than the
// match stores (a draw swap since the writer loaded it): 409 side_mismatch,
// the one body PUT /score, the correction door and POST /decision share.
func respondSideMismatch(c *gin.Context) {
	c.JSON(http.StatusConflict, gin.H{
		"error":   "side_mismatch",
		"message": "The submitted competitors don't match this match's pairing. Check the score sheet and try again.",
	})
}

func (r *selfRunRefusal) Error() string { return r.code }

// body is the response body: the code and the sentence.
func (r *selfRunRefusal) body() gin.H { return gin.H{"error": r.code, "message": r.message} }

// response is the refusal as a handler answers it after its transaction.
func (r *selfRunRefusal) response() *txResponse {
	return &txResponse{status: r.status, body: r.body()}
}

// resultFinalized is the refusal of an anonymous self-run write to a match
// that has finished, which only the organiser corrects. The score path, the
// representative bout's add and remove, and the score sheet's lineup write all
// send it. message is the sentence; the lineup's names what it could not
// change.
func resultFinalized(message string) *selfRunRefusal {
	return &selfRunRefusal{status: http.StatusConflict, code: "result_finalized", message: message}
}

// errResultFinalized is resultFinalized with the score path's sentence, which
// the representative bout's add and remove send too.
var errResultFinalized = resultFinalized("This match result has already been reported. Contact the tournament organizer to correct it.")

// leagueFinishedCode is the 409 a correction of a finished league's result
// answers until the organiser confirms it (warn first, then save).
const leagueFinishedCode = "league_finished"

// LeagueFinishedMessage is the refusal's sentence. The confirm the app shows
// is write_result.jsx's leagueFinishedConfirm; this one is what a queued
// correction refused on replay, with nobody there to confirm it, is told with.
const LeagueFinishedMessage = "This league has finished. Changing this result can change the final table and podium, so it was not saved: correct it again and confirm the change."

// leagueFinishedRefusal is the refusal of a correction on a finished league
// written without the request-only confirmFinished. RemovedCourts names the
// shiaijo the league's matches stand on that the tournament no longer has: if
// the correction puts the league back into match play, those matches lose
// their court (the engine's reopenLeagueTx), and the confirm says so.
type leagueFinishedRefusal struct {
	removedCourts []string
	// message replaces LeagueFinishedMessage when the write is not a result's
	// (the reset of the lots, LeagueFinishedLotsMessage).
	message string
}

func (r *leagueFinishedRefusal) Error() string { return leagueFinishedCode }

// body is the 409 body: the code, the sentence and the removed courts.
func (r *leagueFinishedRefusal) body() gin.H {
	courts := r.removedCourts
	if courts == nil {
		courts = []string{}
	}
	message := r.message
	if message == "" {
		message = LeagueFinishedMessage
	}
	return gin.H{"error": leagueFinishedCode, "message": message, "removedCourts": courts}
}

// LeagueFinishedLotsMessage is the refusal's sentence for clearing the lots of a
// finished league whose places they alone decided.
const LeagueFinishedLotsMessage = "This league has finished on the lots you entered. Clearing them puts the league back into match play, so they were not cleared. Send the request again with confirmFinished set to true to clear them."

// refuseFinishedLeagueCorrection is the ONE check every result door asks (PUT
// /score, quick-score, bulk-score, POST /decision, and the reopen doors): a
// correction of a match of a finished league is refused until the write
// carries the request-only confirmFinished (never stored). It reads the
// competition under the write's own transaction, so a league that finished an
// instant before is judged as finished. corrects is the door's own reading of
// the write as a correction that would land: a completed write over a
// completed match, neither the resend of the result already recorded nor a
// result the match's history keeps because the writer never saw the one in
// force (those land nothing, so there is nothing to warn about). Its scope is
// the reconciler's (the engine's leagueStateBeforeWrite): a pool match, a
// tie-breaker bout included, of a league, engi or not. tl may be nil (no court
// is then named). It answers nil when the write may go on; an error that is
// not a *leagueFinishedRefusal is a fault.
//
// It lives here and not in the engine's ForceOptions because `corrects` is the
// door's own reading of the request (the stamp and basis the writer sent, the
// match's score log, the reason it carried), which only the handler layer
// holds; the engine sees the write after those judgements, so one function the
// doors all ask is the one owner.
func refuseFinishedLeagueCorrection(stx state.StoreTx, tl TournamentLoader, compID, matchID string, corrects, confirmed bool) error {
	if !corrects || confirmed || !engine.IsPoolMatchID(matchID) {
		return nil
	}
	comp, err := stx.LoadCompetition(compID)
	if err != nil {
		return err
	}
	if comp == nil || comp.Format != state.CompFormatLeague || comp.Status != state.CompStatusComplete {
		return nil
	}
	courts, err := leagueRemovedCourts(stx, tl, compID)
	if err != nil {
		return err
	}
	return &leagueFinishedRefusal{removedCourts: courts}
}

// leagueRemovedCourts names, in name order, the shiaijo the competition's pool
// matches stand on that the tournament no longer has (the engine's
// reopenLeagueTx clears them when the league goes back to match play).
func leagueRemovedCourts(stx state.StoreTx, tl TournamentLoader, compID string) ([]string, error) {
	if tl == nil {
		return nil, nil
	}
	tournament, err := tl.LoadTournament()
	if err != nil {
		return nil, err
	}
	matches, err := stx.LoadPoolMatches(compID)
	if err != nil {
		return nil, err
	}
	_, courts := engine.MatchesOnRemovedCourts(matches, tournament)
	return courts, nil
}

// respondIfLeagueFinished answers a leagueFinishedRefusal with its 409 and
// reports whether it did.
func respondIfLeagueFinished(c *gin.Context, err error) bool {
	var refusal *leagueFinishedRefusal
	if !errors.As(err, &refusal) {
		return false
	}
	c.JSON(http.StatusConflict, refusal.body())
	return true
}

// refuseFinishedLeagueReopen is the reopen doors' ask of the same check
// (refuseFinishedLeagueCorrection): reopening a completed match of a finished
// league puts the league back into match play, so it is warned about first,
// unless the request carries the confirmation. A match the competition does not
// hold, or one that is not completed, is the reopen's own answer. It runs in a
// transaction of its own, read-only, before the reopen: a league that finishes
// between the two is reopened without the warning, which the organiser's tap on
// Reopen already asked for.
func refuseFinishedLeagueReopen(txr CompetitionTransactor, tl TournamentLoader, compID, matchID string, confirmed bool) error {
	if confirmed || !engine.IsPoolMatchID(matchID) {
		return nil
	}
	var refusal error
	err := txr.WithTransaction(compID, func(stx state.StoreTx) error {
		matches, err := stx.LoadPoolMatches(compID)
		if err != nil {
			return err
		}
		for i := range matches {
			if matches[i].ID == matchID {
				refusal = refuseFinishedLeagueCorrection(stx, tl, compID, matchID, matches[i].Status == state.MatchStatusCompleted, false)
				break
			}
		}
		return nil
	})
	if err != nil {
		return err
	}
	return refusal
}

// refuseFinishedLeagueLotsReset is the lots reset's ask of the same check
// (refuseFinishedLeagueCorrection): clearing the lots of a finished league that
// they alone decided puts the league back into match play, so it is warned
// about first (engine.LotsResetReopensLeague, a dry run that writes nothing),
// unless the request carries the confirmation. A league the reset leaves
// finished, and any other competition, is cleared as before. A corrupt
// overrides.json is no lots to warn about: the reset is the repair door for
// exactly that file, so the check lets it through. Like
// refuseFinishedLeagueReopen it runs in a transaction of its own: a league that
// finishes between the check and the reset is cleared without the warning.
func refuseFinishedLeagueLotsReset(txr CompetitionTransactor, tl TournamentLoader, eng LotsResetEngine, compID string, confirmed bool) error {
	if confirmed {
		return nil
	}
	var refusal error
	err := txr.WithTransaction(compID, func(stx state.StoreTx) error {
		reopens, err := eng.LotsResetReopensLeague(stx, compID)
		if errors.Is(err, state.ErrCorruptOverrides) {
			return nil
		}
		if err != nil || !reopens {
			return err
		}
		courts, err := leagueRemovedCourts(stx, tl, compID)
		if err != nil {
			return err
		}
		refusal = &leagueFinishedRefusal{removedCourts: courts, message: LeagueFinishedLotsMessage}
		return nil
	})
	if err != nil {
		return err
	}
	return refusal
}

// repBoutHanteiRefusal is the refusal of an anonymous write that would record,
// move or clear the judges' decision (hantei) on the representative bout, or
// finish the match against it. recorded says whether the organiser recorded
// one, which decides the sentence. The recorded one is also the note the public
// score sheet shows under a representative bout the judges decided
// (REP_BOUT_DECIDED_NOTE, admin_scoring_team_bouts.jsx); testdata/rep_bout_hantei_messages.json holds both
// for the two languages.
func repBoutHanteiRefusal(recorded bool) *selfRunRefusal {
	message := "Only the tournament organizer can record a judges' decision (hantei)."
	if recorded {
		message = "The judges decided this representative bout (hantei). Ask the tournament organizer to change it."
	}
	return &selfRunRefusal{status: http.StatusConflict, code: "hantei_organiser_only", message: message}
}

// errDuplicateRepBout refuses a write listing two representative-bout rows: an
// encounter has one.
var errDuplicateRepBout = &selfRunRefusal{
	status:  http.StatusBadRequest,
	code:    "duplicate_daihyosen",
	message: "A team match has one representative bout, and this score lists more than one. Check the scores and try again.",
}

// errRepBoutRemoved refuses a participant's finish that carries a
// representative bout the match does not have. A finish may take its winner
// from that row (the score sheet does when the numbered bouts are tied), so it
// is not written without it; any other write only loses the row
// (holdSelfReportedWriteUnderTx).
var errRepBoutRemoved = &selfRunRefusal{
	status:  http.StatusConflict,
	code:    "no_daihyosen",
	message: "This match's representative bout was removed on another device. Check the scores and finish again.",
}

// errRepBoutAdded refuses a participant's finish that lists the bouts without
// the representative bout the match has: it was decided on a sheet that never
// saw the row. Any other write keeps the stored row (holdSelfReportedWriteUnderTx).
var errRepBoutAdded = &selfRunRefusal{
	status:  http.StatusConflict,
	code:    "daihyosen_added",
	message: "A representative bout was added to this match on another device. Check the scores and finish again.",
}

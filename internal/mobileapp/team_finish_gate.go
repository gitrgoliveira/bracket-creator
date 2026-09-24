package mobileapp

import (
	"log"
	"strconv"
	"strings"

	"github.com/gitrgoliveira/bracket-creator/internal/domain"
	"github.com/gitrgoliveira/bracket-creator/internal/engine"
	"github.com/gitrgoliveira/bracket-creator/internal/state"
)

// teamBoutVacancies is the engine view the team finish gate needs: which
// numbered bouts no side fields a fighter for. *engine.Engine satisfies it,
// and so does every ScoringEngine.
type teamBoutVacancies interface {
	TeamBoutsWithNoFighter(compID, matchID string) (map[int]bool, error)
}

// unfinishedTeamBouts returns, in bout order, the numbered bouts 1..teamSize
// of a team match that carry no result in subs (state.SubMatchResult.
// HasResult). A bout missing from subs has no result either. The daihyosen
// row (state.DaihyosenSubPosition) is never numbered, so it is never
// returned; bouts in noBout (no fighter on either side) are skipped.
func unfinishedTeamBouts(subs []state.SubMatchResult, teamSize int, noBout map[int]bool) []int {
	played := make(map[int]bool, len(subs))
	for i := range subs {
		if subs[i].Position >= 1 && subs[i].HasResult() {
			played[subs[i].Position] = true
		}
	}
	var out []int
	for bout := 1; bout <= teamSize; bout++ {
		if !played[bout] && !noBout[bout] {
			out = append(out, bout)
		}
	}
	return out
}

// teamBoutLabel names a numbered bout for the operator: "Bout 3", plus its
// FIK position name for a five-person team ("Bout 5 (Taisho)").
func teamBoutLabel(teamSize, bout int) string {
	label := "Bout " + strconv.Itoa(bout)
	if teamSize == 5 {
		if pos, ok := domain.PositionForBout(teamSize, bout); ok {
			label += " (" + pos.Label() + ")"
		}
	}
	return label
}

// unfinishedTeamBoutsMessage is the refusal copy for finishing a team match
// while bouts have no result. It names the bouts and what to record, and
// never mentions the lineup: a vacancy is legitimate play and must not read
// as something to complete. The JS twin is unfinishedTeamBoutsMessage in
// web-mobile/js/admin_scoring_team.jsx; both are pinned to
// testdata/unfinished_team_bouts.json.
func unfinishedTeamBoutsMessage(teamSize int, bouts []int) string {
	labels := make([]string, len(bouts))
	for i, b := range bouts {
		labels[i] = teamBoutLabel(teamSize, b)
	}
	var list, verb string
	switch len(labels) {
	case 0:
		return ""
	case 1:
		list, verb = labels[0], "has"
	default:
		list, verb = strings.Join(labels[:len(labels)-1], ", ")+" and "+labels[len(labels)-1], "have"
	}
	return list + " " + verb + " no result. Record a score, a Tie, or a Fusensho before finishing."
}

// refuseUnfinishedTeamFinish is the server half of the team finish gate
// (operator ruling 2026-09-24: every bout of a team match is fought, so there
// are no unfinished team matches). It returns the ValidationError naming the
// bouts when a score write would COMPLETE a team match while a numbered bout
// has no result, and nil when the write may proceed. An error return is a
// competition that could not be read; a competition that does not exist is
// left to the engine write, which reports it.
//
// It is the half that runs BEFORE the write's transaction, because the
// lineup read behind TeamBoutsWithNoFighter goes through the store's own
// locks and would deadlock under the per-competition lock. The refusal it
// returns is only a candidate: the caller applies it inside the transaction
// through teamFinishRefusalUnderTx, which exempts the one write that depends
// on the STORED match, so that read and the write it gates happen under one
// lock (a reopen landing in between cannot slip a write past the gate).
//
// Out of scope here, each for a reason:
//   - a write that does not complete the match (autosave, Start);
//   - a whole-encounter withdrawal (kiken, fusenpai), which is not a fought
//     finish and reaches the engine with the bouts fought so far;
//   - kachinuki, which ends on the operator's explicit End match;
//   - an individual competition, and the pool daihyosen / tiebreaker rows of
//     a team competition, which are single bouts with no numbered positions.
//
// A position neither side fields has no bout and is exempt; a vacancy on one
// side only is not (the present fighter takes a fusensho). See
// engine.Engine.TeamBoutsWithNoFighter. Corrections are gated too, except the
// one teamFinishRefusalUnderTx exempts.
func refuseUnfinishedTeamFinish(store CompetitionStore, eng teamBoutVacancies, compID, matchID string, req *state.MatchResult) (*ValidationError, error) {
	if req.Status != state.MatchStatusCompleted || domain.IsWithdrawalDecisionStr(req.Decision) {
		return nil, nil
	}
	if engine.IsPoolDaihyosenMatchID(matchID) || engine.IsTiebreakerMatchID(matchID) {
		return nil, nil
	}
	comp, err := store.LoadCompetition(compID)
	if err != nil {
		return nil, err
	}
	if comp == nil || comp.TeamSize < 2 || comp.IsKachinuki() {
		return nil, nil
	}
	noBout, err := eng.TeamBoutsWithNoFighter(compID, matchID)
	if err != nil {
		// Fail closed: with no vacancy answer every numbered bout is gated,
		// which only ever asks for more results, never fewer.
		log.Printf("TeamBoutsWithNoFighter(%s, %s) for the team finish gate: %v; exempting no bout", compID, matchID, err)
		noBout = nil
	}
	bouts := unfinishedTeamBouts(req.SubResults, comp.TeamSize, noBout)
	if len(bouts) == 0 {
		return nil, nil
	}
	return &ValidationError{Message: unfinishedTeamBoutsMessage(comp.TeamSize, bouts)}, nil
}

// teamFinishRefusalUnderTx is the in-transaction half of the team finish gate:
// it returns the refusal refuseUnfinishedTeamFinish computed, unless the write
// keeps a withdrawal recorded on the match. That write is a correction to the
// bouts of a match a withdrawal already ended, and it keeps the ruling
// (engine.KeepsWithdrawalRuling, operator ruling 2026-09-24: "Save correction
// should just save what the operator enters"), so the bouts nobody fought
// after the withdrawal are not a finish it has to answer for. check is the
// snapshot applyCorrectionReasonUnderTx read under the same lock the write
// holds, so the stored decision this exemption reads is the one the engine
// will see.
func teamFinishRefusalUnderTx(refusal *ValidationError, check correctionCheck, req *state.MatchResult) *ValidationError {
	if refusal == nil || engine.KeepsWithdrawalRuling(check.StoredStatus, check.StoredDecision, req) {
		return nil
	}
	return refusal
}

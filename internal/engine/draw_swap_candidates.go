package engine

import (
	"fmt"
	"strings"

	"github.com/gitrgoliveira/bracket-creator/internal/helper"
	"github.com/gitrgoliveira/bracket-creator/internal/state"
)

// The draw review's swap candidates (bc-drev, operator decision K4 (c)):
// once the organiser picks a first competitor in the "Swap competitors" mode,
// every other competitor of the draw is marked by what swapping the two would
// do to the dojo separation. Nothing is stored; the answer is recomputed from
// the stored draw on every read, like the suggestions it sits beside.

// Swap candidate verdicts, SwapCandidate.Verdict, as judgeDojos names them:
// each dojo is judged as a whole, by where its pairs of dojo-mates can first
// meet, sorted earliest first, before and after the swap.
const (
	// SwapVerdictWorse: at least one dojo is worse off: the first of its
	// meetings that differs is earlier (sharing a pool is the earliest of
	// all), whatever another dojo gains.
	SwapVerdictWorse = "worse"
	// SwapVerdictBetter: no dojo is worse off and at least one is better off.
	SwapVerdictBetter = "better"
	// SwapVerdictSame: every dojo's meetings are what they are now.
	SwapVerdictSame = "same"
)

// SwapCandidate is one competitor the pick could swap with: their
// participant id, the place they hold now (the shape the swap request
// takes) and what the swap would do to the dojo separation: Verdict, and
// Dojos, the dojos it is about, sorted (those worse off for worse, those
// better off for better, none for the same), so a mark can say whom the swap
// harms or helps rather than read as the candidate's own dojo.
type SwapCandidate struct {
	ID      string    `json:"id"`
	Place   DrawPlace `json:"place"`
	Verdict string    `json:"verdict"`
	Dojos   []string  `json:"dojos,omitempty"`
}

// SwapCandidates is the answer of GET .../draw/swap-candidates: the pick and
// the place they hold, and every other drawn competitor, in draw order.
type SwapCandidates struct {
	Pick       SwapSide        `json:"pick"`
	Candidates []SwapCandidate `json:"candidates"`
}

// SwapCandidatesFor answers, for the competitor pickID of compID's draw, the
// verdict of swapping them with each other drawn competitor. It reads the
// draw in ONE WithTransaction, as the swap's dry run does, so the draw's
// refusals are the swap's own, judged the same way (swappableDraw:
// SwapNotSupported, SwapNoDraw, SwapDrawStarted), and an id the draw does not
// hold is SwapNotInDraw. A draw that cannot be read is an error.
func (e *Engine) SwapCandidatesFor(compID, pickID string) (*SwapCandidates, error) {
	rebuild := e.swapPreviewBuilder(compID)
	var answer *SwapCandidates
	err := e.store.WithTransaction(compID, func(tx state.StoreTx) error {
		snap, err := loadSwapSnapshot(tx, compID)
		if err != nil {
			return err
		}
		snap.rebuild = rebuild
		answer, err = swapCandidates(snap, pickID)
		return err
	})
	if err != nil {
		return nil, err
	}
	return answer, nil
}

// swapCandidates is the pure work of SwapCandidatesFor over what its
// transaction read: the pick's place and, for every other drawn competitor in
// draw order, their place and candidateVerdict.
func swapCandidates(snap swapSnapshot, pickID string) (*SwapCandidates, error) {
	source, err := swappableDraw(snap)
	if err != nil {
		return nil, err
	}
	comp := snap.comp
	summary := DojoSeparation(comp, snap.pools, snap.bracket, snap.roster)
	s := newSwapSuggester(comp, snap.pools, snap.bracket, snap.roster, summary)
	if s == nil {
		return nil, swapRefused(SwapNoDraw, "There is no draw to change yet.")
	}
	x := -1
	for i, e := range s.entrants {
		if pickID != "" && e.c.side.ID == pickID {
			x = i
			break
		}
	}
	if x < 0 {
		return nil, notInDrawRefusal(snap.roster, pickID)
	}
	ex := s.entrants[x]
	out := &SwapCandidates{
		Pick:       SwapSide{ID: ex.c.side.ID, Place: clonePlace(ex.place)},
		Candidates: make([]SwapCandidate, 0, len(s.entrants)-1),
	}
	for y, ey := range s.entrants {
		if y == x {
			continue
		}
		verdict, dojos, err := s.candidateVerdict(snap, summary, source, x, y)
		if err != nil {
			return nil, err
		}
		out.Candidates = append(out.Candidates, SwapCandidate{ID: ey.c.side.ID, Place: clonePlace(ey.place), Verdict: verdict, Dojos: dojos})
	}
	return out, nil
}

// candidateVerdict judges the swap of entrants x and y. A swap that moves a
// seed is worked out by the swap's own steps (exchangePlaces, then
// seededSwap, as planSwap does): when that rebuilds the knockout, any pair's
// meeting can move, so the whole separation after it is compared with
// summary, the separation now. Every other swap keeps the bracket's shape and
// is scored on the two competitors' own pairs (pairShifts). Both judge by
// judgeDojos, which also names the dojos the verdict is about.
func (s *swapSuggester) candidateVerdict(snap swapSnapshot, summary []DojoMeeting, source DrawSource, x, y int) (verdict string, dojos []string, err error) {
	ex, ey := s.entrants[x], s.entrants[y]
	if ex.seed > 0 || ey.seed > 0 {
		req := SwapRequest{
			A:      SwapSide{ID: ex.c.side.ID, Place: clonePlace(ex.place)},
			B:      SwapSide{ID: ey.c.side.ID, Place: clonePlace(ey.place)},
			DryRun: true,
		}
		plan := exchangePlaces(snap, req.A.ID, req.B.ID, source)
		if err := plan.seededSwap(snap, req, source); err != nil {
			return "", nil, fmt.Errorf("judging the swap of %s and %s: %w", req.A.ID, req.B.ID, err)
		}
		if plan.answer.BracketRebuilt {
			after := DojoSeparation(snap.comp, plan.pools, plan.bracket, snap.roster)
			verdict, dojos = separationVerdict(SwapSeparation{Before: summary, After: after})
			return verdict, dojos, nil
		}
	}
	_, verdict, dojos = s.pairShifts(x, y)
	return verdict, dojos, nil
}

// separationVerdict judges a swap from its separation rows before and after
// by judgeDojos, each dojo (helper.DojoKey) holding its pairs' meetings, each
// ranked by rankMeeting on its own bracket's rounds. A pair is matched
// by its two ids, which a swap never changes, and a row that is in one list
// only changes nothing: a swap keeps every pair. dojos names the dojos the
// verdict is about, each as its row spells it (judgeDojos).
func separationVerdict(sep SwapSeparation) (verdict string, dojos []string) {
	before := make(map[[2]string]DojoMeeting, len(sep.Before))
	for _, dm := range sep.Before {
		before[pairIDs(dm.A.ID, dm.B.ID)] = dm
	}
	var tallies []dojoTally
	for _, dm := range sep.After {
		was, ok := before[pairIDs(dm.A.ID, dm.B.ID)]
		if !ok {
			continue
		}
		i := tallyIndex(&tallies, helper.DojoKey(was.Dojo), strings.TrimSpace(was.Dojo))
		t := &tallies[i]
		t.before = append(t.before, rankMeeting(was))
		t.after = append(t.after, rankMeeting(dm))
	}
	verdict, _, dojos = judgeDojos(tallies)
	return verdict, dojos
}

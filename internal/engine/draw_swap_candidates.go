package engine

import (
	"fmt"

	"github.com/gitrgoliveira/bracket-creator/internal/state"
)

// The draw review's swap candidates (bc-drev, operator decision K4 (c)):
// once the organiser picks a first competitor in the "Swap competitors" mode,
// every other competitor of the draw is marked by what swapping the two would
// do to the dojo separation. Nothing is stored; the answer is recomputed from
// the stored draw on every read, like the suggestions it sits beside.

// Swap candidate verdicts, SwapCandidate.Verdict.
const (
	// SwapVerdictWorse: at least one pair of dojo-mates would first meet
	// earlier (sharing a pool is the earliest of all).
	SwapVerdictWorse = "worse"
	// SwapVerdictBetter: no pair meets earlier and at least one meets later.
	SwapVerdictBetter = "better"
	// SwapVerdictSame: every pair first meets where it does now.
	SwapVerdictSame = "same"
)

// SwapCandidate is one competitor the pick could swap with: their
// participant id, the place they hold now (the shape the swap request
// takes) and what the swap would do to the dojo separation.
type SwapCandidate struct {
	ID      string    `json:"id"`
	Place   DrawPlace `json:"place"`
	Verdict string    `json:"verdict"`
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
		verdict, err := s.candidateVerdict(snap, summary, source, x, y)
		if err != nil {
			return nil, err
		}
		out.Candidates = append(out.Candidates, SwapCandidate{ID: ey.c.side.ID, Place: clonePlace(ey.place), Verdict: verdict})
	}
	return out, nil
}

// candidateVerdict judges the swap of entrants x and y. A swap that moves a
// seed is worked out by the swap's own steps (exchangePlaces, then
// seededSwap, as planSwap does): when that rebuilds the knockout, any pair's
// meeting can move, so the whole separation after it is compared with
// summary, the separation now. Every other swap keeps the bracket's shape and
// is scored on the two competitors' own pairs (pairShifts).
func (s *swapSuggester) candidateVerdict(snap swapSnapshot, summary []DojoMeeting, source DrawSource, x, y int) (string, error) {
	ex, ey := s.entrants[x], s.entrants[y]
	if ex.seed > 0 || ey.seed > 0 {
		req := SwapRequest{
			A:      SwapSide{ID: ex.c.side.ID, Place: clonePlace(ex.place)},
			B:      SwapSide{ID: ey.c.side.ID, Place: clonePlace(ey.place)},
			DryRun: true,
		}
		plan := exchangePlaces(snap, req.A.ID, req.B.ID, source)
		if err := plan.seededSwap(snap, req, source); err != nil {
			return "", fmt.Errorf("judging the swap of %s and %s: %w", req.A.ID, req.B.ID, err)
		}
		if plan.answer.BracketRebuilt {
			after := DojoSeparation(snap.comp, plan.pools, plan.bracket, snap.roster)
			return separationVerdict(SwapSeparation{Before: summary, After: after}, bracketRounds(snap.bracket), bracketRounds(plan.bracket)), nil
		}
	}
	gained, earlier := s.pairShifts(x, y)
	return shiftVerdict(len(gained) > 0, earlier), nil
}

// shiftVerdict names a swap from what it does to the pairs it moves: worse
// when any pair meets earlier, whatever else meets later; better when some
// pair meets later; the same otherwise.
func shiftVerdict(later, earlier bool) string {
	switch {
	case earlier:
		return SwapVerdictWorse
	case later:
		return SwapVerdictBetter
	}
	return SwapVerdictSame
}

// separationVerdict judges a swap from its separation rows before and after,
// pair by pair (a pair is keyed by its two ids, which a swap never changes),
// each ranked by rankMeeting on its own bracket's round count. A row that is
// in one list only changes nothing: a swap keeps every pair.
func separationVerdict(sep SwapSeparation, roundsBefore, roundsAfter int) string {
	before := make(map[[2]string]meetingRank, len(sep.Before))
	for _, dm := range sep.Before {
		before[pairIDs(dm.A.ID, dm.B.ID)] = rankMeeting(dm, roundsBefore)
	}
	later, earlier := false, false
	for _, dm := range sep.After {
		was, ok := before[pairIDs(dm.A.ID, dm.B.ID)]
		if !ok {
			continue
		}
		switch c := rankMeeting(dm, roundsAfter).compare(was); {
		case c < 0:
			earlier = true
		case c > 0:
			later = true
		}
	}
	return shiftVerdict(later, earlier)
}

// bracketRounds is a bracket's round count, 0 for none.
func bracketRounds(b *state.Bracket) int {
	if b == nil {
		return 0
	}
	return len(b.Rounds)
}

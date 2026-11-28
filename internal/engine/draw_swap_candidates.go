package engine

import (
	"github.com/gitrgoliveira/bracket-creator/internal/state"
)

// The draw review's swap candidates (bc-drev, operator decision K4 (c)):
// once the organiser picks a first competitor in the "Swap competitors" mode,
// every other unseeded competitor of the draw is marked by what swapping the
// two would do to the dojo separation. A seed keeps its place (operator
// ruling 2026-11-27), so a seeded pick is refused SwapSeeded and no seed is a
// candidate. Nothing is stored; the answer is recomputed from the stored draw
// on every read, like the suggestions it sits beside.

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
// the place they hold, and every other unseeded drawn competitor, in draw
// order.
type SwapCandidates struct {
	Pick       SwapSide        `json:"pick"`
	Candidates []SwapCandidate `json:"candidates"`
}

// SwapCandidatesFor answers, for the competitor pickID of compID's draw, the
// verdict of swapping them with each other drawn competitor. It reads the
// draw in ONE WithTransaction, as the swap's dry run does, so the draw's
// refusals are the swap's own, judged the same way (swappableDraw:
// SwapNotSupported, SwapNoDraw, SwapDrawStarted), an id the draw does not
// hold is SwapNotInDraw and a seeded pick SwapSeeded. A draw that cannot be
// read is an error.
func (e *Engine) SwapCandidatesFor(compID, pickID string) (*SwapCandidates, error) {
	var answer *SwapCandidates
	err := e.store.WithTransaction(compID, func(tx state.StoreTx) error {
		snap, err := loadSwapSnapshot(tx, compID)
		if err != nil {
			return err
		}
		answer, err = swapCandidates(snap, pickID)
		return err
	})
	if err != nil {
		return nil, err
	}
	return answer, nil
}

// swapCandidates is the pure work of SwapCandidatesFor over what its
// transaction read: the pick's place and, for every other unseeded drawn
// competitor in draw order, their place and the swap's verdict, judged on the
// two competitors' own pairs (pairShifts, by judgeDojos, which also names the
// dojos the verdict is about): a swap moves no seed, so the bracket keeps its
// shape.
func swapCandidates(snap swapSnapshot, pickID string) (*SwapCandidates, error) {
	if _, err := swappableDraw(snap); err != nil {
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
	if err := seededRefusal(snap, pickID); err != nil {
		return nil, err
	}
	ex := s.entrants[x]
	out := &SwapCandidates{
		Pick:       SwapSide{ID: ex.c.side.ID, Place: clonePlace(ex.place)},
		Candidates: make([]SwapCandidate, 0, len(s.entrants)-1),
	}
	for y, ey := range s.entrants {
		if y == x || ey.seed > 0 {
			continue
		}
		_, verdict, dojos := s.pairShifts(x, y)
		out.Candidates = append(out.Candidates, SwapCandidate{ID: ey.c.side.ID, Place: clonePlace(ey.place), Verdict: verdict, Dojos: dojos})
	}
	return out, nil
}

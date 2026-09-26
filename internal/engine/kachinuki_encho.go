package engine

import (
	"log"

	"github.com/gitrgoliveira/bracket-creator/internal/domain"
	"github.com/gitrgoliveira/bracket-creator/internal/state"
)

// KachinukiEnchoRefusal refuses a score write that puts a NEW encho on a
// kachinuki bout that is not taisho against taisho (operator ruling
// 2026-09-25, bc-kten: "only the very last match, taisho vs taisho, may go to
// encho"). Any other tied pairing retires per the kachinuki mode in force.
// The rule itself is domain.KachinukiTaishoPairing, which the editor's
// Encho button asks too; this is its server half.
//
// Only a NEW encho is judged: a bout whose stored row already carries one is
// skipped, so an encho recorded before this rule stays loadable and every
// later write (a correction, End match) still lands. And only a pairing the
// app can PROVE is not taisho against taisho is refused: when either side
// has no lineup in force (the roster is then unknown), or anything needed
// cannot be read, the write goes through as before. A tied knockout bout has
// End match held back, so a refusal on a guess would strand the court.
//
// Callers are the score handlers, and only for a kachinuki payload that
// carries a numbered-bout encho (allowNumberedEnchoFromStore), so a write with
// no encho never pays these reads. Once an encho is on record every write of
// that encounter carries it again: those look up the stored match (a cached
// copy) and stop there, before the competition and lineup reads. They run outside the write's
// transaction, the same benign pre-write read the handler's other gates
// make. Returns a *ValidationError naming the bout and its two fighters.
func (e *Engine) KachinukiEnchoRefusal(compID, matchID string, incoming []state.SubMatchResult) error {
	parent, _, roundIdx, err := e.findTeamMatch(compID, matchID)
	if err != nil {
		log.Printf("engine.KachinukiEnchoRefusal compId=%s matchId=%s: %v; not judging the pairing", compID, matchID, err)
		return nil
	}
	if parent == nil {
		return nil
	}
	stored := make(map[int]state.SubMatchResult, len(parent.SubResults))
	for _, s := range parent.SubResults {
		stored[s.Position] = s
	}
	// The new encho, if any, first: once an encounter's encho is on record,
	// every later write of it carries that encho again and must not pay for
	// the competition and lineup reads below.
	type newEncho struct {
		pos  int
		a, b domain.BoutFighter
	}
	var judge []newEncho
	for i := range incoming {
		sub := incoming[i]
		if sub.Position < 1 || !sub.Encho.On() {
			continue
		}
		prior, had := stored[sub.Position]
		if had && prior.Encho.On() {
			continue // already on record: a correction, never refused
		}
		judge = append(judge, newEncho{
			pos: sub.Position,
			a:   boutFighterOf(sub.SideA, sub.SideAMemberID, prior.SideA, prior.SideAMemberID),
			b:   boutFighterOf(sub.SideB, sub.SideBMemberID, prior.SideB, prior.SideBMemberID),
		})
	}
	if len(judge) == 0 {
		return nil
	}
	comp, err := e.store.LoadCompetition(compID)
	if err != nil {
		log.Printf("engine.KachinukiEnchoRefusal compId=%s matchId=%s: %v; not judging the pairing", compID, matchID, err)
		return nil
	}
	if comp == nil || !comp.IsKachinuki() {
		return nil
	}
	var lineupA, lineupB *domain.TeamLineup
	lineupFor := e.lineupInForce(compID, matchID, comp, roundIdx)
	if l, ok := lineupFor(parent.SideA); ok {
		lineupA = &l
	}
	if l, ok := lineupFor(parent.SideB); ok {
		lineupB = &l
	}
	// Who each team has already put up: the bouts before the one judged, as
	// this write has them, else as stored (a running write may leave out a
	// bout it did not change).
	rows := make(map[int]state.SubMatchResult, len(stored)+len(incoming))
	for pos, s := range stored {
		rows[pos] = s
	}
	for _, s := range incoming {
		rows[s.Position] = s
	}
	foughtBefore := func(pos int) (a, b []domain.BoutFighter) {
		for p, s := range rows {
			if p < 1 || p >= pos {
				continue
			}
			prior := stored[p]
			a = append(a, boutFighterOf(s.SideA, s.SideAMemberID, prior.SideA, prior.SideAMemberID))
			b = append(b, boutFighterOf(s.SideB, s.SideBMemberID, prior.SideB, prior.SideBMemberID))
		}
		return a, b
	}
	for _, j := range judge {
		foughtA, foughtB := foughtBefore(j.pos)
		taisho, known := domain.KachinukiTaishoPairing(comp.TeamSize, lineupA, lineupB, j.a, j.b, foughtA, foughtB)
		if known && !taisho {
			return validationErrorf("bout %d: encho is only for the last bout, taisho against taisho, and this pairing is %s against %s. Record the tie instead",
				j.pos, fighterLabel(j.b, "Shiro's fighter"), fighterLabel(j.a, "Aka's fighter"))
		}
	}
	return nil
}

// boutFighterOf is one side of an incoming bout row, completed from the
// stored row at the same position: the server appended the pairing, so the
// stored row carries the member id a running write may omit. The stored id
// is borrowed only when it can belong to the same fighter (the incoming row
// names nobody, or the same name).
func boutFighterOf(name, memberID, priorName, priorMemberID string) domain.BoutFighter {
	f := domain.BoutFighter{Name: name, MemberID: memberID}
	if f.Name == "" {
		f.Name = priorName
	}
	if f.MemberID == "" && (name == "" || name == priorName) {
		f.MemberID = priorMemberID
	}
	return f
}

func fighterLabel(f domain.BoutFighter, fallback string) string {
	if f.Name != "" {
		return f.Name
	}
	return fallback
}

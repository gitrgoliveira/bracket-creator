package engine

import (
	"errors"
)

// ErrNotKachinuki is returned by KachinukiRoster for a competition whose team
// matches are not fought winner-stays-on: only a kachinuki encounter has a
// queue of fighters to read.
var ErrNotKachinuki = errors.New("not a kachinuki competition")

// ErrTeamMatchNotFound is returned by KachinukiRoster for a match id the
// competition's draw does not hold, in its pools or its bracket.
var ErrTeamMatchNotFound = errors.New("match not found")

// KachinukiRosterFighter is one fighter still in a side's queue: the member id
// when the lineup position carries one, and the name the lineup holds (empty
// for a member picked by number and not named yet). The client resolves the
// fighter's current name and team-member number from its own member list.
type KachinukiRosterFighter struct {
	Name     string `json:"name"`
	MemberID string `json:"memberId,omitempty"`
}

// KachinukiRosterSide is one side's queue. LineupFound is false when no
// lineup is in force for the side's team at this match; Remaining is then
// EMPTY, because the only other source, the bout log, knows only the fighters
// already seen and any count taken from it would be false. With a lineup,
// Remaining lists every fighter of it who has not retired, in queue order:
// index 0 is the next fighter in, so the fighter on the court now is in the
// list too (they have not retired).
type KachinukiRosterSide struct {
	LineupFound bool                     `json:"lineupFound"`
	Remaining   []KachinukiRosterFighter `json:"remaining"`
}

// KachinukiRoster is the advisory read of a kachinuki encounter's two queues.
// SideA and SideB are the match's sides as stored (SideA is Aka on the sheet).
type KachinukiRoster struct {
	SideA KachinukiRosterSide `json:"sideA"`
	SideB KachinukiRosterSide `json:"sideB"`
}

// KachinukiRoster answers, for a kachinuki team match, which fighters each
// side has left. It is ADVISORY ONLY (operator ruling 2026-09-24): kachinuki
// is operator-led, team sizes are unregulated, and a side that reads as
// exhausted may still field fighters the app has never seen, so nothing ends
// the encounter, arms End match or holds a button back on this answer.
//
// It reads through kachinukiRemainingRosterOf, the very call the advance
// (MaybeAdvanceKachinuki) appends the next pairing from, so the fighter this
// read names as next is the one the advance then appends. It writes nothing.
func (e *Engine) KachinukiRoster(compID, matchID string) (KachinukiRoster, error) {
	comp, err := e.store.LoadCompetition(compID)
	if err != nil {
		return KachinukiRoster{}, err
	}
	if !comp.IsKachinuki() {
		return KachinukiRoster{}, ErrNotKachinuki
	}
	located, err := e.findTeamMatch(compID, matchID)
	if err != nil {
		return KachinukiRoster{}, err
	}
	if located == nil || located.Result == nil {
		return KachinukiRoster{}, ErrTeamMatchNotFound
	}
	remainingA, remainingB, foundA, foundB := e.kachinukiRemainingRosterOf("engine.KachinukiRoster", compID, comp, located)
	return KachinukiRoster{
		SideA: kachinukiRosterSide(remainingA, foundA),
		SideB: kachinukiRosterSide(remainingB, foundB),
	}, nil
}

// kachinukiRosterSide shapes one side's answer: a side with no lineup in force
// reads no fighters at all (see KachinukiRosterSide), and the list is never
// nil, so the wire carries [] rather than null.
func kachinukiRosterSide(remaining []kachinukiFighter, lineupFound bool) KachinukiRosterSide {
	out := KachinukiRosterSide{LineupFound: lineupFound, Remaining: []KachinukiRosterFighter{}}
	if !lineupFound {
		return out
	}
	for _, f := range remaining {
		out.Remaining = append(out.Remaining, KachinukiRosterFighter(f))
	}
	return out
}

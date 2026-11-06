package engine

import (
	"errors"
	"fmt"
	"sort"
	"time"

	"github.com/gitrgoliveira/bracket-creator/internal/state"
)

// Re-timing a drawn competition (DR1, DR2, DR3). When the organiser changes
// the date or the start time of a competition that is already drawn, its
// matches move with it: every pool, league, bracket and bronze match is
// re-slotted by the draw's own assigners at the new day and start, in the
// order they are stored, so pairings, courts and ids never change and only
// ScheduledAt does. The transaction body calls no public Store method (they
// would deadlock); what it needs from the store (the tournament and the
// court floors) is read by the caller beforehand, see ReadRetimeInputs.

var (
	// ErrRetimeNotDrawReady is returned for a competition that is not in the
	// draw-ready state: a started competition is never re-timed.
	ErrRetimeNotDrawReady = errors.New("only a competition that is drawn and not started can be re-timed")
	// ErrRetimeUnavailable is returned when a league's stored match order
	// cannot be read back as time slots. Nothing is written.
	ErrRetimeUnavailable = errors.New("this competition's match order could not be read back, so its matches were not moved")
	// ErrUndoStale is returned by a restore when a listed match no longer
	// holds the time the move gave it. Nothing is written.
	ErrUndoStale = errors.New("changed since the move; undo not applied")
	// ErrRestoreUnknownMatch is returned by a restore that names a match the
	// competition does not hold. Nothing is written.
	ErrRestoreUnknownMatch = errors.New("restore names a match this competition does not hold")
	// ErrRestoreBadTime is returned by a restore whose time to put back is
	// neither empty nor an "HH:MM" clock. Nothing is written.
	ErrRestoreBadTime = errors.New("restore names a match time that is not HH:MM")
	// ErrRetimePastMidnight is returned when the new day and start would carry a
	// match past midnight: a match time is a bare "HH:MM", so it would read as
	// an early-morning time and sort first in the court queue. Nothing is
	// written.
	ErrRetimePastMidnight = errors.New("the new day and start would run some matches past midnight")
)

// MatchTimeChange is one match whose ScheduledAt a re-time changed. Was and
// Now are ScheduledAt strings.
type MatchTimeChange struct {
	MatchID string `json:"matchId"`
	Was     string `json:"was"`
	Now     string `json:"now"`
}

// RetimeCourt is one shiaijo's span before and after a re-time, in minutes
// from midnight (an end can be 1440 or more for a finish past midnight).
type RetimeCourt struct {
	Court    string `json:"court"`
	OldStart int    `json:"oldStart"`
	OldEnd   int    `json:"oldEnd"`
	NewStart int    `json:"newStart"`
	NewEnd   int    `json:"newEnd"`
}

// RetimeOutcome is what a re-time, or its dry run, found.
type RetimeOutcome struct {
	// AnyChange is true when at least one match time differs.
	AnyChange bool `json:"anyChange"`
	// MatchCount is the number of matches still to be played in the draw.
	MatchCount int `json:"matchCount"`
	// NotFreshCount is the number of matches whose stored time differs from
	// what a re-time at the competition's CURRENT day and start would give:
	// a time set by hand, a pause added or changed, or another competition
	// moved since the draw (DR2: nothing is stored to tell them apart).
	NotFreshCount int `json:"notFreshCount"`
	// Courts holds one span per shiaijo that has a match to play, sorted by
	// label.
	Courts []RetimeCourt `json:"courts"`
	// Changes lists every match whose time changed, with its time before and
	// after. The save carries it back as the exact Undo (DR3).
	Changes []MatchTimeChange `json:"-"`
}

// RetimeInputs is what a re-time reads from the store. It is read BEFORE the
// store lock is taken, because a transaction may not call a public Store
// method.
type RetimeInputs struct {
	Tournament *state.Tournament
	// OldFloor is the court floor for the competition as it is stored now
	// (its current day, start and shiaijo). It is used only to count the
	// matches that no longer hold a fresh time.
	OldFloor map[string]time.Time
	// NewFloor is the court floor for the competition as it will be (the new
	// day, start and shiaijo).
	NewFloor map[string]time.Time
}

// ReadRetimeInputs reads the tournament and both court floors for a re-time
// of oldComp (as stored) into newComp (as it will be).
func (e *Engine) ReadRetimeInputs(oldComp, newComp *state.Competition) (RetimeInputs, error) {
	tournament, err := e.store.LoadTournament()
	if err != nil {
		return RetimeInputs{}, err
	}
	oldFloor, err := e.courtFloorFor(oldComp, tournament)
	if err != nil {
		return RetimeInputs{}, err
	}
	newFloor, err := e.courtFloorFor(newComp, tournament)
	if err != nil {
		return RetimeInputs{}, err
	}
	return RetimeInputs{Tournament: tournament, OldFloor: oldFloor, NewFloor: newFloor}, nil
}

// RetimePreview is the dry run of RetimeDrawTx: what a save would change,
// with nothing written.
func (e *Engine) RetimePreview(oldComp, newComp *state.Competition) (RetimeOutcome, error) {
	in, err := e.ReadRetimeInputs(oldComp, newComp)
	if err != nil {
		return RetimeOutcome{}, err
	}
	pool, err := e.store.LoadPoolMatches(oldComp.ID)
	if err != nil {
		return RetimeOutcome{}, err
	}
	var bracket *state.Bracket
	if newComp.IsKnockoutEnabled() {
		if bracket, err = e.store.LoadBracket(oldComp.ID); err != nil {
			return RetimeOutcome{}, err
		}
	}
	_, _, out, err := planRetime(pool, bracket, oldComp, newComp, in)
	return out, err
}

// RetimeDrawTx re-times the drawn competition's matches from oldComp's day and
// start to newComp's, inside the caller's transaction, and saves them only
// when a time changed. Ids, pairings, courts and order are never touched.
func RetimeDrawTx(tx state.StoreTx, oldComp, newComp *state.Competition, in RetimeInputs) (RetimeOutcome, error) {
	pool, err := tx.LoadPoolMatches(newComp.ID)
	if err != nil {
		return RetimeOutcome{}, err
	}
	var bracket *state.Bracket
	if newComp.IsKnockoutEnabled() {
		if bracket, err = tx.LoadBracket(newComp.ID); err != nil {
			return RetimeOutcome{}, err
		}
	}
	newPool, newBracket, out, err := planRetime(pool, bracket, oldComp, newComp, in)
	if err != nil {
		return RetimeOutcome{}, err
	}
	if !out.AnyChange {
		return out, nil
	}
	// A file the draw never made (no pool matches in a knockout, no bracket in a
	// league) is not created by a re-time.
	if len(newPool) > 0 {
		if err := tx.SavePoolMatches(newComp.ID, newPool); err != nil {
			return RetimeOutcome{}, err
		}
	}
	if newBracket != nil {
		if err := tx.SaveBracket(newComp.ID, newBracket); err != nil {
			return RetimeOutcome{}, err
		}
	}
	return out, nil
}

// RestoreMatchTimesTx puts back the times a move changed (the exact Undo,
// DR3). It checks every listed match first: each must exist, still hold the
// time the move gave it, and have a time to put back that is empty or an
// "HH:MM" clock, else nothing is written.
func RestoreMatchTimesTx(tx state.StoreTx, comp *state.Competition, restore []MatchTimeChange) error {
	if comp.Status != state.CompStatusDrawReady {
		return ErrRetimeNotDrawReady
	}
	if len(restore) == 0 {
		return nil
	}
	pool, err := tx.LoadPoolMatches(comp.ID)
	if err != nil {
		return err
	}
	var bracket *state.Bracket
	if comp.IsKnockoutEnabled() {
		if bracket, err = tx.LoadBracket(comp.ID); err != nil {
			return err
		}
	}
	slots := map[string]*string{}
	for i := range pool {
		slots[pool[i].ID] = &pool[i].ScheduledAt
	}
	if bracket != nil {
		for r := range bracket.Rounds {
			for i := range bracket.Rounds[r] {
				slots[bracket.Rounds[r][i].ID] = &bracket.Rounds[r][i].ScheduledAt
			}
		}
		if bracket.ThirdPlaceMatch != nil {
			slots[bracket.ThirdPlaceMatch.ID] = &bracket.ThirdPlaceMatch.ScheduledAt
		}
	}
	for _, ch := range restore {
		at, ok := slots[ch.MatchID]
		if !ok {
			return fmt.Errorf("%w: %q", ErrRestoreUnknownMatch, ch.MatchID)
		}
		if *at != ch.Now {
			return ErrUndoStale
		}
		if _, ok := state.ParseClockMinutes(ch.Was); ch.Was != "" && !ok {
			return fmt.Errorf("%w: %q", ErrRestoreBadTime, ch.Was)
		}
	}
	for _, ch := range restore {
		*slots[ch.MatchID] = ch.Was
	}
	if len(pool) > 0 {
		if err := tx.SavePoolMatches(comp.ID, pool); err != nil {
			return err
		}
	}
	if bracket != nil {
		return tx.SaveBracket(comp.ID, bracket)
	}
	return nil
}

// planRetime is the one body behind the dry run and the save. It works on
// copies of what it is given and returns the re-timed pool matches and
// bracket (nil when there is none) with the outcome.
func planRetime(pool []state.MatchResult, bracket *state.Bracket, oldComp, newComp *state.Competition, in RetimeInputs) ([]state.MatchResult, *state.Bracket, RetimeOutcome, error) {
	if oldComp.Status != state.CompStatusDrawReady || newComp.Status != state.CompStatusDrawReady {
		return nil, nil, RetimeOutcome{}, ErrRetimeNotDrawReady
	}
	newPool, newBracket, newWraps, err := freshTimes(pool, bracket, newComp, in.Tournament, in.NewFloor)
	if err != nil {
		return nil, nil, RetimeOutcome{}, err
	}
	oldPool, oldBracket, _, err := freshTimes(pool, bracket, oldComp, in.Tournament, in.OldFloor)
	if err != nil {
		return nil, nil, RetimeOutcome{}, err
	}

	stored := scheduledRows(pool, bracket)
	moved := scheduledRows(newPool, newBracket)
	if newWraps || wrapsPastMidnight(moved, newComp) {
		return nil, nil, RetimeOutcome{}, ErrRetimePastMidnight
	}
	fresh := scheduledRows(oldPool, oldBracket)

	var out RetimeOutcome
	for i := range stored {
		if stored[i].at != moved[i].at {
			out.Changes = append(out.Changes, MatchTimeChange{MatchID: stored[i].id, Was: stored[i].at, Now: moved[i].at})
		}
		if stored[i].at != fresh[i].at {
			out.NotFreshCount++
		}
	}
	out.AnyChange = len(out.Changes) > 0
	poolMin, knockoutMin := SlotMinutes(newComp, in.Tournament)
	out.MatchCount, out.Courts = retimeCourts(stored, moved, poolMin, knockoutMin)
	return newPool, newBracket, out, nil
}

// freshTimes copies the stored matches and gives the copies the times a draw
// at comp's day and start would give them, by the draw's own assigners and in
// the draw's own order, on the shiaijo the draw used (InheritedDrawCourts, as
// runDrawPipeline resolves them). wraps reports whether some match would start
// on the next day: the assigners' cursors keep the day a bare "HH:MM" loses.
func freshTimes(pool []state.MatchResult, bracket *state.Bracket, comp *state.Competition, tournament *state.Tournament, floor map[string]time.Time) (newPool []state.MatchResult, newBracket *state.Bracket, wraps bool, err error) {
	drawn := *comp
	drawn.Courts = InheritedDrawCourts(comp.Courts, tournament)
	comp = &drawn
	dayStart, _ := parseCeremonyParams(comp, tournament)
	nextDay := time.Date(dayStart.Year(), dayStart.Month(), dayStart.Day()+1, 0, 0, 0, 0, dayStart.Location())
	// Every assigner returns the cursor after its last slot: a court's last
	// match starts one slot before it.
	startsNextDay := func(end time.Time, knockout bool) bool {
		slot := time.Duration(perMatchElapsedMinutes(comp, tournament, knockout)) * time.Minute
		return !end.Add(-slot).Before(nextDay)
	}

	newPool = append([]state.MatchResult(nil), pool...)
	var poolEnd time.Time
	if comp.Format == state.CompFormatLeague {
		slots, err := recoverLeagueSlots(newPool, comp.Courts)
		if err != nil {
			return nil, nil, false, err
		}
		newPool, poolEnd = assignLeagueSlotTimesFrom(newPool, slots, comp, tournament, floor)
	} else {
		newPool, poolEnd = assignPoolMatchSlotsFrom(newPool, comp, tournament, floor)
	}
	wraps = len(newPool) > 0 && startsNextDay(poolEnd, false)
	if bracket == nil || len(bracket.Rounds) == 0 {
		return newPool, nil, wraps, nil
	}
	newBracket = copyBracketForRetime(bracket)
	bracketEnd := assignBracketMatchSlots(newBracket.Rounds, comp, tournament, knockoutCourtStart(newPool, comp, tournament, floor))
	wraps = wraps || startsNextDay(bracketEnd, true)
	if newBracket.ThirdPlaceMatch != nil {
		scheduleBronze(newBracket, comp, tournament)
	}
	return newPool, newBracket, wraps, nil
}

// copyBracketForRetime copies the parts of a bracket a re-time writes (the
// matches' ScheduledAt) so the original stays as it was.
func copyBracketForRetime(b *state.Bracket) *state.Bracket {
	cp := *b
	cp.Rounds = make([][]state.BracketMatch, len(b.Rounds))
	for r := range b.Rounds {
		cp.Rounds[r] = append([]state.BracketMatch(nil), b.Rounds[r]...)
	}
	if b.ThirdPlaceMatch != nil {
		bronze := *b.ThirdPlaceMatch
		cp.ThirdPlaceMatch = &bronze
	}
	return &cp
}

// scheduledRow is a match's id, court, time and whether it is still to be
// played, in the order the store keeps them: pool matches, bracket rounds,
// then the bronze match.
type scheduledRow struct {
	id        string
	court     string
	at        string
	toPlay    bool
	inBracket bool
}

// wrapsPastMidnight reports whether a re-timed match reads earlier than the
// day's first possible start. The assigners only move a time forward from that
// start, so such a time is one that ran past 24:00 and wrapped to "00:xx".
func wrapsPastMidnight(rows []scheduledRow, comp *state.Competition) bool {
	dayStart, _ := parseCeremonyParams(comp, nil)
	first := dayStart.Hour()*60 + dayStart.Minute()
	for _, r := range rows {
		if mins, ok := state.ParseClockMinutes(r.at); ok && mins < first {
			return true
		}
	}
	return false
}

func scheduledRows(pool []state.MatchResult, bracket *state.Bracket) []scheduledRow {
	rows := make([]scheduledRow, 0, len(pool))
	for _, m := range pool {
		rows = append(rows, scheduledRow{id: m.ID, court: m.Court, at: m.ScheduledAt, toPlay: m.Status == state.MatchStatusScheduled})
	}
	if bracket != nil {
		for _, round := range bracket.Rounds {
			for _, m := range round {
				rows = append(rows, scheduledRow{id: m.ID, court: m.Court, at: m.ScheduledAt, toPlay: m.Status == state.MatchStatusScheduled, inBracket: true})
			}
		}
		if m := bracket.ThirdPlaceMatch; m != nil {
			rows = append(rows, scheduledRow{id: m.ID, court: m.Court, at: m.ScheduledAt, toPlay: m.Status == state.MatchStatusScheduled, inBracket: true})
		}
	}
	return rows
}

// retimeCourts counts the matches still to be played and gives each shiaijo's
// span before (old) and after (moved), in minutes from midnight. old and moved
// list the same matches in the same order.
func retimeCourts(old, moved []scheduledRow, poolMin, knockoutMin int) (int, []RetimeCourt) {
	type span struct {
		set              bool
		oldStart, oldEnd int
		newStart, newEnd int
	}
	spans := map[string]*span{}
	count := 0
	for i := range old {
		if !old[i].toPlay {
			continue
		}
		before, okBefore := state.ParseClockMinutes(old[i].at)
		after, okAfter := state.ParseClockMinutes(moved[i].at)
		if !okBefore || !okAfter {
			continue
		}
		count++
		length := poolMin
		if old[i].inBracket {
			length = knockoutMin
		}
		s := spans[old[i].court]
		if s == nil {
			s = &span{oldStart: before, oldEnd: before + length, newStart: after, newEnd: after + length, set: true}
			spans[old[i].court] = s
			continue
		}
		s.oldStart = min(s.oldStart, before)
		s.oldEnd = max(s.oldEnd, before+length)
		s.newStart = min(s.newStart, after)
		s.newEnd = max(s.newEnd, after+length)
	}
	courts := make([]RetimeCourt, 0, len(spans))
	for court, s := range spans {
		courts = append(courts, RetimeCourt{Court: court, OldStart: s.oldStart, OldEnd: s.oldEnd, NewStart: s.newStart, NewEnd: s.newEnd})
	}
	sort.Slice(courts, func(i, j int) bool { return courts[i].Court < courts[j].Court })
	return count, courts
}

// recoverLeagueSlots reads the time slots of a stored league back from its
// matches, which are kept in playing order. scheduleLeagueSlots fills a slot
// from the first shiaijo on, so a new slot starts wherever the shiaijo index
// does not rise; and it leaves one slot empty exactly where the next match
// would put a player straight back in, so one idle slot sits wherever two
// consecutive filled slots share a player. The result is checked against the
// invariants the scheduler guarantees: every slot fills the shiaijo in order
// from the first (so a match the operator moved to another shiaijo after the
// draw breaks the pattern and the slot boundaries cannot be trusted), every
// side carries a participant id, and no player appears twice in a slot or in
// two adjacent ones. If they do not hold, nothing can be safely re-timed and
// ErrRetimeUnavailable is returned.
func recoverLeagueSlots(matches []state.MatchResult, courts []string) ([]int, error) {
	if len(courts) == 0 {
		courts = []string{""}
	}
	courtIndex := make(map[string]int, len(courts))
	for i, c := range courts {
		courtIndex[c] = i
	}
	slots := make([]int, len(matches))
	slot := 0
	prevIdx := -1
	var prevPlayers map[string]bool // the filled slot before this one
	curPlayers := map[string]bool{}
	adjacent := false // this slot directly follows prevPlayers' slot, with no idle slot between
	for i, m := range matches {
		idx, ok := courtIndex[m.Court]
		if !ok {
			return nil, ErrRetimeUnavailable
		}
		newSlot := i > 0 && idx <= prevIdx
		if newSlot {
			prevPlayers, curPlayers = curPlayers, map[string]bool{}
			slot++
			adjacent = true
			if sharesPlayer(prevPlayers, m) {
				slot++
				adjacent = false
			}
		}
		wantIdx := prevIdx + 1 // the next shiaijo in the slot
		if i == 0 || newSlot {
			wantIdx = 0 // a slot fills from the first shiaijo
		}
		if idx != wantIdx {
			return nil, ErrRetimeUnavailable
		}
		if adjacent && sharesPlayer(prevPlayers, m) {
			return nil, ErrRetimeUnavailable
		}
		for _, id := range []string{m.SideAID, m.SideBID} {
			if id == "" {
				return nil, ErrRetimeUnavailable
			}
			if curPlayers[id] {
				return nil, ErrRetimeUnavailable
			}
			curPlayers[id] = true
		}
		slots[i] = slot
		prevIdx = idx
	}
	return slots, nil
}

// sharesPlayer reports whether m has a side (by participant id) in players. A
// side with no id never counts as shared.
func sharesPlayer(players map[string]bool, m state.MatchResult) bool {
	return (m.SideAID != "" && players[m.SideAID]) || (m.SideBID != "" && players[m.SideBID])
}

package engine

// kachinuki_opener.go owns who fights a kachinuki encounter's first bout
// (operator decision K1 (c), 2026-10-18: "Whatever the lineup says now", with
// "Lineups are stuck per match as well. So a later lineup edit would have to be
// editing the lineup specifically for that match").
//
// The one rule (openerDraw.opener): each side's bout-1 fighter is the first
// fighter, in fighting order (domain.TeamLineup.OrderedMembers, which is how
// the advance and the roster read who fights first), of that match's lineup:
//   - its own lineup: the one an operator saved for it, else the one it
//     started with, which the server keeps as its own (its pin);
//   - else, while it has not started, the lineup in force there by the carry
//     rule (LineupInForce);
//   - else nobody: a match that started with no lineup kept (before this
//     rule, or with its own lineup taken away since) keeps the fighters bout 1
//     holds, and only a save of its own lineup renames it.
//
// No device names them: a tablet's taps on bout 1 are scoring only, so two
// tablets striking on bout 1 at once both land there.
//
// The pin (pinStartLineups) is taken once, as the match leaves scheduled, in
// the transaction of the write that moved it and only when that write stands
// (afterMatchWrite): a copy of the lineup in force then, an empty one when
// there was none, marked with the start's stamp (StartedAt) and the lineup it
// was taken from (StartedFrom). It is the match's alone: a later match never
// carries it (lineupRule.inForce), so an edit of the starting lineup still
// reaches every match that has not started. An operator's save of the match's
// own lineup replaces it. A change to the lineup it was taken from, stamped
// before the start, reaches it whenever it arrives, unless a later-stamped
// change to that position is already there (FollowLineupSave): order is by
// stamp, never arrival.
//
// The server seats the fighters with events of its own: bout.fighter events in
// the match's score log, under the session ServerEventSession, which no client
// may send (RecordScoreEventsTx refuses it), ordered by their stamp like every
// tap. They govern no group, turn no tap away and name who fought, never a
// score or a result, so they pass the closed-match rule: a finished match is
// renamed by a save of its own lineup, its points, result and team figures
// staying put. They are made only where the rule's answer differs from what
// bout 1 holds, so a reseat that finds nothing to change writes nothing, at
// three points, all through openerSeatEventsTx: the score-events door's own
// batch, after every other door's write (seatOpenerAfterWrite), and after a
// lineup or team-member write, for the team's matches (ReseatKachinukiOpeners).
//
// The winner stays on: when bout 1 is renamed, each later bout of that side's
// winning streak from it whose fighter on that side is the one bout 1 held is
// renamed with it, by an event that names the fighter it replaces (Target), so
// a pick made there before it stands, whenever it arrives (operator ruling
// 2026-09-26: the app records what happens and never decides how a match is
// run).

import (
	"cmp"
	"errors"
	"fmt"
	"log"
	"slices"
	"strings"

	"github.com/gitrgoliveira/bracket-creator/internal/domain"
	"github.com/gitrgoliveira/bracket-creator/internal/state"
)

// ServerEventSession is the event session of the events the server makes
// itself. No client may send it: the score-events door refuses it, and
// RecordScoreEventsTx refuses a request carrying it that the server did not
// make (ScoreEventsRequest.server, ErrServerEventSession), so a line of the
// log under it is always the server's. Its events carry no seq, so no
// device's gaps or seqs (ScoreLogGaps, ScoreLogEventSeqs) count them.
const ServerEventSession = "server"

// ErrServerEventSession refuses a score-events request under the server's own
// session that the server did not make.
var ErrServerEventSession = errors.New("the event session \"server\" is the server's own")

// boutFighter is one side's fighter on a bout row: a name, a member id, or
// both (a member with no name yet carries the id alone).
type boutFighter struct {
	Name     string
	MemberID string
}

func (f boutFighter) named() bool { return f.Name != "" || f.MemberID != "" }

// same is whether two fighters are one: by member id when either carries one,
// else by name.
func (f boutFighter) same(g boutFighter) bool {
	if f.MemberID != "" || g.MemberID != "" {
		return f.MemberID == g.MemberID
	}
	return f.Name == g.Name
}

// KachinukiOpener is each side's first fighter; a side the lineup names none
// for is left as bout 1 holds it.
type KachinukiOpener struct {
	A, B boutFighter
}

// sideFighter is the fighter row holds on side.
func sideFighter(row state.SubMatchResult, side domain.MatchSide) boutFighter {
	if side == domain.MatchSideB {
		return boutFighter{Name: row.SideB, MemberID: row.SideBMemberID}
	}
	return boutFighter{Name: row.SideA, MemberID: row.SideAMemberID}
}

// openerSeatEvents is the bout.fighter events, stamped stamp, that seat o on
// m's first numbered bout and rename, along each side's winning streak from
// it, every later bout whose fighter on that side is the one bout 1 held. None
// when bout 1 already holds o, or the match holds no bout.
func openerSeatEvents(m *state.MatchResult, o KachinukiOpener, stamp int64) []domain.ScoreEvent {
	var idx []int
	for i := range m.SubResults {
		if m.SubResults[i].Position > 0 {
			idx = append(idx, i)
		}
	}
	if len(idx) == 0 {
		return nil
	}
	slices.SortFunc(idx, func(a, b int) int { return cmp.Compare(m.SubResults[a].Position, m.SubResults[b].Position) })
	var evs []domain.ScoreEvent
	for _, side := range []domain.MatchSide{domain.MatchSideA, domain.MatchSideB} {
		f, letter := o.A, domain.ScoreSideA
		if side == domain.MatchSideB {
			f, letter = o.B, domain.ScoreSideB
		}
		old := sideFighter(m.SubResults[idx[0]], side)
		if !f.named() || old == f {
			continue
		}
		for k, i := range idx {
			row := m.SubResults[i]
			if k > 0 {
				prev := m.SubResults[idx[k-1]]
				if state.SubBoutWinnerSide(prev, m.SideA, m.SideB) != side || !sideFighter(row, side).same(old) {
					break
				}
			}
			if sideFighter(row, side) == f {
				continue
			}
			ev := domain.ScoreEvent{
				ID:      fmt.Sprintf("srv:%d:%d:%s", stamp, row.Position, letter),
				Session: ServerEventSession, Stamp: stamp,
				Kind: domain.ScoreBoutFighter, Pos: row.Position, Side: letter,
				Name: f.Name, MemberID: f.MemberID,
			}
			if k > 0 {
				// The stayer is carried on only while the bout holds the fighter
				// bout 1 held: a pick stamped before this event, which may
				// reach the server after it, stands (foldBoutRow).
				ev.Target = fighterTarget(old)
			}
			evs = append(evs, ev)
		}
	}
	return evs
}

// openerDraw is what the lineup rule reads: the lineups and the draw.
type openerDraw struct {
	comp    *state.Competition
	lineups map[string]domain.TeamLineup
	pool    []state.MatchResult
	bracket *state.Bracket
}

// readOpenerDraw reads, in the transaction, what the opener of a kachinuki
// match is worked out from. ok is false when there is nothing to work it out
// from (not a kachinuki competition, or lineups.yaml unreadable, which is
// logged): bout 1 then keeps its fighters and no pin is taken.
func readOpenerDraw(h state.StoreTx, comp *state.Competition, compID string) (openerDraw, bool) {
	if comp == nil || !comp.IsKachinuki() {
		return openerDraw{}, false
	}
	lineups, err := h.LoadTeamLineups(compID)
	if err != nil {
		log.Printf("engine: kachinuki opener compId=%s: lineups.yaml load error: %v; bout 1 keeps its fighters", compID, err)
		return openerDraw{}, false
	}
	d := openerDraw{comp: comp, lineups: lineups}
	if d.pool, err = h.LoadPoolMatches(compID); err != nil {
		log.Printf("engine: kachinuki opener compId=%s: pool matches load error: %v; the draw is built from the files that loaded", compID, err)
	}
	if d.bracket, err = h.LoadBracket(compID); err != nil {
		log.Printf("engine: kachinuki opener compId=%s: bracket load error: %v; the draw is built from the files that loaded", compID, err)
	}
	return d, true
}

// inForce is the lineup rule over what d read.
func (d openerDraw) inForce(teamID, matchID string) InForceLineup {
	return LineupInForceFrom(d.lineups, d.pool, d.bracket, d.comp.IsKnockoutEnabled(), teamID, matchID)
}

// opener is THE rule for bout 1's fighters (see the file comment): the first
// fighter of the match's own lineup (saved, or the one it started with), else,
// while the match has not started, of the lineup in force there; else nobody,
// which renames nothing. A match sent back to the queue holding its bouts has
// started (hasStarted): it never follows the carried lineup.
func (d openerDraw) opener(matchID string, started bool, sideAID, sideBID string) KachinukiOpener {
	first := func(teamID string) boutFighter {
		if teamID == "" {
			return boutFighter{}
		}
		in := d.inForce(teamID, matchID)
		if !in.Found {
			return boutFighter{}
		}
		if in.Source.MatchID != matchID && started {
			// Started, with nothing of its own: nothing renames it.
			return boutFighter{}
		}
		slots := in.Lineup.OrderedMembers(d.comp.TeamSize)
		if len(slots) == 0 {
			return boutFighter{}
		}
		return boutFighter{Name: slots[0].Name, MemberID: slots[0].MemberID}
	}
	return KachinukiOpener{A: first(sideAID), B: first(sideBID)}
}

// ownLineup reports whether a lineup is stored for teamID at matchID itself:
// one an operator saved, or the one the match started with.
func ownLineup(lineups map[string]domain.TeamLineup, teamID, matchID string) bool {
	for _, l := range lineups {
		if l.TeamID == teamID && l.MatchID == matchID {
			return true
		}
	}
	return false
}

// startStamp is the stamp of the write that moved m out of scheduled: the
// stamp its result group took (the start names the result group alone), else
// the match's, else the server's now.
func startStamp(m *state.MatchResult) int64 {
	if s := m.GroupStamps[state.GroupResult]; s > 0 {
		return s
	}
	if m.ModifiedAt > 0 {
		return m.ModifiedAt
	}
	return serverNowMs()
}

// pinStartLineups keeps, for each side of a kachinuki match that has just left
// scheduled, the lineup in force there as the lineup it started with (its pin):
// a copy marked with the start's stamp and the lineup it came from, an empty
// one when the team had none, so a save stamped before the start that arrives
// after it still has a lineup to reach (FollowLineupSave). A side with a
// lineup of its own already (an operator saved one before the start, or the
// match started before and kept its pin) keeps it. Staged in h, so it commits
// or aborts with the write that started the match; a pin the store cannot
// stage fails that write, rather than leave a started match silently without
// the lineup it started with.
func pinStartLineups(h state.StoreTx, d *openerDraw, compID string, m *state.MatchResult) error {
	at := startStamp(m)
	for _, teamID := range []string{m.SideAID, m.SideBID} {
		if teamID == "" || ownLineup(d.lineups, teamID, m.ID) {
			continue
		}
		if err := pinLineup(h, d, compID, m.ID, teamID, at); err != nil {
			return err
		}
	}
	return nil
}

// pinLineup keeps the lineup in force for teamID at matchID now as the lineup
// the match started with (its pin), marked at: a copy of it, or an empty one
// when the team has none, and the lineup it came from (StartedFrom). Positions
// the domain would refuse to store (a key the team size does not hold, a
// member at two positions in a lineup saved before that was refused) are left
// out of the copy, the second place of a member keeping its name without the
// id, so the pin always stores; what still fails is the store's own fault and
// is the caller's error. The pin is the match's own from then on, d included.
func pinLineup(h state.StoreTx, d *openerDraw, compID, matchID, teamID string, at int64) error {
	// The carry rule at the start, by stamp (lineup_carry.go): a change made
	// after the start is not in it, whenever it arrived, and neither is a
	// removal made after it (the removed lineups' records are read too).
	all, err := h.LoadTeamLineupsWithRemoved(compID)
	if err != nil {
		return fmt.Errorf("kachinuki match %s/%s: the lineups team %s started with were not readable: %w", compID, matchID, teamID, err)
	}
	// A removal of the match's own lineup made after at, that arrived before
	// the start: in stamp order the match started, then had its own lineup
	// taken away on a started match, which keeps the lineup in force at that
	// removal as its own (keepSnapshotLineup). So the pin is that one (stage 6
	// review 8, m1).
	for _, l := range all {
		if l.TeamID == teamID && l.MatchID == matchID && l.IsRemoved() && l.RemovedAt > at {
			at = l.RemovedAt
		}
	}
	pin := domain.TeamLineup{
		TeamID: teamID, CompetitionID: compID, MatchID: matchID,
		Positions: map[domain.Position]string{}, StartedAt: at,
	}
	lc := newLineupCarry(all, d.pool, d.bracket, d.comp.IsKnockoutEnabled(), d.comp.TeamSize)
	pin.StartedFrom = lc.carryInto(&pin, at)
	if err := h.SetTeamLineup(compID, pin, d.comp.TeamSize); err != nil {
		return fmt.Errorf("kachinuki match %s/%s: the lineup team %s started with was not kept: %w", compID, matchID, teamID, err)
	}
	if fresh, err := h.LoadTeamLineups(compID); err == nil {
		d.lineups = fresh
	}
	return nil
}

// openerSeatEventsTx is the server's fighter events that bring bout 1 of m (and
// the stayer's streak from it) to what the rule says, stamped after every line
// of the log and the match, or none when bout 1 already holds them, m has no
// bout, or the competition is not kachinuki.
func openerSeatEventsTx(h state.StoreTx, comp *state.Competition, compID, matchID string, m *state.MatchResult, lines []state.ScoreLogLine) []domain.ScoreEvent {
	if len(m.SubResults) == 0 {
		return nil
	}
	d, ok := readOpenerDraw(h, comp, compID)
	if !ok || len(d.lineups) == 0 {
		return nil
	}
	stamp := max(serverNowMs(), m.ModifiedAt+1, newestLineStamp(lines)+1)
	return openerSeatEvents(m, d.opener(matchID, hasStarted(m), m.SideAID, m.SideBID), stamp)
}

// afterMatchWrite runs after a match write that stands (its error nil), in the
// same transaction, on a kachinuki competition: the write that moved the match
// out of scheduled keeps the lineups it started with (pinStartLineups), and
// bout 1 is brought to what the rule says (seatOpenerAfterWrite; the
// score-events door seats its own batch). before is the match's status before
// the write.
func (e *Engine) afterMatchWrite(h state.StoreTx, compID, matchID string, before matchBefore, door string) error {
	comp, err := h.LoadCompetition(compID)
	if err != nil || comp == nil || !comp.IsKachinuki() {
		return nil
	}
	// The first start only: a match sent back to the queue holds the bouts it
	// was fought on, and a start of it again is no start (it keeps the lineup
	// it started with, or, started before this rule, none).
	if before.status == state.MatchStatusScheduled && !before.fought {
		stored, _, err := e.lookupExistingResultIn(h, compID, matchID)
		if err == nil && stored != nil && stored.Status != state.MatchStatusScheduled {
			if d, ok := readOpenerDraw(h, comp, compID); ok {
				if err := pinStartLineups(h, &d, compID, stored); err != nil {
					return err
				}
			}
		}
	}
	if door != DoorEvent {
		e.seatOpenerAfterWrite(h, compID, matchID)
	}
	return nil
}

// storedStatus is the match's status before a write, "" when it cannot be read
// (an unknown match is the write's to answer).
func (e *Engine) storedStatus(h state.StoreTx, compID, matchID string) matchBefore {
	// Read only for a kachinuki competition: the write that follows reads
	// the match itself and answers what it cannot read.
	comp, err := h.LoadCompetition(compID)
	if err != nil || comp == nil || !comp.IsKachinuki() {
		return matchBefore{}
	}
	m, _, err := e.lookupExistingResultIn(h, compID, matchID)
	if err != nil || m == nil {
		return matchBefore{}
	}
	return matchBefore{status: m.Status, fought: holdsBouts(m)}
}

// matchBefore is what afterMatchWrite needs of a match as it stood before a
// write: its status, and whether it held the bouts it was fought on.
type matchBefore struct {
	status state.MatchStatus
	fought bool
}

// holdsBouts says m holds a numbered bout row: on a scheduled kachinuki match,
// the bouts it was fought on before it was sent back to the queue.
func holdsBouts(m *state.MatchResult) bool {
	return slices.ContainsFunc(m.SubResults, func(r state.SubMatchResult) bool { return r.Position > 0 })
}

// hasStarted says a kachinuki match was started: it left the queue, or it was
// sent back to it holding the bouts it was fought on.
func hasStarted(m *state.MatchResult) bool {
	return m.Status != state.MatchStatusScheduled || holdsBouts(m)
}

// seatOpenerAfterWrite brings bout 1 of a kachinuki match to what the rule
// says, through the score-events door, as the server's own events. It reports
// whether the match changed.
func (e *Engine) seatOpenerAfterWrite(h state.StoreTx, compID, matchID string) bool {
	comp, err := h.LoadCompetition(compID)
	if err != nil || comp == nil || !comp.IsKachinuki() {
		return false
	}
	stored, _, err := e.lookupExistingResultIn(h, compID, matchID)
	if err != nil || stored == nil {
		return false
	}
	lines, err := h.LoadScoreLog(compID, matchID)
	if err != nil {
		// An unreadable log is the door's to report; bout 1 keeps its fighters.
		return false
	}
	evs := openerSeatEventsTx(h, comp, compID, matchID, stored, lines)
	if len(evs) == 0 {
		return false
	}
	res, err := e.RecordScoreEventsTx(h, compID, matchID, ScoreEventsRequest{Session: ServerEventSession, Events: evs, NoTurnedAwayHistory: true, server: true})
	if err != nil {
		log.Printf("engine: ERROR: match %s/%s: bout 1's fighters not seated: %v", compID, matchID, err)
		return false
	}
	if res.WriteErr != nil {
		log.Printf("engine: match %s/%s: bout 1's fighters not seated: %v", compID, matchID, res.WriteErr)
		return false
	}
	return res.Written
}

// ReseatKachinukiOpenersTx brings bout 1 of every kachinuki match of teamID
// (every team when teamID is empty) that holds a bout to what the rule says,
// whatever its status, after a lineup or team-member write, and returns the
// ids of the matches it changed.
func (e *Engine) ReseatKachinukiOpenersTx(h state.StoreTx, compID, teamID string) ([]string, error) {
	comp, err := h.LoadCompetition(compID)
	if err != nil {
		return nil, err
	}
	if comp == nil || !comp.IsKachinuki() {
		return nil, nil
	}
	holds := func(aID, bID string) bool { return teamID == "" || aID == teamID || bID == teamID }
	var ids []string
	pool, err := h.LoadPoolMatches(compID)
	if err != nil {
		log.Printf("engine: kachinuki opener compId=%s: pool matches load error: %v", compID, err)
	}
	for _, m := range pool {
		if len(m.SubResults) > 0 && holds(m.SideAID, m.SideBID) {
			ids = append(ids, m.ID)
		}
	}
	if bracket, err := h.LoadBracket(compID); err != nil {
		log.Printf("engine: kachinuki opener compId=%s: bracket load error: %v", compID, err)
	} else if bracket != nil {
		var all []state.BracketMatch
		for _, round := range bracket.Rounds {
			all = append(all, round...)
		}
		if bracket.ThirdPlaceMatch != nil {
			all = append(all, *bracket.ThirdPlaceMatch)
		}
		for _, bm := range all {
			if len(bm.SubResults) > 0 && holds(bm.SideAID, bm.SideBID) {
				ids = append(ids, bm.ID)
			}
		}
	}
	var changed []string
	for _, id := range ids {
		if e.seatOpenerAfterWrite(h, compID, id) {
			changed = append(changed, id)
		}
	}
	return changed, nil
}

// ReseatKachinukiOpeners is ReseatKachinukiOpenersTx in a transaction of its own.
func (e *Engine) ReseatKachinukiOpeners(compID, teamID string) ([]string, error) {
	var changed []string
	err := e.store.WithTransaction(compID, func(tx state.StoreTx) error {
		var err error
		changed, err = e.ReseatKachinukiOpenersTx(tx, compID, teamID)
		return err
	})
	return changed, err
}

// UsePreviousLineupTx is "Use the previous match's lineup" (DELETE
// match-lineups): it takes the lineup stored for teamID at matchID itself
// away. On a kachinuki match that has started (hasStarted) that is an edit of
// the match's own lineup, so it is a snapshot, never a live link: the lineup
// in force there now, by the carry rule, is kept as the match's own in its
// place (a pin, pinLineup, marked now), so bout 1 takes those names on every
// device (the reseat and announcement that follow the door) and a later edit
// of the starting lineup renames nothing there.
func (e *Engine) UsePreviousLineupTx(h state.StoreTx, compID, teamID, matchID string, at int64) (bool, error) {
	// Taken away as a change stamped when it was made (the device's stamp,
	// else the server's as it lands; stage 6 review 8, m1): what the lineup
	// held before then is kept as a removed record, so the lineups carried by
	// stamp that were made while it was in force keep reading it, and a change
	// of it made after then stays.
	at = state.LineupChangeStamp(at)
	removed, err := h.RemoveTeamLineupAt(compID, teamID, matchID, 0, at)
	if err != nil {
		return false, err
	}
	comp, err := h.LoadCompetition(compID)
	if err != nil || comp == nil {
		return removed, err
	}
	kept := false
	if comp.IsKachinuki() {
		if kept, err = e.keepSnapshotLineup(h, comp, compID, teamID, matchID, at); err != nil {
			return removed, err
		}
	}
	if !removed && !kept {
		// Nothing in force changed (m8): no carried lineup to work out again.
		return false, nil
	}
	// Every carried lineup of the team is worked out again in the same
	// transaction, as after a save (FollowLineupSave).
	_, err = e.FollowLineupSave(h, comp, domain.TeamLineup{TeamID: teamID, MatchID: matchID}, nil)
	return true, err
}

// keepSnapshotLineup keeps, on a kachinuki match that has started, the lineup
// in force there now as its own (a pin marked now): taking a started match's
// own lineup away is an edit of it taken as a snapshot.
func (e *Engine) keepSnapshotLineup(h state.StoreTx, comp *state.Competition, compID, teamID, matchID string, at int64) (bool, error) {
	m, _, err := e.lookupExistingResultIn(h, compID, matchID)
	var notFound *NotFoundError
	if errors.As(err, &notFound) {
		return false, nil
	}
	if err != nil {
		return false, err
	}
	if m == nil || !hasStarted(m) || (m.SideAID != teamID && m.SideBID != teamID) {
		return false, nil
	}
	d, ok := readOpenerDraw(h, comp, compID)
	if !ok {
		return false, fmt.Errorf("kachinuki match %s/%s: lineups not readable", compID, matchID)
	}
	// A change of the match's own lineup made after the removal is still its
	// own: nothing to keep.
	if ownLineup(d.lineups, teamID, matchID) {
		return false, nil
	}
	// The lineup in force when the removal was made, or, for one made before
	// the match started that arrived after the start, the one in force at the
	// start, which the start would have kept.
	return true, pinLineup(h, &d, compID, matchID, teamID, max(at, startStamp(m)))
}

// UsePreviousLineup is UsePreviousLineupTx in a transaction of its own.
func (e *Engine) UsePreviousLineup(compID, teamID, matchID string) error {
	_, err := e.UsePreviousLineupAt(compID, teamID, matchID, 0)
	return err
}

// UsePreviousLineupAt is UsePreviousLineupTx in a transaction of its own, the
// removal stamped at (0: as it lands). It reports whether anything in force
// changed.
func (e *Engine) UsePreviousLineupAt(compID, teamID, matchID string, at int64) (bool, error) {
	changed := false
	err := e.store.WithTransaction(compID, func(tx state.StoreTx) error {
		var err error
		changed, err = e.UsePreviousLineupTx(tx, compID, teamID, matchID, at)
		return err
	})
	return changed, err
}

// serverFighterEvent is whether e is one of the server's own fighter events
// (kachinuki_opener.go). The closed-match rule lets it through: it names who
// fought, never a score or a result, so an edit of a finished match's own
// lineup renames its bout 1 and nothing of its result moves.
func serverFighterEvent(e *domain.ScoreEvent) bool {
	return e.Session == ServerEventSession && e.Kind == domain.ScoreBoutFighter
}

// fighterTarget is how a server fighter event names the fighter it replaces
// (ScoreEvent.Target): by member id when the fighter carries one, else by name.
func fighterTarget(f boutFighter) string {
	if f.MemberID != "" {
		return "id:" + f.MemberID
	}
	return "name:" + f.Name
}

// fighterIs is whether a side holding name and member holds the fighter
// target names (fighterTarget).
func fighterIs(name, member, target string) bool {
	if id, ok := strings.CutPrefix(target, "id:"); ok {
		return member == id
	}
	n, _ := strings.CutPrefix(target, "name:")
	return member == "" && name == n
}

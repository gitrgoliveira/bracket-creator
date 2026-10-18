package engine

// kachinuki_opener.go owns who fights a kachinuki encounter's first bout
// (operator decision K1 (c), 2026-10-18: "Whatever the lineup says now", with
// "Lineups are stuck per match as well. So a later lineup edit would have to be
// editing the lineup specifically for that match").
//
// Each side's first fighter is the first fighter of that team's lineup in force
// at the match (LineupInForce: the match's own lineup, else the one it carries
// from the team's previous match, else its starting lineup), read in fighting
// order (domain.TeamLineup.OrderedMembers), which is how the advance and the
// roster read who fights first. No device names them: a tablet's taps on bout 1
// are scoring only, so two tablets striking on bout 1 at once always both land
// there, and every surface shows the names the server holds.
//
// The server seats them with events of its own: bout.fighter events in the
// match's score log, under the session ServerEventSession, ordered by their
// stamp like every tap. An event governs no group and turns no tap away, the
// replay reads it back exactly, and a fighter on any bout is then one thing, a
// bout.fighter event (operator rule "Only one behaviour allowed"). They are
// made in two places, both through openerSeatEvents:
//   - every match write of a running kachinuki match: the score-events door
//     adds them to the batch it replays (RecordScoreEventsTx), and every other
//     door sends them through that door once it has written
//     (seatOpenerAfterWrite);
//   - every lineup or team-member write, for each running kachinuki match of
//     the team (ReseatKachinukiOpenersTx), so a later edit of the lineup
//     renames bout 1 on every device, its points staying on bout 1.
//
// The winner stays on: when bout 1 is renamed, each later bout of that side's
// winning streak from it whose fighter on that side is the one bout 1 held is
// renamed with it. A fighter picked there otherwise is the operator's and is
// left alone (operator ruling 2026-09-26: the app records what happens and
// never decides how a match is run).
//
// Not renamed: a match that is not running. A queued match takes the lineup's
// fighters when it is started again (its first write seats it), and a finished
// one keeps the names it was fought under: the closed-match rule turns an event
// after the finish away.

import (
	"cmp"
	"fmt"
	"log"
	"slices"
	"strings"

	"github.com/gitrgoliveira/bracket-creator/internal/domain"
	"github.com/gitrgoliveira/bracket-creator/internal/state"
)

// ServerEventSession is the event session of the events the server makes
// itself. No device holds it, and its events carry no seq, so no device's
// gaps or seqs (ScoreLogGaps, ScoreLogEventSeqs) count them.
const ServerEventSession = "server"

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
// match is worked out from. ok is false when there is nothing to seat from (not
// a kachinuki competition, no lineup saved, or lineups.yaml unreadable, which
// is logged): bout 1 then keeps its fighters.
func readOpenerDraw(h state.StoreTx, comp *state.Competition, compID string) (openerDraw, bool) {
	if comp == nil || !comp.IsKachinuki() {
		return openerDraw{}, false
	}
	lineups, err := h.LoadTeamLineups(compID)
	if err != nil {
		log.Printf("engine: kachinuki opener compId=%s: lineups.yaml load error: %v; bout 1 keeps its fighters", compID, err)
		return openerDraw{}, false
	}
	if len(lineups) == 0 {
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

// opener is the first fighter of each side's lineup in force at matchID.
func (d openerDraw) opener(matchID, sideAID, sideBID string) KachinukiOpener {
	first := func(teamID string) boutFighter {
		if teamID == "" {
			return boutFighter{}
		}
		in := LineupInForceFrom(d.lineups, d.pool, d.bracket, d.comp.IsKnockoutEnabled(), teamID, matchID)
		if !in.Found {
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

// ownLineup reports whether lineups holds a lineup saved for team teamID at
// match matchID itself.
func ownLineup(lineups map[string]domain.TeamLineup, teamID, matchID string) bool {
	for _, l := range lineups {
		if l.TeamID == teamID && l.MatchID == matchID {
			return true
		}
	}
	return false
}

// pinStartLineups saves, for each side of a kachinuki match that has started,
// the lineup in force there as the match's own when it has none (operator
// decision K1 (c): "Lineups are stuck per match as well"): from its start on,
// the match reads its own lineup whatever later edit is made to the starting
// lineup or an earlier match's, so its bout 1 and every lineup screen keep
// the lineup it was started with, and only an edit of this match's own lineup
// renames it. The saved copy holds what the match already read, so nothing
// shown changes. A lineups.yaml write that fails is logged: the match then
// keeps reading the lineup in force.
func pinStartLineups(h state.StoreTx, d *openerDraw, compID, matchID string, teamIDs ...string) {
	for _, teamID := range teamIDs {
		if teamID == "" || ownLineup(d.lineups, teamID, matchID) {
			continue
		}
		in := LineupInForceFrom(d.lineups, d.pool, d.bracket, d.comp.IsKnockoutEnabled(), teamID, matchID)
		if !in.Found {
			continue
		}
		l := copyLineup(in.Lineup)
		l.MatchID, l.Round = matchID, 0
		if err := h.SetTeamLineup(compID, l, d.comp.TeamSize); err != nil {
			log.Printf("engine: kachinuki match %s/%s: the lineup it started with is not kept as its own: %v", compID, matchID, err)
			continue
		}
		if fresh, err := h.LoadTeamLineups(compID); err == nil {
			d.lineups = fresh
		}
	}
}

// openerSeatEventsTx is openerSeatEvents for m, a kachinuki match as the score
// log lines leave it, with the lineups the transaction reads, stamped after
// every line and the match itself. pin keeps, for a match that has started,
// the lineup it started with as its own first (pinStartLineups): a match write
// pins, a lineup write does not (a match started before this rule existed then
// keeps the names it holds).
func openerSeatEventsTx(h state.StoreTx, comp *state.Competition, compID, matchID string, m *state.MatchResult, lines []state.ScoreLogLine, pin bool) []domain.ScoreEvent {
	if comp == nil || !comp.IsKachinuki() {
		return nil
	}
	d, ok := readOpenerDraw(h, comp, compID)
	if !ok {
		return nil
	}
	started := m.Status != state.MatchStatusScheduled
	if pin && started {
		pinStartLineups(h, &d, compID, matchID, m.SideAID, m.SideBID)
	}
	if len(m.SubResults) == 0 {
		return nil
	}
	if !pin && started && !(ownLineup(d.lineups, m.SideAID, matchID) || ownLineup(d.lineups, m.SideBID, matchID)) {
		return nil
	}
	stamp := max(serverNowMs(), m.ModifiedAt+1, newestLineStamp(lines)+1)
	return openerSeatEvents(m, d.opener(matchID, m.SideAID, m.SideBID), stamp)
}

// seatOpenerAfterWrite seats, through the score-events door, a kachinuki match
// a door other than that one has just written (pin true) or a lineup write has
// changed the lineups of (pin false). Reports whether it seated anything.
func (e *Engine) seatOpenerAfterWrite(h state.StoreTx, compID, matchID string, pin bool) bool {
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
	evs := openerSeatEventsTx(h, comp, compID, matchID, stored, lines, pin)
	if len(evs) == 0 {
		return false
	}
	res, err := e.RecordScoreEventsTx(h, compID, matchID, ScoreEventsRequest{Session: ServerEventSession, Events: evs, NoTurnedAwayHistory: true})
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

// ReseatKachinukiOpenersTx seats, after a lineup or team-member write in the
// transaction h, every kachinuki match of team teamID ("" for every team) whose
// first bout the lineups in force now name differently, whatever its status,
// and returns the ids of the matches it changed, for the caller to announce. A
// match that has started reads its own lineup (pinStartLineups), so only an
// edit of that match's own lineup renames it.
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
		if e.seatOpenerAfterWrite(h, compID, id, false) {
			changed = append(changed, id)
		}
	}
	return changed, nil
}

// ReseatKachinukiOpeners is ReseatKachinukiOpenersTx in a transaction of its
// own, for a lineup or team-member door that has already written.
func (e *Engine) ReseatKachinukiOpeners(compID, teamID string) ([]string, error) {
	var changed []string
	err := e.store.WithTransaction(compID, func(tx state.StoreTx) error {
		var err error
		changed, err = e.ReseatKachinukiOpenersTx(tx, compID, teamID)
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

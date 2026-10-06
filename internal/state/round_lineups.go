package state

// round_lineups.go owns the one-time move of round lineups onto matches
// (operator decision 2026-10-06: migrate old content to the new format instead
// of reading it).
//
// Releases up to v2.1.1 let the Lineups page save a lineup for round r (stored
// 0-based, entered 1-based), and read the lineup a team fielded at a match as:
// the match's own, else the round lineup with the highest round at or below the
// match's round (FindBestLineup), else the highest round there was. A team now
// fields the lineup of its previous match unless one is entered for the match
// (engine.LineupInForce), which has no place for a lineup that waits for a
// round. So each round r >= 1 lineup of a team is moved onto the first match of
// the team in match order (MatchPlace) that was at round r or later and that the
// team is seated in by id, written exactly as PUT .../match-lineups/:matchId
// writes one (a match-scoped entry, Round 0), and the round entry is removed.
//
// The round a match had is the one the client read the lineup by
// (resolveRoundIndex, web-mobile/js/admin_helpers.jsx, identical in v2.0.0,
// v2.1.0 and v2.1.1): a knockout match's index in bracket.Rounds, the 3rd-place
// match's len(Rounds), a pool or league match's stored Round (its circle-method
// round, or -1 for a pool drawn without rounds, read as 0), and 0 for a Swiss
// match, which never stored one. legacyLineupRound is that rule.
//
// A team whose lineup waits for a round it is not yet seated in keeps it, and
// the competition's RoundLineupsConverted marker stays unset: every write of the
// draw settles again (Store.settleRoundLineupsAfterWrite for a write that goes
// straight to disk, storeTx.settleRoundLineupsAtCommit for a transaction), so
// the moment the team is seated its lineup moves, in that write. The marker is
// set once nothing waits.

import (
	"cmp"
	"fmt"
	"log"
	"maps"
	"path/filepath"
	"slices"
	"strings"

	"github.com/gitrgoliveira/bracket-creator/internal/domain"
)

// legacyLineupRound is the round v2.1.1 and earlier read m's lineup by.
func legacyLineupRound(m DrawMatch) int {
	if m.Knockout {
		return m.Round
	}
	return max(m.PoolRound, 0)
}

// isTeamCompetition reports whether lineups apply to c at all. The engine
// identifies a team competition by Kind and by TeamSize in different paths.
func isTeamCompetition(c *Competition) bool {
	return c.Kind == "team" || c.TeamSize > 0
}

// lineupRoster is the teams of a competition, by participant id and by name.
type lineupRoster struct {
	ids    map[string]struct{}
	byName map[string][]string
}

func newLineupRoster(players []domain.Player) lineupRoster {
	r := lineupRoster{ids: make(map[string]struct{}, len(players)), byName: make(map[string][]string, len(players))}
	for _, p := range players {
		if p.ID == "" {
			continue
		}
		r.ids[p.ID] = struct{}{}
		r.byName[p.Name] = append(r.byName[p.Name], p.ID)
	}
	return r
}

func (r lineupRoster) has(teamID string) bool {
	_, ok := r.ids[teamID]
	return ok
}

// roundLineupSettlement is what settleRoundLineups decided: the lineups to
// store, whether they differ from those it was given, how many round lineups
// still wait for their team to be seated, and a note for the log on every move,
// drop and re-key.
type roundLineupSettlement struct {
	lineups map[string]domain.TeamLineup
	changed bool
	waiting int
	notes   []string
}

func (r *roundLineupSettlement) notef(format string, args ...any) {
	r.notes = append(r.notes, fmt.Sprintf(format, args...))
}

// scopeOf names what a lineup was saved for, in a log line.
func scopeOf(l domain.TeamLineup) string {
	if l.MatchID != "" {
		return "match " + l.MatchID
	}
	return fmt.Sprintf("round %d", l.Round)
}

// lineupCopy is l as a lineup of team for matchID (empty for the starting
// lineup, which is round 0), carrying its own copies of the maps.
func lineupCopy(l domain.TeamLineup, team, compID, matchID string, round int) domain.TeamLineup {
	return domain.TeamLineup{
		TeamID: team, CompetitionID: compID, Round: round, MatchID: matchID,
		Positions: maps.Clone(l.Positions), MemberIDs: maps.Clone(l.MemberIDs),
	}
}

// settleRoundLineups moves the round lineups it can onto matches, drops those
// no match can ever seat their team in, and leaves the rest waiting. It reads
// only its arguments: comp is the competition (its format says whether a
// knockout is still to come), players its roster, lineups the stored lineups
// (never modified), and draw the matches as DrawMatchesFrom lists them.
//
// onLoad is the load repair's pass, which also keys a lineup saved under a
// team's name by the team's id and gives a team that has only later rounds the
// lowest of them as its starting lineup; a write that seats a team settles
// without them, so a starting lineup removed since is not brought back.
func settleRoundLineups(comp *Competition, players []domain.Player, lineups map[string]domain.TeamLineup, draw []DrawMatch, onLoad bool) roundLineupSettlement {
	res := roundLineupSettlement{lineups: maps.Clone(lineups)}
	if res.lineups == nil {
		res.lineups = map[string]domain.TeamLineup{}
	}
	roster := newLineupRoster(players)
	if onLoad {
		res.keyByTeamID(roster)
		res.seedStartingLineups(comp.ID, roster)
	}
	res.moveRoundLineups(comp, roster, draw)
	return res
}

// keyByTeamID re-keys a lineup whose team id is not a participant id but is
// exactly one team's name (v2.0.0's Lineups page addressed a team that had no
// id yet by its name) under that team's id. When the team already has a lineup
// for the same scope the id-keyed one stays and the name-keyed one is dropped.
// A name that matches no team, or several, is left as it is.
func (r *roundLineupSettlement) keyByTeamID(roster lineupRoster) {
	for _, key := range slices.Sorted(maps.Keys(r.lineups)) {
		l := r.lineups[key]
		if l.TeamID == "" || roster.has(l.TeamID) {
			continue
		}
		teams := roster.byName[l.TeamID]
		switch len(teams) {
		case 0:
			r.notef("the %s lineup of %q is left as it is: no team has that name", scopeOf(l), l.TeamID)
		case 1:
			moved := l
			moved.TeamID = teams[0]
			delete(r.lineups, key)
			r.changed = true
			newKey := lineupStorageKey(moved)
			if _, taken := r.lineups[newKey]; taken {
				r.notef("the %s lineup of %q is dropped: team %s already has one for it", scopeOf(l), l.TeamID, teams[0])
				continue
			}
			r.lineups[newKey] = moved
			r.notef("the %s lineup of %q is now keyed by team %s", scopeOf(l), l.TeamID, teams[0])
		default:
			r.notef("the %s lineup of %q is left as it is: %d teams have that name", scopeOf(l), l.TeamID, len(teams))
		}
	}
}

// seedStartingLineups gives each team that has lineups for later rounds and no
// starting lineup (round 0) a copy of its lowest round's lineup as its starting
// lineup. v2.1.1 fell back to the highest round there was for a match before any
// saved round; a team now carries forward what it fielded first, which is the
// lowest.
func (r *roundLineupSettlement) seedStartingLineups(compID string, roster lineupRoster) {
	lowest := map[string]domain.TeamLineup{}
	started := map[string]bool{}
	for _, l := range r.lineups {
		if l.MatchID != "" || !roster.has(l.TeamID) {
			continue
		}
		switch {
		case l.Round == 0:
			started[l.TeamID] = true
		case l.Round > 0:
			if cur, ok := lowest[l.TeamID]; !ok || l.Round < cur.Round {
				lowest[l.TeamID] = l
			}
		}
	}
	for _, team := range slices.Sorted(maps.Keys(lowest)) {
		if started[team] {
			continue
		}
		src := lowest[team]
		r.lineups[teamLineupKey(team, 0)] = lineupCopy(src, team, compID, "", 0)
		r.changed = true
		r.notef("team %s had no starting lineup: its round %d lineup is now it", team, src.Round)
	}
}

// orderedMatch is a team match with its place in match order.
type orderedMatch struct {
	DrawMatch
	place MatchPlace
}

// moveRoundLineups moves each round lineup (round >= 1) of a team onto the
// first match, in match order, that was at its round or later and that the team
// is seated in; drops one that the team's own lineup for that match shadows, or
// that no match can seat its team in any more; and counts the rest as waiting.
func (r *roundLineupSettlement) moveRoundLineups(comp *Competition, roster lineupRoster, draw []DrawMatch) {
	knockout := comp.IsKnockoutEnabled()
	bracketDrawn := false
	var matches []orderedMatch
	for seq, m := range draw {
		if m.Knockout {
			if !knockout {
				continue // a league or Swiss competition has no knockout stage; its bracket.json is vestigial
			}
			bracketDrawn = true
		}
		if m.Hidden {
			continue
		}
		if place, ok := m.Place(seq); ok {
			matches = append(matches, orderedMatch{DrawMatch: m, place: place})
		}
	}
	slices.SortFunc(matches, func(a, b orderedMatch) int { return a.place.Compare(b.place) })

	// A draw that is still to be written, or a knockout whose bracket is, has
	// no match to say a team cannot be seated in.
	drawn := len(matches) > 0 && (!knockout || bracketDrawn)
	// undecided reports whether a match at round or later still has a side to
	// be decided, which may yet be the team.
	undecided := func(round int) bool {
		return slices.ContainsFunc(matches, func(m orderedMatch) bool {
			return legacyLineupRound(m.DrawMatch) >= round && (m.SideAID == "" || m.SideBID == "")
		})
	}

	type roundLineup struct {
		key   string
		team  string
		round int
	}
	var round []roundLineup
	for key, l := range r.lineups {
		if l.MatchID == "" && l.Round >= 1 && roster.has(l.TeamID) {
			round = append(round, roundLineup{key: key, team: l.TeamID, round: l.Round})
		}
	}
	// The higher round first: two lineups of a team can begin on the same
	// match, where v2.1.1 read the highest round at or below the match's, so
	// the higher is the team's lineup there and the lower is shadowed.
	slices.SortFunc(round, func(a, b roundLineup) int {
		return cmp.Or(strings.Compare(a.team, b.team), cmp.Compare(b.round, a.round))
	})

	for _, rl := range round {
		l := r.lineups[rl.key]
		i := slices.IndexFunc(matches, func(m orderedMatch) bool {
			return m.Seats(rl.team) && legacyLineupRound(m.DrawMatch) >= rl.round
		})
		switch {
		case i >= 0:
			target := matches[i]
			delete(r.lineups, rl.key)
			r.changed = true
			matchKey := teamLineupMatchKey(rl.team, target.ID)
			if _, own := r.lineups[matchKey]; own {
				r.notef("the round %d lineup of team %s is dropped: the team's own lineup for match %s comes before it", rl.round, rl.team, target.ID)
				continue
			}
			r.lineups[matchKey] = lineupCopy(l, rl.team, comp.ID, target.ID, 0)
			r.notef("the round %d lineup of team %s is now its lineup for match %s", rl.round, rl.team, target.ID)
		case drawn && !undecided(rl.round):
			delete(r.lineups, rl.key)
			r.changed = true
			r.notef("the round %d lineup of team %s is dropped: no match of round %d or later can seat the team", rl.round, rl.team, rl.round)
		default:
			r.waiting++
		}
	}
}

// roundLineupStage is what one settlement reads and writes: the competition
// files as its caller sees them (a transaction's own staged bytes first) and the
// caller's write function, so a transaction stages the move with the write that
// seats a team and an abandoned transaction leaves lineups.yaml alone.
type roundLineupStage struct {
	store       *Store
	compID      string
	write       writeFn
	comp        func() (*Competition, error)
	lineups     func() (map[string]domain.TeamLineup, error)
	poolMatches func() ([]MatchResult, error)
	bracket     func() (*Bracket, error)
	players     func(comp *Competition) ([]domain.Player, error)
}

// directRoundLineupStage is the stage of a write that goes straight to disk.
// Caller holds the competition's per-comp lock.
func (s *Store) directRoundLineupStage(compID string) *roundLineupStage {
	st := &roundLineupStage{store: s, compID: compID, write: s.directWrite}
	st.comp = func() (*Competition, error) { return s.loadCompetitionLocked(compID) }
	st.lineups = func() (map[string]domain.TeamLineup, error) { return s.loadTeamLineupsLocked(compID) }
	st.poolMatches = func() ([]MatchResult, error) { return s.LoadPoolMatchesLocked(compID) }
	st.bracket = func() (*Bracket, error) { return s.loadBracketLocked(compID) }
	st.players = st.loadPlayers
	return st
}

// roundLineupStage is the stage of a transaction: every read sees what the
// transaction has staged, every write is staged with it.
func (t *storeTx) roundLineupStage() *roundLineupStage {
	st := &roundLineupStage{store: t.store, compID: t.compID, write: t.txWriteFn()}
	st.comp = func() (*Competition, error) { return t.LoadCompetition(t.compID) }
	st.lineups = func() (map[string]domain.TeamLineup, error) { return t.LoadTeamLineups(t.compID) }
	st.poolMatches = func() ([]MatchResult, error) { return t.LoadPoolMatches(t.compID) }
	st.bracket = func() (*Bracket, error) { return t.LoadBracket(t.compID) }
	st.players = st.loadPlayers
	return st
}

// loadPlayers reads the roster. participants.csv is never staged in a
// transaction (it is written directly), so both stages read it from disk.
func (st *roundLineupStage) loadPlayers(comp *Competition) ([]domain.Player, error) {
	return st.store.loadParticipantsNoLock(st.compID, comp.EffectiveWithZekkenName(), LoadParticipantsOpts{HasIDs: comp.ParticipantIDsHint()})
}

// settle runs one settlement and stores what it decided: the lineups when they
// changed, and the competition with its marker set when nothing waits any more
// (marked reports that it was). A competition that is not a team competition,
// that is already marked, or that does not exist has nothing to settle.
// Everything is read and decided before the first write, so a failure leaves
// nothing half done; a failed write is retried by the next write of the draw,
// and a lineup moved before the marker was stored simply finds nothing to move
// the second time.
func (st *roundLineupStage) settle(onLoad bool) (marked bool, err error) {
	comp, err := st.comp()
	if err != nil || comp == nil || comp.RoundLineupsConverted || !isTeamCompetition(comp) {
		return false, err
	}
	if comp.ID != st.compID {
		// saveCompetitionChangedLocked paths and locks off comp.ID, not the
		// directory this call was handed: see upgradeCompetitionFormatLocked's
		// BUG 1. Leave the files as they are.
		log.Printf("state: round-lineup settlement for %s: config.md id %q does not match its directory; left unconverted", st.compID, comp.ID)
		return false, nil
	}
	lineups, err := st.lineups()
	if err != nil {
		return false, err
	}
	players, err := st.players(comp)
	if err != nil {
		return false, err
	}
	pool, err := st.poolMatches()
	if err != nil {
		return false, err
	}
	bracket, err := st.bracket()
	if err != nil {
		return false, err
	}

	res := settleRoundLineups(comp, players, lineups, DrawMatchesFrom(pool, bracket), onLoad)

	if res.changed {
		if err := st.store.saveTeamLineupsLocked(st.compID, res.lineups, st.write); err != nil {
			return false, err
		}
	}
	for _, note := range res.notes {
		log.Printf("state: round-lineup upgrade for %s: %s", st.compID, note)
	}
	if res.waiting > 0 {
		return false, nil
	}
	settled := *comp
	settled.RoundLineupsConverted = true
	if _, err := st.store.saveCompetitionChangedLocked(&settled, st.write); err != nil {
		return false, fmt.Errorf("round lineups settled but their marker was not saved: %w", err)
	}
	return true, nil
}

// settleRoundLineupsAfterWrite settles after a draw write that went straight to
// disk, under the per-comp lock the write holds. It is best effort, like the
// load repair: the write has landed, and a failure here is retried by the next
// one.
func (s *Store) settleRoundLineupsAfterWrite(compID string) {
	if _, err := s.directRoundLineupStage(compID).settle(false); err != nil {
		log.Printf("state: round-lineup settlement for %s: %v", compID, err)
	}
}

// settleRoundLineupsAtCommit settles at the end of a transaction that wrote the
// draw (bracket.json or pool-matches.csv), against what the transaction staged,
// and stages the move beside those writes so they commit or abort together.
// Once, at the end, so what a transaction later takes back (a rolled-back write)
// was never moved.
func (t *storeTx) settleRoundLineupsAtCommit() {
	wroteDraw := false
	for _, in := range t.wal.Intents() {
		if base := filepath.Base(in.Path); base == "bracket.json" || base == "pool-matches.csv" {
			wroteDraw = true
			break
		}
	}
	if !wroteDraw {
		return
	}
	if _, err := t.roundLineupStage().settle(false); err != nil {
		log.Printf("state: round-lineup settlement for %s: %v", t.compID, err)
	}
}

package state

// round_lineups.go owns the one-time conversion of round lineups into match
// lineups (operator decision 2026-10-06: migrate old content to the new format
// instead of reading it).
//
// Releases up to v2.1.1 let the Lineups page save a lineup for round r (stored
// 0-based, entered 1-based), and read the lineup a team fielded at a match as:
// the match's own (never carried to another match), else the round lineup with
// the highest round at or below the match's round (FindBestLineup), else the
// highest round there was. A team now fields the lineup of its previous match
// unless one is entered for the match (engine.LineupInForce), which has no place
// for a lineup that waits for a round, and one entered for a match no longer stays
// at that match. So a legacy team, a team with a lineup for a round r >= 1 or, found
// at the competition's first settlement, one with a lineup entered for a match it is
// seated in (the match lineups it had were shown nowhere else, and the team carries
// them from now on), is given, for every team match it is seated in by id (in match
// order, MatchPlace; TeamMatches says which matches are team matches, a bye being
// none, since nobody plays it), a lineup of its own for that match equal to what
// v2.1.1 showed there, written exactly as PUT .../match-lineups/:matchId writes one
// (a match-scoped entry, Round 0). A team that had no lineup for a round at all was
// shown none at a match it had no lineup for, and is given an EMPTY lineup of its own
// there (positions {}, which the lineup rule honours as the match's own: it shows
// none rather than carrying an earlier match's). A match that already has an own
// lineup of the team keeps it: that is v2.1.1's own reading. Every match a legacy
// team is seated in then holds its own lineup, so nothing the operator enters later
// for another match changes it, and settling again finds nothing to give. A team
// that is not legacy is left exactly as it is.
//
// The round a match had is the one the client read the lineup by
// (resolveRoundIndex, web-mobile/js/admin_helpers.jsx, identical in v2.0.0,
// v2.1.0 and v2.1.1): a knockout match's index in bracket.Rounds, the 3rd-place
// match's len(Rounds), a pool or league match's stored Round (its circle-method
// round, or -1 for a pool drawn without rounds, read as 0), and 0 for a Swiss
// match, which never stored one. legacyLineupRound is that rule. A pool's bouts
// are numbered in playing order, and a team's stored rounds in that order need
// not rise, which is why a lineup is given per match rather than moved to the
// first match at its round.
//
// Which teams are legacy is decided ONCE, at the competition's first settlement
// (the load pass: no marker and no list recorded), and recorded in config.md
// (Competition.RoundLineupsLegacy). A lineup entered for a match is a reading of
// v2.1.1 only until the first pass; one saved by this release is the operator's
// own and carries to the team's later matches, so it must never make a team
// legacy afterwards, and a restart must not find it again. Later passes read the
// record, plus any team that has a lineup for a round r >= 1. A match lineup left
// behind by a re-seat, for a match the team is not seated in, is no reading v2.1.1
// showed and makes no team legacy.
//
// The round lineups themselves stay on disk (engine.LineupInForce never reads
// one) until the competition is completed: a correction can seat a team in a
// match until it is over, and the match is given the lineup v2.1.1 showed there
// from them. When it is completed they are removed. A legacy team with no round
// lineup waits as well, for the same reason. The competition's
// RoundLineupsConverted marker is set as soon as nothing waits: at the first
// settlement, whatever the status, for a competition with no legacy team (and
// none waiting for an id, below), so such a competition costs nothing after it;
// for one that has a legacy team, when the competition is completed and the
// round lineups are removed, or when the draw's removal leaves none
// (ClearDrawLineups: a team found only for its match lineups has nothing left to
// be shown once they went with the draw). Every write of the draw settles again
// (Store.settleRoundLineupsAfterWrite for a write that goes straight to disk,
// storeTx.settleRoundLineupsAtCommit for a transaction), so a match a team is
// seated in later is given its lineup in the write that seats it; a competition
// that is completed in a write those hooks do not see is settled by the next
// load.
//
// Which (team, match) pairs have been settled is recorded in config.md
// (Competition.RoundLineupsGiven) while the round lineups are kept. A pair that
// is listed is never given a lineup again, whatever lineups.yaml holds, so a
// lineup the operator removes from a match ("Use the previous match's lineup")
// stays removed; a seated pair that is not listed is given the lineup v2.1.1
// showed when the team has none there, and listed either way. The lineups are
// saved before the record, so a failure between the two leaves lineups the next
// pass finds and only lists. The marker clears the record (and the list of legacy
// teams), and ClearDrawLineups clears the record too, since the next draw reuses
// the match ids.
//
// A team with no starting lineup (round 0) is given, on the load pass, what
// v2.1.1 showed before any round it saved: its highest round's lineup, which
// v2.1.1 fell back to. A match seated after the load and read at a round below
// the team's lowest saved one then finds that lineup, as v2.1.1 did, whichever
// pass reads it.
//
// A lineup saved under a team's name is keyed by the team's id. While a team of
// that name has no id yet (a roster recorded without ids, until the participant
// list is applied) it waits too, with the marker unset.

import (
	"cmp"
	"fmt"
	"log"
	"maps"
	"path/filepath"
	"slices"

	"github.com/gitrgoliveira/bracket-creator/internal/domain"
)

// legacyLineupRound is the round v2.1.1 and earlier read m's lineup by.
func legacyLineupRound(m DrawMatch) int {
	if m.Knockout {
		return m.Round
	}
	return max(m.PoolRound, 0)
}

// lineupRoster is the teams of a competition, by participant id and by name.
type lineupRoster struct {
	ids    map[string]struct{}
	byName map[string][]string
	// withoutID holds the names of the players that have no participant id yet:
	// a roster recorded before ids existed keeps none until the operator applies
	// the participant list.
	withoutID map[string]struct{}
}

func newLineupRoster(players []domain.Player) lineupRoster {
	r := lineupRoster{
		ids:       make(map[string]struct{}, len(players)),
		byName:    make(map[string][]string, len(players)),
		withoutID: map[string]struct{}{},
	}
	for _, p := range players {
		if p.ID == "" {
			r.withoutID[p.Name] = struct{}{}
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

// awaitsID reports whether a player named name has no participant id yet, so the
// name cannot be matched to exactly one team until it has one.
func (r lineupRoster) awaitsID(name string) bool {
	_, ok := r.withoutID[name]
	return ok
}

// roundLineupSettlement is what settleRoundLineups decided: the lineups to
// store, whether they differ from those it was given, how many things still wait
// (a legacy team, until the competition is completed, or a lineup saved under the
// name of a team that has no id yet), the (team, match) pairs settled, the legacy
// teams found, and a note for the log on every match lineup given, round lineup
// removed and re-key.
type roundLineupSettlement struct {
	lineups map[string]domain.TeamLineup
	changed bool
	waiting int
	// given is every pair settled so far, as Competition.RoundLineupsGiven
	// records them (a team's id to its sorted match ids; nil for none), and
	// givenGrew whether that is more than the competition's record holds.
	given     map[string][]string
	givenGrew bool
	// legacy is the legacy teams this settlement found, sorted, and
	// legacyFound whether this was the competition's first settlement and found
	// any, so the list is to be recorded (Competition.RoundLineupsLegacy).
	legacy      []string
	legacyFound bool
	notes       []string
}

// cloneRoundLineupsGiven is a deep copy of a Competition.RoundLineupsGiven: the
// map and every list in it, so a copy of the record shares nothing with it.
func cloneRoundLineupsGiven(given map[string][]string) map[string][]string {
	if given == nil {
		return nil
	}
	out := make(map[string][]string, len(given))
	for team, matchIDs := range given {
		out[team] = slices.Clone(matchIDs)
	}
	return out
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

// settleRoundLineups gives each legacy team the lineup v2.1.1 showed at every
// match it is seated in, and removes the round lineups once the competition is
// completed. It reads only its arguments: comp is the competition (its format
// says whether a knockout is played, its status whether it is over,
// RoundLineupsGiven which pairs were settled before and RoundLineupsLegacy which
// teams the first settlement found legacy), players its roster, lineups the
// stored lineups (never modified), and draw the matches as DrawMatchesFrom lists
// them.
//
// A pair (team, match) is settled once: when the team is seated in the match and
// the record does not list the pair, the team is given v2.1.1's reading unless it
// already has a lineup of its own for the match, and the pair is listed either
// way. A listed pair is left alone, whatever the lineups hold.
//
// A lineup saved under a team's name is keyed by the team's id on every pass
// (one whose team has no id yet waits), so the write that follows the minting of
// the ids converts it. onLoad is the load repair's pass, which also gives a
// legacy team that has no starting lineup what v2.1.1 showed before any round it
// saved as one; a write that seats a team settles without it, so a starting
// lineup removed since is not brought back.
func settleRoundLineups(comp *Competition, players []domain.Player, lineups map[string]domain.TeamLineup, draw []DrawMatch, onLoad bool) roundLineupSettlement {
	res := roundLineupSettlement{lineups: maps.Clone(lineups)}
	if res.lineups == nil {
		res.lineups = map[string]domain.TeamLineup{}
	}
	roster := newLineupRoster(players)
	res.keyByTeamID(roster, onLoad)
	matches := TeamMatches(draw, comp.IsKnockoutEnabled())
	legacy := res.legacyTeams(comp, roster, matches)
	res.giveMatchLineups(comp, legacy, matches)
	if onLoad {
		res.seedStartingLineups(comp.ID, legacy)
	}
	res.retireRoundLineups(legacy, comp.Status == CompStatusComplete)
	return res
}

// keyByTeamID re-keys a lineup whose team id is not a participant id but is
// exactly one team's name (v2.0.0's Lineups page addressed a team that had no
// id yet by its name) under that team's id. When the team already has a lineup
// for the same scope the id-keyed one stays and the name-keyed one is dropped.
// A name that matches no team, or several, is left as it is. One that matches a
// player with no id yet waits for it (a roster recorded without ids, until the
// participant list is applied), and so does a name shared with such a player,
// which cannot be told apart until it has one. The notes for the lineups left
// as they are are made on the load pass only, so a write does not repeat them.
func (r *roundLineupSettlement) keyByTeamID(roster lineupRoster, onLoad bool) {
	for _, key := range slices.Sorted(maps.Keys(r.lineups)) {
		l := r.lineups[key]
		if l.TeamID == "" || roster.has(l.TeamID) {
			continue
		}
		if roster.awaitsID(l.TeamID) {
			r.waiting++
			if onLoad {
				r.notef("the %s lineup of %q waits: a team of that name has no id yet", scopeOf(l), l.TeamID)
			}
			continue
		}
		teams := roster.byName[l.TeamID]
		switch len(teams) {
		case 0:
			if onLoad {
				r.notef("the %s lineup of %q is left as it is: no team has that name", scopeOf(l), l.TeamID)
			}
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
			if onLoad {
				r.notef("the %s lineup of %q is left as it is: %d teams have that name", scopeOf(l), l.TeamID, len(teams))
			}
		}
	}
}

// legacyTeams returns, for each legacy team of the roster, all its round
// lineups, the starting lineup (round 0) included, lowest round first: none for a
// team that had no lineup for a round. A team is legacy when it
//
//   - has a lineup for a round r >= 1, which only the Lineups page of releases up
//     to v2.1.1 could save;
//   - is one of the teams comp.RoundLineupsLegacy records, the legacy teams the
//     competition's first settlement found; or
//   - at that first settlement itself (nothing recorded yet), has a match lineup,
//     stored under its id, for a match of the draw that seats it. v2.1.1 read a
//     match's own lineup and never carried it to another match, so such a team
//     was shown at its other matches what its round lineups said, or nothing,
//     where a team now carries the lineup of its previous match. A match lineup
//     for a match that does not seat the team, left behind by a re-seat, is no
//     reading v2.1.1 showed and makes it none; and one saved after the first
//     settlement is this release's, which carries, so it never does.
//
// A team that is none of these is left exactly as it is. Which teams were found
// is recorded when this is the first settlement (legacyFound), so the next one,
// and a restart's, reads them rather than deriving them again.
func (r *roundLineupSettlement) legacyTeams(comp *Competition, roster lineupRoster, matches []TeamMatch) map[string][]domain.TeamLineup {
	rounds := map[string][]domain.TeamLineup{}
	for _, l := range r.lineups {
		if l.MatchID == "" && roster.has(l.TeamID) {
			rounds[l.TeamID] = append(rounds[l.TeamID], l)
		}
	}
	legacy := map[string][]domain.TeamLineup{}
	add := func(team string) {
		lineups := rounds[team]
		slices.SortFunc(lineups, func(a, b domain.TeamLineup) int { return cmp.Compare(a.Round, b.Round) })
		legacy[team] = lineups
	}
	for team, lineups := range rounds {
		if slices.ContainsFunc(lineups, func(l domain.TeamLineup) bool { return l.Round >= 1 }) {
			add(team)
		}
	}
	for _, team := range comp.RoundLineupsLegacy {
		if roster.has(team) {
			add(team)
		}
	}
	first := comp.RoundLineupsLegacy == nil
	if first {
		seatedIn := func(team, matchID string) bool {
			return slices.ContainsFunc(matches, func(m TeamMatch) bool { return m.ID == matchID && m.Seats(team) })
		}
		for _, l := range r.lineups {
			if l.MatchID != "" && roster.has(l.TeamID) && seatedIn(l.TeamID, l.MatchID) {
				add(l.TeamID)
			}
		}
	}
	if first && len(legacy) > 0 {
		r.legacy = slices.Sorted(maps.Keys(legacy))
		r.legacyFound = true
	}
	return legacy
}

// teamsWithRoundLineups is the teams of the roster that have a lineup for a round
// r >= 1 in lineups, each once, sorted: the legacy teams a draw's removal leaves,
// since a team found only for the lineups entered for its matches has nothing left
// to be shown once they went with the draw. A lineup of a team that is no longer on
// the roster is no team's to wait for. nil when there are none, as the record of
// legacy teams (Competition.RoundLineupsLegacy) is when empty.
func teamsWithRoundLineups(lineups map[string]domain.TeamLineup, roster lineupRoster) []string {
	var teams []string
	for _, l := range lineups {
		if l.MatchID == "" && l.Round >= 1 && roster.has(l.TeamID) {
			teams = append(teams, l.TeamID)
		}
	}
	slices.Sort(teams)
	return slices.Compact(teams)
}

// anyAwaitsID reports whether a lineup is stored under the name of a player that
// has no participant id yet, which keyByTeamID keeps waiting for the id: nothing
// may mark the conversion done while one does, or it would stay name-keyed for good.
func (r lineupRoster) anyAwaitsID(lineups map[string]domain.TeamLineup) bool {
	for _, l := range lineups {
		if l.TeamID != "" && !r.has(l.TeamID) && r.awaitsID(l.TeamID) {
			return true
		}
	}
	return false
}

// legacyReading is the lineup v2.1.1 showed a team at a match read at round,
// from the team's round lineups, lowest round first: the one with the highest
// round at or below round, else the highest round there was. ok is false when the
// team had no lineup for a round at all, and v2.1.1 showed none.
func legacyReading(rounds []domain.TeamLineup, round int) (lineup domain.TeamLineup, ok bool) {
	for i := len(rounds) - 1; i >= 0; i-- {
		if rounds[i].Round <= round {
			return rounds[i], true
		}
	}
	if len(rounds) == 0 {
		return domain.TeamLineup{}, false
	}
	// Every round is above round: v2.1.1 fell back to the highest there was.
	return rounds[len(rounds)-1], true
}

// giveMatchLineups settles each pair of a legacy team and a match it is seated in
// by id that the competition's record does not list: the team is given the lineup
// v2.1.1 showed there (legacyReading at the round the match was read at) unless
// it already has a lineup of its own for the match, which is v2.1.1's own reading
// of it and is left as it is, and the pair is listed either way. A team that had
// no lineup for a round at all was shown none, and is given an empty lineup of its
// own, which the lineup rule honours as the match's own, so the match shows none
// rather than what an earlier one does. A listed pair is left alone, so a lineup
// the operator removed from a match stays removed. The reading is taken from the
// round lineups as the pass finds them, so a starting lineup an earlier pass
// seeded is one of them: it is the one v2.1.1 fell back to for a match read below
// the team's lowest round, so it changes no reading.
func (r *roundLineupSettlement) giveMatchLineups(comp *Competition, legacy map[string][]domain.TeamLineup, matches []TeamMatch) {
	settled := make(map[string]map[string]struct{}, len(comp.RoundLineupsGiven))
	for team, matchIDs := range comp.RoundLineupsGiven {
		for _, id := range matchIDs {
			if settled[team] == nil {
				settled[team] = map[string]struct{}{}
			}
			settled[team][id] = struct{}{}
		}
	}
	for _, team := range slices.Sorted(maps.Keys(legacy)) {
		for _, m := range matches {
			if !m.Seats(team) {
				continue
			}
			if _, done := settled[team][m.ID]; done {
				continue
			}
			if settled[team] == nil {
				settled[team] = map[string]struct{}{}
			}
			settled[team][m.ID] = struct{}{}
			r.givenGrew = true
			key := teamLineupMatchKey(team, m.ID)
			if _, own := r.lineups[key]; own {
				continue
			}
			src, shown := legacyReading(legacy[team], legacyLineupRound(m.DrawMatch))
			given := lineupCopy(src, team, comp.ID, m.ID, 0)
			if given.Positions == nil {
				given.Positions = map[domain.Position]string{}
			}
			r.lineups[key] = given
			r.changed = true
			if shown {
				r.notef("team %s: its round %d lineup is now its own lineup for match %s, where v2.1.1 showed it", team, src.Round, m.ID)
			} else {
				r.notef("team %s: an empty lineup is now its own lineup for match %s, where v2.1.1 showed none", team, m.ID)
			}
		}
	}
	r.given = nil
	for team, matchIDs := range settled {
		if r.given == nil {
			r.given = make(map[string][]string, len(settled))
		}
		r.given[team] = slices.Sorted(maps.Keys(matchIDs))
	}
}

// seedStartingLineups gives each legacy team that has no starting lineup (round
// 0) what v2.1.1 showed it before any round it saved, legacyReading at round 0:
// its highest round's lineup, which v2.1.1 fell back to, as its starting lineup.
// A match the team is seated in after the load and read at a round below its
// lowest saved one then finds that lineup, as v2.1.1 did, in whichever pass
// reaches it. It shows on the Lineups page and for a match the draw does not
// hold.
func (r *roundLineupSettlement) seedStartingLineups(compID string, legacy map[string][]domain.TeamLineup) {
	for _, team := range slices.Sorted(maps.Keys(legacy)) {
		if len(legacy[team]) == 0 || legacy[team][0].Round == 0 {
			continue
		}
		src, _ := legacyReading(legacy[team], 0)
		r.lineups[teamLineupKey(team, 0)] = lineupCopy(src, team, compID, "", 0)
		r.changed = true
		r.notef("team %s had no starting lineup: its round %d lineup, which v2.1.1 showed before any round it saved, is now it", team, src.Round)
	}
}

// retireRoundLineups removes each legacy team's round lineups (round >= 1) once
// the competition is completed, and counts each legacy team as waiting until
// then: a correction can seat a team in a match until it is over, and the match is
// given the lineup v2.1.1 showed there. The starting lineup stays. A team with no
// round lineup to remove waits too, since the match it is seated in later is given
// its reading all the same.
func (r *roundLineupSettlement) retireRoundLineups(legacy map[string][]domain.TeamLineup, completed bool) {
	for _, team := range slices.Sorted(maps.Keys(legacy)) {
		held := 0
		for _, l := range legacy[team] {
			if l.Round < 1 {
				continue
			}
			held++
			if completed {
				delete(r.lineups, lineupStorageKey(l))
				r.changed = true
				r.notef("the round %d lineup of team %s is removed: the competition is completed, and each match the team is seated in holds its own lineup", l.Round, team)
			}
		}
		if !completed {
			r.waiting += max(held, 1)
		}
	}
}

// roundLineupStage is what one settlement reads and writes: the competition
// files as its caller sees them (a transaction's own staged bytes first) and the
// caller's write function, so a transaction stages what a settlement decides
// with the write that seats a team and an abandoned transaction leaves
// lineups.yaml alone.
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
// changed, then the competition's record when the pairs settled grew or the first
// settlement found the legacy teams (or, when nothing waits any more, with the
// marker set and the pairs and the legacy teams cleared). saved is the record as
// it was saved, nil when nothing was. A competition that is not a team
// competition, that is already marked, or that does not exist has nothing to
// settle. Everything is read and decided before the first write, so a failure
// leaves nothing half done; a failed write is retried by the next write of the
// draw, and lineups saved before the record failed to be are found there
// already, so the next settlement only lists their pairs.
func (st *roundLineupStage) settle(onLoad bool) (saved *Competition, err error) {
	if st.store.roundLineupsSettled(st.compID) {
		return nil, nil
	}
	comp, err := st.comp()
	if err != nil || comp == nil || comp.RoundLineupsConverted || !comp.IsTeam() {
		return nil, err
	}
	if comp.ID != st.compID {
		// saveCompetitionChangedLocked paths and locks off comp.ID, not the
		// directory this call was handed: see upgradeCompetitionFormatLocked's
		// BUG 1. Leave the files as they are.
		log.Printf("state: round-lineup settlement for %s: config.md id %q does not match its directory; left unconverted", st.compID, comp.ID)
		return nil, nil
	}
	lineups, err := st.lineups()
	if err != nil {
		return nil, err
	}
	players, err := st.players(comp)
	if err != nil {
		return nil, err
	}
	pool, err := st.poolMatches()
	if err != nil {
		return nil, err
	}
	bracket, err := st.bracket()
	if err != nil {
		return nil, err
	}

	res := settleRoundLineups(comp, players, lineups, DrawMatchesFrom(pool, bracket), onLoad)

	if res.changed {
		if err := st.store.saveTeamLineupsLocked(st.compID, res.lineups, st.write); err != nil {
			return nil, err
		}
	}
	for _, note := range res.notes {
		log.Printf("state: round-lineup upgrade for %s: %s", st.compID, note)
	}
	marked := res.waiting == 0
	if !marked && !res.givenGrew && !res.legacyFound {
		return nil, nil
	}
	next := *comp
	if marked {
		next.RoundLineupsConverted = true
		next.RoundLineupsGiven = nil
		next.RoundLineupsLegacy = nil
	} else {
		next.RoundLineupsGiven = res.given
		if res.legacyFound {
			next.RoundLineupsLegacy = res.legacy
		}
	}
	if _, err := st.store.saveCompetitionChangedLocked(&next, st.write); err != nil {
		return nil, fmt.Errorf("round lineups settled but the competition's record of them was not saved: %w", err)
	}
	return &next, nil
}

// roundLineupsSettled reports, from the competition record the store already
// holds and without reading config.md, that compID has nothing to settle: it is
// not a team competition, or its marker is set. Every write of the draw asks the
// settlement, and nearly every competition is marked (a new one is, and so is one
// with no legacy team), so the question is answered without a disk read and a
// parse of the file under the lock. The cached record is checked against the
// file's modification time before it is believed, as every cached read is, and a
// record that is not cached, or is not current, answers false: the settlement
// reads config.md itself then. Caller holds the per-comp lock, which is why this
// takes only the cache's own, and a transaction's staged competition, which the
// cache holds as well, is the record it reads there.
func (s *Store) roundLineupsSettled(compID string) bool {
	cache := s.getFileCache(compID, "config.md")
	cache.mu.RLock()
	defer cache.mu.RUnlock()
	comp, _ := cache.data.(*Competition)
	if comp == nil || cache.mtime != s.FileMtime(compID, "config.md") {
		return false
	}
	return comp.RoundLineupsConverted || !comp.IsTeam()
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
// and stages the lineups it decides beside those writes so they commit or abort
// together. Once, at the end, so what a transaction later takes back (a
// rolled-back write) was never given a lineup.
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

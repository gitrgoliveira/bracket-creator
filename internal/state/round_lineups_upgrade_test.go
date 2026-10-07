package state_test

// round_lineups_upgrade_test.go pins the one-time conversion of round lineups
// into match lineups (operator decision 2026-10-06). Releases up to v2.1.1 let
// the Lineups page save a lineup for round r, and read a match's lineup as its
// own, else the round lineup with the highest round at or below the match's
// round, else the highest round there was; a match's own lineup was never
// carried to another match. A team now carries the lineup of its previous match
// instead, so a team with a lineup for a later round is given, for every match
// it is seated in, a lineup of its own for that match equal to what v2.1.1
// showed there: on load, and then in the write that seats it in a match it was
// not in. The round lineups stay while any match may still seat a team, and go,
// with the marker set, once none can.
//
// The round a match had in v2.1.1 is what it read the lineup by: a knockout
// match's index in bracket.Rounds, the 3rd-place match's len(Rounds), a pool or
// league match's stored Round (the circle-method round; -1 for a pool drawn
// without rounds, read as 0), and 0 for every Swiss match, which never stored
// one. rlV211Reading restates the reading, so the code under test is not its own
// oracle.

import (
	"errors"
	"fmt"
	"maps"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"

	"github.com/gitrgoliveira/bracket-creator/internal/domain"
	"github.com/gitrgoliveira/bracket-creator/internal/helper"
	"github.com/gitrgoliveira/bracket-creator/internal/state"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"gopkg.in/yaml.v3"
)

const rlComp = "rl"

// rlTeams are the participant ids of the teams every fixture seats. inu is the
// fifth, for a pool of five; a fixture that does not seat it leaves it unplayed.
type rlTeams struct{ tora, usagi, kuma, saru, inu string }

func newRLTeams() rlTeams {
	return rlTeams{tora: helper.NewUUID4(), usagi: helper.NewUUID4(), kuma: helper.NewUUID4(), saru: helper.NewUUID4(), inu: helper.NewUUID4()}
}

func (r rlTeams) players() []domain.Player {
	return []domain.Player{
		{ID: r.tora, Name: "Tora", Dojo: "Tora Dojo"},
		{ID: r.usagi, Name: "Usagi", Dojo: "Usagi Dojo"},
		{ID: r.kuma, Name: "Kuma", Dojo: "Kuma Dojo"},
		{ID: r.saru, Name: "Saru", Dojo: "Saru Dojo"},
		{ID: r.inu, Name: "Inu", Dojo: "Inu Dojo"},
	}
}

// names maps each team's id to its name: what a match side calls it, and what a
// tag a failure can be read by is built from.
func (r rlTeams) names() map[string]string {
	return map[string]string{r.tora: "Tora", r.usagi: "Usagi", r.kuma: "Kuma", r.saru: "Saru", r.inu: "Inu"}
}

// rlLineup is a lineup of a team whose first position holds tag, so a test can
// tell which saved lineup it is looking at, and whose member id is derived from
// it, so a copy is told from a rebuild.
func rlLineup(teamID, matchID string, round int, tag string) domain.TeamLineup {
	return domain.TeamLineup{
		TeamID: teamID, CompetitionID: rlComp, Round: round, MatchID: matchID,
		Positions: map[domain.Position]string{domain.PositionNumbered(1): tag},
		MemberIDs: map[domain.Position]string{domain.PositionNumbered(1): "member-" + tag},
	}
}

func rlTeamComp(format string) *state.Competition {
	return &state.Competition{ID: rlComp, Name: "Round Lineups", Kind: "team", TeamSize: 3, Format: format, Status: state.CompStatusKnockout}
}

// rlSeed saves a competition as v2.1.1 left it: the roster, the draw, and
// lineups.yaml written as that release's Lineups page wrote it, with no marker.
// It returns the data folder, which a fresh Store then loads.
func rlSeed(t *testing.T, comp *state.Competition, teams rlTeams, pool []state.MatchResult, bracket *state.Bracket, lineups ...domain.TeamLineup) string {
	t.Helper()
	dir := t.TempDir()
	s, err := state.NewStore(dir)
	require.NoError(t, err)
	require.NoError(t, s.SaveCompetition(comp))
	require.NoError(t, s.SaveParticipantsRestored(comp.ID, teams.players()))
	if pool != nil {
		require.NoError(t, s.SavePoolMatches(comp.ID, pool))
	}
	if bracket != nil {
		require.NoError(t, s.SaveBracket(comp.ID, bracket))
	}
	// Writing the draw may have recorded that nothing waits on a round; v2.1.1
	// recorded no such thing.
	stored, err := s.LoadCompetition(comp.ID)
	require.NoError(t, err)
	stored.RoundLineupsConverted = false
	require.NoError(t, s.SaveCompetition(stored))
	rlWriteLineups(t, dir, comp.ID, lineups...)
	return dir
}

// rlWriteLineups writes lineups.yaml in the shape v2.1.1's saveTeamLineupsLocked
// wrote: a `lineups:` list, in storage-key order.
func rlWriteLineups(t *testing.T, dir, compID string, lineups ...domain.TeamLineup) {
	t.Helper()
	data, err := yaml.Marshal(struct {
		Lineups []domain.TeamLineup `yaml:"lineups"`
	}{Lineups: lineups})
	require.NoError(t, err)
	require.NoError(t, os.WriteFile(filepath.Join(dir, "competitions", compID, "lineups.yaml"), data, 0o600))
}

func rlRaw(t *testing.T, dir, file string) string {
	t.Helper()
	raw, err := os.ReadFile(filepath.Join(dir, "competitions", rlComp, file)) // #nosec G304 -- a path under t.TempDir
	require.NoError(t, err)
	return string(raw)
}

// rlFind is the lineup stored for (teamID, matchID, round), found by fields:
// the map's own keys are the store's.
func rlFind(lineups map[string]domain.TeamLineup, teamID, matchID string, round int) (domain.TeamLineup, bool) {
	for _, l := range lineups {
		if l.TeamID == teamID && l.MatchID == matchID && l.Round == round {
			return l, true
		}
	}
	return domain.TeamLineup{}, false
}

func rlTag(l domain.TeamLineup) string { return l.Positions[domain.PositionNumbered(1)] }

// rlHas asserts a lineup is stored for the scope, carrying tag.
func rlHas(t *testing.T, lineups map[string]domain.TeamLineup, teamID, matchID string, round int, tag string) {
	t.Helper()
	l, ok := rlFind(lineups, teamID, matchID, round)
	if assert.Truef(t, ok, "no lineup for team %s match %q round %d; have %v", teamID, matchID, round, lineups) {
		assert.Equal(t, tag, rlTag(l))
	}
}

func rlLacks(t *testing.T, lineups map[string]domain.TeamLineup, teamID, matchID string, round int) {
	t.Helper()
	_, ok := rlFind(lineups, teamID, matchID, round)
	assert.Falsef(t, ok, "a lineup for team %s match %q round %d is stored; have %v", teamID, matchID, round, lineups)
}

func rlLoad(t *testing.T, store *state.Store) map[string]domain.TeamLineup {
	t.Helper()
	lineups, err := store.LoadTeamLineups(rlComp)
	require.NoError(t, err)
	return lineups
}

func rlMarker(t *testing.T, store *state.Store) bool {
	t.Helper()
	comp, err := store.LoadCompetition(rlComp)
	require.NoError(t, err)
	require.NotNil(t, comp)
	return comp.RoundLineupsConverted
}

// rlCompleted is comp in the completed status, the one a legacy team's round
// lineups are removed in.
func rlCompleted(comp *state.Competition) *state.Competition {
	comp.Status = state.CompStatusComplete
	return comp
}

// rlComplete completes the competition in a config.md write the draw's write
// hooks do not see.
func rlComplete(t *testing.T, store *state.Store) {
	t.Helper()
	_, err := store.UpdateCompetitionChanged(rlComp, func(c *state.Competition) (*state.Competition, error) {
		c.Status = state.CompStatusComplete
		return c, nil
	})
	require.NoError(t, err)
}

// rlGiven is the pairs the competition's record lists as settled: each team's
// participant id, to the ids of the matches settled for it.
func rlGiven(t *testing.T, store *state.Store) map[string][]string {
	t.Helper()
	comp, err := store.LoadCompetition(rlComp)
	require.NoError(t, err)
	require.NotNil(t, comp)
	return comp.RoundLineupsGiven
}

// rlTouchPool is a write of the draw that seats nobody new: a pool match
// started.
func rlTouchPool(t *testing.T, store *state.Store, matchID string) {
	t.Helper()
	found, err := store.UpdatePoolMatchByID(rlComp, matchID, func(m *state.MatchResult) error {
		m.Status = state.MatchStatusRunning
		return nil
	})
	require.NoError(t, err)
	require.True(t, found)
}

// rlRounds is a lineup for each round from first to last of a team, tagged
// "<name>-round<r>" with r the stored round, so a failure names the round a
// lineup was saved for.
func rlRounds(teamID, name string, first, last int) []domain.TeamLineup {
	var out []domain.TeamLineup
	for r := first; r <= last; r++ {
		out = append(out, rlLineup(teamID, "", r, fmt.Sprintf("%s-round%d", name, r)))
	}
	return out
}

// rlMatchRound is the round v2.1.1 read a match's lineup by: a knockout match's
// bracket round index (the 3rd-place match's len(Rounds)), a pool or league
// match's stored round (-1, a pool drawn without rounds, read as 0), and 0 for a
// Swiss match. Written out here rather than asked of the code under test.
func rlMatchRound(m state.DrawMatch) int {
	if m.Knockout {
		return m.Round
	}
	return max(m.PoolRound, 0)
}

// rlV211Reading is the lineup v2.1.1 showed team at a match read at round,
// from the lineups its Lineups page had saved: the team's own lineup for the
// match; else the round lineup with the highest round at or below the match's;
// else the highest round there was. A lineup entered for another match was never
// carried, so it counts for nothing here. This is state.FindBestLineup of v2.1.1,
// restated so the code under test is not its own oracle.
func rlV211Reading(legacy []domain.TeamLineup, team, matchID string, round int) string {
	for _, l := range legacy {
		if l.TeamID == team && l.MatchID == matchID {
			return rlTag(l)
		}
	}
	bestRound, highestRound := -1, -1
	var best, highest string
	for _, l := range legacy {
		if l.TeamID != team || l.MatchID != "" {
			continue
		}
		if l.Round <= round && l.Round > bestRound {
			bestRound, best = l.Round, rlTag(l)
		}
		if l.Round > highestRound {
			highestRound, highest = l.Round, rlTag(l)
		}
	}
	if bestRound >= 0 {
		return best
	}
	return highest
}

// rlAssertShowsWhatV211Showed asserts that every match of draw that seats one of
// teamIDs by id (a hidden bye aside) holds a lineup of its own for that team,
// and that it is the lineup v2.1.1 showed there. A match the team is not seated
// in is not asked about: it holds no lineup, and gets its own in the write that
// seats the team.
func rlAssertShowsWhatV211Showed(t *testing.T, lineups map[string]domain.TeamLineup, legacy []domain.TeamLineup, draw []state.DrawMatch, teamIDs ...string) {
	t.Helper()
	for _, team := range teamIDs {
		for _, m := range draw {
			if m.Hidden || !m.Seats(team) {
				continue
			}
			want := rlV211Reading(legacy, team, m.ID, rlMatchRound(m))
			got, ok := rlFind(lineups, team, m.ID, 0)
			if !assert.Truef(t, ok, "team %s has no lineup for match %s (round %d); v2.1.1 showed %q", team, m.ID, rlMatchRound(m), want) {
				continue
			}
			assert.Equalf(t, want, rlTag(got), "team %s at match %s (round %d)", team, m.ID, rlMatchRound(m))
		}
	}
}

// rlKnockout is a four-team knockout part way through: Tora won round 0 and is
// seated in the final, where Usagi is not yet, since r0-m1 is still to play.
func rlKnockout(teams rlTeams) *state.Bracket {
	return &state.Bracket{Rounds: [][]state.BracketMatch{
		{
			{ID: "r0-m0", SideA: "Tora", SideAID: teams.tora, SideB: "Kuma", SideBID: teams.kuma,
				Status: state.MatchStatusCompleted, Winner: "Tora", WinnerID: teams.tora},
			{ID: "r0-m1", SideA: "Usagi", SideAID: teams.usagi, SideB: "Saru", SideBID: teams.saru},
		},
		{{ID: "r1-m0", SideA: "Tora", SideAID: teams.tora, SideB: "Winner of r0-m1"}},
	}}
}

// rlSeatUsagi is the write that decides r0-m1 and seats Usagi in the final.
func rlSeatUsagi(teams rlTeams) func(*state.Bracket) error {
	return func(b *state.Bracket) error {
		b.Rounds[0][1].Status = state.MatchStatusCompleted
		b.Rounds[0][1].Winner, b.Rounds[0][1].WinnerID = "Usagi", teams.usagi
		b.Rounds[1][0].SideB, b.Rounds[1][0].SideBID = "Usagi", teams.usagi
		return nil
	}
}

// rlLegacyKnockoutLineups is what v2.1.1's Lineups page left for rlKnockout:
// round lineups by id for Tora and Usagi, one saved under a team's NAME (v2.0.0
// addressed an id-less team that way), and one lineup entered for a match.
func rlLegacyKnockoutLineups(teams rlTeams) []domain.TeamLineup {
	return []domain.TeamLineup{
		rlLineup(teams.tora, "", 1, "tora-round1"),
		rlLineup(teams.usagi, "", 1, "usagi-round1"),
		rlLineup("Saru", "", 0, "saru-by-name"),
		rlLineup(teams.kuma, "r0-m0", 0, "kuma-match"),
	}
}

// TestRoundLineups_LoadGivesEachSeatedMatchItsLineupAndKeepsTheRound: on load a
// team with a lineup for a later round has, for each match it is seated in, the
// lineup v2.1.1 showed there; a lineup saved under a team's name is keyed by the
// team's id; a team with no starting lineup gets what v2.1.1 showed before any
// round as one; the pairs settled are recorded; and the round lineups stay, with
// the marker unset, until the competition is completed.
func TestRoundLineups_LoadGivesEachSeatedMatchItsLineupAndKeepsTheRound(t *testing.T) {
	teams := newRLTeams()
	dir := rlSeed(t, rlTeamComp(state.CompFormatKnockout), teams, nil, rlKnockout(teams), rlLegacyKnockoutLineups(teams)...)

	store, err := state.NewStore(dir)
	require.NoError(t, err)
	lineups := rlLoad(t, store)

	t.Run("a seated team has, for each match it is seated in, the lineup v2.1.1 showed there", func(t *testing.T) {
		// Tora has a lineup for round 2 (stored 1) and none for round 1, so v2.1.1
		// showed it at the first match as well: it fell back to the highest round.
		rlHas(t, lineups, teams.tora, "r0-m0", 0, "tora-round1")
		rlHas(t, lineups, teams.tora, "r1-m0", 0, "tora-round1")
		moved, _ := rlFind(lineups, teams.tora, "r1-m0", 0)
		assert.Equal(t, "member-tora-round1", moved.MemberIDs[domain.PositionNumbered(1)], "the member ids move with it")
		assert.Equal(t, rlComp, moved.CompetitionID)
		rlHas(t, lineups, teams.usagi, "r0-m1", 0, "usagi-round1")
		rlLacks(t, lineups, teams.usagi, "r1-m0", 0)
	})

	t.Run("a team with only a later round gets, as its starting lineup, what v2.1.1 showed before any round", func(t *testing.T) {
		rlHas(t, lineups, teams.tora, "", 0, "tora-round1")
		rlHas(t, lineups, teams.usagi, "", 0, "usagi-round1")
		start, _ := rlFind(lineups, teams.tora, "", 0)
		assert.Equal(t, "member-tora-round1", start.MemberIDs[domain.PositionNumbered(1)])
	})

	t.Run("the round lineups stay until the competition is completed", func(t *testing.T) {
		rlHas(t, lineups, teams.tora, "", 1, "tora-round1")
		rlHas(t, lineups, teams.usagi, "", 1, "usagi-round1")
	})

	t.Run("a lineup saved under a team's name is keyed by its id", func(t *testing.T) {
		rlHas(t, lineups, teams.saru, "", 0, "saru-by-name")
		rlLacks(t, lineups, "Saru", "", 0)
		rlLacks(t, lineups, teams.saru, "r0-m1", 0)
	})

	t.Run("a lineup entered for a match is left as it is", func(t *testing.T) {
		rlHas(t, lineups, teams.kuma, "r0-m0", 0, "kuma-match")
	})

	t.Run("the pairs it settled are recorded in config.md, and the marker stays unset", func(t *testing.T) {
		assert.Equal(t, map[string][]string{
			teams.tora:  {"r0-m0", "r1-m0"},
			teams.usagi: {"r0-m1"},
		}, rlGiven(t, store))
		assert.Contains(t, rlRaw(t, dir, "config.md"), "round_lineups_given")
		assert.False(t, rlMarker(t, store))
		assert.NotContains(t, rlRaw(t, dir, "config.md"), "round_lineups_converted")
	})
}

// TestRoundLineups_SeatingATeamGivesItItsLineupInThatWrite: the write that seats
// a team in a match gives it its lineup for that match, in the same write, and
// records the pair. The round lineups stay: the competition is not completed, so
// a correction can still seat another team. Through the store's own door and
// through a transaction alike.
func TestRoundLineups_SeatingATeamGivesItItsLineupInThatWrite(t *testing.T) {
	loaded := func(t *testing.T) (*state.Store, rlTeams, string) {
		teams := newRLTeams()
		dir := rlSeed(t, rlTeamComp(state.CompFormatKnockout), teams, nil, rlKnockout(teams), rlLegacyKnockoutLineups(teams)...)
		store, err := state.NewStore(dir)
		require.NoError(t, err)
		require.False(t, rlMarker(t, store), "precondition: the round lineups wait for the competition to be completed")
		rlLacks(t, rlLoad(t, store), teams.usagi, "r1-m0", 0)
		return store, teams, dir
	}
	given := func(t *testing.T, store *state.Store, teams rlTeams, dir string) {
		t.Helper()
		lineups := rlLoad(t, store)
		rlHas(t, lineups, teams.usagi, "r1-m0", 0, "usagi-round1")
		rlHas(t, lineups, teams.usagi, "", 1, "usagi-round1")
		assert.Contains(t, rlGiven(t, store)[teams.usagi], "r1-m0", "the pair is recorded as settled")
		assert.Contains(t, rlRaw(t, dir, "lineups.yaml"), "matchId: r1-m0")
		assert.False(t, rlMarker(t, store), "every side is decided, but the competition is not over: a correction can still seat another team")
	}

	t.Run("a direct write", func(t *testing.T) {
		store, teams, dir := loaded(t)

		require.NoError(t, store.UpdateBracket(rlComp, rlSeatUsagi(teams)))

		given(t, store, teams, dir)
	})

	t.Run("a transaction", func(t *testing.T) {
		store, teams, dir := loaded(t)

		require.NoError(t, store.WithTransaction(rlComp, func(tx state.StoreTx) error {
			return tx.UpdateBracket(rlComp, rlSeatUsagi(teams))
		}))

		given(t, store, teams, dir)
	})

	t.Run("a write that seats nobody new gives nothing and records nothing", func(t *testing.T) {
		store, teams, dir := loaded(t)
		lineupsBefore, configBefore := rlRaw(t, dir, "lineups.yaml"), rlRaw(t, dir, "config.md")

		require.NoError(t, store.UpdateBracket(rlComp, func(b *state.Bracket) error {
			b.Rounds[0][1].Status = state.MatchStatusRunning
			return nil
		}))

		assert.Equal(t, lineupsBefore, rlRaw(t, dir, "lineups.yaml"))
		assert.Equal(t, configBefore, rlRaw(t, dir, "config.md"))
		assert.False(t, rlMarker(t, store))
		rlHas(t, rlLoad(t, store), teams.usagi, "", 1, "usagi-round1")
	})
}

// TestRoundLineups_ATransactionSettlesWhatItSees: the lineups and the
// competition a transaction has staged are the ones the settlement is made
// against, so a lineup staged earlier in the transaction and a status staged
// beside it both land with the move.
func TestRoundLineups_ATransactionSettlesWhatItSees(t *testing.T) {
	teams := newRLTeams()
	dir := rlSeed(t, rlTeamComp(state.CompFormatKnockout), teams, nil, rlKnockout(teams), rlLegacyKnockoutLineups(teams)...)
	store, err := state.NewStore(dir)
	require.NoError(t, err)

	require.NoError(t, store.WithTransaction(rlComp, func(tx state.StoreTx) error {
		require.NoError(t, tx.SetTeamLineup(rlComp, rlLineup(teams.saru, "", 0, "saru-edited"), 3))
		comp, err := tx.LoadCompetition(rlComp)
		require.NoError(t, err)
		comp.Status = state.CompStatusComplete
		require.NoError(t, tx.SaveCompetition(comp))
		return tx.UpdateBracket(rlComp, rlSeatUsagi(teams))
	}))

	lineups := rlLoad(t, store)
	rlHas(t, lineups, teams.usagi, "r1-m0", 0, "usagi-round1")
	rlHas(t, lineups, teams.saru, "", 0, "saru-edited")
	rlLacks(t, lineups, teams.usagi, "", 1)
	rlLacks(t, lineups, teams.tora, "", 1)
	comp, err := store.LoadCompetition(rlComp)
	require.NoError(t, err)
	assert.Equal(t, state.CompStatusComplete, comp.Status, "the staged status is not written over by the marker")
	assert.True(t, comp.RoundLineupsConverted, "the transaction completed the competition, so its round lineups went with it")
	assert.Empty(t, comp.RoundLineupsGiven, "and the pairs settled are not kept once the marker is set")
}

// TestRoundLineups_AnAbortedTransactionLeavesLineupsAlone: the move is staged
// with the write that seats the team, so a transaction that does not commit
// moves nothing and records nothing; the next one that does commits both.
func TestRoundLineups_AnAbortedTransactionLeavesLineupsAlone(t *testing.T) {
	teams := newRLTeams()
	dir := rlSeed(t, rlTeamComp(state.CompFormatKnockout), teams, nil, rlKnockout(teams), rlLegacyKnockoutLineups(teams)...)
	store, err := state.NewStore(dir)
	require.NoError(t, err)
	lineupsBefore := rlRaw(t, dir, "lineups.yaml")
	configBefore := rlRaw(t, dir, "config.md")
	givenBefore := rlGiven(t, store)

	err = store.WithTransaction(rlComp, func(tx state.StoreTx) error {
		require.NoError(t, tx.UpdateBracket(rlComp, rlSeatUsagi(teams)))
		return errors.New("the transaction does not commit")
	})
	require.Error(t, err)

	assert.Equal(t, lineupsBefore, rlRaw(t, dir, "lineups.yaml"))
	assert.Equal(t, configBefore, rlRaw(t, dir, "config.md"), "the pair the transaction would have settled is not recorded either")
	assert.Equal(t, givenBefore, rlGiven(t, store))
	rlHas(t, rlLoad(t, store), teams.usagi, "", 1, "usagi-round1")
	rlLacks(t, rlLoad(t, store), teams.usagi, "r1-m0", 0)
	assert.False(t, rlMarker(t, store))

	require.NoError(t, store.WithTransaction(rlComp, func(tx state.StoreTx) error {
		return tx.UpdateBracket(rlComp, rlSeatUsagi(teams))
	}))
	rlHas(t, rlLoad(t, store), teams.usagi, "r1-m0", 0, "usagi-round1")
	rlHas(t, rlLoad(t, store), teams.usagi, "", 1, "usagi-round1")
	assert.Contains(t, rlGiven(t, store)[teams.usagi], "r1-m0", "the one that commits records its pair with its lineup")
	assert.False(t, rlMarker(t, store))
}

// TestRoundLineups_AMatchesOwnLineupShadowsTheRoundOne: v2.1.1 read a match's
// own lineup before any round's, so a match the team already has a lineup for
// keeps it, and the round lineup is read for every other match the team is
// seated in; it goes only when no match can seat a team any more.
func TestRoundLineups_AMatchesOwnLineupShadowsTheRoundOne(t *testing.T) {
	teams := newRLTeams()
	legacy := []domain.TeamLineup{
		rlLineup(teams.tora, "", 1, "tora-round1"),
		rlLineup(teams.tora, "r1-m0", 0, "tora-own"),
	}
	dir := rlSeed(t, rlTeamComp(state.CompFormatKnockout), teams, nil, rlKnockout(teams), legacy...)

	store, err := state.NewStore(dir)
	require.NoError(t, err)
	lineups := rlLoad(t, store)

	rlHas(t, lineups, teams.tora, "r1-m0", 0, "tora-own")
	rlHas(t, lineups, teams.tora, "r0-m0", 0, "tora-round1")
	rlHas(t, lineups, teams.tora, "", 0, "tora-round1")
	rlHas(t, lineups, teams.tora, "", 1, "tora-round1")
	assert.False(t, rlMarker(t, store), "the round lineup is kept until the competition is completed, and the final's own lineup is not touched")
	assert.Contains(t, rlGiven(t, store)[teams.tora], "r1-m0", "a pair that already had its own lineup is settled too")

	require.NoError(t, store.UpdateBracket(rlComp, rlSeatUsagi(teams)))

	lineups = rlLoad(t, store)
	rlHas(t, lineups, teams.tora, "r1-m0", 0, "tora-own")
	rlHas(t, lineups, teams.tora, "", 1, "tora-round1")
	assert.False(t, rlMarker(t, store))
}

// TestRoundLineups_TwoRoundLineupsAreReadByTheMatchesRound: v2.1.1 read the
// round lineup with the highest round at or below a match's, else the highest
// round there was, so two round lineups of a team are each the lineup of the
// matches it was read at; the starting lineup is the highest of them, which
// v2.1.1 showed before any round.
func TestRoundLineups_TwoRoundLineupsAreReadByTheMatchesRound(t *testing.T) {
	teams := newRLTeams()
	pool := []state.MatchResult{
		rlBout(teams, "Pool A-0", 0, teams.tora, teams.usagi),
		rlBout(teams, "Pool A-1", 3, teams.tora, teams.kuma),
		rlBout(teams, "Pool A-2", 1, teams.tora, teams.saru),
	}
	legacy := []domain.TeamLineup{
		rlLineup(teams.tora, "", 1, "tora-low"),
		rlLineup(teams.tora, "", 2, "tora-high"),
	}
	dir := rlSeed(t, rlTeamComp(state.CompFormatLeague), teams, pool, nil, legacy...)

	store, err := state.NewStore(dir)
	require.NoError(t, err)
	lineups := rlLoad(t, store)

	// v2.1.1 had nothing at or below round 0 to show, so it fell back to the highest.
	rlHas(t, lineups, teams.tora, "Pool A-0", 0, "tora-high")
	rlHas(t, lineups, teams.tora, "Pool A-1", 0, "tora-high")
	rlHas(t, lineups, teams.tora, "Pool A-2", 0, "tora-low")
	rlAssertShowsWhatV211Showed(t, lineups, legacy, state.DrawMatchesFrom(pool, nil), teams.tora)
	rlHas(t, lineups, teams.tora, "", 0, "tora-high") // the starting lineup is what v2.1.1 showed before any round
	rlHas(t, lineups, teams.tora, "", 1, "tora-low")
	rlHas(t, lineups, teams.tora, "", 2, "tora-high")
	assert.False(t, rlMarker(t, store), "the league is not completed")
}

// TestRoundLineups_LoadSettlesACompletedCompetitionInOnePass: a completed
// competition loaded for the first time gives each team the lineup v2.1.1 showed
// at every match it is seated in, removes the round lineups and sets the marker,
// whatever the draw holds. Whatever a team's round, a lineup it never gets
// seated at survives only as one of those.
func TestRoundLineups_LoadSettlesACompletedCompetitionInOnePass(t *testing.T) {
	t.Run("a knockout, whose final's other side was never decided", func(t *testing.T) {
		teams := newRLTeams()
		bracket := rlKnockout(teams) // r0-m1 and the final's second side are still undecided
		dir := rlSeed(t, rlCompleted(rlTeamComp(state.CompFormatKnockout)), teams, nil, bracket,
			rlLineup(teams.tora, "", 1, "tora-round1"),
			rlLineup(teams.usagi, "", 1, "usagi-round1"),
		)

		store, err := state.NewStore(dir)
		require.NoError(t, err)
		lineups := rlLoad(t, store)

		rlHas(t, lineups, teams.tora, "r0-m0", 0, "tora-round1") // v2.1.1's fallback to the highest round, at the match Tora did play
		rlHas(t, lineups, teams.tora, "r1-m0", 0, "tora-round1")
		rlHas(t, lineups, teams.usagi, "r0-m1", 0, "usagi-round1")
		rlHas(t, lineups, teams.tora, "", 0, "tora-round1")
		rlLacks(t, lineups, teams.tora, "", 1)
		rlLacks(t, lineups, teams.usagi, "", 1)
		assert.True(t, rlMarker(t, store), "given, retired and marked in the one pass")
		assert.Empty(t, rlGiven(t, store), "and the pairs settled are not kept")
		assert.NotContains(t, rlRaw(t, dir, "config.md"), "round_lineups_given")
	})

	t.Run("a Swiss competition, whose matches all had round 0", func(t *testing.T) {
		teams := newRLTeams()
		pool := []state.MatchResult{
			{ID: "Swiss-R1-0", SideA: "Tora", SideAID: teams.tora, SideB: "Usagi", SideBID: teams.usagi},
			{ID: "Swiss-R1-1", SideA: "Kuma", SideAID: teams.kuma, SideB: "Saru", SideBID: teams.saru},
			{ID: "Swiss-R2-0", SideA: "Tora", SideAID: teams.tora, SideB: "Kuma", SideBID: teams.kuma},
		}
		dir := rlSeed(t, rlCompleted(rlTeamComp(state.CompFormatSwiss)), teams, pool, nil, rlLineup(teams.tora, "", 1, "tora-round1"))

		store, err := state.NewStore(dir)
		require.NoError(t, err)
		lineups := rlLoad(t, store)

		// No Swiss match was ever read at round 1, so v2.1.1 showed the round 1
		// lineup only as the highest round there was: at every match of the team.
		rlLacks(t, lineups, teams.tora, "", 1)
		rlHas(t, lineups, teams.tora, "", 0, "tora-round1")
		rlHas(t, lineups, teams.tora, "Swiss-R1-0", 0, "tora-round1")
		rlHas(t, lineups, teams.tora, "Swiss-R2-0", 0, "tora-round1")
		assert.Len(t, lineups, 3)
		assert.True(t, rlMarker(t, store))
	})

	t.Run("a league, whose matches had their round-robin round", func(t *testing.T) {
		teams := newRLTeams()
		pool := []state.MatchResult{
			{ID: "Pool A-0", SideA: "Tora", SideAID: teams.tora, SideB: "Usagi", SideBID: teams.usagi, Round: 0},
			{ID: "Pool A-1", SideA: "Tora", SideAID: teams.tora, SideB: "Kuma", SideBID: teams.kuma, Round: 1},
			{ID: "Pool A-2", SideA: "Usagi", SideAID: teams.usagi, SideB: "Kuma", SideBID: teams.kuma, Round: 2},
		}
		legacy := []domain.TeamLineup{
			rlLineup(teams.tora, "", 1, "tora-round1"),
			rlLineup(teams.tora, "", 2, "tora-round2"),
			rlLineup(teams.usagi, "", 2, "usagi-round2"),
		}
		dir := rlSeed(t, rlCompleted(rlTeamComp(state.CompFormatLeague)), teams, pool, nil, legacy...)

		store, err := state.NewStore(dir)
		require.NoError(t, err)
		lineups := rlLoad(t, store)

		rlHas(t, lineups, teams.tora, "Pool A-1", 0, "tora-round1")
		rlHas(t, lineups, teams.tora, "Pool A-0", 0, "tora-round2") // round 0: nothing at or below it, so the highest
		rlLacks(t, lineups, teams.tora, "Pool A-2", 0)
		rlLacks(t, lineups, teams.tora, "", 2)
		rlHas(t, lineups, teams.usagi, "Pool A-2", 0, "usagi-round2")
		rlAssertShowsWhatV211Showed(t, lineups, legacy, state.DrawMatchesFrom(pool, nil), teams.tora, teams.usagi)
		assert.True(t, rlMarker(t, store))
	})
}

// TestRoundLineups_APoolDrawnWithoutRoundsIsRoundZero: v2.1.1 stored -1 for the
// round of a pool match drawn without rounds and read it as 0, so it showed the
// starting lineup there and the round 1 lineup only at the knockout match it
// feeds, once seated.
func TestRoundLineups_APoolDrawnWithoutRoundsIsRoundZero(t *testing.T) {
	teams := newRLTeams()
	pool := []state.MatchResult{
		{ID: "Pool A-0", SideA: "Tora", SideAID: teams.tora, SideB: "Usagi", SideBID: teams.usagi, Round: -1},
		{ID: "Pool B-0", SideA: "Kuma", SideAID: teams.kuma, SideB: "Saru", SideBID: teams.saru, Round: -1},
	}
	bracket := &state.Bracket{Rounds: [][]state.BracketMatch{
		{{ID: "r0-m0", SideA: "Pool A-1st", SideB: "Pool B-1st"}},
		{{ID: "r1-m0", SideA: "Winner of r0-m0", SideB: "Pool C-1st"}},
	}}
	dir := rlSeed(t, rlTeamComp(state.CompFormatMixed), teams, pool, bracket,
		rlLineup(teams.tora, "", 0, "tora-start"),
		rlLineup(teams.tora, "", 1, "tora-round1"),
	)

	store, err := state.NewStore(dir)
	require.NoError(t, err)
	lineups := rlLoad(t, store)

	rlHas(t, lineups, teams.tora, "Pool A-0", 0, "tora-start") // read at round 0, not at -1, where nothing is at or below it
	rlHas(t, lineups, teams.tora, "", 1, "tora-round1")
	assert.False(t, rlMarker(t, store), "the knockout's slots are not filled yet: Tora may still be seated there")

	require.NoError(t, store.UpdateBracket(rlComp, func(b *state.Bracket) error {
		b.Rounds[0][0].SideA, b.Rounds[0][0].SideAID = "Tora", teams.tora
		b.Rounds[0][0].SideB, b.Rounds[0][0].SideBID = "Kuma", teams.kuma
		b.Rounds[1][0].SideA, b.Rounds[1][0].SideAID = "Tora", teams.tora
		b.Rounds[1][0].SideB, b.Rounds[1][0].SideBID = "Usagi", teams.usagi
		return nil
	}))
	lineups = rlLoad(t, store)
	rlHas(t, lineups, teams.tora, "r0-m0", 0, "tora-start")
	rlHas(t, lineups, teams.tora, "r1-m0", 0, "tora-round1")
	rlHas(t, lineups, teams.tora, "", 1, "tora-round1")
	assert.False(t, rlMarker(t, store), "every slot is filled, but the competition is not completed")
}

// TestRoundLineups_TheThirdPlaceMatchIsAfterTheLastRound: v2.1.1 read the
// 3rd-place match at len(Rounds), so a lineup for the round after the final was
// shown there, and the two semifinal losers, who play only it, are the only
// teams that match can show it for.
func TestRoundLineups_TheThirdPlaceMatchIsAfterTheLastRound(t *testing.T) {
	teams := newRLTeams()
	bracket := &state.Bracket{
		Rounds: [][]state.BracketMatch{
			{
				{ID: "r0-m0", SideA: "Tora", SideAID: teams.tora, SideB: "Kuma", SideBID: teams.kuma,
					Status: state.MatchStatusCompleted, Winner: "Tora", WinnerID: teams.tora},
				{ID: "r0-m1", SideA: "Usagi", SideAID: teams.usagi, SideB: "Saru", SideBID: teams.saru,
					Status: state.MatchStatusCompleted, Winner: "Usagi", WinnerID: teams.usagi},
			},
			{{ID: "r1-m0", SideA: "Tora", SideAID: teams.tora, SideB: "Usagi", SideBID: teams.usagi}},
		},
		ThirdPlaceMatch: &state.BracketMatch{ID: state.BronzeMatchID, SideA: "Kuma", SideAID: teams.kuma, SideB: "Saru", SideBID: teams.saru},
	}
	legacy := []domain.TeamLineup{
		rlLineup(teams.kuma, "", 2, "kuma-bronze"),
		rlLineup(teams.kuma, "", 1, "kuma-final"),
		rlLineup(teams.tora, "", 2, "tora-round2"),
		rlLineup(teams.tora, "", 1, "tora-round1"),
	}
	dir := rlSeed(t, rlCompleted(rlTeamComp(state.CompFormatKnockout)), teams, nil, bracket, legacy...)

	store, err := state.NewStore(dir)
	require.NoError(t, err)
	lineups := rlLoad(t, store)

	t.Run("a lineup for the round after the final is read at the 3rd-place match", func(t *testing.T) {
		rlHas(t, lineups, teams.kuma, state.BronzeMatchID, 0, "kuma-bronze")
		rlLacks(t, lineups, teams.kuma, "", 2)
	})
	t.Run("the lower round's lineup is read at no match its team is seated in", func(t *testing.T) {
		rlLacks(t, lineups, teams.kuma, "", 1)
		rlHas(t, lineups, teams.kuma, "r0-m0", 0, "kuma-bronze") // nothing at or below round 0: the highest round
	})
	t.Run("a team's starting lineup is what v2.1.1 showed before any round it saved: the highest", func(t *testing.T) {
		rlHas(t, lineups, teams.kuma, "", 0, "kuma-bronze")
		rlHas(t, lineups, teams.tora, "", 0, "tora-round2")
	})
	t.Run("a finalist is in no match of that round: its lineup for it is read only where v2.1.1 fell back to it", func(t *testing.T) {
		rlLacks(t, lineups, teams.tora, "", 2)
		rlHas(t, lineups, teams.tora, "r0-m0", 0, "tora-round2")
		rlHas(t, lineups, teams.tora, "r1-m0", 0, "tora-round1")
	})
	t.Run("every match shows what v2.1.1 showed", func(t *testing.T) {
		rlAssertShowsWhatV211Showed(t, lineups, legacy, state.DrawMatchesFrom(nil, bracket), teams.kuma, teams.tora)
	})
	t.Run("the competition is completed, so nothing waits", func(t *testing.T) {
		assert.True(t, rlMarker(t, store))
	})
}

// TestRoundLineups_NothingIsJudgedBeforeTheDraw: a competition not yet drawn has
// no match to give a lineup to and none to say a round lineup can never be read,
// so what a round lineup needs is only done to the starting lineup; each match
// gets its lineup when the draw seats the team.
func TestRoundLineups_NothingIsJudgedBeforeTheDraw(t *testing.T) {
	teams := newRLTeams()
	league := func(t *testing.T) (*state.Store, []state.MatchResult) {
		comp := rlTeamComp(state.CompFormatLeague)
		comp.Status = state.CompStatusSetup
		dir := rlSeed(t, comp, teams, nil, nil,
			rlLineup(teams.tora, "", 2, "tora-round2"),
			rlLineup("Usagi", "", 0, "usagi-by-name"),
		)
		store, err := state.NewStore(dir)
		require.NoError(t, err)
		lineups := rlLoad(t, store)
		rlHas(t, lineups, teams.tora, "", 2, "tora-round2")
		rlHas(t, lineups, teams.tora, "", 0, "tora-round2")
		rlHas(t, lineups, teams.usagi, "", 0, "usagi-by-name")
		require.False(t, rlMarker(t, store))
		return store, []state.MatchResult{
			{ID: "Pool A-0", SideA: "Tora", SideAID: teams.tora, SideB: "Usagi", SideBID: teams.usagi, Round: 0},
			{ID: "Pool A-1", SideA: "Tora", SideAID: teams.tora, SideB: "Kuma", SideBID: teams.kuma, Round: 2},
		}
	}

	t.Run("a draw written directly", func(t *testing.T) {
		store, pool := league(t)

		require.NoError(t, store.SavePoolMatches(rlComp, pool))

		lineups := rlLoad(t, store)
		rlHas(t, lineups, teams.tora, "Pool A-1", 0, "tora-round2")
		rlHas(t, lineups, teams.tora, "Pool A-0", 0, "tora-round2")
		rlHas(t, lineups, teams.tora, "", 2, "tora-round2")
		assert.False(t, rlMarker(t, store), "the league is drawn, not completed")
	})

	t.Run("a draw written in a transaction", func(t *testing.T) {
		store, pool := league(t)

		require.NoError(t, store.WithTransaction(rlComp, func(tx state.StoreTx) error {
			return tx.SavePoolMatches(rlComp, pool)
		}))

		lineups := rlLoad(t, store)
		rlHas(t, lineups, teams.tora, "Pool A-1", 0, "tora-round2")
		rlHas(t, lineups, teams.tora, "Pool A-0", 0, "tora-round2")
		rlHas(t, lineups, teams.tora, "", 2, "tora-round2")
		assert.False(t, rlMarker(t, store), "the league is drawn, not completed")
	})
}

// TestRoundLineups_NameKeyedLineups: a lineup whose team id is not a
// participant id is the team's only when it names exactly one team.
func TestRoundLineups_NameKeyedLineups(t *testing.T) {
	t.Run("an id-keyed lineup for the same scope is kept and the name-keyed one dropped", func(t *testing.T) {
		teams := newRLTeams()
		dir := rlSeed(t, rlTeamComp(state.CompFormatKnockout), teams, nil, rlKnockout(teams),
			rlLineup("Saru", "", 0, "saru-by-name"),
			rlLineup(teams.saru, "", 0, "saru-by-id"),
			rlLineup("Saru", "r0-m1", 0, "saru-match-by-name"),
		)

		store, err := state.NewStore(dir)
		require.NoError(t, err)
		lineups := rlLoad(t, store)

		rlHas(t, lineups, teams.saru, "", 0, "saru-by-id")
		rlHas(t, lineups, teams.saru, "r0-m1", 0, "saru-match-by-name")
		rlLacks(t, lineups, "Saru", "", 0)
		rlLacks(t, lineups, "Saru", "r0-m1", 0)
	})

	t.Run("a name matching no team is left, and a name matching two", func(t *testing.T) {
		teams := newRLTeams()
		dir := t.TempDir()
		s, err := state.NewStore(dir)
		require.NoError(t, err)
		require.NoError(t, s.SaveCompetition(rlTeamComp(state.CompFormatKnockout)))
		twin := []domain.Player{
			{ID: teams.tora, Name: "Twin", Dojo: "North"},
			{ID: teams.usagi, Name: "Twin", Dojo: "South"},
		}
		require.NoError(t, s.SaveParticipantsRestored(rlComp, twin))
		rlWriteLineups(t, dir, rlComp,
			rlLineup("Twin", "", 0, "twin"),
			rlLineup("Nobody", "", 0, "nobody"),
		)

		store, err := state.NewStore(dir)
		require.NoError(t, err)
		lineups := rlLoad(t, store)

		rlHas(t, lineups, "Twin", "", 0, "twin")
		rlHas(t, lineups, "Nobody", "", 0, "nobody")
		assert.True(t, rlMarker(t, store), "a lineup no team can claim is not one waiting for a team to be seated")
	})
}

// TestRoundLineups_ANameKeyedRoundLineupIsKeyedFirstAndThenGivenToItsMatches: a
// round lineup saved under a team's name is the team's once it is keyed by id,
// so the team is a legacy team and has the lineup v2.1.1 showed at each match.
func TestRoundLineups_ANameKeyedRoundLineupIsKeyedFirstAndThenGivenToItsMatches(t *testing.T) {
	teams := newRLTeams()
	dir := rlSeed(t, rlTeamComp(state.CompFormatKnockout), teams, nil, rlKnockout(teams),
		rlLineup("Kuma", "", 1, "kuma-round1-by-name"),
	)

	store, err := state.NewStore(dir)
	require.NoError(t, err)
	lineups := rlLoad(t, store)

	rlHas(t, lineups, teams.kuma, "", 1, "kuma-round1-by-name")
	rlLacks(t, lineups, "Kuma", "", 1)
	rlHas(t, lineups, teams.kuma, "r0-m0", 0, "kuma-round1-by-name")
	rlHas(t, lineups, teams.kuma, "", 0, "kuma-round1-by-name")
}

// TestRoundLineups_ALineupKeyedByIdOnLoadAndItsCopiesCarryTheMemberIds: the lineup
// member-id repair resolves a position's name against the squad of the team the
// lineup is stored under, and a lineup stored under a team's NAME has no squad to
// resolve against until the round-lineup step has keyed it by the team's id. The
// repair therefore runs after that step, so the re-keyed lineup, the starting
// lineup seeded from it and the lineup given to each match all carry the member
// ids, in the one load that moved them, and none is left to the next.
func TestRoundLineups_ALineupKeyedByIdOnLoadAndItsCopiesCarryTheMemberIds(t *testing.T) {
	teams := newRLTeams()
	dir := rlSeed(t, rlTeamComp(state.CompFormatKnockout), teams, nil, rlKnockout(teams))
	seed, err := state.NewStore(dir)
	require.NoError(t, err)
	sato, err := seed.AddTeamMember(rlComp, teams.kuma, "Sato")
	require.NoError(t, err)
	// Reading the competition above found no round lineup waiting and marked it,
	// which v2.1.1 never did.
	_, err = seed.UpdateCompetitionChanged(rlComp, func(c *state.Competition) (*state.Competition, error) {
		c.RoundLineupsConverted = false
		return c, nil
	})
	require.NoError(t, err)
	// v2.0.0's shape: the team's name for a key, a position's name and no id.
	rlWriteLineups(t, dir, rlComp, domain.TeamLineup{
		TeamID: "Kuma", CompetitionID: rlComp, Round: 1,
		Positions: map[domain.Position]string{domain.PositionNumbered(1): "Sato"},
	})

	store, err := state.NewStore(dir)
	require.NoError(t, err)
	lineups := rlLoad(t, store)

	for _, scope := range []struct {
		name    string
		matchID string
		round   int
	}{
		{"the round lineup, keyed by id", "", 1},
		{"the starting lineup it seeded", "", 0},
		{"the lineup given to the match it is seated in", "r0-m0", 0},
	} {
		got, ok := rlFind(lineups, teams.kuma, scope.matchID, scope.round)
		if !assert.Truef(t, ok, "%s is not stored; have %v", scope.name, lineups) {
			continue
		}
		assert.Equal(t, "Sato", got.Positions[domain.PositionNumbered(1)], scope.name)
		assert.Equal(t, sato.ID, got.MemberIDs[domain.PositionNumbered(1)], "%s carries the member id of the name it holds", scope.name)
	}
	assert.NotContains(t, rlRaw(t, dir, "lineups.yaml"), "teamId: Kuma\n", "nothing is left under the name")
}

// TestRoundLineups_TheMarkerStopsTheRepair: the repair is keyed on the marker,
// not on the lineups' shape, and an individual competition, which has no
// lineups, is neither converted nor marked.
func TestRoundLineups_TheMarkerStopsTheRepair(t *testing.T) {
	teams := newRLTeams()
	for _, tc := range []struct {
		name      string
		mark      bool
		kind      string
		teamSize  int
		converted bool
	}{
		{"unmarked team competition", false, "team", 3, true},
		{"marked team competition", true, "team", 3, false},
		{"individual competition", false, "", 0, false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			comp := rlTeamComp(state.CompFormatKnockout)
			comp.Kind, comp.TeamSize = tc.kind, tc.teamSize
			dir := rlSeed(t, comp, teams, nil, rlKnockout(teams), rlLineup(teams.tora, "", 1, "tora-round1"))
			if tc.mark {
				seed, err := state.NewStore(dir)
				require.NoError(t, err)
				stored, err := seed.LoadCompetition(rlComp)
				require.NoError(t, err)
				stored.RoundLineupsConverted = true
				require.NoError(t, seed.SaveCompetition(stored))
				rlWriteLineups(t, dir, rlComp, rlLineup(teams.tora, "", 1, "tora-round1")) // the seeding store's sweep may have moved it
			}
			before := rlRaw(t, dir, "lineups.yaml")

			store, err := state.NewStore(dir)
			require.NoError(t, err)

			if tc.converted {
				rlHas(t, rlLoad(t, store), teams.tora, "r1-m0", 0, "tora-round1")
				assert.NotEqual(t, before, rlRaw(t, dir, "lineups.yaml"))
				return
			}
			assert.Equal(t, before, rlRaw(t, dir, "lineups.yaml"), "left as it was")
			assert.Equal(t, tc.mark, rlMarker(t, store), "and its marker is not touched")
		})
	}
}

// TestRoundLineups_SeatingNeverRebuildsAStartingLineup: giving a team without
// one what v2.1.1 showed before any round it saved is the load repair's, once; a
// write that seats a team later does not bring back a starting lineup the
// operator has since removed.
func TestRoundLineups_SeatingNeverRebuildsAStartingLineup(t *testing.T) {
	for name, seat := range map[string]func(*state.Store, rlTeams) error{
		"a direct write": func(s *state.Store, teams rlTeams) error {
			return s.UpdateBracket(rlComp, rlSeatUsagi(teams))
		},
		"a transaction": func(s *state.Store, teams rlTeams) error {
			return s.WithTransaction(rlComp, func(tx state.StoreTx) error {
				return tx.UpdateBracket(rlComp, rlSeatUsagi(teams))
			})
		},
	} {
		t.Run(name, func(t *testing.T) {
			teams := newRLTeams()
			dir := rlSeed(t, rlTeamComp(state.CompFormatKnockout), teams, nil, rlKnockout(teams), rlLegacyKnockoutLineups(teams)...)
			store, err := state.NewStore(dir)
			require.NoError(t, err)
			rlHas(t, rlLoad(t, store), teams.usagi, "", 0, "usagi-round1")
			require.NoError(t, store.DeleteTeamLineup(rlComp, teams.usagi, 0))

			require.NoError(t, seat(store, teams))

			lineups := rlLoad(t, store)
			rlHas(t, lineups, teams.usagi, "r1-m0", 0, "usagi-round1")
			rlLacks(t, lineups, teams.usagi, "", 0)
		})
	}
}

// TestRoundLineups_AHiddenByeHoldsNoLineup: a structural bye is a match nobody
// plays, and the draw seats its winner at once, so a team that skips rounds is
// seated in one. A lineup is given for the matches the team plays, never for the
// bye, and the bye's empty side is not a side still to be decided.
func TestRoundLineups_AHiddenByeHoldsNoLineup(t *testing.T) {
	teams := newRLTeams()
	bracket := &state.Bracket{Rounds: [][]state.BracketMatch{
		{{ID: "r0-m0", SideA: "Kuma", SideAID: teams.kuma, SideB: "Saru", SideBID: teams.saru}},
		{
			{ID: "r1-m0", SideA: "Tora", SideAID: teams.tora, Hidden: true},
			{ID: "r1-m1", SideA: "Winner of r0-m0", SideB: "Usagi", SideBID: teams.usagi},
		},
		{{ID: "r2-m0", SideA: "Tora", SideAID: teams.tora, SideB: "Winner of r1-m1"}},
	}}
	dir := rlSeed(t, rlTeamComp(state.CompFormatKnockout), teams, nil, bracket, rlLineup(teams.tora, "", 1, "tora-round1"))

	store, err := state.NewStore(dir)
	require.NoError(t, err)
	lineups := rlLoad(t, store)

	rlHas(t, lineups, teams.tora, "r2-m0", 0, "tora-round1")
	rlLacks(t, lineups, teams.tora, "r1-m0", 0)
	rlHas(t, lineups, teams.tora, "", 1, "tora-round1")
	assert.False(t, rlMarker(t, store), "the second semi-final is not decided, so the round lineup is kept; the bye's empty side is not waited for")
}

// TestRoundLineups_ALeaguesVestigialBracketIsNotItsKnockout: a league and a
// Swiss competition have no knockout, whatever a vestigial bracket.json holds,
// so a match in it seats no one and is given no lineup.
func TestRoundLineups_ALeaguesVestigialBracketIsNotItsKnockout(t *testing.T) {
	teams := newRLTeams()
	pool := []state.MatchResult{
		{ID: "Pool A-0", SideA: "Tora", SideAID: teams.tora, SideB: "Usagi", SideBID: teams.usagi, Round: 0},
		{ID: "Pool A-1", SideA: "Kuma", SideAID: teams.kuma, SideB: "Saru", SideBID: teams.saru, Round: 1},
	}
	vestigial := &state.Bracket{Rounds: [][]state.BracketMatch{
		{{ID: "r0-m0", SideA: "Tora", SideAID: teams.tora, SideB: "Kuma", SideBID: teams.kuma}},
		{{ID: "r1-m0", SideA: "Winner of r0-m0", SideB: "Pool A-3rd"}},
	}}
	dir := rlSeed(t, rlTeamComp(state.CompFormatLeague), teams, pool, vestigial, rlLineup(teams.tora, "", 1, "tora-round1"))

	store, err := state.NewStore(dir)
	require.NoError(t, err)

	lineups := rlLoad(t, store)
	rlHas(t, lineups, teams.tora, "Pool A-0", 0, "tora-round1")
	rlLacks(t, lineups, teams.tora, "r0-m0", 0)
	rlHas(t, lineups, teams.tora, "", 1, "tora-round1")
	assert.NotContains(t, rlGiven(t, store)[teams.tora], "r0-m0", "a match in the vestigial bracket seats no one")
	assert.False(t, rlMarker(t, store), "the league is not completed")
}

// TestRoundLineups_ASwissByeHoldsNoLineup: a Swiss round with an odd number of
// teams stores its odd team out as a completed match against nobody (a side
// with neither a name nor an id). Nobody plays it, so a team is given a lineup
// for the matches it plays and never for the bye, which the Lineups page does
// not list: a lineup there could be neither seen nor removed.
func TestRoundLineups_ASwissByeHoldsNoLineup(t *testing.T) {
	teams := newRLTeams()
	comp := rlTeamComp(state.CompFormatSwiss)
	comp.SwissRounds, comp.SwissCurrentRound = 2, 2
	bye := func(id, team string) state.MatchResult {
		return state.MatchResult{ID: id, SideA: teams.names()[team], SideAID: team, Winner: teams.names()[team], WinnerID: team, Status: state.MatchStatusCompleted}
	}
	pool := []state.MatchResult{
		rlBout(teams, "Swiss-R1-0", 0, teams.tora, teams.usagi),
		bye("Swiss-R1-1", teams.kuma),
		rlBout(teams, "Swiss-R2-0", 0, teams.kuma, teams.tora),
		bye("Swiss-R2-1", teams.usagi),
	}
	dir := rlSeed(t, comp, teams, pool, nil, rlLineup(teams.kuma, "", 1, "kuma-round1"), rlLineup(teams.usagi, "", 1, "usagi-round1"))

	store, err := state.NewStore(dir)
	require.NoError(t, err)
	lineups := rlLoad(t, store)

	rlHas(t, lineups, teams.kuma, "Swiss-R2-0", 0, "kuma-round1")
	rlLacks(t, lineups, teams.kuma, "Swiss-R1-1", 0)
	rlHas(t, lineups, teams.usagi, "Swiss-R1-0", 0, "usagi-round1")
	rlLacks(t, lineups, teams.usagi, "Swiss-R2-1", 0)
	assert.Equal(t, map[string][]string{
		teams.kuma:  {"Swiss-R2-0"},
		teams.usagi: {"Swiss-R1-0"},
	}, rlGiven(t, store), "a bye is no pair to settle")
	assert.NotContains(t, rlRaw(t, dir, "lineups.yaml"), "Swiss-R1-1")
	assert.NotContains(t, rlRaw(t, dir, "lineups.yaml"), "Swiss-R2-1")
}

// TestRoundLineups_EveryDoorThatWritesTheDrawSettles: the move is made by the
// write that seats the team, whichever of the store's doors makes it, directly
// or in a transaction.
func TestRoundLineups_EveryDoorThatWritesTheDrawSettles(t *testing.T) {
	teams := newRLTeams()
	// Usagi is seated in neither Pool A-1 (Round 1) nor the final yet. Its slot
	// in Pool A-1 carries no name either: the load repair would stamp the id of
	// a side named for a unique team.
	pool := func() []state.MatchResult {
		return []state.MatchResult{
			{ID: "Pool A-0", SideA: "Kuma", SideAID: teams.kuma, SideB: "Saru", SideBID: teams.saru, Round: 0},
			{ID: "Pool A-1", SideB: "Kuma", SideBID: teams.kuma, Round: 1},
		}
	}
	seatPool := func(m *state.MatchResult) error { m.SideAID = teams.usagi; return nil }
	seatBracketMatch := func(m *state.BracketMatch) { m.SideB, m.SideBID = "Usagi", teams.usagi }

	type door struct {
		name string
		pool bool
		run  func(t *testing.T, s *state.Store)
	}
	doors := []door{
		{"SaveBracket", false, func(t *testing.T, s *state.Store) {
			b := rlKnockout(teams)
			require.NoError(t, rlSeatUsagi(teams)(b))
			require.NoError(t, s.SaveBracket(rlComp, b))
		}},
		{"UpdateBracket", false, func(t *testing.T, s *state.Store) {
			require.NoError(t, s.UpdateBracket(rlComp, rlSeatUsagi(teams)))
		}},
		{"UpdateBracketMatchByID", false, func(t *testing.T, s *state.Store) {
			found, err := s.UpdateBracketMatchByID(rlComp, "r1-m0", seatBracketMatch)
			require.NoError(t, err)
			require.True(t, found)
		}},
		{"SavePoolMatches", true, func(t *testing.T, s *state.Store) {
			p := pool()
			p[1].SideAID = teams.usagi
			require.NoError(t, s.SavePoolMatches(rlComp, p))
		}},
		{"UpdatePoolMatchByID", true, func(t *testing.T, s *state.Store) {
			found, err := s.UpdatePoolMatchByID(rlComp, "Pool A-1", seatPool)
			require.NoError(t, err)
			require.True(t, found)
		}},
		{"a transaction's SaveBracket", false, func(t *testing.T, s *state.Store) {
			b := rlKnockout(teams)
			require.NoError(t, rlSeatUsagi(teams)(b))
			require.NoError(t, s.WithTransaction(rlComp, func(tx state.StoreTx) error { return tx.SaveBracket(rlComp, b) }))
		}},
		{"a transaction's UpdateBracketMatchByID", false, func(t *testing.T, s *state.Store) {
			require.NoError(t, s.WithTransaction(rlComp, func(tx state.StoreTx) error {
				found, err := tx.UpdateBracketMatchByID(rlComp, "r1-m0", seatBracketMatch)
				require.True(t, found)
				return err
			}))
		}},
		{"a transaction's SavePoolMatches", true, func(t *testing.T, s *state.Store) {
			p := pool()
			p[1].SideAID = teams.usagi
			require.NoError(t, s.WithTransaction(rlComp, func(tx state.StoreTx) error { return tx.SavePoolMatches(rlComp, p) }))
		}},
		{"a transaction's UpdatePoolMatchByID", true, func(t *testing.T, s *state.Store) {
			require.NoError(t, s.WithTransaction(rlComp, func(tx state.StoreTx) error {
				found, err := tx.UpdatePoolMatchByID(rlComp, "Pool A-1", seatPool)
				require.True(t, found)
				return err
			}))
		}},
	}
	for _, d := range doors {
		t.Run(d.name, func(t *testing.T) {
			var dir string
			matchID := "r1-m0"
			if d.pool {
				matchID = "Pool A-1"
				dir = rlSeed(t, rlTeamComp(state.CompFormatLeague), teams, pool(), nil, rlLineup(teams.usagi, "", 1, "usagi-round1"))
			} else {
				dir = rlSeed(t, rlTeamComp(state.CompFormatKnockout), teams, nil, rlKnockout(teams), rlLineup(teams.usagi, "", 1, "usagi-round1"))
			}
			store, err := state.NewStore(dir)
			require.NoError(t, err)
			require.False(t, rlMarker(t, store), "precondition: the round lineup waits for the competition to be completed")
			rlLacks(t, rlLoad(t, store), teams.usagi, matchID, 0)

			d.run(t, store)

			lineups := rlLoad(t, store)
			rlHas(t, lineups, teams.usagi, matchID, 0, "usagi-round1")
			rlHas(t, lineups, teams.usagi, "", 1, "usagi-round1")
			assert.Contains(t, rlGiven(t, store)[teams.usagi], matchID)
			assert.False(t, rlMarker(t, store))
		})
	}
}

// TestRoundLineups_ATeamWithAStartingLineupKeepsIt: only a team without a
// starting lineup is given one; one that has it keeps it, and shows it at the
// matches v2.1.1 showed it at, its later rounds at theirs.
func TestRoundLineups_ATeamWithAStartingLineupKeepsIt(t *testing.T) {
	teams := newRLTeams()
	dir := rlSeed(t, rlTeamComp(state.CompFormatKnockout), teams, nil, rlKnockout(teams),
		rlLineup(teams.tora, "", 0, "tora-start"),
		rlLineup(teams.tora, "", 1, "tora-round1"),
	)

	store, err := state.NewStore(dir)
	require.NoError(t, err)
	lineups := rlLoad(t, store)

	rlHas(t, lineups, teams.tora, "", 0, "tora-start")
	rlHas(t, lineups, teams.tora, "r0-m0", 0, "tora-start")
	rlHas(t, lineups, teams.tora, "r1-m0", 0, "tora-round1")
	rlHas(t, lineups, teams.tora, "", 1, "tora-round1")
	assert.Len(t, lineups, 4, "the starting lineup and the round lineup, and one for each of the two matches Tora is seated in")
}

// TestRoundLineups_AFileItCannotReadLeavesTheMarkerUnset: a settlement that
// cannot read what it needs decides nothing and records nothing, so a later
// start retries, and a write of the draw is never failed by it.
func TestRoundLineups_AFileItCannotReadLeavesTheMarkerUnset(t *testing.T) {
	for _, tc := range []struct {
		name string
		file string
		// damaged is not a valid file of its kind.
		damaged string
		// write is a draw write that does not itself read the damaged file.
		write func(t *testing.T, s *state.Store, teams rlTeams)
	}{
		{"lineups.yaml", "lineups.yaml", "lineups: [this is: not: valid yaml", func(t *testing.T, s *state.Store, teams rlTeams) {
			require.NoError(t, s.UpdateBracket(rlComp, rlSeatUsagi(teams)))
		}},
		{"pool-matches.csv", "pool-matches.csv", "\"unterminated", func(t *testing.T, s *state.Store, teams rlTeams) {
			require.NoError(t, s.UpdateBracket(rlComp, rlSeatUsagi(teams)))
		}},
		{"bracket.json", "bracket.json", "{not json", func(t *testing.T, s *state.Store, teams rlTeams) {
			require.NoError(t, s.SavePoolMatches(rlComp, []state.MatchResult{{ID: "Pool A-0", SideAID: teams.tora, SideBID: teams.kuma}}))
		}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			teams := newRLTeams()
			dir := rlSeed(t, rlTeamComp(state.CompFormatKnockout), teams, nil, rlKnockout(teams), rlLegacyKnockoutLineups(teams)...)
			require.NoError(t, os.WriteFile(filepath.Join(dir, "competitions", rlComp, tc.file), []byte(tc.damaged), 0o600))

			store, err := state.NewStore(dir)
			require.NoError(t, err)
			assert.False(t, rlMarker(t, store), "the load repair could not read %s, so it decided nothing", tc.file)
			if tc.file != "lineups.yaml" {
				assert.Contains(t, rlRaw(t, dir, "lineups.yaml"), "round: 1", "and left the lineups as they were")
			}

			tc.write(t, store, teams)
			assert.False(t, rlMarker(t, store), "a write of the draw cannot settle what it cannot read, and is not failed by it")
		})
	}
}

// TestRoundLineups_ARecordWhoseIdIsNotItsDirectoryIsLeftAlone: the marker is
// saved under the record's own id, so a config.md that names another competition
// is never written to on the strength of this directory's lineups.
func TestRoundLineups_ARecordWhoseIdIsNotItsDirectoryIsLeftAlone(t *testing.T) {
	teams := newRLTeams()
	dir := rlSeed(t, rlTeamComp(state.CompFormatKnockout), teams, nil, rlKnockout(teams), rlLineup(teams.tora, "", 1, "tora-round1"))
	cfg := filepath.Join(dir, "competitions", rlComp, "config.md")
	raw, err := os.ReadFile(cfg) // #nosec G304 -- a path under t.TempDir
	require.NoError(t, err)
	require.Contains(t, string(raw), "id: rl\n")
	require.NoError(t, os.WriteFile(cfg, []byte(strings.Replace(string(raw), "id: rl\n", "id: elsewhere\n", 1)), 0o600))
	lineupsBefore := rlRaw(t, dir, "lineups.yaml")

	_, err = state.NewStore(dir)
	require.NoError(t, err)

	assert.Equal(t, lineupsBefore, rlRaw(t, dir, "lineups.yaml"))
	assert.NotContains(t, rlRaw(t, dir, "config.md"), "round_lineups_converted")
	assert.NotContains(t, rlRaw(t, dir, "config.md"), "round_lineups_given")
}

// TestRoundLineups_ReadsTheFileV211Wrote pins the bytes: lineups.yaml as v2.1.1's
// Lineups page and score sheet left it (a `lineups:` list, a team's round
// lineups, a lineup entered for a match with its `matchId`, a name-keyed lineup),
// written as text rather than through the store's own marshalling.
func TestRoundLineups_ReadsTheFileV211Wrote(t *testing.T) {
	teams := newRLTeams()
	dir := rlSeed(t, rlTeamComp(state.CompFormatKnockout), teams, nil, rlKnockout(teams))
	legacy := fmt.Sprintf(`lineups:
    - teamId: %[1]s
      competitionId: rl
      round: 1
      positions:
        "1": Sato
        "2": Ito
      memberIds:
        "1": member-sato
        "2": member-ito
    - teamId: %[2]s
      competitionId: rl
      round: 0
      matchId: r0-m0
      positions:
        "1": Endo
    - teamId: Saru
      competitionId: rl
      round: 0
      positions:
        "1": Kato
`, teams.tora, teams.kuma)
	require.NoError(t, os.WriteFile(filepath.Join(dir, "competitions", rlComp, "lineups.yaml"), []byte(legacy), 0o600))

	store, err := state.NewStore(dir)
	require.NoError(t, err)
	lineups := rlLoad(t, store)

	moved, ok := rlFind(lineups, teams.tora, "r1-m0", 0)
	require.True(t, ok)
	assert.Equal(t, map[domain.Position]string{"1": "Sato", "2": "Ito"}, moved.Positions)
	assert.Equal(t, map[domain.Position]string{"1": "member-sato", "2": "member-ito"}, moved.MemberIDs)
	start, ok := rlFind(lineups, teams.tora, "", 0)
	require.True(t, ok)
	assert.Equal(t, moved.Positions, start.Positions, "the starting lineup is a copy of what v2.1.1 showed before any round")
	rlHas(t, lineups, teams.tora, "", 1, "Sato") // kept: the competition is not completed
	rlHas(t, lineups, teams.kuma, "r0-m0", 0, "Endo")
	rlHas(t, lineups, teams.saru, "", 0, "Kato")
	assert.Contains(t, rlRaw(t, dir, "lineups.yaml"), "matchId: r1-m0", "and the file on disk says it in the shape the match lineup PUT writes")
}

// rlBout is a pool bout between two teams, numbered as a drawn pool numbers it.
func rlBout(teams rlTeams, id string, round int, a, b string) state.MatchResult {
	n := teams.names()
	return state.MatchResult{ID: id, SideA: n[a], SideAID: a, SideB: n[b], SideBID: b, Round: round}
}

// rlPoolOfFour is a pool of four numbered as a real draw numbers one: the bout
// number is the playing order, and the stored round of a team's bouts in that
// order does not rise. Tora plays rounds 1, 0, 2 (Pool A-0, A-3, A-4). It is
// pool A of a drawn eight-team mixed competition.
func rlPoolOfFour(teams rlTeams) []state.MatchResult {
	return []state.MatchResult{
		rlBout(teams, "Pool A-0", 1, teams.tora, teams.usagi),
		rlBout(teams, "Pool A-1", 0, teams.usagi, teams.kuma),
		rlBout(teams, "Pool A-2", 1, teams.kuma, teams.saru),
		rlBout(teams, "Pool A-3", 0, teams.saru, teams.tora),
		rlBout(teams, "Pool A-4", 2, teams.tora, teams.kuma),
		rlBout(teams, "Pool A-5", 2, teams.usagi, teams.saru),
	}
}

// rlPoolOfFive is the single pool of a drawn five-team league, numbered the same
// way. Tora plays rounds 2, 1, 4, 3 (Pool A-2, A-4, A-7, A-9).
func rlPoolOfFive(teams rlTeams) []state.MatchResult {
	return []state.MatchResult{
		rlBout(teams, "Pool A-0", 0, teams.kuma, teams.saru),
		rlBout(teams, "Pool A-1", 0, teams.usagi, teams.inu),
		rlBout(teams, "Pool A-2", 2, teams.tora, teams.kuma),
		rlBout(teams, "Pool A-3", 1, teams.saru, teams.inu),
		rlBout(teams, "Pool A-4", 1, teams.tora, teams.usagi),
		rlBout(teams, "Pool A-5", 3, teams.kuma, teams.inu),
		rlBout(teams, "Pool A-6", 2, teams.usagi, teams.saru),
		rlBout(teams, "Pool A-7", 4, teams.tora, teams.inu),
		rlBout(teams, "Pool A-8", 4, teams.usagi, teams.kuma),
		rlBout(teams, "Pool A-9", 3, teams.tora, teams.saru),
	}
}

// rlEveryRound is the lineups a Lineups page left when it saved a lineup for
// every round from 0 to last for each of the teams.
func rlEveryRound(teams rlTeams, last int, ids ...string) []domain.TeamLineup {
	var out []domain.TeamLineup
	for _, id := range ids {
		out = append(out, rlRounds(id, teams.names()[id], 0, last)...)
	}
	return out
}

// rlThreeRoundKnockout is a knockout part way through: Tora has won its first
// two matches and is seated in the final, whose other side waits for the second
// semi-final, which is not decided yet.
func rlThreeRoundKnockout(teams rlTeams) *state.Bracket {
	return &state.Bracket{Rounds: [][]state.BracketMatch{
		{
			{ID: "r0-m0", SideA: "Tora", SideAID: teams.tora, SideB: "Kuma", SideBID: teams.kuma,
				Status: state.MatchStatusCompleted, Winner: "Tora", WinnerID: teams.tora},
			{ID: "r0-m1", SideA: "Usagi", SideAID: teams.usagi, SideB: "Saru", SideBID: teams.saru,
				Status: state.MatchStatusCompleted, Winner: "Usagi", WinnerID: teams.usagi},
		},
		{
			{ID: "r1-m0", SideA: "Tora", SideAID: teams.tora, SideB: "Usagi", SideBID: teams.usagi,
				Status: state.MatchStatusCompleted, Winner: "Tora", WinnerID: teams.tora},
			{ID: "r1-m1", SideA: "Winner of r0-m2", SideB: "Winner of r0-m3"},
		},
		{{ID: "r2-m0", SideA: "Tora", SideAID: teams.tora, SideB: "Winner of r1-m1"}},
	}}
}

// rlDecideTheOtherSemiFinal is the write that decides the second semi-final, so
// every match of rlThreeRoundKnockout has both its sides.
func rlDecideTheOtherSemiFinal(teams rlTeams) func(*state.Bracket) error {
	return func(b *state.Bracket) error {
		b.Rounds[1][1].SideA, b.Rounds[1][1].SideAID = "Inu", teams.inu
		b.Rounds[1][1].SideB, b.Rounds[1][1].SideBID = "Kuma", teams.kuma
		b.Rounds[2][0].SideB, b.Rounds[2][0].SideBID = "Inu", teams.inu
		return nil
	}
}

// TestRoundLineups_APoolOfFourInPlayingOrderShowsWhatV211Showed: a pool's bouts
// are numbered in playing order, and a team's stored rounds in that order need
// not rise (Tora plays 1, 0, 2), so a lineup saved for a round cannot be moved
// onto "the team's first match at that round or later" without changing what
// several of its matches show. Every match a team is seated in shows what v2.1.1
// showed there: the round lineup with the highest round at or below the match's,
// else the highest round there was.
func TestRoundLineups_APoolOfFourInPlayingOrderShowsWhatV211Showed(t *testing.T) {
	teams := newRLTeams()
	four := []string{teams.tora, teams.usagi, teams.kuma, teams.saru}
	legacy := rlEveryRound(teams, 2, four...)
	pool := rlPoolOfFour(teams)
	round1And2Held := func(t *testing.T, lineups map[string]domain.TeamLineup) {
		t.Helper()
		for _, team := range four {
			for round := 1; round <= 2; round++ {
				rlHas(t, lineups, team, "", round, fmt.Sprintf("%s-round%d", teams.names()[team], round))
			}
		}
	}

	t.Run("a league that is on keeps the round lineups, though every match is held", func(t *testing.T) {
		dir := rlSeed(t, rlTeamComp(state.CompFormatLeague), teams, pool, nil, legacy...)

		store, err := state.NewStore(dir)
		require.NoError(t, err)
		lineups := rlLoad(t, store)

		rlAssertShowsWhatV211Showed(t, lineups, legacy, state.DrawMatchesFrom(pool, nil), four...)
		round1And2Held(t, lineups)
		assert.False(t, rlMarker(t, store))
	})

	t.Run("a mixed competition keeps them too", func(t *testing.T) {
		bracket := &state.Bracket{Rounds: [][]state.BracketMatch{
			{{ID: "r0-m0", SideA: "Pool A-1st", SideB: "Pool B-1st"}},
			{{ID: "r1-m0", SideA: "Winner of r0-m0", SideB: "Pool B-2nd"}},
		}}
		dir := rlSeed(t, rlTeamComp(state.CompFormatMixed), teams, pool, bracket, legacy...)

		store, err := state.NewStore(dir)
		require.NoError(t, err)
		lineups := rlLoad(t, store)

		rlAssertShowsWhatV211Showed(t, lineups, legacy, state.DrawMatchesFrom(pool, bracket), four...)
		round1And2Held(t, lineups)
		assert.False(t, rlMarker(t, store))
	})

	t.Run("a completed league gives up the round lineups with the marker", func(t *testing.T) {
		dir := rlSeed(t, rlCompleted(rlTeamComp(state.CompFormatLeague)), teams, pool, nil, legacy...)

		store, err := state.NewStore(dir)
		require.NoError(t, err)
		lineups := rlLoad(t, store)

		rlAssertShowsWhatV211Showed(t, lineups, legacy, state.DrawMatchesFrom(pool, nil), four...)
		for _, team := range four {
			rlLacks(t, lineups, team, "", 1)
			rlLacks(t, lineups, team, "", 2)
			rlHas(t, lineups, team, "", 0, teams.names()[team]+"-round0")
		}
		assert.True(t, rlMarker(t, store))
	})
}

// TestRoundLineups_AFiveTeamLeagueShowsWhatV211Showed: the same for the single
// pool of a drawn five-team league, whose bouts run in an order that is further
// from its rounds still (Tora plays rounds 2, 1, 4, 3). No match shows another
// round's lineup, and no lineup v2.1.1 showed anywhere is lost.
func TestRoundLineups_AFiveTeamLeagueShowsWhatV211Showed(t *testing.T) {
	teams := newRLTeams()
	five := []string{teams.tora, teams.usagi, teams.kuma, teams.saru, teams.inu}
	legacy := rlEveryRound(teams, 4, five...)
	pool := rlPoolOfFive(teams)
	dir := rlSeed(t, rlTeamComp(state.CompFormatLeague), teams, pool, nil, legacy...)
	roundLineupsHeld := func(lineups map[string]domain.TeamLineup) int {
		held := 0
		for _, l := range lineups {
			if l.MatchID == "" && l.Round >= 1 {
				held++
			}
		}
		return held
	}

	store, err := state.NewStore(dir)
	require.NoError(t, err)
	lineups := rlLoad(t, store)

	rlAssertShowsWhatV211Showed(t, lineups, legacy, state.DrawMatchesFrom(pool, nil), five...)
	assert.Equal(t, len(five)*4, roundLineupsHeld(lineups), "no round lineup is gone before the league is completed")
	assert.False(t, rlMarker(t, store))

	rlComplete(t, store)
	restarted, err := state.NewStore(dir)
	require.NoError(t, err)

	lineups = rlLoad(t, restarted)
	rlAssertShowsWhatV211Showed(t, lineups, legacy, state.DrawMatchesFrom(pool, nil), five...)
	assert.Zero(t, roundLineupsHeld(lineups), "the next load retires them: the match lineups hold everything v2.1.1 showed")
	assert.True(t, rlMarker(t, restarted))
}

// TestRoundLineups_ASemiFinalOnlyLineupDoesNotHideTheRoundLineupAtTheFinal: Tora
// has a lineup for round 3 (stored 2, the final) read from its round 2 lineup
// (stored 1) and one entered for its semi-final alone. v2.1.1 showed the semi-final
// lineup at the semi-final only (a match's own lineup was never carried) and the
// round 2 lineup again at the final.
func TestRoundLineups_ASemiFinalOnlyLineupDoesNotHideTheRoundLineupAtTheFinal(t *testing.T) {
	teams := newRLTeams()
	legacy := []domain.TeamLineup{
		rlLineup(teams.tora, "", 0, "tora-start"),
		rlLineup(teams.tora, "", 1, "tora-round2"),
		rlLineup(teams.tora, "r1-m0", 0, "tora-semi-final"),
	}
	bracket := rlThreeRoundKnockout(teams)
	dir := rlSeed(t, rlTeamComp(state.CompFormatKnockout), teams, nil, bracket, legacy...)

	store, err := state.NewStore(dir)
	require.NoError(t, err)
	lineups := rlLoad(t, store)

	t.Run("each match shows what v2.1.1 showed there", func(t *testing.T) {
		rlHas(t, lineups, teams.tora, "r0-m0", 0, "tora-start")
		rlHas(t, lineups, teams.tora, "r1-m0", 0, "tora-semi-final")
		rlHas(t, lineups, teams.tora, "r2-m0", 0, "tora-round2")
		rlAssertShowsWhatV211Showed(t, lineups, legacy, state.DrawMatchesFrom(nil, bracket), teams.tora)
	})

	t.Run("nothing is deleted while the competition is on", func(t *testing.T) {
		rlHas(t, lineups, teams.tora, "", 1, "tora-round2")
		assert.False(t, rlMarker(t, store))
	})

	t.Run("not when the write that decides the last side is made either", func(t *testing.T) {
		require.NoError(t, store.UpdateBracket(rlComp, rlDecideTheOtherSemiFinal(teams)))

		lineups := rlLoad(t, store)
		rlHas(t, lineups, teams.tora, "r2-m0", 0, "tora-round2")
		rlHas(t, lineups, teams.tora, "r1-m0", 0, "tora-semi-final")
		rlHas(t, lineups, teams.tora, "", 1, "tora-round2")
		assert.False(t, rlMarker(t, store))
	})

	t.Run("it goes with the completion, when a write of the draw sees it", func(t *testing.T) {
		rlComplete(t, store)
		require.NoError(t, store.UpdateBracket(rlComp, func(b *state.Bracket) error { return nil }))

		lineups := rlLoad(t, store)
		rlHas(t, lineups, teams.tora, "r2-m0", 0, "tora-round2")
		rlHas(t, lineups, teams.tora, "r1-m0", 0, "tora-semi-final")
		rlLacks(t, lineups, teams.tora, "", 1)
		assert.True(t, rlMarker(t, store))
	})
}

// TestRoundLineups_ALegacyTeamSeatedLaterGetsItsReadingInTheWriteThatSeatsIt: a
// team seated in an early match at load has the lineup v2.1.1 showed there at
// once; the write that seats it in the final gives it the one v2.1.1 showed at
// the final, which is not the first match's. Through the store's own door and
// through a transaction alike.
func TestRoundLineups_ALegacyTeamSeatedLaterGetsItsReadingInTheWriteThatSeatsIt(t *testing.T) {
	for name, seat := range map[string]func(*state.Store, rlTeams) error{
		"a direct write": func(s *state.Store, teams rlTeams) error {
			return s.UpdateBracket(rlComp, rlSeatUsagi(teams))
		},
		"a transaction": func(s *state.Store, teams rlTeams) error {
			return s.WithTransaction(rlComp, func(tx state.StoreTx) error {
				return tx.UpdateBracket(rlComp, rlSeatUsagi(teams))
			})
		},
	} {
		t.Run(name, func(t *testing.T) {
			teams := newRLTeams()
			dir := rlSeed(t, rlTeamComp(state.CompFormatKnockout), teams, nil, rlKnockout(teams),
				rlLineup(teams.usagi, "", 0, "usagi-start"),
				rlLineup(teams.usagi, "", 1, "usagi-round1"),
			)
			store, err := state.NewStore(dir)
			require.NoError(t, err)
			lineups := rlLoad(t, store)
			rlHas(t, lineups, teams.usagi, "r0-m1", 0, "usagi-start")
			rlLacks(t, lineups, teams.usagi, "r1-m0", 0)
			rlHas(t, lineups, teams.usagi, "", 1, "usagi-round1")

			require.NoError(t, seat(store, teams))

			lineups = rlLoad(t, store)
			rlHas(t, lineups, teams.usagi, "r1-m0", 0, "usagi-round1")
			rlHas(t, lineups, teams.usagi, "r0-m1", 0, "usagi-start")
			rlHas(t, lineups, teams.usagi, "", 1, "usagi-round1")
			assert.False(t, rlMarker(t, store), "the competition is not completed")
		})
	}
}

// TestRoundLineups_ATeamWithoutALaterRoundIsLeftAsItIs: only a team with a round
// lineup for round 2 or later was ever affected by the change of rule; any other
// team keeps exactly the lineups it has, and is given none for the matches it is
// seated in (its own lineups carry under the new rule).
func TestRoundLineups_ATeamWithoutALaterRoundIsLeftAsItIs(t *testing.T) {
	teams := newRLTeams()
	dir := rlSeed(t, rlTeamComp(state.CompFormatKnockout), teams, nil, rlKnockout(teams),
		rlLineup(teams.tora, "", 1, "tora-round1"),
		rlLineup(teams.kuma, "", 0, "kuma-start"),
		rlLineup(teams.kuma, "r0-m0", 0, "kuma-match"),
		rlLineup(teams.saru, "", 0, "saru-start"),
	)

	store, err := state.NewStore(dir)
	require.NoError(t, err)
	lineups := rlLoad(t, store)

	for _, tc := range []struct {
		team string
		tags map[string]string // match id ("" for the starting lineup) to tag
	}{
		{teams.kuma, map[string]string{"": "kuma-start", "r0-m0": "kuma-match"}},
		{teams.saru, map[string]string{"": "saru-start"}},
	} {
		var held int
		for _, l := range lineups {
			if l.TeamID == tc.team {
				held++
			}
		}
		assert.Equal(t, len(tc.tags), held, "team %s holds exactly the lineups it had", tc.team)
		for matchID, tag := range tc.tags {
			rlHas(t, lineups, tc.team, matchID, 0, tag)
		}
	}
	rlHas(t, lineups, teams.tora, "r0-m0", 0, "tora-round1")
}

// TestRoundLineups_TheRoundLineupsGoOnlyWhenTheCompetitionIsCompleted: the round
// lineups wait whatever the draw holds, since a correction can seat a team in a
// match until the competition is over, and are removed, with the marker set,
// when it is.
func TestRoundLineups_TheRoundLineupsGoOnlyWhenTheCompetitionIsCompleted(t *testing.T) {
	teams := newRLTeams()
	roundLineups := func() []domain.TeamLineup {
		return []domain.TeamLineup{rlLineup(teams.tora, "", 1, "tora-round1"), rlLineup(teams.usagi, "", 1, "usagi-round1")}
	}
	inStatus := func(format string, status state.CompetitionStatus) func() *state.Competition {
		return func() *state.Competition {
			c := rlTeamComp(format)
			c.Status = status
			return c
		}
	}
	four := rlPoolOfFour(teams)
	for _, tc := range []struct {
		name    string
		comp    func() *state.Competition
		pool    []state.MatchResult
		bracket *state.Bracket
		retired bool
	}{
		{"a knockout with a side still to be decided", inStatus(state.CompFormatKnockout, state.CompStatusKnockout), nil, rlKnockout(teams), false},
		{"a mixed competition whose bracket is not drawn yet", inStatus(state.CompFormatMixed, state.CompStatusPools), four, nil, false},
		{"a league whose every match has both sides", inStatus(state.CompFormatLeague, state.CompStatusPools), four, nil, false},
		{"a competition drawn and not started", inStatus(state.CompFormatLeague, state.CompStatusDrawReady), four, nil, false},
		{"a completed knockout, whatever is still undecided", inStatus(state.CompFormatKnockout, state.CompStatusComplete), nil, rlKnockout(teams), true},
		{"a completed league", inStatus(state.CompFormatLeague, state.CompStatusComplete), four, nil, true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			dir := rlSeed(t, tc.comp(), teams, tc.pool, tc.bracket, roundLineups()...)

			store, err := state.NewStore(dir)
			require.NoError(t, err)
			lineups := rlLoad(t, store)

			assert.Equal(t, tc.retired, rlMarker(t, store))
			for _, team := range []string{teams.tora, teams.usagi} {
				_, held := rlFind(lineups, team, "", 1)
				assert.Equal(t, !tc.retired, held, "a round lineup is held exactly until the competition is completed")
			}
			if tc.retired {
				assert.Empty(t, rlGiven(t, store))
			}
		})
	}
}

// TestRoundLineups_ALaterSwissRoundIsGivenWhatV211Showed: every Swiss
// match was read at round 0, and the matches of a later round are not in the draw
// until it is generated, so what v2.1.1 showed there is only known while the
// round lineups are kept: a match entered for an earlier round was never carried.
func TestRoundLineups_ALaterSwissRoundIsGivenWhatV211Showed(t *testing.T) {
	teams := newRLTeams()
	comp := rlTeamComp(state.CompFormatSwiss)
	comp.SwissRounds, comp.SwissCurrentRound = 3, 1
	round := func(n int, pairs ...[2]string) []state.MatchResult {
		var out []state.MatchResult
		for i, p := range pairs {
			out = append(out, rlBout(teams, fmt.Sprintf("Swiss-R%d-%d", n, i), 0, p[0], p[1]))
		}
		return out
	}
	one := round(1, [2]string{teams.tora, teams.usagi}, [2]string{teams.kuma, teams.saru})
	legacy := []domain.TeamLineup{
		rlLineup(teams.tora, "", 0, "tora-start"),
		rlLineup(teams.tora, "", 1, "tora-round1"),
		rlLineup(teams.tora, "Swiss-R1-0", 0, "tora-entered-for-round-1"),
	}
	dir := rlSeed(t, comp, teams, one, nil, legacy...)
	store, err := state.NewStore(dir)
	require.NoError(t, err)
	require.False(t, rlMarker(t, store), "precondition: the Swiss competition is on")
	rlHas(t, rlLoad(t, store), teams.tora, "", 1, "tora-round1")

	generate := func(n int, matches []state.MatchResult) {
		t.Helper()
		stored, err := store.LoadCompetition(rlComp)
		require.NoError(t, err)
		stored.SwissCurrentRound = n
		require.NoError(t, store.SaveCompetition(stored))
		prior, err := store.LoadPoolMatches(rlComp)
		require.NoError(t, err)
		require.NoError(t, store.SavePoolMatches(rlComp, append(prior, matches...)))
	}

	// Every Swiss match was read at round 0, and the lineup entered for round 1's
	// match was never carried to a later round: v2.1.1 showed the starting lineup
	// at each of the rounds generated later.
	generate(2, round(2, [2]string{teams.tora, teams.kuma}, [2]string{teams.usagi, teams.saru}))
	lineups := rlLoad(t, store)
	rlHas(t, lineups, teams.tora, "Swiss-R2-0", 0, "tora-start")
	rlHas(t, lineups, teams.tora, "Swiss-R1-0", 0, "tora-entered-for-round-1")
	assert.False(t, rlMarker(t, store))

	generate(3, round(3, [2]string{teams.tora, teams.saru}, [2]string{teams.usagi, teams.kuma}))
	lineups = rlLoad(t, store)
	rlHas(t, lineups, teams.tora, "Swiss-R3-0", 0, "tora-start")
	rlHas(t, lineups, teams.tora, "", 1, "tora-round1")
	assert.False(t, rlMarker(t, store), "the last round is generated and every side is decided: the competition is not completed")

	rlComplete(t, store)
	restarted, err := state.NewStore(dir)
	require.NoError(t, err)
	rlLacks(t, rlLoad(t, restarted), teams.tora, "", 1)
	assert.True(t, rlMarker(t, restarted))
}

// TestRoundLineups_SettlingAgainChangesNothing: a write that seats nobody new
// finds every match already holding its lineup, so it neither writes
// lineups.yaml nor sets the marker.
func TestRoundLineups_SettlingAgainChangesNothing(t *testing.T) {
	teams := newRLTeams()
	four := []string{teams.tora, teams.usagi, teams.kuma, teams.saru}
	bracket := &state.Bracket{Rounds: [][]state.BracketMatch{
		{{ID: "r0-m0", SideA: "Pool A-1st", SideB: "Pool B-1st"}},
		{{ID: "r1-m0", SideA: "Winner of r0-m0", SideB: "Pool B-2nd"}},
	}}
	dir := rlSeed(t, rlTeamComp(state.CompFormatMixed), teams, rlPoolOfFour(teams), bracket, rlEveryRound(teams, 2, four...)...)
	store, err := state.NewStore(dir)
	require.NoError(t, err)
	settled, settledConfig := rlRaw(t, dir, "lineups.yaml"), rlRaw(t, dir, "config.md")
	require.Contains(t, settled, "matchId: Pool A-3", "precondition: the load repair gave every match its lineup")
	require.Contains(t, settledConfig, "round_lineups_given", "and recorded the pairs")

	rlTouchPool(t, store, "Pool A-3")
	restarted, err := state.NewStore(dir)
	require.NoError(t, err)
	rlTouchPool(t, restarted, "Pool A-3")

	assert.Equal(t, settled, rlRaw(t, dir, "lineups.yaml"), "a write that seats nobody new, and a reload, give nothing")
	assert.Equal(t, settledConfig, rlRaw(t, dir, "config.md"), "and record nothing")
	assert.False(t, rlMarker(t, store))
}

// TestRoundLineups_AnIdlessRosterKeepsItsNameKeyedLineupsWaitingForIds: a roster
// recorded without ids (the id repair leaves a three-column, non-zekken roster
// alone when no pools file corroborates it) cannot be told apart from "no team has
// that name" until the operator applies the participant list and ids are minted.
// The lineups v2.0.0 saved under a team's name wait for that, with the marker
// unset, instead of being left name-keyed for good.
func TestRoundLineups_AnIdlessRosterKeepsItsNameKeyedLineupsWaitingForIds(t *testing.T) {
	dir := t.TempDir()
	seed, err := state.NewStore(dir)
	require.NoError(t, err)
	require.NoError(t, seed.SaveCompetition(&state.Competition{
		ID: rlComp, Name: "Round Lineups", Kind: "team", TeamSize: 3, Format: state.CompFormatKnockout, Status: state.CompStatusKnockout,
	}))
	roster := "Tora, Tora Dojo, 1dan\nUsagi, Usagi Dojo, 1dan\nKuma, Kuma Dojo, 1dan\nSaru, Saru Dojo, 1dan\n"
	require.NoError(t, os.WriteFile(filepath.Join(dir, "competitions", rlComp, "participants.csv"), []byte(roster), 0o600))
	require.NoError(t, seed.SaveBracket(rlComp, &state.Bracket{Rounds: [][]state.BracketMatch{
		{{ID: "r0-m0", SideA: "Tora", SideB: "Kuma"}, {ID: "r0-m1", SideA: "Usagi", SideB: "Saru"}},
		{{ID: "r1-m0", SideA: "Winner of r0-m0", SideB: "Winner of r0-m1"}},
	}}))
	stored, err := seed.LoadCompetition(rlComp)
	require.NoError(t, err)
	stored.RoundLineupsConverted = false
	require.NoError(t, seed.SaveCompetition(stored))
	rlWriteLineups(t, dir, rlComp,
		rlLineup("Tora", "", 0, "tora-start"),
		rlLineup("Tora", "", 1, "tora-round1"),
	)

	store, err := state.NewStore(dir)
	require.NoError(t, err)
	players, err := store.LoadParticipants(rlComp, false)
	require.NoError(t, err)
	require.Len(t, players, 4)
	require.Empty(t, players[0].ID, "precondition: the roster has no ids yet")

	t.Run("the load repair leaves them keyed by name and the marker unset", func(t *testing.T) {
		lineups := rlLoad(t, store)
		rlHas(t, lineups, "Tora", "", 0, "tora-start")
		rlHas(t, lineups, "Tora", "", 1, "tora-round1")
		assert.False(t, rlMarker(t, store), "a team named Tora has no id yet, so its lineups may still be re-keyed")
	})

	require.NoError(t, store.SaveParticipants(rlComp, players)) // the operator applies the participant list: ids are minted
	players, err = store.LoadParticipants(rlComp, false)
	require.NoError(t, err)
	toraID := players[0].ID
	require.NotEmpty(t, toraID)

	t.Run("the next write of the draw keys them by id", func(t *testing.T) {
		require.NoError(t, store.UpdateBracket(rlComp, func(b *state.Bracket) error {
			b.Rounds[0][1].Status = state.MatchStatusRunning
			return nil
		}))

		lineups := rlLoad(t, store)
		rlHas(t, lineups, toraID, "", 0, "tora-start")
		rlHas(t, lineups, toraID, "", 1, "tora-round1")
		rlLacks(t, lineups, "Tora", "", 0)
		rlLacks(t, lineups, "Tora", "", 1)
		assert.False(t, rlMarker(t, store), "the draw is not decided, and the team is now one whose round lineup waits")
	})

	t.Run("and a restart gives its first match the lineup v2.1.1 showed there", func(t *testing.T) {
		restarted, err := state.NewStore(dir)
		require.NoError(t, err)

		lineups, err := restarted.LoadTeamLineups(rlComp)
		require.NoError(t, err)
		rlHas(t, lineups, toraID, "r0-m0", 0, "tora-start")
		rlHas(t, lineups, toraID, "", 1, "tora-round1")
	})
}

// TestRoundLineups_ASetupRostersNameKeyedLineupsAreKeyedByTheIdsItsWriteMints: a
// team competition recorded by v2.0.0 and still in Setup has a roster with no
// ids and lineups saved under the teams' names (the Lineups page addressed a team
// with no id by its name). The roster write that mints the ids also prunes the
// lineups of teams no longer on the roster, which it read as every name-keyed
// lineup there was: it deleted them all. They are keyed by the ids the write
// mints instead, through whichever door the write came, and a lineup under a name
// no team has is still an orphan.
func TestRoundLineups_ASetupRostersNameKeyedLineupsAreKeyedByTheIdsItsWriteMints(t *testing.T) {
	seed := func(t *testing.T) (*state.Store, []domain.Player) {
		t.Helper()
		dir := t.TempDir()
		store, err := state.NewStore(dir)
		require.NoError(t, err)
		require.NoError(t, store.SaveCompetition(&state.Competition{
			ID: rlComp, Name: "Round Lineups", Kind: "team", TeamSize: 3, Format: state.CompFormatKnockout, Status: state.CompStatusSetup,
		}))
		roster := "Tora, Tora Dojo, 1dan\nUsagi, Usagi Dojo, 1dan\nKuma, Kuma Dojo, 1dan\n"
		require.NoError(t, os.WriteFile(filepath.Join(dir, "competitions", rlComp, "participants.csv"), []byte(roster), 0o600))
		rlWriteLineups(t, dir, rlComp,
			rlLineup("Tora", "", 0, "tora-start"),
			rlLineup("Tora", "", 1, "tora-round1"),
			rlLineup("Usagi", "", 0, "usagi-start"),
			rlLineup("Nobody", "", 0, "nobody-start"),
		)
		store, err = state.NewStore(dir)
		require.NoError(t, err)
		players, err := store.LoadParticipants(rlComp, false)
		require.NoError(t, err)
		require.Len(t, players, 3)
		require.Empty(t, players[0].ID, "precondition: the roster has no ids yet")
		return store, players
	}
	idOf := func(t *testing.T, store *state.Store, name string) string {
		t.Helper()
		players, err := store.LoadParticipants(rlComp, false)
		require.NoError(t, err)
		for _, p := range players {
			if p.Name == name {
				require.NotEmpty(t, p.ID)
				return p.ID
			}
		}
		require.Failf(t, "no such team", "%s", name)
		return ""
	}
	keyedByIDs := func(t *testing.T, store *state.Store) {
		t.Helper()
		lineups := rlLoad(t, store)
		rlHas(t, lineups, idOf(t, store, "Tora"), "", 0, "tora-start")
		rlHas(t, lineups, idOf(t, store, "Tora"), "", 1, "tora-round1")
		rlHas(t, lineups, idOf(t, store, "Usagi"), "", 0, "usagi-start")
		rlLacks(t, lineups, "Tora", "", 0)
		rlLacks(t, lineups, "Tora", "", 1)
		rlLacks(t, lineups, "Usagi", "", 0)
		rlLacks(t, lineups, "Nobody", "", 0)
		assert.Len(t, lineups, 3, "a lineup under a name no team has is an orphan, as it was")
	}

	t.Run("a roster saved", func(t *testing.T) {
		store, players := seed(t)

		require.NoError(t, store.SaveParticipants(rlComp, players))

		keyedByIDs(t, store)
	})

	t.Run("a participant added", func(t *testing.T) {
		store, _ := seed(t)

		_, err := store.AddParticipant(rlComp, domain.Player{Name: "Saru", Dojo: "Saru Dojo"}, false)
		require.NoError(t, err)

		keyedByIDs(t, store)
	})

	t.Run("a roster restored", func(t *testing.T) {
		store, players := seed(t)

		require.NoError(t, store.SaveParticipantsRestored(rlComp, players))

		keyedByIDs(t, store)
	})
}

// TestRoundLineups_AMatchSeatedAfterLoadReadsWhatV211ShowedAtRoundZero: a team
// with lineups for rounds 1 and 2 and none for round 0 was shown the highest of
// them by v2.1.1 at every match read at round 0 (nothing is at or below it). A
// mixed competition's knockout is read at round 0 for its first round, which a
// team is seated in only once the pools resolve, after the load: the starting
// lineup the load seeded is what that read finds, so it must be the highest.
func TestRoundLineups_AMatchSeatedAfterLoadReadsWhatV211ShowedAtRoundZero(t *testing.T) {
	teams := newRLTeams()
	bracket := &state.Bracket{Rounds: [][]state.BracketMatch{
		{{ID: "r0-m0", SideA: "Pool A-1st", SideB: "Pool B-1st"}},
		{{ID: "r1-m0", SideA: "Winner of r0-m0", SideB: "Pool B-2nd"}},
	}}
	dir := rlSeed(t, rlTeamComp(state.CompFormatMixed), teams, rlPoolOfFour(teams), bracket,
		rlLineup(teams.tora, "", 1, "tora-round1"),
		rlLineup(teams.tora, "", 2, "tora-round2"),
	)
	store, err := state.NewStore(dir)
	require.NoError(t, err)

	lineups := rlLoad(t, store)
	rlHas(t, lineups, teams.tora, "Pool A-3", 0, "tora-round2") // read at round 0: nothing at or below it
	rlHas(t, lineups, teams.tora, "Pool A-0", 0, "tora-round1") // round 1
	rlHas(t, lineups, teams.tora, "Pool A-4", 0, "tora-round2") // round 2
	rlHas(t, lineups, teams.tora, "", 0, "tora-round2")         // the starting lineup: what v2.1.1 showed before any round

	// The pools resolve: Tora is seated in the knockout's first round (read at
	// round 0), then in its second (read at round 1).
	require.NoError(t, store.UpdateBracket(rlComp, func(b *state.Bracket) error {
		b.Rounds[0][0].SideA, b.Rounds[0][0].SideAID = "Tora", teams.tora
		return nil
	}))
	rlHas(t, rlLoad(t, store), teams.tora, "r0-m0", 0, "tora-round2")
	require.NoError(t, store.UpdateBracket(rlComp, func(b *state.Bracket) error {
		b.Rounds[1][0].SideA, b.Rounds[1][0].SideAID = "Tora", teams.tora
		return nil
	}))
	rlHas(t, rlLoad(t, store), teams.tora, "r1-m0", 0, "tora-round1")
}

// TestRoundLineups_AnOperatorsRemovalOfAMatchLineupStands: "Use the previous
// match's lineup" removes a match's own lineup so the match carries the one
// before it. The pair is recorded as settled, so no later pass gives the lineup
// back: not the next write of the draw, whether it goes straight to disk or
// through a transaction, and not a reload.
func TestRoundLineups_AnOperatorsRemovalOfAMatchLineupStands(t *testing.T) {
	removed := func(t *testing.T) (store *state.Store, teams rlTeams, dir string) {
		teams = newRLTeams()
		dir = rlSeed(t, rlTeamComp(state.CompFormatKnockout), teams, nil, rlKnockout(teams), rlLineup(teams.tora, "", 1, "tora-round1"))
		store, err := state.NewStore(dir)
		require.NoError(t, err)
		rlHas(t, rlLoad(t, store), teams.tora, "r0-m0", 0, "tora-round1")
		require.NoError(t, store.DeleteTeamLineupForMatch(rlComp, teams.tora, "r0-m0"))
		return store, teams, dir
	}
	stands := func(t *testing.T, store *state.Store, teams rlTeams) {
		t.Helper()
		lineups := rlLoad(t, store)
		rlLacks(t, lineups, teams.tora, "r0-m0", 0)
		rlHas(t, lineups, teams.tora, "r1-m0", 0, "tora-round1")
		rlHas(t, lineups, teams.tora, "", 1, "tora-round1")
	}

	t.Run("a later write straight to disk", func(t *testing.T) {
		store, teams, _ := removed(t)

		require.NoError(t, store.UpdateBracket(rlComp, rlSeatUsagi(teams)))

		stands(t, store, teams)
		rlHas(t, rlLoad(t, store), teams.tora, "r1-m0", 0, "tora-round1")
	})

	t.Run("a later write in a transaction", func(t *testing.T) {
		store, teams, _ := removed(t)

		require.NoError(t, store.WithTransaction(rlComp, func(tx state.StoreTx) error {
			return tx.UpdateBracket(rlComp, rlSeatUsagi(teams))
		}))

		stands(t, store, teams)
	})

	t.Run("a reload", func(t *testing.T) {
		_, teams, dir := removed(t)

		reloaded, err := state.NewStore(dir)
		require.NoError(t, err)

		stands(t, reloaded, teams)
		assert.Contains(t, rlGiven(t, reloaded)[teams.tora], "r0-m0", "the record survives the restart")
	})
}

// TestRoundLineups_ARemovalOfALineupV211HadEnteredStandsToo: a match that already
// had a lineup of its own when the pass reached it is settled as well, so
// removing that lineup afterwards stands like any other.
func TestRoundLineups_ARemovalOfALineupV211HadEnteredStandsToo(t *testing.T) {
	teams := newRLTeams()
	dir := rlSeed(t, rlTeamComp(state.CompFormatKnockout), teams, nil, rlKnockout(teams),
		rlLineup(teams.tora, "", 1, "tora-round1"),
		rlLineup(teams.tora, "r1-m0", 0, "tora-own"),
	)
	store, err := state.NewStore(dir)
	require.NoError(t, err)
	rlHas(t, rlLoad(t, store), teams.tora, "r1-m0", 0, "tora-own")

	require.NoError(t, store.DeleteTeamLineupForMatch(rlComp, teams.tora, "r1-m0"))
	require.NoError(t, store.UpdateBracket(rlComp, rlSeatUsagi(teams)))

	rlLacks(t, rlLoad(t, store), teams.tora, "r1-m0", 0)
}

// TestRoundLineups_TheGivenPairsAreClearedWithTheMarker: the record of the pairs
// settled is only needed while the round lineups are; the pass that sets the
// marker takes it out of config.md.
func TestRoundLineups_TheGivenPairsAreClearedWithTheMarker(t *testing.T) {
	teams := newRLTeams()
	dir := rlSeed(t, rlTeamComp(state.CompFormatKnockout), teams, nil, rlKnockout(teams), rlLineup(teams.tora, "", 1, "tora-round1"))
	store, err := state.NewStore(dir)
	require.NoError(t, err)
	require.Equal(t, map[string][]string{teams.tora: {"r0-m0", "r1-m0"}}, rlGiven(t, store))
	require.Contains(t, rlRaw(t, dir, "config.md"), "round_lineups_given")

	rlComplete(t, store)
	require.NoError(t, store.UpdateBracket(rlComp, rlSeatUsagi(teams)))

	assert.True(t, rlMarker(t, store))
	assert.Empty(t, rlGiven(t, store))
	assert.NotContains(t, rlRaw(t, dir, "config.md"), "round_lineups_given")
	assert.Contains(t, rlRaw(t, dir, "config.md"), "round_lineups_converted: true")
}

// TestRoundLineups_TheGivenRecordIsReadableInConfigMd: config.md is a file people
// read and edit, so the record of the pairs settled is each team's participant id
// with the ids of the matches settled for it, sorted, as plain YAML: no key with a
// NUL byte in it, no "m:" prefix. The teams come out in the order yaml.v3 sorts a
// map's keys in, so the file reads the same on every save, and a fresh Store
// reads it back as it was.
func TestRoundLineups_TheGivenRecordIsReadableInConfigMd(t *testing.T) {
	teams := newRLTeams()
	dir := rlSeed(t, rlTeamComp(state.CompFormatKnockout), teams, nil, rlKnockout(teams),
		rlLineup(teams.tora, "", 1, "tora-round1"),
		rlLineup(teams.usagi, "", 1, "usagi-round1"),
	)
	store, err := state.NewStore(dir)
	require.NoError(t, err)

	given := map[string][]string{teams.tora: {"r0-m0", "r1-m0"}, teams.usagi: {"r0-m1"}}
	teamIDs := slices.Sorted(maps.Keys(given))
	want := "round_lineups_given:\n"
	for _, id := range teamIDs {
		want += "    " + id + ":\n"
		for _, matchID := range given[id] {
			want += "        - " + matchID + "\n"
		}
	}
	config := rlRaw(t, dir, "config.md")
	assert.Contains(t, config, want)
	assert.NotContains(t, config, "\x00", "no NUL byte")
	assert.NotContains(t, config, `\0`, "and no escaped one")
	assert.NotContains(t, config, "m:"+teams.tora, "no lineup key")
	assert.Equal(t, given, rlGiven(t, store))

	reloaded, err := state.NewStore(dir)
	require.NoError(t, err)
	assert.Equal(t, given, rlGiven(t, reloaded), "a fresh Store reads the record back as it was")
	assert.Equal(t, config, rlRaw(t, dir, "config.md"), "and settling over it writes nothing")
}

// TestRoundLineups_AHandWrittenRecordIsHonoured: a record written by hand in the
// shape config.md shows (the indentation is the writer's own) is read like one the
// server wrote: a pair it lists is left alone, and the pass that settles the rest
// writes the whole record back, sorted.
func TestRoundLineups_AHandWrittenRecordIsHonoured(t *testing.T) {
	teams := newRLTeams()
	dir := rlSeed(t, rlTeamComp(state.CompFormatKnockout), teams, nil, rlKnockout(teams), rlLineup(teams.tora, "", 1, "tora-round1"))
	cfg := filepath.Join(dir, "competitions", rlComp, "config.md")
	raw, err := os.ReadFile(cfg) // #nosec G304 -- a path under t.TempDir
	require.NoError(t, err)
	handWritten := strings.TrimSuffix(string(raw), "---\n") + "round_lineups_given:\n  " + teams.tora + ":\n    - r0-m0\n---\n"
	require.NoError(t, os.WriteFile(cfg, []byte(handWritten), 0o600))

	store, err := state.NewStore(dir)
	require.NoError(t, err)

	lineups := rlLoad(t, store)
	rlLacks(t, lineups, teams.tora, "r0-m0", 0) // listed: left alone, as an operator's removal would be
	rlHas(t, lineups, teams.tora, "r1-m0", 0, "tora-round1")
	assert.Equal(t, map[string][]string{teams.tora: {"r0-m0", "r1-m0"}}, rlGiven(t, store))
	assert.Contains(t, rlRaw(t, dir, "config.md"), "round_lineups_given:\n    "+teams.tora+":\n        - r0-m0\n        - r1-m0\n")
}

// TestRoundLineups_ACorrectionBeforeCompletionSeatsADifferentTeamWithItsReading:
// a result recorded the wrong way round is corrected before the competition is
// over, which seats another team in the final. That team has the lineup v2.1.1
// showed at the final, though every side was decided before the correction.
func TestRoundLineups_ACorrectionBeforeCompletionSeatsADifferentTeamWithItsReading(t *testing.T) {
	teams := newRLTeams()
	dir := rlSeed(t, rlTeamComp(state.CompFormatKnockout), teams, nil, rlKnockout(teams),
		rlLineup(teams.tora, "", 1, "tora-round1"),
		rlLineup(teams.usagi, "", 1, "usagi-round1"),
		rlLineup(teams.saru, "", 0, "saru-start"),
		rlLineup(teams.saru, "", 1, "saru-round1"),
	)
	store, err := state.NewStore(dir)
	require.NoError(t, err)

	require.NoError(t, store.UpdateBracket(rlComp, rlSeatUsagi(teams))) // every side of every match is decided
	lineups := rlLoad(t, store)
	rlHas(t, lineups, teams.usagi, "r1-m0", 0, "usagi-round1")
	rlLacks(t, lineups, teams.saru, "r1-m0", 0)

	require.NoError(t, store.UpdateBracket(rlComp, func(b *state.Bracket) error { // Saru won r0-m1 after all
		b.Rounds[0][1].Winner, b.Rounds[0][1].WinnerID = "Saru", teams.saru
		b.Rounds[1][0].SideB, b.Rounds[1][0].SideBID = "Saru", teams.saru
		return nil
	}))

	lineups = rlLoad(t, store)
	rlHas(t, lineups, teams.saru, "r1-m0", 0, "saru-round1")
	rlHas(t, lineups, teams.saru, "r0-m1", 0, "saru-start")
	assert.False(t, rlMarker(t, store))
}

// TestRoundLineups_ACompletionTheDrawHooksDoNotSeeIsSettledByTheNextLoad: a
// competition completes in a config.md write that none of the draw's write hooks
// sees. Its round lineups wait for the next load, which retires them and sets the
// marker.
func TestRoundLineups_ACompletionTheDrawHooksDoNotSeeIsSettledByTheNextLoad(t *testing.T) {
	teams := newRLTeams()
	four := []string{teams.tora, teams.usagi, teams.kuma, teams.saru}
	bracket := &state.Bracket{Rounds: [][]state.BracketMatch{
		{{ID: "r0-m0", SideA: "Pool A-1st", SideB: "Pool B-1st"}},
		{{ID: "r1-m0", SideA: "Winner of r0-m0", SideB: "Pool B-2nd"}},
	}}
	dir := rlSeed(t, rlTeamComp(state.CompFormatMixed), teams, rlPoolOfFour(teams), bracket, rlEveryRound(teams, 2, four...)...)
	store, err := state.NewStore(dir)
	require.NoError(t, err)

	rlComplete(t, store)

	assert.False(t, rlMarker(t, store), "nothing wrote the draw, so nothing settled")
	rlHas(t, rlLoad(t, store), teams.tora, "", 1, "Tora-round1")
	reloaded, err := state.NewStore(dir)
	require.NoError(t, err)
	lineups := rlLoad(t, reloaded)
	for _, team := range four {
		rlLacks(t, lineups, team, "", 1)
		rlLacks(t, lineups, team, "", 2)
	}
	assert.True(t, rlMarker(t, reloaded))
	assert.Empty(t, rlGiven(t, reloaded))
}

// TestRoundLineups_TheLoadStepsAfterTheSettlementDoNotWriteItAway: the load
// repair saves the competition's record from a copy it keeps for its later
// steps, so the copy has to carry what the settlement saved, or the kachinuki
// encounter-encho repair that follows (which saves the record from it) writes the
// pairs settled, or the marker, away. A kachinuki competition has that repair
// pending until it has run.
func TestRoundLineups_TheLoadStepsAfterTheSettlementDoNotWriteItAway(t *testing.T) {
	for _, tc := range []struct {
		name      string
		completed bool
	}{
		{"the pairs settled of a competition that is on", false},
		{"the marker of a completed competition", true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			teams := newRLTeams()
			comp := rlTeamComp(state.CompFormatKnockout)
			comp.TeamMatchType = state.TeamMatchTypeKachinuki
			if tc.completed {
				rlCompleted(comp)
			}
			dir := rlSeed(t, comp, teams, nil, rlKnockout(teams), rlLineup(teams.tora, "", 1, "tora-round1"))
			cfg := filepath.Join(dir, "competitions", rlComp, "config.md")
			raw, err := os.ReadFile(cfg) // #nosec G304 -- a path under t.TempDir
			require.NoError(t, err)
			// Whatever the seeding wrote, the repair is still pending: the record
			// is as a release that never ran it left it.
			require.NoError(t, os.WriteFile(cfg, []byte(strings.Replace(string(raw), "kachinuki_encounter_encho_cleared: true\n", "", 1)), 0o600))
			require.NotContains(t, rlRaw(t, dir, "config.md"), "kachinuki_encounter_encho_cleared")

			store, err := state.NewStore(dir)
			require.NoError(t, err)

			config := rlRaw(t, dir, "config.md")
			assert.Contains(t, config, "kachinuki_encounter_encho_cleared: true", "precondition: the later step ran and saved the record")
			if tc.completed {
				assert.Contains(t, config, "round_lineups_converted: true", "the marker the settlement set is still there")
				assert.NotContains(t, config, "round_lineups_given")
				assert.True(t, rlMarker(t, store))
				return
			}
			assert.Contains(t, config, "round_lineups_given", "the pairs the settlement recorded are still there")
			assert.Equal(t, map[string][]string{teams.tora: {"r0-m0", "r1-m0"}}, rlGiven(t, store))
			assert.False(t, rlMarker(t, store))
		})
	}
}

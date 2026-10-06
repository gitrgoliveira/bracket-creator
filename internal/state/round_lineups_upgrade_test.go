package state_test

// round_lineups_upgrade_test.go pins the one-time move of round lineups onto
// matches (operator decision 2026-10-06). Releases up to v2.1.1 let the
// Lineups page save a lineup for round r, and read a match's lineup as its own,
// else the highest round at or below the match's round, else the highest round
// there was. A team now carries the lineup of its previous match instead, so
// each round r >= 1 lineup is moved onto the first match of the team that v2.1.1
// would have given it to, on load and then the moment the team is seated.
//
// The round a match had in v2.1.1 is what it read the lineup by: a knockout
// match's index in bracket.Rounds, the 3rd-place match's len(Rounds), a pool or
// league match's stored Round (the circle-method round; -1 for a pool drawn
// without rounds, read as 0), and 0 for every Swiss match, which never stored
// one.

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
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

// rlTeams are the participant ids of the four teams every fixture seats.
type rlTeams struct{ tora, usagi, kuma, saru string }

func newRLTeams() rlTeams {
	return rlTeams{tora: helper.NewUUID4(), usagi: helper.NewUUID4(), kuma: helper.NewUUID4(), saru: helper.NewUUID4()}
}

func (r rlTeams) players() []domain.Player {
	return []domain.Player{
		{ID: r.tora, Name: "Tora", Dojo: "Tora Dojo"},
		{ID: r.usagi, Name: "Usagi", Dojo: "Usagi Dojo"},
		{ID: r.kuma, Name: "Kuma", Dojo: "Kuma Dojo"},
		{ID: r.saru, Name: "Saru", Dojo: "Saru Dojo"},
	}
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

// TestRoundLineups_LoadMovesWhatSeatsAndLeavesTheRest: on load a team seated at
// its round has the lineup moved onto its first match there, a lineup saved
// under a team's name is keyed by the team's id, a team with only later rounds
// gets its lowest as its starting lineup, and a team not yet seated at its
// round keeps the lineup waiting, so the marker stays unset.
func TestRoundLineups_LoadMovesWhatSeatsAndLeavesTheRest(t *testing.T) {
	teams := newRLTeams()
	dir := rlSeed(t, rlTeamComp(state.CompFormatKnockout), teams, nil, rlKnockout(teams), rlLegacyKnockoutLineups(teams)...)

	store, err := state.NewStore(dir)
	require.NoError(t, err)
	lineups := rlLoad(t, store)

	t.Run("a seated team's round lineup is its lineup for the first match it is seated in at that round", func(t *testing.T) {
		rlHas(t, lineups, teams.tora, "r1-m0", 0, "tora-round1")
		rlLacks(t, lineups, teams.tora, "", 1)
		moved, _ := rlFind(lineups, teams.tora, "r1-m0", 0)
		assert.Equal(t, "member-tora-round1", moved.MemberIDs[domain.PositionNumbered(1)], "the member ids move with it")
		assert.Equal(t, rlComp, moved.CompetitionID)
	})

	t.Run("a team with only a later round gets its lowest round as its starting lineup", func(t *testing.T) {
		rlHas(t, lineups, teams.tora, "", 0, "tora-round1")
		rlHas(t, lineups, teams.usagi, "", 0, "usagi-round1")
		start, _ := rlFind(lineups, teams.tora, "", 0)
		assert.Equal(t, "member-tora-round1", start.MemberIDs[domain.PositionNumbered(1)])
	})

	t.Run("a team not yet seated at its round keeps its round lineup waiting", func(t *testing.T) {
		rlHas(t, lineups, teams.usagi, "", 1, "usagi-round1")
		rlLacks(t, lineups, teams.usagi, "r1-m0", 0)
	})

	t.Run("a lineup saved under a team's name is keyed by its id", func(t *testing.T) {
		rlHas(t, lineups, teams.saru, "", 0, "saru-by-name")
		rlLacks(t, lineups, "Saru", "", 0)
	})

	t.Run("a lineup entered for a match is left as it is", func(t *testing.T) {
		rlHas(t, lineups, teams.kuma, "r0-m0", 0, "kuma-match")
	})

	t.Run("the marker stays unset while a lineup waits", func(t *testing.T) {
		assert.False(t, rlMarker(t, store))
		assert.NotContains(t, rlRaw(t, dir, "config.md"), "round_lineups_converted")
	})
}

// TestRoundLineups_SeatingATeamMovesItsLineupInThatWrite: the write that seats
// the waiting team moves its lineup, in the same write, and then the marker is
// set, since nothing waits any more. Through the store's own door and through a
// transaction alike.
func TestRoundLineups_SeatingATeamMovesItsLineupInThatWrite(t *testing.T) {
	loaded := func(t *testing.T) (*state.Store, rlTeams, string) {
		teams := newRLTeams()
		dir := rlSeed(t, rlTeamComp(state.CompFormatKnockout), teams, nil, rlKnockout(teams), rlLegacyKnockoutLineups(teams)...)
		store, err := state.NewStore(dir)
		require.NoError(t, err)
		require.False(t, rlMarker(t, store), "precondition: Usagi's lineup waits")
		return store, teams, dir
	}
	moved := func(t *testing.T, store *state.Store, teams rlTeams, dir string) {
		t.Helper()
		lineups := rlLoad(t, store)
		rlHas(t, lineups, teams.usagi, "r1-m0", 0, "usagi-round1")
		rlLacks(t, lineups, teams.usagi, "", 1)
		assert.True(t, rlMarker(t, store), "nothing waits, so the marker is recorded")
		assert.Contains(t, rlRaw(t, dir, "config.md"), "round_lineups_converted: true", "and persisted")
		assert.Contains(t, rlRaw(t, dir, "lineups.yaml"), "usagi-round1")
		assert.NotContains(t, rlRaw(t, dir, "lineups.yaml"), "round: 1\n", "no round 1 lineup is left on disk")
	}

	t.Run("a direct write", func(t *testing.T) {
		store, teams, dir := loaded(t)

		require.NoError(t, store.UpdateBracket(rlComp, rlSeatUsagi(teams)))

		moved(t, store, teams, dir)
	})

	t.Run("a transaction", func(t *testing.T) {
		store, teams, dir := loaded(t)

		require.NoError(t, store.WithTransaction(rlComp, func(tx state.StoreTx) error {
			return tx.UpdateBracket(rlComp, rlSeatUsagi(teams))
		}))

		moved(t, store, teams, dir)
	})

	t.Run("a write that seats nobody new moves nothing and records nothing", func(t *testing.T) {
		store, teams, dir := loaded(t)
		before := rlRaw(t, dir, "lineups.yaml")

		require.NoError(t, store.UpdateBracket(rlComp, func(b *state.Bracket) error {
			b.Rounds[0][1].Status = state.MatchStatusRunning
			return nil
		}))

		assert.Equal(t, before, rlRaw(t, dir, "lineups.yaml"))
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
	comp, err := store.LoadCompetition(rlComp)
	require.NoError(t, err)
	assert.Equal(t, state.CompStatusComplete, comp.Status, "the staged status is not written over by the marker")
	assert.True(t, comp.RoundLineupsConverted)
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

	err = store.WithTransaction(rlComp, func(tx state.StoreTx) error {
		require.NoError(t, tx.UpdateBracket(rlComp, rlSeatUsagi(teams)))
		return errors.New("the transaction does not commit")
	})
	require.Error(t, err)

	assert.Equal(t, lineupsBefore, rlRaw(t, dir, "lineups.yaml"))
	assert.Equal(t, configBefore, rlRaw(t, dir, "config.md"))
	rlHas(t, rlLoad(t, store), teams.usagi, "", 1, "usagi-round1")
	assert.False(t, rlMarker(t, store))

	require.NoError(t, store.WithTransaction(rlComp, func(tx state.StoreTx) error {
		return tx.UpdateBracket(rlComp, rlSeatUsagi(teams))
	}))
	rlHas(t, rlLoad(t, store), teams.usagi, "r1-m0", 0, "usagi-round1")
	assert.True(t, rlMarker(t, store))
}

// TestRoundLineups_AMatchesOwnLineupShadowsTheRoundOne: v2.1.1 read a match's
// own lineup before any round's, so a round lineup has nothing to say for a
// match the team already has a lineup for, and is dropped there.
func TestRoundLineups_AMatchesOwnLineupShadowsTheRoundOne(t *testing.T) {
	teams := newRLTeams()
	dir := rlSeed(t, rlTeamComp(state.CompFormatKnockout), teams, nil, rlKnockout(teams),
		rlLineup(teams.tora, "", 1, "tora-round1"),
		rlLineup(teams.tora, "r1-m0", 0, "tora-own"),
	)

	store, err := state.NewStore(dir)
	require.NoError(t, err)
	lineups := rlLoad(t, store)

	rlHas(t, lineups, teams.tora, "r1-m0", 0, "tora-own")
	rlLacks(t, lineups, teams.tora, "", 1)
	rlHas(t, lineups, teams.tora, "", 0, "tora-round1")
	assert.True(t, rlMarker(t, store), "nothing waits")
}

// TestRoundLineups_TheHigherRoundWinsAMatchBothStartOn: two round lineups of a
// team that begin on the same match: v2.1.1 read the highest round at or below
// the match's, so the higher one is the team's lineup there.
func TestRoundLineups_TheHigherRoundWinsAMatchBothStartOn(t *testing.T) {
	teams := newRLTeams()
	pool := []state.MatchResult{
		{ID: "Pool A-0", SideA: "Tora", SideAID: teams.tora, SideB: "Usagi", SideBID: teams.usagi, Round: 0},
		{ID: "Pool A-1", SideA: "Tora", SideAID: teams.tora, SideB: "Kuma", SideBID: teams.kuma, Round: 3},
	}
	dir := rlSeed(t, rlTeamComp(state.CompFormatLeague), teams, pool, nil,
		rlLineup(teams.tora, "", 1, "tora-low"),
		rlLineup(teams.tora, "", 2, "tora-high"),
	)

	store, err := state.NewStore(dir)
	require.NoError(t, err)
	lineups := rlLoad(t, store)

	rlHas(t, lineups, teams.tora, "Pool A-1", 0, "tora-high")
	rlHas(t, lineups, teams.tora, "", 0, "tora-low")
	rlLacks(t, lineups, teams.tora, "", 1)
	rlLacks(t, lineups, teams.tora, "", 2)
	assert.True(t, rlMarker(t, store))
}

// TestRoundLineups_LoadDropsWhatNoMatchCanSeat: a round lineup no match of its
// round or later can ever seat the team in is dropped, and the marker is set.
func TestRoundLineups_LoadDropsWhatNoMatchCanSeat(t *testing.T) {
	t.Run("a team knocked out before its round", func(t *testing.T) {
		teams := newRLTeams()
		bracket := rlKnockout(teams)
		require.NoError(t, rlSeatUsagi(teams)(bracket))
		// Kuma, not Tora, won r0-m0.
		bracket.Rounds[0][0].Winner, bracket.Rounds[0][0].WinnerID = "Kuma", teams.kuma
		bracket.Rounds[1][0].SideA, bracket.Rounds[1][0].SideAID = "Kuma", teams.kuma
		dir := rlSeed(t, rlTeamComp(state.CompFormatKnockout), teams, nil, bracket,
			rlLineup(teams.tora, "", 1, "tora-round1"),
			rlLineup(teams.usagi, "", 1, "usagi-round1"),
		)

		store, err := state.NewStore(dir)
		require.NoError(t, err)
		lineups := rlLoad(t, store)

		rlLacks(t, lineups, teams.tora, "", 1)
		rlLacks(t, lineups, teams.tora, "r1-m0", 0)
		rlHas(t, lineups, teams.tora, "", 0, "tora-round1")
		rlHas(t, lineups, teams.usagi, "r1-m0", 0, "usagi-round1")
		assert.True(t, rlMarker(t, store))
	})

	t.Run("a Swiss competition, whose matches all had round 0", func(t *testing.T) {
		teams := newRLTeams()
		pool := []state.MatchResult{
			{ID: "Swiss-R1-0", SideA: "Tora", SideAID: teams.tora, SideB: "Usagi", SideBID: teams.usagi},
			{ID: "Swiss-R1-1", SideA: "Kuma", SideAID: teams.kuma, SideB: "Saru", SideBID: teams.saru},
			{ID: "Swiss-R2-0", SideA: "Tora", SideAID: teams.tora, SideB: "Kuma", SideBID: teams.kuma},
		}
		dir := rlSeed(t, rlTeamComp(state.CompFormatSwiss), teams, pool, nil, rlLineup(teams.tora, "", 1, "tora-round1"))

		store, err := state.NewStore(dir)
		require.NoError(t, err)
		lineups := rlLoad(t, store)

		rlLacks(t, lineups, teams.tora, "", 1)
		rlHas(t, lineups, teams.tora, "", 0, "tora-round1")
		assert.Len(t, lineups, 1, "no match lineup is made: no Swiss match was ever read at round 1")
		assert.True(t, rlMarker(t, store))
	})

	t.Run("a league, whose matches had their round-robin round", func(t *testing.T) {
		teams := newRLTeams()
		pool := []state.MatchResult{
			{ID: "Pool A-0", SideA: "Tora", SideAID: teams.tora, SideB: "Usagi", SideBID: teams.usagi, Round: 0},
			{ID: "Pool A-1", SideA: "Tora", SideAID: teams.tora, SideB: "Kuma", SideBID: teams.kuma, Round: 1},
			{ID: "Pool A-2", SideA: "Usagi", SideAID: teams.usagi, SideB: "Kuma", SideBID: teams.kuma, Round: 2},
		}
		dir := rlSeed(t, rlTeamComp(state.CompFormatLeague), teams, pool, nil,
			rlLineup(teams.tora, "", 1, "tora-round1"),
			rlLineup(teams.tora, "", 2, "tora-round2"),
			rlLineup(teams.usagi, "", 2, "usagi-round2"),
		)

		store, err := state.NewStore(dir)
		require.NoError(t, err)
		lineups := rlLoad(t, store)

		rlHas(t, lineups, teams.tora, "Pool A-1", 0, "tora-round1")
		rlLacks(t, lineups, teams.tora, "Pool A-2", 0)
		rlLacks(t, lineups, teams.tora, "", 2)
		rlHas(t, lineups, teams.usagi, "Pool A-2", 0, "usagi-round2")
		assert.True(t, rlMarker(t, store), "Tora has no round 2 match and a league has no knockout, so that lineup is dropped")
	})
}

// TestRoundLineups_APoolDrawnWithoutRoundsIsRoundZero: v2.1.1 stored -1 for the
// round of a pool match drawn without rounds and read it as 0, so no pool match
// is where a round 1 lineup begins; the knockout it feeds is, once seated.
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
	dir := rlSeed(t, rlTeamComp(state.CompFormatMixed), teams, pool, bracket, rlLineup(teams.tora, "", 1, "tora-round1"))

	store, err := state.NewStore(dir)
	require.NoError(t, err)
	lineups := rlLoad(t, store)

	rlLacks(t, lineups, teams.tora, "Pool A-0", 0)
	rlHas(t, lineups, teams.tora, "", 1, "tora-round1")
	assert.False(t, rlMarker(t, store), "the knockout's slots are not filled yet: Tora may still be seated there")

	require.NoError(t, store.UpdateBracket(rlComp, func(b *state.Bracket) error {
		b.Rounds[0][0].SideA, b.Rounds[0][0].SideAID = "Tora", teams.tora
		b.Rounds[0][0].SideB, b.Rounds[0][0].SideBID = "Kuma", teams.kuma
		b.Rounds[1][0].SideA, b.Rounds[1][0].SideAID = "Tora", teams.tora
		b.Rounds[1][0].SideB, b.Rounds[1][0].SideBID = "Usagi", teams.usagi
		return nil
	}))
	rlHas(t, rlLoad(t, store), teams.tora, "r1-m0", 0, "tora-round1")
	assert.True(t, rlMarker(t, store))
}

// TestRoundLineups_TheThirdPlaceMatchIsAfterTheLastRound: v2.1.1 read the
// 3rd-place match at len(Rounds), so a lineup for the round after the final
// begins there, and the two semifinal losers, who play only it, are the only
// teams a lineup for that round can be moved for.
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
	dir := rlSeed(t, rlTeamComp(state.CompFormatKnockout), teams, nil, bracket,
		rlLineup(teams.kuma, "", 2, "kuma-bronze"),
		rlLineup(teams.kuma, "", 1, "kuma-final"),
		rlLineup(teams.tora, "", 2, "tora-round2"),
		rlLineup(teams.tora, "", 1, "tora-round1"),
	)

	store, err := state.NewStore(dir)
	require.NoError(t, err)
	lineups := rlLoad(t, store)

	t.Run("a lineup for the round after the final begins at the 3rd-place match", func(t *testing.T) {
		rlHas(t, lineups, teams.kuma, state.BronzeMatchID, 0, "kuma-bronze")
		rlLacks(t, lineups, teams.kuma, "", 2)
	})
	t.Run("the lower round's lineup, whose first match is the same one, is shadowed by it", func(t *testing.T) {
		rlLacks(t, lineups, teams.kuma, "", 1)
		rlHas(t, lineups, teams.kuma, "", 0, "kuma-final")
	})
	t.Run("a finalist has no match in that round, so its lineup for it is dropped", func(t *testing.T) {
		rlLacks(t, lineups, teams.tora, "", 2)
		rlHas(t, lineups, teams.tora, "r1-m0", 0, "tora-round1")
	})
	t.Run("nothing waits", func(t *testing.T) {
		assert.True(t, rlMarker(t, store))
	})
}

// TestRoundLineups_NothingIsJudgedBeforeTheDraw: a competition not yet drawn has
// no match to move a lineup onto and none to say it can never apply, so what a
// round lineup needs is only done to the starting lineup; the lineup is moved
// when the draw seats the team.
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
		rlLacks(t, lineups, teams.tora, "", 2)
		assert.True(t, rlMarker(t, store))
	})

	t.Run("a draw written in a transaction", func(t *testing.T) {
		store, pool := league(t)

		require.NoError(t, store.WithTransaction(rlComp, func(tx state.StoreTx) error {
			return tx.SavePoolMatches(rlComp, pool)
		}))

		lineups := rlLoad(t, store)
		rlHas(t, lineups, teams.tora, "Pool A-1", 0, "tora-round2")
		rlLacks(t, lineups, teams.tora, "", 2)
		assert.True(t, rlMarker(t, store))
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
// one the lowest of its round lineups is the load repair's, once; a write that
// seats a team later does not bring back a starting lineup the operator has
// since removed.
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

// TestRoundLineups_AHiddenByeIsNotWhereALineupBegins: a structural bye is a
// match nobody plays, and the draw seats its winner at once, so a team that skips
// rounds is seated in one. A lineup is moved onto the first match the team
// plays, never onto the bye.
func TestRoundLineups_AHiddenByeIsNotWhereALineupBegins(t *testing.T) {
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
	rlLacks(t, lineups, teams.tora, "", 1)
}

// TestRoundLineups_ALeaguesVestigialBracketIsNotItsKnockout: a league and a
// Swiss competition have no knockout, whatever a vestigial bracket.json holds,
// so a bracket slot still to be decided does not keep a lineup waiting.
func TestRoundLineups_ALeaguesVestigialBracketIsNotItsKnockout(t *testing.T) {
	teams := newRLTeams()
	pool := []state.MatchResult{
		{ID: "Pool A-0", SideA: "Tora", SideAID: teams.tora, SideB: "Usagi", SideBID: teams.usagi, Round: 0},
		{ID: "Pool A-1", SideA: "Kuma", SideAID: teams.kuma, SideB: "Saru", SideBID: teams.saru, Round: 1},
	}
	vestigial := &state.Bracket{Rounds: [][]state.BracketMatch{
		{{ID: "r0-m0", SideA: "Pool A-1st", SideB: "Pool A-2nd"}},
		{{ID: "r1-m0", SideA: "Winner of r0-m0", SideB: "Pool A-3rd"}},
	}}
	dir := rlSeed(t, rlTeamComp(state.CompFormatLeague), teams, pool, vestigial, rlLineup(teams.tora, "", 1, "tora-round1"))

	store, err := state.NewStore(dir)
	require.NoError(t, err)

	rlLacks(t, rlLoad(t, store), teams.tora, "", 1)
	assert.True(t, rlMarker(t, store))
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
			require.False(t, rlMarker(t, store), "precondition: the lineup waits for Usagi to be seated")

			d.run(t, store)

			rlHas(t, rlLoad(t, store), teams.usagi, matchID, 0, "usagi-round1")
			assert.True(t, rlMarker(t, store))
		})
	}
}

// TestRoundLineups_ATeamWithAStartingLineupKeepsIt: only a team without a
// starting lineup is given one; one that has it keeps it, and its later rounds
// still move onto matches.
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
	rlHas(t, lineups, teams.tora, "r1-m0", 0, "tora-round1")
	rlLacks(t, lineups, teams.tora, "", 1)
	assert.Len(t, lineups, 2)
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
	assert.Equal(t, moved.Positions, start.Positions, "the starting lineup is a copy of the lowest round's")
	rlLacks(t, lineups, teams.tora, "", 1)
	rlHas(t, lineups, teams.kuma, "r0-m0", 0, "Endo")
	rlHas(t, lineups, teams.saru, "", 0, "Kato")
	assert.Contains(t, rlRaw(t, dir, "lineups.yaml"), "matchId: r1-m0", "and the file on disk says it in the shape the match lineup PUT writes")
}

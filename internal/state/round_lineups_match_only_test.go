package state_test

// round_lineups_match_only_test.go pins the legacy teams that have a v2.1.1 MATCH
// lineup and no lineup for a round (operator decision 2026-10-07: "Show what v2.1.1
// showed"). v2.1.1 read a match's lineup as its own, never carried to another match,
// else the round lineup with the highest round at or below the match's, else the
// highest round, else none. A team now carries the lineup of its previous match, so
// such a team would show at its later matches what v2.1.1 showed only at the match it
// was entered for. The first settlement of a competition therefore treats a team
// with a match lineup, stored under its id, for a match of the current draw that
// seats it, as a legacy team too, and gives every match the team is seated in, as its
// own lineup, what v2.1.1 showed there: its starting lineup, or none (an empty
// lineup of its own) for a team that had no round lineup at all. The teams found are
// recorded in config.md, so a match lineup saved by this release never makes one.

import (
	"os"
	"path/filepath"
	"slices"
	"testing"

	"github.com/gitrgoliveira/bracket-creator/internal/domain"
	"github.com/gitrgoliveira/bracket-creator/internal/state"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// rlThreeMatches is Tora's pool: three matches in playing order, each at its own
// round, and a fourth between two other teams. Pool A-1 is fought already.
func rlThreeMatches(teams rlTeams) []state.MatchResult {
	pool := []state.MatchResult{
		rlBout(teams, "Pool A-0", 0, teams.tora, teams.usagi),
		rlBout(teams, "Pool A-1", 1, teams.tora, teams.kuma),
		rlBout(teams, "Pool A-2", 2, teams.tora, teams.saru),
		rlBout(teams, "Pool A-3", 0, teams.usagi, teams.kuma),
	}
	pool[1].Status = state.MatchStatusCompleted
	return pool
}

// rlIsEmpty asserts a lineup is stored for the scope and holds no position.
func rlIsEmpty(t *testing.T, lineups map[string]domain.TeamLineup, teamID, matchID string) {
	t.Helper()
	l, ok := rlFind(lineups, teamID, matchID, 0)
	if assert.Truef(t, ok, "no lineup for team %s match %q; have %v", teamID, matchID, lineups) {
		assert.Empty(t, l.Positions)
		assert.NotNil(t, l.Positions, "an own lineup that is empty, not a missing one")
	}
}

// TestRoundLineups_ATeamWithMatchLineupsAndAStartingLineupIsShownWhatV211Showed: v2.1.1
// showed the starting lineup at every match the team has no lineup entered for, so
// each of those matches (a fought one too) gets it as its own, and the lineups entered
// stay as they are.
func TestRoundLineups_ATeamWithMatchLineupsAndAStartingLineupIsShownWhatV211Showed(t *testing.T) {
	teams := newRLTeams()
	pool := rlThreeMatches(teams)
	legacy := []domain.TeamLineup{
		rlLineup(teams.tora, "", 0, "tora-start"),
		rlLineup(teams.tora, "Pool A-0", 0, "tora-entered-m0"),
		rlLineup(teams.tora, "Pool A-2", 0, "tora-entered-m2"),
	}
	dir := rlSeed(t, rlTeamComp(state.CompFormatLeague), teams, pool, nil, legacy...)

	store, err := state.NewStore(dir)
	require.NoError(t, err)
	lineups := rlLoad(t, store)

	rlAssertShowsWhatV211Showed(t, lineups, legacy, state.DrawMatchesFrom(pool, nil), teams.tora)
	rlHas(t, lineups, teams.tora, "Pool A-0", 0, "tora-entered-m0")
	rlHas(t, lineups, teams.tora, "Pool A-1", 0, "tora-start") // fought, and v2.1.1 showed the starting lineup there
	rlHas(t, lineups, teams.tora, "Pool A-2", 0, "tora-entered-m2")
	rlHas(t, lineups, teams.tora, "", 0, "tora-start")
	assert.Equal(t, map[string][]string{teams.tora: {"Pool A-0", "Pool A-1", "Pool A-2"}}, rlGiven(t, store))
	assert.False(t, rlMarker(t, store), "the team waits for the competition to be completed: a match it is seated in later is given its reading")
	for _, other := range []string{teams.usagi, teams.kuma, teams.saru} {
		for _, id := range []string{"Pool A-0", "Pool A-1", "Pool A-2", "Pool A-3"} {
			rlLacks(t, lineups, other, id, 0)
		}
	}
}

// A team with a match lineup and no lineup for any round was shown nothing at every
// other match: its own lineups are kept, and each of its other matches gets an empty
// lineup of its own, so none shows what an earlier match did.
func TestRoundLineups_ATeamWithMatchLineupsAndNoRoundLineupIsShownNothingElsewhere(t *testing.T) {
	teams := newRLTeams()
	pool := rlThreeMatches(teams)
	legacy := []domain.TeamLineup{rlLineup(teams.tora, "Pool A-0", 0, "tora-entered-m0")}
	dir := rlSeed(t, rlTeamComp(state.CompFormatLeague), teams, pool, nil, legacy...)

	store, err := state.NewStore(dir)
	require.NoError(t, err)
	lineups := rlLoad(t, store)

	rlAssertShowsWhatV211Showed(t, lineups, legacy, state.DrawMatchesFrom(pool, nil), teams.tora)
	rlHas(t, lineups, teams.tora, "Pool A-0", 0, "tora-entered-m0")
	rlIsEmpty(t, lineups, teams.tora, "Pool A-1")
	rlIsEmpty(t, lineups, teams.tora, "Pool A-2")
	rlLacks(t, lineups, teams.tora, "", 0)
	assert.False(t, rlMarker(t, store))
	// The stored shape of an empty own lineup: a match-scoped entry whose positions
	// are an empty map, and no member ids.
	raw := rlRaw(t, dir, "lineups.yaml")
	assert.Contains(t, raw, "matchId: Pool A-1\n      positions: {}\n")
	assert.Contains(t, raw, "matchId: Pool A-2\n      positions: {}\n")
}

// A match lineup left behind by a re-seat, for a match the team is not seated in, is
// no reading v2.1.1 showed: it makes no team legacy, and nothing waits.
func TestRoundLineups_AMatchLineupForAMatchTheTeamIsNotSeatedInMakesNoTeamLegacy(t *testing.T) {
	teams := newRLTeams()
	pool := rlThreeMatches(teams)
	dir := rlSeed(t, rlTeamComp(state.CompFormatLeague), teams, pool, nil,
		rlLineup(teams.saru, "Pool A-3", 0, "saru-left-behind"), // Pool A-3 is Usagi against Kuma
		rlLineup(teams.saru, "No Such Match", 0, "saru-no-such-match"),
	)

	store, err := state.NewStore(dir)
	require.NoError(t, err)
	lineups := rlLoad(t, store)

	rlLacks(t, lineups, teams.saru, "Pool A-2", 0)
	rlHas(t, lineups, teams.saru, "Pool A-3", 0, "saru-left-behind")
	assert.True(t, rlMarker(t, store), "no team is legacy, so nothing waits")
}

// A completed v2.1.1 competition is pinned and then marked in the one pass: nothing
// can seat a team after it, so no team waits.
func TestRoundLineups_ACompletedCompetitionIsPinnedThenMarked(t *testing.T) {
	teams := newRLTeams()
	pool := rlThreeMatches(teams)
	legacy := []domain.TeamLineup{
		rlLineup(teams.tora, "", 0, "tora-start"),
		rlLineup(teams.tora, "Pool A-0", 0, "tora-entered-m0"),
	}
	dir := rlSeed(t, rlCompleted(rlTeamComp(state.CompFormatLeague)), teams, pool, nil, legacy...)

	store, err := state.NewStore(dir)
	require.NoError(t, err)
	lineups := rlLoad(t, store)

	rlAssertShowsWhatV211Showed(t, lineups, legacy, state.DrawMatchesFrom(pool, nil), teams.tora)
	rlHas(t, lineups, teams.tora, "Pool A-1", 0, "tora-start")
	rlHas(t, lineups, teams.tora, "Pool A-2", 0, "tora-start")
	assert.True(t, rlMarker(t, store))
	assert.Empty(t, rlGiven(t, store), "the record goes with the marker")
}

// A match-only legacy team is seated later by a write of the draw and is given its
// reading in that write: the starting lineup, not what the match it was entered for
// carries.
func TestRoundLineups_AMatchOnlyLegacyTeamSeatedLaterIsGivenItsReadingInThatWrite(t *testing.T) {
	teams := newRLTeams()
	legacy := []domain.TeamLineup{
		rlLineup(teams.usagi, "", 0, "usagi-start"),
		rlLineup(teams.usagi, "r0-m1", 0, "usagi-entered-semi"),
	}
	dir := rlSeed(t, rlTeamComp(state.CompFormatKnockout), teams, nil, rlKnockout(teams), legacy...)
	store, err := state.NewStore(dir)
	require.NoError(t, err)
	lineups := rlLoad(t, store)
	rlHas(t, lineups, teams.usagi, "r0-m1", 0, "usagi-entered-semi")
	rlLacks(t, lineups, teams.usagi, "r1-m0", 0)
	require.False(t, rlMarker(t, store), "precondition: Usagi waits to be seated in the final")

	require.NoError(t, store.UpdateBracket(rlComp, rlSeatUsagi(teams)))

	lineups = rlLoad(t, store)
	rlHas(t, lineups, teams.usagi, "r1-m0", 0, "usagi-start")
	rlHas(t, lineups, teams.usagi, "r0-m1", 0, "usagi-entered-semi")
	assert.False(t, rlMarker(t, store))
}

// rlRecordedLegacy is what the first settlement recorded in config.md as the
// competition's legacy teams, sorted.
func rlRecordedLegacy(teams ...string) []string { return slices.Sorted(slices.Values(teams)) }

// The first settlement records the legacy teams, and the marker does not fire
// while one exists: it waits for the competition to be completed, with the record
// beside it in config.md.
func TestRoundLineups_TheFirstSettlementRecordsTheLegacyTeamsAndDoesNotMark(t *testing.T) {
	teams := newRLTeams()
	pool := rlThreeMatches(teams)
	dir := rlSeed(t, rlTeamComp(state.CompFormatLeague), teams, pool, nil,
		rlLineup(teams.tora, "", 1, "tora-round1"),              // a lineup for a round
		rlLineup(teams.kuma, "Pool A-1", 0, "kuma-entered-m1"),  // a lineup entered for a match it plays
		rlLineup(teams.saru, "Pool A-3", 0, "saru-left-behind"), // Pool A-3 is Usagi against Kuma
	)

	store, err := state.NewStore(dir)
	require.NoError(t, err)

	assert.Equal(t, rlRecordedLegacy(teams.tora, teams.kuma), rlLegacy(t, store), "Saru's lineup is for a match it is not seated in")
	assert.False(t, rlMarker(t, store))
	assert.Contains(t, rlRaw(t, dir, "config.md"), "round_lineups_legacy:\n")
	assert.NotContains(t, rlRaw(t, dir, "config.md"), "round_lineups_converted")
}

// A competition with no legacy team records none and is marked at once: nothing
// waits.
func TestRoundLineups_ACompetitionWithNoLegacyTeamIsMarkedAtTheFirstSettlement(t *testing.T) {
	teams := newRLTeams()
	dir := rlSeed(t, rlTeamComp(state.CompFormatLeague), teams, rlThreeMatches(teams), nil,
		rlLineup(teams.tora, "", 0, "tora-start"))

	store, err := state.NewStore(dir)
	require.NoError(t, err)

	assert.True(t, rlMarker(t, store))
	assert.Empty(t, rlLegacy(t, store))
	assert.NotContains(t, rlRaw(t, dir, "config.md"), "round_lineups_legacy")
}

// A match lineup saved by this release after the first settlement is the
// operator's own, and carries to the team's later matches: the write that seats
// Usagi in the final gives it no lineup, where the same lineup found at the first
// pass would have pinned its reading there (the starting lineup, or none).
func TestRoundLineups_AMatchLineupSavedAfterTheFirstSettlementMakesNoTeamLegacy(t *testing.T) {
	teams := newRLTeams()
	dir := rlSeed(t, rlTeamComp(state.CompFormatKnockout), teams, nil, rlKnockout(teams),
		rlLineup(teams.tora, "", 1, "tora-round1"))
	store, err := state.NewStore(dir)
	require.NoError(t, err)
	require.False(t, rlMarker(t, store), "precondition: Tora waits for the competition to be completed")
	require.Equal(t, rlRecordedLegacy(teams.tora), rlLegacy(t, store), "precondition: the first settlement recorded its legacy teams")

	require.NoError(t, store.SetTeamLineup(rlComp, rlLineup(teams.usagi, "r0-m1", 0, "usagi-saved-now"), 3))
	require.NoError(t, store.UpdateBracket(rlComp, rlSeatUsagi(teams)))

	lineups := rlLoad(t, store)
	rlHas(t, lineups, teams.usagi, "r0-m1", 0, "usagi-saved-now")
	rlLacks(t, lineups, teams.usagi, "r1-m0", 0)
	assert.Equal(t, rlRecordedLegacy(teams.tora), rlLegacy(t, store))
	assert.NotContains(t, rlGiven(t, store), teams.usagi)
	assert.False(t, rlMarker(t, store))
}

// A restart after the first pass reads the record instead of deriving the legacy
// teams again: the match lineup this release saved in between does not make its
// team legacy, and the load writes nothing.
func TestRoundLineups_ARestartAfterTheFirstSettlementDoesNotFindTheLegacyTeamsAgain(t *testing.T) {
	teams := newRLTeams()
	dir := rlSeed(t, rlTeamComp(state.CompFormatKnockout), teams, nil, rlKnockout(teams),
		rlLineup(teams.tora, "", 1, "tora-round1"))
	store, err := state.NewStore(dir)
	require.NoError(t, err)
	require.NoError(t, store.SetTeamLineup(rlComp, rlLineup(teams.usagi, "r0-m1", 0, "usagi-saved-now"), 3))
	config, lineupsFile := rlRaw(t, dir, "config.md"), rlRaw(t, dir, "lineups.yaml")

	restarted, err := state.NewStore(dir)
	require.NoError(t, err)

	assert.Equal(t, config, rlRaw(t, dir, "config.md"), "the load writes nothing to config.md")
	assert.Equal(t, lineupsFile, rlRaw(t, dir, "lineups.yaml"), "or to lineups.yaml")
	assert.Equal(t, rlRecordedLegacy(teams.tora), rlLegacy(t, restarted))
	require.NoError(t, restarted.UpdateBracket(rlComp, rlSeatUsagi(teams)))
	rlLacks(t, rlLoad(t, restarted), teams.usagi, "r1-m0", 0)
	assert.Equal(t, rlRecordedLegacy(teams.tora), rlLegacy(t, restarted))
}

// A discard (or a new draw) takes the match lineups with it, and a team that was
// legacy only for them has nothing left to be shown: it leaves the record, and the
// marker is set when no legacy team remains. A team with a lineup for a round
// stays, since the next draw's matches are given theirs from it.
func TestRoundLineups_ADiscardDropsTheMatchOnlyLegacyTeamsAndMarksWhenNoneRemains(t *testing.T) {
	for _, tc := range []struct {
		name       string
		lineups    func(rlTeams) []domain.TeamLineup
		before     func(rlTeams) []string
		after      func(rlTeams) []string
		wantMarked bool
	}{
		{
			name: "only match-only legacy teams",
			lineups: func(teams rlTeams) []domain.TeamLineup {
				return []domain.TeamLineup{
					rlLineup(teams.tora, "", 0, "tora-start"),
					rlLineup(teams.tora, "Pool A-0", 0, "tora-entered-m0"),
					rlLineup(teams.kuma, "Pool A-1", 0, "kuma-entered-m1"),
				}
			},
			before:     func(teams rlTeams) []string { return rlRecordedLegacy(teams.tora, teams.kuma) },
			after:      func(rlTeams) []string { return nil },
			wantMarked: true,
		},
		{
			name: "a team with a lineup for a round stays",
			lineups: func(teams rlTeams) []domain.TeamLineup {
				return []domain.TeamLineup{
					rlLineup(teams.tora, "", 1, "tora-round1"),
					rlLineup(teams.kuma, "Pool A-1", 0, "kuma-entered-m1"),
				}
			},
			before:     func(teams rlTeams) []string { return rlRecordedLegacy(teams.tora, teams.kuma) },
			after:      func(teams rlTeams) []string { return rlRecordedLegacy(teams.tora) },
			wantMarked: false,
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			teams := newRLTeams()
			dir := rlSeed(t, rlTeamComp(state.CompFormatLeague), teams, rlThreeMatches(teams), nil, tc.lineups(teams)...)
			store, err := state.NewStore(dir)
			require.NoError(t, err)
			require.Equal(t, tc.before(teams), rlLegacy(t, store), "precondition")
			require.False(t, rlMarker(t, store), "precondition")
			require.NotEmpty(t, rlGiven(t, store), "precondition")

			require.NoError(t, store.ClearDrawLineups(rlComp))

			assert.Equal(t, tc.after(teams), rlLegacy(t, store))
			assert.Equal(t, tc.wantMarked, rlMarker(t, store))
			assert.Empty(t, rlGiven(t, store), "the pairs of the discarded draw are not kept")
			for _, l := range rlLoad(t, store) {
				assert.Empty(t, l.MatchID, "every match lineup went with the draw")
			}
			// What was written is what a fresh Store reads back.
			reloaded, err := state.NewStore(dir)
			require.NoError(t, err)
			assert.Equal(t, tc.after(teams), rlLegacy(t, reloaded))
			assert.Equal(t, tc.wantMarked, rlMarker(t, reloaded))
		})
	}
}

// A match lineup v2.0.0 saved under a team's NAME is keyed by the team's id first,
// and then counts: the team is legacy for it like one whose lineup was saved by id.
func TestRoundLineups_ANameKeyedMatchLineupMakesItsTeamLegacyOnceKeyed(t *testing.T) {
	teams := newRLTeams()
	pool := rlThreeMatches(teams)
	dir := rlSeed(t, rlTeamComp(state.CompFormatLeague), teams, pool, nil,
		rlLineup("Kuma", "Pool A-1", 0, "kuma-by-name")) // Kuma plays Pool A-1 and Pool A-3

	store, err := state.NewStore(dir)
	require.NoError(t, err)
	lineups := rlLoad(t, store)

	rlHas(t, lineups, teams.kuma, "Pool A-1", 0, "kuma-by-name")
	rlLacks(t, lineups, "Kuma", "Pool A-1", 0)
	rlIsEmpty(t, lineups, teams.kuma, "Pool A-3")
	assert.Equal(t, rlRecordedLegacy(teams.kuma), rlLegacy(t, store))
	assert.False(t, rlMarker(t, store))
}

// A team with a lineup for a round is legacy whether or not the record lists it
// (its id can arrive after the first settlement), so a discard keeps waiting for
// it: the record is rewritten to the teams that still hold a round lineup, and the
// marker is not set while one does.
func TestRoundLineups_ADiscardKeepsWaitingForATeamTheRecordDoesNotList(t *testing.T) {
	teams := newRLTeams()
	comp := rlTeamComp(state.CompFormatLeague)
	comp.RoundLineupsLegacy = []string{teams.kuma}
	dir := rlSeed(t, comp, teams, rlThreeMatches(teams), nil,
		rlLineup(teams.kuma, "Pool A-1", 0, "kuma-entered-m1"),
		rlLineup(teams.tora, "", 1, "tora-round1"))
	store, err := state.NewStore(dir)
	require.NoError(t, err)
	require.Equal(t, []string{teams.kuma}, rlLegacy(t, store), "precondition: the record is read, not derived again")
	rlHas(t, rlLoad(t, store), teams.tora, "Pool A-0", 0, "tora-round1")

	require.NoError(t, store.ClearDrawLineups(rlComp))

	assert.Equal(t, rlRecordedLegacy(teams.tora), rlLegacy(t, store))
	assert.False(t, rlMarker(t, store))
	rlHas(t, rlLoad(t, store), teams.tora, "", 1, "tora-round1")
}

// A lineup left for a team that is no longer on the roster (an orphan) is no team's
// to wait for: a discard that leaves no team with a round lineup marks the
// conversion done, and the orphan's id is not recorded as a legacy team.
func TestRoundLineups_ADiscardIgnoresALineupOfATeamNoLongerOnTheRoster(t *testing.T) {
	teams := newRLTeams()
	const gone = "0a1b2c3d-0000-4000-8000-00000000dead" // no longer on the roster
	dir := rlSeed(t, rlTeamComp(state.CompFormatLeague), teams, rlThreeMatches(teams), nil,
		rlLineup(teams.kuma, "Pool A-1", 0, "kuma-entered-m1"),
		rlLineup(gone, "", 1, "gone-round1"))
	store, err := state.NewStore(dir)
	require.NoError(t, err)
	require.Equal(t, rlRecordedLegacy(teams.kuma), rlLegacy(t, store), "precondition: only Kuma is legacy")

	require.NoError(t, store.ClearDrawLineups(rlComp))

	assert.Empty(t, rlLegacy(t, store))
	assert.True(t, rlMarker(t, store))
}

// A discard never marks the conversion done while a lineup still waits for a
// player's id: it would stay keyed by name for good. The record is emptied all the
// same, and the settlement that follows the minting of the ids reads the competition
// as it did at its first pass.
func TestRoundLineups_ADiscardNeverMarksWhileALineupWaitsForAnId(t *testing.T) {
	dir := t.TempDir()
	seed, err := state.NewStore(dir)
	require.NoError(t, err)
	require.NoError(t, seed.SaveCompetition(&state.Competition{
		ID: rlComp, Name: "Round Lineups", Kind: "team", TeamSize: 3, Format: state.CompFormatKnockout, Status: state.CompStatusSetup,
		// A record the id-less roster below cannot match: it is hand-written here, since
		// a roster gets all its ids at once.
		RoundLineupsLegacy: []string{"11111111-1111-4111-8111-111111111111"},
	}))
	roster := "Tora, Tora Dojo, 1dan\nUsagi, Usagi Dojo, 1dan\n"
	require.NoError(t, os.WriteFile(filepath.Join(dir, "competitions", rlComp, "participants.csv"), []byte(roster), 0o600))
	rlWriteLineups(t, dir, rlComp, rlLineup("Tora", "", 1, "tora-round1"))
	store, err := state.NewStore(dir)
	require.NoError(t, err)
	require.False(t, rlMarker(t, store), "precondition: Tora's lineup waits for an id")

	require.NoError(t, store.ClearDrawLineups(rlComp))

	assert.False(t, rlMarker(t, store), "a lineup still waits for Tora's id")
	assert.Empty(t, rlLegacy(t, store), "the team the record listed has no lineup for a round: it is dropped")
	rlHas(t, rlLoad(t, store), "Tora", "", 1, "tora-round1")
}

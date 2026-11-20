package engine

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/gitrgoliveira/bracket-creator/internal/domain"
	"github.com/gitrgoliveira/bracket-creator/internal/helper"
	"github.com/gitrgoliveira/bracket-creator/internal/state"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// TestMaybeAdvanceKachinuki_RosterFromLineup verifies that when the match holds
// its own lineup for both teams (as a started match holds its pin), the advance
// uses it to build the full remaining roster. This means
// when SideA-position-1 beats SideB-position-1, SideB still has positions 2+
// queued, so the engine appends the next bout instead of ending the match.
func TestMaybeAdvanceKachinuki_RosterFromLineup(t *testing.T) {
	eng, store, _ := setupTestEngine(t)
	compID := "kachinuki-roster-from-lineup"

	require.NoError(t, store.SaveCompetition(&state.Competition{
		ID:            compID,
		TeamMatchType: state.TeamMatchTypeKachinuki,
		TeamSize:      3,
		Format:        state.CompFormatMixed,
	}))
	// The match's own lineups for both teams.
	require.NoError(t, store.SetTeamLineup(compID, domain.TeamLineup{
		TeamID: redTeamID, MatchID: "P1-0",
		Positions: map[domain.Position]string{
			domain.PositionNumbered(1): "R-1",
			domain.PositionNumbered(2): "R-2",
			domain.PositionNumbered(3): "R-3",
		},
	}, 3))
	require.NoError(t, store.SetTeamLineup(compID, domain.TeamLineup{
		TeamID: whiteTeamID, MatchID: "P1-0",
		Positions: map[domain.Position]string{
			domain.PositionNumbered(1): "W-1",
			domain.PositionNumbered(2): "W-2",
			domain.PositionNumbered(3): "W-3",
		},
	}, 3))

	// Bout 1: R-1 beats W-1. With lineup: remainingA=[R-1], remainingB=[W-2,W-3].
	// Without a lineup (and no team members) a side fields nobody (K2), so
	// nothing would be appended.
	require.NoError(t, store.SavePoolMatches(compID, []state.MatchResult{
		{
			ID:      "P1-0",
			SideA:   "RedTeam",
			SideAID: redTeamID,
			SideB:   "WhiteTeam",
			SideBID: whiteTeamID,
			SubResults: []state.SubMatchResult{
				{Position: 1, SideA: "R-1", SideB: "W-1", Winner: "R-1", Decision: "fought"},
			},
		},
	}))

	changed, _, err := eng.MaybeAdvanceKachinuki(compID, "P1-0")
	require.NoError(t, err)
	assert.True(t, changed, "next bout must be appended (W-2 is in queue from lineup)")

	matches, err := store.LoadPoolMatches(compID)
	require.NoError(t, err)
	require.Len(t, matches, 1)
	require.Len(t, matches[0].SubResults, 2, "bout 2 must be appended")
	assert.Equal(t, "R-1", matches[0].SubResults[1].SideA, "R-1 stays as winner")
	assert.Equal(t, "W-2", matches[0].SubResults[1].SideB, "W-2 is next from lineup")
	assert.NotEqual(t, state.MatchStatusCompleted, matches[0].Status, "match must not be completed yet")
}

// TestMaybeAdvanceKachinuki_RosterFromLineup_ParticipantIDKeyed reproduces
// the real UI flow: the lineup editor saves lineups keyed by the team
// PARTICIPANT ID (teamIdOf(t) resolves to player.id, a UUID assigned by
// the store), and a match side finds them by that id. The pool row here
// carries the team NAME alone, as a row from before side ids does: the
// load-time legacy repair stamps its SideAID/SideBID from the roster, so
// the lookup still reaches the id-keyed lineups instead of silently
// fielding nobody (GAP 2).
func TestMaybeAdvanceKachinuki_RosterFromLineup_ParticipantIDKeyed(t *testing.T) {
	eng, store, _ := setupTestEngine(t)
	compID := "kachinuki-lineup-pid-keyed"

	require.NoError(t, store.SaveCompetition(&state.Competition{
		ID:            compID,
		TeamMatchType: state.TeamMatchTypeKachinuki,
		TeamSize:      3,
		Format:        state.CompFormatMixed,
	}))

	// Team participants exactly as the store creates them: UUID id, team
	// name in Name. (state.LoadParticipants only treats the first CSV
	// column as an ID when it parses as UUID v4.)
	ryuID := helper.NewUUID4()
	toraID := helper.NewUUID4()
	require.NoError(t, store.SaveParticipants(compID, []domain.Player{
		{ID: ryuID, Name: "Ryu", Dojo: "DojoR"},
		{ID: toraID, Name: "Tora", Dojo: "DojoT"},
	}))

	// Lineups keyed by the participant ID, exactly as the UI saves them
	// (NOT by team name).
	require.NoError(t, store.SetTeamLineup(compID, domain.TeamLineup{
		TeamID: ryuID, MatchID: "P1-0",
		Positions: map[domain.Position]string{
			domain.PositionNumbered(1): "R-1",
			domain.PositionNumbered(2): "R-2",
			domain.PositionNumbered(3): "R-3",
		},
	}, 3))
	require.NoError(t, store.SetTeamLineup(compID, domain.TeamLineup{
		TeamID: toraID, MatchID: "P1-0",
		Positions: map[domain.Position]string{
			domain.PositionNumbered(1): "W-1",
			domain.PositionNumbered(2): "W-2",
			domain.PositionNumbered(3): "W-3",
		},
	}, 3))

	// Match sides carry the team NAME. Bout 1: R-1 beats W-1. With the
	// lineup resolved: remainingB=[W-2, W-3], so bout 2 must be appended.
	// If the id-keyed lineup lookup misses, the side fields nobody (K2) and
	// nothing is appended.
	require.NoError(t, store.SavePoolMatches(compID, []state.MatchResult{
		{
			ID:    "P1-0",
			SideA: "Ryu",
			SideB: "Tora",
			SubResults: []state.SubMatchResult{
				{Position: 1, SideA: "R-1", SideB: "W-1", Winner: "R-1", Decision: "fought"},
			},
		},
	}))

	changed, _, err := eng.MaybeAdvanceKachinuki(compID, "P1-0")
	require.NoError(t, err)
	assert.True(t, changed, "next bout must be appended (W-2 queued in the id-keyed lineup)")

	matches, err := store.LoadPoolMatches(compID)
	require.NoError(t, err)
	require.Len(t, matches, 1)
	require.Len(t, matches[0].SubResults, 2, "lineup must resolve via participant id: bout 2 appended")
	assert.Equal(t, "R-1", matches[0].SubResults[1].SideA, "R-1 stays as winner")
	assert.Equal(t, "W-2", matches[0].SubResults[1].SideB, "W-2 is next from the id-keyed lineup")
	assert.NotEqual(t, state.MatchStatusCompleted, matches[0].Status, "match must not be completed yet")
}

// TestKachinukiRemainingRoster_MatchesALineupByTheSidesParticipantID pins that a
// side's lineup is the one stored under its participant id (the UI's storage
// key) and no other: a lineup stored under the team's NAME sits beside it, or
// alone, and is not the team's, and a side that carries no id has none.
func TestKachinukiRemainingRoster_MatchesALineupByTheSidesParticipantID(t *testing.T) {
	comp := &state.Competition{ID: "kachinuki-lineup-by-id", TeamSize: 3, TeamMatchType: state.TeamMatchTypeKachinuki}
	// Each is the match's own lineup: the match holds bouts, so it has started
	// and fields only a lineup of its own.
	lineup := func(team string, fighters ...string) domain.TeamLineup {
		l := domain.TeamLineup{TeamID: team, MatchID: "P1-0", Positions: map[domain.Position]string{}}
		for i, f := range fighters {
			l.Positions[domain.PositionNumbered(i+1)] = f
		}
		return l
	}
	// R-1 beat W-1, so any roster White has beyond that came from a lineup.
	bouts := []state.SubMatchResult{{Position: 1, SideA: "R-1", SideB: "W-1", Winner: "R-1", Decision: "fought"}}
	lineups := map[string]domain.TeamLineup{
		"by-id":         lineup("red-id", "R-1", "R-2", "R-3"),
		"red-by-name":   lineup("Ryu", "X-1", "X-2", "X-3"),
		"white-by-name": lineup("Tora", "W-1", "W-2", "W-3"),
	}

	t.Run("a lineup stored under the id is the side's, though one under its name sits beside it", func(t *testing.T) {
		parent := &state.MatchResult{ID: "P1-0", SideA: "Ryu", SideAID: "red-id", SideB: "Tora", SideBID: "white-id", SubResults: bouts}

		remainingA, remainingB, foundA, foundB := kachinukiRemainingRoster(comp, parent, ruleOver(lineups, nil, nil))
		ok := foundA || foundB

		require.True(t, ok, "Red's lineup resolves")
		assert.Equal(t, fighters("R-1", "R-2", "R-3"), remainingA)
		assert.Empty(t, remainingB, "White has a lineup under its NAME only, which is not the team's: it fields nobody")
	})

	t.Run("a side with no id has no lineup, whatever is stored", func(t *testing.T) {
		parent := &state.MatchResult{ID: "P1-0", SideA: "Ryu", SideB: "Tora", SubResults: bouts}

		_, _, foundA, foundB := kachinukiRemainingRoster(comp, parent, ruleOver(lineups, nil, nil))
		ok := foundA || foundB

		assert.False(t, ok, "no side carries an id, so no lineup resolves")
	})
}

// TestMaybeAdvanceKachinuki_RenameDoesNotRequeueRetiredMember is THE
// headline pin for bc-tmid pass 2. Sequence: Sato fights and LOSES; the
// operator then renames Sato to Suzuki (correcting a typo, say) and
// re-saves the SAME lineup slot under the new name -- the same member,
// same id, only the display name changed, exactly what "select the SAME
// existing member" produces. Before this pass, retirement was NAME-keyed:
// "Suzuki" would not match the retired set recorded under "Sato", and the
// already-defeated fighter would be handed the NEXT bout instead of
// Backup. With id-aware retirement, Suzuki's slot still carries Sato's
// original member id, which IS in the retired-id set, so Backup correctly
// advances instead.
func TestMaybeAdvanceKachinuki_RenameDoesNotRequeueRetiredMember(t *testing.T) {
	eng, store, comp := setupKachinukiComp(t, "kachinuki-rename-defect", 2,
		func(c *state.Competition) { c.Format = state.CompFormatMixed })

	redID := helper.NewUUID4()
	whiteID := helper.NewUUID4()
	require.NoError(t, store.SaveParticipants(comp.ID, []domain.Player{
		{ID: redID, Name: "RedTeam", Dojo: "D"},
		{ID: whiteID, Name: "WhiteTeam", Dojo: "D"},
	}))

	sato, err := store.AddTeamMember(comp.ID, redID, "Sato")
	require.NoError(t, err)
	backup, err := store.AddTeamMember(comp.ID, redID, "Backup")
	require.NoError(t, err)

	// The match's own lineup, id-stamped, as it stood BEFORE the rename: Sato
	// is about to fight the first bout.
	require.NoError(t, store.SetTeamLineup(comp.ID, domain.TeamLineup{
		TeamID: redID, MatchID: "P1-0",
		Positions: map[domain.Position]string{
			domain.PositionNumbered(1): "Sato",
			domain.PositionNumbered(2): "Backup",
		},
		MemberIDs: map[domain.Position]string{
			domain.PositionNumbered(1): sato.ID,
			domain.PositionNumbered(2): backup.ID,
		},
	}, 2))

	// Sato fights bout 1 and LOSES to W-1. SideAMemberID carries Sato's id
	// onto the bout log, exactly as appendNextKachinukiBout would have
	// stamped it (or as the legacy-upgrade repair would have backfilled it).
	require.NoError(t, store.SavePoolMatches(comp.ID, []state.MatchResult{
		{
			ID: "P1-0", SideA: "RedTeam", SideB: "WhiteTeam", Status: state.MatchStatusRunning,
			SubResults: []state.SubMatchResult{
				{Position: 1, SideA: "Sato", SideAMemberID: sato.ID, SideB: "W-1", Winner: "W-1", Decision: "fought"},
			},
		},
	}))

	// The operator renames Sato -> Suzuki (id unchanged) and re-saves the
	// SAME lineup slot under the new name, as re-selecting the same squad
	// member would.
	_, err = store.RenameTeamMember(comp.ID, redID, sato.ID, "Suzuki")
	require.NoError(t, err)
	require.NoError(t, store.SetTeamLineup(comp.ID, domain.TeamLineup{
		TeamID: redID, MatchID: "P1-0",
		Positions: map[domain.Position]string{
			domain.PositionNumbered(1): "Suzuki",
			domain.PositionNumbered(2): "Backup",
		},
		MemberIDs: map[domain.Position]string{
			domain.PositionNumbered(1): sato.ID, // same id, new name
			domain.PositionNumbered(2): backup.ID,
		},
	}, 2))

	changed, post, err := eng.MaybeAdvanceKachinuki(comp.ID, "P1-0")
	require.NoError(t, err)
	require.True(t, changed, "W-1 stays on and must be paired against the next un-retired RED fighter")
	require.Len(t, post.BoutLog, 2)
	next := post.BoutLog[1]
	assert.Equal(t, "Backup", next.SideA,
		"Suzuki (Sato, renamed) already lost and must NOT be requeued despite the name change")
	assert.Equal(t, "W-1", next.SideB, "the bout-1 winner stays on")
}

// TestMaybeAdvanceKachinuki_NoLineupFieldsNobody verifies that when no lineup
// is saved and the teams have no member records (so nothing is fielded by
// member number) each side fields nobody (operator decision K2) and the
// advance answers without error. The empty queues (remainingB=[]) read as a
// match end, but that verdict is advisory only (operator-led completion,
// mp-gmcg), so the engine must leave the match untouched.
func TestMaybeAdvanceKachinuki_NoLineupFieldsNobody(t *testing.T) {
	eng, store, _ := setupTestEngine(t)
	compID := "kachinuki-no-lineup-fallback"

	require.NoError(t, store.SaveCompetition(&state.Competition{
		ID:            compID,
		TeamMatchType: state.TeamMatchTypeKachinuki,
		TeamSize:      3,
		Format:        state.CompFormatMixed,
	}))
	// No lineups saved.
	require.NoError(t, store.SavePoolMatches(compID, []state.MatchResult{
		{
			ID:    "P1-0",
			SideA: "RedTeam",
			SideB: "WhiteTeam",
			SubResults: []state.SubMatchResult{
				{Position: 1, SideA: "R-1", SideB: "W-1", Winner: "R-1", Decision: "fought"},
			},
		},
	}))

	// Without a lineup: remainingB=[] → advisory match end → no-op.
	changed, _, err := eng.MaybeAdvanceKachinuki(compID, "P1-0")
	require.NoError(t, err)
	assert.False(t, changed, "an exhausted snapshot is advisory; the engine must not mutate the match")

	matches, err := store.LoadPoolMatches(compID)
	require.NoError(t, err)
	require.Len(t, matches, 1)
	assert.NotEqual(t, state.MatchStatusCompleted, matches[0].Status, "the engine must never auto-complete")
	assert.Empty(t, matches[0].Winner)
	assert.Empty(t, matches[0].Decision)
	assert.Len(t, matches[0].SubResults, 1, "no bout may be appended off an exhausted snapshot")
}

// TestMaybeAdvanceKachinuki_MatchScopedLineup verifies that a match-scoped
// lineup (keyed by matchID) takes precedence over the round-scoped lineup for
// the same team.
func TestMaybeAdvanceKachinuki_MatchScopedLineup(t *testing.T) {
	eng, store, _ := setupTestEngine(t)
	compID := "kachinuki-match-scoped-lineup"

	require.NoError(t, store.SaveCompetition(&state.Competition{
		ID:            compID,
		TeamMatchType: state.TeamMatchTypeKachinuki,
		TeamSize:      3,
		Format:        state.CompFormatMixed,
	}))

	// Round-scoped lineup (generic): R-1, R-2, R-3.
	require.NoError(t, store.SetTeamLineup(compID, domain.TeamLineup{
		TeamID: redTeamID, Round: 0,
		Positions: map[domain.Position]string{
			domain.PositionNumbered(1): "R-1",
			domain.PositionNumbered(2): "R-2",
			domain.PositionNumbered(3): "R-3",
		},
	}, 3))
	// Match-scoped lineup (specific for "P1-0"): R-A, R-B, R-C.
	require.NoError(t, store.SetTeamLineup(compID, domain.TeamLineup{
		TeamID: redTeamID, MatchID: "P1-0",
		Positions: map[domain.Position]string{
			domain.PositionNumbered(1): "R-A",
			domain.PositionNumbered(2): "R-B",
			domain.PositionNumbered(3): "R-C",
		},
	}, 3))
	// WhiteTeam round-scoped.
	require.NoError(t, store.SetTeamLineup(compID, domain.TeamLineup{
		TeamID: whiteTeamID, Round: 0,
		Positions: map[domain.Position]string{
			domain.PositionNumbered(1): "W-1",
			domain.PositionNumbered(2): "W-2",
			domain.PositionNumbered(3): "W-3",
		},
	}, 3))

	// Bout 1: W-1 beats R-A (from match-scoped lineup).
	require.NoError(t, store.SavePoolMatches(compID, []state.MatchResult{
		{
			ID:      "P1-0",
			SideA:   "RedTeam",
			SideAID: redTeamID,
			SideB:   "WhiteTeam",
			SideBID: whiteTeamID,
			SubResults: []state.SubMatchResult{
				{Position: 1, SideA: "R-A", SideB: "W-1", Winner: "W-1", Decision: "fought"},
			},
		},
	}))

	changed, _, err := eng.MaybeAdvanceKachinuki(compID, "P1-0")
	require.NoError(t, err)
	assert.True(t, changed, "next bout must be appended using match-scoped lineup")

	matches, err := store.LoadPoolMatches(compID)
	require.NoError(t, err)
	require.Len(t, matches[0].SubResults, 2)
	// R-A retired; next from match-scoped roster is R-B (NOT R-2 from round-scoped).
	assert.Equal(t, "R-B", matches[0].SubResults[1].SideA, "R-B from match-scoped lineup, not R-2 from round-scoped")
	assert.Equal(t, "W-1", matches[0].SubResults[1].SideB, "W-1 stays as winner")
}

// TestMaybeAdvanceKachinuki_CarriesThePreviousMatchLineup pins the operator's
// rule (2026-10-05): a team keeps the lineup of its previous match unless a new
// one is entered for the match, so the queue the advance reads for a match that
// has not started is the one saved for the team's EARLIER match, not its
// starting lineup. (Once started, a match fields only a lineup of its own, its
// pin; see TestKachinukiFielded_AStartedMatchWithNothingOfItsOwnFieldsByNumber.)
func TestMaybeAdvanceKachinuki_CarriesThePreviousMatchLineup(t *testing.T) {
	eng, store, comp := setupKachinukiComp(t, "kachinuki-carried-lineup", 3,
		func(c *state.Competition) { c.Format = state.CompFormatMixed })

	redID, whiteID, blueID := helper.NewUUID4(), helper.NewUUID4(), helper.NewUUID4()
	require.NoError(t, store.SaveParticipants(comp.ID, []domain.Player{
		{ID: redID, Name: "RedTeam", Dojo: "DojoR"},
		{ID: whiteID, Name: "WhiteTeam", Dojo: "DojoW"},
		{ID: blueID, Name: "BlueTeam", Dojo: "DojoB"},
	}))
	lineup := func(team, match string, names ...string) domain.TeamLineup {
		l := domain.TeamLineup{TeamID: team, MatchID: match, Positions: map[domain.Position]string{}}
		for i, n := range names {
			l.Positions[domain.PositionNumbered(i+1)] = n
		}
		return l
	}
	// Red's starting lineup, then a different one entered for its first match.
	require.NoError(t, store.SetTeamLineup(comp.ID, lineup(redID, "", "R-1", "R-2", "R-3"), 3))
	require.NoError(t, store.SetTeamLineup(comp.ID, lineup(redID, "Pool A-0", "R-A", "R-B", "R-C"), 3))
	require.NoError(t, store.SetTeamLineup(comp.ID, lineup(blueID, "", "B-1", "B-2", "B-3"), 3))

	require.NoError(t, store.SavePoolMatches(comp.ID, []state.MatchResult{
		{ID: "Pool A-0", SideA: "RedTeam", SideB: "WhiteTeam", SideAID: redID, SideBID: whiteID, Status: state.MatchStatusCompleted},
		{ID: "Pool A-1", SideA: "RedTeam", SideB: "BlueTeam", SideAID: redID, SideBID: blueID, Status: state.MatchStatusScheduled},
	}))

	q := storedKachinukiQueues(t, eng, comp.ID, "Pool A-1")
	assert.True(t, q.queueA.fromLineup())
	assert.Equal(t, []string{"R-A", "R-B", "R-C"}, queueNames(q.queueA),
		"Red's queue is the lineup it carried out of Pool A-0, not its starting lineup")
	assert.Equal(t, []string{"B-1", "B-2", "B-3"}, queueNames(q.queueB), "Blue carries its starting lineup")
}

// TestMaybeAdvanceKachinuki_UnreadableLineupsDoNotFailTheAdvance pins that a
// lineups file that cannot be read never stops the advance: the read is
// logged, not returned, and the sides field nobody (K2; the queues are pinned
// by TestKachinukiFielded_UnreadableLineupsFillNothing).
func TestMaybeAdvanceKachinuki_UnreadableLineupsDoNotFailTheAdvance(t *testing.T) {
	eng, store, comp := setupKachinukiComp(t, "kachinuki-unreadable-lineups", 3)
	require.NoError(t, os.WriteFile(
		filepath.Join(store.GetFolder(), "competitions", comp.ID, "lineups.yaml"),
		[]byte("lineups: [this is: not: valid yaml"), 0o600))
	require.NoError(t, store.SavePoolMatches(comp.ID, []state.MatchResult{{
		ID: "P1-0", SideA: "RedTeam", SideB: "WhiteTeam", Status: state.MatchStatusRunning,
		SubResults: []state.SubMatchResult{
			{Position: 1, SideA: "R-1", SideB: "W-1", Winner: "W-1", Decision: "fought"},
			{Position: 2, SideA: "R-2", SideB: "W-1", Winner: "R-2", Decision: "fought"},
		},
	}}))

	_, _, err := eng.MaybeAdvanceKachinuki(comp.ID, "P1-0")

	require.NoError(t, err, "an unreadable lineups file degrades the roster, it does not fail the advance")
}

// TestMaybeAdvanceKachinuki_AnUnreadableBracketDoesNotCostAPoolMatchItsLineups
// pins that the advance builds the lineup rule from the draw its own match
// lookup loaded, and does not read the draw a second time. A pool match is found
// before the bracket is read, so a bracket.json that cannot be read has no part
// in it. It used to: the lineup read loaded the bracket again, its failure
// resolved no lineup, and the roster fell back to the bout log, which ends a
// side the lineup would have kept fielding.
func TestMaybeAdvanceKachinuki_AnUnreadableBracketDoesNotCostAPoolMatchItsLineups(t *testing.T) {
	eng, store, comp := setupKachinukiComp(t, "kachinuki-advance-bad-bracket", 3,
		func(c *state.Competition) { c.Format = state.CompFormatMixed })
	for team, fighters := range map[string][]string{redTeamID: {"R-1", "R-2", "R-3"}, whiteTeamID: {"W-1", "W-2", "W-3"}} {
		l := domain.TeamLineup{TeamID: team, MatchID: "P1-0", Positions: map[domain.Position]string{}}
		for i, f := range fighters {
			l.Positions[domain.PositionNumbered(i+1)] = f
		}
		require.NoError(t, store.SetTeamLineup(comp.ID, l, 3))
	}
	require.NoError(t, store.SavePoolMatches(comp.ID, []state.MatchResult{{
		ID: "P1-0", SideA: "RedTeam", SideAID: redTeamID, SideB: "WhiteTeam", SideBID: whiteTeamID,
		Status: state.MatchStatusRunning,
		SubResults: []state.SubMatchResult{
			{Position: 1, SideA: "R-1", SideB: "W-1", Winner: "R-1", Decision: "fought"},
		},
	}}))
	require.NoError(t, os.WriteFile(
		filepath.Join(store.GetFolder(), "competitions", comp.ID, "bracket.json"), []byte("{not json"), 0o600))

	changed, post, err := eng.MaybeAdvanceKachinuki(comp.ID, "P1-0")

	require.NoError(t, err)
	require.True(t, changed, "White still has W-2 and W-3 in its lineup")
	require.Len(t, post.BoutLog, 2)
	assert.Equal(t, "W-2", post.BoutLog[1].SideB)
}

// TestMaybeAdvanceKachinuki_TakesALineupFromTheTeamIDNotItsName pins, through the
// advance, that a team's lineup is the one stored under its participant id: the
// same rosters stored under the team NAMES, which the match carries as well, are
// not the teams', so the roster is the bout log's and White, whose only fighter
// so far lost, is not given a next one.
func TestMaybeAdvanceKachinuki_TakesALineupFromTheTeamIDNotItsName(t *testing.T) {
	for _, tc := range []struct {
		name          string
		red, white    string
		wantAdvancing bool
	}{
		{"stored under the participant ids", redTeamID, whiteTeamID, true},
		{"stored under the team names", "RedTeam", "WhiteTeam", false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			eng, store, comp := setupKachinukiComp(t, "kachinuki-lineup-key", 3,
				func(c *state.Competition) { c.Format = state.CompFormatMixed })
			for team, fighters := range map[string][]string{tc.red: {"R-1", "R-2", "R-3"}, tc.white: {"W-1", "W-2", "W-3"}} {
				l := domain.TeamLineup{TeamID: team, MatchID: "P1-0", Positions: map[domain.Position]string{}}
				for i, f := range fighters {
					l.Positions[domain.PositionNumbered(i+1)] = f
				}
				require.NoError(t, store.SetTeamLineup(comp.ID, l, 3))
			}
			require.NoError(t, store.SavePoolMatches(comp.ID, []state.MatchResult{{
				ID: "P1-0", SideA: "RedTeam", SideAID: redTeamID, SideB: "WhiteTeam", SideBID: whiteTeamID,
				Status: state.MatchStatusRunning,
				SubResults: []state.SubMatchResult{
					{Position: 1, SideA: "R-1", SideB: "W-1", Winner: "R-1", Decision: "fought"},
				},
			}}))

			changed, _, err := eng.MaybeAdvanceKachinuki(comp.ID, "P1-0")

			require.NoError(t, err)
			assert.Equal(t, tc.wantAdvancing, changed)
		})
	}
}

// TestMaybeAdvanceKachinuki_LatestRoundLineupFallback verifies that when
// multiple round-scoped lineups exist for a team, the queue a pool match that
// has not started reads picks the one whose round has started: a round-1
// lineup begins after the pool matches, so a pool match (round 0) must use the
// round-0 lineup.
func TestMaybeAdvanceKachinuki_LatestRoundLineupFallback(t *testing.T) {
	eng, store, _ := setupTestEngine(t)
	compID := "kachinuki-latest-round-fallback"

	require.NoError(t, store.SaveCompetition(&state.Competition{
		ID:            compID,
		TeamMatchType: state.TeamMatchTypeKachinuki,
		TeamSize:      3,
		Format:        state.CompFormatMixed,
	}))

	// Round 0 lineup for RedTeam (pool phase).
	require.NoError(t, store.SetTeamLineup(compID, domain.TeamLineup{
		TeamID: redTeamID, Round: 0,
		Positions: map[domain.Position]string{
			domain.PositionNumbered(1): "R-Pool-1",
			domain.PositionNumbered(2): "R-Pool-2",
			domain.PositionNumbered(3): "R-Pool-3",
		},
	}, 3))
	// Round 1 lineup for RedTeam (bracket phase, should NOT be used for pool match).
	require.NoError(t, store.SetTeamLineup(compID, domain.TeamLineup{
		TeamID: redTeamID, Round: 1,
		Positions: map[domain.Position]string{
			domain.PositionNumbered(1): "R-Bracket-1",
			domain.PositionNumbered(2): "R-Bracket-2",
			domain.PositionNumbered(3): "R-Bracket-3",
		},
	}, 3))
	require.NoError(t, store.SetTeamLineup(compID, domain.TeamLineup{
		TeamID: whiteTeamID, Round: 0,
		Positions: map[domain.Position]string{
			domain.PositionNumbered(1): "W-1",
			domain.PositionNumbered(2): "W-2",
			domain.PositionNumbered(3): "W-3",
		},
	}, 3))

	// The pool match has not started yet, so it fields the lineup in force.
	// Round 1 has not started during the pool match, so that is the round-0
	// lineup.
	require.NoError(t, store.SavePoolMatches(compID, []state.MatchResult{
		{
			ID:      "P1-0",
			SideA:   "RedTeam",
			SideAID: redTeamID,
			SideB:   "WhiteTeam",
			SideBID: whiteTeamID,
			Status:  state.MatchStatusScheduled,
		},
	}))

	q := storedKachinukiQueues(t, eng, compID, "P1-0")
	assert.Equal(t, []string{"R-Pool-1", "R-Pool-2", "R-Pool-3"}, queueNames(q.queueA),
		"round-0 lineup used (not bracket round-1 with different names)")
	assert.Equal(t, []string{"W-1", "W-2", "W-3"}, queueNames(q.queueB))
}

// The queue is judged as a whole by the ONE retirement filter
// (filterRemainingFighters), so the name tier never fires for a fighter whose
// OWN member id proves they have not retired: with two teammates sharing a
// display name (a roster that predates the within-team uniqueness rule), the
// one who lost must not retire the one who did not.
func TestKachinukiRemainingRoster_KeepsANamesakeWithADistinctID(t *testing.T) {
	comp := &state.Competition{ID: "kachinuki-namesake", TeamSize: 3, TeamMatchType: state.TeamMatchTypeKachinuki}
	lineups := map[string]domain.TeamLineup{
		"red": {
			TeamID: "red-id", MatchID: "P1-0",
			Positions: map[domain.Position]string{
				domain.PositionNumbered(1): "Tanaka",
				domain.PositionNumbered(2): "Tanaka",
				domain.PositionNumbered(3): "Sato",
			},
			MemberIDs: map[domain.Position]string{
				domain.PositionNumbered(1): "m1",
				domain.PositionNumbered(2): "m2",
				domain.PositionNumbered(3): "m3",
			},
		},
	}
	parent := &state.MatchResult{
		ID: "P1-0", SideA: "Ryu", SideAID: "red-id", SideB: "Tora", SideBID: "white-id",
		SubResults: []state.SubMatchResult{
			{Position: 1, SideA: "Tanaka", SideAMemberID: "m1", SideB: "Taro", SideBMemberID: "m9",
				Winner: "Taro", WinnerMemberID: "m9"},
			{Position: 2, SideA: "Tanaka", SideAMemberID: "m2", SideB: "Taro", SideBMemberID: "m9"},
		},
	}

	remainingA, _, _, _ := kachinukiRemainingRoster(comp, parent, ruleOver(lineups, nil, nil))

	ids := make([]string, 0, len(remainingA))
	for _, f := range remainingA {
		ids = append(ids, f.MemberID)
	}
	assert.NotContains(t, ids, "m1", "the fighter who actually lost is retired, by id")
	assert.Equal(t, []string{"m2", "m3"}, ids,
		"the namesake with a DIFFERENT id has not retired and must still be fielded")
}

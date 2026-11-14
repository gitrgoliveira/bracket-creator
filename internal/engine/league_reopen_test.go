package engine

import (
	"errors"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/gitrgoliveira/bracket-creator/internal/domain"
	"github.com/gitrgoliveira/bracket-creator/internal/state"
)

// finishedLeague sets up a team league with a clear table (Alpha, Beta,
// Gamma, Delta, scenario "noTie") and finishes it through the league's own
// completion, as a played league finishes.
func finishedLeague(t *testing.T, compID string) (*Engine, *state.Store) {
	t.Helper()
	eng, store := setupTeamLeagueComp(t, compID, "noTie")
	require.NoError(t, store.SaveTournament(&state.Tournament{Name: "T", Courts: []string{"A"}}))
	outcome, err := eng.MaybeAutoCompletePools(compID)
	require.NoError(t, err)
	require.Equal(t, AutoCompleteTransitioned, outcome)
	require.Equal(t, state.CompStatusComplete, loadComp(t, store, compID).Status)
	return eng, store
}

// tieAt1st corrects Alpha - Beta to a draw on a finished "noTie" league: Alpha
// and Beta are then level at 1st, a place that cannot be shared.
func tieAt1st(t *testing.T, eng *Engine, compID string) {
	t.Helper()
	_, err := eng.RecordMatchResultWithIneligibility(compID, "Pool A-0", teamDraw("Alpha", "Beta"))
	require.NoError(t, err)
}

// leagueTeamID returns a team's participant id from the league's pool.
func leagueTeamID(t *testing.T, store *state.Store, compID, name string) string {
	t.Helper()
	pools, err := store.LoadPools(compID)
	require.NoError(t, err)
	for _, p := range pools {
		for _, pl := range p.Players {
			if pl.Name == name {
				return pl.ID
			}
		}
	}
	t.Fatalf("team %q not in %s", name, compID)
	return ""
}

// teamWin is a completed team result in which winnerSide ("A" or "B") takes
// both bouts.
func teamWin(t *testing.T, store *state.Store, compID, sideA, sideB, winnerSide string) *state.MatchResult {
	winner := sideA
	if winnerSide == "B" {
		winner = sideB
	}
	return &state.MatchResult{
		Status:     state.MatchStatusCompleted,
		Winner:     winner,
		WinnerID:   leagueTeamID(t, store, compID, winner),
		WinnerSide: winnerSide,
		SubResults: []state.SubMatchResult{
			{Position: 1, SideA: sideA, SideB: sideB, Winner: winner},
			{Position: 2, SideA: sideA, SideB: sideB, Winner: winner},
		},
	}
}

// teamDraw is a completed team result with both bouts drawn.
func teamDraw(sideA, sideB string) *state.MatchResult {
	return &state.MatchResult{
		Status:   state.MatchStatusCompleted,
		Decision: string(domain.DecisionHikiwake),
		SubResults: []state.SubMatchResult{
			{Position: 1, SideA: sideA, SideB: sideB, Decision: string(domain.DecisionHikiwake)},
			{Position: 2, SideA: sideA, SideB: sideB, Decision: string(domain.DecisionHikiwake)},
		},
	}
}

func loadComp(t *testing.T, store *state.Store, compID string) *state.Competition {
	t.Helper()
	comp, err := store.LoadCompetition(compID)
	require.NoError(t, err)
	require.NotNil(t, comp)
	return comp
}

// --- A correction that unsettles a finished league (reconcileLeagueAfterWrite) ---

// A correction that leaves a tie at 1st puts the finished league back into
// match play: the tie is asked again, the verdict owes its tie-breaker, the
// door's pass reports the league as started (competition_started) once, and a
// match on a shiaijo the tournament has since removed loses its court.
func TestLeagueCorrection_TieAt1stReopensAndClearsDeadCourts(t *testing.T) {
	eng, store := finishedLeague(t, "fix-dead-courts")
	// Shiaijo A was removed while the league was completed.
	require.NoError(t, store.SaveTournament(&state.Tournament{Name: "T", Courts: []string{"B"}}))
	tieAt1st(t, eng, "fix-dead-courts")

	assert.Equal(t, state.CompStatusPools, loadComp(t, store, "fix-dead-courts").Status, "back in match play")
	matches, err := store.LoadPoolMatches("fix-dead-courts")
	require.NoError(t, err)
	for _, m := range matches {
		assert.Empty(t, m.Court, "%s lost the removed shiaijo", m.ID)
		assert.Equal(t, state.MatchStatusCompleted, m.Status, "%s keeps its result", m.ID)
	}
	assert.Equal(t, PoolsTiebreakOwed, poolsVerdictNow(t, eng, store, "fix-dead-courts"),
		"POST /complete agrees with the reopened league")

	outcome, err := eng.MaybeAutoCompletePools("fix-dead-courts")
	require.NoError(t, err)
	assert.Equal(t, AutoCompleteStarted, outcome)
	again, err := eng.MaybeAutoCompletePools("fix-dead-courts")
	require.NoError(t, err)
	assert.NotEqual(t, AutoCompleteStarted, again, "the note is taken once")
	assert.NotEqual(t, AutoCompleteTransitioned, again, "the tie at 1st keeps the league open")

	cands, err := eng.LeagueTiebreakCandidates("fix-dead-courts")
	require.NoError(t, err)
	require.Len(t, cands, 1, "the tie is asked")
	assert.Equal(t, 1, cands[0].MinPosition)
}

func TestLeagueCorrection_ReopenKeepsLiveCourts(t *testing.T) {
	eng, store := finishedLeague(t, "fix-live-courts")
	tieAt1st(t, eng, "fix-live-courts")
	require.Equal(t, state.CompStatusPools, loadComp(t, store, "fix-live-courts").Status)
	matches, err := store.LoadPoolMatches("fix-live-courts")
	require.NoError(t, err)
	for _, m := range matches {
		assert.Equal(t, "A", m.Court)
	}
}

// A correction that keeps the table settled leaves the league finished: its
// table and podium follow the corrected result.
func TestLeagueCorrection_KeepingTheTableSettledStaysFinished(t *testing.T) {
	eng, store := finishedLeague(t, "fix-keeps")
	// Gamma beat Delta; the correction gives it to Delta. No two teams are level.
	_, err := eng.RecordMatchResultWithIneligibility("fix-keeps", "Pool A-5", teamWin(t, store, "fix-keeps", "Gamma", "Delta", "B"))
	require.NoError(t, err)

	assert.Equal(t, state.CompStatusComplete, loadComp(t, store, "fix-keeps").Status)
	outcome, err := eng.MaybeAutoCompletePools("fix-keeps")
	require.NoError(t, err)
	assert.Equal(t, AutoCompleteNoChange, outcome)
}

// A correction that makes a tie at 3rd reopens the league while two joint 3rd
// places are off, and leaves it finished while they are on: 3rd is the one
// place a league may share.
func TestLeagueCorrection_TieAt3rdFollowsTheJointThirdSetting(t *testing.T) {
	for _, tc := range []struct {
		name       string
		jointThird bool
		want       state.CompetitionStatus
	}{
		{"joint 3rd off: the tie is broken", false, state.CompStatusPools},
		{"joint 3rd on: the place is shared", true, state.CompStatusComplete},
	} {
		t.Run(tc.name, func(t *testing.T) {
			compID := "fix-3rd-off"
			if tc.jointThird {
				compID = "fix-3rd-on"
			}
			eng, store := finishedLeague(t, compID)
			if tc.jointThird {
				comp := loadComp(t, store, compID)
				comp.LeagueTwoThirdPlaces = true
				require.NoError(t, store.SaveCompetition(comp))
			}
			// Gamma beat Delta; drawn instead, Gamma and Delta are level at 3rd.
			_, err := eng.RecordMatchResultWithIneligibility(compID, "Pool A-5", teamDraw("Gamma", "Delta"))
			require.NoError(t, err)
			assert.Equal(t, tc.want, loadComp(t, store, compID).Status)
		})
	}
}

// A regular match reopened on a finished league puts the league back into
// match play when the door's pass runs.
func TestLeagueReopenedMatch_PutsTheLeagueBackInPlay(t *testing.T) {
	eng, store := finishedLeague(t, "reopen-match")
	// A withdrawal recorded on the finished league (Delta takes the match, no
	// two teams are level, so it stays finished), then reopened.
	_, _, err := eng.RecordDecision("reopen-match", "Pool A-5", "kiken", "aka", "", nil)
	require.NoError(t, err)
	require.Equal(t, state.CompStatusComplete, loadComp(t, store, "reopen-match").Status)
	_, err = eng.ReopenMatch("reopen-match", "Pool A-5", "wrong call")
	require.NoError(t, err)

	outcome, err := eng.MaybeAutoCompletePools("reopen-match")
	require.NoError(t, err)
	assert.Equal(t, AutoCompleteStarted, outcome)
	assert.Equal(t, state.CompStatusPools, loadComp(t, store, "reopen-match").Status)
}

// A correction of a tie-breaker bout of a finished league is reconciled like a
// regular match's: one that leaves the round in a circle puts the league back
// into match play so the lots are asked, and one that only reorders the round
// leaves it finished.
func TestLeagueCorrection_TiebreakerBoutIsReconciled(t *testing.T) {
	correct := func(t *testing.T, eng *Engine, store *state.Store, compID, a, b, winner string) {
		t.Helper()
		all, err := store.LoadPoolMatches(compID)
		require.NoError(t, err)
		for i := range all {
			if !IsPoolDaihyosenMatchID(all[i].ID) {
				continue
			}
			if (all[i].SideA == a && all[i].SideB == b) || (all[i].SideA == b && all[i].SideB == a) {
				_, err := eng.RecordMatchResultWithIneligibility(compID, all[i].ID, &state.MatchResult{
					Status:           state.MatchStatusCompleted,
					Winner:           winner,
					WinnerID:         teamID(winner),
					CorrectionReason: "Scoring error",
				})
				require.NoError(t, err)
				return
			}
		}
		t.Fatalf("no tie-breaker bout between %s and %s", a, b)
	}
	finishedAfterRound := func(t *testing.T, compID string) (*Engine, *state.Store) {
		t.Helper()
		eng, store := cycleLeagueAt3rd(t, compID)
		_, err := eng.GenerateLeagueTiebreakMatches(compID, []string{teamID("Gamma"), teamID("Delta"), teamID("Epsilon")})
		require.NoError(t, err)
		scoreInjectedDH(t, eng, store, compID, rankOrder([]string{"Gamma", "Delta", "Epsilon"}))
		outcome, err := eng.MaybeAutoCompletePools(compID)
		require.NoError(t, err)
		require.Equal(t, AutoCompleteTransitioned, outcome, "the round ordered the tie")
		return eng, store
	}

	t.Run("a circle puts the league back into match play", func(t *testing.T) {
		eng, store := finishedAfterRound(t, "tb-fix-circle")
		// Epsilon beat Gamma instead: Gamma > Delta > Epsilon > Gamma.
		correct(t, eng, store, "tb-fix-circle", "Gamma", "Epsilon", "Epsilon")
		assert.Equal(t, state.CompStatusPools, loadComp(t, store, "tb-fix-circle").Status)
		outcome, err := eng.MaybeAutoCompletePools("tb-fix-circle")
		require.NoError(t, err)
		assert.Equal(t, AutoCompleteStarted, outcome, "the door's pass reports the league running again")
	})

	t.Run("a correction that still orders the round leaves it finished", func(t *testing.T) {
		eng, store := finishedAfterRound(t, "tb-fix-order")
		// Delta beat Gamma instead: Delta > Gamma > Epsilon, still ordered.
		correct(t, eng, store, "tb-fix-order", "Gamma", "Delta", "Delta")
		assert.Equal(t, state.CompStatusComplete, loadComp(t, store, "tb-fix-order").Status)
	})
}

// A pass that races the transaction which noted a reopen finds the league still
// finished, and must leave the note for the pass that follows the commit: taking
// it then lost the report (no competition_started) when the commit landed.
func TestLeagueBackInPlay_LeavesAnUncommittedNoteForTheCommit(t *testing.T) {
	eng, store := finishedLeague(t, "note-race")
	eng.leagueReopenNotes.Store("note-race", struct{}{})

	reopened, _, err := eng.leagueBackInPlay("note-race", loadComp(t, store, "note-race"))
	require.NoError(t, err)
	assert.False(t, reopened, "the transaction has not committed: the league still reads finished")

	// The transaction commits: the league is in match play.
	comp := loadComp(t, store, "note-race")
	comp.Status = state.CompStatusPools
	require.NoError(t, store.SaveCompetition(comp))

	reopened, fresh, err := eng.leagueBackInPlay("note-race", loadComp(t, store, "note-race"))
	require.NoError(t, err)
	assert.True(t, reopened, "the note survived the racing pass")
	assert.Equal(t, state.CompStatusPools, fresh.Status)

	reopened, _, err = eng.leagueBackInPlay("note-race", loadComp(t, store, "note-race"))
	require.NoError(t, err)
	assert.False(t, reopened, "the note is taken once")
}

// --- D6 (b): a league tie-breaker needs a winner ---

func TestLeagueTiebreaker_DrawnResultIsRefused(t *testing.T) {
	eng, store := setupTeamPoolComp(t, "dh-drawn", true)
	injected, err := eng.InjectPoolDaihyosenMatches("dh-drawn")
	require.NoError(t, err)
	require.NotEmpty(t, injected)
	dh := injected[0]

	_, err = eng.RecordMatchResultWithIneligibility("dh-drawn", dh.ID, &state.MatchResult{
		Status:   state.MatchStatusCompleted,
		Decision: string(domain.DecisionHikiwake),
	})
	require.ErrorIs(t, err, ErrTiebreakNeedsWinner)
	var ve *ValidationError
	require.ErrorAs(t, err, &ve)
	assert.Equal(t, errTiebreakNeedsWinnerSentence, ve.Msg)

	after, err := store.LoadPoolMatches("dh-drawn")
	require.NoError(t, err)
	for _, m := range after {
		if m.ID == dh.ID {
			assert.NotEqual(t, state.MatchStatusCompleted, m.Status, "nothing was written")
		}
	}
}

func TestLeagueTiebreaker_DrawnResultStandsInPoolsAndKnockout(t *testing.T) {
	eng, store := setupTeamPoolComp(t, "dh-mixed", true)
	asMixed(t, store, "dh-mixed")
	injected, err := eng.InjectPoolDaihyosenMatches("dh-mixed")
	require.NoError(t, err)
	require.NotEmpty(t, injected)

	_, err = eng.RecordMatchResultWithIneligibility("dh-mixed", injected[0].ID, &state.MatchResult{
		Status:   state.MatchStatusCompleted,
		Decision: string(domain.DecisionHikiwake),
	})
	assert.False(t, errors.Is(err, ErrTiebreakNeedsWinner), "pools + knockout keeps its drawn bout and lots")
}

func TestRefuseDrawnLeagueTiebreak(t *testing.T) {
	league := &state.Competition{Format: state.CompFormatLeague}
	drawn := &state.MatchResult{Status: state.MatchStatusCompleted}
	won := &state.MatchResult{Status: state.MatchStatusCompleted, Winner: "Alpha", WinnerID: "a"}
	running := &state.MatchResult{Status: state.MatchStatusRunning}

	assert.ErrorIs(t, refuseDrawnLeagueTiebreak(league, "Pool A-DH-0", drawn), ErrTiebreakNeedsWinner)
	assert.ErrorIs(t, refuseDrawnLeagueTiebreak(league, "Pool A-TB-0", drawn), ErrTiebreakNeedsWinner, "an individual league's tie-breaker too")
	assert.NoError(t, refuseDrawnLeagueTiebreak(league, "Pool A-DH-0", won))
	assert.NoError(t, refuseDrawnLeagueTiebreak(league, "Pool A-DH-0", running))
	assert.NoError(t, refuseDrawnLeagueTiebreak(league, "Pool A-0", drawn), "a regular league match may be drawn")
	assert.NoError(t, refuseDrawnLeagueTiebreak(&state.Competition{Format: state.CompFormatMixed}, "Pool A-DH-0", drawn))
	assert.NoError(t, refuseDrawnLeagueTiebreak(nil, "Pool A-DH-0", drawn))
}

// --- A fought tie-breaker round that went in a circle: the lots decide ---

// A tie-breaker round that went round in a circle (each team beat one and lost
// to one) leaves a tie at 1st the bouts cannot separate: the lots are offered,
// the league stays open until they are entered, and then it finishes with
// distinct places.
func TestLeagueCyclicRound_LotsFinishTheLeague(t *testing.T) {
	eng, store := setupTeamPoolComp(t, "dh-cycle", true)
	injected, err := eng.InjectPoolDaihyosenMatches("dh-cycle")
	require.NoError(t, err)
	require.Len(t, injected, 3)

	all, err := store.LoadPoolMatches("dh-cycle")
	require.NoError(t, err)
	// A cycle: Alpha beats Beta, Beta beats Gamma, Gamma beats Alpha.
	for i := range all {
		if !IsPoolDaihyosenMatchID(all[i].ID) {
			continue
		}
		all[i].Status = state.MatchStatusCompleted
		pair := map[string]bool{all[i].SideA: true, all[i].SideB: true}
		if pair["Alpha"] && pair["Gamma"] {
			if all[i].SideA == "Gamma" {
				all[i].Winner, all[i].WinnerID, all[i].WinnerSide = all[i].SideA, all[i].SideAID, "A"
			} else {
				all[i].Winner, all[i].WinnerID, all[i].WinnerSide = all[i].SideB, all[i].SideBID, "B"
			}
		}
		if pair["Beta"] && pair["Gamma"] {
			if all[i].SideA == "Beta" {
				all[i].Winner, all[i].WinnerID, all[i].WinnerSide = all[i].SideA, all[i].SideAID, "A"
			} else {
				all[i].Winner, all[i].WinnerID, all[i].WinnerSide = all[i].SideB, all[i].SideBID, "B"
			}
		}
		if pair["Alpha"] && pair["Beta"] {
			if all[i].SideA == "Alpha" {
				all[i].Winner, all[i].WinnerID, all[i].WinnerSide = all[i].SideA, all[i].SideAID, "A"
			} else {
				all[i].Winner, all[i].WinnerID, all[i].WinnerSide = all[i].SideB, all[i].SideBID, "B"
			}
		}
	}
	require.NoError(t, store.SavePoolMatches("dh-cycle", all))
	eng.standingsCache.Delete("dh-cycle")
	eng.standingsFlight.Delete("dh-cycle")

	outcome, err := eng.MaybeAutoCompletePools("dh-cycle")
	require.NoError(t, err)
	require.NotEqual(t, AutoCompleteTransitioned, outcome, "an unbroken tie at 1st keeps the league open")
	assert.Equal(t, PoolsChusenOwed, poolsVerdictNow(t, eng, store, "dh-cycle"))

	report, err := eng.ChusenStatus("dh-cycle")
	require.NoError(t, err)
	require.Len(t, report.Pending, 1, "the lots are offered for the circle")
	assert.Equal(t, 1, report.Pending[0].MinPosition)

	for rank, name := range []string{"Beta", "Gamma", "Alpha"} {
		require.NoError(t, store.SaveRankOverride("dh-cycle", "Pool A", teamID(name), rank+1))
	}
	eng.standingsCache.Delete("dh-cycle")
	eng.standingsFlight.Delete("dh-cycle")
	outcome, err = eng.MaybeAutoCompletePools("dh-cycle")
	require.NoError(t, err)
	assert.Equal(t, AutoCompleteTransitioned, outcome, "the lots settle the circle")
	assert.Equal(t, state.CompStatusComplete, loadComp(t, store, "dh-cycle").Status)
}

// --- The both-sides-barred draw on a league tie-breaker ---

// "Record as drawn (neither can fight)" on a league tie-breaker is let through
// for a team bout and an individual one alike: the place is then settled by
// the lots (barredDrawOwesLots). A plain draw through the score door is still
// refused.
func TestRefuseDrawnLeagueTiebreak_BothBarredDrawIsLetThrough(t *testing.T) {
	league := &state.Competition{Format: state.CompFormatLeague}
	barred := &state.MatchResult{Status: state.MatchStatusCompleted,
		Decision: string(domain.DecisionHikiwake), WriteDoor: DoorDecision}
	scored := &state.MatchResult{Status: state.MatchStatusCompleted,
		Decision: string(domain.DecisionHikiwake)}

	assert.NoError(t, refuseDrawnLeagueTiebreak(league, "Pool A-DH-0", barred), "a team tie-breaker")
	assert.NoError(t, refuseDrawnLeagueTiebreak(league, "Pool A-TB-0", barred), "an individual tie-breaker")
	assert.NoError(t, refuseDrawnLeagueTiebreak(league, "Pool A-0", barred), "a regular league match")
	assert.ErrorIs(t, refuseDrawnLeagueTiebreak(league, "Pool A-DH-0", scored), ErrTiebreakNeedsWinner)
	assert.ErrorIs(t, refuseDrawnLeagueTiebreak(league, "Pool A-TB-0", scored), ErrTiebreakNeedsWinner)
}

// A participant's team-member add is refused on the finished league and works
// once a correction puts it back into match play: the reopen is a real reopen
// for the writes that ask whether the competition has finished.
func TestLeagueCorrection_ReopensTeamMemberWrites(t *testing.T) {
	eng, store := finishedLeague(t, "undo-members")
	alpha := leagueTeamID(t, store, "undo-members", "Alpha")
	// The roster the draw was made from: the pool's teams, under the same ids.
	pools, err := store.LoadPools("undo-members")
	require.NoError(t, err)
	var roster []domain.Player
	for _, p := range pools {
		for _, pl := range p.Players {
			if pl.Dojo == "" {
				pl.Dojo = "Dojo " + pl.Name
			}
			roster = append(roster, pl)
		}
	}
	require.NoError(t, store.SaveParticipants("undo-members", roster))

	_, err = store.AddTeamMemberUpTo("undo-members", alpha, "Late Arrival", 0)
	require.ErrorIs(t, err, state.ErrCompetitionFinished)

	tieAt1st(t, eng, "undo-members")
	member, err := store.AddTeamMemberUpTo("undo-members", alpha, "Late Arrival", 0)
	require.NoError(t, err)
	assert.Equal(t, "Late Arrival", member.Name)
}

// The match lineups saved while the league was played survive the reopen: it
// moves the status and the courts, never a team's lineup.
func TestLeagueCorrection_ReopenKeepsMatchLineups(t *testing.T) {
	eng, store := finishedLeague(t, "undo-lineups")
	alpha := leagueTeamID(t, store, "undo-lineups", "Alpha")
	require.NoError(t, store.SetTeamLineup("undo-lineups", domain.TeamLineup{
		TeamID: alpha, CompetitionID: "undo-lineups", MatchID: "Pool A-0",
		Positions: map[domain.Position]string{"1": "First", "2": "Second"},
	}, 2))
	before, err := store.LoadTeamLineups("undo-lineups")
	require.NoError(t, err)
	require.NotEmpty(t, before)

	tieAt1st(t, eng, "undo-lineups")
	require.Equal(t, state.CompStatusPools, loadComp(t, store, "undo-lineups").Status)
	after, err := store.LoadTeamLineups("undo-lineups")
	require.NoError(t, err)
	assert.Equal(t, before, after)
}

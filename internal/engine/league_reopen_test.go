package engine

import (
	"errors"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/gitrgoliveira/bracket-creator/internal/domain"
	"github.com/gitrgoliveira/bracket-creator/internal/state"
)

// finishedLeague sets up a team league whose table carries the scenario's tie
// and marks it as the finalize door leaves it: shared ranks accepted, league
// completed.
func finishedLeague(t *testing.T, compID, scenario string) (*Engine, *state.Store) {
	t.Helper()
	eng, store := setupTeamLeagueComp(t, compID, scenario)
	comp, err := store.LoadCompetition(compID)
	require.NoError(t, err)
	comp.LeagueTiebreakFinalized = true
	comp.Status = state.CompStatusComplete
	require.NoError(t, store.SaveCompetition(comp))
	require.NoError(t, store.SaveTournament(&state.Tournament{Name: "T", Courts: []string{"A"}}))
	return eng, store
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

// --- Undo (DELETE .../league-tiebreak/finalize) ---

func TestUndoLeagueSharedRanks_ReopensFinishedLeagueAndClearsDeadCourts(t *testing.T) {
	eng, store := finishedLeague(t, "undo-finished", "topTie")
	// Shiaijo A was removed while the league was completed.
	require.NoError(t, store.SaveTournament(&state.Tournament{Name: "T", Courts: []string{"B"}}))

	out, err := eng.UndoLeagueSharedRanks("undo-finished")
	require.NoError(t, err)
	assert.True(t, out.Reopened)
	assert.Equal(t, []string{"A"}, out.RemovedCourts)
	assert.Len(t, out.UnassignedMatches, 6, "every league match sat on the removed shiaijo")

	comp := loadComp(t, store, "undo-finished")
	assert.Equal(t, state.CompStatusPools, comp.Status, "back in match play")
	assert.False(t, comp.LeagueTiebreakFinalized)
	matches, err := store.LoadPoolMatches("undo-finished")
	require.NoError(t, err)
	for _, m := range matches {
		assert.Empty(t, m.Court, "%s lost the removed shiaijo", m.ID)
		assert.Equal(t, state.MatchStatusCompleted, m.Status, "%s keeps its result", m.ID)
	}

	cands, err := eng.LeagueTiebreakCandidates("undo-finished")
	require.NoError(t, err)
	assert.Len(t, cands, 1, "the tie is asked again")
}

func TestUndoLeagueSharedRanks_LiveCourtsAreKept(t *testing.T) {
	eng, store := finishedLeague(t, "undo-courts", "topTie")
	out, err := eng.UndoLeagueSharedRanks("undo-courts")
	require.NoError(t, err)
	assert.True(t, out.Reopened)
	assert.Empty(t, out.UnassignedMatches)
	matches, err := store.LoadPoolMatches("undo-courts")
	require.NoError(t, err)
	for _, m := range matches {
		assert.Equal(t, "A", m.Court)
	}
}

func TestUndoLeagueSharedRanks_InPlayClearsTheFlagOnly(t *testing.T) {
	eng, store := setupTeamLeagueComp(t, "undo-inplay", "topTie", func(c *state.Competition) {
		c.LeagueTiebreakFinalized = true
	})
	out, err := eng.UndoLeagueSharedRanks("undo-inplay")
	require.NoError(t, err)
	assert.False(t, out.Reopened)
	comp := loadComp(t, store, "undo-inplay")
	assert.Equal(t, state.CompStatusPools, comp.Status)
	assert.False(t, comp.LeagueTiebreakFinalized)
}

func TestUndoLeagueSharedRanks_Refusals(t *testing.T) {
	eng, store := setupTeamLeagueComp(t, "undo-not-accepted", "topTie")

	_, err := eng.UndoLeagueSharedRanks("undo-not-accepted")
	assert.ErrorIs(t, err, ErrLeagueNotFinalized)

	_, err = eng.UndoLeagueSharedRanks("no-such-competition")
	var nf *NotFoundError
	assert.ErrorAs(t, err, &nf)

	require.NoError(t, store.SaveCompetition(&state.Competition{
		ID: "undo-mixed", Format: state.CompFormatMixed, Status: state.CompStatusPools,
		Kind: "team", TeamSize: 2, LeagueTiebreakFinalized: true,
	}))
	_, err = eng.UndoLeagueSharedRanks("undo-mixed")
	var ve *ValidationError
	assert.ErrorAs(t, err, &ve)
	assert.True(t, loadComp(t, store, "undo-mixed").LeagueTiebreakFinalized, "a refusal writes nothing")
}

// --- The completed-league reconciler (reconcileLeagueAfterWrite) ---

// A correction that keeps the accepted tie where it was leaves the league
// finished: its table and podium follow the corrected result.
func TestLeagueCorrection_KeepingTheAcceptedTieStaysFinished(t *testing.T) {
	eng, store := finishedLeague(t, "fix-keeps", "topTie")
	// Gamma beat Delta; the correction gives it to Delta. Alpha and Beta
	// still share 1st.
	_, err := eng.RecordMatchResultWithIneligibility("fix-keeps", "Pool A-5", teamWin(t, store, "fix-keeps", "Gamma", "Delta", "B"))
	require.NoError(t, err)

	comp := loadComp(t, store, "fix-keeps")
	assert.Equal(t, state.CompStatusComplete, comp.Status)
	assert.True(t, comp.LeagueTiebreakFinalized)
	outcome, err := eng.MaybeAutoCompletePools("fix-keeps")
	require.NoError(t, err)
	assert.Equal(t, AutoCompleteNoChange, outcome)
}

// A correction that settles a tie (no tie left) also leaves it finished.
func TestLeagueCorrection_SettlingTheTieStaysFinished(t *testing.T) {
	eng, store := finishedLeague(t, "fix-settles", "bottomTie")
	// Beta beat Delta; drawn instead, Gamma and Delta no longer share 3rd.
	_, err := eng.RecordMatchResultWithIneligibility("fix-settles", "Pool A-4", teamDraw("Beta", "Delta"))
	require.NoError(t, err)
	assert.Equal(t, state.CompStatusComplete, loadComp(t, store, "fix-settles").Status)
}

// A correction that makes a tie the operator never saw puts the league back
// into match play, so the tie is asked again, and the door's pass reports it
// as a start (competition_started).
func TestLeagueCorrection_NewTieReopensTheLeague(t *testing.T) {
	eng, store := finishedLeague(t, "fix-newtie", "topTie")
	// Gamma beat Delta; drawn instead, Gamma and Delta share 3rd, in the band.
	_, err := eng.RecordMatchResultWithIneligibility("fix-newtie", "Pool A-5", teamDraw("Gamma", "Delta"))
	require.NoError(t, err)

	comp := loadComp(t, store, "fix-newtie")
	assert.Equal(t, state.CompStatusPools, comp.Status, "back in match play")
	assert.False(t, comp.LeagueTiebreakFinalized, "the accepted ranks went with the table")

	outcome, err := eng.MaybeAutoCompletePools("fix-newtie")
	require.NoError(t, err)
	assert.Equal(t, AutoCompleteStarted, outcome)
	again, err := eng.MaybeAutoCompletePools("fix-newtie")
	require.NoError(t, err)
	assert.NotEqual(t, AutoCompleteStarted, again, "the note is taken once")
	assert.NotEqual(t, AutoCompleteTransitioned, again, "the new tie keeps the league open")

	cands, err := eng.LeagueTiebreakCandidates("fix-newtie")
	require.NoError(t, err)
	assert.Len(t, cands, 2, "both ties are asked")
}

// A regular match reopened on a finished league puts the league back into
// match play when the door's pass runs.
func TestLeagueReopenedMatch_PutsTheLeagueBackInPlay(t *testing.T) {
	eng, store := finishedLeague(t, "reopen-match", "topTie")
	// A withdrawal recorded on the finished league (Delta takes the match, the
	// accepted tie at 1st stands, so it stays finished), then reopened.
	_, _, err := eng.RecordDecision("reopen-match", "Pool A-5", "kiken", "aka", "", nil)
	require.NoError(t, err)
	require.Equal(t, state.CompStatusComplete, loadComp(t, store, "reopen-match").Status)
	_, err = eng.ReopenMatch("reopen-match", "Pool A-5", "wrong call")
	require.NoError(t, err)

	outcome, err := eng.MaybeAutoCompletePools("reopen-match")
	require.NoError(t, err)
	assert.Equal(t, AutoCompleteStarted, outcome)
	comp := loadComp(t, store, "reopen-match")
	assert.Equal(t, state.CompStatusPools, comp.Status)
	assert.False(t, comp.LeagueTiebreakFinalized)
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
	eng, store := finishedLeague(t, "note-race", "topTie")
	leagueReopenNotes.Store(leagueReopenNote{e: eng, compID: "note-race"}, struct{}{})

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

// Shared ranks accepted while matches are still played go when a result
// changes: the table they were accepted on is gone.
func TestLeagueResultChange_InPlayTakesBackAcceptedRanks(t *testing.T) {
	eng, store := setupTeamLeagueComp(t, "inplay-change", "topTie", func(c *state.Competition) {
		c.LeagueTiebreakFinalized = true
	})
	_, err := eng.RecordMatchResultWithIneligibility("inplay-change", "Pool A-5", teamWin(t, store, "inplay-change", "Gamma", "Delta", "B"))
	require.NoError(t, err)
	comp := loadComp(t, store, "inplay-change")
	assert.False(t, comp.LeagueTiebreakFinalized)
	assert.NotEqual(t, state.CompStatusComplete, comp.Status)
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

// --- Accept on a fought, cyclic tie-breaker round ---

// A tie-breaker round that went round in a circle (each team beat one and lost
// to one) leaves the tie unbroken; in a league that is the operator's to accept
// as shared ranks, and the Accept finishes the league (no lots).
func TestLeagueAccept_OnACyclicRoundFinishesTheLeague(t *testing.T) {
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
	require.NotEqual(t, AutoCompleteTransitioned, outcome, "an unbroken tie keeps the league open until the operator decides")

	comp := loadComp(t, store, "dh-cycle")
	comp.LeagueTiebreakFinalized = true
	require.NoError(t, store.SaveCompetition(comp))
	outcome, err = eng.MaybeAutoCompletePools("dh-cycle")
	require.NoError(t, err)
	assert.Equal(t, AutoCompleteTransitioned, outcome, "accepted shared ranks finish the league")
	assert.Equal(t, state.CompStatusComplete, loadComp(t, store, "dh-cycle").Status)
}

// --- The accepted shared ranks, as the candidates read shows them ---

func TestLeagueSharedRanks(t *testing.T) {
	eng, _ := finishedLeague(t, "shared-ranks", "topTie")
	groups, err := eng.LeagueSharedRanks("shared-ranks")
	require.NoError(t, err)
	require.Len(t, groups, 1)
	assert.Equal(t, 1, groups[0].MinPosition)
	names := []string{}
	for _, p := range groups[0].Teams {
		names = append(names, p.Player.Name)
	}
	assert.ElementsMatch(t, []string{"Alpha", "Beta"}, names)

	eng2, _ := setupTeamLeagueComp(t, "shared-none", "topTie")
	groups, err = eng2.LeagueSharedRanks("shared-none")
	require.NoError(t, err)
	assert.Nil(t, groups, "nothing accepted, nothing shown")

	_, err = eng2.LeagueSharedRanks("no-such-competition")
	var nf *NotFoundError
	assert.ErrorAs(t, err, &nf)
}

// --- After Undo, the league's own verdict owes the tie again ---

func TestUndoLeagueSharedRanks_VerdictOwesTheTieAgain(t *testing.T) {
	eng, store := finishedLeague(t, "undo-verdict", "topTie")
	_, err := eng.UndoLeagueSharedRanks("undo-verdict")
	require.NoError(t, err)
	assert.Equal(t, PoolsTiebreakOwed, poolsVerdictNow(t, eng, store, "undo-verdict"),
		"POST /complete agrees with the reopened league")
}

// --- The both-sides-barred hikiwake on a league tie-breaker ---

func TestLeagueTiebreaker_BothBarredHikiwakeIsRefused(t *testing.T) {
	eng, store := setupTeamPoolComp(t, "dh-barred", true)
	injected, err := eng.InjectPoolDaihyosenMatches("dh-barred")
	require.NoError(t, err)
	require.NotEmpty(t, injected)
	dh := injected[0]

	_, err = eng.RecordMatchResultWithIneligibility("dh-barred", dh.ID, &state.MatchResult{
		Status:    state.MatchStatusCompleted,
		Decision:  string(domain.DecisionHikiwake),
		Changed:   []string{state.GroupResult},
		WriteDoor: DoorDecision,
	})
	require.ErrorIs(t, err, ErrTiebreakNeedsWinner)
	var ve *ValidationError
	require.ErrorAs(t, err, &ve)
	assert.Equal(t, errTiebreakBothBarredSentence+" "+errTiebreakBothBarredTeamRemedy, ve.Msg)

	after, err := store.LoadPoolMatches("dh-barred")
	require.NoError(t, err)
	for _, m := range after {
		if m.ID == dh.ID {
			assert.NotEqual(t, state.MatchStatusCompleted, m.Status, "nothing was written")
		}
	}
}

func TestRefuseDrawnLeagueTiebreak_BothBarredSentences(t *testing.T) {
	league := &state.Competition{Format: state.CompFormatLeague}
	barred := &state.MatchResult{Status: state.MatchStatusCompleted,
		Decision: string(domain.DecisionHikiwake), WriteDoor: DoorDecision}

	err := refuseDrawnLeagueTiebreak(league, "Pool A-DH-0", barred)
	var ve *ValidationError
	require.ErrorAs(t, err, &ve)
	assert.Equal(t, errTiebreakBothBarredSentence+" "+errTiebreakBothBarredTeamRemedy, ve.Msg)

	assert.NoError(t, refuseDrawnLeagueTiebreak(league, "Pool A-TB-0", barred),
		"an individual league has no Accept to point at: its barred ippon-shobu draw is the exemption")

	assert.NoError(t, refuseDrawnLeagueTiebreak(league, "Pool A-0", barred), "a regular league match keeps the carve-out")
}

// A participant's team-member add is refused on the finished league and works
// once Undo puts it back into match play: the reopen is a real reopen for the
// writes that ask whether the competition has finished.
func TestUndoLeagueSharedRanks_ReopensTeamMemberWrites(t *testing.T) {
	eng, store := finishedLeague(t, "undo-members", "topTie")
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

	_, err = eng.UndoLeagueSharedRanks("undo-members")
	require.NoError(t, err)
	member, err := store.AddTeamMemberUpTo("undo-members", alpha, "Late Arrival", 0)
	require.NoError(t, err)
	assert.Equal(t, "Late Arrival", member.Name)
}

// The match lineups saved while the league was played survive the reopen:
// Undo moves the status and the courts, never a team's lineup.
func TestUndoLeagueSharedRanks_KeepsMatchLineups(t *testing.T) {
	eng, store := finishedLeague(t, "undo-lineups", "topTie")
	alpha := leagueTeamID(t, store, "undo-lineups", "Alpha")
	require.NoError(t, store.SetTeamLineup("undo-lineups", domain.TeamLineup{
		TeamID: alpha, CompetitionID: "undo-lineups", MatchID: "Pool A-0",
		Positions: map[domain.Position]string{"1": "First", "2": "Second"},
	}, 2))
	before, err := store.LoadTeamLineups("undo-lineups")
	require.NoError(t, err)
	require.NotEmpty(t, before)

	_, err = eng.UndoLeagueSharedRanks("undo-lineups")
	require.NoError(t, err)
	after, err := store.LoadTeamLineups("undo-lineups")
	require.NoError(t, err)
	assert.Equal(t, before, after)
}

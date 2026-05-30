package engine

import (
	"errors"
	"os"
	"testing"

	"github.com/gitrgoliveira/bracket-creator/internal/domain"
	"github.com/gitrgoliveira/bracket-creator/internal/helper"
	"github.com/gitrgoliveira/bracket-creator/internal/state"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestGetBracketRanking(t *testing.T) {
	dir, err := os.MkdirTemp("", "engine-ranking-test-*")
	require.NoError(t, err)
	defer os.RemoveAll(dir)

	store, err := state.NewStore(dir)
	require.NoError(t, err)
	eng := New(store)

	compID := "test-comp"
	comp := &state.Competition{
		ID:   compID,
		Name: "Test Comp",
	}
	require.NoError(t, store.SaveCompetition(comp))

	players := []domain.Player{
		{Name: "Alice", Dojo: "DojoA"},
		{Name: "Bob", Dojo: "DojoB"},
		{Name: "Charlie", Dojo: "DojoC"},
		{Name: "Dave", Dojo: "DojoD"},
	}
	require.NoError(t, store.SaveParticipants(compID, players))

	bracket := &state.Bracket{
		Rounds: [][]state.BracketMatch{
			{
				{ID: "M1", SideA: "Alice", SideB: "Bob", Winner: "Alice", Status: state.MatchStatusCompleted},
				{ID: "M2", SideA: "Charlie", SideB: "Dave", Winner: "Charlie", Status: state.MatchStatusCompleted},
			},
			{
				{ID: "M3", SideA: "Alice", SideB: "Charlie", Winner: "Alice", Status: state.MatchStatusCompleted},
			},
		},
	}
	require.NoError(t, store.SaveBracket(compID, bracket))

	tests := []struct {
		rank     int
		wantName string
		wantErr  bool
	}{
		{rank: 1, wantName: "Alice", wantErr: false},
		{rank: 2, wantName: "Charlie", wantErr: false},
		{rank: 3, wantName: "Bob", wantErr: false},
		{rank: 4, wantName: "Dave", wantErr: false},
		{rank: 5, wantErr: true},
	}

	for _, tt := range tests {
		player, err := eng.GetBracketRanking(compID, tt.rank)
		if tt.wantErr {
			assert.Error(t, err)
		} else {
			require.NoError(t, err)
			assert.Equal(t, tt.wantName, player.Name)
		}
	}
}

func TestGetBracketRanking_Errors(t *testing.T) {
	dir, err := os.MkdirTemp("", "engine-ranking-err-test-*")
	require.NoError(t, err)
	defer os.RemoveAll(dir)

	store, err := state.NewStore(dir)
	require.NoError(t, err)
	eng := New(store)

	// No bracket
	_, err = eng.GetBracketRanking("nonexistent", 1)
	assert.Error(t, err)

	// Empty bracket
	compID := "empty"
	require.NoError(t, store.SaveCompetition(&state.Competition{ID: compID, Name: "Empty"}))
	require.NoError(t, store.SaveBracket(compID, &state.Bracket{Rounds: [][]state.BracketMatch{}}))
	_, err = eng.GetBracketRanking(compID, 1)
	assert.Error(t, err)
}

// TestResolvePoolWinners verifies that a playoffs competition linked to a
// finalized mixed source resolves its roster from the source's pool winners
// (ranks 1..totalWinners) via GetPoolRanking. With 2 source participants the
// default sizing (poolSize 3 → 1 pool, winners 2) yields totalWinners = 2.
func TestResolvePoolWinners(t *testing.T) {
	eng, store, _ := setupTestEngine(t)

	srcID := "src-mixed"
	require.NoError(t, store.SaveCompetition(&state.Competition{
		ID:     srcID,
		Name:   "Source Mixed",
		Format: state.CompFormatMixed,
		Status: state.CompStatusComplete,
	}))
	require.NoError(t, store.SaveParticipants(srcID, []domain.Player{
		{Name: "Alice", Dojo: "DojoA"},
		{Name: "Bob", Dojo: "DojoB"},
	}))
	require.NoError(t, store.SavePools(srcID, []helper.Pool{
		{PoolName: "Pool A", Players: []helper.Player{{Name: "Alice"}, {Name: "Bob"}}},
	}))
	require.NoError(t, store.SavePoolMatches(srcID, []state.MatchResult{
		{ID: "Pool A-0", SideA: "Alice", SideB: "Bob", Winner: "Alice",
			IpponsA: []string{"M"}, Status: state.MatchStatusCompleted},
	}))

	playoff := &state.Competition{
		ID:           "src-mixed-playoffs",
		Name:         "Source Mixed - Playoffs",
		Format:       state.CompFormatPlayoffs,
		SourceCompID: srcID,
	}
	require.NoError(t, store.SaveCompetition(playoff))

	roster, err := eng.resolvePoolWinners(playoff)
	require.NoError(t, err)
	require.Len(t, roster, 2, "1 pool × 2 winners = 2 qualifiers")
	assert.Equal(t, "Alice", roster[0].Name, "rank 1 = pool winner")
	assert.Equal(t, "Bob", roster[1].Name, "rank 2 = pool runner-up")
}

// TestResolvePoolWinners_SourceNotFinal verifies that resolving before the
// source's pools are final returns a clear validation error rather than a
// partial roster.
func TestResolvePoolWinners_SourceNotFinal(t *testing.T) {
	eng, store, _ := setupTestEngine(t)

	srcID := "src-pending"
	require.NoError(t, store.SaveCompetition(&state.Competition{
		ID:     srcID,
		Name:   "Pending Source",
		Format: state.CompFormatMixed,
		Status: state.CompStatusPools, // not final yet
	}))
	playoff := &state.Competition{ID: "p", Name: "P", Format: state.CompFormatPlayoffs, SourceCompID: srcID}

	_, err := eng.resolvePoolWinners(playoff)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "not final")
}

// TestResolvePoolWinners_SourceNotFound verifies a missing SourceCompID
// surfaces a not-found error.
func TestResolvePoolWinners_SourceNotFound(t *testing.T) {
	eng, _, _ := setupTestEngine(t)
	playoff := &state.Competition{ID: "p", Name: "P", Format: state.CompFormatPlayoffs, SourceCompID: "ghost"}
	_, err := eng.resolvePoolWinners(playoff)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "not found")
}

// TestGetPoolRanking_Basic verifies that rank 1 returns the winner of
// pool 1, rank 2 the winner of pool 2, rank 3 the runner-up of pool 1, etc.
func TestGetPoolRanking_Basic(t *testing.T) {
	eng, store, _ := setupTestEngine(t)
	compID := "pool-ranking"

	require.NoError(t, store.SaveCompetition(&state.Competition{
		ID:     compID,
		Name:   "Pool Ranking",
		Format: state.CompFormatMixed,
		Status: state.CompStatusComplete,
	}))

	// Two players so we get one pool and one match.
	require.NoError(t, store.SaveParticipants(compID, []domain.Player{
		{Name: "Alice", Dojo: "DojoA"},
		{Name: "Bob", Dojo: "DojoB"},
	}))

	// Save pool structure so CalculatePoolStandings has pool info.
	require.NoError(t, store.SavePools(compID, []helper.Pool{
		{
			PoolName: "Pool A",
			Players: []helper.Player{
				{Name: "Alice"},
				{Name: "Bob"},
			},
		},
	}))

	// Alice beats Bob.
	require.NoError(t, store.SavePoolMatches(compID, []state.MatchResult{
		{
			ID:      "Pool A-0",
			SideA:   "Alice",
			SideB:   "Bob",
			Winner:  "Alice",
			IpponsA: []string{"M"},
			Status:  state.MatchStatusCompleted,
		},
	}))

	p, err := eng.GetPoolRanking(compID, 1)
	require.NoError(t, err)
	assert.Equal(t, "Alice", p.Name, "rank 1 must be the pool winner")

	p, err = eng.GetPoolRanking(compID, 2)
	require.NoError(t, err)
	assert.Equal(t, "Bob", p.Name, "rank 2 must be the pool runner-up")
}

// TestGetPoolRanking_NotFound verifies that a competition with no pool
// data returns a not-found error.
func TestGetPoolRanking_NotFound(t *testing.T) {
	eng, store, _ := setupTestEngine(t)
	compID := "pool-ranking-empty"

	require.NoError(t, store.SaveCompetition(&state.Competition{
		ID:     compID,
		Name:   "Empty",
		Format: state.CompFormatMixed,
	}))

	_, err := eng.GetPoolRanking(compID, 1)
	assert.Error(t, err)
}

// TestGetPoolRanking_OutOfRange verifies that requesting a rank beyond
// the pool's depth returns an error.
func TestGetPoolRanking_OutOfRange(t *testing.T) {
	eng, store, _ := setupTestEngine(t)
	compID := "pool-ranking-oob"

	require.NoError(t, store.SaveCompetition(&state.Competition{
		ID:     compID,
		Name:   "OOB",
		Format: state.CompFormatMixed,
		Status: state.CompStatusComplete,
	}))

	require.NoError(t, store.SaveParticipants(compID, []domain.Player{
		{Name: "Alice"}, {Name: "Bob"},
	}))
	require.NoError(t, store.SavePools(compID, []helper.Pool{
		{PoolName: "Pool A", Players: []helper.Player{{Name: "Alice"}, {Name: "Bob"}}},
	}))
	require.NoError(t, store.SavePoolMatches(compID, []state.MatchResult{
		{ID: "Pool A-0", SideA: "Alice", SideB: "Bob", Winner: "Alice", Status: state.MatchStatusCompleted},
	}))

	// Pool has 2 players, so rank 100 should not be found.
	_, err := eng.GetPoolRanking(compID, 100)
	assert.Error(t, err)
}

// TestCalculatePoolStandings_TeamSubDraw covers the sub.Winner=="" branch in
// computeStandings (lines 341-343). In a best-of-3 team kendo match each
// position fights individually; a position where both fighters score 2 ippons
// each is impossible in normal play (the bout ends when one side reaches 2)
// but valid to construct in tests to exercise the IndividualDraws counter.
func TestCalculatePoolStandings_TeamSubDraw(t *testing.T) {
	eng, store, _ := setupTestEngine(t)
	compID := "team-sub-draw"

	require.NoError(t, store.SaveCompetition(&state.Competition{
		ID:       compID,
		Name:     "Team Sub Draw",
		Kind:     "team",
		Format:   state.CompFormatMixed,
		Status:   state.CompStatusPools,
		TeamSize: 3,
	}))

	require.NoError(t, store.SaveParticipants(compID, []domain.Player{
		{Name: "TeamA"}, {Name: "TeamB"},
	}))
	require.NoError(t, store.SavePools(compID, []helper.Pool{
		{PoolName: "Pool A", Players: []helper.Player{
			{Name: "TeamA"}, {Name: "TeamB"},
		}},
	}))

	// Team match is a draw (Winner==""), one sub-bout is also a draw:
	// 1-1 ippons with time expired — valid in best-of-3 (neither side
	// reached 2 before the clock ran out).
	require.NoError(t, store.SavePoolMatches(compID, []state.MatchResult{
		{
			ID:     "Pool A-0",
			SideA:  "TeamA",
			SideB:  "TeamB",
			Winner: "",
			Status: state.MatchStatusCompleted,
			SubResults: []state.SubMatchResult{
				{Position: 0, SideA: "A1", SideB: "B1",
					IpponsA: []string{"M"}, IpponsB: []string{"M"},
					Winner: ""},
			},
		},
	}))

	standings, err := eng.CalculatePoolStandings(compID)
	require.NoError(t, err)
	poolStandings := standings["Pool A"]
	require.Len(t, poolStandings, 2)

	// Both teams drew the match, each sub-bout is also a draw.
	for _, s := range poolStandings {
		assert.Equal(t, 1, s.Draws, "%s: team match must be a draw", s.Player.Name)
		assert.Equal(t, 1, s.IndividualDraws, "%s: sub-bout draw must increment IndividualDraws", s.Player.Name)
	}
}

// TestStartCompetition_PlayoffsFromSource exercises the end-to-end pools→playoffs
// transition (mp-j39, replacing the reserved-slot flow): a playoffs competition
// linked to a finalized mixed source via SourceCompID resolves its roster from
// the source's pool winners at start, persists it, then generates the bracket.
func TestStartCompetition_PlayoffsFromSource(t *testing.T) {
	eng, store, _ := setupTestEngine(t)

	// Finalized mixed source: one pool of two, Alice beats Bob.
	srcID := "src-mixed"
	require.NoError(t, store.SaveCompetition(&state.Competition{
		ID: srcID, Name: "Source Mixed", Format: state.CompFormatMixed,
		Status: state.CompStatusComplete,
	}))
	require.NoError(t, store.SaveParticipants(srcID, []domain.Player{
		{Name: "Alice", Dojo: "DojoA"}, {Name: "Bob", Dojo: "DojoB"},
	}))
	require.NoError(t, store.SavePools(srcID, []helper.Pool{
		{PoolName: "Pool A", Players: []helper.Player{{Name: "Alice"}, {Name: "Bob"}}},
	}))
	require.NoError(t, store.SavePoolMatches(srcID, []state.MatchResult{
		{ID: "Pool A-0", SideA: "Alice", SideB: "Bob", Winner: "Alice",
			IpponsA: []string{"M"}, Status: state.MatchStatusCompleted},
	}))

	// Playoffs comp linked to the source — empty roster on disk.
	playoffID := "src-playoffs"
	require.NoError(t, store.SaveCompetition(&state.Competition{
		ID: playoffID, Name: "Source Mixed - Playoffs",
		Format: state.CompFormatPlayoffs, SourceCompID: srcID,
	}))

	require.NoError(t, eng.StartCompetition(playoffID))

	// Roster resolved from the source's pool winners and persisted.
	roster, err := store.LoadParticipants(playoffID, false)
	require.NoError(t, err)
	require.Len(t, roster, 2, "1 pool × 2 winners")
	assert.ElementsMatch(t, []string{"Alice", "Bob"},
		[]string{roster[0].Name, roster[1].Name})

	// Bracket generated from the resolved roster.
	bracket, err := store.LoadBracket(playoffID)
	require.NoError(t, err)
	require.NotNil(t, bracket)
	assert.NotEmpty(t, bracket.Rounds, "bracket must be built from the resolved roster")

	comp, err := store.LoadCompetition(playoffID)
	require.NoError(t, err)
	assert.Equal(t, state.CompStatusPlayoffs, comp.Status,
		"StartCompetition runs the draw AND transitions a playoffs comp to playoffs")
	assert.True(t, comp.HasParticipantIDs,
		"resolved roster persisted with UUID ids → HasParticipantIDs flipped")
}

// TestStartCompetition_PlayoffsFromSource_NotFinal verifies that starting a
// source-linked playoffs comp before the source's pools are final fails with a
// clear error rather than generating a bracket from a partial roster.
func TestStartCompetition_PlayoffsFromSource_NotFinal(t *testing.T) {
	eng, store, _ := setupTestEngine(t)

	srcID := "src-pending"
	require.NoError(t, store.SaveCompetition(&state.Competition{
		ID: srcID, Name: "Pending", Format: state.CompFormatMixed,
		Status: state.CompStatusPools, // not final yet
	}))
	playoffID := "pending-playoffs"
	require.NoError(t, store.SaveCompetition(&state.Competition{
		ID: playoffID, Name: "Pending - Playoffs",
		Format: state.CompFormatPlayoffs, SourceCompID: srcID,
	}))

	err := eng.StartCompetition(playoffID)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "not final")

	// No bracket should have been generated.
	bracket, _ := store.LoadBracket(playoffID)
	if bracket != nil {
		assert.Empty(t, bracket.Rounds, "no bracket on a failed start")
	}
}

// TestResolvePoolWinners_NonMixedSource verifies the API-contract guard: a
// source competition that is not mixed format is rejected even if it has
// finalized pools (GetPoolRanking would otherwise mis-resolve a non-pool
// source).
func TestResolvePoolWinners_NonMixedSource(t *testing.T) {
	eng, store, _ := setupTestEngine(t)

	srcID := "src-league"
	require.NoError(t, store.SaveCompetition(&state.Competition{
		ID: srcID, Name: "League Src", Format: state.CompFormatLeague,
		Status: state.CompStatusComplete,
	}))
	require.NoError(t, store.SavePools(srcID, []helper.Pool{
		{PoolName: "Pool A", Players: []helper.Player{{Name: "Alice"}, {Name: "Bob"}}},
	}))
	playoff := &state.Competition{ID: "p", Name: "P", Format: state.CompFormatPlayoffs, SourceCompID: srcID}

	_, err := eng.resolvePoolWinners(playoff)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "must be a mixed")
}

// TestResolvePoolWinners_PoolCountFromPersistedPools pins the fix for the
// over-promotion bug: totalWinners must come from the ACTUAL finalized pool
// count, not a ceiling-division recomputation from participant count. Here 5
// participants are split into 2 pools on disk; with PoolWinners=1 the result
// must be exactly 2 winners (one per real pool), regardless of PoolSize/Mode.
func TestResolvePoolWinners_PoolCountFromPersistedPools(t *testing.T) {
	eng, store, _ := setupTestEngine(t)

	srcID := "src-2pools"
	require.NoError(t, store.SaveCompetition(&state.Competition{
		ID: srcID, Name: "Two Pools", Format: state.CompFormatMixed,
		Status:      state.CompStatusComplete,
		PoolSize:    5, // ceiling math on 5 parts would give numPools=1 → wrong
		PoolWinners: 1,
	}))
	require.NoError(t, store.SaveParticipants(srcID, []domain.Player{
		{Name: "A"}, {Name: "B"}, {Name: "C"}, {Name: "D"}, {Name: "E"},
	}))
	require.NoError(t, store.SavePools(srcID, []helper.Pool{
		{PoolName: "Pool A", Players: []helper.Player{{Name: "A"}, {Name: "B"}, {Name: "C"}}},
		{PoolName: "Pool B", Players: []helper.Player{{Name: "D"}, {Name: "E"}}},
	}))
	require.NoError(t, store.SavePoolMatches(srcID, []state.MatchResult{
		{ID: "Pool A-0", SideA: "A", SideB: "B", Winner: "A", IpponsA: []string{"M"}, Status: state.MatchStatusCompleted},
		{ID: "Pool A-1", SideA: "A", SideB: "C", Winner: "A", IpponsA: []string{"M"}, Status: state.MatchStatusCompleted},
		{ID: "Pool A-2", SideA: "B", SideB: "C", Winner: "B", IpponsA: []string{"M"}, Status: state.MatchStatusCompleted},
		{ID: "Pool B-0", SideA: "D", SideB: "E", Winner: "D", IpponsA: []string{"M"}, Status: state.MatchStatusCompleted},
	}))

	playoff := &state.Competition{ID: "p2", Name: "P2", Format: state.CompFormatPlayoffs, SourceCompID: srcID}
	roster, err := eng.resolvePoolWinners(playoff)
	require.NoError(t, err)
	assert.Len(t, roster, 2, "2 real pools × 1 winner = 2 (NOT ceil(5/5)=1 pool)")
}

// TestStartCompetition_PlayoffsFromSource_ExistingRosterNotClobbered verifies
// the anti-clobber guard: a playoffs comp that has a SourceCompID link BUT an
// already-populated roster must keep that roster (no source resolution), so a
// manual roster or an accidental SourceCompID can't silently wipe participants.
func TestStartCompetition_PlayoffsFromSource_ExistingRosterNotClobbered(t *testing.T) {
	eng, store, _ := setupTestEngine(t)

	// Source is deliberately NOT final — if resolution ran it would error.
	srcID := "src-unfinished"
	require.NoError(t, store.SaveCompetition(&state.Competition{
		ID: srcID, Name: "Unfinished", Format: state.CompFormatMixed,
		Status: state.CompStatusPools,
	}))

	playoffID := "manual-roster-playoffs"
	require.NoError(t, store.SaveCompetition(&state.Competition{
		ID: playoffID, Name: "Manual - Playoffs",
		Format: state.CompFormatPlayoffs, SourceCompID: srcID,
	}))
	require.NoError(t, store.SaveParticipants(playoffID, []domain.Player{
		{Name: "Manual1", Dojo: "D1"}, {Name: "Manual2", Dojo: "D2"},
		{Name: "Manual3", Dojo: "D3"}, {Name: "Manual4", Dojo: "D4"},
	}))

	require.NoError(t, eng.StartCompetition(playoffID),
		"existing roster must be used directly — no source resolution, no error")

	roster, err := store.LoadParticipants(playoffID, false)
	require.NoError(t, err)
	require.Len(t, roster, 4)
	names := make([]string, len(roster))
	for i, p := range roster {
		names[i] = p.Name
	}
	assert.ElementsMatch(t, []string{"Manual1", "Manual2", "Manual3", "Manual4"}, names,
		"manual roster preserved, not replaced by source pool winners")

	bracket, err := store.LoadBracket(playoffID)
	require.NoError(t, err)
	require.NotNil(t, bracket)
	assert.NotEmpty(t, bracket.Rounds, "bracket built from the manual roster")
}

// TestStartCompetition_PlayoffsFromSource_SaveFailureRollsBackToSetup pins the
// rollback guard: if the trailing roster save fails AFTER the atomic Status
// commit, Status must revert to setup rather than getting stuck at draw-ready.
// Otherwise a retry would take StartCompetition's draw-ready fast path
// (transitionDrawToRunning) and start the playoffs with an empty
// participants.csv. The failure is injected via the saveResolvedPlayoffRoster
// seam so it triggers ONLY at the trailing save (the same participants.csv
// path is read empty at the start of the pipeline, so a filesystem fault would
// break the initial load instead).
func TestStartCompetition_PlayoffsFromSource_SaveFailureRollsBackToSetup(t *testing.T) {
	eng, store, _ := setupTestEngine(t)

	srcID := "src-rb"
	require.NoError(t, store.SaveCompetition(&state.Competition{
		ID: srcID, Name: "Src RB", Format: state.CompFormatMixed, Status: state.CompStatusComplete,
	}))
	require.NoError(t, store.SaveParticipants(srcID, []domain.Player{{Name: "Alice"}, {Name: "Bob"}}))
	require.NoError(t, store.SavePools(srcID, []helper.Pool{
		{PoolName: "Pool A", Players: []helper.Player{{Name: "Alice"}, {Name: "Bob"}}},
	}))
	require.NoError(t, store.SavePoolMatches(srcID, []state.MatchResult{
		{ID: "Pool A-0", SideA: "Alice", SideB: "Bob", Winner: "Alice",
			IpponsA: []string{"M"}, Status: state.MatchStatusCompleted},
	}))

	playoffID := "src-rb-playoffs"
	require.NoError(t, store.SaveCompetition(&state.Competition{
		ID: playoffID, Name: "Src RB - Playoffs",
		Format: state.CompFormatPlayoffs, SourceCompID: srcID,
	}))

	// Inject a deterministic failure at the trailing roster save.
	orig := saveResolvedPlayoffRoster
	saveResolvedPlayoffRoster = func(*state.Store, string, []domain.Player) error {
		return errors.New("injected disk failure")
	}
	defer func() { saveResolvedPlayoffRoster = orig }()

	err := eng.StartCompetition(playoffID)
	require.Error(t, err, "the injected roster-save failure must surface")
	assert.Contains(t, err.Error(), "injected disk failure")

	comp, lerr := store.LoadCompetition(playoffID)
	require.NoError(t, lerr)
	assert.Equal(t, state.CompStatusSetup, comp.Status,
		"Status must roll back to setup after a roster-save failure, not get stuck at draw-ready")
}

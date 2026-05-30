package engine

import (
	"testing"

	"github.com/gitrgoliveira/bracket-creator/internal/domain"
	"github.com/gitrgoliveira/bracket-creator/internal/helper"
	"github.com/gitrgoliveira/bracket-creator/internal/state"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// saveParticipantsWithCheckIn writes participants where names listed in
// checkedIn are flagged CheckedIn=true. Used by the mp-w7x exclusion tests.
func saveParticipantsWithCheckIn(t *testing.T, store *state.Store, compID string, names []string, checkedIn map[string]bool) {
	t.Helper()
	players := make([]domain.Player, len(names))
	for i, n := range names {
		players[i] = domain.Player{
			Name:      n,
			Dojo:      "Dojo" + string(rune('A'+i%5)),
			CheckedIn: checkedIn[n],
		}
	}
	require.NoError(t, store.SaveParticipants(compID, players))
}

// poolPlayerNames collects every player name across all generated pools.
func poolPlayerNames(pools []helper.Pool) map[string]bool {
	names := map[string]bool{}
	for _, p := range pools {
		for _, pl := range p.Players {
			names[pl.Name] = true
		}
	}
	return names
}

// bracketLeafNames collects the real (non-bye) competitor names seeded into
// the first round of a bracket. Byes are the empty string (bracket.go).
func bracketLeafNames(b *state.Bracket) map[string]bool {
	names := map[string]bool{}
	if b == nil || len(b.Rounds) == 0 {
		return names
	}
	for _, m := range b.Rounds[0] {
		if m.SideA != "" {
			names[m.SideA] = true
		}
		if m.SideB != "" {
			names[m.SideB] = true
		}
	}
	return names
}

// TestFilterCheckedIn pins the opt-in semantics of the mp-w7x helper directly.
func TestFilterCheckedIn(t *testing.T) {
	mk := func(name string, in bool) domain.Player {
		return domain.Player{Name: name, CheckedIn: in}
	}

	t.Run("nobody checked in returns the roster unchanged (opt-in)", func(t *testing.T) {
		in := []domain.Player{mk("A", false), mk("B", false), mk("C", false)}
		got := filterCheckedIn(in)
		require.Len(t, got, 3)
	})

	t.Run("mixed returns only the checked-in players", func(t *testing.T) {
		in := []domain.Player{mk("A", true), mk("B", false), mk("C", true)}
		got := filterCheckedIn(in)
		require.Len(t, got, 2)
		assert.Equal(t, "A", got[0].Name)
		assert.Equal(t, "C", got[1].Name)
	})

	t.Run("all checked in returns everyone", func(t *testing.T) {
		in := []domain.Player{mk("A", true), mk("B", true)}
		got := filterCheckedIn(in)
		require.Len(t, got, 2)
	})

	t.Run("empty roster is a no-op", func(t *testing.T) {
		got := filterCheckedIn(nil)
		assert.Empty(t, got)
	})
}

// TestStartCompetition_MixedFormat_ExcludesNonCheckedIn verifies that when
// check-in is in use, only checked-in participants reach the pool draw.
func TestStartCompetition_MixedFormat_ExcludesNonCheckedIn(t *testing.T) {
	eng, store, _ := setupTestEngine(t)
	compID := "checkin-pools"

	createTestCompetition(t, store, compID, "mixed", 3)
	saveParticipantsWithCheckIn(t, store, compID,
		[]string{"Alice", "Bob", "Charlie", "Dave", "Eve", "Frank"},
		map[string]bool{"Alice": true, "Bob": true, "Charlie": true, "Dave": true},
	)

	require.NoError(t, eng.StartCompetition(compID))

	pools, err := store.LoadPools(compID)
	require.NoError(t, err)
	names := poolPlayerNames(pools)

	assert.Len(t, names, 4, "only the 4 checked-in players should be drawn")
	for _, want := range []string{"Alice", "Bob", "Charlie", "Dave"} {
		assert.Contains(t, names, want)
	}
	for _, dropped := range []string{"Eve", "Frank"} {
		assert.NotContains(t, names, dropped, "non-checked-in player must be excluded")
	}
}

// TestStartCompetition_NoneCheckedIn_IncludesAll pins the opt-in fallback:
// a competition that never used check-in draws everyone.
func TestStartCompetition_NoneCheckedIn_IncludesAll(t *testing.T) {
	eng, store, _ := setupTestEngine(t)
	compID := "no-checkin-pools"

	createTestCompetition(t, store, compID, "mixed", 3)
	saveParticipantsWithCheckIn(t, store, compID,
		[]string{"Alice", "Bob", "Charlie", "Dave", "Eve", "Frank"},
		map[string]bool{}, // nobody checked in
	)

	require.NoError(t, eng.StartCompetition(compID))

	pools, err := store.LoadPools(compID)
	require.NoError(t, err)
	assert.Len(t, poolPlayerNames(pools), 6, "with no check-in, all participants are drawn")
}

// TestStartCompetition_PlayoffsFormat_ExcludesNonCheckedIn verifies the filter
// reaches the elimination-bracket path too.
func TestStartCompetition_PlayoffsFormat_ExcludesNonCheckedIn(t *testing.T) {
	eng, store, _ := setupTestEngine(t)
	compID := "checkin-playoffs"

	createTestCompetition(t, store, compID, "playoffs", 0)
	saveParticipantsWithCheckIn(t, store, compID,
		[]string{"Alice", "Bob", "Charlie", "Dave", "Eve"},
		map[string]bool{"Alice": true, "Bob": true, "Charlie": true, "Dave": true},
	)

	require.NoError(t, eng.StartCompetition(compID))

	bracket, err := store.LoadBracket(compID)
	require.NoError(t, err)
	names := bracketLeafNames(bracket)

	assert.Len(t, names, 4, "only the 4 checked-in players should seed the bracket")
	assert.NotContains(t, names, "Eve", "non-checked-in player must be excluded")
}

// TestStartCompetition_PlayoffsFromSource_FinalistsNotExcluded is the critical
// regression guard for mp-w7x GAP 1: a playoffs competition whose roster is
// resolved from a source's pool winners must NOT have those finalists dropped.
// resolvePoolWinners builds them with CheckedIn=false, so a filter placed after
// resolution would empty the bracket. The filter must run on the (empty) disk
// roster BEFORE resolution, leaving the finalists untouched.
func TestStartCompetition_PlayoffsFromSource_FinalistsNotExcluded(t *testing.T) {
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

	playoffID := "src-mixed-playoffs"
	require.NoError(t, store.SaveCompetition(&state.Competition{
		ID:           playoffID,
		Name:         "Source Mixed - Playoffs",
		Kind:         "individual",
		Format:       state.CompFormatPlayoffs,
		SourceCompID: srcID,
		Courts:       []string{"A"},
		StartTime:    "09:00",
		Status:       "setup",
	}))

	require.NoError(t, eng.StartCompetition(playoffID))

	bracket, err := store.LoadBracket(playoffID)
	require.NoError(t, err)
	names := bracketLeafNames(bracket)

	assert.Len(t, names, 2, "both resolved finalists must seed the bracket despite CheckedIn=false")
	assert.Contains(t, names, "Alice")
	assert.Contains(t, names, "Bob")
}

// TestGenerateSwissRound_ExcludesNonCheckedIn is the regression guard for
// mp-w7x GAP 2: the Swiss path reloads the roster from disk, so the filter
// must live inside GenerateSwissRound, not only in runDrawPipeline.
func TestGenerateSwissRound_ExcludesNonCheckedIn(t *testing.T) {
	eng, store, _ := setupTestEngine(t)
	compID := "checkin-swiss"

	require.NoError(t, store.SaveCompetition(&state.Competition{
		ID:          compID,
		Name:        "Swiss Check-in",
		Kind:        "individual",
		Format:      state.CompFormatSwiss,
		SwissRounds: 3,
		Courts:      []string{"A"},
		StartTime:   "09:00",
		Status:      "setup",
	}))
	saveParticipantsWithCheckIn(t, store, compID,
		[]string{"Alice", "Bob", "Charlie", "Dave", "Eve", "Frank"},
		map[string]bool{"Alice": true, "Bob": true, "Charlie": true, "Dave": true},
	)

	matches, err := eng.GenerateSwissRound(compID, 1)
	require.NoError(t, err)

	seen := map[string]bool{}
	for _, m := range matches {
		if m.SideA != "" {
			seen[m.SideA] = true
		}
		if m.SideB != "" {
			seen[m.SideB] = true
		}
	}
	assert.NotContains(t, seen, "Eve", "non-checked-in player must not be paired")
	assert.NotContains(t, seen, "Frank", "non-checked-in player must not be paired")
	assert.Contains(t, seen, "Alice")
}

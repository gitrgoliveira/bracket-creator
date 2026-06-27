package engine

import (
	"testing"

	"github.com/gitrgoliveira/bracket-creator/internal/state"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// emptyComp saves a roster-less competition so its clash footprint resolves to
// the MinClashFootprintMinutes floor (estimate is ~0 with no participants).
func emptyComp(t *testing.T, store *state.Store, id, name, date, start string, courts []string) {
	t.Helper()
	require.NoError(t, store.SaveCompetition(&state.Competition{
		ID:        id,
		Name:      name,
		Format:    state.CompFormatPlayoffs,
		Kind:      "individual",
		Date:      date,
		StartTime: start,
		Courts:    courts,
		Status:    state.CompStatusSetup,
	}))
}

func TestDetectClashes_UnknownComp(t *testing.T) {
	eng, _, _ := setupTestEngine(t)
	_, err := eng.DetectClashesForCompetition("nope")
	require.Error(t, err)
	var nfe *NotFoundError
	assert.ErrorAs(t, err, &nfe)
}

func TestDetectClashes_OverlapSameCourt(t *testing.T) {
	eng, store, _ := setupTestEngine(t)
	// Empty comps → 30-min footprint each. A 09:00 and 09:15 both on court A
	// overlap (09:00–09:30 vs 09:15–09:45).
	emptyComp(t, store, "a", "Alpha", "01-07-2026", "09:00", []string{"A"})
	emptyComp(t, store, "b", "Bravo", "01-07-2026", "09:15", []string{"A"})

	clashes, err := eng.DetectClashesForCompetition("a")
	require.NoError(t, err)
	require.Len(t, clashes, 1)
	assert.Equal(t, "b", clashes[0].OtherCompID)
	assert.Equal(t, "Bravo", clashes[0].OtherCompName)
	assert.Equal(t, []string{"A"}, clashes[0].SharedCourts)
	// Overlap window = [max(09:00,09:15), min(09:30,09:45)] = 09:15–09:30.
	assert.Equal(t, "09:15", clashes[0].OverlapStart)
	assert.Equal(t, "09:30", clashes[0].OverlapEnd)
}

func TestDetectClashes_NoOverlapWhenSequential(t *testing.T) {
	eng, store, _ := setupTestEngine(t)
	// 30-min footprints back-to-back: 09:00–09:30 and 09:30–10:00 do NOT
	// overlap (half-open windows touch but don't intersect).
	emptyComp(t, store, "a", "Alpha", "01-07-2026", "09:00", []string{"A"})
	emptyComp(t, store, "b", "Bravo", "01-07-2026", "09:30", []string{"A"})

	clashes, err := eng.DetectClashesForCompetition("a")
	require.NoError(t, err)
	assert.Empty(t, clashes)
}

func TestDetectClashes_DifferentCourtsNoClash(t *testing.T) {
	eng, store, _ := setupTestEngine(t)
	emptyComp(t, store, "a", "Alpha", "01-07-2026", "09:00", []string{"A"})
	emptyComp(t, store, "b", "Bravo", "01-07-2026", "09:00", []string{"B"})

	clashes, err := eng.DetectClashesForCompetition("a")
	require.NoError(t, err)
	assert.Empty(t, clashes, "same time + same day but disjoint courts must not clash")
}

func TestDetectClashes_DifferentDaysNoClash(t *testing.T) {
	eng, store, _ := setupTestEngine(t)
	emptyComp(t, store, "a", "Alpha", "01-07-2026", "09:00", []string{"A"})
	emptyComp(t, store, "b", "Bravo", "02-07-2026", "09:00", []string{"A"})

	clashes, err := eng.DetectClashesForCompetition("a")
	require.NoError(t, err)
	assert.Empty(t, clashes, "same court + same time but different days must not clash")
}

func TestDetectClashes_PartialCourtOverlapReportsShared(t *testing.T) {
	eng, store, _ := setupTestEngine(t)
	emptyComp(t, store, "a", "Alpha", "01-07-2026", "09:00", []string{"A", "B"})
	emptyComp(t, store, "b", "Bravo", "01-07-2026", "09:10", []string{"B", "C"})

	clashes, err := eng.DetectClashesForCompetition("a")
	require.NoError(t, err)
	require.Len(t, clashes, 1)
	assert.Equal(t, []string{"B"}, clashes[0].SharedCourts, "only the shared court is reported")
}

func TestDetectClashes_UnplaceableTargetReturnsEmpty(t *testing.T) {
	eng, store, _ := setupTestEngine(t)
	// Target has no date → cannot be placed → no clashes even though a same-time
	// competition exists on the same court.
	emptyComp(t, store, "a", "Alpha", "", "09:00", []string{"A"})
	emptyComp(t, store, "b", "Bravo", "01-07-2026", "09:00", []string{"A"})

	clashes, err := eng.DetectClashesForCompetition("a")
	require.NoError(t, err)
	assert.Empty(t, clashes)
}

func TestDetectClashes_NilCourtsResolveToDefault(t *testing.T) {
	eng, store, _ := setupTestEngine(t)
	// Two competitions with NO explicit courts (nil) — legacy data saved before
	// court materialization. Both must resolve to the default ["A"] (no tournament
	// saved) and therefore clash at the same date/time, rather than being silently
	// skipped because their court lists are empty.
	emptyComp(t, store, "a", "Alpha", "01-07-2026", "09:00", nil)
	emptyComp(t, store, "b", "Bravo", "01-07-2026", "09:00", nil)

	clashes, err := eng.DetectClashesForCompetition("a")
	require.NoError(t, err)
	require.Len(t, clashes, 1, "nil-courts competitions must resolve to a default court and clash")
	assert.Equal(t, []string{"A"}, clashes[0].SharedCourts)
}

func TestDetectClashes_NilCourtsInheritTournamentCourts(t *testing.T) {
	eng, store, _ := setupTestEngine(t)
	// With a tournament present, nil competition courts inherit the tournament's
	// courts (mirrors resolveCompetitionCourts at draw time).
	require.NoError(t, store.SaveTournament(&state.Tournament{Courts: []string{"X", "Y"}}))
	emptyComp(t, store, "a", "Alpha", "01-07-2026", "09:00", nil)
	emptyComp(t, store, "b", "Bravo", "01-07-2026", "09:00", nil)

	clashes, err := eng.DetectClashesForCompetition("a")
	require.NoError(t, err)
	require.Len(t, clashes, 1)
	assert.Equal(t, []string{"X", "Y"}, clashes[0].SharedCourts,
		"nil-courts competitions must inherit the tournament's courts")
}

func TestFmtHHMM_NoMidnightWrap(t *testing.T) {
	// Times past midnight must render as 24:xx / 25:xx, NOT wrap to 00:xx, so an
	// operator reading the clash warning sees the next-day rollover.
	assert.Equal(t, "24:15", fmtHHMM(24*60+15))
	assert.Equal(t, "25:00", fmtHHMM(25*60))
	assert.Equal(t, "00:00", fmtHHMM(0))
	assert.Equal(t, "00:00", fmtHHMM(-5), "negative clamps to 00:00")
	assert.Equal(t, "23:59", fmtHHMM(23*60+59))
}

func TestDetectClashes_PastMidnightOverlapEndNoWrap(t *testing.T) {
	eng, store, _ := setupTestEngine(t)
	// Both start late: 23:50 + 30-min floor = 24:20. The overlap end must render
	// "24:10" (min(24:20, 24:15)), not a misleading "00:10".
	emptyComp(t, store, "a", "Alpha", "01-07-2026", "23:50", []string{"A"})
	emptyComp(t, store, "b", "Bravo", "01-07-2026", "23:45", []string{"A"})

	clashes, err := eng.DetectClashesForCompetition("a")
	require.NoError(t, err)
	require.Len(t, clashes, 1)
	// Windows: Alpha 23:50–24:20, Bravo 23:45–24:15. Overlap = 23:50–24:15.
	assert.Equal(t, "23:50", clashes[0].OverlapStart)
	assert.Equal(t, "24:15", clashes[0].OverlapEnd)
}

func TestDetectClashes_RosteredFootprintExtendsWindow(t *testing.T) {
	eng, store, _ := setupTestEngine(t)
	// Alpha gets a roster so its real estimate exceeds the 30-min floor and its
	// window stretches to overlap a competition that starts 40 min later.
	require.NoError(t, store.SaveCompetition(&state.Competition{
		ID: "a", Name: "Alpha", Format: state.CompFormatPlayoffs, Kind: "individual",
		Date: "01-07-2026", StartTime: "09:00", Courts: []string{"A"},
		PoolMatchDuration: 3, PlayoffMatchDuration: 5, Status: state.CompStatusSetup,
	}))
	saveTestParticipants(t, store, "a", []string{"P1", "P2", "P3", "P4", "P5", "P6", "P7", "P8"})
	est, err := eng.EstimateScheduleForCompetition("a")
	require.NoError(t, err)
	require.Greater(t, est.TotalDurationMinutes, 40, "test assumes Alpha's estimate exceeds 40m")

	emptyComp(t, store, "b", "Bravo", "01-07-2026", "09:40", []string{"A"})

	clashes, err := eng.DetectClashesForCompetition("a")
	require.NoError(t, err)
	require.Len(t, clashes, 1, "Alpha's rostered window must reach Bravo at 09:40")
	assert.Equal(t, "b", clashes[0].OtherCompID)
}

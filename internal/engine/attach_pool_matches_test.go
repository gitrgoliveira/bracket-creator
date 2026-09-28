package engine

import (
	"bytes"
	"fmt"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	excelize "github.com/xuri/excelize/v2"

	"github.com/gitrgoliveira/bracket-creator/internal/helper"
	"github.com/gitrgoliveira/bracket-creator/internal/state"
)

// TestAttachPoolMatches_SkipsUnresolvableSide is the regression test for the nil
// dereference: a pool match whose side resolves to no pool member (e.g. a
// participant removed after the match was recorded) must be SKIPPED, not stored as
// a nil *Player that PrintPoolMatches would dereference and panic on.
func TestAttachPoolMatches_SkipsUnresolvableSide(t *testing.T) {
	t.Parallel()
	pools := []helper.Pool{{
		PoolName: "Pool A",
		Players: []helper.Player{
			{ID: "id-1", Name: "Ann", Dojo: "North"},
			{ID: "id-2", Name: "Bea", Dojo: "South"},
		},
	}}
	results := []state.MatchResult{
		{ID: "Pool A-0", SideA: "Ann", SideAID: "id-1", SideB: "Bea", SideBID: "id-2"},      // resolvable
		{ID: "Pool A-1", SideA: "Ann", SideAID: "id-1", SideB: "Ghost", SideBID: "id-gone"}, // SideB gone
	}

	ordinals := AttachPoolMatches(pools, results)

	require.Len(t, pools[0].Matches, 1, "the match with an unresolvable side must be skipped")
	require.NotNil(t, pools[0].Matches[0].SideA)
	require.NotNil(t, pools[0].Matches[0].SideB)
	assert.Equal(t, "Ann", pools[0].Matches[0].SideA.Name)
	assert.Equal(t, "Bea", pools[0].Matches[0].SideB.Name)
	// The kept match retains its ORIGINAL suffix (0), not a compacted index.
	assert.Equal(t, []int{0}, ordinals["Pool A"])
}

// TestAttachPoolMatches_MiddleSkipPreservesOrdinals is the regression test for the
// ordinal-shift desync: when a MIDDLE match is skipped (unresolvable side), the
// surviving matches must keep their original suffixes so later grid rows don't
// look up the wrong stored result.
func TestAttachPoolMatches_MiddleSkipPreservesOrdinals(t *testing.T) {
	t.Parallel()
	pools := []helper.Pool{{
		PoolName: "Pool A",
		Players: []helper.Player{
			{ID: "id-1", Name: "Ann", Dojo: "Dojo Ann"}, {ID: "id-2", Name: "Bea", Dojo: "Dojo Bea"}, {ID: "id-3", Name: "Cid", Dojo: "Dojo Cid"},
		},
	}}
	results := []state.MatchResult{
		{ID: "Pool A-0", SideA: "Ann", SideAID: "id-1", SideB: "Bea", SideBID: "id-2"},
		{ID: "Pool A-1", SideA: "Ann", SideAID: "id-1", SideB: "Ghost", SideBID: "id-gone"}, // MIDDLE skip
		{ID: "Pool A-2", SideA: "Bea", SideBID: "id-2", SideB: "Cid"},                       // resolvable by name/id
	}
	// Give Pool A-2 a resolvable SideAID too.
	results[2].SideAID = "id-2"
	results[2].SideBID = "id-3"

	ordinals := AttachPoolMatches(pools, results)

	require.Len(t, pools[0].Matches, 2)
	// Kept matches are 0 and 2 (1 was skipped); the ordinals slice preserves that,
	// so grid row 1 maps to suffix 2, NOT compacted index 1.
	assert.Equal(t, []int{0, 2}, ordinals["Pool A"])
}

// TestAttachPoolMatches_PrefersSideIDs is the regression test for the same-name
// participant bug in AttachPoolMatches: two competitors can share a name but sit
// in different dojos (allowed), so a name-only side lookup attaches the wrong
// Player. Each side is resolved by its SideAID/SideBID UUID, and by nothing
// else (TestAttachPoolMatches_IDlessSidesAreUnresolvable).
func TestAttachPoolMatches_PrefersSideIDs(t *testing.T) {
	t.Parallel()

	pools := []helper.Pool{{
		PoolName: "Pool A",
		Players: []helper.Player{
			{ID: "id-1", Name: "Sam", Dojo: "North"},
			{ID: "id-2", Name: "Sam", Dojo: "South"},
		},
	}}
	results := []state.MatchResult{
		{ID: "Pool A-0", SideA: "Sam", SideAID: "id-1", SideB: "Sam", SideBID: "id-2"},
	}

	AttachPoolMatches(pools, results)

	require.Len(t, pools[0].Matches, 1)
	m := pools[0].Matches[0]
	require.NotNil(t, m.SideA)
	require.NotNil(t, m.SideB)
	// Resolved by ID, so the two same-name sides map to DISTINCT players with the
	// correct dojos, not both to whichever "Sam" was added last.
	assert.Equal(t, "North", m.SideA.Dojo, "SideAID id-1 must resolve to the North Sam")
	assert.Equal(t, "South", m.SideB.Dojo, "SideBID id-2 must resolve to the South Sam")
	assert.NotSame(t, m.SideA, m.SideB, "same-name sides must resolve to distinct players")
}

// TestAttachPoolMatches_IDlessSidesAreUnresolvable is the converted twin of
// the deleted TestAttachPoolMatches_FallsBackToName, which pinned resolution
// falling back to the display name when legacy results carried no side
// UUIDs. The bc-pnum operator ruling ("a record that carries an id field is
// resolved by id only; an empty id resolves to NOTHING") removed that
// fallback: a result row with no SideAID/SideBID at all now resolves to no
// Player on either side and the match is skipped entirely, exactly like the
// single-sided case already pinned by TestAttachPoolMatches_SkipsUnresolvableSide.
func TestAttachPoolMatches_IDlessSidesAreUnresolvable(t *testing.T) {
	t.Parallel()

	pools := []helper.Pool{{
		PoolName: "Pool A",
		Players: []helper.Player{
			{ID: "id-1", Name: "Ann", Dojo: "North"},
			{ID: "id-2", Name: "Bea", Dojo: "South"},
		},
	}}
	results := []state.MatchResult{
		{ID: "Pool A-0", SideA: "Ann", SideB: "Bea"}, // no SideAID/SideBID
	}

	ordinals := AttachPoolMatches(pools, results)

	assert.Empty(t, pools[0].Matches, "an id-less row must resolve to no Player on either side and be skipped")
	assert.Empty(t, ordinals["Pool A"], "a skipped match contributes no ordinal")
}

// poolMatchRows counts the Pool Matches rows that are a match: both columns A
// and G name a competitor off the data sheet's name column (a pool header
// names the pool, and a results or ranking row carries a RANK or INDEX
// formula in G).
func poolMatchRows(t *testing.T, xlsx []byte) int {
	t.Helper()
	f, err := excelize.OpenReader(bytes.NewReader(xlsx))
	require.NoError(t, err)
	defer func() { require.NoError(t, f.Close()) }()
	rows, err := f.GetRows(helper.SheetPoolMatches)
	require.NoError(t, err)
	n := 0
	for r := range rows {
		a, err := f.GetCellFormula(helper.SheetPoolMatches, fmt.Sprintf("A%d", r+1))
		require.NoError(t, err)
		g, err := f.GetCellFormula(helper.SheetPoolMatches, fmt.Sprintf("G%d", r+1))
		require.NoError(t, err)
		if strings.Contains(a, "!$B$") && strings.Contains(g, "!$B$") {
			n++
		}
	}
	return n
}

// TestExportCompetitionXlsx_PoolMatchBlocksSurviveARestart pins that the
// stored-draw export builds its Pool Matches grid from the stored pool results,
// as the results export does: pools.csv records membership only, and a fresh Store
// and Engine over the same data dir (a server restart) used to print no match
// blocks at all.
func TestExportCompetitionXlsx_PoolMatchBlocksSurviveARestart(t *testing.T) {
	eng, store, dir := setupTestEngine(t)
	compID := "restart-pools"
	createTestCompetition(t, store, compID, state.CompFormatMixed, 3)
	saveTestParticipants(t, store, compID, []string{"Ann", "Bea", "Cid", "Dan", "Eve", "Fay"})
	require.NoError(t, eng.StartCompetition(compID))
	stored, err := store.LoadPoolMatches(compID)
	require.NoError(t, err)
	require.Len(t, stored, 6, "two pools of three, three matches each")

	before, err := eng.ExportCompetitionXlsx(compID)
	require.NoError(t, err)

	restartedStore, err := state.NewStore(dir)
	require.NoError(t, err)
	after, err := New(restartedStore).ExportCompetitionXlsx(compID)
	require.NoError(t, err)

	assert.Equal(t, len(stored), poolMatchRows(t, before), "the draw's own process prints every match")
	assert.Equal(t, len(stored), poolMatchRows(t, after), "a restarted process prints every match too")
}

package engine

import (
	"errors"
	"fmt"
	"testing"

	"github.com/gitrgoliveira/bracket-creator/internal/state"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// namedPlayers returns n test competitor names.
func namedPlayers(n int) []string {
	out := make([]string, n)
	for i := range out {
		out[i] = fmt.Sprintf("P%02d", i+1)
	}
	return out
}

// requireEveryMatchHasACourt is the assertion the whole inheritance rule
// exists for. The per-court operator view (/admin/shiaijo/:court) is built
// from the tournament's labels and filters matches on this field, so a match
// with an empty court is invisible for the entire event.
func requireEveryMatchHasACourt(t *testing.T, store *state.Store, compID string) []string {
	t.Helper()
	matches, err := store.LoadPoolMatches(compID)
	require.NoError(t, err)
	require.NotEmpty(t, matches, "the draw should have produced matches")
	labels := make([]string, 0, len(matches))
	for _, m := range matches {
		require.NotEmptyf(t, m.Court,
			"match %s was written with no shiaijo, so no operator view can ever show it", m.ID)
		labels = append(labels, m.Court)
	}
	return distinctCourts(labels)
}

// TestDrawInheritsTheVenueWhenTheCompetitionHasNoShiaijo pins the server-side
// behaviour for a competition stored with no courts key: the draw inherits the
// tournament's shiaijo, exactly as an HTTP write would have resolved it, and
// materialises the result so config.md and the generated matches agree.
//
// The defect: both gates in runDrawPipeline skipped an empty list and the
// generators read it as one UNNAMED court, so starting such a competition
// returned success and wrote matches with an empty Court column. Records with
// no courts key arrive from legacy data, imported manifests and hand-edited
// config files, so the HTTP-side resolution alone never covered them.
func TestDrawInheritsTheVenueWhenTheCompetitionHasNoShiaijo(t *testing.T) {
	eng, store, _ := setupTestEngine(t)
	require.NoError(t, store.SaveTournament(&state.Tournament{
		Name: "Venue Cup", Courts: []string{"A", "B", "C", "D"},
	}))
	const compID = "no-courts"
	createTestCompetition(t, store, compID, state.CompFormatMixed, 4, func(c *state.Competition) {
		c.Courts = nil
	})
	saveTestParticipants(t, store, compID, namedPlayers(16))

	require.NoError(t, eng.GenerateDraw(compID))

	assert.Equal(t, []string{"A", "B", "C", "D"}, requireEveryMatchHasACourt(t, store, compID))

	comp, err := store.LoadCompetition(compID)
	require.NoError(t, err)
	assert.Equal(t, []string{"A", "B", "C", "D"}, comp.Courts,
		"the inherited allocation is materialised, so the settings screen states what the draw used")
	assert.Equal(t, state.CompStatusDrawReady, comp.Status)
}

// A venue whose court count is not a legal allocation must not be inherited
// silently: picking which two of three shiaijo a competition runs on is the
// operator's call. The create path already rules that way (omitting courts
// reaches the same outcome as stating them), so the draw does too.
func TestDrawRefusesToInheritAnIllegalVenueCount(t *testing.T) {
	eng, store, _ := setupTestEngine(t)
	require.NoError(t, store.SaveTournament(&state.Tournament{
		Name: "Three Court Cup", Courts: []string{"A", "B", "C"},
	}))
	const compID = "inherit-three"
	createTestCompetition(t, store, compID, state.CompFormatMixed, 4, func(c *state.Competition) {
		c.Courts = nil
	})
	saveTestParticipants(t, store, compID, namedPlayers(16))

	err := eng.GenerateDraw(compID)

	require.Error(t, err)
	assert.Contains(t, err.Error(), "shiaijo count must be a power of two")
	assert.Contains(t, err.Error(), "It has no shiaijo of its own, so the draw would run on all 3 of the tournament's",
		"the operator must be told where the count they never chose came from")
	var vErr *ValidationError
	assert.True(t, errors.As(err, &vErr),
		"an unusable allocation is an operator input error (HTTP 400), not a 500")

	comp, err := store.LoadCompetition(compID)
	require.NoError(t, err)
	assert.Equal(t, state.CompStatusSetup, comp.Status, "a refused draw must not commit")
	assert.Empty(t, comp.Courts, "a refused draw must not materialise anything")
}

// The count rule is scoped to bracket-drawing formats, so a league inherits
// any venue at all and simply runs on every shiaijo it has.
func TestLeagueInheritsAnyVenueCount(t *testing.T) {
	eng, store, _ := setupTestEngine(t)
	require.NoError(t, store.SaveTournament(&state.Tournament{
		Name: "Three Court Cup", Courts: []string{"A", "B", "C"},
	}))
	const compID = "league-inherit"
	createTestCompetition(t, store, compID, state.CompFormatLeague, 12, func(c *state.Competition) {
		c.Courts = nil
	})
	saveTestParticipants(t, store, compID, namedPlayers(12))

	require.NoError(t, eng.GenerateDraw(compID))

	assert.Equal(t, []string{"A", "B", "C"}, requireEveryMatchHasACourt(t, store, compID))
	comp, err := store.LoadCompetition(compID)
	require.NoError(t, err)
	assert.Equal(t, []string{"A", "B", "C"}, comp.Courts)
}

// The bootstrap edge: no tournament record at all. There is still no such
// thing as a match on no shiaijo, so the draw falls back to a single named
// court rather than writing blanks.
func TestDrawNamesASingleShiaijoWithNoTournament(t *testing.T) {
	eng, store, _ := setupTestEngine(t)
	const compID = "no-tournament"
	createTestCompetition(t, store, compID, state.CompFormatMixed, 4, func(c *state.Competition) {
		c.Courts = nil
	})
	saveTestParticipants(t, store, compID, namedPlayers(16))

	require.NoError(t, eng.GenerateDraw(compID))

	assert.Equal(t, []string{"A"}, requireEveryMatchHasACourt(t, store, compID))
}

// InheritedDrawCourts is the pure half; table-drive it so the resolution is
// pinned independently of the pipeline wiring.
func TestInheritedDrawCourts(t *testing.T) {
	tourn := &state.Tournament{Name: "T", Courts: []string{"A", "B", "C"}}
	tests := []struct {
		desc  string
		comp  []string
		tourn *state.Tournament
		want  []string
	}{
		{"an explicit allocation is returned untouched", []string{"B", "D"}, tourn, []string{"B", "D"}},
		{"an explicit allocation is never trimmed to a legal count", []string{"A", "B", "C"}, tourn, []string{"A", "B", "C"}},
		{"an empty list inherits the venue whole", nil, tourn, []string{"A", "B", "C"}},
		{"no tournament falls back to one named shiaijo", nil, nil, []string{"A"}},
		{"a tournament with no courts falls back too", nil, &state.Tournament{Name: "T"}, []string{"A"}},
	}
	for _, tc := range tests {
		t.Run(tc.desc, func(t *testing.T) {
			assert.Equal(t, tc.want, InheritedDrawCourts(tc.comp, tc.tourn))
		})
	}
}

// The inherited list must be a COPY: the pipeline stores it on the competition
// and the transform persists it, so aliasing the tournament's slice would let
// a competition write reach back into the venue record.
func TestInheritedDrawCourtsDoesNotAliasTheTournament(t *testing.T) {
	tourn := &state.Tournament{Name: "T", Courts: []string{"A", "B"}}
	got := InheritedDrawCourts(nil, tourn)
	got[0] = "Z"
	assert.Equal(t, []string{"A", "B"}, tourn.Courts)
}

package engine

import (
	"testing"

	"github.com/gitrgoliveira/bracket-creator/internal/domain"
	"github.com/gitrgoliveira/bracket-creator/internal/state"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// TestGenerateDraw_RefusesDuplicateTeamMemberRoster pins bc-tmdup's
// pre-flight half: a competition whose roster holds a team with the same
// member name twice must have its draw refused as a *engine.ValidationError
// (-> HTTP 400 at the generate-draw handler / StartCompetition's one-click
// path), naming the offending team and the repeated member.
//
// This is the OTHER half of the bc-tmdup pair. The participant-write floor
// (state.checkTeamMemberNameCollisions) grandfathers a pre-existing on-disk
// duplicate so a live event's check-ins keep working against data that
// predates the rule -- which by itself would let a duplicate roster start
// cleanly and only surface later, on the first check-in after the
// competition goes live. This pre-flight closes that: the roster reaching
// here is still fully editable (the competition has not started), so a
// refusal here is always actionable, and no NEW competition can start
// holding a duplicate in the first place.
//
// The roster is saved while the competition is still Kind=="individual" (so
// the write-time floor, which is unconditional for a TEAM competition, never
// runs), then the competition is flipped to a team format over that exact
// on-disk roster -- the same technique
// TestTeamNameUniqueness_GrandfathersStoredDuplicates (internal/state) uses,
// standing in here for a hand-authored CSV or archive import, since the app
// itself has no UI that authors a member list at all.
func TestGenerateDraw_RefusesDuplicateTeamMemberRoster(t *testing.T) {
	eng, store, _ := setupTestEngine(t)
	compID := "dup-team-member-roster"

	createTestCompetition(t, store, compID, state.CompFormatPlayoffs, 0, func(c *state.Competition) {
		c.Courts = []string{"A"}
	})
	require.NoError(t, store.SaveParticipants(compID, []domain.Player{
		{Name: "Tora A", Dojo: "Tora Dojo", Metadata: []string{"Alice", "Bob", "Alice"}},
		{Name: "Tora B", Dojo: "Tora Dojo", Metadata: []string{"Carol", "Dan", "Eve"}},
		{Name: "Tora C", Dojo: "Tora Dojo", Metadata: []string{"Frank", "Grace", "Heidi"}},
		{Name: "Tora D", Dojo: "Tora Dojo", Metadata: []string{"Ivan", "Judy", "Karl"}},
	}))
	// Flip to a team competition over that exact on-disk roster: config.md
	// changes, participants.csv does not.
	require.NoError(t, store.SaveCompetition(&state.Competition{
		ID:        compID,
		Name:      "Test Competition",
		Kind:      "team",
		TeamSize:  3,
		Format:    state.CompFormatPlayoffs,
		Courts:    []string{"A"},
		StartTime: "09:00",
		Status:    state.CompStatusSetup,
	}))

	err := eng.GenerateDraw(compID)
	require.Error(t, err)
	var ve *ValidationError
	require.ErrorAs(t, err, &ve, "must surface as a *engine.ValidationError (-> HTTP 400 at POST /competitions/:id/generate-draw)")
	assert.Contains(t, ve.Error(), "Tora A", "the error must name the offending team")
	assert.Contains(t, ve.Error(), "Alice", "the error must name the repeated member")

	comp, lerr := store.LoadCompetition(compID)
	require.NoError(t, lerr)
	assert.Equal(t, state.CompStatusSetup, comp.Status, "a rejected draw must not transition the competition")
	bracket, berr := store.LoadBracket(compID)
	require.NoError(t, berr)
	assert.Empty(t, bracket.Rounds, "nothing may be persisted for a refused draw")
}

// TestGenerateDraw_CleanTeamRosterUnaffected is the control: a team roster
// with no duplicate member name draws normally, so the new pre-flight does
// not misfire on ordinary team data.
func TestGenerateDraw_CleanTeamRosterUnaffected(t *testing.T) {
	eng, store, _ := setupTestEngine(t)
	compID := "clean-team-member-roster"

	createTestCompetition(t, store, compID, state.CompFormatPlayoffs, 0, func(c *state.Competition) {
		c.Kind = "team"
		c.TeamSize = 3
		c.Courts = []string{"A"}
	})
	require.NoError(t, store.SaveParticipants(compID, []domain.Player{
		{Name: "Tora A", Dojo: "Tora Dojo", Metadata: []string{"Alice", "Bob", "Carol"}},
		{Name: "Tora B", Dojo: "Tora Dojo", Metadata: []string{"Dan", "Eve", "Frank"}},
		{Name: "Tora C", Dojo: "Tora Dojo", Metadata: []string{"Grace", "Heidi", "Ivan"}},
		{Name: "Tora D", Dojo: "Tora Dojo", Metadata: []string{"Judy", "Karl", "Liam"}},
	}))

	require.NoError(t, eng.GenerateDraw(compID), "a clean team roster must not be affected by the new pre-flight")

	comp, lerr := store.LoadCompetition(compID)
	require.NoError(t, lerr)
	assert.Equal(t, state.CompStatusDrawReady, comp.Status)
}

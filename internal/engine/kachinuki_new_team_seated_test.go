package engine

import (
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/gitrgoliveira/bracket-creator/internal/domain"
	"github.com/gitrgoliveira/bracket-creator/internal/helper"
	"github.com/gitrgoliveira/bracket-creator/internal/state"
)

// bc-kno1, plan section 5: a team new in a roster save gets its members only
// on a LATER load (the save seeds over the roster then on disk, then clears the
// once-per-process upgrade stamp), and the transaction a start or a score write
// runs in does not run the upgrade itself. The chain that covers it in
// production is the draw, which reads the participants. So a roster save that
// adds a team, the draw, then the start and the first strike with no other read
// in between (the store's normal doors only, no hand-written member list) seat
// bout 1 with the new team's member 1, nameless, as every other team's.
func TestKachinukiOpener_ATeamNewInTheLastRosterSaveIsSeatedByNumber(t *testing.T) {
	eng, store, _ := setupTestEngine(t)
	const compID = "kno-new-team"
	createTestCompetition(t, store, compID, state.CompFormatLeague, 2, func(c *state.Competition) {
		c.Kind, c.TeamSize, c.TeamMatchType = "team", 3, state.TeamMatchTypeKachinuki
	})
	ryuID, toraID := helper.NewUUID4(), helper.NewUUID4()
	ryu := domain.Player{ID: ryuID, Name: "Ryu", Dojo: "DojoR"}
	require.NoError(t, store.SaveParticipants(compID, []domain.Player{ryu}))
	// Tora is new in this save, so the save itself seeds no member for it.
	require.NoError(t, store.SaveParticipants(compID, []domain.Player{ryu, {ID: toraID, Name: "Tora", Dojo: "DojoT"}}))
	require.NoError(t, eng.GenerateDraw(compID))

	// No read between the draw and the writes: the start, then the first strike,
	// which makes bout 1's row and is seated in the same batch.
	t0 := time.Now().UnixMilli() - 5000
	startKachinuki(t, eng, compID, "Pool A-0", t0)
	r := postIn(t, eng, compID, "Pool A-0", domain.ScoreEvent{ID: "s1", Session: "evs", Seq: 1, Stamp: t0 + 10, Kind: domain.ScoreIpponAdd, Pos: 1, Side: "a", Letter: "M"})
	require.Equal(t, domain.ScoreOutcomeApplied, r.Answers[0].Outcome)

	m := openerMatch(t, store, compID, "Pool A-0")
	byTeam := map[string]string{m.SideAID: "", m.SideBID: ""}
	require.Len(t, byTeam, 2, "the match holds both teams by id")
	require.Contains(t, byTeam, ryuID)
	require.Contains(t, byTeam, toraID)
	want := []string{teamMemberIDs(t, store, compID, m.SideAID, 1)[0], teamMemberIDs(t, store, compID, m.SideBID, 1)[0]}
	assert.Equal(t, want, boutOneMembers(t, store, compID), "bout 1 is each team's member 1, the team new in the last save included")
	require.NotEmpty(t, m.SubResults)
	assert.Equal(t, []string{"", ""}, []string{m.SubResults[0].SideA, m.SubResults[0].SideB}, "the seeded members carry no name")
}

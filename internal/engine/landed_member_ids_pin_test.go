package engine

import (
	"slices"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/gitrgoliveira/bracket-creator/internal/state"
)

// The self-run member judge (mobileapp.landedMembersRefusal) is only asked
// about a write whose payload INTRODUCES a member id (introducesJudgedMember),
// and that gate is equivalent to judging every write only while the ENGINE never
// lands a member id the judge would refuse that the payload did not introduce.
// These are PINS, green by design: they hold the invariant from this side, so a
// change inside the write that fills ids from somewhere new fails here rather
// than silently slipping past a gate that does not look.
//
// The only id fillers inside the write are preserveKachinukiMemberIDs (the
// stored row's id at the same position and name) and ResolveMemberWinnerID /
// ReconcileWinnerMemberID (the winner's id, derived from the row's own landed
// side ids, which the judge accepts). The lineup-sourced ids of
// MaybeAdvanceKachinuki are added AFTER the transaction, in the handler, and
// moving that step inside the write is exactly what these pins catch. The load
// repair (state.resolveSubMemberIDs) runs once per competition per process, not
// on the in-transaction read-back, and fills only a row's own team's members.
//
// The invariant, per landed row and judged field (either side's member id, and
// the winner's): the id is empty, or the payload's row at that position carried
// it, or the stored row at that position held it, or (the winner only) it is one
// of the landed row's own side ids.

// assertNoIntroducedMemberID fails for any landed member id at a judged field
// that neither the payload nor the stored match supplied.
func assertNoIntroducedMemberID(t *testing.T, stored, payload, landed []state.SubMatchResult) {
	t.Helper()
	supplied := func(got string, field func(state.SubMatchResult) string, row state.SubMatchResult) bool {
		return got == "" ||
			got == field(state.SubResultAt(payload, row.Position)) ||
			got == field(state.SubResultAt(stored, row.Position))
	}
	sideA := func(r state.SubMatchResult) string { return r.SideAMemberID }
	sideB := func(r state.SubMatchResult) string { return r.SideBMemberID }
	winner := func(r state.SubMatchResult) string { return r.WinnerMemberID }
	for _, row := range landed {
		assert.True(t, supplied(row.SideAMemberID, sideA, row),
			"position %d: side A member id %q was supplied by neither the payload nor the stored row", row.Position, row.SideAMemberID)
		assert.True(t, supplied(row.SideBMemberID, sideB, row),
			"position %d: side B member id %q was supplied by neither the payload nor the stored row", row.Position, row.SideBMemberID)
		ownFighter := row.WinnerMemberID == row.SideAMemberID || row.WinnerMemberID == row.SideBMemberID
		assert.True(t, supplied(row.WinnerMemberID, winner, row) || ownFighter,
			"position %d: winner member id %q was supplied by neither the payload, the stored row, nor the row's own landed fighters", row.Position, row.WinnerMemberID)
	}
}

func TestEngineLandsNoMemberIDThePayloadDidNotIntroduce(t *testing.T) {
	const (
		teamK = "team-k"
		teamO = "team-o"
	)
	kachinukiRows := func() []state.SubMatchResult {
		return []state.SubMatchResult{
			{Position: 1, SideA: "Sato", SideAMemberID: "k-sato", SideB: "Tanaka", SideBMemberID: "o-tanaka",
				IpponsA: []string{"M"}, Winner: "Sato", WinnerMemberID: "k-sato", Decision: "fought"},
			{Position: 2, SideA: "Sato", SideAMemberID: "k-sato", SideB: "Ito", SideBMemberID: "o-ito"},
		}
	}
	fixedRows := func() []state.SubMatchResult {
		return []state.SubMatchResult{
			{Position: 1, SideA: "K1", SideAMemberID: "k1", SideB: "O1", SideBMemberID: "o1",
				IpponsA: []string{"M"}, Winner: "K1", WinnerMemberID: "k1", Decision: "fought"},
			{Position: state.DaihyosenSubPosition, SideA: "Kyoto", SideAMemberID: "k-rep", SideB: "Osaka", SideBMemberID: "o-rep",
				Decision: "daihyosen"},
		}
	}
	// stripIDs is a payload that names its fighters and sends no member id.
	stripIDs := func(rows []state.SubMatchResult) []state.SubMatchResult {
		out := make([]state.SubMatchResult, len(rows))
		for i, r := range rows {
			r.SideAMemberID, r.SideBMemberID, r.WinnerMemberID = "", "", ""
			out[i] = r
		}
		return out
	}

	tests := []struct {
		name    string
		comp    state.Competition
		stored  func() []state.SubMatchResult
		payload func() []state.SubMatchResult
		// inherited are the (position, side A id, side B id) the landed rows must
		// still hold, so the pin is not vacuously true of a write that dropped
		// every id: the carry-over mechanisms it guards are really exercised.
		inherited map[int][2]string
	}{
		{
			name:   "kachinuki: a write echoing the stored member ids",
			comp:   state.Competition{ID: "pin-k-echo", Name: "Pin", Kind: "team", TeamSize: 3, TeamMatchType: state.TeamMatchTypeKachinuki},
			stored: kachinukiRows,
			payload: func() []state.SubMatchResult {
				rows := kachinukiRows()
				rows[1].IpponsB = []string{"K"}
				rows[1].Winner, rows[1].WinnerMemberID, rows[1].Decision = "Ito", "o-ito", "fought"
				return rows
			},
			inherited: map[int][2]string{1: {"k-sato", "o-tanaka"}, 2: {"k-sato", "o-ito"}},
		},
		{
			name:   "kachinuki: a write whose rows carry names and no member ids, over a position the stored match lacks",
			comp:   state.Competition{ID: "pin-k-names", Name: "Pin", Kind: "team", TeamSize: 3, TeamMatchType: state.TeamMatchTypeKachinuki},
			stored: kachinukiRows,
			payload: func() []state.SubMatchResult {
				rows := stripIDs(kachinukiRows())
				rows[1].IpponsB = []string{"K"}
				rows[1].Winner, rows[1].Decision = "Ito", "fought"
				return append(rows, state.SubMatchResult{Position: 3, SideA: "Ota", SideB: "Ito"})
			},
			inherited: map[int][2]string{1: {"k-sato", "o-tanaka"}, 2: {"k-sato", "o-ito"}, 3: {"", ""}},
		},
		{
			name:   "fixed order with a representative bout: a write echoing the stored member ids",
			comp:   state.Competition{ID: "pin-f-echo", Name: "Pin", Kind: "team", TeamSize: 3, TeamMatchType: state.TeamMatchTypeFixed},
			stored: fixedRows,
			payload: func() []state.SubMatchResult {
				rows := fixedRows()
				rows[0].IpponsA = []string{"M", "K"}
				rows[1].IpponsA, rows[1].Winner = []string{"M"}, "Kyoto"
				return rows
			},
			inherited: map[int][2]string{1: {"k1", "o1"}, state.DaihyosenSubPosition: {"k-rep", "o-rep"}},
		},
		{
			name:   "fixed order with a representative bout: a write whose rows carry names and no member ids",
			comp:   state.Competition{ID: "pin-f-names", Name: "Pin", Kind: "team", TeamSize: 3, TeamMatchType: state.TeamMatchTypeFixed},
			stored: fixedRows,
			payload: func() []state.SubMatchResult {
				rows := stripIDs(fixedRows())
				rows[0].IpponsA = []string{"M", "K"}
				rows[1].IpponsA, rows[1].Winner = []string{"M"}, "Kyoto"
				return append(rows, state.SubMatchResult{Position: 2, SideA: "K2", SideB: "O2"})
			},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			eng, store, _ := setupTestEngine(t)
			comp := tt.comp
			require.NoError(t, store.SaveCompetition(&comp))
			sideA, sideB := "Kyoto", "Osaka"
			require.NoError(t, store.SavePoolMatches(comp.ID, []state.MatchResult{{
				ID: "P1-1", SideA: sideA, SideAID: teamK, SideB: sideB, SideBID: teamO,
				Status: state.MatchStatusRunning, SubResults: tt.stored(),
			}}))

			payload := tt.payload()
			// The write reshapes the rows it is handed in place, so what the
			// payload carried is read from a copy made before it.
			sent := slices.Clone(payload)
			patch := &state.MatchResult{
				SideA: sideA, SideAID: teamK, SideB: sideB, SideBID: teamO,
				Status: state.MatchStatusRunning, SubResults: payload,
			}
			require.NoError(t, store.WithTransaction(comp.ID, func(tx state.StoreTx) error {
				_, terr := eng.RecordMatchResultWithIneligibilityTx(tx, comp.ID, "P1-1", patch)
				return terr
			}))

			landed := loadPoolMatchByID(t, store, comp.ID, "P1-1")
			require.NotNil(t, landed)
			require.NotEmpty(t, landed.SubResults)
			assertNoIntroducedMemberID(t, tt.stored(), sent, landed.SubResults)

			for position, ids := range tt.inherited {
				row := state.SubResultAt(landed.SubResults, position)
				assert.Equal(t, ids, [2]string{row.SideAMemberID, row.SideBMemberID},
					"position %d: the ids the write was meant to carry over are still there", position)
			}
		})
	}
}

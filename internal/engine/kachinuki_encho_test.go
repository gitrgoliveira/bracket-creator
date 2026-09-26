package engine

import (
	"testing"

	"github.com/gitrgoliveira/bracket-creator/internal/domain"
	"github.com/gitrgoliveira/bracket-creator/internal/state"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// bc-kten (operator ruling 2026-09-25): in a kachinuki encounter only the
// very last bout, taisho against taisho, may go to encho. The rule is
// domain.KachinukiTaishoPairing; these pin its server half,
// KachinukiEnchoRefusal: a NEW encho on a provably non-taisho pairing is
// refused, a stored one stays correctable, and an unknown roster is never
// refused on.

// kachinukiEnchoFixture: a 3-person kachinuki pool match, Red (aka, sideA)
// against White (shiro, sideB). Bout 1 was drawn, bout 2 (the second
// fighters) is being fought, and bout 3 would be taisho against taisho.
func kachinukiEnchoFixture(t *testing.T, compID string, withLineups bool, stored []state.SubMatchResult) *Engine {
	t.Helper()
	eng, store, _ := setupTestEngine(t)
	require.NoError(t, store.SaveCompetition(&state.Competition{
		ID: compID, TeamMatchType: state.TeamMatchTypeKachinuki, TeamSize: 3,
	}))
	if withLineups {
		for _, team := range []struct{ id, p string }{{"RedTeam", "R"}, {"WhiteTeam", "W"}} {
			require.NoError(t, store.SetTeamLineup(compID, domain.TeamLineup{
				TeamID: team.id, Round: 0,
				Positions: map[domain.Position]string{
					domain.PositionNumbered(1): team.p + "1",
					domain.PositionNumbered(2): team.p + "2",
					domain.PositionNumbered(3): team.p + "3",
				},
			}, 3))
		}
	}
	require.NoError(t, store.SavePoolMatches(compID, []state.MatchResult{{
		ID: "P1-0", SideA: "RedTeam", SideB: "WhiteTeam", Status: state.MatchStatusRunning,
		SubResults: stored,
	}}))
	return eng
}

func drawnFirstBout() state.SubMatchResult {
	return state.SubMatchResult{Position: 1, SideA: "R1", SideB: "W1", Decision: "hikiwake"}
}

func enchoBout(pos int, a, b string) state.SubMatchResult {
	return state.SubMatchResult{Position: pos, SideA: a, SideB: b, Encho: &state.EnchoMetadata{PeriodCount: 1}}
}

func TestKachinukiEnchoRefusal_MidEncounterPairingIsRefused(t *testing.T) {
	eng := kachinukiEnchoFixture(t, "kten-mid", true, []state.SubMatchResult{
		drawnFirstBout(), {Position: 2, SideA: "R2", SideB: "W2"},
	})
	err := eng.KachinukiEnchoRefusal("kten-mid", "P1-0", []state.SubMatchResult{drawnFirstBout(), enchoBout(2, "R2", "W2")})
	var verr *ValidationError
	require.ErrorAs(t, err, &verr, "encho on the second fighters is not taisho against taisho")
	assert.Contains(t, verr.Error(), "bout 2")
	assert.Contains(t, verr.Error(), "W2 against R2", "names both fighters, Shiro first as on court")
}

func TestKachinukiEnchoRefusal_TaishoAgainstTaishoIsAccepted(t *testing.T) {
	eng := kachinukiEnchoFixture(t, "kten-taisho", true, []state.SubMatchResult{
		drawnFirstBout(), {Position: 2, SideA: "R2", SideB: "W2", Decision: "hikiwake"}, {Position: 3, SideA: "R3", SideB: "W3"},
	})
	err := eng.KachinukiEnchoRefusal("kten-taisho", "P1-0", []state.SubMatchResult{enchoBout(3, "R3", "W3")})
	assert.NoError(t, err)
}

// The operator may pick a later bout's fighter out of lineup order. Here R3
// fought bout 2, so R2, placed before R3, is Red's last fighter and bout 3 is
// the last bout: refusing its encho would leave a tied knockout no way to
// finish. The write carries bout 3 alone, so who fought is read from the
// stored bouts.
func TestKachinukiEnchoRefusal_LastBoutAfterAnOutOfOrderPickIsAccepted(t *testing.T) {
	eng := kachinukiEnchoFixture(t, "kten-out-of-order", true, []state.SubMatchResult{
		drawnFirstBout(), {Position: 2, SideA: "R3", SideB: "W2", Decision: "hikiwake"}, {Position: 3, SideA: "R2", SideB: "W3"},
	})
	err := eng.KachinukiEnchoRefusal("kten-out-of-order", "P1-0", []state.SubMatchResult{enchoBout(3, "R2", "W3")})
	assert.NoError(t, err)
}

// The names can ride only on the stored row (the server appended the
// pairing); the check still judges the real fighters.
func TestKachinukiEnchoRefusal_FightersReadFromTheStoredRow(t *testing.T) {
	eng := kachinukiEnchoFixture(t, "kten-stored-names", true, []state.SubMatchResult{
		drawnFirstBout(), {Position: 2, SideA: "R2", SideB: "W2"},
	})
	err := eng.KachinukiEnchoRefusal("kten-stored-names", "P1-0", []state.SubMatchResult{enchoBout(2, "", "")})
	var verr *ValidationError
	require.ErrorAs(t, err, &verr)
}

// An encho already on record (before this rule) stays correctable: a later
// write carrying it again, e.g. End match, is not refused.
func TestKachinukiEnchoRefusal_StoredEnchoIsNotRejudged(t *testing.T) {
	eng := kachinukiEnchoFixture(t, "kten-legacy", true, []state.SubMatchResult{
		drawnFirstBout(), enchoBout(2, "R2", "W2"),
	})
	err := eng.KachinukiEnchoRefusal("kten-legacy", "P1-0", []state.SubMatchResult{drawnFirstBout(), enchoBout(2, "R2", "W2")})
	assert.NoError(t, err)
}

// With no lineup in force the app cannot tell who the taisho is, so it never
// refuses: a tied knockout bout has End match held back, and refusing encho
// on a guess would leave no way to finish.
func TestKachinukiEnchoRefusal_UnknownRosterIsNeverRefused(t *testing.T) {
	eng := kachinukiEnchoFixture(t, "kten-no-lineup", false, []state.SubMatchResult{
		drawnFirstBout(), {Position: 2, SideA: "R2", SideB: "W2"},
	})
	err := eng.KachinukiEnchoRefusal("kten-no-lineup", "P1-0", []state.SubMatchResult{enchoBout(2, "R2", "W2")})
	assert.NoError(t, err)
}

func TestKachinukiEnchoRefusal_NotKachinuki(t *testing.T) {
	eng, store, _ := setupTestEngine(t)
	require.NoError(t, store.SaveCompetition(&state.Competition{ID: "kten-fixed", TeamSize: 3}))
	err := eng.KachinukiEnchoRefusal("kten-fixed", "P1-0", []state.SubMatchResult{enchoBout(2, "R2", "W2")})
	assert.NoError(t, err)
}

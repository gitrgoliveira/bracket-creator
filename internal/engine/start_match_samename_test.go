package engine

import (
	"testing"
	"time"

	"github.com/gitrgoliveira/bracket-creator/internal/domain"
	"github.com/gitrgoliveira/bracket-creator/internal/helper"
	"github.com/gitrgoliveira/bracket-creator/internal/state"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// StartMatch's eligibility gate must read the two competitors from the match
// row's OWN side ids, not by scanning the roster for the side names: two
// competitors from different dojos may legally share a display name, and a name
// scan answers with whichever of them the roster lists first. Both directions of
// that misattribution are pinned here, because each is harmful on its own: the
// false block refuses an eligible competitor, and the missed block lets a
// withdrawn one fight.
//
// samSouth is deliberately FIRST in every roster below, so a name scan resolves
// "Sam" to the competitor who is NOT in the match under test.
func TestStartMatch_SameNameEligibilityUsesTheRowIDs(t *testing.T) {
	type fixture struct {
		eng      *Engine
		store    *state.Store
		compID   string
		samSouth string
		samNorth string
	}
	roster := func(t *testing.T, compID, format string) fixture {
		t.Helper()
		eng, store, _ := setupTestEngine(t)
		createTestCompetition(t, store, compID, format, 2)
		f := fixture{eng: eng, store: store, compID: compID, samSouth: helper.NewUUID4(), samNorth: helper.NewUUID4()}
		require.NoError(t, store.SaveParticipants(compID, []domain.Player{
			{ID: f.samSouth, Name: "Sam", Dojo: "South"},
			{ID: f.samNorth, Name: "Sam", Dojo: "North"},
			{ID: "b1e7b5f6-0000-4000-8000-00000000000a", Name: "Kenji", Dojo: "East"},
		}))
		return f
	}
	poolFixture := func(t *testing.T, compID string) fixture {
		t.Helper()
		f := roster(t, compID, "league")
		require.NoError(t, f.store.SavePoolMatches(compID, []state.MatchResult{{
			ID: "Pool A-0", SideA: "Sam", SideAID: f.samNorth,
			SideB: "Kenji", SideBID: "b1e7b5f6-0000-4000-8000-00000000000a",
			Status: state.MatchStatusScheduled,
		}}))
		return f
	}
	bracketFixture := func(t *testing.T, compID string) fixture {
		t.Helper()
		f := roster(t, compID, "playoffs")
		require.NoError(t, f.store.SaveBracket(compID, &state.Bracket{Rounds: [][]state.BracketMatch{{{
			ID: "m1", SideA: "Sam", SideAID: f.samNorth,
			SideB: "Kenji", SideBID: "b1e7b5f6-0000-4000-8000-00000000000a",
			Status: state.MatchStatusScheduled,
		}}}}))
		return f
	}
	withdraw := func(t *testing.T, f fixture, playerID string) {
		t.Helper()
		require.NoError(t, f.store.SetCompetitorStatus(f.compID, domain.CompetitorStatus{
			PlayerID: playerID, Eligible: false, Reason: "kiken", MatchID: "other-match",
			RecordedAt: time.Now().UTC(),
		}))
	}

	t.Run("pool row, the namesake withdrew", func(t *testing.T) {
		f := poolFixture(t, "samename-pool-eligible")
		withdraw(t, f, f.samSouth)
		assert.NoError(t, f.eng.StartMatch(f.compID, "Pool A-0"),
			"the Sam this row names is eligible; a namesake's withdrawal must not block the start")
	})

	t.Run("pool row, this row's competitor withdrew", func(t *testing.T) {
		f := poolFixture(t, "samename-pool-ineligible")
		withdraw(t, f, f.samNorth)
		err := f.eng.StartMatch(f.compID, "Pool A-0")
		require.Error(t, err, "the Sam this row names withdrew; the start must be refused")
		assert.ErrorIs(t, err, ErrIneligibleCompetitor)
	})

	t.Run("bracket row, the namesake withdrew", func(t *testing.T) {
		f := bracketFixture(t, "samename-bracket-eligible")
		withdraw(t, f, f.samSouth)
		assert.NoError(t, f.eng.StartMatch(f.compID, "m1"),
			"a bracket row carries its own side ids too (bc-brid); the namesake must not block it")
	})

	// The production start path is the TRANSACTIONAL door (handlers_match.go
	// calls StartMatchTx inside the court-exclusivity lock), so it is pinned
	// here too rather than left to the shared body: a twin that drifts is how
	// this class of defect survives a green suite.
	t.Run("transactional door, this row's competitor withdrew", func(t *testing.T) {
		f := poolFixture(t, "samename-pool-tx")
		withdraw(t, f, f.samNorth)
		err := f.store.WithTransaction(f.compID, func(tx state.StoreTx) error {
			return f.eng.StartMatchTx(tx, f.compID, "Pool A-0")
		})
		require.Error(t, err, "the Sam this row names withdrew; the transactional start must refuse too")
		assert.ErrorIs(t, err, ErrIneligibleCompetitor)
	})

	t.Run("bracket row, this row's competitor withdrew", func(t *testing.T) {
		f := bracketFixture(t, "samename-bracket-ineligible")
		withdraw(t, f, f.samNorth)
		err := f.eng.StartMatch(f.compID, "m1")
		require.Error(t, err, "the Sam this bracket row names withdrew; the start must be refused")
		assert.ErrorIs(t, err, ErrIneligibleCompetitor)
	})
}

package state

import (
	"errors"
	"testing"

	"github.com/gitrgoliveira/bracket-creator/internal/domain"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// Operator ruling: team names are unique regardless of dojo; individuals may
// share a name across dojos (disambiguated by uuid). The gate sits in
// saveParticipantsNoLock so every persistence path enforces it.
func TestTeamNameUniqueness(t *testing.T) {
	newStore := func(t *testing.T, kind string, teamSize int) (*Store, string) {
		t.Helper()
		s, err := NewStore(t.TempDir())
		require.NoError(t, err)
		comp := &Competition{ID: "c1", Name: "C1", Kind: kind, TeamSize: teamSize}
		require.NoError(t, s.SaveCompetition(comp))
		return s, comp.ID
	}

	t.Run("team competition rejects same-name teams even across dojos", func(t *testing.T) {
		s, id := newStore(t, "team", 3)
		err := s.SaveParticipants(id, []domain.Player{
			{Name: "Seibukan", Dojo: "Kyoto"},
			{Name: "Seibukan", Dojo: "Osaka"},
		})
		require.Error(t, err)
		assert.True(t, errors.Is(err, ErrDuplicateName))
		assert.Contains(t, err.Error(), "team names must be unique")
	})

	t.Run("teamSize alone (kind empty) also routes as a team", func(t *testing.T) {
		// Mirrors the JS canonical team check: kind=="team" OR teamSize>0.
		s, id := newStore(t, "", 5)
		err := s.SaveParticipants(id, []domain.Player{
			{Name: "Alpha", Dojo: "A"},
			{Name: "Alpha", Dojo: "B"},
		})
		require.Error(t, err)
		assert.True(t, errors.Is(err, ErrDuplicateName))
	})

	t.Run("individual competition still allows same name across dojos", func(t *testing.T) {
		s, id := newStore(t, "individual", 0)
		err := s.SaveParticipants(id, []domain.Player{
			{Name: "Tanaka", Dojo: "Kyoto"},
			{Name: "Tanaka", Dojo: "Osaka"},
		})
		require.NoError(t, err)
	})

	t.Run("distinct team names pass", func(t *testing.T) {
		s, id := newStore(t, "team", 3)
		err := s.SaveParticipants(id, []domain.Player{
			{Name: "Kyoto", Dojo: "K"},
			{Name: "Osaka", Dojo: "O"},
		})
		require.NoError(t, err)
	})
}

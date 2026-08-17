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

	t.Run("collision is detected through the shared normalization", func(t *testing.T) {
		// Byte-identical fixtures would pass under a plain == implementation and
		// so pin nothing about the normalizer the gate actually keys on.
		for _, tc := range []struct{ name, a, b string }{
			{"case and padding", " seibukan ", "Seibukan"},
			{"internal whitespace", "Sei  bukan", "Sei bukan"},
			{"diacritics", "Kōbe", "Kobe"},
		} {
			t.Run(tc.name, func(t *testing.T) {
				s, id := newStore(t, "team", 3)
				err := s.SaveParticipants(id, []domain.Player{
					{Name: tc.a, Dojo: "Kyoto"},
					{Name: tc.b, Dojo: "Osaka"},
				})
				require.Error(t, err)
				assert.True(t, errors.Is(err, ErrDuplicateName))
			})
		}
	})

	t.Run("the message does not contradict itself", func(t *testing.T) {
		// This gate fires precisely when the dojos DIFFER, so it must not
		// surface ErrDuplicateName's "same name and dojo" text: that reached the
		// operator verbatim in the 409 body and argued with itself.
		s, id := newStore(t, "team", 3)
		err := s.SaveParticipants(id, []domain.Player{
			{Name: "Seibukan", Dojo: "Kyoto"},
			{Name: "Seibukan", Dojo: "Osaka"},
		})
		require.Error(t, err)
		assert.NotContains(t, err.Error(), "same name and dojo")
		assert.Contains(t, err.Error(), "Seibukan")
	})
}

// A roster that already holds same-name teams predates the uniqueness rule and
// must stay writable: every rewrite funnels through saveParticipantsNoLock,
// including check-in, which is deliberately ungated so it keeps working after
// the competition starts. Rejecting the stored duplicate would turn the next
// check-in into an unmappable 500 with no in-app repair.
func TestTeamNameUniqueness_GrandfathersStoredDuplicates(t *testing.T) {
	s, err := NewStore(t.TempDir())
	require.NoError(t, err)
	id := "c1"
	// Written while the competition was individual, i.e. legal at the time.
	require.NoError(t, s.SaveCompetition(&Competition{ID: id, Name: "C1", Kind: "individual"}))
	require.NoError(t, s.SaveParticipants(id, []domain.Player{
		{Name: "Seibukan", Dojo: "Kyoto"},
		{Name: "Seibukan", Dojo: "Osaka"},
		{Name: "Kobukan", Dojo: "Nara"},
	}))
	require.NoError(t, s.SaveCompetition(&Competition{ID: id, Name: "C1", Kind: "team", TeamSize: 3, Status: "pools"}))

	stored, err := s.LoadParticipants(id, false)
	require.NoError(t, err)
	require.Len(t, stored, 3)

	t.Run("check-in still works", func(t *testing.T) {
		p, uerr := s.UpdateParticipant(id, stored[0].ID, false, func(p *domain.Player) error {
			p.CheckedIn = true
			return nil
		})
		require.NoError(t, uerr, "a live event must not be bricked by data entered legally")
		assert.True(t, p.CheckedIn)
	})

	t.Run("bulk check-in still works", func(t *testing.T) {
		res, berr := s.BulkCheckIn(id, []string{stored[1].ID, stored[2].ID})
		require.NoError(t, berr)
		assert.Equal(t, 2, res.CheckedIn)
	})

	t.Run("but a NEW collision is still refused", func(t *testing.T) {
		err := s.SaveParticipants(id, []domain.Player{
			{Name: "Seibukan", Dojo: "Kyoto"},
			{Name: "Seibukan", Dojo: "Osaka"},
			{Name: "Kobukan", Dojo: "Nara"},
			{Name: "Kobukan", Dojo: "Mie"}, // introduced by this write
		})
		require.Error(t, err)
		assert.True(t, errors.Is(err, ErrDuplicateName))
		assert.Contains(t, err.Error(), "Kobukan")
		assert.NotContains(t, err.Error(), "Seibukan", "the pre-existing pair is not re-reported")
	})
}

// Restoring an operator's own archive must never be blocked by a rule that
// postdates it. The restore writes into a freshly created competition, so there
// is nothing on disk to grandfather against and the exemption has to be
// explicit; fresh operator input still goes through SaveParticipants.
func TestSaveParticipantsRestored_ExemptFromTeamNameRule(t *testing.T) {
	s, err := NewStore(t.TempDir())
	require.NoError(t, err)
	id := "c1"
	require.NoError(t, s.SaveCompetition(&Competition{ID: id, Name: "C1", Kind: "team", TeamSize: 3}))
	roster := []domain.Player{
		{Name: "Seibukan", Dojo: "Kyoto"},
		{Name: "Seibukan", Dojo: "Osaka"},
	}

	require.Error(t, s.SaveParticipants(id, roster), "fresh operator input is still refused")
	require.NoError(t, s.SaveParticipantsRestored(id, roster), "an archive restore is exempt")

	got, err := s.LoadParticipants(id, false)
	require.NoError(t, err)
	assert.Len(t, got, 2)
}

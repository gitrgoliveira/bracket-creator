package state

import (
	"errors"
	"testing"

	"github.com/gitrgoliveira/bracket-creator/internal/domain"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// Operator ruling (bc-tmdup, 2026-09-09): two members of ONE team sharing a
// name must be impossible. A team's members live in that team's own
// Player.Metadata (the ordered trailing columns on its roster row); see
// ErrDuplicateTeamMember's doc comment in participants.go for how that is
// told apart from an individual competitor's dan-grade metadata. The gate
// sits in saveParticipantsNoLock, the one write floor every participant
// write path funnels through, so every caller enforces it -- including ones
// that never pass through an HTTP handler at all.
func newTeamMemberTestStore(t *testing.T, kind string, teamSize int, engi bool) (*Store, string) {
	t.Helper()
	s, err := NewStore(t.TempDir())
	require.NoError(t, err)
	comp := &Competition{ID: "c1", Name: "C1", Kind: kind, TeamSize: teamSize, Engi: engi}
	require.NoError(t, s.SaveCompetition(comp))
	return s, comp.ID
}

// Two members of ONE team sharing a name is refused; the message names the
// team and the repeated name so an operator can act on it.
func TestTeamMemberUniqueness_RejectsSameTeamCollision(t *testing.T) {
	s, id := newTeamMemberTestStore(t, "team", 3, false)
	err := s.SaveParticipants(id, []domain.Player{
		{Name: "Tora A", Dojo: "Tora Dojo", Metadata: []string{"Alice", "Bob", "Alice"}},
	})
	require.Error(t, err)
	assert.True(t, errors.Is(err, ErrDuplicateTeamMember))
	assert.Contains(t, err.Error(), "Tora A", "the message must name the offending team")
	assert.Contains(t, err.Error(), "Alice", "the message must name the repeated member")
}

// A write touching several teams, each with its OWN fresh internal
// duplicate, must report every offending team in one refusal rather than
// stopping at the first (matching checkNewTeamNameCollisions' own
// accumulate-all shape): a bulk import of many bad rows needs one
// fix-and-retry pass, not one per row.
func TestTeamMemberUniqueness_AccumulatesEveryOffendingTeam(t *testing.T) {
	s, id := newTeamMemberTestStore(t, "team", 3, false)
	err := s.SaveParticipants(id, []domain.Player{
		{Name: "Tora A", Dojo: "Tora Dojo", Metadata: []string{"Alice", "Bob", "Alice"}},
		{Name: "Tora B", Dojo: "Tora Dojo", Metadata: []string{"Carol", "Dan", "Eve"}},
		{Name: "Tora C", Dojo: "Tora Dojo", Metadata: []string{"Frank", "Grace", "Frank"}},
	})
	require.Error(t, err)
	assert.True(t, errors.Is(err, ErrDuplicateTeamMember))
	assert.Contains(t, err.Error(), "Tora A", "the first offending team must be named")
	assert.Contains(t, err.Error(), "Alice", "the first repeated member must be named")
	assert.Contains(t, err.Error(), "Tora C", "a SECOND offending team must also be named, not just the first")
	assert.Contains(t, err.Error(), "Frank", "the second repeated member must also be named")
}

// The same name on two DIFFERENT teams is accepted: the rule is scoped to a
// single team's own roster row, not the whole competition.
func TestTeamMemberUniqueness_AllowsSameNameAcrossDifferentTeams(t *testing.T) {
	s, id := newTeamMemberTestStore(t, "team", 3, false)
	err := s.SaveParticipants(id, []domain.Player{
		{Name: "Tora A", Dojo: "Tora Dojo", Metadata: []string{"Alice", "Bob"}},
		{Name: "Tora B", Dojo: "Tora Dojo", Metadata: []string{"Alice", "Carol"}},
	})
	require.NoError(t, err, "the same member name on two different teams must be allowed")
}

// teamSize alone (Kind empty) routes as a team, mirroring the sibling
// team-name rule's own discriminator test.
func TestTeamMemberUniqueness_TeamSizeAloneRoutesAsTeam(t *testing.T) {
	s, id := newTeamMemberTestStore(t, "", 5, false)
	err := s.SaveParticipants(id, []domain.Player{
		{Name: "Alpha", Dojo: "A", Metadata: []string{"Dan", "Dan"}},
	})
	require.Error(t, err)
	assert.True(t, errors.Is(err, ErrDuplicateTeamMember))
}

// An individual competition is unaffected by this rule, even for a player
// whose metadata carries a dan grade (the same array a team uses for its
// member list). The discriminator is the COMPETITION KIND, not whether the
// metadata array happens to contain a repeated value -- otherwise a
// coincidental repeat in unrelated trailing columns would wrongly refuse a
// legitimate individual roster.
func TestTeamMemberUniqueness_IndividualCompetitionUnaffected(t *testing.T) {
	s, id := newTeamMemberTestStore(t, "individual", 0, false)

	t.Run("ordinary dan grade metadata saves fine", func(t *testing.T) {
		err := s.SaveParticipants(id, []domain.Player{
			{Name: "Akira Tanaka", Dojo: "Gyokusen", Metadata: []string{"3"}},
			{Name: "Yuki Tanaka", Dojo: "Gyokusen", Metadata: []string{"3"}},
		})
		require.NoError(t, err)
	})

	t.Run("a coincidental repeat within one individual's own metadata is not a violation", func(t *testing.T) {
		// Contrived (production never writes two identical trailing columns
		// for an individual), but this is exactly the scenario a wrong
		// discriminator (e.g. "any player with a repeated metadata entry")
		// would misfire on. Kind=="individual" must skip the rule entirely.
		err := s.SaveParticipants(id, []domain.Player{
			{Name: "Akira Tanaka", Dojo: "Gyokusen", Metadata: []string{"3", "3"}},
		})
		require.NoError(t, err, "an individual's metadata is dan-grade data, never a team member list")
	})
}

// TestTeamMemberUniqueness_EngiFlagIsIrrelevantToTheKindGate actually
// exercises comp.Engi, unlike an earlier version of this test that set
// Engi:true on an INDIVIDUAL competition: checkTeamMemberNameCollisions'
// gate is `comp.Kind != "team" && comp.TeamSize == 0` (see its own doc
// comment) and never reads comp.Engi at all, so an individual-kind
// competition skips the rule regardless of Engi -- that shape re-proves
// TestTeamMemberUniqueness_IndividualCompetitionUnaffected and nothing about
// Engi specifically.
//
// Engi and a team competition are structurally mutually exclusive
// (handlers_competition.go refuses the combination outright), so an engi
// pair's combined "Name 1 - Name 2" lives in Player.Name and never reaches
// this function as a team's Metadata in real operation -- but that
// exclusion is enforced at the HTTP boundary, not inside this function
// itself. This test bypasses that boundary on purpose (constructing a
// Kind:"team", Engi:true competition directly at the state layer, which no
// real request can produce) to prove the RULE ITSELF is indifferent to
// comp.Engi: a stray or future-added Engi flag on an otherwise team-shaped
// competition must not silently defeat the member-uniqueness check.
func TestTeamMemberUniqueness_EngiFlagIsIrrelevantToTheKindGate(t *testing.T) {
	s, id := newTeamMemberTestStore(t, "team", 3, true)
	err := s.SaveParticipants(id, []domain.Player{
		{Name: "Tora A", Dojo: "Tora Dojo", Metadata: []string{"Alice", "Bob", "Alice"}},
	})
	require.Error(t, err, "a stray Engi flag on a team-shaped competition must not bypass the rule")
	assert.True(t, errors.Is(err, ErrDuplicateTeamMember))
}

// The floor lives in saveParticipantsNoLock itself, not in an HTTP handler:
// AddParticipant and UpdateParticipant (the exact Store methods the mobile
// app's HTTP handlers wrap) refuse the same collision with no gin.Context,
// no request, and no handler in the call stack at all.
func TestTeamMemberUniqueness_CaughtByWritesThatBypassTheHTTPBoundary(t *testing.T) {
	t.Run("AddParticipant", func(t *testing.T) {
		s, id := newTeamMemberTestStore(t, "team", 3, false)
		_, err := s.AddParticipant(id, domain.Player{Name: "Tora A", Dojo: "Tora Dojo", Metadata: []string{"Alice", "Bob", "Alice"}}, false)
		require.Error(t, err)
		assert.True(t, errors.Is(err, ErrDuplicateTeamMember))
	})

	t.Run("UpdateParticipant", func(t *testing.T) {
		s, id := newTeamMemberTestStore(t, "team", 3, false)
		require.NoError(t, s.SaveParticipants(id, []domain.Player{
			{Name: "Tora A", Dojo: "Tora Dojo", Metadata: []string{"Alice", "Bob"}},
		}))
		stored, err := s.LoadParticipants(id, false)
		require.NoError(t, err)
		require.Len(t, stored, 1)

		_, err = s.UpdateParticipant(id, stored[0].ID, false, func(p *domain.Player) error {
			p.Metadata = []string{"Alice", "Bob", "Alice"}
			return nil
		})
		require.Error(t, err)
		assert.True(t, errors.Is(err, ErrDuplicateTeamMember))
	})

	t.Run("SaveParticipantsRestored (the archive-import path) is NOT exempt", func(t *testing.T) {
		// Unlike the team-NAME rule, this one is never grandfathered: see
		// ErrDuplicateTeamMember's doc comment for why (mirrors ErrBlankDojo).
		s, id := newTeamMemberTestStore(t, "team", 3, false)
		err := s.SaveParticipantsRestored(id, []domain.Player{
			{Name: "Tora A", Dojo: "Tora Dojo", Metadata: []string{"Alice", "Bob", "Alice"}},
		})
		require.Error(t, err)
		assert.True(t, errors.Is(err, ErrDuplicateTeamMember))
	})
}

// Blank/whitespace member slots (an unfilled lineup position) are not two
// members named "": counting them as a collision would refuse an ordinary
// partially-filled roster.
func TestTeamMemberUniqueness_BlankSlotsAreNotACollision(t *testing.T) {
	s, id := newTeamMemberTestStore(t, "team", 5, false)
	err := s.SaveParticipants(id, []domain.Player{
		{Name: "Tora A", Dojo: "Tora Dojo", Metadata: []string{"Alice", "", "  ", "Bob"}},
	})
	require.NoError(t, err)
}

// TestTeamMemberUniqueness_GrandfathersStoredDuplicates pins the bc-tmdup
// blocking finding: this rule is NEW, and the app has no UI that authors a
// member list at all, so every member list on disk arrived by hand-authored
// CSV or archive import -- data that can predate this rule. Refusing a write
// on the strength of what the STORED roster already holds (rather than only
// what the write introduces) would brick the next check-in on a live event
// over data that was legal when it was entered, with no in-app repair (the
// roster paste box that IS enabled post-start discards every team's member
// list, since its parser reads a fixed column set).
//
// The roster is written while the competition is still Kind=="individual"
// (so the write-time floor -- unconditional for a TEAM competition -- never
// runs), then the competition is flipped to team over that exact roster,
// mirroring TestTeamNameUniqueness_GrandfathersStoredDuplicates' own
// technique for the sibling rule: the on-disk bytes stand in for a
// hand-authored CSV or archive import that predates the rule.
func TestTeamMemberUniqueness_GrandfathersStoredDuplicates(t *testing.T) {
	s, err := NewStore(t.TempDir())
	require.NoError(t, err)
	id := "c1"
	require.NoError(t, s.SaveCompetition(&Competition{ID: id, Name: "C1", Kind: "individual"}))
	require.NoError(t, s.SaveParticipants(id, []domain.Player{
		{Name: "Tora A", Dojo: "Tora Dojo", Metadata: []string{"Alice", "Bob", "Alice"}},
		{Name: "Tora B", Dojo: "Tora Dojo", Metadata: []string{"Carol", "Dan", "Eve"}},
		{Name: "Tora C", Dojo: "Tora Dojo", Metadata: []string{"Frank", "Grace", "Heidi"}},
	}))
	// Now a live, started team competition over that same on-disk roster.
	require.NoError(t, s.SaveCompetition(&Competition{ID: id, Name: "C1", Kind: "team", TeamSize: 3, Status: CompStatusPools}))

	stored, err := s.LoadParticipants(id, false)
	require.NoError(t, err)
	require.Len(t, stored, 3)

	t.Run("check-in on the offending team itself still works", func(t *testing.T) {
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
		// IDs carried forward from the earlier LoadParticipants, matching
		// how a real caller behaves: the grandfather comparison keys by
		// helper.PlayerKey (id when present), and the SPA's own paste-box
		// reconciliation (mintParticipantIds, web-mobile/js/admin_participants.jsx)
		// re-attaches an existing participant's id by (name, dojo) before
		// ever sending a resave to the server -- an id-less payload for an
		// already-stored row is not how any real write path constructs one.
		err := s.SaveParticipants(id, []domain.Player{
			{ID: stored[0].ID, Name: "Tora A", Dojo: "Tora Dojo", Metadata: []string{"Alice", "Bob", "Alice"}}, // unchanged pre-existing dup
			{ID: stored[1].ID, Name: "Tora B", Dojo: "Tora Dojo", Metadata: []string{"Carol", "Carol", "Eve"}}, // introduced by this write
			{ID: stored[2].ID, Name: "Tora C", Dojo: "Tora Dojo", Metadata: []string{"Frank", "Grace", "Heidi"}},
		})
		require.Error(t, err)
		assert.True(t, errors.Is(err, ErrDuplicateTeamMember))
		assert.Contains(t, err.Error(), "Tora B", "the message must name the offending team")
		assert.Contains(t, err.Error(), "Carol", "the message must name the repeated member")
		assert.NotContains(t, err.Error(), "Tora A", "the pre-existing pair is not re-reported")
	})
}

// TestTeamMemberUniqueness_RenameKeepsGrandfatheredDuplicate pins the
// checkTeamMemberNameCollisions grandfather comparison's identity key: the
// stored-vs-incoming match is by helper.PlayerKey (participant id, else
// normalized (name, dojo)), never by the raw team Name. A bare-name key
// would miss a pre-start team rename entirely -- the renamed row's stored
// bucket goes unconsulted under the OLD name, so a duplicate that was
// already grandfathered gets re-reported as fresh and an otherwise
// unrelated rename is refused. This is exactly the scenario
// TestTeamMemberUniqueness_GrandfathersStoredDuplicates covers for an
// UNCHANGED team name; this test is its rename counterpart.
func TestTeamMemberUniqueness_RenameKeepsGrandfatheredDuplicate(t *testing.T) {
	s, err := NewStore(t.TempDir())
	require.NoError(t, err)
	id := "c1"
	require.NoError(t, s.SaveCompetition(&Competition{ID: id, Name: "C1", Kind: "individual"}))
	require.NoError(t, s.SaveParticipants(id, []domain.Player{
		{Name: "Tora A", Dojo: "Tora Dojo", Metadata: []string{"Alice", "Bob", "Alice"}},
	}))
	// Now a team competition, still in setup (rename is a setup-gated
	// operation, per checkNewTeamNameCollisions' own doc comment), over that
	// same on-disk grandfathered roster.
	require.NoError(t, s.SaveCompetition(&Competition{ID: id, Name: "C1", Kind: "team", TeamSize: 3, Status: CompStatusSetup}))

	stored, err := s.LoadParticipants(id, false)
	require.NoError(t, err)
	require.Len(t, stored, 1)
	teamID := stored[0].ID
	require.NotEmpty(t, teamID, "the first save must have minted an id")

	// Rename the team, carrying its id forward (as any real rename path
	// must -- SaveParticipants itself never re-uses an id-less row's
	// identity across a name change). The member list, including its
	// pre-existing duplicate, is unchanged.
	err = s.SaveParticipants(id, []domain.Player{
		{ID: teamID, Name: "Tora Alpha", Dojo: "Tora Dojo", Metadata: []string{"Alice", "Bob", "Alice"}},
	})
	require.NoError(t, err, "a rename must still resolve to its stored bucket by identity, not by the old name")

	renamed, rerr := s.LoadParticipants(id, false)
	require.NoError(t, rerr)
	require.Len(t, renamed, 1)
	assert.Equal(t, "Tora Alpha", renamed[0].Name)
	assert.Equal(t, teamID, renamed[0].ID)
}

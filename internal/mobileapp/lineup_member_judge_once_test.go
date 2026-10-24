package mobileapp

import (
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/gin-gonic/gin"
	"github.com/gitrgoliveira/bracket-creator/internal/domain"
	"github.com/gitrgoliveira/bracket-creator/internal/state"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// bc-evts stage 3 review 4, G8: a lineup save reads team-members.yaml at most
// once (match-writes.md: every member-id check goes through ONE memberJudge).
// The anonymous caller's refusal check and the organiser's keep-only-held-ids
// pass used to build a judge each, so an anonymous save naming an id read the
// file twice.

// squadsReadCounter counts LoadSquads on the transaction handle it wraps.
type squadsReadCounter struct {
	state.StoreTx
	reads *int
}

func (c squadsReadCounter) LoadSquads(compID string) (map[string][]domain.TeamMember, error) {
	*c.reads++
	return c.StoreTx.LoadSquads(compID)
}

// countingTransactor hands every transaction a squadsReadCounter.
type countingTransactor struct {
	*state.Store
	reads *int
}

func (c countingTransactor) WithTransaction(compID string, fn func(tx state.StoreTx) error) error {
	return c.Store.WithTransaction(compID, func(tx state.StoreTx) error {
		return fn(squadsReadCounter{StoreTx: tx, reads: c.reads})
	})
}

func TestLineupSave_ReadsTheTeamMembersOnce(t *testing.T) {
	gin.SetMode(gin.TestMode)
	f := newTeamWritesFixture(t, true)
	reads := 0
	r := gin.New()
	RegisterLineupHandlers(r.Group("/api"), f.store, f.store, countingTransactor{Store: f.store, reads: &reads}, stubBroadcaster{}, f.store, NewFileVerifier(f.store))

	body := LineupRequest{
		Positions: map[domain.Position]string{domain.PosSenpo: "Sato"},
		MemberIDs: map[domain.Position]string{domain.PosSenpo: f.namedA},
		Changed:   &[]domain.Position{domain.PosSenpo},
	}
	req := jsonReq(http.MethodPut, f.lineupPath("PoolA-0"), body)
	req.Header.Set("X-Tournament-Password", "")
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)

	require.Equal(t, http.StatusOK, w.Code, w.Body.String())
	assert.Equal(t, 1, reads, "team-members.yaml is read once per save")
}

// unreadableSquadsTx answers LoadSquads as a members file that cannot be read.
type unreadableSquadsTx struct{ state.StoreTx }

func (unreadableSquadsTx) LoadSquads(string) (map[string][]domain.TeamMember, error) {
	return nil, assert.AnError
}

type unreadableSquadsTransactor struct{ *state.Store }

func (u unreadableSquadsTransactor) WithTransaction(compID string, fn func(tx state.StoreTx) error) error {
	return u.Store.WithTransaction(compID, func(tx state.StoreTx) error { return fn(unreadableSquadsTx{tx}) })
}

// G7: the organiser's save naming a member id, with team-members.yaml
// unreadable, is refused 409 team_members_unreadable, as every member-id check
// is (match-writes.md), where it used to store the ids unjudged.
func TestLineupSave_OrganiserRefusedWhenTheTeamMembersCannotBeRead(t *testing.T) {
	gin.SetMode(gin.TestMode)
	f := newTeamWritesFixture(t, false)
	r := gin.New()
	RegisterLineupHandlers(r.Group("/api"), f.store, f.store, unreadableSquadsTransactor{f.store}, stubBroadcaster{}, f.store, NewFileVerifier(f.store))

	body := LineupRequest{
		Positions: map[domain.Position]string{domain.PosSenpo: "Sato"},
		MemberIDs: map[domain.Position]string{domain.PosSenpo: f.namedA},
	}
	req := jsonReq(http.MethodPut, f.lineupPath("PoolA-0"), body)
	req.Header.Set("X-Tournament-Password", "main-pw")
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)

	require.Equal(t, http.StatusConflict, w.Code, w.Body.String())
	assert.Contains(t, w.Body.String(), "team_members_unreadable")
}

// A save that names the positions it changed is judged on those alone (stage 3
// review 11, Y9): the member ids its body carries at positions it did not
// change are not read, so a save changing a position by name alone reads no
// members file, and the unchanged positions' ids are neither judged nor
// stripped (the server lands only the changed ones).
func TestLineupSave_JudgesOnlyTheChangedPositions(t *testing.T) {
	gin.SetMode(gin.TestMode)
	f := newTeamWritesFixture(t, false)
	r := gin.New()
	RegisterLineupHandlers(r.Group("/api"), f.store, f.store, unreadableSquadsTransactor{f.store}, stubBroadcaster{}, f.store, NewFileVerifier(f.store))

	changed := []domain.Position{domain.PosSenpo}
	body := LineupRequest{
		Positions: map[domain.Position]string{domain.PosSenpo: "Sato", domain.PosJiho: "Kato"},
		MemberIDs: map[domain.Position]string{domain.PosJiho: f.namedA},
		Changed:   &changed,
	}
	req := jsonReq(http.MethodPut, f.lineupPath("PoolA-0"), body)
	req.Header.Set("X-Tournament-Password", "main-pw")
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)

	require.Equal(t, http.StatusOK, w.Code, w.Body.String())
}

// teamHoldingMembers saves one team on compID's roster and records each of
// `members` (id, a lowercase UUID v4, to name) as one of its members, so a
// lineup save naming those ids names members the team holds: a save keeps only
// those (memberIDsTheTeamHolds). Returns the team's participant id.
func teamHoldingMembers(t *testing.T, store *state.Store, compID string, members ...[2]string) string {
	t.Helper()
	require.NoError(t, store.SaveParticipants(compID, []domain.Player{{Name: "Team A", Dojo: "Dojo A"}}))
	teams, err := store.LoadParticipants(compID, false)
	require.NoError(t, err)
	require.Len(t, teams, 1)
	for _, m := range members {
		_, _, err := store.AddTeamMemberAs(compID, teams[0].ID, m[0], m[1])
		require.NoError(t, err)
	}
	return teams[0].ID
}

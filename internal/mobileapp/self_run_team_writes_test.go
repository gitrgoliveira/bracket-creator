package mobileapp

// A self-run tournament opens the public score sheet's team writes to an
// anonymous competitor (operator decision, bc-dhas): the match lineup PUT and
// naming a team member (POST .../members, PUT .../members/:memberId). An
// anonymous caller is held to the score path's rule: no change to a match
// that has finished, and no renaming a member who already has a name. The
// organiser, and every officiated tournament, are unchanged. The Lineups
// page's other writes stay behind the main password.
//
// Every request is built the way the public page sends it: an EMPTY
// X-Tournament-Password header, not a missing one.

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/gitrgoliveira/bracket-creator/internal/domain"
	"github.com/gitrgoliveira/bracket-creator/internal/state"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// teamWritesFixture is a team competition with two teams, a running match
// ("PoolA-0") and a finished one ("PoolA-1"), and each team's numbered blank
// members seeded the way the app seeds them.
type teamWritesFixture struct {
	r      *gin.Engine
	store  *state.Store
	teamA  string
	blankA string // a member of teamA with no name yet
	namedA string // a member of teamA who has a name
}

func newTeamWritesFixture(t *testing.T, selfRun bool) teamWritesFixture {
	t.Helper()
	store := newTempStore(t)
	if selfRun {
		seedSelfRunTournament(t, store, "admin-pw")
	} else {
		seedOfficiatedTournament(t, store)
	}
	require.NoError(t, store.SaveCompetition(&state.Competition{ID: "c1", Name: "Teams", Kind: "team", TeamSize: 5}))
	require.NoError(t, store.SaveParticipants("c1", []domain.Player{
		{Name: "Tora", Dojo: "Tora Dojo"},
		{Name: "Kuma", Dojo: "Kuma Dojo"},
	}))
	teams, err := store.LoadParticipants("c1", false)
	require.NoError(t, err)
	require.Len(t, teams, 2)
	a, b := teams[0], teams[1]
	require.NoError(t, store.SavePoolMatches("c1", []state.MatchResult{
		{ID: "PoolA-0", SideA: a.Name, SideB: b.Name, SideAID: a.ID, SideBID: b.ID, Status: state.MatchStatusRunning},
		{ID: "PoolA-1", SideA: a.Name, SideB: b.Name, SideAID: a.ID, SideBID: b.ID, Status: state.MatchStatusCompleted, Winner: a.Name, WinnerID: a.ID},
	}))
	store.EnsureLegacyUpgraded("c1")
	named, err := store.AddTeamMember("c1", a.ID, "Sato")
	require.NoError(t, err)
	squads, err := store.LoadSquads("c1")
	require.NoError(t, err)
	blank := ""
	for _, m := range squads[a.ID] {
		if m.Name == "" {
			blank = m.ID
			break
		}
	}
	require.NotEmpty(t, blank, "the team is seeded with numbered members that have no name yet")
	return teamWritesFixture{
		r:      setupSelfRunRouter(t, store, NewFileVerifier(store)),
		store:  store,
		teamA:  a.ID,
		blankA: blank,
		namedA: named.ID,
	}
}

// send serves one request with the given main password; "" is the public page.
func (f teamWritesFixture) send(method, path, password string, body any) *httptest.ResponseRecorder {
	req := jsonReq(method, path, body)
	req.Header.Set("X-Tournament-Password", password)
	w := httptest.NewRecorder()
	f.r.ServeHTTP(w, req)
	return w
}

func (f teamWritesFixture) lineupPath(matchID string) string {
	return "/api/competitions/c1/teams/" + f.teamA + "/match-lineups/" + matchID
}

func (f teamWritesFixture) membersPath() string {
	return "/api/competitions/c1/teams/" + f.teamA + "/members"
}

func (f teamWritesFixture) memberName(t *testing.T, memberID string) string {
	t.Helper()
	squads, err := f.store.LoadSquads("c1")
	require.NoError(t, err)
	for _, m := range squads[f.teamA] {
		if m.ID == memberID {
			return m.Name
		}
	}
	t.Fatalf("member %s not found", memberID)
	return ""
}

func (f teamWritesFixture) savedLineup(t *testing.T, matchID string) (domain.TeamLineup, bool) {
	t.Helper()
	lineups, err := f.store.LoadTeamLineups("c1")
	require.NoError(t, err)
	return findMatchLineup(lineups, f.teamA, matchID)
}

func senpo(name, memberID string) LineupRequest {
	return LineupRequest{
		Positions: map[domain.Position]string{domain.PosSenpo: name},
		MemberIDs: map[domain.Position]string{domain.PosSenpo: memberID},
	}
}

func TestSelfRun_MatchLineup_RefusesAnOverlongName(t *testing.T) {
	f := newTeamWritesFixture(t, true)

	w := f.send(http.MethodPut, f.lineupPath("PoolA-0"), "", senpo(strings.Repeat("x", MaxLenPlayerName+1), f.blankA))
	require.Equal(t, http.StatusBadRequest, w.Code, w.Body.String())
	_, ok := f.savedLineup(t, "PoolA-0")
	assert.False(t, ok, "a refused lineup writes nothing")
}

func TestSelfRun_MatchLineup_RefusesAMemberTheTeamDoesNotHold(t *testing.T) {
	f := newTeamWritesFixture(t, true)
	squads, err := f.store.LoadSquads("c1")
	require.NoError(t, err)
	foreign := ""
	for teamID, members := range squads {
		if teamID != f.teamA {
			foreign = members[0].ID
		}
	}
	require.NotEmpty(t, foreign, "the other team's member")

	for name, id := range map[string]string{"another team's member": foreign, "an id no team holds": "00000000-0000-4000-8000-000000000000"} {
		t.Run(name, func(t *testing.T) {
			w := f.send(http.MethodPut, f.lineupPath("PoolA-0"), "", senpo("Mei Ito", id))
			requireRefusal(t, w, http.StatusBadRequest, "team_member_not_in_team", "The member chosen for senpo is not on this team. Pick again from the list.")
			_, ok := f.savedLineup(t, "PoolA-0")
			assert.False(t, ok, "a refused lineup writes nothing")
		})
	}
}

func TestSelfRun_ScoreSheetTeamWrites_AnonymousCallerSaves(t *testing.T) {
	f := newTeamWritesFixture(t, true)

	w := f.send(http.MethodPut, f.lineupPath("PoolA-0"), "", senpo("Mei Ito", f.blankA))
	require.Equal(t, http.StatusOK, w.Code, "the match lineup: %s", w.Body.String())
	lineup, ok := f.savedLineup(t, "PoolA-0")
	require.True(t, ok)
	assert.Equal(t, "Mei Ito", lineup.Positions[domain.PosSenpo])
	assert.Equal(t, f.blankA, lineup.MemberIDs[domain.PosSenpo])

	w = f.send(http.MethodPut, f.membersPath()+"/"+f.blankA, "", map[string]any{"name": "Mei Ito"})
	require.Equal(t, http.StatusNoContent, w.Code, "naming a member with no name yet: %s", w.Body.String())
	assert.Equal(t, "Mei Ito", f.memberName(t, f.blankA))

	w = f.send(http.MethodPost, f.membersPath(), "", map[string]any{"name": "Ren Abe"})
	require.Equal(t, http.StatusCreated, w.Code, "adding a member: %s", w.Body.String())
}

func TestSelfRun_ScoreSheetTeamWrites_AnonymousGuards(t *testing.T) {
	t.Run("a finished match's lineup is refused", func(t *testing.T) {
		f := newTeamWritesFixture(t, true)
		w := f.send(http.MethodPut, f.lineupPath("PoolA-1"), "", senpo("Mei Ito", f.blankA))
		require.Equal(t, http.StatusConflict, w.Code, w.Body.String())
		assert.Contains(t, w.Body.String(), `"result_finalized"`)
		_, saved := f.savedLineup(t, "PoolA-1")
		assert.False(t, saved, "nothing is written")
	})

	t.Run("a lineup for a match that does not exist is refused", func(t *testing.T) {
		f := newTeamWritesFixture(t, true)
		w := f.send(http.MethodPut, f.lineupPath("PoolZ-9"), "", senpo("Mei Ito", f.blankA))
		require.Equal(t, http.StatusNotFound, w.Code, w.Body.String())
		_, saved := f.savedLineup(t, "PoolZ-9")
		assert.False(t, saved)
	})

	t.Run("renaming a member who has a name is refused", func(t *testing.T) {
		f := newTeamWritesFixture(t, true)
		w := f.send(http.MethodPut, f.membersPath()+"/"+f.namedA, "", map[string]any{"name": "Someone Else"})
		requireRefusal(t, w, http.StatusConflict, "member_already_named",
			"This team member already has a name. Ask the tournament organizer to change it.")
		assert.Equal(t, "Sato", f.memberName(t, f.namedA))
	})
}

// squadReadBarrier holds each squad read until a second one has happened (or
// a second has passed): the interleaving where two renames both check the
// member before either writes.
type squadReadBarrier struct {
	*state.Store
	mu      sync.Mutex
	reads   int
	release chan struct{}
}

func (b *squadReadBarrier) LoadSquads(compID string) (map[string][]domain.TeamMember, error) {
	squads, err := b.Store.LoadSquads(compID)
	b.mu.Lock()
	if b.reads++; b.reads == 2 {
		close(b.release)
	}
	b.mu.Unlock()
	select {
	case <-b.release:
	case <-time.After(time.Second):
	}
	return squads, err
}

// Two anonymous callers naming one blank member at once cannot both succeed:
// the second finds the first's name and is refused, however their requests
// interleave. The squad store holds each read until both requests have read,
// so a check made before the rename's own lock would pass for both.
func TestSelfRun_NamingABlankMemberAtOnceOneWins(t *testing.T) {
	f := newTeamWritesFixture(t, true)
	gin.SetMode(gin.TestMode)
	r := gin.New()
	squads := &squadReadBarrier{Store: f.store, release: make(chan struct{})}
	RegisterSquadHandlers(r.Group("/api"), squads, f.store, stubBroadcaster{}, f.store, NewFileVerifier(f.store))

	names := []string{"Mei Ito", "Ren Abe"}
	codes := make([]int, len(names))
	var wg sync.WaitGroup
	for i, name := range names {
		wg.Add(1)
		go func() {
			defer wg.Done()
			req := jsonReq(http.MethodPut, f.membersPath()+"/"+f.blankA, map[string]any{"name": name})
			req.Header.Set("X-Tournament-Password", "")
			w := httptest.NewRecorder()
			r.ServeHTTP(w, req)
			codes[i] = w.Code
		}()
	}
	wg.Wait()

	assert.ElementsMatch(t, []int{http.StatusNoContent, http.StatusConflict}, codes, "one caller names the member, the other is refused")
	winner := names[0]
	if codes[1] == http.StatusNoContent {
		winner = names[1]
	}
	assert.Equal(t, winner, f.memberName(t, f.blankA), "the member keeps the name of the caller that succeeded")
}

// The organiser keeps every rule the operator console has always had.
func TestSelfRun_ScoreSheetTeamWrites_OrganiserUnrestricted(t *testing.T) {
	f := newTeamWritesFixture(t, true)

	w := f.send(http.MethodPut, f.lineupPath("PoolA-1"), "main-pw", senpo("Mei Ito", f.blankA))
	require.Equal(t, http.StatusOK, w.Code, "a finished match's lineup stays editable: %s", w.Body.String())

	w = f.send(http.MethodPut, f.membersPath()+"/"+f.namedA, "main-pw", map[string]any{"name": "Sato Kenji"})
	require.Equal(t, http.StatusNoContent, w.Code, "renaming a named member: %s", w.Body.String())
	assert.Equal(t, "Sato Kenji", f.memberName(t, f.namedA))
}

// A password that is sent but wrong is neither the organiser nor a
// participant. The public page always sends an EMPTY header, so a wrong one is
// an organiser holding a stale password: it is answered 401 as a gated route
// answers it, not refused with a participant's sentence telling them to ask
// the tournament organizer.
func TestSelfRun_ScoreSheetTeamWrites_AWrongPasswordIsRefused(t *testing.T) {
	f := newTeamWritesFixture(t, true)
	for _, tc := range []struct {
		name, method, path string
		body               any
		asParticipant      int // what the public page's empty header gets
	}{
		{"a finished match's lineup", http.MethodPut, f.lineupPath("PoolA-1"), senpo("Mei Ito", f.blankA), http.StatusConflict},
		{"renaming a named member", http.MethodPut, f.membersPath() + "/" + f.namedA, map[string]any{"name": "Someone Else"}, http.StatusConflict},
		{"adding a member", http.MethodPost, f.membersPath(), map[string]any{"name": "Ren Abe"}, http.StatusCreated},
	} {
		t.Run(tc.name, func(t *testing.T) {
			requireInvalidPassword(t, f.send(tc.method, tc.path, "stale-pw", tc.body))
			w := f.send(tc.method, tc.path, "", tc.body)
			assert.Equal(t, tc.asParticipant, w.Code, "an empty header is still a participant: %s", w.Body.String())
		})
	}
	_, saved := f.savedLineup(t, "PoolA-1")
	assert.False(t, saved, "no lineup is written")
	assert.Equal(t, "Sato", f.memberName(t, f.namedA), "no member is renamed")
}

// requireInvalidPassword checks a write was refused the way AuthMiddleware
// refuses a wrong password on a gated route.
func requireInvalidPassword(t *testing.T, w *httptest.ResponseRecorder) {
	t.Helper()
	require.Equal(t, http.StatusUnauthorized, w.Code, w.Body.String())
	assert.JSONEq(t, `{"error":"invalid tournament password"}`, w.Body.String())
}

// An officiated tournament opens nothing: the same writes still need the
// main password.
func TestOfficiated_ScoreSheetTeamWrites_StillNeedThePassword(t *testing.T) {
	f := newTeamWritesFixture(t, false)
	for _, tc := range []struct {
		name, method, path string
		body               any
	}{
		{"match lineup", http.MethodPut, f.lineupPath("PoolA-0"), senpo("Mei Ito", f.blankA)},
		{"name a member", http.MethodPut, f.membersPath() + "/" + f.blankA, map[string]any{"name": "Mei Ito"}},
		{"add a member", http.MethodPost, f.membersPath(), map[string]any{"name": "Ren Abe"}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			w := f.send(tc.method, tc.path, "", tc.body)
			assert.Equal(t, http.StatusUnauthorized, w.Code, w.Body.String())
			w = f.send(tc.method, tc.path, "main-pw", tc.body)
			assert.Less(t, w.Code, 300, "with the password it still works: %s", w.Body.String())
		})
	}
	_, saved := f.savedLineup(t, "PoolA-0")
	assert.True(t, saved, "the organiser's lineup write landed")
}

// The Lineups page's other writes, and the team-members read, stay behind
// the main password in a self-run tournament: the public page never makes them.
func TestSelfRun_LineupsPageWrites_StayGated(t *testing.T) {
	f := newTeamWritesFixture(t, true)
	for _, tc := range []struct{ name, method, path string }{
		{"round lineup PUT", http.MethodPut, "/api/competitions/c1/teams/" + f.teamA + "/lineups/0"},
		{"round lineup DELETE", http.MethodDelete, "/api/competitions/c1/teams/" + f.teamA + "/lineups/0"},
		{"match lineup DELETE", http.MethodDelete, f.lineupPath("PoolA-0")},
		{"team members GET", http.MethodGet, "/api/competitions/c1/team-members"},
		{"member name clear", http.MethodDelete, f.membersPath() + "/" + f.namedA},
	} {
		t.Run(tc.name, func(t *testing.T) {
			w := f.send(tc.method, tc.path, "", senpo("Mei Ito", f.blankA))
			assert.Equal(t, http.StatusUnauthorized, w.Code, w.Body.String())
		})
	}
}

// A participant's lineup write names a team the match holds: an id neither
// side carries would add a lineup nobody reads, one key per request. The
// organiser keeps the lineup route as it was.
func TestSelfRun_MatchLineupNamesATeamInTheMatch(t *testing.T) {
	f := newTeamWritesFixture(t, true)
	stranger := "/api/competitions/c1/teams/33333333-3333-4333-3333-333333333333/match-lineups/PoolA-0"

	w := f.send(http.MethodPut, stranger, "", senpo("Mei Ito", f.blankA))
	requireRefusal(t, w, http.StatusNotFound, "team_not_in_match",
		"This team is not in this match. Check the score sheet and try again.")
	lineups, err := f.store.LoadTeamLineups("c1")
	require.NoError(t, err)
	assert.Empty(t, lineups, "nothing is written")

	w = f.send(http.MethodPut, stranger, "main-pw", senpo("Mei Ito", ""))
	require.Equal(t, http.StatusOK, w.Code, "the organiser's write: %s", w.Body.String())
}

// A participant adds a member while the competition runs, up to a limit, and
// never a name longer than a competitor's. The organiser keeps adding past
// both limits of a participant's.
func TestSelfRun_AddingAMemberIsBounded(t *testing.T) {
	long := strings.Repeat("a", MaxLenPlayerName+1)

	t.Run("a name longer than a competitor's is refused", func(t *testing.T) {
		f := newTeamWritesFixture(t, true)
		for _, password := range []string{"", "main-pw"} {
			w := f.send(http.MethodPost, f.membersPath(), password, map[string]any{"name": long})
			assert.Equal(t, http.StatusBadRequest, w.Code, "adding, password %q: %s", password, w.Body.String())
			w = f.send(http.MethodPut, f.membersPath()+"/"+f.blankA, password, map[string]any{"name": long})
			assert.Equal(t, http.StatusBadRequest, w.Code, "naming, password %q: %s", password, w.Body.String())
		}
		assert.Empty(t, f.memberName(t, f.blankA))
	})

	t.Run("a wrong password is refused before the body is judged", func(t *testing.T) {
		f := newTeamWritesFixture(t, true)
		requireInvalidPassword(t, f.send(http.MethodPost, f.membersPath(), "stale-pw", map[string]any{"name": long}))
		requireInvalidPassword(t, f.send(http.MethodPut, f.membersPath()+"/"+f.blankA, "stale-pw", map[string]any{"name": long}))
		requireInvalidPassword(t, f.send(http.MethodPut, f.lineupPath("PoolA-0"), "stale-pw", map[string]any{"positions": "not a map"}))
	})

	t.Run("a participant adds up to the limit", func(t *testing.T) {
		f := newTeamWritesFixture(t, true)
		squads, err := f.store.LoadSquads("c1")
		require.NoError(t, err)
		limit := 2*5 + state.SquadReserveSlots
		for i := len(squads[f.teamA]); i < limit; i++ {
			w := f.send(http.MethodPost, f.membersPath(), "", map[string]any{"name": "Member " + string(rune('A'+i))})
			require.Equal(t, http.StatusCreated, w.Code, w.Body.String())
		}
		w := f.send(http.MethodPost, f.membersPath(), "", map[string]any{"name": "One Too Many"})
		requireRefusal(t, w, http.StatusConflict, "team_member_limit",
			"This team already lists as many members as the score sheet can add. Ask the tournament organizer to add more.")
		w = f.send(http.MethodPost, f.membersPath(), "main-pw", map[string]any{"name": "One Too Many"})
		require.Equal(t, http.StatusCreated, w.Code, "the organiser adds past it: %s", w.Body.String())
	})

	t.Run("a finished competition's teams are the organiser's", func(t *testing.T) {
		f := newTeamWritesFixture(t, true)
		require.NoError(t, f.store.SaveCompetition(&state.Competition{ID: "c1", Name: "Teams", Kind: "team", TeamSize: 5, Status: state.CompStatusComplete}))
		const finished = "This competition has finished, so its teams can no longer be changed. Contact the tournament organizer to correct it."
		requireRefusal(t, f.send(http.MethodPost, f.membersPath(), "", map[string]any{"name": "Ren Abe"}),
			http.StatusConflict, "competition_finished", finished)
		requireRefusal(t, f.send(http.MethodPut, f.membersPath()+"/"+f.blankA, "", map[string]any{"name": "Mei Ito"}),
			http.StatusConflict, "competition_finished", finished)
		assert.Empty(t, f.memberName(t, f.blankA))

		w := f.send(http.MethodPost, f.membersPath(), "main-pw", map[string]any{"name": "Ren Abe"})
		require.Equal(t, http.StatusCreated, w.Code, w.Body.String())
		w = f.send(http.MethodPut, f.membersPath()+"/"+f.blankA, "main-pw", map[string]any{"name": "Mei Ito"})
		require.Equal(t, http.StatusNoContent, w.Code, w.Body.String())
	})
}

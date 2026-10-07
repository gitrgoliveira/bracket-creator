package mobileapp

// handlers_lineup_changed_test.go pins what a lineup save may say about which
// positions it changed (operator decision 2026-10-07: "Only changed positions").
// PUT .../match-lineups/:matchId and PUT .../lineups/0 take an optional
// `changed` list of position keys. Absent, the body is the whole lineup and
// replaces the stored one, as a queued save persisted by the previous build
// replays. Present, the server reads only the changed keys (the client may send
// its whole form beside them) and lands them on a base it reads under the write's
// lock: the match's own stored lineup, else the lineup in force there (what the
// team carries from its previous match, else its starting lineup), else empty.
// Two devices changing different positions then both keep their change, in
// either arrival order.

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/gin-gonic/gin"
	"github.com/gitrgoliveira/bracket-creator/internal/domain"
	"github.com/gitrgoliveira/bracket-creator/internal/state"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// changedLineupFixture is a self-run team competition of five-person teams in a
// league: Tora (the team the lineups are for) plays PoolA-0 and PoolA-1, both
// running, and PoolA-2 is a match Tora is not seated in.
type changedLineupFixture struct {
	r     *gin.Engine
	store *state.Store
	team  string
}

const (
	organiserPassword = "main-pw"
	publicPassword    = ""
)

func newChangedLineupFixture(t *testing.T) changedLineupFixture {
	t.Helper()
	store := newTempStore(t)
	seedSelfRunTournament(t, store, "admin-pw")
	require.NoError(t, store.SaveCompetition(&state.Competition{
		ID: "c1", Name: "Teams", Kind: "team", TeamSize: 5, Format: state.CompFormatLeague, Status: state.CompStatusPools,
	}))
	require.NoError(t, store.SaveParticipants("c1", []domain.Player{
		{Name: "Tora", Dojo: "Tora Dojo"}, {Name: "Kuma", Dojo: "Kuma Dojo"}, {Name: "Saru", Dojo: "Saru Dojo"},
	}))
	teams, err := store.LoadParticipants("c1", false)
	require.NoError(t, err)
	require.Len(t, teams, 3)
	tora, kuma, saru := teams[0], teams[1], teams[2]
	require.NoError(t, store.SavePoolMatches("c1", []state.MatchResult{
		{ID: "PoolA-0", SideA: tora.Name, SideAID: tora.ID, SideB: kuma.Name, SideBID: kuma.ID, Status: state.MatchStatusRunning},
		{ID: "PoolA-1", SideA: tora.Name, SideAID: tora.ID, SideB: saru.Name, SideBID: saru.ID, Status: state.MatchStatusRunning},
		{ID: "PoolA-2", SideA: kuma.Name, SideAID: kuma.ID, SideB: saru.Name, SideBID: saru.ID, Status: state.MatchStatusRunning},
	}))
	store.EnsureLegacyUpgraded("c1")
	return changedLineupFixture{r: setupSelfRunRouter(t, store, NewFileVerifier(store)), store: store, team: tora.ID}
}

func (f changedLineupFixture) matchPath(matchID string) string {
	return "/api/competitions/c1/teams/" + f.team + "/match-lineups/" + matchID
}

func (f changedLineupFixture) startingPath() string {
	return "/api/competitions/c1/teams/" + f.team + "/lineups/0"
}

func (f changedLineupFixture) put(path, password string, body any) *httptest.ResponseRecorder {
	req := jsonReq(http.MethodPut, path, body)
	req.Header.Set("X-Tournament-Password", password)
	w := httptest.NewRecorder()
	f.r.ServeHTTP(w, req)
	return w
}

// lineupFrom decodes the whole lineup a save answered with.
func lineupFrom(t *testing.T, w *httptest.ResponseRecorder) domain.TeamLineup {
	t.Helper()
	require.Equal(t, http.StatusOK, w.Code, w.Body.String())
	var l domain.TeamLineup
	require.NoError(t, json.Unmarshal(w.Body.Bytes(), &l))
	return l
}

// storedMatch is the lineup stored for Tora at a match, and whether there is one.
func (f changedLineupFixture) storedMatch(t *testing.T, matchID string) (domain.TeamLineup, bool) {
	t.Helper()
	lineups, err := f.store.LoadTeamLineups("c1")
	require.NoError(t, err)
	return findMatchLineup(lineups, f.team, matchID)
}

func (f changedLineupFixture) storedStarting(t *testing.T) (domain.TeamLineup, bool) {
	t.Helper()
	lineups, err := f.store.LoadTeamLineups("c1")
	require.NoError(t, err)
	return findRoundLineup(lineups, f.team, 0)
}

// positions builds a positions map from alternating position and name arguments.
func posMap(pairs ...string) map[domain.Position]string {
	out := map[domain.Position]string{}
	for i := 0; i+1 < len(pairs); i += 2 {
		out[domain.Position(pairs[i])] = pairs[i+1]
	}
	return out
}

// save is a body in the shape the client sends: positions, member ids when given,
// and, when changed names any position, the positions the save changed.
func saveBody(p, ids map[domain.Position]string, changed ...domain.Position) map[string]any {
	body := map[string]any{"positions": p}
	if ids != nil {
		body["memberIds"] = ids
	}
	if len(changed) > 0 {
		body["changed"] = changed
	}
	return body
}

var (
	pSenpo  = domain.PosSenpo
	pJiho   = domain.PosJiho
	pChuken = domain.PosChuken
)

// A first partial save at a match with no lineup of its own lands on the lineup
// the team carries there, not on an empty one: the match's own lineup is read
// whole (the previous match's is never merged in), so an empty base would drop
// every carried position on the next read.
func TestChangedLineup_LandsOnTheLineupInForce(t *testing.T) {
	t.Run("carried from the team's previous match", func(t *testing.T) {
		f := newChangedLineupFixture(t)
		whole := posMap("senpo", "Ito", "jiho", "Ueno", "chuken", "Endo")
		require.Equal(t, http.StatusOK, f.put(f.matchPath("PoolA-0"), organiserPassword, saveBody(whole, nil)).Code)

		w := f.put(f.matchPath("PoolA-1"), organiserPassword, saveBody(posMap("jiho", "Ueda"), nil, pJiho))

		got := lineupFrom(t, w)
		assert.Equal(t, posMap("senpo", "Ito", "jiho", "Ueda", "chuken", "Endo"), got.Positions,
			"the answer is the whole lineup as stored: the carried positions and the change")
		stored, ok := f.storedMatch(t, "PoolA-1")
		require.True(t, ok)
		assert.Equal(t, got.Positions, stored.Positions)
		first, _ := f.storedMatch(t, "PoolA-0")
		assert.Equal(t, whole, first.Positions, "the match it was carried from is left alone")
	})

	t.Run("carried from the starting lineup when no earlier match has one", func(t *testing.T) {
		f := newChangedLineupFixture(t)
		require.Equal(t, http.StatusOK, f.put(f.startingPath(), organiserPassword,
			saveBody(posMap("senpo", "Ito", "jiho", "Ueno"), map[domain.Position]string{pSenpo: "m-ito", pJiho: "m-ueno"})).Code)

		w := f.put(f.matchPath("PoolA-0"), organiserPassword, saveBody(posMap("jiho", "Ueda"), nil, pJiho))

		got := lineupFrom(t, w)
		assert.Equal(t, posMap("senpo", "Ito", "jiho", "Ueda"), got.Positions)
		assert.Equal(t, map[domain.Position]string{pSenpo: "m-ito"}, got.MemberIDs,
			"the changed position took no id, so its carried id is gone with the name it belonged to, and the other keeps its own")
	})

	t.Run("an empty base when the team carries nothing", func(t *testing.T) {
		f := newChangedLineupFixture(t)

		w := f.put(f.matchPath("PoolA-0"), organiserPassword, saveBody(posMap("senpo", "Ito"), nil, pSenpo))

		assert.Equal(t, posMap("senpo", "Ito"), lineupFrom(t, w).Positions)
	})

	t.Run("a match the team is not seated in has the base the read shows there", func(t *testing.T) {
		f := newChangedLineupFixture(t)
		require.Equal(t, http.StatusOK, f.put(f.startingPath(), organiserPassword, saveBody(posMap("senpo", "Ito", "jiho", "Ueno"), nil)).Code)
		require.Equal(t, http.StatusOK, f.put(f.matchPath("PoolA-0"), organiserPassword, saveBody(posMap("senpo", "Endo", "jiho", "Ueno"), nil)).Code)

		w := f.put(f.matchPath("PoolA-2"), organiserPassword, saveBody(posMap("jiho", "Ueda"), nil, pJiho))

		assert.Equal(t, posMap("senpo", "Endo", "jiho", "Ueda"), lineupFrom(t, w).Positions,
			"the match's place in the draw is what counts, as in the lineup-in-force read: PoolA-0 comes before it")
	})

	t.Run("a match the draw does not hold has the starting lineup as its base", func(t *testing.T) {
		f := newChangedLineupFixture(t)
		require.Equal(t, http.StatusOK, f.put(f.startingPath(), organiserPassword, saveBody(posMap("senpo", "Ito", "jiho", "Ueno"), nil)).Code)
		require.Equal(t, http.StatusOK, f.put(f.matchPath("PoolA-0"), organiserPassword, saveBody(posMap("senpo", "Endo", "jiho", "Ueno"), nil)).Code)

		w := f.put(f.matchPath("NoSuchMatch"), organiserPassword, saveBody(posMap("jiho", "Ueda"), nil, pJiho))

		assert.Equal(t, posMap("senpo", "Ito", "jiho", "Ueda"), lineupFrom(t, w).Positions)
	})
}

// Two devices that change different positions of one lineup both keep their
// change, whichever arrives first, because each lands on what is stored when it
// arrives. Each sends its whole form, as the client does, with the position it
// did not touch as it last read it.
func TestChangedLineup_TwoDevicesChangingDifferentPositionsBothKeepTheirs(t *testing.T) {
	deviceA := saveBody(posMap("senpo", "Ito-new", "jiho", "Ueno-old"), nil, pSenpo)
	deviceB := saveBody(posMap("senpo", "Ito-old", "jiho", "Ueno-new"), nil, pJiho)
	want := posMap("senpo", "Ito-new", "jiho", "Ueno-new")

	for name, order := range map[string][]map[string]any{"A then B": {deviceA, deviceB}, "B then A": {deviceB, deviceA}} {
		t.Run(name, func(t *testing.T) {
			f := newChangedLineupFixture(t)
			require.Equal(t, http.StatusOK, f.put(f.matchPath("PoolA-0"), organiserPassword, saveBody(posMap("senpo", "Ito-old", "jiho", "Ueno-old"), nil)).Code)

			var last *httptest.ResponseRecorder
			for _, body := range order {
				last = f.put(f.matchPath("PoolA-0"), organiserPassword, body)
				require.Equal(t, http.StatusOK, last.Code, last.Body.String())
			}

			assert.Equal(t, want, lineupFrom(t, last).Positions)
			stored, _ := f.storedMatch(t, "PoolA-0")
			assert.Equal(t, want, stored.Positions)
		})
	}

	t.Run("on a match with no lineup of its own", func(t *testing.T) {
		f := newChangedLineupFixture(t)
		require.Equal(t, http.StatusOK, f.put(f.startingPath(), organiserPassword, saveBody(posMap("senpo", "Ito-old", "jiho", "Ueno-old"), nil)).Code)

		require.Equal(t, http.StatusOK, f.put(f.matchPath("PoolA-1"), organiserPassword, deviceB).Code)
		require.Equal(t, http.StatusOK, f.put(f.matchPath("PoolA-1"), organiserPassword, deviceA).Code)

		stored, _ := f.storedMatch(t, "PoolA-1")
		assert.Equal(t, want, stored.Positions, "the first save made the carried lineup the match's own, and the second landed on it")
	})
}

func TestChangedLineup_TheSamePositionTheLaterArrivalWins(t *testing.T) {
	f := newChangedLineupFixture(t)
	require.Equal(t, http.StatusOK, f.put(f.matchPath("PoolA-0"), organiserPassword, saveBody(posMap("senpo", "Ito"), nil)).Code)

	require.Equal(t, http.StatusOK, f.put(f.matchPath("PoolA-0"), organiserPassword, saveBody(posMap("senpo", "first"), nil, pSenpo)).Code)
	w := f.put(f.matchPath("PoolA-0"), organiserPassword, saveBody(posMap("senpo", "second"), nil, pSenpo))

	assert.Equal(t, posMap("senpo", "second"), lineupFrom(t, w).Positions)
}

func TestChangedLineup_Refusals(t *testing.T) {
	t.Run("a changed position with no entry in positions is a 400 and writes nothing", func(t *testing.T) {
		f := newChangedLineupFixture(t)
		require.Equal(t, http.StatusOK, f.put(f.matchPath("PoolA-0"), organiserPassword, saveBody(posMap("senpo", "Ito"), nil)).Code)

		w := f.put(f.matchPath("PoolA-0"), organiserPassword, saveBody(posMap("senpo", "Ito"), nil, pSenpo, pJiho))

		require.Equal(t, http.StatusBadRequest, w.Code, w.Body.String())
		assert.Contains(t, w.Body.String(), "jiho")
		stored, _ := f.storedMatch(t, "PoolA-0")
		assert.Equal(t, posMap("senpo", "Ito"), stored.Positions)
	})

	t.Run("an empty changed list is a 400", func(t *testing.T) {
		f := newChangedLineupFixture(t)

		w := f.put(f.matchPath("PoolA-0"), organiserPassword, map[string]any{"positions": posMap("senpo", "Ito"), "changed": []string{}})

		require.Equal(t, http.StatusBadRequest, w.Code, w.Body.String())
		_, ok := f.storedMatch(t, "PoolA-0")
		assert.False(t, ok, "nothing was stored")
	})

	t.Run("a changed position the team size does not have is a 400", func(t *testing.T) {
		f := newChangedLineupFixture(t)

		w := f.put(f.matchPath("PoolA-0"), organiserPassword, saveBody(posMap("7", "Ito"), nil, "7"))

		require.Equal(t, http.StatusBadRequest, w.Code, w.Body.String())
	})

	t.Run("a member at two positions is the duplicate refusal, naming both, not a silent drop", func(t *testing.T) {
		f := newChangedLineupFixture(t)
		require.Equal(t, http.StatusOK, f.put(f.matchPath("PoolA-0"), organiserPassword,
			saveBody(posMap("senpo", "Ito", "jiho", "Ueno"), map[domain.Position]string{pSenpo: "m-ito", pJiho: "m-ueno"})).Code)

		w := f.put(f.matchPath("PoolA-0"), organiserPassword,
			saveBody(posMap("jiho", "Ito"), map[domain.Position]string{pJiho: "m-ito"}, pJiho))

		require.Equal(t, http.StatusBadRequest, w.Code, w.Body.String())
		assert.Contains(t, w.Body.String(), "senpo", "the position the member already holds")
		assert.Contains(t, w.Body.String(), "jiho", "the position the change put it at")
		stored, _ := f.storedMatch(t, "PoolA-0")
		assert.Equal(t, "Ueno", stored.Positions[pJiho], "the refused change is not stored")
	})
}

// Everything in the body that is not a changed position is ignored, including a
// key a whole lineup would be refused for.
func TestChangedLineup_IgnoresWhatIsNotChanged(t *testing.T) {
	f := newChangedLineupFixture(t)
	require.Equal(t, http.StatusOK, f.put(f.matchPath("PoolA-0"), organiserPassword,
		saveBody(posMap("senpo", "Ito", "jiho", "Ueno"), map[domain.Position]string{pSenpo: "m-ito"})).Code)
	form := posMap("senpo", "Ito-stale", "jiho", "Ueno-new", "taisho", "Kato", "nonsense", "x", "fukusho", strings.Repeat("x", MaxLenPlayerName+1))
	ids := map[domain.Position]string{pSenpo: "m-stale", pJiho: "m-ueno", "nonsense": "m-x"}

	w := f.put(f.matchPath("PoolA-0"), organiserPassword, saveBody(form, ids, pJiho))

	got := lineupFrom(t, w)
	assert.Equal(t, posMap("senpo", "Ito", "jiho", "Ueno-new"), got.Positions,
		"only pJiho changed: another position's stale value, a position the lineup never held, an invalid key and an overlong name are all ignored")
	assert.Equal(t, map[domain.Position]string{pSenpo: "m-ito", pJiho: "m-ueno"}, got.MemberIDs)
}

func TestChangedLineup_ClearsAndPlacesUnnamedMembers(t *testing.T) {
	t.Run("a changed position with no name and no id is removed from both maps", func(t *testing.T) {
		f := newChangedLineupFixture(t)
		require.Equal(t, http.StatusOK, f.put(f.matchPath("PoolA-0"), organiserPassword,
			saveBody(posMap("senpo", "Ito", "jiho", "Ueno"), map[domain.Position]string{pSenpo: "m-ito", pJiho: "m-ueno"})).Code)

		w := f.put(f.matchPath("PoolA-0"), organiserPassword, saveBody(posMap("jiho", ""), nil, pJiho))

		got := lineupFrom(t, w)
		assert.Equal(t, posMap("senpo", "Ito"), got.Positions, "the key is gone, not kept with an empty name")
		assert.Equal(t, map[domain.Position]string{pSenpo: "m-ito"}, got.MemberIDs)
	})

	t.Run("a changed position with an id and no name places an unnamed member", func(t *testing.T) {
		f := newChangedLineupFixture(t)
		require.Equal(t, http.StatusOK, f.put(f.matchPath("PoolA-0"), organiserPassword, saveBody(posMap("senpo", "Ito"), nil)).Code)

		w := f.put(f.matchPath("PoolA-0"), organiserPassword, saveBody(posMap("chuken", ""), map[domain.Position]string{pChuken: "m-blank"}, pChuken))

		got := lineupFrom(t, w)
		name, held := got.Positions[pChuken]
		assert.True(t, held, "the position is held")
		assert.Empty(t, name)
		assert.Equal(t, "m-blank", got.MemberIDs[pChuken])
	})

	t.Run("a changed position with a name and no id drops the id the position had", func(t *testing.T) {
		f := newChangedLineupFixture(t)
		require.Equal(t, http.StatusOK, f.put(f.matchPath("PoolA-0"), organiserPassword,
			saveBody(posMap("senpo", "Ito"), map[domain.Position]string{pSenpo: "m-ito"})).Code)

		w := f.put(f.matchPath("PoolA-0"), organiserPassword, saveBody(posMap("senpo", "Someone new"), nil, pSenpo))

		got := lineupFrom(t, w)
		assert.Equal(t, posMap("senpo", "Someone new"), got.Positions)
		assert.NotContains(t, got.MemberIDs, pSenpo, "the old member's id does not stay beside a new name")
	})

	t.Run("clearing every position leaves an own lineup that is empty", func(t *testing.T) {
		f := newChangedLineupFixture(t)
		require.Equal(t, http.StatusOK, f.put(f.matchPath("PoolA-0"), organiserPassword, saveBody(posMap("senpo", "Ito"), nil)).Code)

		w := f.put(f.matchPath("PoolA-0"), organiserPassword, saveBody(posMap("senpo", ""), nil, pSenpo))

		assert.Empty(t, lineupFrom(t, w).Positions)
		_, ok := f.storedMatch(t, "PoolA-0")
		assert.True(t, ok, "the match still has a lineup of its own, and it is empty")
	})
}

// Without `changed` the body is the whole lineup and replaces the stored one: the
// shape a save queued by the previous build replays in.
func TestChangedLineup_WholeReplacementWhenChangedIsAbsent(t *testing.T) {
	f := newChangedLineupFixture(t)
	require.Equal(t, http.StatusOK, f.put(f.matchPath("PoolA-0"), organiserPassword,
		saveBody(posMap("senpo", "Ito", "jiho", "Ueno"), map[domain.Position]string{pSenpo: "m-ito"})).Code)

	w := f.put(f.matchPath("PoolA-0"), organiserPassword, saveBody(posMap("chuken", "Endo"), nil))

	got := lineupFrom(t, w)
	assert.Equal(t, posMap("chuken", "Endo"), got.Positions)
	assert.Empty(t, got.MemberIDs)

	t.Run("an empty positions map clears the lineup, and still is a whole lineup", func(t *testing.T) {
		w := f.put(f.matchPath("PoolA-0"), organiserPassword, map[string]any{"positions": map[string]string{}})

		assert.Empty(t, lineupFrom(t, w).Positions)
	})
}

func TestChangedLineup_TheStartingLineup(t *testing.T) {
	t.Run("lands on the stored starting lineup", func(t *testing.T) {
		f := newChangedLineupFixture(t)
		require.Equal(t, http.StatusOK, f.put(f.startingPath(), organiserPassword, saveBody(posMap("senpo", "Ito", "jiho", "Ueno"), nil)).Code)

		w := f.put(f.startingPath(), organiserPassword, saveBody(posMap("senpo", "Ito-stale", "jiho", "Ueda"), nil, pJiho))

		got := lineupFrom(t, w)
		assert.Equal(t, posMap("senpo", "Ito", "jiho", "Ueda"), got.Positions)
		assert.Zero(t, got.Round)
		assert.Empty(t, got.MatchID)
		stored, ok := f.storedStarting(t)
		require.True(t, ok)
		assert.Equal(t, got.Positions, stored.Positions)
	})

	t.Run("an empty base when none is stored, never a match's lineup", func(t *testing.T) {
		f := newChangedLineupFixture(t)
		require.Equal(t, http.StatusOK, f.put(f.matchPath("PoolA-0"), organiserPassword, saveBody(posMap("senpo", "Endo"), nil)).Code)

		w := f.put(f.startingPath(), organiserPassword, saveBody(posMap("jiho", "Ueda"), nil, pJiho))

		assert.Equal(t, posMap("jiho", "Ueda"), lineupFrom(t, w).Positions)
	})

	t.Run("a later round is still refused", func(t *testing.T) {
		f := newChangedLineupFixture(t)

		w := f.put("/api/competitions/c1/teams/"+f.team+"/lineups/1", organiserPassword, saveBody(posMap("jiho", "Ueda"), nil, pJiho))

		require.Equal(t, http.StatusBadRequest, w.Code, w.Body.String())
	})
}

// A participant of a self-run tournament saves a match's lineup from the public
// score sheet. The refusals that depend on the match are unchanged, and the check
// that a member id belongs to the team judges the ids of the changed positions
// only: the rest of the body is ignored, so a stale id in it cannot refuse a save.
func TestChangedLineup_AnAnonymousCaller(t *testing.T) {
	f := newChangedLineupFixture(t)
	squads, err := f.store.LoadSquads("c1")
	require.NoError(t, err)
	require.NotEmpty(t, squads[f.team])
	mine := squads[f.team][0].ID

	t.Run("an id outside the team in a position that did not change is not judged", func(t *testing.T) {
		w := f.put(f.matchPath("PoolA-0"), publicPassword,
			saveBody(posMap("senpo", "Ito", "jiho", "Ueno"), map[domain.Position]string{pSenpo: mine, pJiho: "not-on-this-team"}, pSenpo))

		got := lineupFrom(t, w)
		assert.Equal(t, mine, got.MemberIDs[pSenpo])
		assert.Equal(t, posMap("senpo", "Ito"), got.Positions, "and the unchanged position's name is not stored either")
	})

	t.Run("an id outside the team in a changed position is refused", func(t *testing.T) {
		w := f.put(f.matchPath("PoolA-1"), publicPassword,
			saveBody(posMap("jiho", "Ueno"), map[domain.Position]string{pJiho: "not-on-this-team"}, pJiho))

		require.Equal(t, http.StatusBadRequest, w.Code, w.Body.String())
		assert.Contains(t, w.Body.String(), "team_member_not_in_team")
		_, ok := f.storedMatch(t, "PoolA-1")
		assert.False(t, ok)
	})

	t.Run("a whole lineup is judged on every id, as before", func(t *testing.T) {
		w := f.put(f.matchPath("PoolA-1"), publicPassword,
			saveBody(posMap("senpo", "Ito", "jiho", "Ueno"), map[domain.Position]string{pSenpo: mine, pJiho: "not-on-this-team"}))

		require.Equal(t, http.StatusBadRequest, w.Code, w.Body.String())
	})

	t.Run("a team the match does not hold is still refused", func(t *testing.T) {
		w := f.put(f.matchPath("PoolA-2"), publicPassword, saveBody(posMap("senpo", "Ito"), nil, pSenpo))

		require.Equal(t, http.StatusNotFound, w.Code, w.Body.String())
		assert.Contains(t, w.Body.String(), "team_not_in_match")
	})

	t.Run("a finished match is still refused", func(t *testing.T) {
		_, err := f.store.UpdatePoolMatchByID("c1", "PoolA-0", func(m *state.MatchResult) error {
			m.Status = state.MatchStatusCompleted
			return nil
		})
		require.NoError(t, err)

		w := f.put(f.matchPath("PoolA-0"), publicPassword, saveBody(posMap("senpo", "Ito-again"), map[domain.Position]string{pSenpo: mine}, pSenpo))

		require.Equal(t, http.StatusConflict, w.Code, w.Body.String())
		assert.Contains(t, w.Body.String(), "result_finalized")
	})

	t.Run("the starting lineup stays the organiser's", func(t *testing.T) {
		w := f.put(f.startingPath(), publicPassword, saveBody(posMap("jiho", "Ueda"), nil, pJiho))

		assert.Equal(t, http.StatusUnauthorized, w.Code, w.Body.String())
	})
}

// A save is announced as it was before, naming the team and the match.
func TestChangedLineup_IsAnnouncedLikeAnyLineupSave(t *testing.T) {
	gin.SetMode(gin.TestMode)
	f := newChangedLineupFixture(t)
	hub := &payloadBroadcaster{}
	r := gin.New()
	admin := r.Group("/api")
	admin.Use(AuthMiddleware(NewFileVerifier(f.store), f.store))
	RegisterLineupHandlers(admin, f.store, f.store, f.store, hub, f.store, NewFileVerifier(f.store))

	req := jsonReq(http.MethodPut, f.matchPath("PoolA-0"), saveBody(posMap("senpo", "Ito"), nil, pSenpo))
	req.Header.Set("X-Tournament-Password", organiserPassword)
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)

	require.Equal(t, http.StatusOK, w.Code, w.Body.String())
	assert.Equal(t, gin.H{"competitionId": "c1", "teamId": f.team, "matchId": "PoolA-0"}, hub.last(t))
}

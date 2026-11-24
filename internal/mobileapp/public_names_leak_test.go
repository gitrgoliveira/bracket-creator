package mobileapp

// The public-names leak test (bc-hnam, bc-anon), the gate for hiding
// competitor names from the public view. In an officiated tournament it seeds
// an individual, a team and an engi competition at each hiding option (per
// person, per competition, hidden), plus a competition whose links have
// expired and one that shows names, draws them, records results and puts a
// match on each court. Then it walks every route the router mounts: each must
// be classified, and every answer a caller without the password can reach
// (200s and refusal bodies alike) must carry no roster name, zekken name or
// team member name of a hiding competition. The event stream is checked the
// same way, replay included. A reveal key unlocks exactly its own scope:
// names only, never a write or an organiser-only read; a key of the wrong
// kind, of another competition, of a hidden competition or past its expiry
// reveals nothing. The organiser's answers and stream keep the real names,
// so a masked label can never be saved back as a fighter's name. The Swiss
// and self-run refusals hold, and a tournament made self-run over hiding
// competitions asks nothing, changes nothing stored and shows every name.
//
// Shapes this test fixes ahead of the code (plan Design 2 and 5):
//   - X-Reveal-Key: <compId>:<key>[,<compId>:<key>...]
//   - POST /api/events/ticket answers {"ticket": "<opaque>"}; the stream is
//     opened as /api/events?ticket=<opaque>, and a spent, unknown or
//     expired ticket is 401.
//   - every masked payload carries "namesHidden":true.

import (
	"context"
	"crypto/hmac"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"net/url"
	"regexp"
	"sort"
	"strings"
	"sync/atomic"
	"testing"
	"testing/fstest"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/gitrgoliveira/bracket-creator/internal/domain"
	"github.com/gitrgoliveira/bracket-creator/internal/engine"
	"github.com/gitrgoliveira/bracket-creator/internal/resources"
	"github.com/gitrgoliveira/bracket-creator/internal/state"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

const leakPassword = "leak-main-pw"

// leakFarFuture and leakPast are RevealUntil values: links that stay open
// for the whole test, and links that expired long ago.
const (
	leakFarFuture = "2099-12-31T23:00"
	leakPast      = "2001-01-01T10:00"
)

// leakRouteClass is what a route is to a caller without the password.
type leakRouteClass int

const (
	// leakOrganiser routes answer 401 without the password, reveal key or not.
	leakOrganiser leakRouteClass = iota + 1
	// leakPublicVaries routes answer without the password and carry
	// competitor data, so the answer depends on the audience: masked for
	// the public, and never cached (no-store).
	leakPublicVaries
	// leakPublicFixed routes answer without the password and carry no
	// competitor name of their own (or carry free text the organiser typed,
	// shown as typed per E6), so no audience changes them.
	leakPublicFixed
	// leakStream routes are the event stream and its ticket, checked by
	// TestPublicNamesLeak's stream subtests rather than by the walk.
	leakStream
)

// leakRoutes classifies every route the tournament app mounts. A route the
// router mounts that is not here, or one named here the router does not
// mount, fails the test: a new route must be decided before it ships.
var leakRoutes = map[string]leakRouteClass{
	// The event stream.
	"GET /api/events":         leakStream,
	"POST /api/events/ticket": leakStream,

	// Public, no competitor data of their own.
	"GET /health":                       leakPublicFixed,
	"GET /api/auth-config":              leakPublicFixed,
	"GET /api/version":                  leakPublicFixed,
	"GET /api/time":                     leakPublicFixed,
	"GET /api/schedule/estimate":        leakPublicFixed,
	"GET /api/branding/logo":            leakPublicFixed,
	"HEAD /api/branding/logo":           leakPublicFixed,
	"GET /api/sponsors/:file":           leakPublicFixed,
	"POST /api/tournament/reset":        leakPublicFixed,
	"GET /api/tournament/announcement":  leakPublicFixed,
	"GET /api/tournament/announcements": leakPublicFixed,
	// Sign-up is self-run only (E5) and refused in an officiated tournament.
	"GET /api/register/competitions/:id":  leakPublicFixed,
	"POST /api/register/competitions/:id": leakPublicFixed,

	// Public reads that carry competitor data.
	"GET /api/viewer/tournament":                                    leakPublicVaries,
	"GET /api/viewer/competitions":                                  leakPublicVaries,
	"GET /api/viewer/competitions/:id":                              leakPublicVaries,
	"GET /api/viewer/court/:court/current":                          leakPublicVaries,
	"GET /api/viewer/court/:court/matches":                          leakPublicVaries,
	"GET /api/competitions/:id/competitor-status":                   leakPublicVaries,
	"GET /api/competitions/:id/league/standings":                    leakPublicVaries,
	"GET /api/competitions/:id/league-tiebreak/candidates":          leakPublicVaries,
	"GET /api/competitions/:id/swiss/standings":                     leakPublicVaries,
	"GET /api/competitions/:id/matches/:mid/kachinuki-roster":       leakPublicVaries,
	"GET /api/competitions/:id/teams/:tid/lineups/:round":           leakPublicVaries,
	"GET /api/competitions/:id/teams/:tid/match-lineups/:matchId":   leakPublicVaries,
	"GET /api/competitions/:id/teams/:tid/lineup-in-force/:matchId": leakPublicVaries,
	"POST /api/competitions/:id/teams/:tid/fielded-lineup/:matchId": leakPublicVaries,

	// Organiser only.
	"DELETE /api/announcements":                                          leakOrganiser,
	"DELETE /api/announcements/:id":                                      leakOrganiser,
	"DELETE /api/branding/logo":                                          leakOrganiser,
	"DELETE /api/competitions/:id":                                       leakOrganiser,
	"DELETE /api/competitions/:id/draw":                                  leakOrganiser,
	"DELETE /api/competitions/:id/league-tiebreak":                       leakOrganiser,
	"DELETE /api/competitions/:id/matches/:mid/daihyosen":                leakOrganiser,
	"DELETE /api/competitions/:id/matches/:mid/kachinuki-bout":           leakOrganiser,
	"DELETE /api/competitions/:id/overrides":                             leakOrganiser,
	"DELETE /api/competitions/:id/participants/:pid/checkin":             leakOrganiser,
	"DELETE /api/competitions/:id/teams/:tid/lineups/:round":             leakOrganiser,
	"DELETE /api/competitions/:id/teams/:tid/match-lineups/:matchId":     leakOrganiser,
	"DELETE /api/competitions/:id/teams/:tid/members/:memberId":          leakOrganiser,
	"DELETE /api/sponsors/:index":                                        leakOrganiser,
	"DELETE /api/tournament/backups/:name":                               leakOrganiser,
	"GET /api/competitions":                                              leakOrganiser,
	"GET /api/competitions/:id":                                          leakOrganiser,
	"GET /api/competitions/:id/backup":                                   leakOrganiser,
	"GET /api/competitions/:id/chusen-candidates":                        leakOrganiser,
	"GET /api/competitions/:id/draw-warnings":                            leakOrganiser,
	"GET /api/competitions/:id/export":                                   leakOrganiser,
	"GET /api/competitions/:id/export-results":                           leakOrganiser,
	"GET /api/competitions/:id/matches/:mid/history":                     leakOrganiser,
	"GET /api/competitions/:id/matches/:mid/start-check":                 leakOrganiser,
	"GET /api/competitions/:id/participants":                             leakOrganiser,
	"GET /api/competitions/:id/reveal-links":                             leakOrganiser,
	"GET /api/competitions/:id/reveal-links/:participantId":              leakOrganiser,
	"GET /api/competitions/:id/schedule/clashes":                         leakOrganiser,
	"GET /api/competitions/:id/schedule/estimate":                        leakOrganiser,
	"GET /api/competitions/:id/seeds":                                    leakOrganiser,
	"GET /api/competitions/:id/team-members":                             leakOrganiser,
	"GET /api/number-prefix-default":                                     leakOrganiser,
	"GET /api/schedule/calendar":                                         leakOrganiser,
	"GET /api/schedule/calendar/preview":                                 leakOrganiser,
	"GET /api/schedule/draft-estimate":                                   leakOrganiser,
	"GET /api/tournament":                                                leakOrganiser,
	"GET /api/tournament/backup":                                         leakOrganiser,
	"GET /api/tournament/backups":                                        leakOrganiser,
	"GET /api/tournament/backups/:name":                                  leakOrganiser,
	"POST /api/branding/logo":                                            leakOrganiser,
	"POST /api/competitions":                                             leakOrganiser,
	"POST /api/competitions/:id/bracket/quarantine":                      leakOrganiser,
	"POST /api/competitions/:id/competitor-status":                       leakOrganiser,
	"POST /api/competitions/:id/competitors/:pid/reinstate":              leakOrganiser,
	"POST /api/competitions/:id/complete":                                leakOrganiser,
	"POST /api/competitions/:id/generate-draw":                           leakOrganiser,
	"POST /api/competitions/:id/invalidate":                              leakOrganiser,
	"POST /api/competitions/:id/league-tiebreak":                         leakOrganiser,
	"POST /api/competitions/:id/matches/:mid/daihyosen":                  leakOrganiser,
	"POST /api/competitions/:id/matches/:mid/decision":                   leakOrganiser,
	"POST /api/competitions/:id/matches/:mid/reopen":                     leakOrganiser,
	"POST /api/competitions/:id/matches/:mid/requeue-blocker-and-reopen": leakOrganiser,
	"POST /api/competitions/:id/matches/:mid/revert-to-queue":            leakOrganiser,
	"POST /api/competitions/:id/matches/:mid/score-events":               leakOrganiser,
	"POST /api/competitions/:id/matches/bulk-score":                      leakOrganiser,
	"POST /api/competitions/:id/participants":                            leakOrganiser,
	"POST /api/competitions/:id/participants/checkin-bulk":               leakOrganiser,
	"POST /api/competitions/:id/pools/:poolId/tiebreak-round":            leakOrganiser,
	"POST /api/competitions/:id/start":                                   leakOrganiser,
	"POST /api/competitions/:id/swiss/generate-round":                    leakOrganiser,
	"POST /api/competitions/:id/teams/:tid/members":                      leakOrganiser,
	"POST /api/print/:type":                                              leakOrganiser,
	"POST /api/sponsors":                                                 leakOrganiser,
	"POST /api/tournament":                                               leakOrganiser,
	"POST /api/tournament/announce":                                      leakOrganiser,
	"POST /api/tournament/import":                                        leakOrganiser,
	"POST /api/tournament/restore":                                       leakOrganiser,
	"POST /api/tournament/restore/plan":                                  leakOrganiser,
	"PUT /api/competitions/:id":                                          leakOrganiser,
	"PUT /api/competitions/:id/awards":                                   leakOrganiser,
	"PUT /api/competitions/:id/matches/:mid/court":                       leakOrganiser,
	"PUT /api/competitions/:id/matches/:mid/quick-score":                 leakOrganiser,
	"PUT /api/competitions/:id/matches/:mid/score":                       leakOrganiser,
	"PUT /api/competitions/:id/matches/:mid/time":                        leakOrganiser,
	"PUT /api/competitions/:id/participants/:pid":                        leakOrganiser,
	"PUT /api/competitions/:id/participants/:pid/checkin":                leakOrganiser,
	"PUT /api/competitions/:id/pools/:poolId/override-rank":              leakOrganiser,
	"PUT /api/competitions/:id/seeds":                                    leakOrganiser,
	"PUT /api/competitions/:id/teams/:tid/lineups/:round":                leakOrganiser,
	"PUT /api/competitions/:id/teams/:tid/match-lineups/:matchId":        leakOrganiser,
	"PUT /api/competitions/:id/teams/:tid/members/:memberId":             leakOrganiser,
	"PUT /api/tournament":                                                leakOrganiser,
}

// leakSecret is one string a caller without the password must never read,
// with the competition and competitor it belongs to (for a team member,
// the team).
type leakSecret struct {
	text, comp, owner string
	zekken            bool // a zekken name, which not every payload carries
}

// names are the secrets of c that every full payload carries: its names,
// without the zekken names.
func (c *leakComp) names() []leakSecret {
	var out []leakSecret
	for _, s := range c.secrets {
		if !s.zekken {
			out = append(out, s)
		}
	}
	return out
}

type leakComp struct {
	id, kind, option, revealUntil, court string
	players                              []domain.Player
	members                              map[string][]domain.TeamMember // team id -> members
	secrets                              []leakSecret
	scoredMatch, runningMatch, poolName  string
	side                                 string // a side of scoredMatch: the team whose lineups the walk reads
	secret                               string // the stored reveal secret, hex
}

func (c *leakComp) hides() bool { return state.PublicNamesHidesNames(c.option) }

func (c *leakComp) isTeam() bool { return c.kind == "team" || c.kind == "kachinuki" }

type leakWorld struct {
	r     *gin.Engine
	hub   *Hub
	store *state.Store
	comps []*leakComp
	byID  map[string]*leakComp
	now   int64
}

// leakAddr gives every request its own client address, so the per-IP rate
// limiter never answers the walk's thousands of requests with a 429.
var leakAddr atomic.Uint32

func leakRequest(method, path string, headers map[string]string, body any) *http.Request {
	req := jsonReq(method, path, body)
	n := leakAddr.Add(1)
	req.RemoteAddr = fmt.Sprintf("10.%d.%d.%d:4242", (n>>16)&0xff, (n>>8)&0xff, n&0xff)
	for k, v := range headers {
		req.Header.Set(k, v)
	}
	return req
}

func (w *leakWorld) do(method, path string, headers map[string]string, body any) *httptest.ResponseRecorder {
	rec := httptest.NewRecorder()
	w.r.ServeHTTP(rec, leakRequest(method, path, headers, body))
	return rec
}

func (w *leakWorld) admin(method, path string, body any) *httptest.ResponseRecorder {
	return w.do(method, path, map[string]string{"X-Tournament-Password": leakPassword}, body)
}

func (w *leakWorld) stamp() int64 {
	w.now++
	return w.now
}

// leakToken is a name part no other string of the fixture contains.
func leakToken(compTag string, i int, part string) string {
	return fmt.Sprintf("Zq%s%d%s", compTag, i, part)
}

// buildLeakWorld seeds the officiated tournament the leak test walks.
func buildLeakWorld(t *testing.T) *leakWorld {
	t.Helper()
	gin.SetMode(gin.TestMode)
	store := newTempStore(t)
	courts := []string{"A", "B", "C", "D", "E", "F", "G", "H", "I", "J", "K", "L"}
	require.NoError(t, store.SaveTournament(&state.Tournament{
		Name: "Leak Taikai", Date: "01-06-2026", Venue: "Budokan", Courts: courts,
		Password: leakPassword, Mode: state.TournamentModeOfficiated,
	}))
	eng := engine.New(store)
	res := resources.NewResources(nil, fstest.MapFS{"web-mobile/index.html": {Data: []byte("<html><head></head><body>leak</body></html>")}})
	// History large enough that the replay below holds every event the
	// fixture makes.
	r, hub, limiter := NewRouterWithHub(store, eng, res, NewFileVerifier(store), NewHubWithHistory(4000), false)
	t.Cleanup(limiter.Close)
	t.Cleanup(hub.Close)
	w := &leakWorld{r: r, hub: hub, store: store, byID: map[string]*leakComp{}, now: time.Now().UnixMilli() - 600_000}

	type spec struct{ id, kind, option, revealUntil, tag string }
	var specs []spec
	for _, kind := range []string{"individual", "team", "engi"} {
		for _, option := range []string{state.PublicNamesPerson, state.PublicNamesCompetition, state.PublicNamesHidden} {
			ru := ""
			if state.PublicNamesHasLinks(option) {
				ru = leakFarFuture
			}
			specs = append(specs, spec{kind + "-" + option, kind, option, ru, strings.ToUpper(kind[:1] + option[:1])})
		}
	}
	specs = append(specs,
		spec{"individual-expired", "individual", state.PublicNamesCompetition, leakPast, "IX"},
		spec{"individual-shown", "individual", state.PublicNamesShown, "", "IS"},
		spec{"kachinuki-hidden", "kachinuki", state.PublicNamesHidden, "", "KH"},
	)
	for i, sp := range specs {
		c := &leakComp{id: sp.id, kind: sp.kind, option: sp.option, revealUntil: sp.revealUntil, court: courts[i], members: map[string][]domain.TeamMember{}}
		w.createLeakComp(t, c, sp.tag)
		w.comps = append(w.comps, c)
		w.byID[c.id] = c
	}

	// A hidden competition keeps the secret of an earlier option with links
	// (E3): a key derived from it must still reveal nothing.
	for _, c := range w.comps {
		stored, err := store.LoadCompetition(c.id)
		require.NoError(t, err)
		if c.option == state.PublicNamesHidden {
			stored.RevealSecret = strings.Repeat("5e", 32)
			require.NoError(t, store.SaveCompetition(stored))
		}
		c.secret = stored.RevealSecret
		if state.PublicNamesHasLinks(c.option) {
			require.Len(t, c.secret, 64, "%s mints its secret", c.id)
		}
	}

	// Every result write is pushed; wait until the last one is in the
	// stream's history so the replay holds them all.
	last := w.comps[len(w.comps)-1]
	require.Eventually(t, func() bool { return leakHubHolds(w.hub, last.scoredMatch) }, 3*time.Second, 10*time.Millisecond)
	return w
}

func (w *leakWorld) createLeakComp(t *testing.T, c *leakComp, tag string) {
	t.Helper()
	body := map[string]any{
		"id": c.id, "name": "Leak " + c.id, "courts": []string{c.court},
		"publicNames": c.option,
	}
	if c.revealUntil != "" {
		body["revealUntil"] = c.revealUntil
	}
	lower := strings.ToLower(tag)
	var players []map[string]any
	switch c.kind {
	case "individual":
		body["kind"], body["format"] = "individual", state.CompFormatMixed
		body["poolSize"], body["poolWinners"], body["withZekkenName"] = 3, 2, true
		for i := 1; i <= 6; i++ {
			players = append(players, map[string]any{"name": leakToken(tag, i, "name"), "dojo": fmt.Sprintf("Dojo %s %d", lower, i)})
		}
	case "team", "kachinuki":
		body["kind"], body["format"], body["teamSize"] = "team", state.CompFormatLeague, 3
		if c.kind == "kachinuki" {
			body["teamMatchType"] = state.TeamMatchTypeKachinuki
		}
		for i := 1; i <= 4; i++ {
			players = append(players, map[string]any{"name": fmt.Sprintf("Team %s %d", lower, i), "dojo": fmt.Sprintf("Dojo %s %d", lower, i)})
		}
	case "engi":
		body["kind"], body["format"], body["engi"], body["engiSolo"] = "individual", state.CompFormatKnockout, true, false
		for i := 1; i <= 4; i++ {
			players = append(players, map[string]any{
				"name": leakToken(tag, i, "aname") + " - " + leakToken(tag, i, "bname"),
				"dojo": fmt.Sprintf("Dojo %s %d", lower, i),
			})
		}
	}
	body["players"] = players
	rec := w.admin(http.MethodPost, "/api/competitions", body)
	require.Equal(t, http.StatusCreated, rec.Code, "%s: %s", c.id, rec.Body.String())

	if c.kind == "individual" {
		// Each competitor's zekken name, distinct from the name. The
		// competition keeps a zekken column, so it is read with one.
		roster, err := w.store.LoadParticipants(c.id, true)
		require.NoError(t, err)
		for i := range roster {
			roster[i].DisplayName = strings.ToUpper(leakToken(tag, i+1, "zek"))
		}
		require.NoError(t, w.store.SaveParticipants(c.id, roster))
	}
	if c.isTeam() {
		teams, err := w.store.LoadParticipants(c.id, false)
		require.NoError(t, err)
		for ti, team := range teams {
			for mi, pos := range []string{"senpo", "chuken", "taisho"} {
				rec := w.admin(http.MethodPost, "/api/competitions/"+c.id+"/teams/"+team.ID+"/members", map[string]any{"name": leakToken(tag, ti*10+mi+1, pos)})
				require.Equal(t, http.StatusCreated, rec.Code, rec.Body.String())
				var m domain.TeamMember
				require.NoError(t, json.Unmarshal(rec.Body.Bytes(), &m))
				c.members[team.ID] = append(c.members[team.ID], m)
			}
		}
	}

	rec = w.admin(http.MethodPost, "/api/competitions/"+c.id+"/start", nil)
	require.Equal(t, http.StatusOK, rec.Code, "%s: %s", c.id, rec.Body.String())

	// Only the individual competitions here keep a zekken column.
	players2, err := w.store.LoadParticipants(c.id, c.kind == "individual")
	require.NoError(t, err)
	c.players = players2
	for _, p := range c.players {
		switch c.kind {
		case "individual":
			c.secrets = append(c.secrets, leakSecret{text: p.Name, comp: c.id, owner: p.ID}, leakSecret{text: p.DisplayName, comp: c.id, owner: p.ID, zekken: true})
		case "engi":
			a, b, ok := strings.Cut(p.Name, " - ")
			require.True(t, ok, p.Name)
			c.secrets = append(c.secrets, leakSecret{text: a, comp: c.id, owner: p.ID}, leakSecret{text: b, comp: c.id, owner: p.ID})
		case "team", "kachinuki":
			for _, m := range c.members[p.ID] {
				c.secrets = append(c.secrets, leakSecret{text: m.Name, comp: c.id, owner: p.ID})
			}
		}
	}

	switch c.kind {
	case "individual":
		pools, err := w.store.LoadPools(c.id)
		require.NoError(t, err)
		require.NotEmpty(t, pools)
		c.poolName = pools[0].PoolName
		matches, err := w.store.LoadPoolMatches(c.id)
		require.NoError(t, err)
		require.GreaterOrEqual(t, len(matches), 4)
		w.scoreMatch(t, c, matches[0], true)
		w.scoreMatch(t, c, matches[1], false)
		// A withdrawal, so the competitor-status read and its event carry a
		// competitor of this competition.
		lastM := matches[len(matches)-1]
		rec := w.admin(http.MethodPost, "/api/competitions/"+c.id+"/competitor-status", map[string]any{
			"playerId": lastM.SideBID, "eligible": false, "reason": "kiken-injury: knee", "matchId": lastM.ID,
		})
		require.Equal(t, http.StatusOK, rec.Code, rec.Body.String())
	case "team", "kachinuki":
		matches, err := w.store.LoadPoolMatches(c.id)
		require.NoError(t, err)
		require.NotEmpty(t, matches)
		m := matches[0]
		c.poolName = "Pool A"
		for _, teamID := range []string{m.SideAID, m.SideBID} {
			members := c.members[teamID]
			require.Len(t, members, 3)
			rec := w.admin(http.MethodPut, "/api/competitions/"+c.id+"/teams/"+teamID+"/match-lineups/"+url.PathEscape(m.ID), map[string]any{
				"positions": map[string]string{"1": members[0].Name, "2": members[1].Name, "3": members[2].Name},
				"memberIds": map[string]string{"1": members[0].ID, "2": members[1].ID, "3": members[2].ID},
			})
			require.Equal(t, http.StatusOK, rec.Code, rec.Body.String())
		}
		w.scoreMatch(t, c, m, false)
		c.scoredMatch, c.side = m.ID, m.SideAID
	case "engi":
		bracket, err := w.store.LoadBracket(c.id)
		require.NoError(t, err)
		require.NotNil(t, bracket)
		require.NotEmpty(t, bracket.Rounds)
		bm := bracket.Rounds[0][0]
		w.scoreMatch(t, c, state.MatchResult{ID: bm.ID, SideA: bm.SideA, SideB: bm.SideB, SideAID: bm.SideAID, SideBID: bm.SideBID}, false)
		c.scoredMatch, c.side = bm.ID, bm.SideAID
		c.poolName = "Pool A"
	}
}

// scoreMatch finishes m (finish) or starts it on its court (!finish) as the
// organiser, so the draw, the standings and the stream carry its sides.
func (w *leakWorld) scoreMatch(t *testing.T, c *leakComp, m state.MatchResult, finish bool) {
	t.Helper()
	body := map[string]any{
		"id": m.ID, "sideA": m.SideA, "sideB": m.SideB, "sideAId": m.SideAID, "sideBId": m.SideBID,
		"modifiedAt": w.stamp(),
	}
	if finish {
		body["status"] = string(state.MatchStatusCompleted)
		body["winner"], body["winnerId"] = m.SideA, m.SideAID
		body["ipponsA"], body["ipponsB"] = []string{"M", "K"}, []string{}
		c.scoredMatch, c.side = m.ID, m.SideAID
	} else {
		body["status"] = string(state.MatchStatusRunning)
		body["startOnly"] = true
		c.runningMatch = m.ID
	}
	rec := w.admin(http.MethodPut, "/api/competitions/"+c.id+"/matches/"+url.PathEscape(m.ID)+"/score", body)
	require.Equal(t, http.StatusOK, rec.Code, "%s %s: %s", c.id, m.ID, rec.Body.String())
}

// leakHubHolds reports whether the stream's history holds an event whose
// marshalled payload contains want.
func leakHubHolds(h *Hub, want string) bool {
	ch, entries, _, _ := h.subscribeWithReplay(1)
	if ch == nil {
		return false
	}
	defer h.Unsubscribe(ch)
	for _, e := range entries {
		if strings.Contains(e.payload, want) {
			return true
		}
	}
	return false
}

func (w *leakWorld) allSecrets() []leakSecret {
	var out []leakSecret
	for _, c := range w.comps {
		if c.hides() {
			out = append(out, c.secrets...)
		}
	}
	return out
}

// leaks lists the secrets body carries that allowed does not let through.
func leaks(body string, secrets []leakSecret, allowed func(leakSecret) bool) []string {
	var out []string
	for _, s := range secrets {
		if allowed != nil && allowed(s) {
			continue
		}
		if strings.Contains(body, s.text) {
			out = append(out, fmt.Sprintf("%s (%s, %s)", s.text, s.comp, s.owner))
		}
	}
	return out
}

// --- reveal keys ---------------------------------------------------------

// leakHMAC derives a key the way the state package does, for any label, so
// the test can make keys of the wrong kind.
func leakHMAC(secretHex, label string) string {
	secret, err := hex.DecodeString(secretHex)
	if err != nil {
		return "bad"
	}
	mac := hmac.New(sha256.New, secret)
	mac.Write([]byte(label))
	return hex.EncodeToString(mac.Sum(nil))
}

func revealHeader(pairs ...string) map[string]string {
	return map[string]string{"X-Reveal-Key": strings.Join(pairs, ",")}
}

func revealPair(compID, key string) string { return compID + ":" + key }

func (w *leakWorld) competitionKey(t *testing.T, id string) string {
	t.Helper()
	stored, err := w.store.LoadCompetition(id)
	require.NoError(t, err)
	key := state.CompetitionRevealKey(stored)
	require.NotEmpty(t, key, id)
	return key
}

func (w *leakWorld) competitorKey(t *testing.T, id, participantID string) string {
	t.Helper()
	stored, err := w.store.LoadCompetition(id)
	require.NoError(t, err)
	key := state.CompetitorRevealKey(stored, participantID)
	require.NotEmpty(t, key, id)
	return key
}

// leakAudience is one caller of the walk: the headers it sends and the
// secrets it may read.
type leakAudience struct {
	name    string
	headers map[string]string
	allowed func(leakSecret) bool
}

func (w *leakWorld) audiences(t *testing.T) []leakAudience {
	t.Helper()
	ip, tp, ep := w.byID["individual-person"], w.byID["team-person"], w.byID["engi-person"]
	ipKey := w.competitorKey(t, ip.id, ip.players[0].ID)
	tpKey := w.competitorKey(t, tp.id, tp.players[0].ID)
	epKey := w.competitorKey(t, ep.id, ep.players[0].ID)
	icKey := w.competitionKey(t, "individual-competition")
	tcKey := w.competitionKey(t, "team-competition")
	ecKey := w.competitionKey(t, "engi-competition")
	only := func(comp, owner string) func(leakSecret) bool {
		return func(s leakSecret) bool { return s.comp == comp && (owner == "" || s.owner == owner) }
	}
	either := func(fs ...func(leakSecret) bool) func(leakSecret) bool {
		return func(s leakSecret) bool {
			for _, f := range fs {
				if f(s) {
					return true
				}
			}
			return false
		}
	}
	return []leakAudience{
		{name: "competition key", headers: revealHeader(revealPair("individual-competition", icKey)), allowed: only("individual-competition", "")},
		{name: "team competition key", headers: revealHeader(revealPair("team-competition", tcKey)), allowed: only("team-competition", "")},
		{name: "engi competition key", headers: revealHeader(revealPair("engi-competition", ecKey)), allowed: only("engi-competition", "")},
		{name: "competitor key", headers: revealHeader(revealPair(ip.id, ipKey)), allowed: only(ip.id, ip.players[0].ID)},
		{name: "team competitor key", headers: revealHeader(revealPair(tp.id, tpKey)), allowed: only(tp.id, tp.players[0].ID)},
		{name: "engi competitor key", headers: revealHeader(revealPair(ep.id, epKey)), allowed: only(ep.id, ep.players[0].ID)},
		{name: "keys that reveal nothing", headers: revealHeader(w.badKeys(t)...)},
		{
			name: "every valid key at once",
			headers: revealHeader(
				revealPair("individual-competition", icKey), revealPair("team-competition", tcKey), revealPair("engi-competition", ecKey),
				revealPair(ip.id, ipKey), revealPair(tp.id, tpKey), revealPair(ep.id, epKey),
			),
			allowed: either(only("individual-competition", ""), only("team-competition", ""), only("engi-competition", ""),
				only(ip.id, ip.players[0].ID), only(tp.id, tp.players[0].ID), only(ep.id, ep.players[0].ID)),
		},
	}
}

// badKeys are keys a judge must drop: the wrong kind for the competition's
// option, a key of one competition sent under another's id, a key of a
// hidden competition, a key past its expiry, and malformed ones.
func (w *leakWorld) badKeys(t *testing.T) []string {
	t.Helper()
	ic, ip, ih, ix := w.byID["individual-competition"], w.byID["individual-person"], w.byID["individual-hidden"], w.byID["individual-expired"]
	return []string{
		// a competitor key for a per-competition competition
		revealPair(ic.id, leakHMAC(ic.secret, "competitor:"+ic.players[0].ID)),
		// a competition key for a per-person competition
		revealPair(ip.id, leakHMAC(ip.secret, "competition")),
		// a valid competition key sent under another competition's id
		revealPair("team-competition", w.competitionKey(t, ic.id)),
		// a valid competitor key sent under another competition's id
		revealPair("team-person", w.competitorKey(t, ip.id, ip.players[0].ID)),
		// keys of a hidden competition that still holds a secret
		revealPair(ih.id, leakHMAC(ih.secret, "competition")),
		revealPair(ih.id, leakHMAC(ih.secret, "competitor:"+ih.players[0].ID)),
		// the real key of a competition whose links expired
		revealPair(ix.id, leakHMAC(ix.secret, "competition")),
		// malformed
		revealPair(ic.id, "not-hex"),
		revealPair(ic.id, ""),
		ic.id,
		":" + w.competitionKey(t, ic.id),
	}
}

// --- the walk ------------------------------------------------------------

// leakParams are the values a route's parameters take for one competition.
func (c *leakComp) leakParams() map[string]string {
	team, member := c.side, "none"
	if ms := c.members[team]; len(ms) > 0 {
		member = ms[0].ID
	}
	match := url.PathEscape(c.scoredMatch)
	return map[string]string{
		"id": c.id, "mid": match, "matchId": match, "tid": team, "pid": team, "participantId": team,
		"memberId": member, "round": "0", "poolId": url.PathEscape(c.poolName), "court": c.court,
		"name": "none.zip", "index": "0", "file": "none.png", "type": "pools",
	}
}

// bogusParams keeps the competition and gives every other parameter a value
// nothing holds, so the walk reads the refusal bodies too.
func (c *leakComp) bogusParams() map[string]string {
	p := c.leakParams()
	for k := range p {
		if k != "id" {
			p[k] = "nope-" + k
		}
	}
	return p
}

func fillRoute(path string, params map[string]string) string {
	segs := strings.Split(path, "/")
	for i, s := range segs {
		if strings.HasPrefix(s, ":") {
			segs[i] = params[s[1:]]
		}
	}
	return strings.Join(segs, "/")
}

type leakCall struct {
	method, path string
	class        leakRouteClass
}

// leakCalls is every request the walk makes for one audience: each public
// route for each competition with real and with unknown parameters, each
// organiser route both ways, and the page itself.
func (w *leakWorld) leakCalls() []leakCall {
	var calls []leakCall
	routes := w.r.Routes()
	sort.Slice(routes, func(i, j int) bool { return routes[i].Method+routes[i].Path < routes[j].Method+routes[j].Path })
	for _, ri := range routes {
		class := leakRoutes[ri.Method+" "+ri.Path]
		if class == 0 || class == leakStream {
			continue
		}
		comps := w.comps
		if !strings.Contains(ri.Path, ":") || class == leakOrganiser {
			comps = w.comps[:1]
		}
		for _, c := range comps {
			for _, params := range []map[string]string{c.leakParams(), c.bogusParams()} {
				calls = append(calls, leakCall{method: ri.Method, path: fillRoute(ri.Path, params), class: class})
			}
		}
	}
	// The page itself, with its link-preview tags, a reveal link's page, and
	// an unknown API path.
	for _, p := range []string{"/", "/index.html", "/reveal/individual-competition", "/api/nope"} {
		calls = append(calls, leakCall{method: http.MethodGet, path: p, class: leakPublicFixed})
	}
	return calls
}

func (w *leakWorld) walk(t *testing.T, aud leakAudience) {
	t.Helper()
	secrets := w.allSecrets()
	for _, call := range w.leakCalls() {
		var body any
		if call.method == http.MethodPost || call.method == http.MethodPut {
			body = map[string]any{}
		}
		rec := w.do(call.method, call.path, aud.headers, body)
		require.NotEqual(t, http.StatusTooManyRequests, rec.Code, "%s: the walk must not be rate limited", call.path)
		if found := leaks(rec.Body.String(), secrets, aud.allowed); len(found) > 0 {
			t.Errorf("[%s] %s %s answered %d carrying %s", aud.name, call.method, call.path, rec.Code, strings.Join(found, ", "))
		}
		switch call.class {
		case leakOrganiser:
			if rec.Code != http.StatusUnauthorized {
				t.Errorf("[%s] %s %s answered %d: a reveal key unlocks no organiser route and no write", aud.name, call.method, call.path, rec.Code)
			}
		case leakPublicVaries:
			if rec.Code == http.StatusOK && rec.Header().Get("Cache-Control") != "no-store" {
				t.Errorf("[%s] %s %s varies by audience but is cacheable (Cache-Control %q)", aud.name, call.method, call.path, rec.Header().Get("Cache-Control"))
			}
		}
	}
}

func (w *leakWorld) viewerRead(t *testing.T, compID string, headers map[string]string) string {
	t.Helper()
	rec := w.do(http.MethodGet, "/api/viewer/competitions/"+compID, headers, nil)
	require.Equal(t, http.StatusOK, rec.Code, rec.Body.String())
	return rec.Body.String()
}

// --- the event stream ----------------------------------------------------

type leakStreamConn struct {
	w      *mockResponseWriter
	cancel func()
	done   chan struct{}
}

// close ends the request and answers its status and body. A stream the
// server refused has ended by itself.
func (s *leakStreamConn) close(t *testing.T) (int, string) {
	t.Helper()
	s.cancel()
	close(s.w.closeChan)
	select {
	case <-s.done:
	case <-time.After(2 * time.Second):
		t.Fatal("the event stream did not end")
	}
	return s.w.Code, s.w.BodyString()
}

// openStream opens /api/events with the query given (a ticket, a replay
// point) and returns once the server has written something: the stream's
// opening, or its refusal.
func (w *leakWorld) openStream(t *testing.T, query string) *leakStreamConn {
	t.Helper()
	ctx, cancel := context.WithCancel(context.Background())
	req := leakRequest(http.MethodGet, "/api/events"+query, nil, nil).WithContext(ctx)
	conn := &leakStreamConn{w: &mockResponseWriter{ResponseRecorder: httptest.NewRecorder(), closeChan: make(chan bool)}, cancel: cancel, done: make(chan struct{})}
	go func() {
		w.r.ServeHTTP(conn.w, req)
		close(conn.done)
	}()
	require.Eventually(t, func() bool { return conn.w.BodyString() != "" }, 2*time.Second, 5*time.Millisecond, "the event stream answered nothing")
	return conn
}

// settle broadcasts a marker and waits for each stream to show it: every
// replayed event was written before it.
func (w *leakWorld) settle(t *testing.T, conns ...*leakStreamConn) {
	t.Helper()
	marker := fmt.Sprintf("leak-marker-%d", w.stamp())
	w.hub.Broadcast(EventTournamentUpdated, map[string]string{"marker": marker})
	for _, c := range conns {
		waitForFrame(t, c.w, marker)
	}
}

func (w *leakWorld) ticket(t *testing.T, headers map[string]string) string {
	t.Helper()
	rec := w.do(http.MethodPost, "/api/events/ticket", headers, nil)
	require.Less(t, rec.Code, 300, "a ticket: %d %s", rec.Code, rec.Body.String())
	var body struct {
		Ticket string `json:"ticket"`
	}
	require.NoError(t, json.Unmarshal(rec.Body.Bytes(), &body), rec.Body.String())
	require.NotEmpty(t, body.Ticket, rec.Body.String())
	return body.Ticket
}

// leakNumberRe finds the competitor numbers a payload carries.
var leakNumberRe = regexp.MustCompile(`"number":"([^"]+)"`)

// --- tests ---------------------------------------------------------------

func TestPublicNamesLeak(t *testing.T) {
	w := buildLeakWorld(t)

	t.Run("every mounted route is classified", func(t *testing.T) {
		mounted := map[string]bool{}
		for _, ri := range w.r.Routes() {
			key := ri.Method + " " + ri.Path
			mounted[key] = true
			if _, ok := leakRoutes[key]; !ok {
				t.Errorf("route %s is not classified in leakRoutes: decide whether a caller without the password may reach it", key)
			}
		}
		for key := range leakRoutes {
			if !mounted[key] {
				t.Errorf("leakRoutes names %s, which the router does not mount", key)
			}
		}
	})

	t.Run("no answer without the password carries a hidden name", func(t *testing.T) {
		w.walk(t, leakAudience{name: "public"})
	})

	t.Run("a reveal key unlocks its own names and nothing else", func(t *testing.T) {
		for _, aud := range w.audiences(t) {
			t.Run(aud.name, func(t *testing.T) { w.walk(t, aud) })
		}
	})

	t.Run("the masked view shows numbers and says so", func(t *testing.T) {
		for _, c := range w.comps {
			if !c.hides() {
				continue
			}
			body := w.viewerRead(t, c.id, nil)
			assert.Contains(t, body, `"namesHidden":true`, c.id)
			// The numbers the organiser's read carries, every one shown to the
			// public too.
			full := w.viewerRead(t, c.id, map[string]string{"X-Tournament-Password": leakPassword})
			numbers := leakNumberRe.FindAllStringSubmatch(full, -1)
			require.NotEmpty(t, numbers, "%s numbers its competitors at the draw", c.id)
			for _, n := range numbers {
				assert.Contains(t, body, `"`+n[1]+`"`, "%s shows competitor number %s", c.id, n[1])
			}
		}
	})

	t.Run("each reveal key reveals what it unlocks", func(t *testing.T) {
		for _, id := range []string{"individual-competition", "team-competition", "engi-competition"} {
			body := w.viewerRead(t, id, revealHeader(revealPair(id, w.competitionKey(t, id))))
			for _, s := range w.byID[id].names() {
				assert.Contains(t, body, s.text, "the competition link of %s reveals every name", id)
			}
		}
		for _, id := range []string{"individual-person", "team-person", "engi-person"} {
			c := w.byID[id]
			own := c.players[0].ID
			body := w.viewerRead(t, id, revealHeader(revealPair(id, w.competitorKey(t, id, own))))
			for _, s := range c.secrets {
				if s.owner == own && !s.zekken {
					assert.Contains(t, body, s.text, "%s's own link reveals their name (a team's, its members')", own)
				} else if s.owner != own {
					assert.NotContains(t, body, s.text, "%s's link reveals no one else", own)
				}
			}
		}
	})

	t.Run("a key of the wrong kind, of another competition, of a hidden one or past its expiry reveals nothing", func(t *testing.T) {
		for _, pair := range w.badKeys(t) {
			compID, _, _ := strings.Cut(pair, ":")
			if _, ok := w.byID[compID]; !ok {
				compID = "individual-competition"
			}
			body := w.viewerRead(t, compID, revealHeader(pair))
			if found := leaks(body, w.allSecrets(), nil); len(found) > 0 {
				t.Errorf("key %q revealed %s", pair, strings.Join(found, ", "))
			}
		}
	})

	t.Run("a key that worked stops at its expiry", func(t *testing.T) {
		c := w.byID["individual-competition"]
		key := w.competitionKey(t, c.id)
		require.Contains(t, w.viewerRead(t, c.id, revealHeader(revealPair(c.id, key))), c.secrets[0].text)
		stored, err := w.store.LoadCompetition(c.id)
		require.NoError(t, err)
		stored.RevealUntil = leakPast
		require.NoError(t, w.store.SaveCompetition(stored))
		defer func() {
			stored.RevealUntil = leakFarFuture
			require.NoError(t, w.store.SaveCompetition(stored))
		}()
		body := w.viewerRead(t, c.id, revealHeader(revealPair(c.id, key)))
		assert.Empty(t, leaks(body, c.secrets, nil), "the same key past its expiry reveals nothing")
	})

	t.Run("a wrong password on a public read is refused, never answered masked", func(t *testing.T) {
		rec := w.do(http.MethodGet, "/api/viewer/competitions/individual-hidden", map[string]string{"X-Tournament-Password": "stale-pw"}, nil)
		assert.Equal(t, http.StatusUnauthorized, rec.Code, rec.Body.String())
		assert.Empty(t, leaks(rec.Body.String(), w.allSecrets(), nil))
	})

	t.Run("the organiser's answers keep the real names", func(t *testing.T) {
		headers := map[string]string{"X-Tournament-Password": leakPassword}
		for _, c := range w.comps {
			body := w.viewerRead(t, c.id, headers)
			assert.NotContains(t, body, `"namesHidden":true`, c.id)
			for _, s := range c.names() {
				assert.Contains(t, body, s.text, "the organiser's viewer read of %s", c.id)
			}
			if !c.isTeam() {
				rec := w.admin(http.MethodGet, "/api/competitions/"+c.id+"/participants", nil)
				require.Equal(t, http.StatusOK, rec.Code, rec.Body.String())
				for _, s := range c.secrets {
					assert.Contains(t, rec.Body.String(), s.text, "the organiser's roster of %s", c.id)
				}
			}
			if c.runningMatch != "" {
				rec := w.do(http.MethodGet, "/api/viewer/court/"+c.court+"/current", headers, nil)
				require.Equal(t, http.StatusOK, rec.Code, rec.Body.String())
				assert.NotContains(t, rec.Body.String(), `"namesHidden":true`, "the court console's feed for %s", c.id)
			}
		}
	})

	t.Run("the shown competition still shows its names to the public", func(t *testing.T) {
		body := w.viewerRead(t, "individual-shown", nil)
		for _, s := range w.byID["individual-shown"].names() {
			assert.Contains(t, body, s.text)
		}
	})

	t.Run("the public stream's replay and live events carry no hidden name", func(t *testing.T) {
		conn := w.openStream(t, "?lastEventId=1")
		w.settle(t, conn)
		// A result recorded while the stream is open: the running match of a
		// hidden competition is finished.
		c := w.byID["individual-hidden"]
		matches, err := w.store.LoadPoolMatches(c.id)
		require.NoError(t, err)
		var live state.MatchResult
		for _, m := range matches {
			if m.ID == c.runningMatch {
				live = m
			}
		}
		require.NotEmpty(t, live.ID)
		w.scoreMatch(t, c, live, true)
		require.Eventually(t, func() bool { return leakHubHolds(w.hub, live.ID) }, 3*time.Second, 10*time.Millisecond)
		w.settle(t, conn)
		code, body := conn.close(t)
		require.Equal(t, http.StatusOK, code)
		assert.NotContains(t, body, "resync_required", "the replay covered the whole history")
		assert.Contains(t, body, live.ID, "the live result reached the public stream")
		if found := leaks(body, w.allSecrets(), nil); len(found) > 0 {
			t.Errorf("the public stream carried %s", strings.Join(found, ", "))
		}
	})

	t.Run("the organiser's stream keeps the real fighter names", func(t *testing.T) {
		tk := w.ticket(t, map[string]string{"X-Tournament-Password": leakPassword})
		conn := w.openStream(t, "?lastEventId=1&ticket="+url.QueryEscape(tk))
		w.settle(t, conn)
		_, body := conn.close(t)
		carries := func(c *leakComp) bool {
			for _, p := range c.players {
				if strings.Contains(body, p.Name) {
					return true
				}
			}
			return false
		}
		for _, id := range []string{"individual-person", "individual-competition", "individual-hidden", "engi-hidden"} {
			assert.True(t, carries(w.byID[id]), "the organiser's stream carries the fighter names of %s: a masked label would reach the team editor", id)
		}
		assert.NotContains(t, body, `"namesHidden":true`)
	})

	t.Run("a reveal ticket's stream reveals its own names only", func(t *testing.T) {
		id := "individual-competition"
		tk := w.ticket(t, revealHeader(revealPair(id, w.competitionKey(t, id))))
		conn := w.openStream(t, "?lastEventId=1&ticket="+url.QueryEscape(tk))
		w.settle(t, conn)
		_, body := conn.close(t)
		if found := leaks(body, w.allSecrets(), func(s leakSecret) bool { return s.comp == id }); len(found) > 0 {
			t.Errorf("the reveal stream carried %s", strings.Join(found, ", "))
		}
		revealed := false
		for _, s := range w.byID[id].names() {
			revealed = revealed || strings.Contains(body, s.text)
		}
		assert.True(t, revealed, "the competition link's stream shows that competition's names")
	})

	t.Run("a spent, unknown or wrongly asked ticket is refused, never downgraded", func(t *testing.T) {
		tk := w.ticket(t, map[string]string{"X-Tournament-Password": leakPassword})
		code, _ := w.openStream(t, "?ticket="+url.QueryEscape(tk)).close(t)
		require.Equal(t, http.StatusOK, code, "the ticket's first use opens the stream")
		for name, query := range map[string]string{
			"spent":   "?ticket=" + url.QueryEscape(tk),
			"unknown": "?ticket=not-a-ticket",
		} {
			code, body := w.openStream(t, query).close(t)
			assert.Equal(t, http.StatusUnauthorized, code, "a %s ticket: %s", name, body)
			assert.NotContains(t, body, ": open", "a %s ticket opens no stream, not even a public one", name)
		}
		rec := w.do(http.MethodPost, "/api/events/ticket", map[string]string{"X-Tournament-Password": "stale-pw"}, nil)
		assert.Equal(t, http.StatusUnauthorized, rec.Code, "a ticket asked with a wrong password: %s", rec.Body.String())
	})
}

// The refusals that keep the option out of the format and the mode it does
// not cover hold for every hiding option.
func TestPublicNamesLeak_SwissAndSelfRunRefusals(t *testing.T) {
	for _, option := range []string{state.PublicNamesPerson, state.PublicNamesCompetition, state.PublicNamesHidden} {
		t.Run(option, func(t *testing.T) {
			f := newPublicNamesFixture(t, state.TournamentModeOfficiated)
			w := f.admin(http.MethodPost, "/api/competitions", createBody("Swiss", state.CompFormatSwiss, map[string]any{"publicNames": option}))
			assert.Equal(t, http.StatusBadRequest, w.Code, w.Body.String())
			assert.Equal(t, codePublicNamesSwiss, codeOf(t, w))

			s := newPublicNamesFixture(t, state.TournamentModeSelfRun)
			w = s.admin(http.MethodPost, "/api/competitions", createBody("Self", state.CompFormatKnockout, map[string]any{"publicNames": option}))
			assert.Equal(t, http.StatusConflict, w.Code, w.Body.String())
			assert.Equal(t, codePublicNamesSelfRun, codeOf(t, w))
		})
	}
}

// A tournament made self-run over competitions that hide names asks nothing
// and changes nothing stored, and the public pages show every name.
func TestPublicNamesLeak_SelfRunOverHidingCompetitionsShowsNames(t *testing.T) {
	f := newPublicNamesFixture(t, "")
	names := map[string][]string{}
	for i, option := range []string{state.PublicNamesPerson, state.PublicNamesCompetition, state.PublicNamesHidden} {
		id := fmt.Sprintf("c%d", i+1)
		f.saveComp(t, &state.Competition{ID: id, Name: "Kata " + id, Kind: "individual", Format: state.CompFormatKnockout, PublicNames: option, RevealSecret: strings.Repeat("ab", 32)})
		var players []domain.Player
		for j := 1; j <= 3; j++ {
			n := leakToken("SW"+id, j, "name")
			players = append(players, domain.Player{Name: n, Dojo: fmt.Sprintf("Dojo %d", j)})
			names[id] = append(names[id], n)
		}
		require.NoError(t, f.store.SaveParticipants(id, players))
	}
	body := map[string]any{
		"name": "New", "date": "01-06-2026", "courts": []string{"A"}, "password": publicNamesPassword,
		"mode": state.TournamentModeSelfRun,
	}
	w := f.send(http.MethodPost, "/api/tournament", "", body)
	require.Less(t, w.Code, 300, w.Body.String())

	for id, ns := range names {
		assert.NotEqual(t, state.PublicNamesShown, f.comp(t, id).PublicNames, "the stored setting is untouched")
		rec := f.send(http.MethodGet, "/api/viewer/competitions/"+id, "", nil)
		require.Equal(t, http.StatusOK, rec.Code, rec.Body.String())
		assert.NotContains(t, rec.Body.String(), `"namesHidden":true`)
		for _, n := range ns {
			assert.Contains(t, rec.Body.String(), n, "%s shows its names to the public again", id)
		}
	}
	rec := f.send(http.MethodGet, "/api/viewer/competitions", "", nil)
	require.Equal(t, http.StatusOK, rec.Code, rec.Body.String())
	assert.NotContains(t, rec.Body.String(), `"namesHidden":true`)
}

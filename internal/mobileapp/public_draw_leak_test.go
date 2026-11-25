package mobileapp

// The draw leak probe (bc-drpb), the gate for keeping a draft draw off the
// public pages until it is published. In an officiated tournament it seeds a
// draft of each shape a draw takes (pools, knockout, league, Swiss, a team
// league, a kachinuki team league and a competition that hides names), each
// left hidden, plus a draft that was published and one that was started.
// Every competition carries a number prefix no other string of the fixture
// holds, so a competitor number from a hidden draw is found wherever it lands.
//
// Then it walks every leakPublicVaries route of the route table for every
// competition, as the public and as the holder of a reveal link, and fails on
// any number from a hidden draw; for each hidden draft it compares each answer
// with the same competition's answer before its draw (taken after the draw
// was made once and discarded, so the match ids the routes are asked about are
// the draw's own), which a pool, a match id, a bracket, a status naming a
// match, a lineup saved for a match or the draw order would change. The event
// stream is checked the same way: every catalogue event that carries a draw,
// broadcast for each hidden draft, reaches the public and the reveal streams
// with no data, live and replayed, a court move and a time move of a hidden
// draft's match among them, and a published draft's event is withheld again
// on a replay once the draft is hidden again.

import (
	"encoding/json"
	"fmt"
	"net/http"
	"net/url"
	"regexp"
	"sort"
	"strings"
	"testing"
	"testing/fstest"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/gitrgoliveira/bracket-creator/internal/domain"
	"github.com/gitrgoliveira/bracket-creator/internal/engine"
	"github.com/gitrgoliveira/bracket-creator/internal/resources"
	"github.com/gitrgoliveira/bracket-creator/internal/state"
)

// The states a draft is left in.
const (
	pdlHidden    = "hidden"
	pdlPublished = "published"
	pdlStarted   = "started"
)

// pdlMovedTime is the time the probe moves a hidden draft's match to: a
// string no other answer of the fixture carries.
const pdlMovedTime = "2026-06-01T13:47"

type pdlComp struct {
	id, kind, format, matchType, option, state, court, prefix string
	players                                                   int
	match, side                                               string // a match of the draw with both sides known, and one of its sides
	members                                                   map[string][]domain.TeamMember
	// before is the public's answer to every route before the draw, per
	// audience, keyed by request.
	before map[string]map[string]pdlAnswer
}

func (c *pdlComp) isTeam() bool { return c.kind == "team" }

// numberRe finds a competitor number of c: its prefix followed by a digit.
// The prefix alone (the config's numberPrefix) is always followed by a quote.
func (c *pdlComp) numberRe() *regexp.Regexp { return regexp.MustCompile(c.prefix + `\d`) }

func (c *pdlComp) params() map[string]string {
	m := url.PathEscape(c.match)
	return map[string]string{"id": c.id, "mid": m, "matchId": m, "tid": c.side, "round": "0", "court": c.court}
}

func (c *pdlComp) bogusParams() map[string]string {
	p := c.params()
	for k := range p {
		if k != "id" {
			p[k] = "nope-" + k
		}
	}
	return p
}

type pdlAnswer struct {
	code         int
	body         string
	view         any // what is compared with the answer before the draw
	cacheControl string
}

type pdlAudience struct {
	name    string
	headers map[string]string
}

type pdlWorld struct {
	lw        *leakWorld
	comps     []*pdlComp
	byID      map[string]*pdlComp
	audiences []pdlAudience
}

// pdlPublicRoutes is every route the route table classifies as public and
// varying by audience, sorted.
func pdlPublicRoutes() []string {
	var out []string
	for route, class := range leakRoutes {
		if class == leakPublicVaries {
			out = append(out, route)
		}
	}
	sort.Strings(out)
	return out
}

// pdlVolatile are the config fields the draw itself writes, the version that
// hashes them, the flag the first roster load repairs and the drawHidden
// marker: everything a hidden draft's competition record may differ in from
// the same competition before its draw. swissCurrentRound is the round the
// draw made (1), a config field the draw writes beside the status, which
// already says a draw exists; it names no pairing.
var pdlVolatile = []string{"status", "configVersion", "drawHidden", "poolSize", "roundRobin", "hasParticipantIDs", "swissCurrentRound"}

// pdlNormalise is a copy of v with the volatile fields taken out of every
// object that is the competition compID's own record.
func pdlNormalise(v any, compID string) any {
	switch x := v.(type) {
	case map[string]any:
		out := make(map[string]any, len(x))
		own := x["id"] == compID
		for k, val := range x {
			if own && containsString(pdlVolatile, k) {
				continue
			}
			out[k] = pdlNormalise(val, compID)
		}
		return out
	case []any:
		out := make([]any, len(x))
		for i, val := range x {
			out[i] = pdlNormalise(val, compID)
		}
		return out
	}
	return v
}

func containsString(list []string, s string) bool {
	for _, x := range list {
		if x == s {
			return true
		}
	}
	return false
}

// pdlEntryOf is the entry of compID in a list of competition payloads, nil
// when it is not listed.
func pdlEntryOf(list any, compID string) any {
	entries, _ := list.([]any)
	for _, e := range entries {
		entry, _ := e.(map[string]any)
		if cfg, ok := entry["config"].(map[string]any); ok && cfg["id"] == compID {
			return entry
		}
	}
	return nil
}

// pdlView is the part of an answer to route that belongs to compID: the
// competition's entry on the list and the court feed (which carry the others
// too), nothing for the tournament (which carries no competition), and the
// whole answer otherwise, each with the volatile fields taken out.
func pdlView(route, compID string, body []byte) any {
	if !json.Valid(body) {
		return string(body)
	}
	var v any
	if err := json.Unmarshal(body, &v); err != nil {
		return string(body)
	}
	switch route {
	case "GET /api/viewer/tournament":
		return nil
	case "GET /api/viewer/competitions":
		return pdlNormalise(pdlEntryOf(v, compID), compID)
	case "GET /api/viewer/court/:court/matches":
		obj, _ := v.(map[string]any)
		return pdlNormalise(pdlEntryOf(obj["competitions"], compID), compID)
	}
	return pdlNormalise(v, compID)
}

// answers is every public route's answer for c to headers, with the draw's
// parameters and with unknown ones.
func (w *pdlWorld) answers(t *testing.T, c *pdlComp, headers map[string]string) map[string]pdlAnswer {
	t.Helper()
	out := map[string]pdlAnswer{}
	for _, route := range pdlPublicRoutes() {
		method, path, _ := strings.Cut(route, " ")
		for _, params := range []map[string]string{c.params(), c.bogusParams()} {
			var body any
			if method == http.MethodPost || method == http.MethodPut {
				body = map[string]any{"positions": map[string]string{}}
			}
			filled := fillRoute(path, params)
			rec := w.lw.do(method, filled, headers, body)
			require.NotEqual(t, http.StatusTooManyRequests, rec.Code, "%s: the probe must not be rate limited", filled)
			out[method+" "+filled] = pdlAnswer{
				code: rec.Code, body: rec.Body.String(), view: pdlView(route, c.id, rec.Body.Bytes()),
				cacheControl: rec.Header().Get("Cache-Control"),
			}
		}
	}
	return out
}

func (w *pdlWorld) hidden() []*pdlComp {
	var out []*pdlComp
	for _, c := range w.comps {
		if c.state == pdlHidden {
			out = append(out, c)
		}
	}
	return out
}

// drawLeaks lists what body carries of a hidden draft's draw: a competitor
// number, or the time a hidden match was moved to.
func (w *pdlWorld) drawLeaks(body string) []string {
	var out []string
	for _, c := range w.hidden() {
		if n := c.numberRe().FindString(body); n != "" {
			out = append(out, fmt.Sprintf("number %s of %s", n, c.id))
		}
	}
	if strings.Contains(body, pdlMovedTime) {
		out = append(out, "the moved time "+pdlMovedTime)
	}
	return out
}

// probe walks every public route for every competition as aud, and fails on
// any draw content of a hidden draft, on a cacheable answer, and on a hidden
// draft's answer that differs from its answer before its draw.
func (w *pdlWorld) probe(t *testing.T, aud pdlAudience) {
	t.Helper()
	for _, c := range w.comps {
		got := w.answers(t, c, aud.headers)
		for req, a := range got {
			if found := w.drawLeaks(a.body); len(found) > 0 {
				t.Errorf("[%s] %s answered %d carrying %s", aud.name, req, a.code, strings.Join(found, ", "))
			}
			if a.code == http.StatusOK && a.cacheControl != "no-store" {
				t.Errorf("[%s] %s varies by audience but is cacheable (Cache-Control %q)", aud.name, req, a.cacheControl)
			}
		}
		if c.state != pdlHidden {
			continue
		}
		w.compareWithBefore(t, c, aud, got)
	}
}

// compareWithBefore fails on any answer of the hidden draft c that is not
// the same competition's answer before its draw.
func (w *pdlWorld) compareWithBefore(t *testing.T, c *pdlComp, aud pdlAudience, got map[string]pdlAnswer) {
	t.Helper()
	before := c.before[aud.name]
	require.Len(t, got, len(before), "%s: the same requests before and after the draw", c.id)
	for req, want := range before {
		a, ok := got[req]
		if !assert.True(t, ok, "[%s] %s asked before the draw", aud.name, req) {
			continue
		}
		assert.Equal(t, want.code, a.code, "[%s] %s: status as before the draw (%s)", aud.name, req, a.body)
		assert.Equal(t, want.view, a.view, "[%s] %s: the public reads the hidden draft %s as before its draw", aud.name, req, c.id)
	}
}

// pdlFrame is one event the stream wrote.
type pdlFrame struct {
	Type EventType       `json:"type"`
	Data json.RawMessage `json:"data"`
	Seq  int64           `json:"seq"`
}

func (f pdlFrame) hasData() bool { return len(f.Data) > 0 && string(f.Data) != "null" }

// carriesDraw is whether the event, as the organiser's stream carries it,
// carries draw content (sseEventSpec.carriesDraw).
func (f pdlFrame) carriesDraw() bool {
	return sseEventCatalogue[f.Type].carriesDraw(fmt.Sprintf(`{"data":%s}`, f.Data))
}

func (f pdlFrame) competitionID() string {
	var probe struct {
		CompetitionID string `json:"competitionId"`
	}
	if !f.hasData() || json.Unmarshal(f.Data, &probe) != nil {
		return ""
	}
	return probe.CompetitionID
}

// pdlFrames are the events of an SSE body, by seq.
func pdlFrames(t *testing.T, body string) map[int64]pdlFrame {
	t.Helper()
	out := map[int64]pdlFrame{}
	for _, line := range strings.Split(body, "\n") {
		raw, ok := strings.CutPrefix(line, "data: ")
		if !ok {
			continue
		}
		var f pdlFrame
		if json.Unmarshal([]byte(raw), &f) != nil || f.Type == "" {
			continue
		}
		out[f.Seq] = f
	}
	return out
}

func (w *pdlWorld) isHidden(compID string) bool {
	c, ok := w.byID[compID]
	return ok && c.state == pdlHidden
}

// checkStream fails on any event of a public or reveal stream that carries
// a hidden draft's draw: a draw event naming a hidden draft with its data, or
// any draw content at all. Every draw event of a hidden draft the organiser's
// stream carries must reach it, with no data.
func (w *pdlWorld) checkStream(t *testing.T, name, body string, organiser map[int64]pdlFrame) {
	t.Helper()
	if found := w.drawLeaks(body); len(found) > 0 {
		t.Errorf("the %s stream carried %s", name, strings.Join(found, ", "))
	}
	frames := pdlFrames(t, body)
	for seq, f := range frames {
		if f.carriesDraw() && w.isHidden(f.competitionID()) {
			t.Errorf("the %s stream was sent %s (seq %d) of the hidden draft %s with its data: %s", name, f.Type, seq, f.competitionID(), f.Data)
		}
	}
	for seq, of := range organiser {
		if !of.carriesDraw() || !w.isHidden(of.competitionID()) {
			continue
		}
		f, ok := frames[seq]
		if assert.True(t, ok, "the %s stream was sent %s (seq %d) of %s, so no gap is seen", name, of.Type, seq, of.competitionID()) {
			assert.False(t, f.hasData(), "the %s stream was sent %s (seq %d) of %s with no data", name, of.Type, seq, of.competitionID())
		}
	}
}

// lastEvent is the newest event of type et naming compID in the stream's
// history.
func (w *pdlWorld) lastEvent(t *testing.T, et EventType, compID string) historyEntry {
	t.Helper()
	ch, entries, _, _ := w.lw.hub.subscribeWithReplay(1)
	require.NotNil(t, ch)
	defer w.lw.hub.Unsubscribe(ch)
	for i := len(entries) - 1; i >= 0; i-- {
		if entries[i].typ == et && entries[i].compID == compID {
			return entries[i]
		}
	}
	t.Fatalf("no %s of %s in the stream's history", et, compID)
	return historyEntry{}
}

func buildPDLWorld(t *testing.T) *pdlWorld {
	t.Helper()
	gin.SetMode(gin.TestMode)
	store := newTempStore(t)
	courts := []string{"A", "B", "C", "D", "E", "F", "G", "H", "I"}
	require.NoError(t, store.SaveTournament(&state.Tournament{
		Name: "Draw Taikai", Date: "01-06-2026", Venue: "Budokan", Courts: courts,
		Password: leakPassword, Mode: state.TournamentModeOfficiated,
	}))
	eng := engine.New(store)
	res := resources.NewResources(nil, fstest.MapFS{"web-mobile/index.html": {Data: []byte("<html><head></head><body>draw</body></html>")}})
	r, hub, limiter := NewRouterWithHub(store, eng, res, NewFileVerifier(store), NewHubWithHistory(4000), false)
	t.Cleanup(limiter.Close)
	t.Cleanup(hub.Close)
	w := &pdlWorld{
		lw:   &leakWorld{r: r, hub: hub, store: store, byID: map[string]*leakComp{}, now: time.Now().UnixMilli() - 600_000},
		byID: map[string]*pdlComp{},
	}
	// The competition that hides names comes first: its link is the reveal
	// audience every other draft's answers before the draw are read for too.
	specs := []*pdlComp{
		{id: "draw-names", kind: "individual", format: state.CompFormatMixed, option: state.PublicNamesCompetition, state: pdlHidden, prefix: "QXG", players: 6},
		{id: "draw-pools", kind: "individual", format: state.CompFormatMixed, state: pdlHidden, prefix: "QXA", players: 6},
		{id: "draw-knockout", kind: "individual", format: state.CompFormatKnockout, state: pdlHidden, prefix: "QXB", players: 6},
		{id: "draw-league", kind: "individual", format: state.CompFormatLeague, state: pdlHidden, prefix: "QXC", players: 4},
		{id: "draw-swiss", kind: "individual", format: state.CompFormatSwiss, state: pdlHidden, prefix: "QXD", players: 6},
		{id: "draw-team", kind: "team", format: state.CompFormatLeague, matchType: string(state.TeamMatchTypeFixed), state: pdlHidden, prefix: "QXE", players: 3},
		{id: "draw-kachinuki", kind: "team", format: state.CompFormatLeague, matchType: string(state.TeamMatchTypeKachinuki), state: pdlHidden, prefix: "QXF", players: 2},
		{id: "draw-published", kind: "individual", format: state.CompFormatMixed, state: pdlPublished, prefix: "QPA", players: 6},
		{id: "draw-started", kind: "individual", format: state.CompFormatMixed, state: pdlStarted, prefix: "QSA", players: 6},
	}
	for i, c := range specs {
		c.court = courts[i]
		w.addComp(t, c)
		w.comps = append(w.comps, c)
		w.byID[c.id] = c
	}
	return w
}

func (w *pdlWorld) addComp(t *testing.T, c *pdlComp) {
	t.Helper()
	lower := strings.ToLower(c.prefix)
	body := map[string]any{
		"id": c.id, "name": "Draw " + c.id, "courts": []string{c.court}, "kind": c.kind,
		"format": c.format, "numberPrefix": c.prefix,
	}
	var players []map[string]any
	for i := 1; i <= c.players; i++ {
		name := fmt.Sprintf("Fighter %s %d", lower, i)
		if c.isTeam() {
			name = fmt.Sprintf("Team %s %d", lower, i)
		}
		players = append(players, map[string]any{"name": name, "dojo": fmt.Sprintf("Dojo %s %d", lower, i)})
	}
	body["players"] = players
	switch {
	case c.isTeam():
		body["teamSize"], body["teamMatchType"] = 3, c.matchType
	case c.format == state.CompFormatMixed:
		body["poolSize"], body["poolWinners"] = 3, 2
	case c.format == state.CompFormatSwiss:
		body["swissRounds"] = 2
	}
	if c.option != "" {
		body["publicNames"], body["revealUntil"] = c.option, leakFarFuture
	}
	rec := w.lw.admin(http.MethodPost, "/api/competitions", body)
	require.Equal(t, http.StatusCreated, rec.Code, "%s: %s", c.id, rec.Body.String())

	if c.option != "" {
		key := w.lw.competitionKey(t, c.id)
		w.audiences = []pdlAudience{
			{name: "public"},
			{name: "competition link", headers: revealHeader(revealPair(c.id, key))},
		}
	}
	c.members = map[string][]domain.TeamMember{}
	if c.isTeam() {
		teams, err := w.lw.store.LoadParticipants(c.id, false)
		require.NoError(t, err)
		for ti, team := range teams {
			for mi, pos := range []string{"senpo", "chuken", "taisho"} {
				rec := w.lw.admin(http.MethodPost, "/api/competitions/"+c.id+"/teams/"+team.ID+"/members", map[string]any{"name": leakToken(lower, ti*10+mi+1, pos)})
				require.Equal(t, http.StatusCreated, rec.Code, rec.Body.String())
				var m domain.TeamMember
				require.NoError(t, json.Unmarshal(rec.Body.Bytes(), &m))
				c.members[team.ID] = append(c.members[team.ID], m)
			}
		}
	}

	// The draw is made once to learn the match the routes are asked about,
	// then discarded so the answers before the draw are read at that match.
	w.generate(t, c)
	first := c.match
	if c.state == pdlHidden {
		rec := w.lw.admin(http.MethodDelete, "/api/competitions/"+c.id+"/draw", nil)
		require.Equal(t, http.StatusNoContent, rec.Code, "%s: %s", c.id, rec.Body.String())
		c.before = map[string]map[string]pdlAnswer{}
		for _, aud := range w.audiences {
			c.before[aud.name] = w.answers(t, c, aud.headers)
		}
		w.generate(t, c)
		require.Equal(t, first, c.match, "%s: premise: the draw gives the same match again", c.id)
	}

	switch c.state {
	case pdlHidden:
		stored, err := w.lw.store.LoadCompetition(c.id)
		require.NoError(t, err)
		require.True(t, stored.DrawHidden, "%s: premise: a generated draw is stored hidden", c.id)
		w.addDrawContent(t, c)
	case pdlPublished:
		rec := w.lw.admin(http.MethodPost, "/api/competitions/"+c.id+"/publish-draw", nil)
		require.Equal(t, http.StatusOK, rec.Code, "%s: %s", c.id, rec.Body.String())
	case pdlStarted:
		rec := w.lw.admin(http.MethodPost, "/api/competitions/"+c.id+"/start", nil)
		require.Equal(t, http.StatusOK, rec.Code, "%s: %s", c.id, rec.Body.String())
	}
}

// generate makes c's draw through the organiser's route and picks a match
// of it whose sides are both known.
func (w *pdlWorld) generate(t *testing.T, c *pdlComp) {
	t.Helper()
	rec := w.lw.admin(http.MethodPost, "/api/competitions/"+c.id+"/generate-draw", nil)
	require.Equal(t, http.StatusOK, rec.Code, "%s: %s", c.id, rec.Body.String())
	matches, err := w.lw.store.LoadPoolMatches(c.id)
	require.NoError(t, err)
	c.match, c.side = "", ""
	for _, m := range matches {
		if m.SideAID != "" && m.SideBID != "" {
			c.match, c.side = m.ID, m.SideAID
			return
		}
	}
	bracket, err := w.lw.store.LoadBracket(c.id)
	require.NoError(t, err)
	require.NotNil(t, bracket, "%s: the draw holds a match", c.id)
	for _, round := range bracket.Rounds {
		for _, m := range round {
			if m.SideAID != "" && m.SideBID != "" {
				c.match, c.side = m.ID, m.SideAID
				return
			}
		}
	}
	t.Fatalf("%s: the draw holds no match with both sides known", c.id)
}

// addDrawContent records what only a draw can hold, straight into the
// store so no draft starts: a status naming the match, a team's lineup saved
// for it, and, in the pools draft, a match running on its shiaijo.
func (w *pdlWorld) addDrawContent(t *testing.T, c *pdlComp) {
	t.Helper()
	require.NoError(t, w.lw.store.SetCompetitorStatus(c.id, domain.CompetitorStatus{
		PlayerID: c.side, Eligible: false, Reason: "fusenpai", MatchID: c.match,
		RecordedAt: time.Date(2030, 6, 1, 10, 0, 0, 0, time.UTC),
	}))
	if c.isTeam() {
		members := c.members[c.side]
		require.Len(t, members, 3)
		require.NoError(t, w.lw.store.SetTeamLineup(c.id, domain.TeamLineup{
			TeamID: c.side, MatchID: c.match, Positions: map[domain.Position]string{domain.PositionNumbered(1): members[0].Name},
		}, 3))
	}
	if c.id == "draw-pools" {
		matches, err := w.lw.store.LoadPoolMatches(c.id)
		require.NoError(t, err)
		for i := range matches {
			if matches[i].ID == c.match {
				matches[i].Court, matches[i].Status = c.court, state.MatchStatusRunning
			}
		}
		require.NoError(t, w.lw.store.SavePoolMatches(c.id, matches))
	}
}

// openStreams opens a public, a reveal and an organiser stream that replay
// from the start of the history.
func (w *pdlWorld) openStreams(t *testing.T) map[string]*leakStreamConn {
	t.Helper()
	org := w.lw.ticket(t, map[string]string{"X-Tournament-Password": leakPassword})
	reveal := w.lw.ticket(t, w.audiences[1].headers)
	conns := map[string]*leakStreamConn{
		"public":    w.lw.openStream(t, "?lastEventId=1"),
		"reveal":    w.lw.openStream(t, "?lastEventId=1&ticket="+url.QueryEscape(reveal)),
		"organiser": w.lw.openStream(t, "?lastEventId=1&ticket="+url.QueryEscape(org)),
	}
	w.lw.settle(t, conns["public"], conns["reveal"], conns["organiser"])
	return conns
}

func (w *pdlWorld) closeStreams(t *testing.T, conns map[string]*leakStreamConn) map[string]string {
	t.Helper()
	w.lw.settle(t, conns["public"], conns["reveal"], conns["organiser"])
	out := map[string]string{}
	for name, conn := range conns {
		code, body := conn.close(t)
		require.Equal(t, http.StatusOK, code, "the %s stream: %s", name, body)
		assert.NotContains(t, body, "resync_required", "the %s stream's replay covered the whole history", name)
		out[name] = body
	}
	return out
}

// replay opens a stream for headers' ticket (none for the public) replaying
// from just before seq, and answers the frame of seq.
func (w *pdlWorld) replay(t *testing.T, ticketHeaders map[string]string, seq int64) pdlFrame {
	t.Helper()
	query := fmt.Sprintf("?lastEventId=%d", seq-1)
	if ticketHeaders != nil {
		query += "&ticket=" + url.QueryEscape(w.lw.ticket(t, ticketHeaders))
	}
	conn := w.lw.openStream(t, query)
	waitForFrame(t, conn.w, fmt.Sprintf(`"seq":%d`, seq))
	code, body := conn.close(t)
	require.Equal(t, http.StatusOK, code, body)
	f, ok := pdlFrames(t, body)[seq]
	require.True(t, ok, "the replay holds seq %d: %s", seq, body)
	return f
}

func TestPublicDrawLeak(t *testing.T) {
	w := buildPDLWorld(t)
	organiser := map[string]string{"X-Tournament-Password": leakPassword}

	t.Run("the probe covers the shapes it must", func(t *testing.T) {
		require.Len(t, w.audiences, 2, "the public and a reveal link")
		routes := pdlPublicRoutes()
		require.NotEmpty(t, routes)
		for _, c := range w.comps {
			if c.state == pdlHidden {
				require.Len(t, c.before["public"], len(w.answers(t, c, nil)), c.id)
			}
		}
		// The organiser reads every hidden draft's draw, and the public the
		// published and the started one, with their numbers: a probe that
		// found no number anywhere would prove nothing.
		for _, c := range w.comps {
			body := w.lw.viewerRead(t, c.id, organiser)
			if c.kind == "individual" && c.format != state.CompFormatSwiss {
				assert.Regexp(t, c.numberRe(), body, "the organiser reads %s's numbers", c.id)
			}
			if c.state != pdlHidden {
				public := w.lw.viewerRead(t, c.id, nil)
				assert.Regexp(t, c.numberRe(), public, "the public reads the %s draft %s's numbers", c.state, c.id)
				assert.NotContains(t, public, `"drawHidden":true`, c.id)
			}
		}
	})

	t.Run("no public answer carries a hidden draw, and each hidden draft reads as before its draw", func(t *testing.T) {
		for _, aud := range w.audiences {
			t.Run(aud.name, func(t *testing.T) { w.probe(t, aud) })
		}
	})

	t.Run("the organiser reads the hidden drafts' draw on the public routes", func(t *testing.T) {
		for _, c := range w.hidden() {
			got := w.answers(t, c, organiser)
			changed := false
			for req, want := range c.before["public"] {
				changed = changed || !assert.ObjectsAreEqual(want.view, got[req].view)
			}
			assert.True(t, changed, "the organiser's answers for %s carry its draw", c.id)
		}
	})

	t.Run("every draw event of a hidden draft reaches the public with no data, live and replayed", func(t *testing.T) {
		conns := w.openStreams(t)

		// A court move of the pools draft's match onto the published draft's
		// shiaijo, and a time move of the Swiss draft's match.
		pools, published := w.byID["draw-pools"], w.byID["draw-published"]
		matches, err := w.lw.store.LoadPoolMatches(pools.id)
		require.NoError(t, err)
		var moved string
		for _, m := range matches {
			if m.ID != pools.match {
				moved = m.ID
				break
			}
		}
		require.NotEmpty(t, moved)
		rec := w.lw.admin(http.MethodPut, "/api/competitions/"+pools.id+"/matches/"+url.PathEscape(moved)+"/court", map[string]any{"court": published.court})
		require.Equal(t, http.StatusOK, rec.Code, rec.Body.String())
		courtMove := w.lastEvent(t, EventMatchUpdated, pools.id)

		swiss := w.byID["draw-swiss"]
		rec = w.lw.admin(http.MethodPut, "/api/competitions/"+swiss.id+"/matches/"+url.PathEscape(swiss.match)+"/time", map[string]any{"scheduledAt": pdlMovedTime})
		require.Equal(t, http.StatusOK, rec.Code, rec.Body.String())

		// A team's lineup saved for the team draft's match.
		team := w.byID["draw-team"]
		members := team.members[team.side]
		rec = w.lw.admin(http.MethodPut, "/api/competitions/"+team.id+"/teams/"+team.side+"/match-lineups/"+url.PathEscape(team.match), map[string]any{
			"positions": map[string]string{"1": members[0].Name, "2": members[1].Name, "3": members[2].Name},
			"memberIds": map[string]string{"1": members[0].ID, "2": members[1].ID, "3": members[2].ID},
		})
		require.Equal(t, http.StatusOK, rec.Code, rec.Body.String())
		lineup := w.lastEvent(t, EventLineupUpdated, team.id)

		// Every catalogue event that carries a draw, for every hidden draft,
		// carrying one of its numbers.
		var drawTypes []EventType
		for et, spec := range sseEventCatalogue {
			if spec.draw {
				drawTypes = append(drawTypes, et)
			}
		}
		sort.Slice(drawTypes, func(i, j int) bool { return drawTypes[i] < drawTypes[j] })
		require.NotEmpty(t, drawTypes)
		for _, c := range w.hidden() {
			number := c.prefix + "99"
			for _, et := range drawTypes {
				w.lw.hub.Broadcast(et, gin.H{
					"competitionId": c.id, "matchId": c.match, "number": number,
					"match":  gin.H{"id": c.match, "sideA": number, "sideAId": c.side, "court": c.court, "scheduledAt": pdlMovedTime},
					"status": gin.H{"playerId": c.side, "status": "withdrawn", "matchId": c.match},
					"teamId": c.side, "swissCurrentRound": 1, "matchCount": 3,
				})
			}
		}

		bodies := w.closeStreams(t, conns)
		org := pdlFrames(t, bodies["organiser"])
		for _, e := range []historyEntry{courtMove, lineup} {
			f, ok := org[e.seq]
			if assert.True(t, ok, "the organiser's stream was sent %s of %s", e.typ, e.compID) {
				assert.True(t, f.hasData(), "the organiser's stream carries %s of %s as broadcast", e.typ, e.compID)
			}
		}
		assert.Contains(t, bodies["organiser"], pools.prefix+"99", "the organiser's stream carries the hidden drafts' draw events")
		w.checkStream(t, "public", bodies["public"], org)
		w.checkStream(t, "reveal", bodies["reveal"], org)

		// Replayed one by one, the court move and the lineup are withheld too.
		for _, e := range []historyEntry{courtMove, lineup} {
			assert.False(t, w.replay(t, nil, e.seq).hasData(), "a public replay of %s of %s", e.typ, e.compID)
			assert.False(t, w.replay(t, w.audiences[1].headers, e.seq).hasData(), "a reveal replay of %s of %s", e.typ, e.compID)
		}

		// The public court feed of the shiaijo the match moved to shows the
		// published draft and nothing of the hidden one, and every hidden
		// draft still reads as before its draw.
		for _, aud := range w.audiences {
			rec := w.lw.do(http.MethodGet, "/api/viewer/court/"+published.court+"/matches", aud.headers, nil)
			require.Equal(t, http.StatusOK, rec.Code, rec.Body.String())
			var feed map[string]any
			require.NoError(t, json.Unmarshal(rec.Body.Bytes(), &feed))
			assert.Nil(t, pdlEntryOf(feed["competitions"], pools.id), "[%s] the hidden draft whose match moved here is left out", aud.name)
			assert.Regexp(t, published.numberRe(), rec.Body.String(), "[%s] the published draft is on its shiaijo", aud.name)
			w.probe(t, aud)
		}
	})

	t.Run("a published draft's events are withheld again on a replay once it is hidden again", func(t *testing.T) {
		c, started := w.byID["draw-knockout"], w.byID["draw-started"]
		rec := w.lw.admin(http.MethodPost, "/api/competitions/"+c.id+"/publish-draw", nil)
		require.Equal(t, http.StatusOK, rec.Code, rec.Body.String())
		c.state = pdlPublished
		visibility := w.lastEvent(t, EventDrawVisibilityChanged, "")

		rec = w.lw.admin(http.MethodPut, "/api/competitions/"+c.id+"/matches/"+url.PathEscape(c.match)+"/court", map[string]any{"court": started.court})
		require.Equal(t, http.StatusOK, rec.Code, rec.Body.String())
		move := w.lastEvent(t, EventMatchUpdated, c.id)
		f := w.replay(t, nil, move.seq)
		require.True(t, f.hasData(), "published, the public replay carries the court move")
		assert.Equal(t, c.id, f.competitionID())
		assert.Regexp(t, c.numberRe(), w.lw.viewerRead(t, c.id, nil), "published, the public reads its numbers")
		assert.True(t, w.replay(t, nil, visibility.seq).hasData(), "the publish itself reaches the public as broadcast")

		rec = w.lw.admin(http.MethodPost, "/api/competitions/"+c.id+"/unpublish-draw", nil)
		require.Equal(t, http.StatusOK, rec.Code, rec.Body.String())
		c.state = pdlHidden
		assert.False(t, w.replay(t, nil, move.seq).hasData(), "hidden again, the public replay withholds the court move")
		assert.False(t, w.replay(t, w.audiences[1].headers, move.seq).hasData(), "hidden again, a reveal replay withholds it too")
		assert.True(t, w.replay(t, map[string]string{"X-Tournament-Password": leakPassword}, move.seq).hasData(), "the organiser's replay carries it")
		for _, aud := range w.audiences {
			w.compareWithBefore(t, c, aud, w.answers(t, c, aud.headers))
		}
	})
}

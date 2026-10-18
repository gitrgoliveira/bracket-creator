package mobileapp

// The permutation gate for who names bout 1 of a kachinuki encounter (operator
// decision K1 (c), engine/kachinuki_opener.go). Three review rounds in a row
// found new defects in it, so every ordering of the writes that touch it is
// driven through the real doors, and the one rule is asserted after every step:
//
//  1. bout 1's fighter on each side is the first fighter of that match's
//     lineup: its own saved lineup, else the lineup it started with, else
//     (while it has not started) the lineup in force by the carry rule; a
//     match started before this rule (no lineup kept at its start) is renamed
//     only by an edit of its own lineup;
//  2. no operator tap is refused for a fighter;
//  3. a finished match's result, winner, team figures and points never move;
//  4. a later, unstarted match follows the starting lineup and the old carry
//     rule (an earlier match's own lineup), never the lineup a match started
//     with;
//  5. a reseat after any step changes nothing (the log does not grow, the
//     store already holds what the rule says);
//  6. a client batch under the server's session is refused and changes
//     nothing.
//
// Order is by each change's stamp: a starting-lineup save made before the
// start (stamped before it) counts for the match whichever way it arrives.

import (
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"slices"
	"sort"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/gitrgoliveira/bracket-creator/internal/domain"
	"github.com/gitrgoliveira/bracket-creator/internal/engine"
	"github.com/gitrgoliveira/bracket-creator/internal/state"
	"github.com/stretchr/testify/require"
)

// The steps an ordering is made of.
const (
	opStartingPre  = "starting lineup save stamped before the start"
	opStartingPost = "starting lineup save stamped after the start"
	opStart        = "start"
	opOwn          = "own lineup save"
	opTap1         = "device 1 strikes bout 1"
	opTap2         = "device 2 strikes bout 1"
	opPick2        = "pick on bout 2"
	opRemove       = "Remove this bout"
	opReadd        = "bout 2 added again"
	opFinish       = "finish"
	opLate         = "late tap after the finish"
	opRefused      = "tap stamped before the start"
	opSpoof        = "client batch under the server's session"
)

const gateMatch = "Pool A-0"
const gateLater = "Pool A-1"

// gateWorld is one ordering's competition and what the steps have done so far.
type gateWorld struct {
	t       *testing.T
	r       *gin.Engine
	store   *state.Store
	eng     *engine.Engine
	comp    string
	legacy  bool // the match started before this rule: running, bout 1 named, no lineup kept
	teamA   string
	teamB   string
	members map[string]string // name -> member id (team A and B)
	base    int64             // the device stamps' frame, in the past
	arrived map[string]bool
	order   []string
	// The model of the starting lineup's first fighter of team A, by arrival
	// (the lineup door orders by arrival), and the lineup match 1 started
	// with (first fighter and the stamp of the change that put it there).
	startingNow      string
	startingNowStamp int64
	pinned           string
	pinnedStamp      int64
	finished         *state.MatchResult
	failures         map[string]int
}

func (w *gateWorld) stamp(op string) int64 {
	switch op {
	case opRefused:
		return w.base + 150
	case opStartingPre:
		return w.base + 100
	case opStart:
		return w.base + 200
	case opStartingPost:
		return w.base + 300
	case opTap1:
		return w.base + 400
	case opTap2:
		return w.base + 410
	case opSpoof:
		return w.base + 420
	case opOwn:
		return w.base + 500
	case opPick2:
		return w.base + 600
	}
	// Made after a server write (the removal's tombstone, the finish): now.
	return time.Now().UnixMilli()
}

func (w *gateWorld) fail(kind, format string, args ...any) {
	w.failures[kind]++
	if w.failures[kind] <= 3 {
		w.t.Logf("[%s] %s after %v: %s", kind, w.comp, w.order, fmt.Sprintf(format, args...))
	}
}

func (w *gateWorld) do(method, path string, body any) *httptest.ResponseRecorder {
	w.t.Helper()
	rec := httptest.NewRecorder()
	w.r.ServeHTTP(rec, squadJSONReq(method, path, "secret", body))
	return rec
}

func (w *gateWorld) matchPath(matchID string) string {
	return "/api/competitions/" + w.comp + "/matches/" + url.PathEscape(matchID)
}

func (w *gateWorld) stored(matchID string) state.MatchResult {
	w.t.Helper()
	ms, err := w.store.LoadPoolMatches(w.comp)
	require.NoError(w.t, err)
	for _, m := range ms {
		if m.ID == matchID {
			return m
		}
	}
	w.t.Fatalf("%s: no match %s", w.comp, matchID)
	return state.MatchResult{}
}

func (w *gateWorld) logLen() int {
	lines, err := w.store.LoadScoreLog(w.comp, gateMatch)
	require.NoError(w.t, err)
	return len(lines)
}

// tap posts one score event as an operator's device and returns its outcome.
func (w *gateWorld) tap(session string, ev domain.ScoreEvent) (int, string) {
	ev.Session = session
	rec := w.do(http.MethodPost, w.matchPath(gateMatch)+"/score-events", map[string]any{"session": session, "events": []domain.ScoreEvent{ev}})
	if rec.Code != http.StatusOK {
		return rec.Code, ""
	}
	var out struct {
		Outcomes []scoreEventOutcome `json:"outcomes"`
	}
	require.NoError(w.t, json.Unmarshal(rec.Body.Bytes(), &out), rec.Body.String())
	require.Len(w.t, out.Outcomes, 1)
	return rec.Code, out.Outcomes[0].Outcome
}

func (w *gateWorld) saveStarting(name string, stamp int64, op string) {
	pos := domain.PositionNumbered(1)
	rec := w.do(http.MethodPut, "/api/competitions/"+w.comp+"/teams/"+w.teamA+"/lineups/0", map[string]any{
		"positions": map[domain.Position]string{pos: name}, "memberIds": map[domain.Position]string{pos: w.members[name]},
		"changed": []domain.Position{pos}, "changedAt": map[domain.Position]int64{pos: stamp}, "writeId": w.comp + op,
	})
	require.Equal(w.t, http.StatusOK, rec.Code, rec.Body.String())
	// The lineup door orders by arrival: the later arrival holds the position.
	w.startingNow, w.startingNowStamp = name, stamp
	// A save stamped before the start lands on the lineup the match started
	// with whichever way it arrives, unless a later-stamped change is there.
	if w.arrived[opStart] && stamp < w.stamp(opStart) && stamp > w.pinnedStamp {
		w.pinned, w.pinnedStamp = name, stamp
	}
}

func (w *gateWorld) step(op string) {
	t := w.t
	t.Helper()
	w.order = append(w.order, op)
	switch op {
	case opStartingPre:
		w.saveStarting("Kato", w.stamp(op), op)
	case opStartingPost:
		w.saveStarting("Mori", w.stamp(op), op)
	case opStart:
		m := w.stored(gateMatch)
		rec := w.do(http.MethodPut, w.matchPath(gateMatch)+"/score", map[string]any{
			"sideA": m.SideA, "sideB": m.SideB, "sideAId": m.SideAID, "sideBId": m.SideBID,
			"status": "running", "startOnly": true, "ipponsA": []string{}, "ipponsB": []string{},
			"changed": []string{state.GroupResult}, "modifiedAt": w.stamp(op), "writeId": w.comp + op,
		})
		require.Equal(t, http.StatusOK, rec.Code, rec.Body.String())
		w.pinned, w.pinnedStamp = w.startingNow, w.startingNowStamp
	case opOwn:
		pos := domain.PositionNumbered(1)
		rec := w.do(http.MethodPut, "/api/competitions/"+w.comp+"/teams/"+w.teamA+"/match-lineups/"+url.PathEscape(gateMatch), map[string]any{
			"positions": map[domain.Position]string{pos: "Ito"}, "memberIds": map[domain.Position]string{pos: w.members["Ito"]},
			"changed": []domain.Position{pos}, "changedAt": map[domain.Position]int64{pos: w.stamp(op)}, "writeId": w.comp + op,
		})
		require.Equal(t, http.StatusOK, rec.Code, rec.Body.String())
	case opTap1, opTap2:
		side, letter, session := "a", "M", "dev1"
		if op == opTap2 {
			side, letter, session = "b", "K", "dev2"
		}
		code, outcome := w.tap(session, domain.ScoreEvent{ID: w.comp + op, Seq: 1, Stamp: w.stamp(op), Kind: domain.ScoreIpponAdd, Pos: 1, Side: side, Letter: letter})
		require.Equal(t, http.StatusOK, code)
		if outcome != domain.ScoreOutcomeApplied {
			w.fail("tap refused", "%s answered %q", op, outcome)
		}
	case opPick2, opReadd:
		seq := int64(2)
		if op == opReadd {
			seq = 3
		}
		code, outcome := w.tap("dev1", domain.ScoreEvent{ID: w.comp + op, Seq: seq, Stamp: w.stamp(op), Kind: domain.ScoreBoutFighter, Pos: 2, Side: "a", Name: "Kubo", MemberID: w.members["Kubo"]})
		require.Equal(t, http.StatusOK, code)
		if outcome != domain.ScoreOutcomeApplied {
			w.fail("tap refused", "%s answered %q", op, outcome)
		}
	case opRemove:
		rec := w.do(http.MethodDelete, w.matchPath(gateMatch)+"/kachinuki-bout", nil)
		require.Equal(t, http.StatusOK, rec.Code, rec.Body.String())
	case opFinish:
		m := w.stored(gateMatch)
		rec := w.do(http.MethodPut, w.matchPath(gateMatch)+"/score", map[string]any{
			"sideA": m.SideA, "sideB": m.SideB, "sideAId": m.SideAID, "sideBId": m.SideBID,
			"status": "completed", "decision": "hikiwake",
			"ipponsA": []string{}, "ipponsB": []string{}, "subResults": m.SubResults,
			"changed": []string{state.GroupResult}, "modifiedAt": w.stamp(op), "writeId": w.comp + op,
		})
		require.Equal(t, http.StatusOK, rec.Code, rec.Body.String())
		f := w.stored(gateMatch)
		require.Equal(t, state.MatchStatusCompleted, f.Status, "the finish lands")
		w.finished = &f
	case opLate:
		code, outcome := w.tap("dev2", domain.ScoreEvent{ID: w.comp + op, Seq: 2, Stamp: w.stamp(op), Kind: domain.ScoreIpponAdd, Pos: 1, Side: "b", Letter: "D"})
		require.Equal(t, http.StatusOK, code)
		if outcome != domain.ScoreOutcomeMatchClosed {
			w.fail("late tap", "answered %q, want matchClosed", outcome)
		}
	case opRefused:
		code, outcome := w.tap("dev3", domain.ScoreEvent{ID: w.comp + op, Seq: 1, Stamp: w.stamp(op), Kind: domain.ScoreIpponAdd, Pos: 1, Side: "a", Letter: "D"})
		require.Equal(t, http.StatusOK, code)
		if outcome == domain.ScoreOutcomeApplied {
			w.fail("refused write", "a tap stamped before the start was applied")
		}
	case opSpoof:
		before := w.stored(gateMatch)
		beforeLog := w.logLen()
		rec := w.do(http.MethodPost, w.matchPath(gateMatch)+"/score-events", map[string]any{"session": engine.ServerEventSession, "events": []domain.ScoreEvent{
			{ID: w.comp + op, Session: engine.ServerEventSession, Stamp: w.stamp(op), Kind: domain.ScoreBoutFighter, Pos: 1, Side: "a", Name: "Zed"},
			{ID: w.comp + op + "i", Session: engine.ServerEventSession, Stamp: w.stamp(op), Kind: domain.ScoreIpponAdd, Pos: 1, Side: "a", Letter: "M"},
		}})
		if rec.Code != http.StatusBadRequest {
			w.fail("spoof", "answered %d, want 400", rec.Code)
		}
		after := w.stored(gateMatch)
		if w.logLen() != beforeLog || after.Status != before.Status || boutName(after, 1, "a") != boutName(before, 1, "a") || len(after.SubResults) != len(before.SubResults) {
			w.fail("spoof", "the store changed: status %s -> %s, bout 1 %q -> %q", before.Status, after.Status, boutName(before, 1, "a"), boutName(after, 1, "a"))
		}
	}
	w.arrived[op] = true
	w.check()
}

func boutRow(m state.MatchResult, pos int) (state.SubMatchResult, bool) {
	for _, s := range m.SubResults {
		if s.Position == pos {
			return s, true
		}
	}
	return state.SubMatchResult{}, false
}

func boutName(m state.MatchResult, pos int, side string) string {
	row, ok := boutRow(m, pos)
	if !ok {
		return "<no row>"
	}
	if side == "b" {
		return row.SideB
	}
	return row.SideA
}

// check asserts the rule after a step.
func (w *gateWorld) check() {
	t := w.t
	m := w.stored(gateMatch)
	// 1. Bout 1's names.
	if _, ok := boutRow(m, 1); ok {
		wantA := ""
		switch {
		case w.arrived[opOwn]:
			wantA = "Ito"
		case w.legacy:
			wantA = "Aoki"
		case w.arrived[opStart]:
			wantA = w.pinned
		}
		if wantA != "" && boutName(m, 1, "a") != wantA {
			kind := "bout 1 (started here)"
			if w.legacy {
				kind = "bout 1 (started before this rule)"
			}
			w.fail(kind, "bout 1 Shiro is %q, want %q", boutName(m, 1, "a"), wantA)
		}
		if got := boutName(m, 1, "b"); got != "Fujii" {
			w.fail("bout 1 Aka", "bout 1 Aka is %q, want Fujii", got)
		}
	}
	// 3. A finished match's result never moves.
	if w.finished != nil {
		f := *w.finished
		if m.Status != f.Status || m.Winner != f.Winner || m.WinnerID != f.WinnerID || m.Decision != f.Decision {
			w.fail("finished result", "result moved: %s %q %q -> %s %q %q", f.Status, f.Winner, f.Decision, m.Status, m.Winner, m.Decision)
		}
		if a, b := f.TeamResult(), m.TeamResult(); (a == nil) != (b == nil) || (a != nil && *a != *b) {
			w.fail("finished result", "team figures moved: %+v -> %+v", a, b)
		}
		for _, row := range f.SubResults {
			now, ok := boutRow(m, row.Position)
			if !ok || strings.Join(now.IpponsA, "") != strings.Join(row.IpponsA, "") || strings.Join(now.IpponsB, "") != strings.Join(row.IpponsB, "") ||
				state.SubBoutWinnerSide(now, m.SideA, m.SideB) != state.SubBoutWinnerSide(row, f.SideA, f.SideB) {
				w.fail("finished result", "bout %d moved", row.Position)
			}
		}
	}
	// 4. The later match follows the starting lineup or the old carry rule.
	in, err := w.eng.LineupInForce(w.comp, w.teamA, gateLater)
	require.NoError(t, err)
	wantLater, wantSource := w.startingNow, ""
	if w.arrived[opOwn] {
		wantLater, wantSource = "Ito", gateMatch
	}
	if got := in.Lineup.Positions[domain.PositionNumbered(1)]; got != wantLater || in.Source.MatchID != wantSource {
		w.fail("later match", "%s fields %q from %q, want %q from %q", gateLater, got, in.Source.MatchID, wantLater, wantSource)
	}
	// 5. A reseat after the step changes nothing.
	before := w.logLen()
	changed, err := w.eng.ReseatKachinukiOpeners(w.comp, "")
	require.NoError(t, err)
	if len(changed) > 0 || w.logLen() != before {
		w.fail("reseat", "a reseat after the step changed %v (log %d -> %d)", changed, before, w.logLen())
	}
}

// newGateWorld makes one ordering's competition: teams A (Tora), B (Washi) and
// C (Kuma), a kachinuki encounter A v B and a later one A v C, and each team's
// starting lineup (A: Sato, B: Fujii, C: Goto), saved before any device stamp.
func newGateWorld(t *testing.T, r *gin.Engine, store *state.Store, eng *engine.Engine, comp string, legacy bool, failures map[string]int) *gateWorld {
	t.Helper()
	require.NoError(t, store.SaveCompetition(&state.Competition{ID: comp, Kind: "team", TeamMatchType: state.TeamMatchTypeKachinuki, TeamSize: 3, Status: state.CompStatusPools}))
	require.NoError(t, store.SaveParticipants(comp, []domain.Player{{Name: "Tora", Dojo: "Tora Dojo"}, {Name: "Washi", Dojo: "Washi Dojo"}, {Name: "Kuma", Dojo: "Kuma Dojo"}}))
	teams, err := store.LoadParticipants(comp, false)
	require.NoError(t, err)
	ids := map[string]string{}
	for _, p := range teams {
		ids[p.Name] = p.ID
	}
	w := &gateWorld{t: t, r: r, store: store, eng: eng, comp: comp, legacy: legacy, teamA: ids["Tora"], teamB: ids["Washi"],
		members: map[string]string{}, arrived: map[string]bool{}, failures: failures,
		base: time.Now().UnixMilli() - 600_000, startingNow: "Sato", pinned: "Sato"}
	for team, names := range map[string][]string{ids["Tora"]: {"Sato", "Kato", "Mori", "Ito", "Kubo", "Aoki"}, ids["Washi"]: {"Fujii"}, ids["Kuma"]: {"Goto"}} {
		for _, n := range names {
			mem, err := store.AddTeamMember(comp, team, n)
			require.NoError(t, err)
			w.members[n] = mem.ID
		}
	}
	for team, n := range map[string]string{ids["Tora"]: "Sato", ids["Washi"]: "Fujii", ids["Kuma"]: "Goto"} {
		pos := domain.PositionNumbered(1)
		require.NoError(t, store.SetTeamLineup(comp, domain.TeamLineup{TeamID: team, Round: 0,
			Positions: map[domain.Position]string{pos: n}, MemberIDs: map[domain.Position]string{pos: w.members[n]}}, 3))
	}
	first := state.MatchResult{ID: gateMatch, SideA: "Tora", SideB: "Washi", SideAID: ids["Tora"], SideBID: ids["Washi"], Status: state.MatchStatusScheduled}
	if legacy {
		// Started before this rule: running, bout 1 named by hand, no lineup
		// kept at its start.
		first.Status = state.MatchStatusRunning
		first.ModifiedAt = w.base + 200
		first.SubResults = []state.SubMatchResult{{Position: 1, SideA: "Aoki", SideAMemberID: w.members["Aoki"], SideB: "Fujii", SideBMemberID: w.members["Fujii"], IpponsA: []string{}, IpponsB: []string{}}}
		w.arrived[opStart] = true
	}
	require.NoError(t, store.SavePoolMatches(comp, []state.MatchResult{
		first,
		{ID: gateLater, SideA: "Tora", SideB: "Kuma", SideAID: ids["Tora"], SideBID: ids["Kuma"], Status: state.MatchStatusScheduled},
	}))
	return w
}

// interleave returns every merge of extra into core that keeps each one's own
// order... and every order of extra among itself: each extra item is placed at
// every gap of the sequence built so far.
// gateOrderings is every ordering the gate drives. The scoring steps keep the
// order a court imposes (the start before the taps, a removal after the pick
// it removes and before the bout is added again, the finish after the bouts,
// the late tap after the finish), with the two devices' taps in both orders
// and the bout 2 pick between or after them; the three lineup saves are placed
// at every point, in every order among themselves; the spoofed batch comes
// first, in the middle or last, in turn; and a tap reaching the match before it
// starts (refused, not started) comes before the start or first.
func gateOrderings(legacy bool) [][]string {
	var cores [][]string
	for _, taps := range [][]string{{opTap1, opTap2}, {opTap2, opTap1}} {
		pick := []string{opPick2, opRemove, opReadd}
		for _, at := range []int{1, 2} {
			c := []string{}
			if !legacy {
				c = append(c, opStart)
			}
			c = append(c, taps[:at]...)
			c = append(c, pick...)
			c = append(c, taps[at:]...)
			c = append(c, opFinish, opLate)
			cores = append(cores, c)
		}
	}
	saves := []string{opStartingPre, opStartingPost, opOwn}
	place := func(s []string, op string, at int) []string {
		r := make([]string, 0, len(s)+1)
		r = append(r, s[:at]...)
		r = append(r, op)
		return append(r, s[at:]...)
	}
	var out [][]string
	n := 0
	for _, core := range cores {
		finish := slices.Index(core, opFinish)
		// The places a lineup save can arrive at: before the start (or, for
		// a match started before this rule, before any tap), once the match
		// is under way, just before the finish, and after the late tap.
		gaps := []int{0, 1, finish, len(core)}
		seen := map[string]bool{}
		for _, perm := range [][]int{{0, 1, 2}, {0, 2, 1}, {1, 0, 2}, {1, 2, 0}, {2, 0, 1}, {2, 1, 0}} {
			for g := 0; g < len(gaps)*len(gaps)*len(gaps); g++ {
				at := []int{gaps[g%4], gaps[(g/4)%4], gaps[g/16]}
				// Placed in perm order, each at its gap counted on the core,
				// after the saves already placed there.
				seq := append([]string(nil), core...)
				for k, i := range perm {
					offset := 0
					for _, j := range perm[:k] {
						if at[j] <= at[i] {
							offset++
						}
					}
					seq = place(seq, saves[i], at[i]+offset)
				}
				key := strings.Join(seq, "|")
				if seen[key] {
					continue
				}
				seen[key] = true
				if !legacy {
					seq = place(seq, opRefused, []int{0, slices.Index(seq, opStart)}[n%2])
				}
				seq = place(seq, opSpoof, []int{0, len(seq) / 2, len(seq)}[(n/2)%3])
				out = append(out, seq)
				n++
			}
		}
	}
	return out
}

func TestKachinukiBoutOnePermutationGate(t *testing.T) {
	gin.SetMode(gin.TestMode)
	dir, err := os.MkdirTemp("", "kachinuki-gate-*")
	require.NoError(t, err)
	t.Cleanup(func() { os.RemoveAll(dir) })
	store, err := state.NewStore(dir)
	require.NoError(t, err)
	require.NoError(t, store.SaveTournament(&state.Tournament{Name: "Gate", Password: "secret"}))
	eng := engine.New(store)
	hub := NewHub()
	t.Cleanup(hub.Close)
	verifier := NewFileVerifier(store)
	r := gin.New()
	admin := r.Group("/api")
	admin.Use(AuthMiddleware(verifier, store))
	RegisterMatchHandlers(admin, eng, store, store, hub, verifier, store)
	RegisterLineupHandlers(admin, store, store, store, hub, store, verifier, eng)

	type ordering struct {
		legacy bool
		ops    []string
	}
	var all []ordering
	for _, legacy := range []bool{false, true} {
		for _, ops := range gateOrderings(legacy) {
			all = append(all, ordering{legacy, ops})
		}
	}
	// Each ordering is its own competition, so they run side by side in
	// chunks; the failures are counted together.
	var mu sync.Mutex
	failures := map[string]int{}
	const chunks = 8
	t.Run("orderings", func(t *testing.T) {
		for c := range chunks {
			t.Run(fmt.Sprint(c), func(t *testing.T) {
				t.Parallel()
				local := map[string]int{}
				for i := c; i < len(all); i += chunks {
					w := newGateWorld(t, r, store, eng, fmt.Sprintf("g%d", i), all[i].legacy, local)
					for _, op := range all[i].ops {
						w.step(op)
					}
				}
				mu.Lock()
				defer mu.Unlock()
				for k, v := range local {
					failures[k] += v
				}
			})
		}
	})
	kinds := make([]string, 0, len(failures))
	for k := range failures {
		kinds = append(kinds, k)
	}
	sort.Strings(kinds)
	for _, k := range kinds {
		t.Errorf("%s: %d failed assertions", k, failures[k])
	}
	t.Logf("%d orderings driven", len(all))
}

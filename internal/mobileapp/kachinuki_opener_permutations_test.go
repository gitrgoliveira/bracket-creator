package mobileapp

// The permutation gate for who names bout 1 of a kachinuki encounter (operator
// decision K1 (c), engine/kachinuki_opener.go) and for the closed-match rule's
// D5 around it. Every ordering of the writes that touch them is driven through
// the real doors, and the one rule is asserted after every step:
//
//  1. bout 1's fighter on each side is the first fighter of that match's
//     lineup: its own saved lineup, else the lineup it started with (or the
//     snapshot "Use the previous match's lineup" took), else (while it has
//     not started) the lineup in force by the carry rule; a match started
//     before this rule is renamed only by an edit of its own lineup; a match
//     sent back to the queue keeps following only its own lineup;
//  2. no operator tap is refused for a fighter, and every tap ends as the
//     closed-match rule says (a tap stamped before a finish that did not see
//     it is turned away, beforeBaseline; one stamped after it, matchClosed);
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
// start (stamped before it) counts for the match whichever way it arrives; an
// unstamped one (an older build's) is ordered by its arrival.

import (
	"encoding/json"
	"fmt"
	"math"
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
	opUnstamped    = "starting lineup save with no stamp (an older build)"
	opStart        = "start"
	opHeldStart    = "start held (stamped before the start that stands)"
	opOwn          = "own lineup save"
	opUsePrevious  = "Use the previous match's lineup"
	opTap1         = "device 1 strikes bout 1"
	opTap2         = "device 2 strikes bout 1"
	opPick2        = "pick on bout 2"
	opRemove       = "Remove this bout"
	opReadd        = "bout 2 added again"
	opSendBack     = "send back to the queue"
	opRestart      = "start again"
	opFinish       = "finish"
	opLate         = "late tap stamped after the finish"
	opLateEarly    = "late tap stamped before the finish"
	opRefused      = "tap stamped before the start"
	opSpoof        = "client batch under the server's session"
	opEarly0       = "own lineup save of the first match, stamped before the start"
	opEarly1       = "own lineup save of the second match, stamped before the start"
	opDelEarly0    = "the first match's own lineup taken away, stamped as it lands"
)

// The match the gate drives is team A's third; the two before it are never
// started, and their own lineups, saved before its start, are what the carry
// rule gave it at its start whatever order they arrive in (stage 6 review 5).
const gateEarly0 = "Pool A-0"
const gateEarly1 = "Pool A-1"
const gateMatch = "Pool A-2"
const gateLater = "Pool A-3"

// gateEarlyName is the first fighter an earlier match's own lineup names.
var gateEarlyName = map[string]string{gateEarly0: "Endo", gateEarly1: "Hara"}

// gateEarlyRank is match order among the lineups before the gate's match: the
// starting lineup, then each earlier match's own.
var gateEarlyRank = map[string]int{"": 0, gateEarly0: 1, gateEarly1: 2}

// gateLineup is the model of the lineup record match 1 holds for team A: an
// operator's save, the lineup it started with, or the snapshot "Use the
// previous match's lineup" took.
type gateLineup struct {
	name      string
	pin       bool
	startedAt int64  // a pin's start; a change stamped before it reaches it
	stamp     int64  // the stamp of the change that put name there
	from      string // a pin's source: "" (the starting lineup) or an earlier match
}

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
	// The model: team A's starting lineup's first fighter, the latest change
	// BY STAMP (operator decision L1; an unstamped save is stamped with the
	// server's time as it lands), with its stamp, every change of it that has
	// arrived, and match 1's own record.
	startingNow      string
	startingNowStamp int64
	startingSaves    []gateLineup
	own              *gateLineup
	finished         *state.MatchResult
	finishStamp      int64
	// earlier is the latest match, in match order, before the gate's match
	// whose own lineup was saved ("" for none).
	earlier string
	// lateEarlySeen is the log's length just before the late tap stamped
	// before the finish arrived ahead of the finish (-1 otherwise): the
	// finish's basis then holds every line but that tap's.
	lateEarlySeen int
	failures      map[string]int
	// kinds is the kinds of assertion this ordering failed, counted once
	// per ordering under "orderings: <kind>".
	kinds map[string]bool
}

func (w *gateWorld) stamp(op string) int64 {
	switch op {
	case opRefused:
		return w.base + 150
	case opStartingPre:
		return w.base + 100
	case opHeldStart:
		return w.base + 190
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
	case opLateEarly:
		return w.base + 450
	case opOwn:
		return w.base + 500
	case opPick2:
		return w.base + 600
	case opEarly0:
		return w.base + 50
	case opEarly1:
		return w.base + 30
	}
	// Made after a server write (the removal's tombstone, the send back, the
	// finish): now.
	return time.Now().UnixMilli()
}

func (w *gateWorld) fail(kind, format string, args ...any) {
	w.failures[kind]++
	if !w.kinds[kind] {
		w.kinds[kind] = true
		w.failures["orderings: "+kind]++
	}
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

func (w *gateWorld) started() bool { return w.legacy || w.arrived[opStart] }

func (w *gateWorld) saveStarting(name string, stamp int64, op string) {
	pos := domain.PositionNumbered(1)
	body := map[string]any{
		"positions": map[domain.Position]string{pos: name}, "memberIds": map[domain.Position]string{pos: w.members[name]},
		"changed": []domain.Position{pos}, "writeId": w.comp + op,
	}
	if stamp > 0 {
		body["changedAt"] = map[domain.Position]int64{pos: stamp}
	}
	rec := w.do(http.MethodPut, "/api/competitions/"+w.comp+"/teams/"+w.teamA+"/lineups/0", body)
	require.Equal(w.t, http.StatusOK, rec.Code, rec.Body.String())
	if stamp <= 0 {
		// Stamped with the server's time as it landed, after every stamp
		// this gate's devices made: it holds the position now.
		lineups, err := w.store.LoadTeamLineups(w.comp)
		require.NoError(w.t, err)
		for _, l := range lineups {
			if l.TeamID == w.teamA && l.MatchID == "" && l.Round == 0 {
				stamp = l.Stamps[pos]
			}
		}
		require.Positive(w.t, stamp, "an unstamped change is stamped as it lands")
	}
	w.startingSaves = append(w.startingSaves, gateLineup{name: name, stamp: stamp})
	// The position holds the latest change by stamp, whichever arrived last.
	w.startingNow, w.startingNowStamp = w.startingAt(math.MaxInt64)
	// A pin taken from the starting lineup holds what it held at the pin's
	// stamp, whatever order the changes arrive in.
	if w.own != nil && w.own.pin && w.own.from == "" {
		w.own.name, w.own.stamp = w.startingAt(w.own.startedAt)
	}
}

// startingAt is the starting lineup's first fighter at stamp at: of the
// changes arrived so far (and the one it began with, stamp 0), the latest
// made before at.
func (w *gateWorld) startingAt(at int64) (string, int64) {
	name, stamp := "Sato", int64(0)
	for _, s := range w.startingSaves {
		if s.stamp < at && s.stamp > stamp {
			name, stamp = s.name, s.stamp
		}
	}
	return name, stamp
}

// inForce is the lineup the carry rule gives the gate's match now, before any
// lineup of its own: the latest earlier match's own lineup, else the starting
// lineup.
func (w *gateWorld) inForce() gateLineup {
	if w.earlier != "" {
		return gateLineup{name: gateEarlyName[w.earlier], from: w.earlier}
	}
	return gateLineup{name: w.startingNow, stamp: w.startingNowStamp}
}

// earlierNow is the latest earlier match whose own lineup is in force now:
// one taken away is not (its removal is stamped as it lands, after every
// device change, so it reaches no lineup carried before it, the pin
// included, which inForce models).
func (w *gateWorld) earlierNow() string {
	switch {
	case w.arrived[opEarly1]:
		return gateEarly1
	case w.arrived[opEarly0] && !w.arrived[opDelEarly0]:
		return gateEarly0
	}
	return ""
}

// earlierAt is the latest earlier match whose own lineup was in force at
// stamp at: saved (every save here is stamped before at), and not taken away
// at or before at (a removal is in force from its stamp on).
func (w *gateWorld) earlierAt(at int64) string {
	switch {
	case w.arrived[opEarly1]:
		return gateEarly1
	case w.arrived[opEarly0]:
		if r := w.removedAt(gateEarly0); r == 0 || r >= at {
			return gateEarly0
		}
	}
	return ""
}

// removedAt is the stamp of the latest removal of team A's own lineup of
// match, as the store keeps it (0 for none).
func (w *gateWorld) removedAt(match string) int64 {
	all, err := w.store.LoadTeamLineupsWithRemoved(w.comp)
	require.NoError(w.t, err)
	at := int64(0)
	for _, l := range all {
		if l.TeamID == w.teamA && l.MatchID == match && l.IsRemoved() {
			at = max(at, l.RemovedAt)
		}
	}
	return at
}

// saveEarlier saves an earlier match's own lineup, stamped before the start.
func (w *gateWorld) saveEarlier(match, op string) {
	pos := domain.PositionNumbered(1)
	name := gateEarlyName[match]
	rec := w.do(http.MethodPut, "/api/competitions/"+w.comp+"/teams/"+w.teamA+"/match-lineups/"+url.PathEscape(match), map[string]any{
		"positions": map[domain.Position]string{pos: name}, "memberIds": map[domain.Position]string{pos: w.members[name]},
		"changed": []domain.Position{pos}, "changedAt": map[domain.Position]int64{pos: w.stamp(op)}, "writeId": w.comp + op,
	})
	require.Equal(w.t, http.StatusOK, rec.Code, rec.Body.String())
	if gateEarlyRank[match] > gateEarlyRank[w.earlier] {
		w.earlier = match
	}
	// Saved before the start, it reaches a pin whenever it arrives: the pin
	// is the carry rule at its start, so a later lineup in match order than
	// the one it holds replaces it whole.
	if w.own != nil && w.own.pin && gateEarlyRank[match] > gateEarlyRank[w.own.from] {
		w.own.name, w.own.from, w.own.stamp = name, match, w.stamp(op)
	}
}

func (w *gateWorld) start(op string, stamp int64) {
	m := w.stored(gateMatch)
	rec := w.do(http.MethodPut, w.matchPath(gateMatch)+"/score", map[string]any{
		"sideA": m.SideA, "sideB": m.SideB, "sideAId": m.SideAID, "sideBId": m.SideBID,
		"status": "running", "startOnly": true, "ipponsA": []string{}, "ipponsB": []string{},
		"changed": []string{state.GroupResult}, "modifiedAt": stamp, "writeId": w.comp + op,
	})
	require.Equal(w.t, http.StatusOK, rec.Code, rec.Body.String())
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
	case opUnstamped:
		w.saveStarting("Sakai", 0, op)
	case opStart:
		w.start(op, w.stamp(op))
		if w.own == nil {
			at := w.stamp(op)
			in := w.inForce()
			if r := w.removedAt(gateMatch); r > at {
				// "Use the previous match's lineup" is stamped as it lands,
				// after the start: in stamp order the match started and then
				// kept the lineup in force at that removal as its own
				// (stage 6 review 8, m1).
				at = r
				in = gateLineup{}
				if e := w.earlierAt(at); e != "" {
					in = gateLineup{name: gateEarlyName[e], from: e}
				}
			}
			if in.from == "" {
				// The carry rule at the start, by stamp.
				in.name, in.stamp = w.startingAt(at)
			}
			w.own = &gateLineup{name: in.name, pin: true, startedAt: at, stamp: in.stamp, from: in.from}
		}
	case opEarly0:
		w.saveEarlier(gateEarly0, op)
	case opEarly1:
		w.saveEarlier(gateEarly1, op)
	case opDelEarly0:
		// Stamped as it lands, after every device change here, the start
		// included: the lineup the match started with keeps reading it.
		rec := w.do(http.MethodDelete, "/api/competitions/"+w.comp+"/teams/"+w.teamA+"/match-lineups/"+url.PathEscape(gateEarly0), map[string]any{"writeId": w.comp + op})
		require.Equal(t, http.StatusNoContent, rec.Code, rec.Body.String())
	case opHeldStart:
		before := w.stored(gateMatch)
		w.start(op, w.stamp(op))
		if after := w.stored(gateMatch); after.Status != before.Status || boutName(after, 1, "a") != boutName(before, 1, "a") {
			w.fail("held start", "a held start moved the match: %s -> %s", before.Status, after.Status)
		}
	case opRestart:
		w.start(op, w.stamp(op))
	case opSendBack:
		rec := w.do(http.MethodPost, w.matchPath(gateMatch)+"/revert-to-queue", map[string]any{"modifiedAt": w.stamp(op), "writeId": w.comp + op})
		require.Equal(t, http.StatusOK, rec.Code, rec.Body.String())
		require.Equal(t, state.MatchStatusScheduled, w.stored(gateMatch).Status)
	case opOwn:
		pos := domain.PositionNumbered(1)
		rec := w.do(http.MethodPut, "/api/competitions/"+w.comp+"/teams/"+w.teamA+"/match-lineups/"+url.PathEscape(gateMatch), map[string]any{
			"positions": map[domain.Position]string{pos: "Ito"}, "memberIds": map[domain.Position]string{pos: w.members["Ito"]},
			"changed": []domain.Position{pos}, "changedAt": map[domain.Position]int64{pos: w.stamp(op)}, "writeId": w.comp + op,
		})
		require.Equal(t, http.StatusOK, rec.Code, rec.Body.String())
		// Ordered by its stamp against the removal (stage 6 review 8, m1):
		// "Use the previous match's lineup" is stamped as it lands, after
		// every device change here, so a save of the match's own lineup
		// arriving after it was made before it and is taken away by it.
		if !w.arrived[opUsePrevious] {
			w.own = &gateLineup{name: "Ito"}
		}
	case opUsePrevious:
		rec := w.do(http.MethodDelete, "/api/competitions/"+w.comp+"/teams/"+w.teamA+"/match-lineups/"+url.PathEscape(gateMatch), map[string]any{"writeId": w.comp + op})
		require.Equal(t, http.StatusNoContent, rec.Code, rec.Body.String())
		w.own = nil
		if w.started() {
			// A snapshot of the lineup in force now, kept as the match's own,
			// at the server's time (read back from the store).
			at := int64(0)
			lineups, err := w.store.LoadTeamLineups(w.comp)
			require.NoError(t, err)
			for _, l := range lineups {
				if l.TeamID == w.teamA && l.MatchID == gateMatch && l.IsStartPin() {
					at = l.StartedAt
				}
			}
			require.Positive(t, at, "the snapshot is kept as a pin")
			// In force now: a lineup taken away before the snapshot is not.
			in := gateLineup{name: w.startingNow, stamp: w.startingNowStamp}
			if e := w.earlierNow(); e != "" {
				in = gateLineup{name: gateEarlyName[e], from: e}
			}
			if in.from == "" {
				in.name, in.stamp = w.startingAt(at)
			}
			w.own = &gateLineup{name: in.name, pin: true, startedAt: at, stamp: in.stamp, from: in.from}
		}
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
		w.finishStamp = w.stamp(op)
		// A finish as a device sends one: naming the result alone, with a
		// basis holding every line the board showed.
		// The board the finishing court showed: every line, but a late tap
		// that reached the server just before the finish (it never saw it).
		seen := w.logLen()
		if w.lateEarlySeen >= 0 {
			seen = w.lateEarlySeen
		}
		rec := w.do(http.MethodPut, w.matchPath(gateMatch)+"/score", map[string]any{
			"sideA": m.SideA, "sideB": m.SideB, "sideAId": m.SideAID, "sideBId": m.SideBID,
			"status": "completed", "decision": "hikiwake",
			"ipponsA": []string{}, "ipponsB": []string{}, "subResults": m.SubResults,
			"changed": []string{state.GroupResult}, "modifiedAt": w.finishStamp, "writeId": w.comp + op,
			"session": "dev9", "basis": map[string]any{"seen": seen},
		})
		require.Equal(t, http.StatusOK, rec.Code, rec.Body.String())
		f := w.stored(gateMatch)
		require.Equal(t, state.MatchStatusCompleted, f.Status, "the finish lands")
		w.finished = &f
	case opLate, opLateEarly:
		stamp, session, seq := max(w.stamp(op), w.finishStamp+1), "dev2", int64(2)
		want := domain.ScoreOutcomeMatchClosed
		if op == opLateEarly {
			stamp, session, seq, want = w.stamp(op), "dev4", 1, domain.ScoreOutcomeBeforeBaseline
			if !w.arrived[opFinish] {
				// Ahead of the finish it lands, and is turned away when the
				// finish arrives (check() reads its history line and the
				// match's turnedAway after every step).
				want = domain.ScoreOutcomeApplied
				w.lateEarlySeen = w.logLen()
			}
		}
		code, outcome := w.tap(session, domain.ScoreEvent{ID: w.comp + op, Seq: seq, Stamp: stamp, Kind: domain.ScoreIpponAdd, Pos: 1, Side: "b", Letter: "D"})
		require.Equal(t, http.StatusOK, code)
		if outcome != want {
			w.fail("late tap", "%s answered %q, want %s", op, outcome, want)
		}
	case opRefused:
		// A foul, so it never decides bout 1 ahead of the strikes.
		code, outcome := w.tap("dev3", domain.ScoreEvent{ID: w.comp + op, Seq: 1, Stamp: w.stamp(op), Kind: domain.ScoreFoulAdd, Pos: 1, Side: "a"})
		require.Equal(t, http.StatusOK, code)
		want := domain.ScoreOutcomeApplied
		switch {
		case w.legacy:
			want = domain.ScoreOutcomeBeforeBaseline // stamped before the stored match
		case !w.arrived[opStart]:
			want = domain.ScoreOutcomeNotStarted
		case w.arrived[opFinish]:
			want = domain.ScoreOutcomeBeforeBaseline // the finish never saw it
		}
		if outcome != want {
			w.fail("tap before the start", "answered %q, want %s", outcome, want)
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
		case w.own != nil:
			wantA = w.own.name
		case w.legacy:
			wantA = "Aoki"
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
	switch {
	case w.own != nil && !w.own.pin:
		wantLater, wantSource = w.own.name, gateMatch
	case w.earlierNow() != "":
		wantLater, wantSource = gateEarlyName[w.earlierNow()], w.earlierNow()
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
	// 7. A late tap ends as the closed-match rule says, in the match's
	// history (its latest line) and on the match, whose turnedAway tells the
	// device that made it when its own answer said it landed (review 5).
	if w.arrived[opLate] || w.arrived[opLateEarly] {
		hist, err := w.store.LoadMatchHistory(w.comp, gateMatch)
		require.NoError(t, err)
		last := map[string]string{}
		for _, e := range hist {
			if e.Event != nil {
				last[e.Event.ID] = e.EventOutcome
			}
		}
		away := w.stored(gateMatch).TurnedAway
		for _, op := range []string{opLate, opLateEarly} {
			if !w.arrived[op] {
				continue
			}
			want := domain.ScoreOutcomeMatchClosed
			if op == opLateEarly {
				want = domain.ScoreOutcomeBeforeBaseline
				if !w.arrived[opFinish] {
					want = domain.ScoreOutcomeApplied
				}
			}
			id := w.comp + op
			if last[id] != want {
				w.fail("late tap history", "%s: its latest history line reads %q, want %s", op, last[id], want)
			}
			if got, listed := away[id]; (want == domain.ScoreOutcomeApplied) == listed || (listed && got != want) {
				w.fail("late tap turned away", "%s: the match lists it turned away as %q (listed %v), want %s", op, got, listed, want)
			}
		}
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
		members: map[string]string{}, arrived: map[string]bool{}, failures: failures, kinds: map[string]bool{},
		base: time.Now().UnixMilli() - 600_000, startingNow: "Sato", lateEarlySeen: -1}
	for team, names := range map[string][]string{ids["Tora"]: {"Sato", "Kato", "Mori", "Ito", "Kubo", "Aoki", "Sakai", "Endo", "Hara"}, ids["Washi"]: {"Fujii"}, ids["Kuma"]: {"Goto"}} {
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
	}
	require.NoError(t, store.SavePoolMatches(comp, []state.MatchResult{
		{ID: gateEarly0, SideA: "Tora", SideB: "Kuma", SideAID: ids["Tora"], SideBID: ids["Kuma"], Status: state.MatchStatusScheduled},
		{ID: gateEarly1, SideA: "Tora", SideB: "Kuma", SideAID: ids["Tora"], SideBID: ids["Kuma"], Status: state.MatchStatusScheduled},
		first,
		{ID: gateLater, SideA: "Tora", SideB: "Kuma", SideAID: ids["Tora"], SideBID: ids["Kuma"], Status: state.MatchStatusScheduled},
	}))
	return w
}

// gateOrderings is every ordering the gate drives. The scoring steps keep the
// order a court imposes (the start before the taps, a removal after the pick
// it removes and before the bout is added again, a send back and the start
// again together before the finish, the finish after the bouts, the late taps
// after the finish), with the two devices' taps in both orders, the bout 2
// pick between or after them, and the send back present or not. The three
// stamped lineup saves are placed before the start, once the match is under
// way, just before the finish and at the end, in every order among
// themselves. The rest come at places that turn with each ordering, so every
// place is driven: the unstamped save, Use the previous match's lineup, the
// held start, the tap stamped before the start (before or after the start, or
// after the finish), the spoofed batch, and the tap stamped before the finish
// that arrives after it.
func gateOrderings(legacy bool) [][]string {
	var cores [][]string
	for _, sendBack := range []bool{false, true} {
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
				if sendBack {
					c = append(c, opSendBack, opRestart)
				}
				c = append(c, opFinish, opLate)
				cores = append(cores, c)
			}
		}
	}
	saves := []string{opStartingPre, opStartingPost, opOwn}
	place := func(s []string, op string, at int) []string {
		at = max(0, min(at, len(s)))
		r := make([]string, 0, len(s)+1)
		r = append(r, s[:at]...)
		r = append(r, op)
		return append(r, s[at:]...)
	}
	after := func(s []string, op string) int { return slices.Index(s, op) + 1 }
	var out [][]string
	n := 0
	for _, core := range cores {
		finish := slices.Index(core, opFinish)
		gaps := []int{0, 1, finish, len(core)}
		seen := map[string]bool{}
		for _, perm := range [][]int{{0, 1, 2}, {0, 2, 1}, {1, 0, 2}, {1, 2, 0}, {2, 0, 1}, {2, 1, 0}} {
			for g := 0; g < len(gaps)*len(gaps)*len(gaps); g++ {
				at := []int{gaps[g%4], gaps[(g/4)%4], gaps[g/16]}
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
				// The turning places, each on its own radix of n. First the
				// own lineups of the two matches before the gate's, both
				// stamped before its start, arriving before it or after it, in
				// either order (stage 6 review 5).
				// After the start (a legacy match started before the gate:
				// after its first tap).
				late := func() int {
					if legacy {
						return after(seq, opTap1)
					}
					return after(seq, opStart)
				}
				switch (n / 7) % 7 {
				case 1:
					seq = place(seq, opEarly0, 0)
				case 2:
					seq = place(seq, opEarly0, late())
				case 3:
					seq = place(seq, opEarly0, late())
					seq = place(seq, opEarly1, after(seq, opEarly0))
				case 4:
					seq = place(seq, opEarly1, late())
					seq = place(seq, opEarly0, after(seq, opEarly1))
				case 5:
					seq = place(seq, opEarly1, 0)
					seq = place(seq, opEarly0, late())
				case 6:
					seq = place(seq, opEarly1, late())
					seq = place(seq, opEarly0, len(seq))
				}
				if c := (n / 2) % 4; c > 0 {
					seq = place(seq, opUnstamped, []int{0, 0, after(seq, opTap1), len(seq)}[c])
				}
				// The first match's own lineup taken away (review 7): just
				// after its save, or at the end.
				if c := (n / 11) % 3; c > 0 && slices.Contains(seq, opEarly0) {
					seq = place(seq, opDelEarly0, []int{0, after(seq, opEarly0), len(seq)}[c])
				}
				if c := (n / 8) % 4; c > 0 {
					seq = place(seq, opUsePrevious, []int{0, 0, after(seq, opTap1), slices.Index(seq, opFinish)}[c])
				}
				if !legacy {
					if c := (n / 32) % 3; c > 0 {
						seq = place(seq, opHeldStart, []int{0, after(seq, opStart), after(seq, opFinish)}[c])
					}
					seq = place(seq, opRefused, []int{0, slices.Index(seq, opStart), after(seq, opStart), after(seq, opFinish)}[(n/96)%4])
				}
				seq = place(seq, opSpoof, []int{0, len(seq) / 2, len(seq)}[(n/3)%3])
				// The late tap stamped before the finish: after the finish or
				// at the end, and for every third ordering also just ahead of
				// the finish, which then never saw it (review 5).
				out = append(out, place(seq, opLateEarly, []int{after(seq, opFinish), len(seq)}[n%2]))
				if n%3 == 0 {
					out = append(out, place(seq, opLateEarly, slices.Index(seq, opFinish)))
				}
				n++
			}
		}
	}
	return out
}

// gateRaceKeep is how many of the gate's orderings run under the race detector
// (raceSample): about a tenth of the 2560; every one runs without it.
const gateRaceKeep = 256

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
	var every []ordering
	for _, legacy := range []bool{false, true} {
		for _, ops := range gateOrderings(legacy) {
			every = append(every, ordering{legacy, ops})
		}
	}
	all := raceSample(every, gateRaceKeep)
	// A sample still drives every operation the orderings hold, in both
	// kinds of match, at least once.
	opsOf := func(set []ordering) map[string]bool {
		seen := map[string]bool{}
		for _, o := range set {
			for _, op := range o.ops {
				seen[fmt.Sprint(o.legacy, op)] = true
			}
		}
		return seen
	}
	require.Equal(t, opsOf(every), opsOf(all), "the race sample drives every operation of the orderings")
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
	t.Logf("%d of %d orderings driven", len(all), len(every))
}

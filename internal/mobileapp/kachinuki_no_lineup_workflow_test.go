package mobileapp

import (
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"net/url"
	"testing"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/gitrgoliveira/bracket-creator/internal/domain"
	"github.com/gitrgoliveira/bracket-creator/internal/engine"
	"github.com/gitrgoliveira/bracket-creator/internal/helper"
	"github.com/gitrgoliveira/bracket-creator/internal/state"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// The workflow bc-kno1 serves (operator decision K1 (a), Q1, Q4 (b)): a court
// operator opens a kachinuki encounter with no lineup saved, scores bout 1 and
// taps Record bout. The server fields each team's members by member number,
// member 1 first and up to the team size (the reserves only when picked), so
// bout 1 is each team's member 1, Record bout appends the loser's next member
// against the winner, and the last member's recorded loss reads "no fighters
// left" (the roster's exhausted). The members are the store's own seeding:
// nameless, so every fighter is named by id alone and a nameless winner is
// recorded as the TEAM name plus the member id (bc-dnst). Driven through the
// real doors as the organiser at the court (an officiated tournament) and as an
// anonymous participant on the public page (a self-run one, empty password).

type noLineupWorld struct {
	t        *testing.T
	r        *gin.Engine
	store    *state.Store
	password string
	comp     string
	match    string
	teamA    string
	teamB    string
	// a and b are each team's members by member number (1-based: a[1] is
	// team A's member 1).
	a, b  map[int]string
	stamp int64
}

const noLineupTeamSize = 3

func newNoLineupWorld(t *testing.T, selfRun bool) *noLineupWorld {
	t.Helper()
	store := newTempStore(t)
	password := "main-pw"
	if selfRun {
		seedSelfRunTournament(t, store)
		password = ""
	} else {
		seedOfficiatedTournament(t, store)
	}
	w := &noLineupWorld{t: t, store: store, password: password, comp: "knl", match: "Pool A-0",
		teamA: helper.NewUUID4(), teamB: helper.NewUUID4(), stamp: time.Now().UnixMilli() - 60_000}
	require.NoError(t, store.SaveCompetition(&state.Competition{
		ID: w.comp, Name: "Kachinuki", Kind: "team", Format: state.CompFormatLeague, Status: state.CompStatusPools,
		TeamSize: noLineupTeamSize, TeamMatchType: state.TeamMatchTypeKachinuki,
	}))
	require.NoError(t, store.SaveParticipants(w.comp, []domain.Player{
		{ID: w.teamA, Name: "Tora", Dojo: "Tora Dojo"},
		{ID: w.teamB, Name: "Washi", Dojo: "Washi Dojo"},
	}))
	require.NoError(t, store.SavePoolMatches(w.comp, []state.MatchResult{{
		ID: w.match, SideA: "Tora", SideB: "Washi", SideAID: w.teamA, SideBID: w.teamB,
		Status: state.MatchStatusScheduled, Court: "A",
	}}))
	w.a, w.b = map[int]string{}, map[int]string{}
	for n := 1; n <= noLineupTeamSize+state.SquadReserveSlots; n++ {
		w.a[n] = seededMember(t, store, w.comp, w.teamA, n).ID
		w.b[n] = seededMember(t, store, w.comp, w.teamB, n).ID
	}
	lineups, err := store.LoadTeamLineups(w.comp)
	require.NoError(t, err)
	require.Empty(t, lineups, "no lineup is saved")
	w.r = setupSelfRunRouter(t, store, NewFileVerifier(store))
	return w
}

func (w *noLineupWorld) path(suffix string) string {
	return "/api/competitions/" + w.comp + "/matches/" + url.PathEscape(w.match) + suffix
}

func (w *noLineupWorld) send(method, path string, body any) *httptest.ResponseRecorder {
	w.t.Helper()
	req := jsonReq(method, path, body)
	req.Header.Set("X-Tournament-Password", w.password)
	rec := httptest.NewRecorder()
	w.r.ServeHTTP(rec, req)
	return rec
}

func (w *noLineupWorld) next() int64 {
	w.stamp += 1_000
	return w.stamp
}

func (w *noLineupWorld) stored() state.MatchResult {
	w.t.Helper()
	ms, err := w.store.LoadPoolMatches(w.comp)
	require.NoError(w.t, err)
	require.Len(w.t, ms, 1)
	return ms[0]
}

// start starts the match from scheduled, as the court does, so the start pins
// each team's lineup in force: none, an empty pin.
func (w *noLineupWorld) start() {
	w.t.Helper()
	rec := w.send(http.MethodPut, w.path("/score"), map[string]any{
		"sideA": "Tora", "sideB": "Washi", "sideAId": w.teamA, "sideBId": w.teamB,
		"status": "running", "startOnly": true, "ipponsA": []string{}, "ipponsB": []string{},
		"changed": []string{state.GroupResult}, "modifiedAt": w.next(),
	})
	require.Equal(w.t, http.StatusOK, rec.Code, "the start: %s", rec.Body.String())
	require.Equal(w.t, state.MatchStatusRunning, w.stored().Status)
}

// recordBout sends the sheet's Record bout: every bout so far as the match
// holds it, with the live bout won by side A on two points. The nameless
// winner is recorded as the team name plus the member id, as the sheet sends
// it, beside the fighters' ids the server's answer gave the row. withIDs false
// sends bout 1 naming no fighter at all, its winner by the team name alone (a
// sheet that had not yet read who the server seats there): the seat owns bout
// 1. Every later bout is a pairing the server appended, and the sheet sends it
// as the answer gave it.
func (w *noLineupWorld) recordBout(withIDs bool) state.MatchResult {
	w.t.Helper()
	m := w.stored()
	rows := make([]map[string]any, 0, len(m.SubResults)+1)
	for _, s := range m.SubResults {
		row := map[string]any{
			"position": s.Position, "sideA": s.SideA, "sideB": s.SideB,
			"ipponsA": s.IpponsA, "ipponsB": s.IpponsB, "winner": s.Winner, "decision": s.Decision,
			"sideAMemberId": s.SideAMemberID, "sideBMemberId": s.SideBMemberID, "winnerMemberId": s.WinnerMemberID,
		}
		if s.IpponsA == nil {
			row["ipponsA"] = []string{}
		}
		if s.IpponsB == nil {
			row["ipponsB"] = []string{}
		}
		rows = append(rows, row)
	}
	live := len(rows) - 1
	if live < 0 {
		rows = append(rows, map[string]any{"position": 1, "sideA": "", "sideB": "", "ipponsB": []string{}})
		live = 0
	}
	rows[live]["ipponsA"] = []string{"M", "K"}
	rows[live]["winner"] = "Tora"
	rows[live]["decision"] = "fought"
	if withIDs || live > 0 {
		rows[live]["winnerMemberId"] = rows[live]["sideAMemberId"]
	} else {
		delete(rows[live], "sideAMemberId")
		delete(rows[live], "sideBMemberId")
		delete(rows[live], "winnerMemberId")
	}
	// The sheet stamps its write after the match it shows (bc-hlck: the Record
	// bout answer carries the advance's stamp, which is the server's time).
	w.stamp = max(w.next(), m.ModifiedAt+1)
	rec := w.send(http.MethodPut, w.path("/score"), map[string]any{
		"sideA": "Tora", "sideB": "Washi", "sideAId": w.teamA, "sideBId": w.teamB,
		"status": "running", "kachinukiBoutFinal": true, "modifiedAt": w.stamp,
		"ipponsA": []string{}, "ipponsB": []string{}, "subResults": rows,
	})
	require.Equal(w.t, http.StatusOK, rec.Code, "Record bout: %s", rec.Body.String())
	return w.stored()
}

type noLineupRoster struct {
	SideA       noLineupRosterSide `json:"sideA"`
	SideB       noLineupRosterSide `json:"sideB"`
	NextPairing bool               `json:"nextPairing"`
	Warning     string             `json:"warning"`
}

type noLineupRosterSide struct {
	LineupFound bool                            `json:"lineupFound"`
	On          *engine.KachinukiRosterFighter  `json:"on"`
	Remaining   []engine.KachinukiRosterFighter `json:"remaining"`
	Exhausted   bool                            `json:"exhausted"`
}

func (w *noLineupWorld) roster() noLineupRoster {
	w.t.Helper()
	rec := w.send(http.MethodGet, w.path("/kachinuki-roster"), nil)
	require.Equal(w.t, http.StatusOK, rec.Code, rec.Body.String())
	var out noLineupRoster
	require.NoError(w.t, json.Unmarshal(rec.Body.Bytes(), &out), rec.Body.String())
	return out
}

func remainingIDs(side noLineupRosterSide) []string {
	ids := []string{}
	for _, f := range side.Remaining {
		ids = append(ids, f.MemberID)
	}
	return ids
}

// requireBout checks bout pos pairs side A's member na against side B's member
// nb, both nameless, so named by id alone.
func (w *noLineupWorld) requireBout(m state.MatchResult, pos, na, nb int) {
	w.t.Helper()
	row := state.SubResultAt(m.SubResults, pos)
	require.Equal(w.t, pos, row.Position, "bout %d is stored", pos)
	assert.Equal(w.t, w.a[na], row.SideAMemberID, "bout %d: side A is team A's member %d", pos, na)
	assert.Equal(w.t, w.b[nb], row.SideBMemberID, "bout %d: side B is team B's member %d", pos, nb)
	assert.Empty(w.t, row.SideA+row.SideB, "bout %d: nameless members are named by id alone", pos)
}

func TestKachinukiNoLineupWorkflow_FieldsMembersByNumber(t *testing.T) {
	for _, tc := range []struct {
		name    string
		selfRun bool
		withIDs bool
	}{
		{"organiser at the court, the sheet sending the seated ids", false, true},
		{"organiser at the court, bout 1 naming no fighter", false, false},
		{"self-run participant, the sheet sending the seated ids", true, true},
		{"self-run participant, bout 1 naming no fighter", true, false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			w := newNoLineupWorld(t, tc.selfRun)
			w.start()

			// Nobody has fought: each side fields members 1 to the team size,
			// member 1 first, never the reserves.
			r := w.roster()
			assert.Empty(t, r.Warning)
			assert.True(t, r.SideA.LineupFound, "a team with no lineup fields its members by number")
			assert.True(t, r.SideB.LineupFound, "a team with no lineup fields its members by number")

			// Bout 1: team A's member 1 beats team B's member 1. Record bout
			// appends the loser's member 2 against the winner, who stays on.
			m := w.recordBout(tc.withIDs)
			require.Len(t, m.SubResults, 2, "Record bout appends bout 2")
			w.requireBout(m, 1, 1, 1)
			w.requireBout(m, 2, 1, 2)
			bout1 := state.SubResultAt(m.SubResults, 1)
			assert.Equal(t, "Tora", bout1.Winner, "a nameless winner is recorded as the team name")
			assert.Equal(t, w.a[1], bout1.WinnerMemberID, "and the member id")
			assert.True(t, bout1.Recorded)
			r = w.roster()
			require.NotNil(t, r.SideA.On)
			assert.Equal(t, w.a[1], r.SideA.On.MemberID, "the winner stays on")
			assert.Equal(t, []string{w.a[2], w.a[3]}, remainingIDs(r.SideA), "members 2 and 3 wait, never the reserves")
			require.NotNil(t, r.SideB.On)
			assert.Equal(t, w.b[2], r.SideB.On.MemberID)
			assert.Equal(t, []string{w.b[3]}, remainingIDs(r.SideB), "member 3 is team B's last; the reserves are never queued")
			assert.False(t, r.SideB.Exhausted)

			// Bout 2: member 1 beats member 2, Record bout appends member 3.
			m = w.recordBout(tc.withIDs)
			require.Len(t, m.SubResults, 3, "Record bout appends bout 3")
			w.requireBout(m, 3, 1, 3)

			// Bout 3: team B's last member loses. Nothing is appended, and the
			// roster reads that team B has no fighters left.
			m = w.recordBout(tc.withIDs)
			require.Len(t, m.SubResults, 3, "no fighter is left to pair")
			assert.True(t, state.SubResultAt(m.SubResults, 3).Recorded, "the last bout is recorded")
			r = w.roster()
			assert.Empty(t, r.Warning)
			assert.True(t, r.SideB.Exhausted, "no fighters left")
			assert.False(t, r.SideA.Exhausted)
			assert.False(t, r.NextPairing, "a further tap writes nothing")
			assert.Equal(t, []string{}, remainingIDs(r.SideB))
			assert.Equal(t, state.MatchStatusRunning, w.stored().Status, "operator-led: the encounter ends only on End match")
		})
	}
}

// A bout-1 pick goes through the match's lineup (the sheet saves it there), and
// the seat owns bout 1 (engine.ReseatKachinukiOpenersTx): a score write naming
// bout 1's fighters is overruled by it, and a lineup save that lands after the
// score write reseats bout 1. So both arrival orders of a pick on one device and
// a point on another (which still shows member 1 by number) leave the same bout
// 1: the picked member, the other side's member 1, and the point.
func TestKachinukiNoLineupWorkflow_ABoutOnePickAndAPointConvergeInBothOrders(t *testing.T) {
	boutOne := map[string]state.SubMatchResult{}
	for _, pickFirst := range []bool{true, false} {
		name := map[bool]string{true: "the pick lands before the point", false: "the pick lands after the point"}[pickFirst]
		t.Run(name, func(t *testing.T) {
			w := newNoLineupWorld(t, false)
			w.start()
			pickAt, pointAt := w.next(), w.next()
			pick := func() {
				pos := domain.PositionNumbered(1)
				rec := w.send(http.MethodPut, "/api/competitions/"+w.comp+"/teams/"+w.teamA+"/match-lineups/"+url.PathEscape(w.match), map[string]any{
					"positions": map[domain.Position]string{pos: ""}, "memberIds": map[domain.Position]string{pos: w.a[3]},
					"changed": []domain.Position{pos}, "changedAt": map[domain.Position]int64{pos: pickAt},
				})
				require.Equal(t, http.StatusOK, rec.Code, "the pick: %s", rec.Body.String())
			}
			point := func() {
				rec := w.send(http.MethodPut, w.path("/score"), map[string]any{
					"sideA": "Tora", "sideB": "Washi", "sideAId": w.teamA, "sideBId": w.teamB,
					"status": "running", "modifiedAt": pointAt, "ipponsA": []string{}, "ipponsB": []string{},
					"subResults": []map[string]any{{
						"position": 1, "sideA": "", "sideB": "", "ipponsA": []string{"M"}, "ipponsB": []string{},
						"winner": "Tora", "winnerMemberId": w.a[1], "sideAMemberId": w.a[1], "sideBMemberId": w.b[1],
					}},
				})
				require.Equal(t, http.StatusOK, rec.Code, "the point: %s", rec.Body.String())
			}
			if pickFirst {
				pick()
				point()
			} else {
				point()
				pick()
			}
			row := state.SubResultAt(w.stored().SubResults, 1)
			assert.Equal(t, w.a[3], row.SideAMemberID, "the pick holds bout 1")
			assert.Equal(t, w.b[1], row.SideBMemberID, "the other side keeps its member 1")
			assert.Empty(t, row.SideA+row.SideB)
			assert.Equal(t, []string{"M"}, row.IpponsA, "the point stays on bout 1")
			assert.Equal(t, "Tora", row.Winner)
			assert.Equal(t, w.a[3], row.WinnerMemberID, "the win follows side A's fighter, the picked member, not the member 1 the point named")
			// Each order is its own world, so the member ids differ: compare by
			// member number.
			label := map[string]string{}
			for n := range w.a {
				label[w.a[n]], label[w.b[n]] = fmt.Sprintf("A%d", n), fmt.Sprintf("B%d", n)
			}
			row.SideAMemberID, row.SideBMemberID, row.WinnerMemberID = label[row.SideAMemberID], label[row.SideBMemberID], label[row.WinnerMemberID]
			boutOne[name] = row
		})
	}
	require.Len(t, boutOne, 2)
	assert.Equal(t, boutOne["the pick lands before the point"], boutOne["the pick lands after the point"], "both orders leave the same bout 1")
}

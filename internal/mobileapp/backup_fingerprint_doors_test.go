package mobileapp

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/url"
	"sort"
	"strings"
	"testing"
	"time"

	"github.com/gitrgoliveira/bracket-creator/internal/domain"
	"github.com/gitrgoliveira/bracket-creator/internal/state"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// These tests drive each write door through the real handlers, take the
// snapshot before and after a write that changes nothing, and record which
// files that write still moves. Every file a no-op moves must be one the
// fingerprint normalises (state.FingerprintStampedFiles) or leaves out
// (state.FingerprintSkippedDirs), and the fingerprint must not change; a real
// change through the same door must change it.

type doorHarness struct {
	t     *testing.T
	pt    *populatedTournament
	moved map[string]bool // every file kind a no-op door moved
}

func (h *doorHarness) files() map[string][]byte {
	h.t.Helper()
	snap, err := h.pt.Store.Snapshot(state.ScopeTournament, "")
	require.NoError(h.t, err)
	return snap.Files
}

func (h *doorHarness) send(method, path string, body any) {
	h.t.Helper()
	w := populatedServeJSON(h.t, h.pt.Router, h.pt.Password, method, path, body)
	require.Less(h.t, w.Code, 300, "%s %s: %s", method, path, w.Body.String())
}

// fileKind names a snapshot path the way the stamp table does: a file inside
// a competition folder by its name, a file in one of its subfolders by the
// subfolder, anything else by its whole path.
func fileKind(p string) string {
	after, ok := strings.CutPrefix(p, "competitions/")
	if !ok {
		return p
	}
	_, rest, _ := strings.Cut(after, "/")
	if dir, _, nested := strings.Cut(rest, "/"); nested {
		return dir
	}
	return rest
}

func movedKinds(before, after map[string][]byte) []string {
	kinds := map[string]bool{}
	for p, b := range before {
		if a, ok := after[p]; !ok || !bytes.Equal(a, b) {
			kinds[fileKind(p)] = true
		}
	}
	for p := range after {
		if _, ok := before[p]; !ok {
			kinds[fileKind(p)] = true
		}
	}
	out := make([]string, 0, len(kinds))
	for k := range kinds {
		out = append(out, k)
	}
	sort.Strings(out)
	return out
}

func normalisedKind(kind string) bool {
	for _, sf := range state.FingerprintStampedFiles {
		if sf.Name == kind {
			return true
		}
	}
	for _, d := range state.FingerprintSkippedDirs {
		if d == kind {
			return true
		}
	}
	return false
}

// noOp sends a write that changes nothing and checks that it moved exactly
// wantMoved (the measured evidence), that each of those is normalised, and
// that the fingerprint held.
func (h *doorHarness) noOp(method, path string, body any, wantMoved ...string) {
	h.t.Helper()
	before := h.files()
	h.send(method, path, body)
	after := h.files()
	moved := movedKinds(before, after)
	if wantMoved == nil {
		wantMoved = []string{}
	}
	assert.ElementsMatch(h.t, wantMoved, moved, "files a no-op %s %s moved", method, path)
	for _, k := range moved {
		assert.True(h.t, normalisedKind(k), "%s moved on a no-op but the fingerprint neither normalises nor skips it", k)
		h.moved[k] = true
	}
	assert.Equal(h.t, state.Fingerprint(before), state.Fingerprint(after), "a no-op %s %s changed the fingerprint", method, path)
}

// real sends a write that changes something and checks the fingerprint moved.
func (h *doorHarness) real(method, path string, body any) {
	h.t.Helper()
	before := h.files()
	h.send(method, path, body)
	assert.NotEqual(h.t, state.Fingerprint(before), state.Fingerprint(h.files()), "a real change through %s %s kept the fingerprint", method, path)
}

func (h *doorHarness) poolMatch(compID, matchID string) state.MatchResult {
	h.t.Helper()
	pm, err := h.pt.Store.LoadPoolMatches(compID)
	require.NoError(h.t, err)
	for _, m := range pm {
		if m.ID == matchID {
			return m
		}
	}
	h.t.Fatalf("pool match %s not found in %s", matchID, compID)
	return state.MatchResult{}
}

func (h *doorHarness) bracketMatch(compID, matchID string) state.BracketMatch {
	h.t.Helper()
	b, err := h.pt.Store.LoadBracket(compID)
	require.NoError(h.t, err)
	for _, round := range b.Rounds {
		for _, m := range round {
			if m.ID == matchID {
				return m
			}
		}
	}
	h.t.Fatalf("bracket match %s not found in %s", matchID, compID)
	return state.BracketMatch{}
}

func scorePayload(id, sideA, sideB, sideAID, sideBID, winner, winnerID string, status state.MatchStatus, ipponsA, ipponsB []string, stamp int64) map[string]any {
	return map[string]any{
		"id": id, "sideA": sideA, "sideB": sideB, "sideAId": sideAID, "sideBId": sideBID,
		"winner": winner, "winnerId": winnerID, "ipponsA": ipponsA, "ipponsB": ipponsB,
		"status": string(status), "modifiedAt": stamp,
	}
}

func TestFingerprintDoors(t *testing.T) {
	pt := buildPopulatedTournament(t)
	h := &doorHarness{t: t, pt: pt, moved: map[string]bool{}}
	now := time.Now().UnixMilli()

	t.Run("tournament settings", func(t *testing.T) {
		h.t = t
		tour, err := pt.Store.LoadTournament()
		require.NoError(t, err)
		body := map[string]any{
			"name": tour.Name, "date": tour.Date, "venue": tour.Venue, "courts": tour.Courts,
			"password": pt.Password, "durationDays": tour.DurationDays,
			"clockToElapsedMultiplier": tour.ClockToElapsedMultiplier, "slowestCourtBufferPct": tour.SlowestCourtBufferPct,
			"mode": tour.Mode, "sponsors": tour.Sponsors,
		}
		h.noOp(http.MethodPut, "/api/tournament", body)
		body["venue"] = "North Budokan"
		h.real(http.MethodPut, "/api/tournament", body)
	})

	t.Run("competition settings", func(t *testing.T) {
		h.t = t
		comp, err := pt.Store.LoadCompetition(pt.MixedCompID)
		require.NoError(t, err)
		comp.Players = nil
		h.noOp(http.MethodPut, "/api/competitions/"+pt.MixedCompID, comp)
		comp.Name = "Individual Mixed Open"
		h.real(http.MethodPut, "/api/competitions/"+pt.MixedCompID, comp)
	})

	t.Run("pool rescore", func(t *testing.T) {
		h.t = t
		m := h.poolMatch(pt.MixedCompID, pt.MixedScoredMatchID)
		path := "/api/competitions/" + pt.MixedCompID + "/matches/" + url.PathEscape(m.ID) + "/score"
		body := scorePayload(m.ID, m.SideA, m.SideB, m.SideAID, m.SideBID, m.Winner, m.WinnerID, m.Status, m.IpponsA, m.IpponsB, m.ModifiedAt)
		h.noOp(http.MethodPut, path, body, "history")
		body["modifiedAt"] = now
		body["correctionReason"] = "Scoring error: added second ippon"
		h.noOp(http.MethodPut, path, body, "history")
		body["modifiedAt"] = now + 1
		body["ipponsA"] = []string{"M"}
		h.real(http.MethodPut, path, body)
	})

	t.Run("knockout rescore", func(t *testing.T) {
		h.t = t
		m := h.bracketMatch(pt.KnockoutCompID, pt.KnockoutMatchID)
		path := "/api/competitions/" + pt.KnockoutCompID + "/matches/" + url.PathEscape(m.ID) + "/score"
		body := scorePayload(m.ID, m.SideA, m.SideB, m.SideAID, m.SideBID, m.Winner, m.WinnerID, m.Status, m.IpponsA, m.IpponsB, m.ModifiedAt)
		h.noOp(http.MethodPut, path, body, "history")
		body["modifiedAt"] = now
		h.noOp(http.MethodPut, path, body, "history")
		body["modifiedAt"] = now + 1
		body["ipponsA"] = []string{"M"}
		body["correctionReason"] = "Scoring error: one ippon"
		h.real(http.MethodPut, path, body)
	})

	t.Run("pool decision", func(t *testing.T) {
		h.t = t
		pm, err := pt.Store.LoadPoolMatches(pt.MixedCompID)
		require.NoError(t, err)
		var target string
		for _, m := range pm {
			if m.Status == state.MatchStatusScheduled && m.SideAID != pt.IneligiblePlayerID && m.SideBID != pt.IneligiblePlayerID {
				target = m.ID
				break
			}
		}
		require.NotEmpty(t, target, "a scheduled pool match with two eligible sides")
		path := "/api/competitions/" + pt.MixedCompID + "/matches/" + url.PathEscape(target) + "/decision"
		body := map[string]any{"decision": "fusensho", "decisionBy": "shiro", "modifiedAt": now - 5000}
		h.real(http.MethodPost, path, body)
		h.noOp(http.MethodPost, path, body, "history")
		body["modifiedAt"] = now
		h.noOp(http.MethodPost, path, body, "pool-matches.csv", "events", "history")
	})

	t.Run("knockout decision", func(t *testing.T) {
		h.t = t
		b, err := pt.Store.LoadBracket(pt.KnockoutCompID)
		require.NoError(t, err)
		var target string
		for _, m := range b.Rounds[0] {
			if m.Status == state.MatchStatusScheduled && m.SideAID != "" && m.SideBID != "" {
				target = m.ID
				break
			}
		}
		require.NotEmpty(t, target, "a scheduled first-round knockout match")
		path := "/api/competitions/" + pt.KnockoutCompID + "/matches/" + url.PathEscape(target) + "/decision"
		body := map[string]any{"decision": "fusensho", "decisionBy": "aka", "modifiedAt": now - 5000}
		h.real(http.MethodPost, path, body)
		h.noOp(http.MethodPost, path, body, "history")
		body["modifiedAt"] = now
		h.noOp(http.MethodPost, path, body, "bracket.json", "events", "history")
	})

	t.Run("competitor status", func(t *testing.T) {
		h.t = t
		statuses, err := pt.Store.LoadCompetitorStatus(pt.MixedCompID)
		require.NoError(t, err)
		st, ok := statuses[pt.IneligiblePlayerID]
		require.True(t, ok)
		path := "/api/competitions/" + pt.MixedCompID + "/competitor-status"
		body := map[string]any{"playerId": st.PlayerID, "eligible": st.Eligible, "reason": st.Reason, "matchId": st.MatchID}
		h.noOp(http.MethodPost, path, body, "competitor-status.yaml")
		body["reason"] = "kiken-injury: knee strain"
		h.real(http.MethodPost, path, body)
	})

	t.Run("team member", func(t *testing.T) {
		h.t = t
		all, err := pt.Store.LoadSquads(pt.TeamCompID)
		require.NoError(t, err)
		var teamID string
		var member domain.TeamMember
		for _, tid := range pt.TeamIDs {
			for _, m := range all[tid] {
				if m.Name != "" && member.ID == "" {
					teamID, member = tid, m
				}
			}
		}
		require.NotEmpty(t, member.ID, "a named team member")
		path := "/api/competitions/" + pt.TeamCompID + "/teams/" + teamID + "/members/" + member.ID
		h.noOp(http.MethodPut, path, map[string]any{"name": member.Name}, "team-members.yaml")
		h.real(http.MethodPut, path, map[string]any{"name": member.Name + " Jr"})
	})

	t.Run("lineups", func(t *testing.T) {
		h.t = t
		team := pt.TeamIDs[0]
		for _, path := range []string{
			"/api/competitions/" + pt.TeamCompID + "/teams/" + team + "/match-lineups/" + url.PathEscape(pt.TeamMatchID),
			"/api/competitions/" + pt.TeamCompID + "/teams/" + team + "/lineups/0",
		} {
			w := populatedServeJSON(t, pt.Router, pt.Password, http.MethodGet, path, nil)
			require.Equal(t, http.StatusOK, w.Code, w.Body.String())
			var saved struct {
				Positions map[string]string `json:"positions"`
				MemberIDs map[string]string `json:"memberIds"`
			}
			require.NoError(t, json.Unmarshal(w.Body.Bytes(), &saved))
			require.NotEmpty(t, saved.Positions, path)
			h.noOp(http.MethodPut, path, map[string]any{"positions": saved.Positions, "memberIds": saved.MemberIDs}, "lineups.yaml")
			saved.Positions["1"], saved.Positions["3"] = saved.Positions["3"], saved.Positions["1"]
			saved.MemberIDs["1"], saved.MemberIDs["3"] = saved.MemberIDs["3"], saved.MemberIDs["1"]
			h.real(http.MethodPut, path, map[string]any{"positions": saved.Positions, "memberIds": saved.MemberIDs})
			h.real(http.MethodDelete, path, nil)
			h.noOp(http.MethodDelete, path, nil, "lineups.yaml")
		}
	})

	t.Run("pool rank override", func(t *testing.T) {
		h.t = t
		pools, err := pt.Store.LoadPools(pt.MixedCompID)
		require.NoError(t, err)
		var first, second string
		for _, p := range pools {
			if p.PoolName == pt.OverriddenPoolName && len(p.Players) >= 2 {
				first, second = p.Players[0].ID, p.Players[1].ID
			}
		}
		require.NotEmpty(t, first, "the overridden pool holds two players")
		path := "/api/competitions/" + pt.MixedCompID + "/pools/" + url.PathEscape(pt.OverriddenPoolName) + "/override-rank"
		ranks := func(first, second string) map[string]any {
			return map[string]any{"ranks": []map[string]any{{"playerId": first, "rank": 1}, {"playerId": second, "rank": 2}}}
		}
		h.noOp(http.MethodPut, path, ranks(first, second))
		h.real(http.MethodPut, path, ranks(second, first))
	})

	t.Run("roster and check-in", func(t *testing.T) {
		h.t = t
		const compID = "door-roster"
		w := populatedServeJSON(t, pt.Router, pt.Password, http.MethodPost, "/api/competitions", map[string]any{
			"id": compID, "name": "Door Roster", "kind": "individual", "format": state.CompFormatMixed,
			"poolSize": 3, "poolWinners": 2, "courts": []string{"A"}, "checkInEnabled": true,
			"players": []map[string]any{
				{"name": "Abe Ko", "dojo": "Kobe Dojo"},
				{"name": "Mori Jun", "dojo": "Sendai Dojo"},
				{"name": "Ueda Sho", "dojo": "Nara Dojo"},
			},
		})
		require.Equal(t, http.StatusCreated, w.Code, w.Body.String())
		players, err := pt.Store.LoadParticipants(compID, false)
		require.NoError(t, err)
		require.Len(t, players, 3)

		rosterBody := func(ps []domain.Player) []map[string]any {
			out := make([]map[string]any, 0, len(ps))
			for _, p := range ps {
				row := map[string]any{"name": p.Name, "displayName": p.DisplayName, "dojo": p.Dojo}
				if p.ID != "" {
					row["id"] = p.ID
				}
				out = append(out, row)
			}
			return out
		}
		participants := "/api/competitions/" + compID + "/participants"
		h.noOp(http.MethodPost, participants, map[string]any{"players": rosterBody(players)})

		// The participants page saves the roster through the competition PUT.
		comp, err := pt.Store.LoadCompetition(compID)
		require.NoError(t, err)
		comp.Players = players
		h.noOp(http.MethodPut, "/api/competitions/"+compID, comp)

		h.real(http.MethodPut, participants+"/"+players[0].ID+"/checkin", map[string]any{"checkedIn": true})
		h.noOp(http.MethodPut, participants+"/"+players[0].ID+"/checkin", map[string]any{"checkedIn": true})
		ids := []string{players[0].ID}
		h.noOp(http.MethodPost, participants+"/checkin-bulk", map[string]any{"participantIds": ids})

		players, err = pt.Store.LoadParticipants(compID, false)
		require.NoError(t, err)
		grown := append(players, domain.Player{Name: "Kudo Rin", Dojo: "Akita Dojo"})
		h.real(http.MethodPost, participants, map[string]any{"players": rosterBody(grown)})
	})

	// Every stamp-table entry and every skipped folder was moved by a measured
	// no-op above: the table lists only what the doors were seen to churn.
	t.Run("table matches the evidence", func(t *testing.T) {
		for _, sf := range state.FingerprintStampedFiles {
			assert.True(t, h.moved[sf.Name], "%s is in the stamp table but no no-op door moved it", sf.Name)
		}
		for _, d := range state.FingerprintSkippedDirs {
			assert.True(t, h.moved[d], "%s/ is skipped but no no-op door moved it", d)
		}
	})
}

package mobileapp

import (
	"encoding/json"
	"net/http"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/gitrgoliveira/bracket-creator/internal/state"
)

// The roster PUT (PUT /competitions/:id with a players field) is the
// Participants page's whole-roster write: a typed seed, a row moved, a
// shuffle, Clear seeds, a seed file and Apply changes. Another desk changes
// the same roster meanwhile (a check-in, a competitor added or removed, a
// seed of its own). Nothing another desk changed may be lost to a PUT built
// before it: a check-in is never written by the roster PUT at all, and a PUT
// built from an older roster (its rosterVersion) is answered 409
// roster_changed with the roster as it stands, applying nothing, so the page
// lays its change on that roster and sends it again.

type rosterView struct {
	Players []map[string]any
	Version string
}

func readRoster(t *testing.T, do func(string, string, []byte) *rosterResp) rosterView {
	t.Helper()
	w := do("GET", "/api/viewer/competitions/c1", nil)
	require.Equal(t, http.StatusOK, w.Code, w.Body)
	var body struct {
		Config struct {
			Players       []map[string]any `json:"players"`
			RosterVersion string           `json:"rosterVersion"`
		} `json:"config"`
	}
	require.NoError(t, json.Unmarshal(w.Body, &body))
	return rosterView{Players: body.Config.Players, Version: body.Config.RosterVersion}
}

type rosterResp struct {
	Code int
	Body []byte
}

func rosterFixture(t *testing.T) (*state.Store, func(string, string, []byte) *rosterResp) {
	t.Helper()
	store, do := seedEnforcementFixture(t)
	return store, func(method, path string, body []byte) *rosterResp {
		w := do(method, path, body)
		return &rosterResp{Code: w.Code, Body: w.Body.Bytes()}
	}
}

// withSeed answers a copy of the roster with the competitor named `name`
// given `seed` (0 clears it), as the page builds a typed seed's PUT.
func withSeed(players []map[string]any, name string, seed int) []map[string]any {
	out := make([]map[string]any, 0, len(players))
	for _, p := range players {
		q := map[string]any{}
		for k, v := range p {
			q[k] = v
		}
		if q["name"] == name {
			q["seed"] = seed
		}
		out = append(out, q)
	}
	return out
}

func without(players []map[string]any, name string) []map[string]any {
	out := make([]map[string]any, 0, len(players))
	for _, p := range players {
		if p["name"] != name {
			out = append(out, p)
		}
	}
	return out
}

func putRosterBody(t *testing.T, players []map[string]any, version string) []byte {
	t.Helper()
	b, err := json.Marshal(map[string]any{
		"id": "c1", "name": "C1", "format": string(state.CompFormatMixed),
		"players": players, "rosterVersion": version,
	})
	require.NoError(t, err)
	return b
}

func idOf(t *testing.T, players []map[string]any, name string) string {
	t.Helper()
	for _, p := range players {
		if p["name"] == name {
			id, _ := p["id"].(string)
			require.NotEmpty(t, id)
			return id
		}
	}
	t.Fatalf("no %s on the roster", name)
	return ""
}

func storedByName(t *testing.T, store *state.Store) map[string]struct {
	CheckedIn bool
	Seed      int
} {
	t.Helper()
	players, err := store.LoadParticipantsOpt("c1", false, state.LoadParticipantsOpts{WithSeeds: true})
	require.NoError(t, err)
	out := map[string]struct {
		CheckedIn bool
		Seed      int
	}{}
	for _, p := range players {
		out[p.Name] = struct {
			CheckedIn bool
			Seed      int
		}{p.CheckedIn, p.Seed}
	}
	return out
}

func TestRosterPut_KeepsACheckInAnotherDeskMadeWhileThePutWasOut(t *testing.T) {
	store, do := rosterFixture(t)
	seen := readRoster(t, do)

	// Desk B checks Alice in; desk A's seed PUT was built before it.
	w := do("PUT", "/api/competitions/c1/participants/"+idOf(t, seen.Players, "Alice")+"/checkin", nil)
	require.Equal(t, http.StatusOK, w.Code, string(w.Body))

	w = do("PUT", "/api/competitions/c1", putRosterBody(t, withSeed(seen.Players, "Dave", 1), seen.Version))
	require.Equal(t, http.StatusOK, w.Code, string(w.Body))

	got := storedByName(t, store)
	assert.True(t, got["Alice"].CheckedIn, "the roster PUT must never write a check-in: Alice's, made by another desk while the PUT was out, is kept")
	assert.Equal(t, 1, got["Dave"].Seed, "the operator's seed lands")
}

func TestRosterPut_NeverChecksInOrOutThroughTheRoster(t *testing.T) {
	store, do := rosterFixture(t)
	seen := readRoster(t, do)
	players := withSeed(seen.Players, "Bob", 0)
	for _, p := range players {
		if p["name"] == "Bob" {
			p["checkedIn"] = true
		}
	}
	w := do("PUT", "/api/competitions/c1", putRosterBody(t, players, seen.Version))
	require.Equal(t, http.StatusOK, w.Code, string(w.Body))
	assert.False(t, storedByName(t, store)["Bob"].CheckedIn, "only the check-in doors check a competitor in")
}

func TestRosterPut_RefusesARosterBuiltBeforeACompetitorWasAdded(t *testing.T) {
	store, do := rosterFixture(t)
	seen := readRoster(t, do)
	require.NotEmpty(t, seen.Version, "the competition detail names the roster's version")

	// Desk B adds Eve; desk A's seed PUT was built before it.
	add, err := json.Marshal(map[string]any{"name": "Eve", "dojo": "F"})
	require.NoError(t, err)
	w := do("POST", "/api/competitions/c1/participants", add)
	require.Equal(t, http.StatusOK, w.Code, string(w.Body))

	w = do("PUT", "/api/competitions/c1", putRosterBody(t, withSeed(seen.Players, "Dave", 1), seen.Version))
	require.Equal(t, http.StatusConflict, w.Code, "a PUT built from an older roster is refused: %s", w.Body)
	var refusal struct {
		Code          string           `json:"code"`
		Players       []map[string]any `json:"players"`
		RosterVersion string           `json:"rosterVersion"`
	}
	require.NoError(t, json.Unmarshal(w.Body, &refusal))
	assert.Equal(t, "roster_changed", refusal.Code)
	assert.NotEmpty(t, refusal.RosterVersion)
	assert.NotEqual(t, seen.Version, refusal.RosterVersion)
	names := map[string]bool{}
	for _, p := range refusal.Players {
		names[p["name"].(string)] = true
	}
	assert.True(t, names["Eve"], "the refusal carries the roster as it stands")

	got := storedByName(t, store)
	_, hasEve := got["Eve"]
	assert.True(t, hasEve, "Eve, added by another desk, is not erased")
	assert.Equal(t, 0, got["Dave"].Seed, "a refused PUT applies nothing")

	// The page lays its change on the roster it was handed and sends again.
	w = do("PUT", "/api/competitions/c1", putRosterBody(t, withSeed(refusal.Players, "Dave", 1), refusal.RosterVersion))
	require.Equal(t, http.StatusOK, w.Code, string(w.Body))
	got = storedByName(t, store)
	_, hasEve = got["Eve"]
	assert.True(t, hasEve)
	assert.Equal(t, 1, got["Dave"].Seed, "the operator's seed lands after the rebuild")
}

func TestRosterPut_DoesNotBringBackACompetitorAnotherDeskRemoved(t *testing.T) {
	store, do := rosterFixture(t)
	seen := readRoster(t, do)

	w := do("PUT", "/api/competitions/c1", putRosterBody(t, without(seen.Players, "Bob"), seen.Version))
	require.Equal(t, http.StatusOK, w.Code, string(w.Body))

	w = do("PUT", "/api/competitions/c1", putRosterBody(t, withSeed(seen.Players, "Carol", 2), seen.Version))
	require.Equal(t, http.StatusConflict, w.Code, string(w.Body))
	_, hasBob := storedByName(t, store)["Bob"]
	assert.False(t, hasBob, "Bob, removed by another desk, is not brought back")
}

func TestRosterPut_TwoDesksSettingDifferentSeedsBothLand(t *testing.T) {
	store, do := rosterFixture(t)
	seen := readRoster(t, do)

	w := do("PUT", "/api/competitions/c1", putRosterBody(t, withSeed(seen.Players, "Alice", 1), seen.Version))
	require.Equal(t, http.StatusOK, w.Code, string(w.Body))

	w = do("PUT", "/api/competitions/c1", putRosterBody(t, withSeed(seen.Players, "Carol", 2), seen.Version))
	require.Equal(t, http.StatusConflict, w.Code, string(w.Body))
	var refusal struct {
		Players       []map[string]any `json:"players"`
		RosterVersion string           `json:"rosterVersion"`
	}
	require.NoError(t, json.Unmarshal(w.Body, &refusal))

	w = do("PUT", "/api/competitions/c1", putRosterBody(t, withSeed(refusal.Players, "Carol", 2), refusal.RosterVersion))
	require.Equal(t, http.StatusOK, w.Code, string(w.Body))

	got := storedByName(t, store)
	assert.Equal(t, 1, got["Alice"].Seed)
	assert.Equal(t, 2, got["Carol"].Seed)
}

func TestRosterPut_ACheckInDoesNotRefuseThePut(t *testing.T) {
	_, do := rosterFixture(t)
	seen := readRoster(t, do)
	w := do("PUT", "/api/competitions/c1/participants/"+idOf(t, seen.Players, "Carol")+"/checkin", nil)
	require.Equal(t, http.StatusOK, w.Code, string(w.Body))
	after := readRoster(t, do)
	assert.Equal(t, seen.Version, after.Version, "a check-in is not a roster change the PUT writes, so it leaves the version")
}

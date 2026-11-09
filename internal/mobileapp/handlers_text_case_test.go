package mobileapp

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/gitrgoliveira/bracket-creator/internal/domain"
	"github.com/gitrgoliveira/bracket-creator/internal/helper"
	"github.com/gitrgoliveira/bracket-creator/internal/state"
	bctest "github.com/gitrgoliveira/bracket-creator/internal/test"
)

// A seeding typed in another case is stored title-cased, like the roster it
// names (operator decision D14), so it still finds its competitor.
func TestPutSeedsStoresTitleCasedNamesAndDojos(t *testing.T) {
	store, do := seedEnforcementFixture(t)
	body, err := json.Marshal([]domain.SeedAssignment{{Name: "alice", Dojo: "d", SeedRank: 1}})
	require.NoError(t, err)
	w := do("PUT", "/api/competitions/c1/seeds", body)
	require.Equal(t, http.StatusOK, w.Code, w.Body.String())

	stored, err := store.LoadSeedsRaw("c1")
	require.NoError(t, err)
	require.Len(t, stored, 1)
	assert.Equal(t, "Alice", stored[0].Name)
	assert.Equal(t, "D", stored[0].Dojo)
	assert.NotEmpty(t, stored[0].ID, "the title-cased row resolves on the roster")
}

// The fighting-spirit award's free-text recipient is title-cased where it is
// saved (operator decision D15).
func TestPutAwardsStoresATitleCasedRecipient(t *testing.T) {
	store, do := seedEnforcementFixture(t)
	body, err := json.Marshal(map[string]any{
		"fightingSpiritAwards": []map[string]any{
			{"title": "Fighting spirit", "recipientName": "  aiko mori ", "recipientDojo": "tobukan"},
		},
	})
	require.NoError(t, err)
	w := do("PUT", "/api/competitions/c1/awards", versionedAwardsBody(t, store, "c1", body))
	require.Equal(t, http.StatusOK, w.Code, w.Body.String())

	comp, err := store.LoadCompetition("c1")
	require.NoError(t, err)
	require.Len(t, comp.FightingSpiritAwards, 1)
	assert.Equal(t, "Aiko Mori", comp.FightingSpiritAwards[0].RecipientName)
	assert.Equal(t, "Tobukan", comp.FightingSpiritAwards[0].RecipientDojo)
	assert.Equal(t, "Fighting spirit", comp.FightingSpiritAwards[0].Title, "the award's title is not a name and keeps its case")
}

// A participants.csv holding two rows that are one competitor once their text
// is canonicalised (only a hand edit writes them) is answered like any other
// damaged file, naming participants.csv and both lines (operator decision
// D21): the Participants page's read and the roster save get the corrupt_file
// body, and the public competition page degrades and lists it in dataIssues.
func TestDuplicateRosterRowsAreAnsweredAsADamagedFile(t *testing.T) {
	r, store, _, _, dir := setupTestRouter(t)
	require.NoError(t, store.SaveCompetition(&state.Competition{ID: "c1", Name: "C1", Format: state.CompFormatMixed, PoolSize: 3, PoolWinners: 2}))
	require.NoError(t, store.SaveParticipants("c1", []domain.Player{
		{Name: "John Smith", Dojo: "Kenshinkan"}, {Name: "Yuki Tanaka", Dojo: "Tobukan"},
	}))
	path := filepath.Join(dir, "competitions", "c1", "participants.csv")
	raw, err := bctest.ReadFile(path)
	require.NoError(t, err)
	rows := strings.Count(string(raw), "\n")
	require.NoError(t, os.WriteFile(path, append(raw, []byte(helper.NewUUID4()+",john smith,kenshinkan\n")...), 0o600))
	later := time.Now().Add(time.Minute)
	require.NoError(t, os.Chtimes(path, later, later))
	dupLine := rows + 1

	do := func(method, url, body string) (int, map[string]any) {
		w := httptest.NewRecorder()
		req, err := http.NewRequest(method, url, strings.NewReader(body))
		require.NoError(t, err)
		req.Header.Set("Content-Type", "application/json")
		r.ServeHTTP(w, req)
		var out map[string]any
		require.NoError(t, json.Unmarshal(w.Body.Bytes(), &out), w.Body.String())
		return w.Code, out
	}
	assertNamesTheLines := func(t *testing.T, issue map[string]any) {
		t.Helper()
		assert.Equal(t, "participants.csv", issue["file"])
		assert.EqualValues(t, dupLine, issue["line"])
		assert.Contains(t, issue["detail"], "and "+strconv.Itoa(dupLine)+" hold the same competitor")
	}

	code, body := do("GET", "/api/competitions/c1/participants", "")
	require.Equal(t, http.StatusInternalServerError, code)
	assert.Equal(t, "corrupt_file", body["code"])
	assertNamesTheLines(t, body)

	code, body = do("POST", "/api/competitions/c1/participants", `{"players":[{"name":"Rin Sato","dojo":"Seibukan"}]}`)
	require.Equal(t, http.StatusInternalServerError, code, "the save refuses to build on a roster it cannot read")
	assert.Equal(t, "corrupt_file", body["code"])
	assertNamesTheLines(t, body)

	code, body = do("GET", "/api/viewer/competitions/c1", "")
	require.Equal(t, http.StatusOK, code)
	assert.Equal(t, false, body["rosterAvailable"])
	issues, _ := body["dataIssues"].([]any)
	var found map[string]any
	for _, i := range issues {
		if m, ok := i.(map[string]any); ok && m["file"] == "participants.csv" && m["kind"] == "corrupt-file" {
			found = m
		}
	}
	require.NotNil(t, found, "dataIssues names participants.csv: %v", issues)
	assertNamesTheLines(t, found)
}

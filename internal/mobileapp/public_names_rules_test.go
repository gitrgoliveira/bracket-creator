package mobileapp

// The public-names option's server rules: which values a competition and the
// tournament default take, where hiding names is refused (self-run, Swiss),
// how the tournament default applies to the competitions (E2), the reveal
// secret's minting and life (E3), and the organiser's read of the reveal
// links.

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

const publicNamesPassword = "main-pw"

// publicNamesFixture is the full router over a store, signed in as the
// organiser unless a request says otherwise.
type publicNamesFixture struct {
	r     *gin.Engine
	store *state.Store
}

func newPublicNamesFixture(t *testing.T, mode string) publicNamesFixture {
	t.Helper()
	store := newTempStore(t)
	switch mode {
	case state.TournamentModeSelfRun:
		seedSelfRunTournament(t, store)
	case state.TournamentModeOfficiated:
		seedOfficiatedTournament(t, store)
	}
	return publicNamesFixture{r: setupSelfRunRouter(t, store, NewFileVerifier(store)), store: store}
}

func (f publicNamesFixture) send(method, path, password string, body any) *httptest.ResponseRecorder {
	req := jsonReq(method, path, body)
	req.Header.Set("X-Tournament-Password", password)
	w := httptest.NewRecorder()
	f.r.ServeHTTP(w, req)
	return w
}

func (f publicNamesFixture) admin(method, path string, body any) *httptest.ResponseRecorder {
	return f.send(method, path, publicNamesPassword, body)
}

func (f publicNamesFixture) comp(t *testing.T, id string) *state.Competition {
	t.Helper()
	c, err := f.store.LoadCompetition(id)
	require.NoError(t, err)
	require.NotNil(t, c, "competition %s", id)
	return c
}

// saveComp writes a competition straight to the store, as a hand edit or
// an older save would leave it.
func (f publicNamesFixture) saveComp(t *testing.T, c *state.Competition) {
	t.Helper()
	if c.Courts == nil {
		c.Courts = []string{"A"}
	}
	if c.Status == "" {
		c.Status = state.CompStatusSetup
	}
	require.NoError(t, f.store.SaveCompetition(c))
}

func codeOf(t *testing.T, w *httptest.ResponseRecorder) string {
	t.Helper()
	var body struct {
		Code string `json:"code"`
	}
	require.NoError(t, json.Unmarshal(w.Body.Bytes(), &body), w.Body.String())
	return body.Code
}

func createBody(name, format string, extra map[string]any) map[string]any {
	body := map[string]any{"name": name, "kind": "individual", "format": format, "courts": []string{"A"}}
	if format == state.CompFormatSwiss {
		body["swissRounds"] = 3
	}
	for k, v := range extra {
		body[k] = v
	}
	return body
}

func createdID(t *testing.T, w *httptest.ResponseRecorder) string {
	t.Helper()
	require.Less(t, w.Code, 300, w.Body.String())
	var body struct {
		ID string `json:"id"`
	}
	require.NoError(t, json.Unmarshal(w.Body.Bytes(), &body), w.Body.String())
	require.NotEmpty(t, body.ID, w.Body.String())
	return body.ID
}

func TestPublicNames_CreateTakesAnOptionAndMintsTheSecretOnce(t *testing.T) {
	f := newPublicNamesFixture(t, state.TournamentModeOfficiated)
	sent := strings.Repeat("ab", 32)
	w := f.admin(http.MethodPost, "/api/competitions", createBody("Per Person", state.CompFormatKnockout, map[string]any{
		"publicNames":   "person",
		"revealUntil":   " 2026-06-01T18:00 ",
		"revealSecret":  sent,
		"reveal_secret": sent,
	}))
	id := createdID(t, w)
	assert.NotContains(t, w.Body.String(), sent, "the answer never carries a secret")
	assert.NotContains(t, w.Body.String(), "revealSecret")

	c := f.comp(t, id)
	assert.Equal(t, state.PublicNamesPerson, c.PublicNames)
	assert.Equal(t, "2026-06-01T18:00", c.RevealUntil, "trimmed")
	assert.Len(t, c.RevealSecret, 64, "a kind with links mints a secret")
	assert.NotEqual(t, sent, c.RevealSecret, "a client-sent secret is ignored")
}

func TestPublicNames_CreateRefusals(t *testing.T) {
	t.Run("a value that is not an option is 400", func(t *testing.T) {
		f := newPublicNamesFixture(t, state.TournamentModeOfficiated)
		w := f.admin(http.MethodPost, "/api/competitions", createBody("Both", state.CompFormatKnockout, map[string]any{"publicNames": "both"}))
		assert.Equal(t, http.StatusBadRequest, w.Code, w.Body.String())
		assert.Contains(t, w.Body.String(), "publicNames must be one of")
	})
	t.Run("an expiry that does not read is 400", func(t *testing.T) {
		f := newPublicNamesFixture(t, state.TournamentModeOfficiated)
		w := f.admin(http.MethodPost, "/api/competitions", createBody("Late", state.CompFormatKnockout, map[string]any{"publicNames": "competition", "revealUntil": "tomorrow"}))
		assert.Equal(t, http.StatusBadRequest, w.Code, w.Body.String())
		assert.Contains(t, w.Body.String(), "revealUntil must be")
	})
	t.Run("hiding names in a self-run tournament is 409", func(t *testing.T) {
		f := newPublicNamesFixture(t, state.TournamentModeSelfRun)
		w := f.admin(http.MethodPost, "/api/competitions", createBody("Self", state.CompFormatKnockout, map[string]any{"publicNames": "hidden"}))
		assert.Equal(t, http.StatusConflict, w.Code, w.Body.String())
		assert.Equal(t, codePublicNamesSelfRun, codeOf(t, w))
	})
	t.Run("showing names in a self-run tournament is fine", func(t *testing.T) {
		f := newPublicNamesFixture(t, state.TournamentModeSelfRun)
		w := f.admin(http.MethodPost, "/api/competitions", createBody("Self", state.CompFormatKnockout, map[string]any{"publicNames": ""}))
		createdID(t, w)
	})
	t.Run("hiding names on a Swiss competition is 400", func(t *testing.T) {
		f := newPublicNamesFixture(t, state.TournamentModeOfficiated)
		w := f.admin(http.MethodPost, "/api/competitions", createBody("Swiss", state.CompFormatSwiss, map[string]any{"publicNames": "person"}))
		assert.Equal(t, http.StatusBadRequest, w.Code, w.Body.String())
		assert.Equal(t, codePublicNamesSwiss, codeOf(t, w))
	})
}

func TestPublicNames_NewCompetitionStartsWithTheTournamentDefault(t *testing.T) {
	f := newPublicNamesFixture(t, state.TournamentModeOfficiated)
	tourn, err := f.store.LoadTournament()
	require.NoError(t, err)
	tourn.DefaultPublicNames = state.PublicNamesCompetition
	require.NoError(t, f.store.SaveTournament(tourn))

	id := createdID(t, f.admin(http.MethodPost, "/api/competitions", createBody("Follows", state.CompFormatKnockout, nil)))
	c := f.comp(t, id)
	assert.Equal(t, state.PublicNamesCompetition, c.PublicNames, "an omitted option takes the default")
	assert.Len(t, c.RevealSecret, 64)

	id = createdID(t, f.admin(http.MethodPost, "/api/competitions", createBody("Chooses", state.CompFormatKnockout, map[string]any{"publicNames": ""})))
	assert.Equal(t, state.PublicNamesShown, f.comp(t, id).PublicNames, "a chosen \"\" is a real value")

	id = createdID(t, f.admin(http.MethodPost, "/api/competitions", createBody("Swiss Follows", state.CompFormatSwiss, nil)))
	assert.Equal(t, state.PublicNamesShown, f.comp(t, id).PublicNames, "Swiss is left out of the default")
}

func TestPublicNames_SettingsPutKeepsTheSecretForTheCompetitionsLife(t *testing.T) {
	f := newPublicNamesFixture(t, state.TournamentModeOfficiated)
	f.saveComp(t, &state.Competition{ID: "c1", Name: "Kata", Kind: "individual", Format: state.CompFormatKnockout})
	put := func(extra map[string]any) *httptest.ResponseRecorder {
		body := map[string]any{"name": "Kata", "kind": "individual", "format": state.CompFormatKnockout, "courts": []string{"A"}}
		for k, v := range extra {
			body[k] = v
		}
		return f.admin(http.MethodPut, "/api/competitions/c1", body)
	}

	w := put(map[string]any{"publicNames": "hidden"})
	require.Equal(t, http.StatusOK, w.Code, w.Body.String())
	assert.Empty(t, f.comp(t, "c1").RevealSecret, "a kind with no links mints nothing")

	w = put(map[string]any{"publicNames": "competition", "revealSecret": strings.Repeat("cd", 32)})
	require.Equal(t, http.StatusOK, w.Code, w.Body.String())
	secret := f.comp(t, "c1").RevealSecret
	require.Len(t, secret, 64)
	assert.NotEqual(t, strings.Repeat("cd", 32), secret, "a client-sent secret is ignored")
	assert.NotContains(t, w.Body.String(), secret)

	w = put(map[string]any{"publicNames": ""})
	require.Equal(t, http.StatusOK, w.Code, w.Body.String())
	assert.Equal(t, secret, f.comp(t, "c1").RevealSecret, "switching links off keeps the secret")

	w = put(map[string]any{"publicNames": "person"})
	require.Equal(t, http.StatusOK, w.Code, w.Body.String())
	c := f.comp(t, "c1")
	assert.Equal(t, state.PublicNamesPerson, c.PublicNames)
	assert.Equal(t, secret, c.RevealSecret, "switching back gives the same secret, so the same links")

	w = put(nil)
	require.Equal(t, http.StatusOK, w.Code, w.Body.String())
	assert.Equal(t, state.PublicNamesPerson, f.comp(t, "c1").PublicNames, "an omitted option keeps the stored one")

	w = put(map[string]any{"revealUntil": "2026-06-01T25:00"})
	assert.Equal(t, http.StatusBadRequest, w.Code, w.Body.String())
	w = put(map[string]any{"revealUntil": "2026-06-01T20:30"})
	require.Equal(t, http.StatusOK, w.Code, w.Body.String())
	assert.Equal(t, "2026-06-01T20:30", f.comp(t, "c1").RevealUntil)

	w = put(nil)
	require.Equal(t, http.StatusOK, w.Code, w.Body.String())
	assert.Equal(t, "2026-06-01T20:30", f.comp(t, "c1").RevealUntil, "an omitted expiry keeps the stored one")
	w = put(map[string]any{"revealUntil": " "})
	require.Equal(t, http.StatusOK, w.Code, w.Body.String())
	assert.Empty(t, f.comp(t, "c1").RevealUntil, "an empty expiry clears it, so the links last to the end of the tournament")

	w = put(map[string]any{"publicNames": "both"})
	assert.Equal(t, http.StatusBadRequest, w.Code, w.Body.String())
}

func TestPublicNames_SettingsPutRefusals(t *testing.T) {
	t.Run("switching a hiding competition to Swiss is 400", func(t *testing.T) {
		f := newPublicNamesFixture(t, state.TournamentModeOfficiated)
		f.saveComp(t, &state.Competition{ID: "c1", Name: "Kata", Kind: "individual", Format: state.CompFormatKnockout, PublicNames: state.PublicNamesHidden})
		w := f.admin(http.MethodPut, "/api/competitions/c1", map[string]any{"name": "Kata", "kind": "individual", "format": state.CompFormatSwiss, "swissRounds": 3, "courts": []string{"A"}})
		assert.Equal(t, http.StatusBadRequest, w.Code, w.Body.String())
		assert.Equal(t, codePublicNamesSwiss, codeOf(t, w))
		assert.Equal(t, state.CompFormatKnockout, f.comp(t, "c1").Format, "nothing changed")
	})
	t.Run("switching to Swiss while showing names again is fine", func(t *testing.T) {
		f := newPublicNamesFixture(t, state.TournamentModeOfficiated)
		f.saveComp(t, &state.Competition{ID: "c1", Name: "Kata", Kind: "individual", Format: state.CompFormatKnockout, PublicNames: state.PublicNamesHidden})
		w := f.admin(http.MethodPut, "/api/competitions/c1", map[string]any{"name": "Kata", "kind": "individual", "format": state.CompFormatSwiss, "swissRounds": 3, "courts": []string{"A"}, "publicNames": ""})
		require.Equal(t, http.StatusOK, w.Code, w.Body.String())
	})
	t.Run("choosing a hiding option in a self-run tournament is 409", func(t *testing.T) {
		f := newPublicNamesFixture(t, state.TournamentModeSelfRun)
		f.saveComp(t, &state.Competition{ID: "c1", Name: "Kata", Kind: "individual", Format: state.CompFormatKnockout})
		w := f.admin(http.MethodPut, "/api/competitions/c1", map[string]any{"name": "Kata", "kind": "individual", "format": state.CompFormatKnockout, "courts": []string{"A"}, "publicNames": "competition"})
		assert.Equal(t, http.StatusConflict, w.Code, w.Body.String())
		assert.Equal(t, codePublicNamesSelfRun, codeOf(t, w))
		assert.Equal(t, state.PublicNamesShown, f.comp(t, "c1").PublicNames)
	})
	t.Run("a stored option the save leaves alone is never refused", func(t *testing.T) {
		f := newPublicNamesFixture(t, state.TournamentModeSelfRun)
		f.saveComp(t, &state.Competition{ID: "c1", Name: "Kata", Kind: "individual", Format: state.CompFormatKnockout, PublicNames: state.PublicNamesHidden})
		w := f.admin(http.MethodPut, "/api/competitions/c1", map[string]any{"name": "Kata Renamed", "kind": "individual", "format": state.CompFormatKnockout, "courts": []string{"A"}})
		require.Equal(t, http.StatusOK, w.Code, w.Body.String())
		assert.Equal(t, "Kata Renamed", f.comp(t, "c1").Name)
	})
	t.Run("a self-run competition holding a hiding option switches to Swiss", func(t *testing.T) {
		// The option does nothing in a self-run tournament, and its settings
		// do not show it, so a value a restore left there never refuses a
		// format change.
		f := newPublicNamesFixture(t, state.TournamentModeSelfRun)
		f.saveComp(t, &state.Competition{ID: "c1", Name: "Kata", Kind: "individual", Format: state.CompFormatKnockout, PublicNames: state.PublicNamesHidden})
		w := f.admin(http.MethodPut, "/api/competitions/c1", map[string]any{"name": "Kata", "kind": "individual", "format": state.CompFormatSwiss, "swissRounds": 3, "courts": []string{"A"}})
		require.Equal(t, http.StatusOK, w.Code, w.Body.String())
		c1 := f.comp(t, "c1")
		assert.Equal(t, state.CompFormatSwiss, c1.Format)
		assert.Equal(t, state.PublicNamesHidden, c1.PublicNames, "the stored setting is left untouched")
	})
}

func tournamentPutBody(t *testing.T, f publicNamesFixture, extra map[string]any) map[string]any {
	t.Helper()
	stored, err := f.store.LoadTournament()
	require.NoError(t, err)
	body := map[string]any{
		"name": stored.Name, "date": stored.Date, "venue": stored.Venue, "courts": stored.Courts,
		"mode": stored.Mode, "defaultPublicNames": stored.DefaultPublicNames,
	}
	for k, v := range extra {
		body[k] = v
	}
	return withStoredTournamentVersion(t, f.store, body)
}

// A save the transform would refuse is refused before the organiser is asked
// to confirm what the default would change: a confirmation given for it
// would only be followed by the same refusal.
func TestPublicNames_TournamentDefaultSaveRefusedForItsOtherFieldsBeforeTheConfirmation(t *testing.T) {
	f := newPublicNamesFixture(t, state.TournamentModeOfficiated)
	f.saveComp(t, &state.Competition{ID: "c1", Name: "Kata", Kind: "individual", Format: state.CompFormatKnockout})

	w := f.admin(http.MethodPut, "/api/tournament", tournamentPutBody(t, f, map[string]any{"defaultPublicNames": "person", "mode": state.TournamentModeSelfRun}))
	require.Equal(t, http.StatusBadRequest, w.Code, w.Body.String())
	assert.Contains(t, w.Body.String(), errModeImmutable.Error())

	w = f.admin(http.MethodPut, "/api/tournament", tournamentPutBody(t, f, map[string]any{
		"defaultPublicNames": "person",
		"pauses":             []map[string]any{{"name": "Lunch", "start": "12:00", "length": "1h", "day": 9}},
	}))
	require.Equal(t, http.StatusBadRequest, w.Code, w.Body.String())
	assert.Contains(t, w.Body.String(), "Day 9")

	stored, err := f.store.LoadTournament()
	require.NoError(t, err)
	assert.Empty(t, stored.DefaultPublicNames)
	assert.Empty(t, f.comp(t, "c1").PublicNames)
}

func TestPublicNames_TournamentDefaultAppliesAfterConfirmation(t *testing.T) {
	f := newPublicNamesFixture(t, state.TournamentModeOfficiated)
	f.saveComp(t, &state.Competition{ID: "c1", Name: "Kata", Kind: "individual", Format: state.CompFormatKnockout})
	f.saveComp(t, &state.Competition{ID: "c2", Name: "Shiai", Kind: "individual", Format: state.CompFormatKnockout, PublicNames: state.PublicNamesPerson})
	f.saveComp(t, &state.Competition{ID: "c3", Name: "Swiss", Kind: "individual", Format: state.CompFormatSwiss})

	w := f.admin(http.MethodPut, "/api/tournament", tournamentPutBody(t, f, map[string]any{"defaultPublicNames": "person"}))
	require.Equal(t, http.StatusConflict, w.Code, w.Body.String())
	assert.Equal(t, codePublicNamesApplyConfirm, codeOf(t, w))
	var refusal struct {
		Competitions []publicNamesTarget `json:"competitions"`
	}
	require.NoError(t, json.Unmarshal(w.Body.Bytes(), &refusal))
	require.Len(t, refusal.Competitions, 1, "only the competition the default changes is listed; Swiss is left out")
	assert.Equal(t, "c1", refusal.Competitions[0].ID)
	stored, err := f.store.LoadTournament()
	require.NoError(t, err)
	assert.Empty(t, stored.DefaultPublicNames, "nothing saved before the confirmation")
	assert.Empty(t, f.comp(t, "c1").PublicNames)

	w = f.admin(http.MethodPut, "/api/tournament", tournamentPutBody(t, f, map[string]any{"defaultPublicNames": "person", "confirmPublicNames": true}))
	require.Equal(t, http.StatusOK, w.Code, w.Body.String())
	stored, err = f.store.LoadTournament()
	require.NoError(t, err)
	assert.Equal(t, state.PublicNamesPerson, stored.DefaultPublicNames)
	c1 := f.comp(t, "c1")
	assert.Equal(t, state.PublicNamesPerson, c1.PublicNames)
	assert.Len(t, c1.RevealSecret, 64, "the default mints the secret too")
	assert.Equal(t, state.PublicNamesShown, f.comp(t, "c3").PublicNames, "Swiss is left alone")

	// Each competition can still be changed on its own afterwards, and a
	// save that leaves the default alone does not put it back.
	w = f.admin(http.MethodPut, "/api/competitions/c1", map[string]any{"name": "Kata", "kind": "individual", "format": state.CompFormatKnockout, "courts": []string{"A"}, "publicNames": ""})
	require.Equal(t, http.StatusOK, w.Code, w.Body.String())
	w = f.admin(http.MethodPut, "/api/tournament", tournamentPutBody(t, f, map[string]any{"venue": "Budokan"}))
	require.Equal(t, http.StatusOK, w.Code, w.Body.String())
	assert.Equal(t, state.PublicNamesShown, f.comp(t, "c1").PublicNames)
}

// A Swiss competition left hiding names (a hand edit, a restore) is brought
// back to showing them by a default that shows names, as any other is; only a
// hiding default leaves Swiss out.
func TestPublicNames_AShowingDefaultBringsBackAHidingSwissCompetition(t *testing.T) {
	f := newPublicNamesFixture(t, state.TournamentModeOfficiated)
	w := f.admin(http.MethodPut, "/api/tournament", tournamentPutBody(t, f, map[string]any{"defaultPublicNames": "person", "confirmPublicNames": true}))
	require.Equal(t, http.StatusOK, w.Code, w.Body.String())
	f.saveComp(t, &state.Competition{ID: "c1", Name: "Swiss", Kind: "individual", Format: state.CompFormatSwiss, PublicNames: state.PublicNamesHidden})

	w = f.admin(http.MethodPut, "/api/tournament", tournamentPutBody(t, f, map[string]any{"defaultPublicNames": ""}))
	require.Equal(t, http.StatusConflict, w.Code, w.Body.String())
	assert.Equal(t, codePublicNamesApplyConfirm, codeOf(t, w))
	var refusal struct {
		Competitions []publicNamesTarget `json:"competitions"`
	}
	require.NoError(t, json.Unmarshal(w.Body.Bytes(), &refusal))
	require.Len(t, refusal.Competitions, 1, "the hiding Swiss competition is listed")
	assert.Equal(t, "c1", refusal.Competitions[0].ID)

	w = f.admin(http.MethodPut, "/api/tournament", tournamentPutBody(t, f, map[string]any{"defaultPublicNames": "", "confirmPublicNames": true}))
	require.Equal(t, http.StatusOK, w.Code, w.Body.String())
	assert.Equal(t, state.PublicNamesShown, f.comp(t, "c1").PublicNames)
}

func TestPublicNamesDefaultChanges(t *testing.T) {
	swissHiding := &state.Competition{Format: state.CompFormatSwiss, PublicNames: state.PublicNamesHidden}
	knockout := &state.Competition{Format: state.CompFormatKnockout}
	assert.True(t, publicNamesDefaultChanges(swissHiding, state.PublicNamesShown), "a showing default reaches a hiding Swiss competition")
	assert.False(t, publicNamesDefaultChanges(swissHiding, state.PublicNamesPerson), "a hiding default never reaches Swiss")
	assert.False(t, publicNamesDefaultChanges(&state.Competition{Format: state.CompFormatSwiss}, state.PublicNamesPerson))
	assert.True(t, publicNamesDefaultChanges(knockout, state.PublicNamesPerson))
	assert.False(t, publicNamesDefaultChanges(knockout, state.PublicNamesShown), "an option already taken is no change")
	assert.False(t, publicNamesDefaultChanges(nil, state.PublicNamesShown))
}

func TestPublicNames_TournamentDefaultRefusals(t *testing.T) {
	t.Run("a value that is not an option is 400", func(t *testing.T) {
		f := newPublicNamesFixture(t, state.TournamentModeOfficiated)
		w := f.admin(http.MethodPut, "/api/tournament", tournamentPutBody(t, f, map[string]any{"defaultPublicNames": "both"}))
		assert.Equal(t, http.StatusBadRequest, w.Code, w.Body.String())
	})
	t.Run("a hiding default in a self-run tournament is 409", func(t *testing.T) {
		f := newPublicNamesFixture(t, state.TournamentModeSelfRun)
		w := f.admin(http.MethodPut, "/api/tournament", tournamentPutBody(t, f, map[string]any{"defaultPublicNames": "hidden", "confirmPublicNames": true}))
		assert.Equal(t, http.StatusConflict, w.Code, w.Body.String())
		assert.Equal(t, codePublicNamesSelfRun, codeOf(t, w))
	})
	t.Run("the first save of a self-run tournament with a hiding default is 409", func(t *testing.T) {
		f := newPublicNamesFixture(t, "")
		w := f.send(http.MethodPost, "/api/tournament", "", map[string]any{
			"name": "New", "date": "01-06-2026", "courts": []string{"A"}, "password": publicNamesPassword,
			"mode": state.TournamentModeSelfRun, "defaultPublicNames": "person",
		})
		assert.Equal(t, http.StatusConflict, w.Code, w.Body.String())
		assert.Equal(t, codePublicNamesSelfRun, codeOf(t, w))
	})
}

// Self-run tournaments are out of scope: creating one over competitions that
// hide names asks nothing, refuses nothing and rewrites no competition. The
// stored setting is left as it is; the public pages show names while the
// tournament is self-run.
func TestPublicNames_CreatingASelfRunTournamentOverHidingCompetitionsChangesNothing(t *testing.T) {
	for _, method := range []string{http.MethodPost, http.MethodPut} {
		t.Run(method, func(t *testing.T) {
			f := newPublicNamesFixture(t, "")
			f.saveComp(t, &state.Competition{ID: "c1", Name: "Kata", Kind: "individual", Format: state.CompFormatKnockout, PublicNames: state.PublicNamesCompetition, RevealSecret: strings.Repeat("12", 32)})
			body := map[string]any{
				"name": "New", "date": "01-06-2026", "courts": []string{"A"}, "password": publicNamesPassword,
				"mode": state.TournamentModeSelfRun,
			}
			if method == http.MethodPut {
				body = withStoredTournamentVersion(t, f.store, body)
			}
			w := f.send(method, "/api/tournament", "", body)
			require.Less(t, w.Code, 300, w.Body.String())
			stored, err := f.store.LoadTournament()
			require.NoError(t, err)
			require.NotNil(t, stored)
			assert.Equal(t, state.TournamentModeSelfRun, stored.Mode)
			c1 := f.comp(t, "c1")
			assert.Equal(t, state.PublicNamesCompetition, c1.PublicNames, "the stored setting is untouched")
			assert.Equal(t, strings.Repeat("12", 32), c1.RevealSecret)
		})
	}
}

// Self-run tournaments are out of scope: a device holding an old password
// reads the public pages there as it always did, and the public view shows
// names whatever a competition's stored setting says. An officiated
// tournament still refuses the wrong password.
func TestPublicNames_WrongPasswordOnAPublicReadIsRefusedOnlyWhileOfficiated(t *testing.T) {
	const path = "/api/viewer/competitions/c1"
	hiding := &state.Competition{ID: "c1", Name: "Kata", Kind: "individual", Format: state.CompFormatKnockout, PublicNames: state.PublicNamesHidden}

	selfRun := newPublicNamesFixture(t, state.TournamentModeSelfRun)
	selfRun.saveComp(t, hiding)
	require.NoError(t, selfRun.store.SaveParticipants("c1", []domain.Player{{Name: "Zed Selfrun", Dojo: "Dojo One"}}))
	w := selfRun.send(http.MethodGet, path, "an-old-password", nil)
	require.Equal(t, http.StatusOK, w.Code, w.Body.String())
	assert.Contains(t, w.Body.String(), "Zed Selfrun")
	assert.Equal(t, state.PublicNamesHidden, selfRun.comp(t, "c1").PublicNames, "the stored setting is untouched")

	officiated := newPublicNamesFixture(t, state.TournamentModeOfficiated)
	officiated.saveComp(t, hiding)
	w = officiated.send(http.MethodGet, path, "an-old-password", nil)
	assert.Equal(t, http.StatusUnauthorized, w.Code, w.Body.String())
}

func TestPublicNames_OfficiatedTournamentCreatedOverHidingCompetitionsAsksNothing(t *testing.T) {
	f := newPublicNamesFixture(t, "")
	f.saveComp(t, &state.Competition{ID: "c1", Name: "Kata", Kind: "individual", Format: state.CompFormatKnockout, PublicNames: state.PublicNamesHidden})
	w := f.send(http.MethodPost, "/api/tournament", "", map[string]any{
		"name": "New", "date": "01-06-2026", "courts": []string{"A"}, "password": publicNamesPassword,
	})
	require.Less(t, w.Code, 300, w.Body.String())
	assert.Equal(t, state.PublicNamesHidden, f.comp(t, "c1").PublicNames)
}

// A hiding default a self-run tournament already holds (hand-edited, or
// brought in by a restore) is never applied to the competitions, confirmed
// save or not.
func TestPublicNames_ConfirmedSaveNeverAppliesAStoredHidingDefaultToASelfRunTournament(t *testing.T) {
	f := newPublicNamesFixture(t, state.TournamentModeSelfRun)
	f.saveComp(t, &state.Competition{ID: "c1", Name: "Kata", Kind: "individual", Format: state.CompFormatKnockout})
	stored, err := f.store.LoadTournament()
	require.NoError(t, err)
	edited := *stored
	edited.DefaultPublicNames = state.PublicNamesHidden
	require.NoError(t, f.store.SaveTournament(&edited))

	w := f.admin(http.MethodPut, "/api/tournament", tournamentPutBody(t, f, map[string]any{"confirmPublicNames": true}))
	require.Equal(t, http.StatusOK, w.Code, w.Body.String())
	assert.Empty(t, f.comp(t, "c1").PublicNames, "a self-run tournament's competition is not made to hide names")
}

// A first save, and a POST over the tournament that is there, move the default
// like a PUT does: the competitions already on disk are set to it once the
// organiser has seen the list.
func TestPublicNames_TheDefaultOfAFirstSaveOrAPostAppliesAfterConfirmation(t *testing.T) {
	cases := map[string]struct {
		mode   string
		method string
		body   func(t *testing.T, f publicNamesFixture) map[string]any
	}{
		"POST creating the tournament": {"", http.MethodPost, func(t *testing.T, f publicNamesFixture) map[string]any {
			return map[string]any{"name": "New", "date": "01-06-2026", "courts": []string{"A"}, "password": publicNamesPassword}
		}},
		"PUT creating the tournament": {"", http.MethodPut, func(t *testing.T, f publicNamesFixture) map[string]any {
			return withStoredTournamentVersion(t, f.store, map[string]any{"name": "New", "date": "01-06-2026", "courts": []string{"A"}, "password": publicNamesPassword})
		}},
		"POST over the tournament": {state.TournamentModeOfficiated, http.MethodPost, func(t *testing.T, f publicNamesFixture) map[string]any {
			return map[string]any{"name": "Officiated Test", "date": "01-06-2026", "courts": []string{"A"}, "password": publicNamesPassword}
		}},
	}
	for name, tc := range cases {
		t.Run(name, func(t *testing.T) {
			f := newPublicNamesFixture(t, tc.mode)
			f.saveComp(t, &state.Competition{ID: "c1", Name: "Kata", Kind: "individual", Format: state.CompFormatKnockout})
			f.saveComp(t, &state.Competition{ID: "c2", Name: "Swiss", Kind: "individual", Format: state.CompFormatSwiss})
			body := tc.body(t, f)
			body["defaultPublicNames"] = "person"

			w := f.send(tc.method, "/api/tournament", publicNamesPassword, body)
			require.Equal(t, http.StatusConflict, w.Code, w.Body.String())
			assert.Equal(t, codePublicNamesApplyConfirm, codeOf(t, w))
			var refusal struct {
				Competitions []publicNamesTarget `json:"competitions"`
			}
			require.NoError(t, json.Unmarshal(w.Body.Bytes(), &refusal))
			require.Len(t, refusal.Competitions, 1, "Swiss is left out")
			assert.Equal(t, "c1", refusal.Competitions[0].ID)
			assert.Empty(t, f.comp(t, "c1").PublicNames, "nothing changed before the confirmation")
			stored, err := f.store.LoadTournament()
			require.NoError(t, err)
			if stored != nil {
				assert.Empty(t, stored.DefaultPublicNames)
			}

			body["confirmPublicNames"] = true
			w = f.send(tc.method, "/api/tournament", publicNamesPassword, body)
			require.Less(t, w.Code, 300, w.Body.String())
			stored, err = f.store.LoadTournament()
			require.NoError(t, err)
			require.NotNil(t, stored)
			assert.Equal(t, state.PublicNamesPerson, stored.DefaultPublicNames)
			assert.Equal(t, state.PublicNamesPerson, f.comp(t, "c1").PublicNames)
			assert.Equal(t, state.PublicNamesShown, f.comp(t, "c2").PublicNames, "Swiss is left alone")
		})
	}
}

// A POST over a self-run tournament that already holds a hiding default
// (hand-edited, or brought in by a restore) is not locked out: the refusal is
// for a default the save introduces, as the PUT's is.
func TestPublicNames_APostThatLeavesAStoredHidingDefaultAloneIsNotRefused(t *testing.T) {
	f := newPublicNamesFixture(t, state.TournamentModeSelfRun)
	stored, err := f.store.LoadTournament()
	require.NoError(t, err)
	edited := *stored
	edited.DefaultPublicNames = state.PublicNamesHidden
	require.NoError(t, f.store.SaveTournament(&edited))
	body := map[string]any{
		"name": stored.Name, "date": stored.Date, "courts": stored.Courts, "password": publicNamesPassword,
		"defaultPublicNames": state.PublicNamesHidden,
	}

	w := f.admin(http.MethodPost, "/api/tournament", body)
	require.Equal(t, http.StatusCreated, w.Code, w.Body.String())

	body["defaultPublicNames"] = state.PublicNamesPerson
	w = f.admin(http.MethodPost, "/api/tournament", body)
	require.Equal(t, http.StatusConflict, w.Code, "a hiding default the POST introduces is still refused")
	assert.Equal(t, codePublicNamesSelfRun, codeOf(t, w))
}

// A started competition cannot change its format, so switching it to Swiss is
// told that, and not first that its hidden names do not suit Swiss.
func TestPublicNames_AStartedHidingCompetitionSwitchedToSwissIsToldTheStartedRefusalFirst(t *testing.T) {
	f := newPublicNamesFixture(t, state.TournamentModeOfficiated)
	f.saveComp(t, &state.Competition{ID: "c1", Name: "Kata", Kind: "individual", Format: state.CompFormatKnockout, Status: state.CompStatusPools, PublicNames: state.PublicNamesHidden})
	w := f.admin(http.MethodPut, "/api/competitions/c1", map[string]any{"name": "Kata", "kind": "individual", "format": state.CompFormatSwiss, "swissRounds": 3, "courts": []string{"A"}})
	require.Equal(t, http.StatusConflict, w.Code, w.Body.String())
	assert.NotEqual(t, codePublicNamesSwiss, codeOf(t, w))
	assert.Contains(t, w.Body.String(), "format and kind can only be changed before the competition starts")
	assert.Equal(t, state.CompFormatKnockout, f.comp(t, "c1").Format)
}

func TestPublicNames_RevealLinks(t *testing.T) {
	t.Run("per competition answers the one key", func(t *testing.T) {
		f := newPublicNamesFixture(t, state.TournamentModeOfficiated)
		c := &state.Competition{ID: "c1", Name: "Kata", Kind: "individual", Format: state.CompFormatKnockout, PublicNames: state.PublicNamesCompetition, RevealSecret: strings.Repeat("34", 32), RevealUntil: "2099-01-01T10:00"}
		f.saveComp(t, c)
		w := f.admin(http.MethodGet, "/api/competitions/c1/reveal-links", nil)
		require.Equal(t, http.StatusOK, w.Code, w.Body.String())
		assert.Equal(t, "no-store", w.Header().Get("Cache-Control"))
		assert.NotContains(t, w.Body.String(), c.RevealSecret, "the secret never leaves the server")
		var got revealLinksResponse
		require.NoError(t, json.Unmarshal(w.Body.Bytes(), &got))
		assert.Equal(t, state.CompetitionRevealKey(c), got.CompetitionKey)
		assert.NotEmpty(t, got.CompetitionKey)
		assert.Empty(t, got.Competitors)
		assert.True(t, got.Open)
		assert.Equal(t, "2099-01-01T10:00", got.ExpiresAt)
	})
	t.Run("per person answers each competitor's key", func(t *testing.T) {
		f := newPublicNamesFixture(t, state.TournamentModeOfficiated)
		c := &state.Competition{ID: "c1", Name: "Kata", Kind: "individual", Format: state.CompFormatKnockout, PublicNames: state.PublicNamesPerson, RevealSecret: strings.Repeat("56", 32)}
		f.saveComp(t, c)
		require.NoError(t, f.store.SaveParticipants("c1", []domain.Player{{Name: "Sato", Dojo: "Tora"}, {Name: "Kato", Dojo: "Kuma"}}))
		players, err := f.store.LoadParticipants("c1", false)
		require.NoError(t, err)
		w := f.admin(http.MethodGet, "/api/competitions/c1/reveal-links", nil)
		require.Equal(t, http.StatusOK, w.Code, w.Body.String())
		var got revealLinksResponse
		require.NoError(t, json.Unmarshal(w.Body.Bytes(), &got))
		assert.Empty(t, got.CompetitionKey)
		require.Len(t, got.Competitors, 2)
		for i, p := range players {
			assert.Equal(t, p.ID, got.Competitors[i].ID)
			assert.Equal(t, state.CompetitorRevealKey(c, p.ID), got.Competitors[i].Key)
		}
		// The tournament ends 01-06-2026 with no Closing time: 01:00 the next morning.
		assert.Equal(t, "2026-06-02T01:00", got.ExpiresAt)
		assert.False(t, got.Open, "that time has passed")
	})
	t.Run("hidden and shown answer no key", func(t *testing.T) {
		f := newPublicNamesFixture(t, state.TournamentModeOfficiated)
		f.saveComp(t, &state.Competition{ID: "c1", Name: "Kata", Kind: "individual", Format: state.CompFormatKnockout, PublicNames: state.PublicNamesHidden, RevealSecret: strings.Repeat("78", 32)})
		w := f.admin(http.MethodGet, "/api/competitions/c1/reveal-links", nil)
		require.Equal(t, http.StatusOK, w.Code, w.Body.String())
		var got revealLinksResponse
		require.NoError(t, json.Unmarshal(w.Body.Bytes(), &got))
		assert.Empty(t, got.CompetitionKey)
		assert.Empty(t, got.Competitors)
		assert.False(t, got.Open)
	})
	t.Run("an unknown competition is 404", func(t *testing.T) {
		f := newPublicNamesFixture(t, state.TournamentModeOfficiated)
		w := f.admin(http.MethodGet, "/api/competitions/nope/reveal-links", nil)
		assert.Equal(t, http.StatusNotFound, w.Code, w.Body.String())
	})
	t.Run("organiser only, in self-run too", func(t *testing.T) {
		f := newPublicNamesFixture(t, state.TournamentModeSelfRun)
		f.saveComp(t, &state.Competition{ID: "c1", Name: "Kata", Kind: "individual", Format: state.CompFormatKnockout, PublicNames: state.PublicNamesCompetition, RevealSecret: strings.Repeat("9a", 32)})
		assert.True(t, isSelfRunMainGatedConfigRoute(http.MethodGet, "/api/competitions/:id/reveal-links"))
		w := f.send(http.MethodGet, "/api/competitions/c1/reveal-links", "", nil)
		assert.Equal(t, http.StatusUnauthorized, w.Code, w.Body.String())
		w = f.send(http.MethodGet, "/api/competitions/c1/reveal-links", "wrong", nil)
		assert.Equal(t, http.StatusUnauthorized, w.Code, w.Body.String())
		w = f.admin(http.MethodGet, "/api/competitions/c1/reveal-links", nil)
		assert.Equal(t, http.StatusOK, w.Code, w.Body.String())
	})
}

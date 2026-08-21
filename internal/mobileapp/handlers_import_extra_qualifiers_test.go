package mobileapp

import (
	"bytes"
	"encoding/json"
	"mime/multipart"
	"net/http"
	"net/http/httptest"
	"os"
	"testing"

	"github.com/gitrgoliveira/bracket-creator/internal/state"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// bc-qual review round: the tournament importer is the third write path for a
// competition, beside POST and PUT /competitions, and handlers_import.go's own
// comments call the rule it lives by "cross-file guard symmetry": every
// constraint the REST API applies to a field, the importer applies too, so a
// manifest cannot land a record the API would refuse.
//
// ExtraQualifiers broke that symmetry in both directions. The manifest had no
// key at all, so a competition an operator had set up with a non-default
// knockout-qualifier mode could not be expressed as a manifest -- and a
// manifest that tried had the key silently dropped by yaml.Unmarshal, which is
// worse than a rejection because it reads as accepted. Adding the key without
// its guard would then have been the mirror failure: an import persisting a
// pairing (maximum sizing, or pool_winners >= 2) whose draw generatePools
// rejects much later, with a message about pool formation rather than about the
// setting.
//
// Each case below states the guard it pins and what the operator gets without
// it.

func importOneComp(t *testing.T, r http.Handler, manifest string) ImportResult {
	t.Helper()
	body := &bytes.Buffer{}
	writer := multipart.NewWriter(body)

	manifestPart, err := writer.CreateFormFile("files", "manifest.yaml")
	require.NoError(t, err)
	_, err = manifestPart.Write([]byte(manifest))
	require.NoError(t, err)

	playersPart, err := writer.CreateFormFile("files", "players.csv")
	require.NoError(t, err)
	_, err = playersPart.Write([]byte("Player 1,Dojo A\nPlayer 2,Dojo B\nPlayer 3,Dojo C\n"))
	require.NoError(t, err)
	require.NoError(t, writer.Close())

	w := httptest.NewRecorder()
	req, _ := http.NewRequest("POST", "/api/tournament/import", body)
	req.Header.Set("Content-Type", writer.FormDataContentType())
	r.ServeHTTP(w, req)
	require.Equal(t, http.StatusOK, w.Code, "per-row validation is soft: the batch still answers 200")

	var resp map[string][]ImportResult
	require.NoError(t, json.Unmarshal(w.Body.Bytes(), &resp))
	require.Len(t, resp["results"], 1)
	return resp["results"][0]
}

func TestImport_ExtraQualifiers_RoundTripsAValidSetting(t *testing.T) {
	r, store, _, _, tempDir := setupTestRouter(t)
	defer os.RemoveAll(tempDir)

	res := importOneComp(t, r, `
competitions:
  - id: "eq-valid"
    name: "Valid Qualifiers"
    kind: "individual"
    format: "mixed"
    courts: ["A", "B"]
    pool_size: 3
    pool_size_mode: "min"
    pool_winners: 1
    extra_qualifiers: "larger-pools"
    participants: "players.csv"
`)
	require.Empty(t, res.Error)

	comp, err := store.LoadCompetition("eq-valid")
	require.NoError(t, err)
	require.NotNil(t, comp)
	assert.Equal(t, state.ExtraQualifiersLargerPools, comp.ExtraQualifiers,
		"the manifest key was dropped: an operator cannot express this competition as a manifest at all")
}

func TestImport_ExtraQualifiers_RejectsAnUnknownValue(t *testing.T) {
	r, store, _, _, tempDir := setupTestRouter(t)
	defer os.RemoveAll(tempDir)

	res := importOneComp(t, r, `
competitions:
  - id: "eq-unknown"
    name: "Unknown Qualifiers"
    kind: "individual"
    format: "mixed"
    courts: ["A", "B"]
    pool_size: 3
    pool_size_mode: "min"
    pool_winners: 1
    extra_qualifiers: "oversized"
    participants: "players.csv"
`)
	assert.Contains(t, res.Error, "extraQualifiers",
		"an unknown value must be refused here, exactly as POST /competitions refuses it")

	comp, err := store.LoadCompetition("eq-unknown")
	require.NoError(t, err)
	assert.Nil(t, comp, "a refused row must not reach disk")
}

func TestImport_ExtraQualifiers_RejectsMaximumSizingPairing(t *testing.T) {
	r, store, _, _, tempDir := setupTestRouter(t)
	defer os.RemoveAll(tempDir)

	res := importOneComp(t, r, `
competitions:
  - id: "eq-max"
    name: "Max Sizing Qualifiers"
    kind: "individual"
    format: "mixed"
    courts: ["A", "B"]
    pool_size: 3
    pool_size_mode: "max"
    pool_winners: 1
    extra_qualifiers: "fill-bracket"
    participants: "players.csv"
`)
	assert.Contains(t, res.Error, "minimum-players-per-pool sizing",
		"stored unchecked, this pairing surfaces at generate-draw as a pool-formation complaint instead")

	comp, err := store.LoadCompetition("eq-max")
	require.NoError(t, err)
	assert.Nil(t, comp)
}

// The PoolSizeMode default is applied by the importer, not the manifest, so
// validation has to run AFTER it: an omitted pool_size_mode means "max" on
// disk, and validating the empty value would read it as minimum sizing, accept
// the mode, and store the pair the rule forbids.
func TestImport_ExtraQualifiers_ValidatedAgainstTheDefaultedSizingMode(t *testing.T) {
	r, store, _, _, tempDir := setupTestRouter(t)
	defer os.RemoveAll(tempDir)

	res := importOneComp(t, r, `
competitions:
  - id: "eq-default-mode"
    name: "Defaulted Sizing"
    kind: "individual"
    format: "mixed"
    courts: ["A", "B"]
    pool_size: 3
    pool_winners: 1
    extra_qualifiers: "larger-pools"
    participants: "players.csv"
`)
	assert.Contains(t, res.Error, "minimum-players-per-pool sizing",
		"an omitted pool_size_mode defaults to maximum, so this row is the forbidden pairing and must be refused")

	comp, err := store.LoadCompetition("eq-default-mode")
	require.NoError(t, err)
	assert.Nil(t, comp)
}

// A format with no pool-fed knockout cannot mean anything by this setting, and
// POST /competitions zeroes it rather than refusing. The importer shares that
// statement (normalizeExtraQualifiers) instead of carrying its own copy, so the
// two cannot answer differently for the same record.
func TestImport_ExtraQualifiers_ZeroedForAFormatWithNoPoolFedKnockout(t *testing.T) {
	r, store, _, _, tempDir := setupTestRouter(t)
	defer os.RemoveAll(tempDir)

	res := importOneComp(t, r, `
competitions:
  - id: "eq-league"
    name: "League With Qualifiers"
    kind: "individual"
    format: "league"
    courts: ["A", "B"]
    pool_size: 3
    pool_size_mode: "min"
    pool_winners: 1
    extra_qualifiers: "fill-bracket"
    participants: "players.csv"
`)
	require.Empty(t, res.Error, "a league is not refused for this, it is normalized")

	comp, err := store.LoadCompetition("eq-league")
	require.NoError(t, err)
	require.NotNil(t, comp)
	assert.Equal(t, state.ExtraQualifiersNone, comp.ExtraQualifiers,
		"a league kept a knockout-qualifier mode on disk that its draw can never honour")
}

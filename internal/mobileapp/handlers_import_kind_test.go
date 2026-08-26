package mobileapp

import (
	"os"
	"strconv"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// bc-symm-settings-create-parity, review round: the same cross-file guard
// symmetry TestImport_ExtraQualifiers_* pins for the qualifier setting, now
// for Kind and TeamSize. The importer is the third write path for a
// competition beside POST and PUT /competitions, and it writes straight
// through store.SaveCompetitionChanged rather than via those handlers, so a
// constraint added to them does not reach it on its own.
//
// Kind is the sharper of the two, for the reason ValidateCompetitionKind's
// own doc comment gives: an unrecognised Kind is not rejected anywhere
// downstream. It simply runs as "individual" through the whole engine, so
// `kind: banana` produced a competition that looked accepted, behaved as
// something else, and reported no error at any layer. It could not even be
// corrected in place afterwards: the settings PUT's kind guard is
// deliberately change-scoped, so it never speaks up about a stored value on
// its own account.
//
// TeamSize rides along because the manifest carries `team_size` as an
// independent key with no cross-check against `kind`, and
// `kind: individual` + `team_size: 3` is a pairing every other write path
// refuses outright.
//
// importOneComp lives in handlers_import_extra_qualifiers_test.go.

func TestImport_RejectsAnUnrecognisedKind(t *testing.T) {
	r, store, _, _, tempDir := setupTestRouter(t)
	defer os.RemoveAll(tempDir)

	res := importOneComp(t, r, `
competitions:
  - id: "kind-unknown"
    name: "Unknown Kind"
    kind: "banana"
    format: "playoffs"
    courts: ["A"]
    participants: "players.csv"
`)
	assert.Contains(t, res.Error, "kind",
		"an unrecognised kind must be refused here, exactly as POST /competitions refuses it; nothing downstream rejects it, the engine just runs it as individual")

	comp, err := store.LoadCompetition("kind-unknown")
	require.NoError(t, err)
	assert.Nil(t, comp, "a refused row must not reach disk")
}

func TestImport_AcceptsTheKindsTheServerRecognises(t *testing.T) {
	// "" is in the accepted set on purpose -- see ValidateCompetitionKind --
	// so a manifest that simply omits the key keeps working.
	for _, tc := range []struct {
		name     string
		id       string
		kindLine string
		teamLine string
	}{
		{"individual", "kind-individual", "    kind: \"individual\"\n", ""},
		{"team", "kind-team", "    kind: \"team\"\n", "    team_size: 3\n"},
		{"omitted", "kind-omitted", "", ""},
	} {
		t.Run(tc.name, func(t *testing.T) {
			r, store, _, _, tempDir := setupTestRouter(t)
			defer os.RemoveAll(tempDir)

			res := importOneComp(t, r, `
competitions:
  - id: "`+tc.id+`"
    name: "Kind `+tc.name+`"
`+tc.kindLine+tc.teamLine+`    format: "playoffs"
    courts: ["A"]
    participants: "players.csv"
`)
			require.Empty(t, res.Error)

			comp, err := store.LoadCompetition(tc.id)
			require.NoError(t, err)
			require.NotNil(t, comp, "a legal kind must still import")
		})
	}
}

func TestImport_RejectsATeamSizeItsKindForbids(t *testing.T) {
	for _, tc := range []struct {
		name string
		id   string
		kind string
		size int
	}{
		// ValidateCompetitionTeamSize: a non-team kind requires exactly 0.
		{"individual with a team size", "ts-individual", "individual", 3},
		// ... and 1 is rejected for every kind, being neither.
		{"team size of one", "ts-one", "team", 1},
		// ... and a team needs at least 2.
		{"team with no size", "ts-team-zero", "team", 0},
	} {
		t.Run(tc.name, func(t *testing.T) {
			r, store, _, _, tempDir := setupTestRouter(t)
			defer os.RemoveAll(tempDir)

			res := importOneComp(t, r, `
competitions:
  - id: "`+tc.id+`"
    name: "Team size `+tc.name+`"
    kind: "`+tc.kind+`"
    team_size: `+strconv.Itoa(tc.size)+`
    format: "playoffs"
    courts: ["A"]
    participants: "players.csv"
`)
			assert.Contains(t, res.Error, "teamSize",
				"the manifest carries kind and team_size as independent keys, so without this guard it lands a pairing POST and PUT both refuse")

			comp, err := store.LoadCompetition(tc.id)
			require.NoError(t, err)
			assert.Nil(t, comp, "a refused row must not reach disk")
		})
	}
}

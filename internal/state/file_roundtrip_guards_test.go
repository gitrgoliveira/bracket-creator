package state

import (
	"reflect"
	"strings"
	"testing"

	"github.com/gitrgoliveira/bracket-creator/internal/domain"
	"github.com/gitrgoliveira/bracket-creator/internal/helper"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// This file extends TestPoolMatchRoundTripIsComplete's guarantee to the other
// hand-maintained CSV files: participants.csv, pools.csv and seeds.csv. Each
// guard sweeps the persisted struct's exported fields via
// reflection, gives every field a distinctive value, and fails when a field
// neither survives a save + reload from a FRESH store nor appears in that
// file's allow-list with the reason it is legitimately absent.
//
// Why: these files persist by hand-written column code, and nothing fails
// when a new field misses it. That is how DecisionBy, DecisionReason, Encho
// and ModifiedAt were lost from pool-matches.csv, and how SeedAssignment.Dojo
// was lost from seeds.csv while the (name, dojo) matching in ApplySeeds
// depended on it. The struct-marshalled files (bracket.json, config.md,
// competitor-status.yaml, lineups.yaml, overrides.json, tournament.md) are
// immune by construction and need no per-field guard here;
// TestMarshalledStructsStayFullyMarshalled below pins the property that makes
// them immune, checking each type against the tag its file marshals under.

// sweepFields compares in against got field-by-field, consulting allowlist for
// fields that are legitimately not persisted in this file.
func sweepFields[T any](t *testing.T, file string, in, got T, allowlist map[string]string) {
	t.Helper()
	inV, gotV := reflect.ValueOf(in), reflect.ValueOf(got)
	typ := inV.Type()
	for i := range typ.NumField() {
		f := typ.Field(i)
		if !f.IsExported() {
			continue
		}
		if reason, skip := allowlist[f.Name]; skip {
			assert.NotEmptyf(t, reason, "%s needs a reason, not an empty string", f.Name)
			continue
		}
		assert.Equalf(t, inV.Field(i).Interface(), gotV.Field(i).Interface(),
			"%s.%s did not survive a %s save/reload. Either persist it or add it "+
				"to this guard's allow-list with the reason it is transient.",
			typ.Name(), f.Name, file)
	}
	// The allow-list must name real fields: a rename that leaves a stale entry
	// behind would silently stop covering the field it was meant to exempt.
	for name := range allowlist {
		_, ok := typ.FieldByName(name)
		assert.Truef(t, ok, "allow-list names %q, which is not a %s field", name, typ.Name())
	}
}

func TestParticipantRoundTripIsComplete(t *testing.T) {
	notPersisted := map[string]string{
		"PoolPosition": "pool-draw ordering, owned by pools.csv (whose position " +
			"column is derived from row order at save); participants.csv conveys " +
			"roster order by row order alone, and the field is json:\"-\" for the " +
			"same reason.",
		"Seed": "owned by seeds.csv (SaveSeeds) and merged back at load; " +
			"persisting it here too would create a second source of truth that " +
			"could disagree.",
		"Number": "the competitor tag is assigned at pool-draw time and owned by " +
			"pools.csv; participants.csv predates the draw and handlers re-merge " +
			"the number from the pools (mergePoolNumbersIntoPlayersSlice).",
	}

	dir := t.TempDir()
	s, err := NewStore(dir)
	require.NoError(t, err)
	// WithZekkenName on: the superset column layout (the DisplayName column
	// only exists in zekken mode).
	require.NoError(t, s.SaveCompetition(&Competition{ID: "c", Name: "C", WithZekkenName: true}))

	in := domain.Player{
		ID:          "3f2504e0-4f89-41d3-9a0c-0305e82c3301", // must look like a UUID for the ID-column autodetect
		Name:        "Tanaka Ichiro",
		DisplayName: "TANAKA",
		Dojo:        "Kyoto",
		Metadata:    []string{"3 Dan"},
		Source:      "manual",
		CheckedIn:   true,
	}
	require.NoError(t, s.SaveParticipants("c", []domain.Player{in}))

	fresh, err := NewStore(dir)
	require.NoError(t, err)
	loaded, err := fresh.LoadParticipants("c", true)
	require.NoError(t, err)
	require.Len(t, loaded, 1)

	sweepFields(t, "participants.csv", in, loaded[0], notPersisted)
}

func TestPoolPlayerRoundTripIsComplete(t *testing.T) {
	notPersisted := map[string]string{
		"Metadata": "pools.csv is a draw projection; roster truth (grades, " +
			"member names) lives in participants.csv and nothing reads it off a " +
			"pool player.",
		"Source":    "registration provenance, roster truth in participants.csv; same reason.",
		"CheckedIn": "attendance, roster truth in participants.csv; same reason.",
	}

	dir := t.TempDir()
	s, err := NewStore(dir)
	require.NoError(t, err)
	require.NoError(t, s.SaveCompetition(&Competition{ID: "c", Name: "C"}))

	in := helper.Player{
		ID:          "3f2504e0-4f89-41d3-9a0c-0305e82c3301",
		Name:        "Tanaka Ichiro",
		DisplayName: "TANAKA",
		Dojo:        "Kyoto",
		Seed:        3,
		Number:      "A7",
		// The position column is DERIVED from slice order at save (writer
		// stores the 0-based index; reader returns it 1-based), so the value
		// that round-trips for the first player is 1. Setting anything else
		// here would test the input value, not the file.
		PoolPosition: 1,
	}
	require.NoError(t, s.SavePools("c", []helper.Pool{{PoolName: "Pool A", Players: []helper.Player{in}}}))

	fresh, err := NewStore(dir)
	require.NoError(t, err)
	pools, err := fresh.LoadPools("c")
	require.NoError(t, err)
	require.Len(t, pools, 1)
	require.Len(t, pools[0].Players, 1)
	assert.Equal(t, "Pool A", pools[0].PoolName)

	sweepFields(t, "pools.csv", in, pools[0].Players[0], notPersisted)
}

func TestSeedAssignmentRoundTripIsComplete(t *testing.T) {
	dir := t.TempDir()
	s, err := NewStore(dir)
	require.NoError(t, err)
	require.NoError(t, s.SaveCompetition(&Competition{ID: "c", Name: "C"}))

	// Dojo matters: a seed assignment is matched to its participant by
	// (name, dojo) because names are not unique within a competition. It was
	// silently dropped by the writer until this guard existed, which made a
	// seed for either of two same-named players unresolvable after a restart.
	in := domain.SeedAssignment{Name: "Tanaka Ichiro", Dojo: "Kyoto", SeedRank: 1}
	require.NoError(t, s.SaveSeeds("c", []domain.SeedAssignment{in}))

	fresh, err := NewStore(dir)
	require.NoError(t, err)
	loaded, err := fresh.LoadSeeds("c")
	require.NoError(t, err)
	require.Len(t, loaded, 1)

	sweepFields(t, "seeds.csv", in, loaded[0], nil)
}

// TestMarshalledStructsStayFullyMarshalled pins the property that exempts the
// struct-marshalled files from the guards above: they persist by marshalling
// the WHOLE struct (bracket.json for Bracket/BracketMatch, the SubResults cell
// of pool-matches.csv for SubMatchResult, config.md front-matter for
// Competition, tournament.md for Tournament, competitor-status.yaml and
// lineups.yaml for their domain payloads, overrides.json for Overrides), so a
// new field is persisted the moment it is declared, with no column code to
// forget.
//
// That immunity holds exactly as long as no field opts out of marshalling
// UNDER THE TAG ITS FILE USES: `yaml:"-"` on a YAML-persisted struct is the
// same silent loss as `json:"-"` on a JSON-persisted one, and a json-only
// sweep cannot see it. The opt-out is sometimes right (Competition.Players is
// a view assembled from participants.csv, not config.md data), so this test
// does not forbid it; it forces the same decision-in-the-open the pool guard
// forces, via an allow-list naming each opted-out field and why.
func TestMarshalledStructsStayFullyMarshalled(t *testing.T) {
	// One row per marshalled type: the type, the tag key its file marshals
	// under, and the fields it may legitimately opt out of marshalling (with
	// the reason). Adding a `<tag>:"-"` field to any of these types fails
	// this test until the reason lands in this table rather than in a
	// silent tag.
	notMarshalled := []struct {
		typ   reflect.Type
		tag   string
		allow map[string]string
	}{
		{reflect.TypeOf(SubMatchResult{}), "json", nil},
		{reflect.TypeOf(BracketMatch{}), "json", nil},
		{reflect.TypeOf(Bracket{}), "json", nil},
		{reflect.TypeOf(Overrides{}), "json", nil},
		{reflect.TypeOf(Tournament{}), "yaml", nil},
		{reflect.TypeOf(domain.CompetitorStatus{}), "yaml", nil},
		{reflect.TypeOf(domain.TeamLineup{}), "yaml", nil},
		{reflect.TypeOf(Competition{}), "yaml", map[string]string{
			"Players": "a view, not config data: the roster is assembled onto " +
				"the struct from participants.csv (and seeds.csv) at load time; " +
				"marshalling it into config.md front-matter would store a second " +
				"copy that the next roster write silently outdates.",
		}},
	}

	for _, row := range notMarshalled {
		typ, tagKey, allow := row.typ, row.tag, row.allow
		for i := range typ.NumField() {
			f := typ.Field(i)
			if !f.IsExported() {
				continue
			}
			if reason, ok := allow[f.Name]; ok {
				assert.NotEmptyf(t, reason, "%s.%s needs a reason", typ.Name(), f.Name)
				continue
			}
			tag := f.Tag.Get(tagKey)
			name, _, _ := strings.Cut(tag, ",")
			assert.NotEqualf(t, "-", name,
				"%s.%s is %s:\"-\" and therefore NOT persisted, silently. If that "+
					"is intended, add it to the notMarshalled allow-list with the "+
					"reason; if not, it must carry a real %s tag.",
				typ.Name(), f.Name, tagKey, tagKey)
		}
	}
}

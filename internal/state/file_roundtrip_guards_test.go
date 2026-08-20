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
// hand-maintained CSV files: participants.csv, pools.csv, schedule.csv and
// seeds.csv. Each guard sweeps the persisted struct's exported fields via
// reflection, gives every field a distinctive value, and fails when a field
// neither survives a save + reload from a FRESH store nor appears in that
// file's allow-list with the reason it is legitimately absent.
//
// Why: these files persist by hand-written column code, and nothing fails
// when a new field misses it. That is how DecisionBy, DecisionReason, Encho
// and ModifiedAt were lost from pool-matches.csv, and how SeedAssignment.Dojo
// was lost from seeds.csv while the (name, dojo) matching in ApplySeeds
// depended on it. The struct-marshalled files (bracket.json, config.md,
// competitor-status.yaml, lineups.yaml, overrides.json) are immune by
// construction and need no guard here; TestMarshalledStructsStayFullyMarshalled
// below pins the property that makes them immune.

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

func TestScheduleEntryRoundTripIsComplete(t *testing.T) {
	dir := t.TempDir()
	s, err := NewStore(dir)
	require.NoError(t, err)
	require.NoError(t, s.SaveCompetition(&Competition{ID: "c", Name: "C"}))

	in := ScheduleEntry{
		MatchType:   "pool",
		MatchRef:    "Pool A-1",
		Court:       "B",
		Date:        "21-08-2026",
		ScheduledAt: "09:45",
		Status:      "scheduled",
		IsBreak:     true,
		Label:       "Lunch",
	}
	_, err = s.SaveScheduleChanged("c", []ScheduleEntry{in})
	require.NoError(t, err)

	fresh, err := NewStore(dir)
	require.NoError(t, err)
	loaded, err := fresh.LoadSchedule("c")
	require.NoError(t, err)
	require.Len(t, loaded, 1)

	// Every ScheduleEntry field is persisted; no allow-list.
	sweepFields(t, "schedule.csv", in, loaded[0], nil)
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
// JSON-persisted match structures from the guards above: they are persisted by
// marshalling the WHOLE struct (bracket.json for Bracket/BracketMatch; the
// SubResults cell of pool-matches.csv for SubMatchResult), so a new field is
// persisted the moment it is declared, with no column code to forget.
//
// That immunity holds exactly as long as no field opts out of marshalling. A
// `json:"-"` tag is that opt-out, and it is sometimes right (MatchResult's
// WinnerSide is a transient handler hint), so this test does not forbid it; it
// forces the same decision-in-the-open the pool guard forces, via an
// allow-list naming each opted-out field and why it is transient.
func TestMarshalledStructsStayFullyMarshalled(t *testing.T) {
	notMarshalled := map[string]map[string]string{
		"SubMatchResult": {},
		"BracketMatch":   {},
		"Bracket":        {},
	}

	for _, typ := range []reflect.Type{
		reflect.TypeOf(SubMatchResult{}),
		reflect.TypeOf(BracketMatch{}),
		reflect.TypeOf(Bracket{}),
	} {
		allow := notMarshalled[typ.Name()]
		require.NotNilf(t, allow, "add %s to the notMarshalled table", typ.Name())
		for i := range typ.NumField() {
			f := typ.Field(i)
			if !f.IsExported() {
				continue
			}
			if reason, ok := allow[f.Name]; ok {
				assert.NotEmptyf(t, reason, "%s.%s needs a reason", typ.Name(), f.Name)
				continue
			}
			tag := f.Tag.Get("json")
			name, _, _ := strings.Cut(tag, ",")
			assert.NotEqualf(t, "-", name,
				"%s.%s is json:\"-\" and therefore NOT persisted, silently. If that "+
					"is intended, add it to notMarshalled with the reason; if not, it "+
					"must carry a real json tag.", typ.Name(), f.Name)
		}
	}
}

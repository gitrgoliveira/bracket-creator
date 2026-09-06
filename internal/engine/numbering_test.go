package engine

import (
	"bytes"
	"fmt"
	"log"
	"os"
	"path/filepath"
	"runtime"
	"testing"

	"github.com/gitrgoliveira/bracket-creator/internal/helper"
	"github.com/gitrgoliveira/bracket-creator/internal/state"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// TestRenumberCompetitors_RewritesUnderNewPrefix is the basic in-place
// rewrite: pool membership, order and PoolPosition are untouched, only
// Number changes, and it changes for every competitor in every pool (bc-pnum
// G1/G4 -- ONE counter running through the pools in their given order).
func TestRenumberCompetitors_RewritesUnderNewPrefix(t *testing.T) {
	eng, store, _ := setupTestEngine(t)
	const compID = "renumber-rewrite"

	require.NoError(t, store.SaveCompetition(&state.Competition{
		ID: compID, Name: "Renumber Rewrite", Kind: "individual", Format: "mixed",
		Status: "draw-ready", NumberPrefix: "X",
	}))
	require.NoError(t, store.SavePools(compID, []helper.Pool{
		{PoolName: "Pool A", Players: []helper.Player{
			{ID: "p1", Name: "Alice", Dojo: "Dojo Alice", PoolPosition: 1, Number: "Y1"},
			{ID: "p2", Name: "Bob", Dojo: "Dojo Bob", PoolPosition: 2, Number: "Y2"},
		}},
		{PoolName: "Pool B", Players: []helper.Player{
			{ID: "p3", Name: "Carol", Dojo: "Dojo Carol", PoolPosition: 1, Number: "Y3"},
		}},
	}))

	changed, err := eng.RenumberCompetitors(compID)
	require.NoError(t, err)
	assert.True(t, changed, "a rewrite that actually renumbers must report changed=true")

	pools, err := store.LoadPools(compID)
	require.NoError(t, err)
	require.Len(t, pools, 2)
	require.Len(t, pools[0].Players, 2)
	require.Len(t, pools[1].Players, 1)

	assert.Equal(t, "X1", pools[0].Players[0].Number)
	assert.Equal(t, "X2", pools[0].Players[1].Number)
	assert.Equal(t, "X3", pools[1].Players[0].Number, "the counter must continue into the second pool, not restart")

	// Membership, order and identity are untouched.
	assert.Equal(t, "Alice", pools[0].Players[0].Name)
	assert.Equal(t, "p1", pools[0].Players[0].ID)
	assert.EqualValues(t, 1, pools[0].Players[0].PoolPosition)
	assert.Equal(t, "Carol", pools[1].Players[0].Name)
}

// TestRenumberCompetitors_TrimsPaddedPrefix is PR #416 finding 11: the
// blank-prefix guard checked strings.TrimSpace(comp.NumberPrefix), but
// helper.NumberPools was handed the RAW, untrimmed comp.NumberPrefix -- so a
// prefix padded with real (non-whitespace-only) surrounding whitespace, e.g.
// " X" (a value a hand-edited config.md or an upstream trim gap could leave
// behind), passed the guard on its trimmed form yet still stamped the
// untrimmed value, whitespace and all, onto every competitor number.
func TestRenumberCompetitors_TrimsPaddedPrefix(t *testing.T) {
	eng, store, _ := setupTestEngine(t)
	const compID = "renumber-padded-prefix"

	require.NoError(t, store.SaveCompetition(&state.Competition{
		ID: compID, Name: "Renumber Padded Prefix", Kind: "individual", Format: "mixed",
		Status: "draw-ready", NumberPrefix: " X ",
	}))
	require.NoError(t, store.SavePools(compID, []helper.Pool{
		{PoolName: "Pool A", Players: []helper.Player{
			{ID: "p1", Name: "Alice", Dojo: "Dojo Alice", PoolPosition: 1, Number: "Y1"},
		}},
	}))

	changed, err := eng.RenumberCompetitors(compID)
	require.NoError(t, err)
	assert.True(t, changed)

	pools, err := store.LoadPools(compID)
	require.NoError(t, err)
	require.Len(t, pools, 1)
	require.Len(t, pools[0].Players, 1)
	assert.Equal(t, "X1", pools[0].Players[0].Number, "the number must be composed from the TRIMMED prefix, not the raw padded one")
}

// TestRenumberCompetitors_Idempotent_SecondCallDoesNotRewriteTheFile pins G4's
// idempotency: a second call over an already-correct pools.csv must not touch
// the file at all (neither its bytes nor its mtime), because the settings PUT
// handler now calls RenumberCompetitors unconditionally on EVERY successful
// save, including one that never touched numberPrefix. If this regresses to
// an unconditional rewrite, an operator saving an unrelated field (say, the
// competition date) on every draw-ready competition would rewrite every
// pools.csv on every save for no reason.
func TestRenumberCompetitors_Idempotent_SecondCallDoesNotRewriteTheFile(t *testing.T) {
	eng, store, dir := setupTestEngine(t)
	const compID = "renumber-idempotent"

	require.NoError(t, store.SaveCompetition(&state.Competition{
		ID: compID, Name: "Renumber Idempotent", Kind: "individual", Format: "mixed",
		Status: "draw-ready", NumberPrefix: "K",
	}))
	require.NoError(t, store.SavePools(compID, []helper.Pool{
		{PoolName: "Pool A", Players: []helper.Player{
			{ID: "p1", Name: "Alice", Dojo: "Dojo Alice", PoolPosition: 1, Number: "K1"},
			{ID: "p2", Name: "Bob", Dojo: "Dojo Bob", PoolPosition: 2, Number: "K2"},
		}},
	}))

	poolsPath := dir + "/competitions/" + compID + "/pools.csv"
	before, err := os.ReadFile(poolsPath)
	require.NoError(t, err)

	// Byte-equality alone can't distinguish "skipped the write" from "wrote
	// the exact same content" (both leave identical bytes). pools.csv does
	// carry a version counter now (bc-pnum A2: engine.standingsTokens keys on
	// it too, so a real writer must bump it), but this test asserts the
	// stronger claim -- no write attempt at all, not merely one that left the
	// counter alone -- by making any REAL write observably fail: atomicWrite
	// creates a sibling temp file before renaming it over pools.csv, which
	// needs write permission on the DIRECTORY, not the file itself, so a
	// read-only competition directory turns "attempted a write" into an
	// error. A genuine no-op (nothing differs) never touches the directory at
	// all, so it still succeeds. Same technique atomic_write_test.go uses for
	// the same reason.
	if runtime.GOOS == "windows" {
		t.Skip("chmod 0500 isn't enforced on Windows the same way")
	}
	if os.Getuid() == 0 {
		t.Skip("Skipping permission test: root bypasses file permission restrictions")
	}
	compDir := dir + "/competitions/" + compID
	require.NoError(t, os.Chmod(compDir, 0o500))
	defer func() { _ = os.Chmod(compDir, 0o700) }() // let t.TempDir() cleanup remove it

	// The numbers already match the stored prefix, so this call has nothing
	// to change and must never attempt to write pools.csv.
	changed, err := eng.RenumberCompetitors(compID)
	require.NoError(t, err, "a no-op renumber must never attempt to write pools.csv")
	assert.False(t, changed, "a no-op renumber must report changed=false")

	require.NoError(t, os.Chmod(compDir, 0o700))
	after, err := os.ReadFile(poolsPath)
	require.NoError(t, err)
	assert.Equal(t, before, after, "pools.csv bytes must be unchanged when no number actually differs")
}

// TestRenumberCompetitors_HealsAPreviouslyUnhealedFile stands in for "a
// settings save after a renumber failure heals pools.csv" (bc-pnum G4): the
// function has no way to distinguish "a prior renumber failed" from "the
// prefix moved and nothing has reconciled pools.csv since" -- on disk both
// look identical, a pools.csv whose numbers don't match the stored
// NumberPrefix -- and it heals both the same way, unconditionally, on the
// very next call. A pools.csv with an entirely empty Number column (G7's
// legacy shape, e.g. drawn before this rule existed) is the same case again.
func TestRenumberCompetitors_HealsAPreviouslyUnhealedFile(t *testing.T) {
	eng, store, _ := setupTestEngine(t)
	const compID = "renumber-heal"

	require.NoError(t, store.SaveCompetition(&state.Competition{
		ID: compID, Name: "Renumber Heal", Kind: "individual", Format: "mixed",
		Status: "draw-ready", NumberPrefix: "K",
	}))
	// pools.csv left over from a prefix change that never got renumbered
	// (or, equally, a legacy file whose Number column was never populated
	// at all -- both are just "stale/blank on disk").
	require.NoError(t, store.SavePools(compID, []helper.Pool{
		{PoolName: "Pool A", Players: []helper.Player{
			{ID: "p1", Name: "Alice", Dojo: "Dojo Alice", PoolPosition: 1, Number: "OLD1"},
			{ID: "p2", Name: "Bob", Dojo: "Dojo Bob", PoolPosition: 2, Number: ""},
		}},
	}))

	changed, err := eng.RenumberCompetitors(compID)
	require.NoError(t, err)
	assert.True(t, changed, "healing a stale/blank Number column must report changed=true")

	pools, err := store.LoadPools(compID)
	require.NoError(t, err)
	require.Len(t, pools, 1)
	assert.Equal(t, "K1", pools[0].Players[0].Number)
	assert.Equal(t, "K2", pools[0].Players[1].Number)
}

// TestRenumberCompetitors_NoPools_IsANoOp covers the playoffs-only /
// not-yet-drawn case: no pools.csv exists, so there is nothing to rewrite,
// and the call must not error or create one.
func TestRenumberCompetitors_NoPools_IsANoOp(t *testing.T) {
	eng, store, dir := setupTestEngine(t)
	const compID = "renumber-no-pools"

	require.NoError(t, store.SaveCompetition(&state.Competition{
		ID: compID, Name: "Renumber No Pools", Kind: "individual", Format: "playoffs",
		Status: "setup", NumberPrefix: "K",
	}))

	changed, err := eng.RenumberCompetitors(compID)
	require.NoError(t, err)
	assert.False(t, changed, "a competition with no pools.csv has nothing to change")

	_, err = os.Stat(dir + "/competitions/" + compID + "/pools.csv")
	assert.True(t, os.IsNotExist(err), "no pools.csv should be created for a competition with none")
}

// TestRenumberCompetitors_EmptyPrefix_Refused pins bc-pnum A3's second line
// of defense: even though every app caller is supposed to assign a default
// prefix before calling this (G2), a stored blank prefix (a bug state
// upstream, e.g. an inherit step that was skipped) must never reach
// helper.NumberPools, which would compose bare "1","2","3" -- no letters at
// all -- into pools.csv. This must error and leave pools.csv untouched
// rather than silently write bare digits.
func TestRenumberCompetitors_EmptyPrefix_Refused(t *testing.T) {
	eng, store, _ := setupTestEngine(t)
	const compID = "renumber-empty-prefix"

	require.NoError(t, store.SaveCompetition(&state.Competition{
		ID: compID, Name: "Renumber Empty Prefix", Kind: "individual", Format: "mixed",
		Status: "pools", NumberPrefix: "",
	}))
	require.NoError(t, store.SavePools(compID, []helper.Pool{
		{PoolName: "Pool A", Players: []helper.Player{
			{ID: "p1", Name: "Alice", Dojo: "Dojo Alice", PoolPosition: 1},
			{ID: "p2", Name: "Bob", Dojo: "Dojo Bob", PoolPosition: 2},
		}},
	}))

	changed, err := eng.RenumberCompetitors(compID)
	require.Error(t, err, "a blank stored prefix must refuse rather than write bare digits")
	assert.False(t, changed)

	pools, err := store.LoadPools(compID)
	require.NoError(t, err)
	for _, p := range pools[0].Players {
		assert.Empty(t, p.Number, "the refusal must leave pools.csv untouched, not write bare digit numbers")
	}
}

// TestRenumberCompetitors_NotFound pins the 404 sentinel for an unknown
// competition id, matching every other engine entry point's NotFoundError
// convention.
func TestRenumberCompetitors_NotFound(t *testing.T) {
	eng, _, _ := setupTestEngine(t)

	changed, err := eng.RenumberCompetitors("does-not-exist")
	require.Error(t, err)
	assert.False(t, changed, "a not-found error must report changed=false, not a partial success")
	var nfe *NotFoundError
	assert.ErrorAs(t, err, &nfe, "unknown compID must return NotFoundError")
}

// TestMigrateNumberPrefixes pins the load-time migration: competitions saved
// without a prefix get the derived default, unique against every other
// competition INCLUDING the ones assigned in the same pass, their pools.csv
// is numbered under it, prefixed competitions are untouched, and a second run
// is a no-op.
func TestMigrateNumberPrefixes(t *testing.T) {
	store, err := state.NewStore(t.TempDir())
	require.NoError(t, err)
	eng := New(store)

	require.NoError(t, store.SaveCompetition(&state.Competition{ID: "kendo", Name: "Kendo", Format: state.CompFormatMixed, NumberPrefix: "K", Status: state.CompStatusPools}))
	require.NoError(t, store.SavePools("kendo", []helper.Pool{{PoolName: "Pool A", Players: []helper.Player{{Name: "A", Dojo: "D", Number: "K1"}}}}))
	// Two legacy competitions whose initials both start with K: the first
	// must avoid "K" (taken by kendo) and the second must avoid what the
	// first was just given.
	require.NoError(t, store.SaveCompetition(&state.Competition{ID: "kendo-open", Name: "Kendo Open", Format: state.CompFormatMixed, Status: state.CompStatusPools}))
	require.NoError(t, store.SavePools("kendo-open", []helper.Pool{{PoolName: "Pool A", Players: []helper.Player{{Name: "B", Dojo: "D"}, {Name: "C", Dojo: "D"}}}}))
	require.NoError(t, store.SaveCompetition(&state.Competition{ID: "kendo-open-2", Name: "Kendo Open", Format: state.CompFormatMixed, Status: state.CompStatusSetup}))

	migrated, err := eng.MigrateNumberPrefixes()
	require.NoError(t, err)
	assert.ElementsMatch(t, []string{"kendo-open", "kendo-open-2"}, migrated)

	untouched, err := store.LoadCompetition("kendo")
	require.NoError(t, err)
	assert.Equal(t, "K", untouched.NumberPrefix)
	first, err := store.LoadCompetition("kendo-open")
	require.NoError(t, err)
	second, err := store.LoadCompetition("kendo-open-2")
	require.NoError(t, err)
	assert.NotEmpty(t, first.NumberPrefix)
	assert.NotEmpty(t, second.NumberPrefix)
	assert.NotEqual(t, "K", first.NumberPrefix)
	assert.NotEqual(t, first.NumberPrefix, second.NumberPrefix, "the second assignment must avoid the first")

	pools, err := store.LoadPools("kendo-open")
	require.NoError(t, err)
	require.Len(t, pools, 1)
	for i, p := range pools[0].Players {
		assert.Equalf(t, fmt.Sprintf("%s%d", first.NumberPrefix, i+1), p.Number, "pools.csv must be numbered by the migration")
	}

	again, err := eng.MigrateNumberPrefixes()
	require.NoError(t, err)
	assert.Empty(t, again, "a second run finds nothing to migrate")
}

// TestMigrateNumberPrefixes_OneBadCompetitionDoesNotStopTheRest pins the
// availability rule: an unreadable config.md and an unparseable pools.csv are
// each that competition's problem, logged, and the pass carries on. The
// revert this pins: returning the first error, which turned one bad file into
// a refusal to start the whole app.
func TestMigrateNumberPrefixes_OneBadCompetitionDoesNotStopTheRest(t *testing.T) {
	dir := t.TempDir()
	store, err := state.NewStore(dir)
	require.NoError(t, err)
	eng := New(store)

	require.NoError(t, store.SaveCompetition(&state.Competition{ID: "broken-config", Name: "Broken", Format: state.CompFormatMixed, Status: state.CompStatusPools}))
	require.NoError(t, os.WriteFile(filepath.Join(dir, "competitions", "broken-config", "config.md"), []byte("not front matter at all"), 0o600))
	require.NoError(t, store.SaveCompetition(&state.Competition{ID: "broken-pools", Name: "Broken Pools", Format: state.CompFormatMixed, Status: state.CompStatusPools}))
	require.NoError(t, os.WriteFile(filepath.Join(dir, "competitions", "broken-pools", "pools.csv"), []byte("a,b\na,\"bad\nquote"), 0o600))
	require.NoError(t, store.SaveCompetition(&state.Competition{ID: "fine", Name: "Fine", Format: state.CompFormatMixed, Status: state.CompStatusPools}))
	require.NoError(t, store.SavePools("fine", []helper.Pool{{PoolName: "Pool A", Players: []helper.Player{{Name: "A", Dojo: "D"}}}}))

	migrated, err := eng.MigrateNumberPrefixes()
	require.NoError(t, err, "one bad competition must not stop the migration")
	assert.ElementsMatch(t, []string{"broken-pools", "fine"}, migrated, "the readable competitions are still migrated; the unreadable config is skipped")

	fine, err := store.LoadCompetition("fine")
	require.NoError(t, err)
	assert.NotEmpty(t, fine.NumberPrefix)
	pools, err := store.LoadPools("fine")
	require.NoError(t, err)
	assert.Equal(t, fine.NumberPrefix+"1", pools[0].Players[0].Number)

	brokenPools, err := store.LoadCompetition("broken-pools")
	require.NoError(t, err)
	assert.NotEmpty(t, brokenPools.NumberPrefix, "the prefix is saved even though its pools.csv could not be numbered")
}

// TestMigrateNumberPrefixes_ResumesNumberingOnTheNextStart pins resumability:
// a competition that already has a prefix but whose pools.csv is still
// unnumbered (the shape a failed earlier pass leaves behind) is numbered by
// the next run, even though it assigns no prefix. The revert this pins:
// numbering only the competitions assigned in the same pass.
func TestMigrateNumberPrefixes_ResumesNumberingOnTheNextStart(t *testing.T) {
	store, err := state.NewStore(t.TempDir())
	require.NoError(t, err)
	eng := New(store)

	require.NoError(t, store.SaveCompetition(&state.Competition{ID: "half-migrated", Name: "Half", Format: state.CompFormatMixed, Status: state.CompStatusPools, NumberPrefix: "H"}))
	require.NoError(t, store.SavePools("half-migrated", []helper.Pool{{PoolName: "Pool A", Players: []helper.Player{{Name: "A", Dojo: "D"}, {Name: "B", Dojo: "D"}}}}))

	migrated, err := eng.MigrateNumberPrefixes()
	require.NoError(t, err)
	assert.Empty(t, migrated, "no prefix was assigned")
	pools, err := store.LoadPools("half-migrated")
	require.NoError(t, err)
	assert.Equal(t, []string{"H1", "H2"}, []string{pools[0].Players[0].Number, pools[0].Players[1].Number})
}

// TestDefaultNumberPrefixFor_LogsCorrelatedWarningOnSkippedSibling is PR #416
// finding T2: an unreadable sibling (unparseable config.md) is tolerated by
// takenNumberPrefixes (log-and-skip, never refuse), but the resulting
// assignment was logged, if at all, with no link back to WHICH sibling(s)
// were invisible to it -- an operator investigating a possible prefix
// collision after the fact had two independent log lines to manually
// cross-reference instead of one that already named both.
func TestDefaultNumberPrefixFor_LogsCorrelatedWarningOnSkippedSibling(t *testing.T) {
	eng, store, _ := setupTestEngine(t)
	require.NoError(t, store.SaveCompetition(&state.Competition{ID: "readable", Name: "Readable", Format: state.CompFormatMixed, NumberPrefix: "R"}))
	require.NoError(t, store.SaveCompetition(&state.Competition{ID: "broken", Name: "Broken", Format: state.CompFormatMixed, NumberPrefix: "B"}))
	corruptCompetitionConfig(t, store, "broken")

	var logBuf bytes.Buffer
	prevOut := log.Writer()
	prevFlags := log.Flags()
	log.SetOutput(&logBuf)
	log.SetFlags(0)
	t.Cleanup(func() { log.SetOutput(prevOut); log.SetFlags(prevFlags) })

	prefix, err := eng.DefaultNumberPrefixFor("New Competition", "")
	require.NoError(t, err)
	require.NotEmpty(t, prefix)

	logged := logBuf.String()
	assert.Contains(t, logged, "New Competition", "the warning must name the assignment it applies to")
	assert.Contains(t, logged, "broken", "the warning must name the skipped sibling id")
	assert.Contains(t, logged, prefix, "the warning must name the prefix that was assigned")
}

// TestMigrateNumberPrefixes_LogsWhichCompetitionWasRenumbered is PR #416
// finding 9: MigrateNumberPrefixes's final loop discarded RenumberCompetitors'
// bool (whether it actually rewrote pools.csv) entirely, so nothing in the
// log correlated a competitor-numbering rewrite with the competition it
// happened to. A competition with an ALREADY-VALID prefix but a pools.csv
// Number column holding the wrong values (mirrors ...ResumesNumberingOnThe
// NextStart above) is renumbered on this pass, and that must now be logged
// by id and prefix.
func TestMigrateNumberPrefixes_LogsWhichCompetitionWasRenumbered(t *testing.T) {
	store, err := state.NewStore(t.TempDir())
	require.NoError(t, err)
	eng := New(store)

	require.NoError(t, store.SaveCompetition(&state.Competition{
		ID: "half-migrated-2", Name: "Half Two", Format: state.CompFormatMixed, Status: state.CompStatusPools, NumberPrefix: "H",
	}))
	require.NoError(t, store.SavePools("half-migrated-2", []helper.Pool{
		{PoolName: "Pool A", Players: []helper.Player{{Name: "A", Dojo: "D"}, {Name: "B", Dojo: "D"}}},
	}))

	var logBuf bytes.Buffer
	prevOut := log.Writer()
	prevFlags := log.Flags()
	log.SetOutput(&logBuf)
	log.SetFlags(0)
	t.Cleanup(func() { log.SetOutput(prevOut); log.SetFlags(prevFlags) })

	migrated, err := eng.MigrateNumberPrefixes()
	require.NoError(t, err)
	assert.Empty(t, migrated, "no NEW prefix was assigned, only an existing one applied")

	assert.Contains(t, logBuf.String(), "half-migrated-2", "the log must name the competition whose competitor numbers were rewritten")
	assert.Contains(t, logBuf.String(), `"H"`, "the log must name the prefix the rewrite happened under")
}

// TestRenumberCompetitors_InvalidatesStandingsCache pins bc-pnum A2:
// RenumberCompetitors is a pools.csv writer, and CalculatePoolStandings's
// result carries each player's Number (Player.Number rides inside
// state.PlayerStanding.Player), so a cache keyed only on pool-matches.csv and
// overrides.json served the OLD numbers after a prefix change for the rest of
// the process's life once standings had been computed once. Warm the cache
// under the old prefix first -- that is what makes this a real regression
// guard rather than a coincidence of never having read standings before the
// renumber.
func TestRenumberCompetitors_InvalidatesStandingsCache(t *testing.T) {
	eng, store, _ := setupTestEngine(t)
	const compID = "renumber-standings-cache"

	require.NoError(t, store.SaveCompetition(&state.Competition{
		ID: compID, Name: "Renumber Standings Cache", Kind: "individual", Format: "mixed",
		Status: "pools", NumberPrefix: "Z",
	}))
	require.NoError(t, store.SavePools(compID, []helper.Pool{
		{PoolName: "Pool A", Players: []helper.Player{
			{ID: "p1", Name: "Alice", Dojo: "Dojo Alice", PoolPosition: 1, Number: "Z1"},
			{ID: "p2", Name: "Bob", Dojo: "Dojo Bob", PoolPosition: 2, Number: "Z2"},
		}},
	}))

	before, err := eng.CalculatePoolStandings(compID)
	require.NoError(t, err)
	beforeNumbers := map[string]string{}
	for _, ps := range before["Pool A"] {
		beforeNumbers[ps.Player.Name] = ps.Player.Number
	}
	require.Equal(t, "Z1", beforeNumbers["Alice"])
	require.Equal(t, "Z2", beforeNumbers["Bob"])
	versionBeforeRenumber := store.FileVersion(compID, "pools.csv")

	comp, err := store.LoadCompetition(compID)
	require.NoError(t, err)
	comp.NumberPrefix = "Y"
	require.NoError(t, store.SaveCompetition(comp))

	changed, err := eng.RenumberCompetitors(compID)
	require.NoError(t, err)
	require.True(t, changed)
	assert.Greater(t, store.FileVersion(compID, "pools.csv"), versionBeforeRenumber,
		"RenumberCompetitors's SavePools call must bump the pools.csv version")

	after, err := eng.CalculatePoolStandings(compID)
	require.NoError(t, err)
	afterNumbers := map[string]string{}
	for _, ps := range after["Pool A"] {
		afterNumbers[ps.Player.Name] = ps.Player.Number
	}
	assert.Equal(t, "Y1", afterNumbers["Alice"],
		"standings must reflect the renumber, not the cached pre-renumber tokens")
	assert.Equal(t, "Y2", afterNumbers["Bob"])
}

// TestTakenNumberPrefixesAndDefaultFor pins the engine's one derivation: the
// taken set excludes the named competition and the default avoids the rest.
func TestTakenNumberPrefixesAndDefaultFor(t *testing.T) {
	store, err := state.NewStore(t.TempDir())
	require.NoError(t, err)
	eng := New(store)
	require.NoError(t, store.SaveCompetition(&state.Competition{ID: "a", Name: "Kendo", Format: state.CompFormatMixed, NumberPrefix: "K"}))
	require.NoError(t, store.SaveCompetition(&state.Competition{ID: "b", Name: "Kendo Open", Format: state.CompFormatMixed, NumberPrefix: "KO"}))

	taken, skipped, err := eng.takenNumberPrefixes("b")
	require.NoError(t, err)
	assert.Equal(t, []string{"K"}, taken, "excludeID's own prefix is not taken")
	assert.Empty(t, skipped, "no unreadable siblings in this fixture")

	// K and KO are both taken, so every plain digit suffix on "KO" ("KO2"..)
	// is ambiguous with "KO" itself (bc-pnum A1); the derivation falls to the
	// zero-padded escape on the shorter stem, "K02".
	prefix, err := eng.DefaultNumberPrefixFor("Kendo Open", "")
	require.NoError(t, err)
	assert.Equal(t, "K02", prefix, "K and KO are both taken")
	prefix, err = eng.DefaultNumberPrefixFor("Kendo Open", "b")
	require.NoError(t, err)
	assert.Equal(t, "KO", prefix, "re-deriving for b may reuse b's own prefix")
}

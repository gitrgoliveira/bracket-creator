package engine

import (
	"os"
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

	require.NoError(t, eng.RenumberCompetitors(compID))

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
	// the exact same content" (both leave identical bytes), and pools.csv
	// carries no version counter to check either (R5: nothing caches
	// pools.csv keyed by version, only pool-matches.csv bumps one). So this
	// makes any REAL write observably fail instead: atomicWrite creates a
	// sibling temp file before renaming it over pools.csv, which needs write
	// permission on the DIRECTORY, not the file itself, so a read-only
	// competition directory turns "attempted a write" into an error. A
	// genuine no-op (nothing differs) never touches the directory at all, so
	// it still succeeds. Same technique atomic_write_test.go uses for the
	// same reason.
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
	require.NoError(t, eng.RenumberCompetitors(compID), "a no-op renumber must never attempt to write pools.csv")

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

	require.NoError(t, eng.RenumberCompetitors(compID))

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

	require.NoError(t, eng.RenumberCompetitors(compID))

	_, err := os.Stat(dir + "/competitions/" + compID + "/pools.csv")
	assert.True(t, os.IsNotExist(err), "no pools.csv should be created for a competition with none")
}

// TestRenumberCompetitors_NotFound pins the 404 sentinel for an unknown
// competition id, matching every other engine entry point's NotFoundError
// convention.
func TestRenumberCompetitors_NotFound(t *testing.T) {
	eng, _, _ := setupTestEngine(t)

	err := eng.RenumberCompetitors("does-not-exist")
	require.Error(t, err)
	var nfe *NotFoundError
	assert.ErrorAs(t, err, &nfe, "unknown compID must return NotFoundError")
}

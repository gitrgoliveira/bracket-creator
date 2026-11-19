package state

import (
	"testing"

	"github.com/gitrgoliveira/bracket-creator/internal/helper"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// The token a multi-load reader compares moves on every competition write, a
// transaction's and an overrides save's alike, and never on a read.
func TestCompetitionWriteToken_MovesOnWritesNotReads(t *testing.T) {
	store, err := NewStore(t.TempDir())
	require.NoError(t, err)
	compID := "write-token"
	require.NoError(t, store.SaveCompetition(&Competition{
		ID: compID, Format: CompFormatMixed, Status: CompStatusPools,
	}))

	// The first read of a competition runs its one-time legacy upgrade under
	// the write lock, which moves the token; the reads after it do not.
	_, err = store.LoadPoolMatches(compID)
	require.NoError(t, err)

	before := store.CompetitionWriteToken(compID)
	_, err = store.LoadCompetition(compID)
	require.NoError(t, err)
	_, err = store.LoadPoolMatches(compID)
	require.NoError(t, err)
	assert.Equal(t, before, store.CompetitionWriteToken(compID), "a read moves nothing")

	require.NoError(t, store.WithTransaction(compID, func(StoreTx) error { return nil }))
	afterTx := store.CompetitionWriteToken(compID)
	assert.Greater(t, afterTx, before, "a transaction moves the token")

	require.NoError(t, store.SaveRankOverride(compID, "Pool A", helper.NewUUID4(), 1))
	assert.Greater(t, store.CompetitionWriteToken(compID), afterTx, "an overrides save moves the token")
}

// A read that waited behind a writer finds the token already moved once it
// returns: the count moves before the write lock lets readers in.
func TestCompetitionWriteToken_AReadAfterAWriterSeesItMoved(t *testing.T) {
	store, err := NewStore(t.TempDir())
	require.NoError(t, err)
	compID := "write-token-order"
	require.NoError(t, store.SaveCompetition(&Competition{
		ID: compID, Format: CompFormatMixed, Status: CompStatusPools,
	}))

	before := store.CompetitionWriteToken(compID)
	mu, err := store.lockCompWrite(compID)
	require.NoError(t, err)
	read := make(chan uint64)
	go func() {
		_, lerr := store.LoadPoolMatches(compID)
		assert.NoError(t, lerr)
		read <- store.CompetitionWriteToken(compID)
	}()
	mu.Unlock()
	assert.Greater(t, <-read, before)
}

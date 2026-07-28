package engine

import (
	"sync"

	"github.com/gitrgoliveira/bracket-creator/internal/state"
)

// standingsCacheEntry keys cached standings on BOTH the mtime and the store's
// monotonic write version of each input file. mtime alone is unsound: it has
// ~1ms granularity, so two pool-match saves in the same tick leave it unchanged
// and the pre-write standings keep being served (mp-n6ke). That mattered beyond
// a stale read because LoadPoolMatches returns FRESH matches from the store's
// own cache, so InjectTiebreakerMatches would compare fresh matches against
// stale standings, inject a phantom tiebreaker bout into an untied pool, and
// stall bracket advancement mid-tournament.
type standingsCacheEntry struct {
	poolMatchesMtime   int64
	overridesMtime     int64
	poolMatchesVersion uint64
	overridesVersion   uint64
	result             map[string][]state.PlayerStanding
}

type Engine struct {
	store           *state.Store
	standingsCache  sync.Map // map[compID string]*standingsCacheEntry
	standingsFlight sync.Map // map[compID string]*sync.Once, collapses concurrent cold-cache calls
}

func New(store *state.Store) *Engine {
	return &Engine{
		store: store,
	}
}

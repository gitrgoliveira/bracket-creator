package engine

import (
	"sync"

	"github.com/gitrgoliveira/bracket-creator/internal/state"
)

// standingsTokens is the cache-validity key for one competition's standings: the
// (mtime, version) pair of each input file. It keys on BOTH the mtime and the
// store's monotonic write version because mtime alone is unsound: it has ~1ms
// granularity, so two pool-match saves in the same tick leave it unchanged and
// the pre-write standings keep being served (mp-n6ke). That mattered beyond a
// stale read because LoadPoolMatches returns FRESH matches from the store's own
// cache, so InjectTiebreakerMatches would compare fresh matches against stale
// standings, inject a phantom tiebreaker bout into an untied pool, and stall
// bracket advancement mid-tournament.
//
// All fields are comparable, so validity is a single `==` on the whole struct.
// Keep it that way: a hand-written field-by-field check has to be repeated at
// every read path, and the one that gets forgotten is the one that serves stale
// standings.
type standingsTokens struct {
	poolMatchesMtime   int64
	overridesMtime     int64
	poolsMtime         int64
	configMtime        int64
	poolMatchesVersion uint64
	overridesVersion   uint64
	poolsVersion       uint64
	// config.md is an input too: computeStandingsFrom reads the competition
	// record for the scoring mode (Engi, TeamSize) and markTiedStandings for
	// the format, so a settings save that flips one of those must invalidate
	// a live entry just as a pools.csv or pool-matches.csv write does.
	configVersion uint64
}

type standingsCacheEntry struct {
	standingsTokens
	result map[string][]state.PlayerStanding
}

type Engine struct {
	store           *state.Store
	standingsCache  sync.Map // map[compID string]*standingsCacheEntry
	standingsFlight sync.Map // map[compID string]*sync.Once, collapses concurrent cold-cache calls

	// afterMatchRead, when set (tests only), runs at the seam where a
	// read-modify-write of a match file has read what it will write back:
	// the pool-matches injections and the Swiss append (inside their one
	// transaction), and the kachinuki advance (between its read and its
	// locked write). It lets a test land a concurrent write in that window
	// and pin that nothing it wrote is lost (bc-mrgc phase 3). Production
	// never sets it.
	afterMatchRead func(compID string)
}

// noteMatchRead runs the test seam above, when one is set.
func (e *Engine) noteMatchRead(compID string) {
	if e.afterMatchRead != nil {
		e.afterMatchRead(compID)
	}
}

func New(store *state.Store) *Engine {
	return &Engine{
		store: store,
	}
}

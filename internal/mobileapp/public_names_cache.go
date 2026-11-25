package mobileapp

import (
	"fmt"
	"sync"
	"time"

	"github.com/gitrgoliveira/bracket-creator/internal/domain"
	"github.com/gitrgoliveira/bracket-creator/internal/state"
)

// The cache of name masks, keyed by the files a mask is built from (bc-hnam).

// nameMaskFiles are the competition files a mask is built from; a write to
// any of them invalidates it.
var nameMaskFiles = [...]string{"config.md", "participants.csv", "pools.csv", "bracket.json", "team-members.yaml"}

type nameMaskToken [len(nameMaskFiles)]struct {
	mtime   int64
	version uint64
}

// nameMaskEntry is a built mask. A build that failed is kept too (err set,
// mask fail-closed) until retryAt, so an unreadable file is read once per
// nameMaskRetryAfter, not once per public read.
type nameMaskEntry struct {
	token   nameMaskToken
	mask    *nameMask
	err     error
	retryAt time.Time
}

const (
	// nameMaskRetryAfter is how long a failed build is served before the
	// files are read again.
	nameMaskRetryAfter = 2 * time.Second
	// nameMaskEvictEvery is how often a rebuild also drops the entries of
	// competitions that no longer exist.
	nameMaskEvictEvery = time.Minute
)

// maskFlightKey names one build: a competition's files as they were (token).
type maskFlightKey struct {
	compID string
	token  nameMaskToken
}

// maskFlight is a build in progress; its waiters read mask and err once done
// is closed.
type maskFlight struct {
	done chan struct{}
	mask *nameMask
	err  error
}

// nameMaskCache holds one mask per competition, valid while the files it was
// built from are unchanged (Store.FileToken: the in-process write counter and
// the mtime, for an edit made outside the app). Concurrent reads that find
// the same stale token share one build.
type nameMaskCache struct {
	store     *state.Store
	now       func() time.Time
	build     func(compID string) (*nameMask, error)
	mu        sync.Mutex
	entries   map[string]nameMaskEntry
	flights   map[maskFlightKey]*maskFlight
	lastEvict time.Time
}

func newNameMaskCache(store *state.Store) *nameMaskCache {
	c := &nameMaskCache{store: store, now: time.Now, entries: map[string]nameMaskEntry{}, flights: map[maskFlightKey]*maskFlight{}}
	c.build = c.buildMask
	return c
}

func (c *nameMaskCache) token(compID string) nameMaskToken {
	var t nameMaskToken
	for i, f := range nameMaskFiles {
		t[i].mtime, t[i].version = c.store.FileToken(compID, f)
	}
	return t
}

// get is compID's mask, built when the cached one is out of date. The token
// is sampled BEFORE the files are read, so a write landing during the build
// leaves an entry whose token is already stale and is rebuilt next time.
// Readers that find the same stale token share one build. On a read error the
// mask fails closed (failClosedMask, or what could be read) and that answer is
// kept for nameMaskRetryAfter, then the files are read again. A rebuild also
// drops, at most once per nameMaskEvictEvery, the entries of competitions that
// no longer exist (evictGone), so a deleted or replaced competition's names do
// not stay in memory.
func (c *nameMaskCache) get(compID string) (*nameMask, error) {
	tok := c.token(compID)
	key := maskFlightKey{compID, tok}
	c.mu.Lock()
	if e, ok := c.entries[compID]; ok && e.token == tok && (e.err == nil || c.now().Before(e.retryAt)) {
		c.mu.Unlock()
		return e.mask, e.err
	}
	if f, ok := c.flights[key]; ok {
		c.mu.Unlock()
		<-f.done
		return f.mask, f.err
	}
	f := &maskFlight{done: make(chan struct{})}
	c.flights[key] = f
	c.mu.Unlock()

	// A build that panics still releases its waiters, with a mask that hides.
	f.mask, f.err = failClosedMask(&state.Competition{ID: compID}), fmt.Errorf("name mask: build did not finish")
	defer func() {
		c.mu.Lock()
		delete(c.flights, key)
		c.mu.Unlock()
		close(f.done)
	}()
	f.mask, f.err = c.build(compID)

	c.mu.Lock()
	c.entries[compID] = nameMaskEntry{token: tok, mask: f.mask, err: f.err, retryAt: c.now().Add(nameMaskRetryAfter)}
	evict := c.now().Sub(c.lastEvict) >= nameMaskEvictEvery
	if evict {
		c.lastEvict = c.now()
	}
	c.mu.Unlock()
	if evict {
		c.evictGone()
	}
	return f.mask, f.err
}

// evictGone drops the entry of every competition the store no longer lists. A
// listing that fails drops nothing: the entries still match their tokens or are
// rebuilt on their next read.
func (c *nameMaskCache) evictGone() {
	ids, err := c.store.ListCompetitions()
	if err != nil {
		return
	}
	live := make(map[string]bool, len(ids))
	for _, id := range ids {
		live[id] = true
	}
	c.mu.Lock()
	defer c.mu.Unlock()
	for id := range c.entries {
		if !live[id] {
			delete(c.entries, id)
		}
	}
}

func (c *nameMaskCache) buildMask(compID string) (*nameMask, error) {
	comp, err := c.store.LoadCompetition(compID)
	if err != nil || comp == nil {
		if err == nil {
			err = fmt.Errorf("competition %q not found", compID)
		}
		return failClosedMask(&state.Competition{ID: compID}), fmt.Errorf("name mask: %w", err)
	}
	// A mask reads names, dojos, numbers and ids, never a seed, so seeds.csv
	// is not read.
	players, err := c.store.LoadParticipantsOpt(compID, comp.EffectiveWithZekkenName(), state.LoadParticipantsOpts{HasIDs: comp.ParticipantIDsHint()})
	if err != nil {
		return failClosedMask(comp), fmt.Errorf("name mask: participants: %w", err)
	}
	// A hidden draft's numbers are its draw (bc-drpb): its mask holds none, so
	// a hidden name reads "?" as it does before the draw, and the draw files
	// are not read. Judged from the competition alone (DrawHiddenIn with no
	// tournament, which counts as officiated): the mask is consulted only where
	// names are hidden, and a self-run tournament, whose draw is public as soon
	// as it is made, never hides names, so no self-run read reaches it. The
	// cache is keyed on config.md, so a publish or an unpublish rebuilds it.
	if !comp.DrawHiddenIn(nil) {
		var bracket *state.Bracket
		if needsBracket, ok := numberingApplies(comp); ok && needsBracket {
			if bracket, err = c.store.LoadBracket(compID); err != nil {
				return failClosedMask(comp), fmt.Errorf("name mask: bracket: %w", err)
			}
		}
		if err := numbersFromDrawWithBracket(c.store, comp, players, bracket); err != nil {
			return failClosedMask(comp), fmt.Errorf("name mask: numbers: %w", err)
		}
	}
	var squads map[string][]domain.TeamMember
	if comp.IsTeam() {
		if squads, err = c.store.LoadSquads(compID); err != nil {
			// Members unknown: every member name reads "?".
			return buildNameMask(comp, players, nil), fmt.Errorf("name mask: team members: %w", err)
		}
	}
	return buildNameMask(comp, players, squads), nil
}

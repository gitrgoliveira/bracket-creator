package mobileapp

import (
	"fmt"
	"sync"

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

type nameMaskEntry struct {
	token nameMaskToken
	mask  *nameMask
}

// nameMaskCache holds one mask per competition, valid while the files it was
// built from are unchanged (Store.FileToken: the in-process write counter and
// the mtime, for an edit made outside the app).
type nameMaskCache struct {
	store   *state.Store
	mu      sync.Mutex
	entries map[string]nameMaskEntry
}

func newNameMaskCache(store *state.Store) *nameMaskCache {
	return &nameMaskCache{store: store, entries: map[string]nameMaskEntry{}}
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
// leaves an entry whose token is already stale and is rebuilt next time. On a
// read error the mask fails closed (failClosedMask, or what could be read)
// and is not cached, so the next read tries again. Every rebuild also drops
// the entries of competitions that no longer exist (evictGone), so a deleted
// or replaced competition's names do not stay in memory.
func (c *nameMaskCache) get(compID string) (*nameMask, error) {
	tok := c.token(compID)
	c.mu.Lock()
	e, ok := c.entries[compID]
	c.mu.Unlock()
	if ok && e.token == tok {
		return e.mask, nil
	}
	m, err := c.build(compID)
	c.mu.Lock()
	if err != nil {
		delete(c.entries, compID)
	} else {
		c.entries[compID] = nameMaskEntry{token: tok, mask: m}
	}
	c.mu.Unlock()
	c.evictGone()
	return m, err
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

func (c *nameMaskCache) build(compID string) (*nameMask, error) {
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
	var bracket *state.Bracket
	if needsBracket, ok := numberingApplies(comp); ok && needsBracket {
		if bracket, err = c.store.LoadBracket(compID); err != nil {
			return failClosedMask(comp), fmt.Errorf("name mask: bracket: %w", err)
		}
	}
	if err := numbersFromDrawWithBracket(c.store, comp, players, bracket); err != nil {
		return failClosedMask(comp), fmt.Errorf("name mask: numbers: %w", err)
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

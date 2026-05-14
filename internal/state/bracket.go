package state

import (
	"encoding/json"
	"os"
)

func (s *Store) LoadBracket(compID string) (*Bracket, error) {
	if err := ValidateCompetitionID(compID); err != nil {
		return nil, err
	}
	data, err := s.loadCached(compID, "bracket.json", parseBracketFile)
	if err != nil {
		return nil, err
	}
	return s.copyBracket(data.(*Bracket)), nil
}

func parseBracketFile(path string) (any, error) {
	raw, err := os.ReadFile(path) // #nosec G304 — path built by compPath which calls filepath.Clean
	if err != nil {
		if os.IsNotExist(err) {
			return &Bracket{Rounds: [][]BracketMatch{}}, nil
		}
		return nil, err
	}
	var b Bracket
	if err := json.Unmarshal(raw, &b); err != nil {
		return nil, err
	}
	return &b, nil
}

func (s *Store) copyBracket(b *Bracket) *Bracket {
	if b == nil {
		return nil
	}
	res := &Bracket{
		Rounds: make([][]BracketMatch, len(b.Rounds)),
	}
	for i, round := range b.Rounds {
		res.Rounds[i] = make([]BracketMatch, len(round))
		copy(res.Rounds[i], round)
	}
	return res
}

func (s *Store) SaveBracket(compID string, b *Bracket) error {
	mu := s.getCompLock(compID)
	mu.Lock()
	defer mu.Unlock()

	return s.saveBracketLocked(compID, b)
}

// saveBracketLocked persists the bracket to disk and refreshes the
// cache. Caller MUST hold the per-competition lock
// (s.getCompLock(compID)). Used by both SaveBracket (which takes the
// lock) and UpdateBracketMatchByID (which holds the lock across
// load + mutate + save).
func (s *Store) saveBracketLocked(compID string, b *Bracket) error {
	path := s.compPath(compID, "bracket.json")
	data, err := json.MarshalIndent(b, "", "  ")
	if err != nil {
		return err
	}

	if err := os.WriteFile(path, data, 0600); err != nil {
		return err
	}

	cache := s.getFileCache(compID, "bracket.json")
	cache.mu.Lock()
	cache.data = s.copyBracket(b)
	cache.mtime = s.FileMtime(compID, "bracket.json")
	cache.mu.Unlock()

	return nil
}

// UpdateBracket atomically loads the bracket for compID, calls mutate
// with the loaded bracket (which may be nil if no bracket file exists
// yet), and — if mutate returns nil — persists the bracket. The entire
// load + mutate + save sequence runs under the per-competition lock so
// concurrent calls serialize correctly.
//
// mutate may modify the bracket arbitrarily (e.g. update one match AND
// propagate the winner to the next round) — this is the more general
// primitive that supports recordBracketMatchResult's
// propagateBracketWinner behavior. For single-match mutations, see
// also engine.withBracketMatch which delegates to this.
//
// If mutate returns a non-nil error, no write happens and the error
// is returned unchanged (callers can use errors.Is to discriminate
// not-found vs validation vs I/O). Importantly, returning errors from
// mutate is how callers signal "match not found, don't save the
// unchanged bracket back" — the alternative ("found" bool) would
// either save unnecessarily or duplicate the not-found error path
// at every caller.
//
// IMPORTANT: mutate runs while this method holds the per-competition
// lock. It MUST NOT call any other Store method that acquires the
// same lock (SavePoolMatches, SaveBracket, SaveCompetitionChanged,
// recursive UpdateBracket / UpdatePoolMatchByID / UpdateBracket calls,
// etc.) — `sync.Mutex` is non-recursive and would deadlock.
func (s *Store) UpdateBracket(compID string, mutate func(*Bracket) error) error {
	if err := ValidateCompetitionID(compID); err != nil {
		return err
	}

	mu := s.getCompLock(compID)
	mu.Lock()
	defer mu.Unlock()

	// Load directly under the lock (see UpdatePoolMatchByID for why
	// we bypass the cached path here).
	path := s.compPath(compID, "bracket.json")
	parsed, err := parseBracketFile(path)
	if err != nil {
		return err
	}
	bracket, _ := parsed.(*Bracket)

	if err := mutate(bracket); err != nil {
		return err
	}

	if bracket == nil {
		// mutate didn't error but there's no bracket to save (file
		// didn't exist and mutate didn't create one). Nothing to do.
		return nil
	}
	return s.saveBracketLocked(compID, bracket)
}

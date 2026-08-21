package state

import (
	"encoding/json"
	"os"
)

func (s *Store) LoadBracket(compID string) (*Bracket, error) {
	if err := ValidateCompetitionID(compID); err != nil {
		return nil, err
	}
	bracket, err := s.cachedBracket(compID)
	if err != nil {
		return nil, err
	}
	return s.copyBracket(bracket), nil
}

func parseBracketFile(path string) (any, error) {
	raw, err := os.ReadFile(path) // #nosec G304, path built by compPath which calls filepath.Clean
	if err != nil {
		if os.IsNotExist(err) {
			return &Bracket{Rounds: [][]BracketMatch{}}, nil
		}
		return nil, err
	}
	b, err := parseBracketBytes(raw)
	if err != nil {
		return nil, err
	}
	return b, nil
}

// parseBracketBytes parses a bracket.json blob from in-memory bytes.
// Used by tx-internal read-your-own-writes: the storeTx loader peeks
// at WAL-staged bytes (via wal.PendingBytes) and falls through to
// this parser. Same never-nil contract as parseBracketFile: an empty
// or absent slice deserializes to `&Bracket{Rounds: [][]BracketMatch{}}`.
func parseBracketBytes(raw []byte) (*Bracket, error) {
	if len(raw) == 0 {
		return &Bracket{Rounds: [][]BracketMatch{}}, nil
	}
	var b Bracket
	if err := json.Unmarshal(raw, &b); err != nil {
		return nil, err
	}
	// Clamp negative engi flag counts to 0 on load, symmetric with the pool CSV
	// parser (pools.go parsePoolMatchesRecords): flags are validated
	// non-negative at the HTTP boundary, so a corrupted / hand-edited
	// bracket.json must not load negative counts that would break engi
	// standings / score rendering.
	for i := range b.Rounds {
		for j := range b.Rounds[i] {
			clampBracketMatchFlags(&b.Rounds[i][j])
			// Legacy score strings fold into ippon arrays, and legacy
			// decidedByHantei flags fold into the mark inside them, on load
			// (legacy_hantei.go).
			b.Rounds[i][j].NormalizeLegacy()
		}
	}
	if b.ThirdPlaceMatch != nil {
		clampBracketMatchFlags(b.ThirdPlaceMatch)
		b.ThirdPlaceMatch.NormalizeLegacy()
	}
	return &b, nil
}

// clampBracketMatchFlags forces negative engi flag counts to 0 (see
// parseBracketBytes). Non-negative values pass through unchanged.
func clampBracketMatchFlags(m *BracketMatch) {
	if m.FlagsA < 0 {
		m.FlagsA = 0
	}
	if m.FlagsB < 0 {
		m.FlagsB = 0
	}
}

func (s *Store) copyBracket(b *Bracket) *Bracket {
	if b == nil {
		return nil
	}
	res := &Bracket{
		Rounds:  make([][]BracketMatch, len(b.Rounds)),
		Preview: b.Preview,
	}
	for i, round := range b.Rounds {
		res.Rounds[i] = make([]BracketMatch, len(round))
		copy(res.Rounds[i], round)
		// The shallow copy above aliases the Encho pointer, SubResults slice
		// (and its nested IpponsA/B/Encho), the Feeders slice, and the match's
		// own IpponsA/IpponsB slices with the cached bracket; so a caller
		// mutating a returned match could corrupt cached state without going
		// through SaveBracket/UpdateBracket. Deep-copy them to match the pool
		// match copy path (copyMatchResults).
		for j := range res.Rounds[i] {
			res.Rounds[i][j].Encho = round[j].Encho.Clone()
			res.Rounds[i][j].SubResults = cloneSubResults(round[j].SubResults)
			if round[j].Feeders != nil {
				res.Rounds[i][j].Feeders = append([]string(nil), round[j].Feeders...)
			}
			if round[j].IpponsA != nil {
				res.Rounds[i][j].IpponsA = append([]string(nil), round[j].IpponsA...)
			}
			if round[j].IpponsB != nil {
				res.Rounds[i][j].IpponsB = append([]string(nil), round[j].IpponsB...)
			}
		}
	}
	// Deep-copy the optional bronze match so a returned bracket never aliases the
	// cached ThirdPlaceMatch pointer (or its nested Encho/SubResults/Feeders/
	// IpponsA/B).
	if b.ThirdPlaceMatch != nil {
		tpm := *b.ThirdPlaceMatch
		tpm.Encho = b.ThirdPlaceMatch.Encho.Clone()
		tpm.SubResults = cloneSubResults(b.ThirdPlaceMatch.SubResults)
		if b.ThirdPlaceMatch.Feeders != nil {
			tpm.Feeders = append([]string(nil), b.ThirdPlaceMatch.Feeders...)
		}
		if b.ThirdPlaceMatch.IpponsA != nil {
			tpm.IpponsA = append([]string(nil), b.ThirdPlaceMatch.IpponsA...)
		}
		if b.ThirdPlaceMatch.IpponsB != nil {
			tpm.IpponsB = append([]string(nil), b.ThirdPlaceMatch.IpponsB...)
		}
		res.ThirdPlaceMatch = &tpm
	}
	return res
}

func (s *Store) SaveBracket(compID string, b *Bracket) error {
	// Defense-in-depth: validate compID before acquiring the lock and
	// writing via compPath. StartCompetition can reach this path via
	// generatePlayoffs(comp.ID, ...), a corrupted or out-of-band edit
	// to config.md with a traversal-shaped ID could otherwise make
	// bracket.json land outside the competition directory. Sibling
	// LoadBracket and UpdateBracket already validate; align with them.
	if err := ValidateCompetitionID(compID); err != nil {
		return err
	}
	mu := s.getCompLock(compID)
	mu.Lock()
	defer mu.Unlock()

	return s.saveBracketLocked(compID, b, s.directWrite)
}

// loadBracketLocked reads the bracket directly from disk WITHOUT
// acquiring the per-competition lock. Caller MUST already hold the
// per-comp lock (typically via WithTransaction). Bypasses the cache for
// the same reason UpdateBracket does; the caller's lock is what
// coordinates with concurrent writers.
//
// Returns an empty `&Bracket{Rounds: [][]BracketMatch{}}` when no file
// exists, matching LoadBracket's never-nil contract.
func (s *Store) loadBracketLocked(compID string) (*Bracket, error) {
	if err := ValidateCompetitionID(compID); err != nil {
		return nil, err
	}
	path := s.compPath(compID, "bracket.json")
	parsed, err := parseBracketFile(path)
	if err != nil {
		return nil, err
	}
	bracket, _ := parsed.(*Bracket)
	return s.copyBracket(bracket), nil
}

// saveBracketLocked persists the bracket to disk and refreshes the
// cache. Caller MUST hold the per-competition lock
// (s.getCompLock(compID)). Used by both SaveBracket (which takes the
// lock) and UpdateBracket (which holds the lock across
// load + mutate + save).
//
// The write parameter routes the actual file write; directWrite
// (default) goes straight to atomicWriteFile, while a WAL-capturing
// writer (from storeTx) stages the bytes in the transaction's
// intent log for deferred commit. The cache refresh runs in BOTH
// modes, readers within the same tx body need to see the staged
// bytes via the cache because the on-disk file hasn't moved yet;
// and the cache mtime is updated using the LOCAL file's mtime which
// is unchanged in WAL mode (so a follow-up cache-aware Load will
// re-parse from the cached copy without going to disk). T211/T212.
func (s *Store) saveBracketLocked(compID string, b *Bracket, write writeFn) error {
	path := s.compPath(compID, "bracket.json")
	data, err := json.MarshalIndent(b, "", "  ")
	if err != nil {
		return err
	}

	if err := write(path, data, 0600); err != nil {
		return err
	}

	cache := s.getFileCache(compID, "bracket.json")
	cache.mu.Lock()
	cache.data = s.copyBracket(b)
	cache.mtime = s.FileMtime(compID, "bracket.json")
	cache.mu.Unlock()

	// Bump last, mirroring savePoolMatchesLocked: this is the single chokepoint
	// every bracket writer funnels through (SaveBracket, UpdateBracket,
	// UpdateBracketMatchByID, and the storeTx variants), so any future cache
	// keyed on FileVersion("bracket.json") invalidates by construction rather
	// than depending on a new writer remembering to bump (mp-gmcg review R4; per
	// CLAUDE.md an extra bump only costs a recompute, a missed one serves stale
	// data). No version-keyed consumer reads the bracket token today, so this is
	// hardening against the asymmetry, not a live-bug fix.
	s.bumpFileVersion(compID, "bracket.json")

	return nil
}

// UpdateBracket atomically loads the bracket for compID, calls mutate
// with the loaded bracket (always non-nil, parseBracketFile returns an
// empty `&Bracket{Rounds: [][]BracketMatch{}}` when no file exists yet;
// so callers can rely on a non-nil receiver and an empty Rounds slice
// as the "no bracket yet" sentinel), and, if mutate returns nil,
// persists the bracket. The entire load + mutate + save sequence runs
// under the per-competition lock so concurrent calls serialize
// correctly.
//
// mutate may modify the bracket arbitrarily (e.g. update one match AND
// propagate the winner to the next round), this is the more general
// primitive that supports recordBracketMatchResult's
// propagateBracketWinner behavior. For single-match mutations, see
// also engine.withBracketMatch which delegates to this.
//
// If mutate returns a non-nil error, no write happens and the error
// is returned unchanged (callers can use errors.Is to discriminate
// not-found vs validation vs I/O). Importantly, returning errors from
// mutate is how callers signal "match not found, don't save the
// unchanged bracket back", the alternative ("found" bool) would
// either save unnecessarily or duplicate the not-found error path
// at every caller.
//
// IMPORTANT: mutate runs while this method holds the per-competition
// lock. It MUST NOT call any other Store method that acquires the
// same lock (SavePoolMatches, SaveBracket, SaveCompetitionChanged,
// recursive UpdateBracket / UpdatePoolMatchByID / UpdateBracket calls,
// etc.), `sync.Mutex` is non-recursive and would deadlock.
func (s *Store) UpdateBracket(compID string, mutate func(*Bracket) error) error {
	if err := ValidateCompetitionID(compID); err != nil {
		return err
	}

	mu := s.getCompLock(compID)
	mu.Lock()
	defer mu.Unlock()

	return s.updateBracketLocked(compID, mutate, s.directWrite)
}

// updateBracketLocked is the lock-free body of UpdateBracket. Caller
// MUST already hold the per-comp write lock. Used by the tx-aware
// path so the same load + mutate + save sequence runs without
// re-acquiring the lock from inside a WithTransaction closure
// (T156, NFR-010). The write parameter selects direct-to-disk vs
// WAL-capturing semantics (see saveBracketLocked).
func (s *Store) updateBracketLocked(compID string, mutate func(*Bracket) error, write writeFn) error {
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

	// bracket is always non-nil here, parseBracketFile returns an empty
	// `&Bracket{...}` on missing file (never nil). The nil-check would be
	// dead code; trust the contract from parseBracketFile.
	return s.saveBracketLocked(compID, bracket, write)
}

// findBracketMatchByID returns a pointer to the bracket match with the given
// ID — searching the rounds FIRST, then the ThirdPlaceMatch sibling — or nil.
// The bronze (3rd-place) match is a SIBLING of Rounds, not an element, so a
// rounds-only loop never reaches it: forgetting that branch is the recurring
// bug this shared walk exists to prevent (mp-gmcg). It walks a bracket only;
// callers that also need pool matches wrap it (e.g. MatchStatusByID below).
func findBracketMatchByID(b *Bracket, matchID string) *BracketMatch {
	if b == nil {
		return nil
	}
	for rIdx := range b.Rounds {
		for mIdx := range b.Rounds[rIdx] {
			if b.Rounds[rIdx][mIdx].ID == matchID {
				return &b.Rounds[rIdx][mIdx]
			}
		}
	}
	if b.ThirdPlaceMatch != nil && b.ThirdPlaceMatch.ID == matchID {
		return b.ThirdPlaceMatch
	}
	return nil
}

// MatchStatusByID returns the status of the match with the given ID, searching
// pool matches FIRST, then the bracket (rounds, then the bronze sibling), or
// found=false. It reads the CACHED parse directly and copies NOTHING: the
// caller wants only the status enum, so this avoids the deep SubResults/bracket
// clone that LoadPoolMatches / LoadBracket make on every call (mp-gmcg review
// E5). Only VALUES are returned — the cached slices are never exposed or
// mutated, so reading them without a defensive copy is safe (writers replace
// the cached parse, never mutate it in place). The no-copy read is the whole
// reason this exists as a separate traversal rather than a status projection
// over a fuller snapshot reader.
func (s *Store) MatchStatusByID(compID, matchID string) (MatchStatus, bool, error) {
	if err := ValidateCompetitionID(compID); err != nil {
		return "", false, err
	}
	results, err := s.cachedPoolMatches(compID)
	if err != nil {
		return "", false, err
	}
	for i := range results {
		if results[i].ID == matchID {
			return results[i].Status, true, nil
		}
	}
	b, err := s.cachedBracket(compID)
	if err != nil {
		return "", false, err
	}
	if b != nil {
		if bm := findBracketMatchByID(b, matchID); bm != nil {
			return bm.Status, true, nil
		}
	}
	return "", false, nil
}

// MatchSidesByID returns the STORED pairing (sideA, sideB, sideAID, sideBID)
// for the match with the given ID, searching pool matches first then the
// bracket (rounds, then the bronze sibling), or found=false. Same no-copy
// cached-read shape as MatchStatusByID and for the same reason: a caller
// that only wants the side identity should not pay for a deep
// SubResults/bracket clone.
//
// Purpose: a score payload is allowed to omit sideA/sideB (the engine's
// reconcileSides backfills them from the stored pairing before any write
// lands), but the request-boundary legacy-hantei fold
// (state.MatchResult.NormalizeLegacyHantei) runs BEFORE that backfill and
// needs to know the real sides to attribute a flagged verdict to a
// competitor. Handlers call this to backfill the payload's sides for
// attribution purposes only, ahead of validation; the engine's own
// reconcileSides still runs its normal (redundant, idempotent) backfill
// and mismatch check afterwards, so this read cannot weaken that guard.
//
// sideAID/sideBID are the bc-dmsr follow-up (mobileapp review): the SPA
// sends winnerId but never sideAId/sideBId (it computes them locally for its
// own mark placement and discards them - api_serializers.jsx), so
// validateHanteiMarkPlacement never saw all three ids and fell back to a
// name comparison the engine's later id-aware stripInvalidHantei could
// disagree with. Handlers backfill the omitted ids from here the same way
// they already backfill the omitted names, so the validator and the engine
// attribute by the SAME triple. Pool matches carry ids (SideAID/SideBID);
// BracketMatch persists no ids at all, so sideAID/sideBID are always "" for
// a bracket result and that path stays on the name fallback unchanged - this
// widened return does not alter the existing sideA/sideB behaviour for the
// legacy-hantei name backfill caller, which keeps ignoring the two new
// values.
func (s *Store) MatchSidesByID(compID, matchID string) (sideA, sideB, sideAID, sideBID string, found bool, err error) {
	if err := ValidateCompetitionID(compID); err != nil {
		return "", "", "", "", false, err
	}
	results, err := s.cachedPoolMatches(compID)
	if err != nil {
		return "", "", "", "", false, err
	}
	for i := range results {
		if results[i].ID == matchID {
			return results[i].SideA, results[i].SideB, results[i].SideAID, results[i].SideBID, true, nil
		}
	}
	b, err := s.cachedBracket(compID)
	if err != nil {
		return "", "", "", "", false, err
	}
	if b != nil {
		if bm := findBracketMatchByID(b, matchID); bm != nil {
			// BracketMatch has no SideAID/SideBID fields: a bracket result
			// always returns empty ids here, matching AttributeWinnerSide's
			// existing name-only fallback for bracket matches.
			return bm.SideA, bm.SideB, "", "", true, nil
		}
	}
	return "", "", "", "", false, nil
}

// UpdateBracketMatchByID finds the bracket match with the given ID (via
// findBracketMatchByID, so rounds AND the bronze sibling), applies mutate, and
// saves. Returns found=false with NO write when no match has that ID. This is
// the bracket-match analogue of UpdatePoolMatchByID (mp-gmcg review): a
// consumer mutating one bracket match by id no longer hand-rolls the
// load → walk-rounds → bronze-sibling → save sequence, so the bronze branch
// can't be forgotten in a copy of it.
//
// IMPORTANT: mutate runs under the per-competition lock; it MUST NOT call any
// other Store method that acquires the same lock (the non-recursive mutex
// deadlocks). Same contract as UpdateBracket.
func (s *Store) UpdateBracketMatchByID(compID, matchID string, mutate func(*BracketMatch)) (bool, error) {
	if err := ValidateCompetitionID(compID); err != nil {
		return false, err
	}
	mu := s.getCompLock(compID)
	mu.Lock()
	defer mu.Unlock()
	return s.updateBracketMatchByIDLocked(compID, matchID, mutate, s.directWrite)
}

// updateBracketMatchByIDLocked is the lock-free body of UpdateBracketMatchByID
// (caller holds the per-comp write lock), mirroring updatePoolMatchByIDLocked.
// It saves ONLY when the match is found, so a miss costs a parse but no write.
func (s *Store) updateBracketMatchByIDLocked(compID, matchID string, mutate func(*BracketMatch), write writeFn) (bool, error) {
	// Load directly under the lock (see UpdatePoolMatchByID for why we bypass
	// the cached path here).
	path := s.compPath(compID, "bracket.json")
	parsed, err := parseBracketFile(path)
	if err != nil {
		return false, err
	}
	bracket, _ := parsed.(*Bracket)
	if bm := findBracketMatchByID(bracket, matchID); bm != nil {
		mutate(bm)
		return true, s.saveBracketLocked(compID, bracket, write)
	}
	return false, nil
}

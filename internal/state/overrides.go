package state

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"os"

	"github.com/gitrgoliveira/bracket-creator/internal/helper"
)

// ErrCorruptOverrides is the sentinel wrapped (via %w) around a JSON parse
// failure reading overrides.json. Standings computation deliberately fails
// closed on a corrupt overrides file (a silently-dropped override could
// resurrect a broken chusen or a stale manual rank), but every override
// writer -- SaveRankOverrideChanged, SaveWinnerOverride, ResetOverrides --
// goes through modifyOverridesChanged, which LOADS the file first, so a
// corrupt file has no writer that can repair it: even "reset" fails
// identically. errors.Is(err, ErrCorruptOverrides) lets a caller (the HTTP
// layer) recognise this specific, operator-recoverable failure and map it to
// a terminal 4xx naming overrides.json, and route the operator to
// ResetOverridesForceChanged, the one write that does NOT parse first.
var ErrCorruptOverrides = errors.New("overrides.json is corrupt")

// Overrides.PoolRanks is keyed PoolID -> overrideKey -> Rank. overrideKey is
// helper.CompetitorKey(id, name, dojo) for every override written since
// bc-cse (id-preferred, normalized name+dojo composite fallback -- see that
// function's doc comment for the operator identity rule: (name, dojo), not
// name, so two same-name competitors from different dojos never share one
// override entry). A file written BEFORE bc-cse instead holds bare player
// names as keys: those legacy entries are never rewritten (read-only
// compatibility, see lookupPoolRankOverride in internal/engine for the
// read-side fallback and the tradeoff it documents) -- SaveRankOverride*
// below always writes the new identity-keyed form.
type Overrides struct {
	PoolRanks map[string]map[string]int `json:"poolRanks"`
	Winners   map[string]string         `json:"winners"` // MatchID -> WinnerName
}

func (s *Store) LoadOverrides(compID string) (*Overrides, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return s.loadOverridesLocked(compID)
}

func (s *Store) SaveOverrides(compID string, o *Overrides) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.saveOverridesLocked(compID, o)
}

// loadOverridesLocked reads overrides without acquiring the mutex.
// Caller must hold at least s.mu.RLock.
func (s *Store) loadOverridesLocked(compID string) (*Overrides, error) {
	data, err := os.ReadFile(s.compPath(compID, "overrides.json"))
	if err != nil {
		if os.IsNotExist(err) {
			return &Overrides{
				PoolRanks: make(map[string]map[string]int),
				Winners:   make(map[string]string),
			}, nil
		}
		return nil, err
	}
	var o Overrides
	if err := json.Unmarshal(data, &o); err != nil {
		return nil, fmt.Errorf("%w: overrides.json: %v", ErrCorruptOverrides, err)
	}
	if o.PoolRanks == nil {
		o.PoolRanks = make(map[string]map[string]int)
	}
	if o.Winners == nil {
		o.Winners = make(map[string]string)
	}
	return &o, nil
}

// saveOverridesLocked writes overrides without acquiring the mutex.
// Caller must hold s.mu.Lock.
// Deliberately does NOT create the competition directory. It used to
// os.MkdirAll it before writing, which meant an override save landing after
// DeleteCompetition rebuilt competitions/<id>/ around a lone overrides.json.
// That orphan outlives the delete: ListCompetitions returns every directory
// under competitions/, and because IDs are deterministic name slugs, a
// competition recreated under the same name adopts the dead one's rank and
// winner overrides.
//
// Locking cannot fix this, which is why the fix is here instead. The save does
// not have to interleave with the delete to resurrect the directory, it only
// has to run after it, so serialising the two would still leave the orphan.
//
// atomicWriteFile opens its temp file with O_CREATE but never creates the
// parent, so dropping the MkdirAll makes a write to a deleted competition fail
// with ENOENT, which is the correct outcome.
//
// saveCompetitionChangedLocked is the ONE writer that legitimately creates the
// directory, because creating the competition is its job. Every other saver
// must rely on it already existing. saveCompetitorStatusLocked and
// saveTeamLineupsLocked had the same MkdirAll and were fixed with this one; if
// you add a per-competition writer, do not reintroduce it.
func (s *Store) saveOverridesLocked(compID string, o *Overrides) error {
	data, err := json.MarshalIndent(o, "", "  ")
	if err != nil {
		return err
	}
	if err := s.atomicWrite(s.compPath(compID, "overrides.json"), data, 0600); err != nil {
		return err
	}
	// overrides.json has no fileCache entry of its own (loadOverridesLocked
	// reads it raw), but standings depend on it, so the version counter still
	// has to move for caches keyed on it. This is the only overrides writer.
	s.bumpFileVersion(compID, "overrides.json")
	return nil
}

// modifyOverridesChanged loads, mutates, and saves overrides under a single
// write lock, reporting whether the marshalled content changed.
func (s *Store) modifyOverridesChanged(compID string, fn func(*Overrides)) (bool, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	o, err := s.loadOverridesLocked(compID)
	if err != nil {
		return false, err
	}
	// Snapshot before mutation for comparison (compact marshal; indent is cosmetic).
	before, err := json.Marshal(o)
	if err != nil {
		return false, err
	}
	fn(o)
	after, err := json.Marshal(o)
	if err != nil {
		return false, err
	}
	if bytes.Equal(before, after) {
		return false, nil
	}
	return true, s.saveOverridesLocked(compID, o)
}

// modifyOverrides loads, mutates, and saves overrides under a single write lock,
// eliminating the Load(RLock) → mutate → Save(Lock) lost-update window.
func (s *Store) modifyOverrides(compID string, fn func(*Overrides)) error {
	_, err := s.modifyOverridesChanged(compID, fn)
	return err
}

// SaveRankOverrideChanged saves a manual pool-rank override for one
// competitor and reports whether the overrides file actually changed. Use
// this to gate broadcasts.
//
// The override is keyed by helper.CompetitorKey(playerID, playerName,
// playerDojo) (bc-cse), never by bare playerName: two competitors sharing a
// display name from different dojos are legal (operator identity rule,
// CLAUDE.md) and must not collide on one override entry. playerID and
// playerDojo may be empty (an older API client sending only playerName), in
// which case CompetitorKey degrades to its normalized-name(+empty dojo)
// composite -- callers that can resolve the competitor's real id/dojo from
// the roster before calling this (as the mobileapp handler does) should
// always do so, since that is what actually disambiguates a same-name pair.
// This function never touches a pre-existing legacy bare-name key; see
// Overrides.PoolRanks' doc comment for the read-side compatibility story.
func (s *Store) SaveRankOverrideChanged(compID, poolID, playerID, playerName, playerDojo string, rank int) (bool, error) {
	key := helper.CompetitorKey(playerID, playerName, playerDojo)
	return s.modifyOverridesChanged(compID, func(o *Overrides) {
		if o.PoolRanks[poolID] == nil {
			o.PoolRanks[poolID] = make(map[string]int)
		}
		o.PoolRanks[poolID][key] = rank
	})
}

func (s *Store) SaveRankOverride(compID, poolID, playerID, playerName, playerDojo string, rank int) error {
	_, err := s.SaveRankOverrideChanged(compID, poolID, playerID, playerName, playerDojo, rank)
	return err
}

func (s *Store) SaveWinnerOverride(compID, matchID, winnerName string) error {
	return s.modifyOverrides(compID, func(o *Overrides) {
		o.Winners[matchID] = winnerName
	})
}

// ResetOverridesChanged clears all overrides and reports whether the file changed
// (false when overrides were already empty).
func (s *Store) ResetOverridesChanged(compID string) (bool, error) {
	return s.modifyOverridesChanged(compID, func(o *Overrides) {
		o.PoolRanks = make(map[string]map[string]int)
		o.Winners = make(map[string]string)
	})
}

func (s *Store) ResetOverrides(compID string) error {
	_, err := s.ResetOverridesChanged(compID)
	return err
}

// ResetOverridesForce clears all overrides WITHOUT first loading/parsing the
// existing file. This is the repair door for a corrupt overrides.json
// (ErrCorruptOverrides): every OTHER override writer, ResetOverridesChanged
// included, goes through modifyOverridesChanged, which loads first and so
// fails identically against the very file it would need to repair -- there
// is otherwise no way to clear a corrupt overrides.json short of an operator
// deleting the file by hand.
//
// Calls SaveOverrides directly with a fresh empty value -- the same
// save-side primitive every other writer already uses -- so the file
// version still bumps (saveOverridesLocked's bumpFileVersion) and the
// standings cache correctly invalidates, exactly as any other overrides
// write does.
func (s *Store) ResetOverridesForce(compID string) error {
	return s.SaveOverrides(compID, &Overrides{
		PoolRanks: make(map[string]map[string]int),
		Winners:   make(map[string]string),
	})
}

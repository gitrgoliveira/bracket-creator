// Package state, team_lineup.go owns the on-disk persistence for
// domain.TeamLineup (FR-040, Slice 7.B / T126).
//
// One file per competition lives at
// tournament-data/competitions/<id>/lineups.yaml and is keyed by
// "<teamId>-<round>". A missing file is treated as "no lineups
// submitted yet". All load/mutate/save sequences run under the
// per-competition write lock (s.getCompLock) so concurrent PUTs to
// different teams in the same competition serialize correctly without
// clobbering each other's work, same pattern competitor_status.go
// uses.
//
// Lineups are always editable, including while a match is running or
// completed. Scored bouts freeze fighter names in SubMatchResult.SideA/SideB
// at score-time; the lineup is only read to populate the next unscored bout.
package state

import (
	"fmt"
	"maps"
	"os"
	"slices"
	"sort"

	"github.com/gitrgoliveira/bracket-creator/internal/domain"
	"gopkg.in/yaml.v3"
)

// teamLineupFile is the on-disk YAML shape. Wire-stable: persisted as a
// sorted slice so file contents are deterministic.
type teamLineupFile struct {
	Lineups []domain.TeamLineup `yaml:"lineups"`
}

const teamLineupFilename = "lineups.yaml"

// teamLineupKey is the in-memory map key for a ROUND-scoped lineup. Two
// lineups for the same team in different rounds coexist (a 5-person team
// might rotate a kiken'd jiho between round 1 and round 2), so the round
// is part of the key, not just teamID.
func teamLineupKey(teamID string, round int) string {
	return fmt.Sprintf("%s-%d", teamID, round)
}

// teamLineupMatchKey is the in-memory map key for a MATCH-scoped lineup
// (mp-825). Prefixed with "m:" so it can never collide with a
// round-scoped key (which is "<teamID>-<int>"); both scopes share one
// persisted map and the prefix keeps them disjoint.
//
// The two components are joined with a NUL byte rather than "-" because
// both teamID and matchID are opaque and routinely contain hyphens (pool
// match IDs are "PoolA-0"). A "-" delimiter would be ambiguous,
// ("a-b","c") and ("a","b-c") would collide, so NUL, which cannot
// appear in either, is used (same technique as lineupKey in
// kachinuki_export.go). This key is only ever a map key; it is never
// persisted or parsed back, so the delimiter choice is free.
func teamLineupMatchKey(teamID, matchID string) string {
	return "m:" + teamID + "\x00" + matchID
}

// lineupStorageKey returns the map key a lineup is stored under:
// match-scoped when MatchID is set, else round-scoped. This is the one
// place the scope decision lives, every load/set/delete routes through
// it so the two namespaces stay consistent.
func lineupStorageKey(l domain.TeamLineup) string {
	if l.MatchID != "" {
		return teamLineupMatchKey(l.TeamID, l.MatchID)
	}
	return teamLineupKey(l.TeamID, l.Round)
}

// parseTeamLineupsFile reads and parses lineups.yaml at path. A missing
// file is "no lineups yet" and returns an empty map.
func parseTeamLineupsFile(path string) (map[string]domain.TeamLineup, error) {
	data, err := os.ReadFile(path) // #nosec G304, compPath cleans the path.
	if err != nil {
		if os.IsNotExist(err) {
			return map[string]domain.TeamLineup{}, nil
		}
		return nil, err
	}
	return parseTeamLineupsBytes(data)
}

// LoadTeamLineups returns every lineup persisted for compID, keyed by
// "<teamID>-<round>". A missing file is treated as "no lineups yet"
// and returns an empty map (consistent with LoadCompetitorStatus).
//
// Cache-aware (mtime-keyed via loadCached, same as LoadBracket): repeated
// reads within the same mtime are served from memory. loadCached itself
// takes the per-competition read lock, so the explicit RLock in the old body
// is not lost, just moved. loadCached also validates compID, so no separate
// ValidateCompetitionID call is needed here (mirrors LoadBracket).
// Returns a deep copy so callers can mutate the map freely.
func (s *Store) LoadTeamLineups(compID string) (map[string]domain.TeamLineup, error) {
	data, err := s.loadCached(compID, teamLineupFilename, func(path string) (any, error) {
		return parseTeamLineupsFile(path)
	})
	if err != nil {
		return nil, err
	}
	return copyTeamLineups(data.(map[string]domain.TeamLineup)), nil
}

// loadTeamLineupsLocked reads the lineup file directly from disk WITHOUT
// acquiring the per-competition lock. Caller MUST already hold the lock
// (typically via WithTransaction or setTeamLineupLocked). Bypasses the cache:
// locked callers are usually about to mutate and need a fresh private map,
// mirroring the same pattern in loadBracketLocked.
func (s *Store) loadTeamLineupsLocked(compID string) (map[string]domain.TeamLineup, error) {
	return parseTeamLineupsFile(s.compPath(compID, teamLineupFilename))
}

// copyTeamLineups deep-copies a lineups map so cached data is never
// aliased to a caller (callers mutate the returned map in
// load-mutate-save flows, see setTeamLineupLocked).
func copyTeamLineups(in map[string]domain.TeamLineup) map[string]domain.TeamLineup {
	out := make(map[string]domain.TeamLineup, len(in))
	for k, l := range in {
		l.Positions = maps.Clone(l.Positions)
		out[k] = l
	}
	return out
}

// parseTeamLineupsBytes parses lineups.yaml from in-memory bytes.
// Used by tx-internal read-your-own-writes (storeTx LoadTeamLineups).
// Empty input → empty map, matching the "file does not exist" contract.
func parseTeamLineupsBytes(data []byte) (map[string]domain.TeamLineup, error) {
	if len(data) == 0 {
		return map[string]domain.TeamLineup{}, nil
	}
	var file teamLineupFile
	if err := yaml.Unmarshal(data, &file); err != nil {
		return nil, err
	}
	out := make(map[string]domain.TeamLineup, len(file.Lineups))
	for _, l := range file.Lineups {
		out[lineupStorageKey(l)] = l
	}
	return out, nil
}

// saveTeamLineupsLocked persists the lineups map. Caller MUST hold
// the per-comp write lock. The write parameter routes the actual
// file write, directWrite for non-tx callers, WAL-capturing writer
// for tx callers. See saveBracketLocked (T211/T212).
func (s *Store) saveTeamLineupsLocked(compID string, lineups map[string]domain.TeamLineup, write writeFn) error {
	if err := os.MkdirAll(s.compPath(compID), 0700); err != nil {
		return err
	}
	keys := make([]string, 0, len(lineups))
	for k := range lineups {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	file := teamLineupFile{Lineups: make([]domain.TeamLineup, 0, len(keys))}
	for _, k := range keys {
		file.Lineups = append(file.Lineups, lineups[k])
	}
	data, err := yaml.Marshal(&file)
	if err != nil {
		return err
	}
	// Refresh the cache after a successful write, mirroring saveBracketLocked
	// (T211/T212). The refresh runs in both direct-write and WAL-write modes:
	// in WAL mode the on-disk file hasn't moved yet, but readers within the
	// same transaction see the staged data via the cache; a follow-up
	// cache-aware Load will re-parse from the cached copy rather than going to
	// disk. This is what makes the teamLineupFilename entries in
	// transactions.go's invalidate/refresh switches effective.
	if err := write(s.compPath(compID, teamLineupFilename), data, 0600); err != nil {
		return err
	}
	cache := s.getFileCache(compID, teamLineupFilename)
	cache.mu.Lock()
	cache.data = copyTeamLineups(lineups)
	cache.mtime = s.FileMtime(compID, teamLineupFilename)
	cache.mu.Unlock()
	return nil
}

// SetTeamLineup validates and persists a lineup, replacing any prior
// entry for the same (teamID, round). The caller MUST pass the
// competition's team size so ValidatePositions can enforce the FIK
// position-key rules. Lineups are always editable, including while a
// match is running or completed.
//
// FR-040, FR-041 / R4 / CHK012.
func (s *Store) SetTeamLineup(compID string, lineup domain.TeamLineup, teamSize int) error {
	if err := ValidateCompetitionID(compID); err != nil {
		return err
	}
	if err := lineup.ValidatePositions(teamSize); err != nil {
		return err
	}
	mu := s.getCompLock(compID)
	mu.Lock()
	defer mu.Unlock()
	return s.setTeamLineupLocked(compID, lineup, teamSize, s.directWrite)
}

// setTeamLineupLocked applies the load-mutate-save dance WITHOUT
// acquiring the per-competition lock. Caller MUST already hold it
// (typically via WithTransaction).
//
// ValidatePositions() is re-run here so the lock-free path is as safe
// as the public method; transaction bodies don't have to remember to
// validate first. The write parameter routes the final save (T211/T212).
func (s *Store) setTeamLineupLocked(compID string, lineup domain.TeamLineup, teamSize int, write writeFn) error {
	if err := lineup.ValidatePositions(teamSize); err != nil {
		return err
	}
	current, err := s.loadTeamLineupsLocked(compID)
	if err != nil {
		return err
	}
	key := lineupStorageKey(lineup)
	// Defensive: the persisted record carries CompetitionID so the file
	// is self-describing even if the directory is moved.
	lineup.CompetitionID = compID
	current[key] = lineup
	return s.saveTeamLineupsLocked(compID, current, write)
}

// DeleteTeamLineup removes the lineup for (teamID, round) if present.
// Lineups are always deletable, including while a match is running.
//
// Returns nil when no entry exists (idempotent delete).
func (s *Store) DeleteTeamLineup(compID, teamID string, round int) error {
	if err := ValidateCompetitionID(compID); err != nil {
		return err
	}
	mu := s.getCompLock(compID)
	mu.Lock()
	defer mu.Unlock()

	current, err := s.loadTeamLineupsLocked(compID)
	if err != nil {
		return err
	}
	key := teamLineupKey(teamID, round)
	if _, ok := current[key]; !ok {
		return nil
	}
	delete(current, key)
	return s.saveTeamLineupsLocked(compID, current, s.directWrite)
}

// FindBestLineup returns the most relevant lineup for teamID from a pre-loaded
// lineups map, following the AMENDMENT 1 priority order:
//  1. Match-scoped entry keyed by matchID (exact match for the specific bout).
//  2. Round-scoped: the highest round <= maxRound (the current match's round,
//     e.g. 0 for pool matches, bracket round index for knockout matches).
//  3. Round-scoped: the highest round overall (fallback when no saved lineup
//     has round <= maxRound, e.g. operator saved a bracket-phase lineup but the
//     current match is a pool match, or vice versa).
//
// Returns the lineup and true when found, the zero value and false otherwise.
// Callers should use LoadTeamLineups to obtain the map before calling this.
func FindBestLineup(lineups map[string]domain.TeamLineup, teamID, matchID string, maxRound int) (domain.TeamLineup, bool) {
	return FindBestLineupAny(lineups, []string{teamID}, matchID, maxRound)
}

// FindBestLineupAny is FindBestLineup for a set of candidate team keys.
// The lineup editor keys lineups by the team PARTICIPANT ID (player.id)
// while match sides carry the team display NAME, so callers resolving a
// lineup for a match side must try both keys ("match on id OR name").
// The AMENDMENT 1 priority tiers apply ACROSS the whole key set: a
// match-scoped entry under any key beats a round-scoped entry under any
// key. Within a tier, ties between keys resolve to the first teamID in
// the slice that has an entry.
func FindBestLineupAny(lineups map[string]domain.TeamLineup, teamIDs []string, matchID string, maxRound int) (domain.TeamLineup, bool) {
	// 1. Match-scoped (exact), first key wins.
	if matchID != "" {
		for _, teamID := range teamIDs {
			key := teamLineupMatchKey(teamID, matchID)
			if l, ok := lineups[key]; ok {
				return l, true
			}
		}
	}
	// 2. Round-scoped: highest round <= maxRound.
	// 3. Round-scoped: highest round overall (AMENDMENT 1 fallback).
	var best domain.TeamLineup
	hasBest := false
	var fallback domain.TeamLineup
	hasFallback := false
	for _, l := range lineups {
		if l.MatchID != "" || !slices.Contains(teamIDs, l.TeamID) {
			continue // skip wrong team or match-scoped entries
		}
		if l.Round <= maxRound {
			if !hasBest || l.Round > best.Round {
				best = l
				hasBest = true
			}
		}
		if !hasFallback || l.Round > fallback.Round {
			fallback = l
			hasFallback = true
		}
	}
	if hasBest {
		return best, true
	}
	if hasFallback {
		return fallback, true
	}
	return domain.TeamLineup{}, false
}

// DeleteTeamLineupForMatch removes the match-scoped lineup for
// (teamID, matchID) if present (mp-825). Lineups are always deletable,
// including while a match is running.
//
// Returns nil when no entry exists (idempotent delete).
func (s *Store) DeleteTeamLineupForMatch(compID, teamID, matchID string) error {
	if err := ValidateCompetitionID(compID); err != nil {
		return err
	}
	mu := s.getCompLock(compID)
	mu.Lock()
	defer mu.Unlock()

	current, err := s.loadTeamLineupsLocked(compID)
	if err != nil {
		return err
	}
	key := teamLineupMatchKey(teamID, matchID)
	if _, ok := current[key]; !ok {
		return nil
	}
	delete(current, key)
	return s.saveTeamLineupsLocked(compID, current, s.directWrite)
}

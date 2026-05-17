// Package state — team_lineup.go owns the on-disk persistence for
// domain.TeamLineup (FR-040, Slice 7.B / T126).
//
// One file per competition lives at
// tournament-data/competitions/<id>/lineups.yaml and is keyed by
// "<teamId>-<round>". A missing file is treated as "no lineups
// submitted yet". All load/mutate/save sequences run under the
// per-competition write lock (s.getCompLock) so concurrent PUTs to
// different teams in the same competition serialize correctly without
// clobbering each other's work — same pattern competitor_status.go
// uses.
//
// Locking is a hard requirement here, not a nicety: the FR-040 contract
// says a lineup is mutable up until the round's first match goes live,
// then frozen. The "is this round live?" check has to happen INSIDE the
// lock taken by SetTeamLineup (T128a) or two concurrent writers could
// both pass an unlocked TOCTOU check and one would overwrite a lineup
// that the other side had just legitimately frozen.
package state

import (
	"errors"
	"fmt"
	"os"
	"sort"
	"time"

	"github.com/gitrgoliveira/bracket-creator/internal/domain"
	"gopkg.in/yaml.v3"
)

// ErrLineupLocked is returned by SetTeamLineup when the team's lineup
// for the given round is already frozen (the round's first match went
// live). Mapped to HTTP 409 by the handler. FR-040.
var ErrLineupLocked = errors.New("team lineup locked: round has started")

// teamLineupFile is the on-disk YAML shape. Wire-stable: persisted as a
// sorted slice so file contents are deterministic.
type teamLineupFile struct {
	Lineups []domain.TeamLineup `yaml:"lineups"`
}

const teamLineupFilename = "lineups.yaml"

// teamLineupKey is the in-memory map key. Two lineups for the same
// team in different rounds coexist (a 5-person team might rotate a
// kiken'd jiho between round 1 and round 2), so the round is part of
// the key — not just teamID.
func teamLineupKey(teamID string, round int) string {
	return fmt.Sprintf("%s-%d", teamID, round)
}

// LoadTeamLineups returns every lineup persisted for compID, keyed by
// "<teamID>-<round>". A missing file is treated as "no lineups yet"
// and returns an empty map (consistent with LoadCompetitorStatus).
//
// Uses the per-competition read lock so concurrent writes for the
// same competition can't race with this read.
func (s *Store) LoadTeamLineups(compID string) (map[string]domain.TeamLineup, error) {
	if err := ValidateCompetitionID(compID); err != nil {
		return nil, err
	}
	mu := s.getCompLock(compID)
	mu.RLock()
	defer mu.RUnlock()
	return s.loadTeamLineupsLocked(compID)
}

func (s *Store) loadTeamLineupsLocked(compID string) (map[string]domain.TeamLineup, error) {
	path := s.compPath(compID, teamLineupFilename)
	data, err := os.ReadFile(path) // #nosec G304 — compPath cleans the path.
	if err != nil {
		if os.IsNotExist(err) {
			return map[string]domain.TeamLineup{}, nil
		}
		return nil, err
	}
	return parseTeamLineupsBytes(data)
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
		out[teamLineupKey(l.TeamID, l.Round)] = l
	}
	return out, nil
}

// saveTeamLineupsLocked persists the lineups map. Caller MUST hold
// the per-comp write lock. The write parameter routes the actual
// file write — directWrite for non-tx callers, WAL-capturing writer
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
	return write(s.compPath(compID, teamLineupFilename), data, 0600)
}

// SetTeamLineup validates and persists a lineup, replacing any prior
// entry for the same (teamID, round). The caller MUST pass the
// competition's team size — Validate() enforces the FIK back-fill rule
// against it.
//
// Refuses with ErrLineupLocked when the prior entry has a non-nil
// LockedAt (the round's first match has gone live). The check runs
// INSIDE the per-comp write lock so a concurrent LockTeamLineupsForRound
// can't race with this set (T128a).
//
// FR-040, FR-041 / R4 / CHK012.
func (s *Store) SetTeamLineup(compID string, lineup domain.TeamLineup, teamSize int) error {
	if err := ValidateCompetitionID(compID); err != nil {
		return err
	}
	if err := lineup.Validate(teamSize); err != nil {
		return err
	}
	mu := s.getCompLock(compID)
	mu.Lock()
	defer mu.Unlock()
	return s.setTeamLineupLocked(compID, lineup, teamSize, s.directWrite)
}

// setTeamLineupLocked applies the freeze-check + load-mutate-save dance
// WITHOUT acquiring the per-competition lock. Caller MUST already hold
// it (typically via WithTransaction).
//
// Validate() is re-run here so the lock-free path is as safe as the
// public method — transaction bodies don't have to remember to
// validate first. The write parameter routes the final save
// (T211/T212).
func (s *Store) setTeamLineupLocked(compID string, lineup domain.TeamLineup, teamSize int, write writeFn) error {
	if err := lineup.Validate(teamSize); err != nil {
		return err
	}
	current, err := s.loadTeamLineupsLocked(compID)
	if err != nil {
		return err
	}
	key := teamLineupKey(lineup.TeamID, lineup.Round)
	if existing, ok := current[key]; ok && existing.LockedAt != nil {
		return ErrLineupLocked
	}
	// T128a race-condition guard: a concurrent score-save could have
	// transitioned a round's first match to running BETWEEN the
	// caller's last GET and this PUT — without the lineup file yet
	// recording LockedAt (the engine wires that as a follow-up).
	// Re-check the round's match status inside this lock and refuse
	// the write if the round is no longer mutable. Cheap pure-disk
	// reads, both keyed to the same compPath we already cleaned.
	if locked, err := s.roundHasLiveOrCompletedMatchLocked(compID, lineup.Round); err != nil {
		return err
	} else if locked {
		return ErrLineupLocked
	}
	// Defensive: the persisted record carries CompetitionID so the file
	// is self-describing even if the directory is moved.
	lineup.CompetitionID = compID
	current[key] = lineup
	return s.saveTeamLineupsLocked(compID, current, write)
}

// roundHasLiveOrCompletedMatchLocked is the T128a in-lock check: are
// any matches for the given round currently running or completed?
//
// Caller MUST already hold the per-competition lock. Reads pool-matches
// and bracket directly from disk (bypassing the cache) so a concurrent
// engine writer that just released its lock can't leave us reading
// stale snapshot data.
//
// Round mapping:
//   - Bracket: round N maps to Rounds[N] (zero-indexed; round 0 == first
//     elimination round).
//   - Pool matches: there's no explicit round field in pool-matches.csv,
//     so for now every pool match is treated as round 0. This matches
//     the engine-side TODO at LockTeamLineupsForRound — multi-round
//     lineups will need a richer mapping when team-pool-rotation
//     lands.
func (s *Store) roundHasLiveOrCompletedMatchLocked(compID string, round int) (bool, error) {
	// Bracket round-N: scan only that round's slice for any running/
	// completed match.
	bracketParsed, err := parseBracketFile(s.compPath(compID, "bracket.json"))
	if err != nil {
		return false, err
	}
	if bracket, ok := bracketParsed.(*Bracket); ok && bracket != nil {
		if round >= 0 && round < len(bracket.Rounds) {
			for _, m := range bracket.Rounds[round] {
				if m.Status == MatchStatusRunning || m.Status == MatchStatusCompleted {
					return true, nil
				}
			}
		}
	}
	// Pool matches collapse to round 0 — only check when round == 0.
	if round == 0 {
		poolParsed, err := parsePoolMatchesFile(s.compPath(compID, "pool-matches.csv"))
		if err != nil {
			return false, err
		}
		poolMatches, _ := poolParsed.([]MatchResult)
		for _, m := range poolMatches {
			if m.Status == MatchStatusRunning || m.Status == MatchStatusCompleted {
				return true, nil
			}
		}
	}
	return false, nil
}

// DeleteTeamLineup removes the lineup for (teamID, round) if present.
// Same lock-protected refusal as SetTeamLineup when the entry is
// already locked: deleting a frozen lineup would re-open the round to
// edits and break the freeze contract.
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
	existing, ok := current[key]
	if !ok {
		return nil
	}
	if existing.LockedAt != nil {
		return ErrLineupLocked
	}
	delete(current, key)
	return s.saveTeamLineupsLocked(compID, current, s.directWrite)
}

// LockTeamLineupsForRound stamps LockedAt on every persisted lineup
// whose Round matches `round`. Idempotent: lineups already locked keep
// their original LockedAt (the first-live-match time stays canonical;
// re-running this method on the same round is a no-op for those
// records).
//
// Called by the engine when a team match transitions to running (T128).
// Returns nil when no lineups exist for this competition or this round.
func (s *Store) LockTeamLineupsForRound(compID string, round int, lockedAt time.Time) error {
	if err := ValidateCompetitionID(compID); err != nil {
		return err
	}
	mu := s.getCompLock(compID)
	mu.Lock()
	defer mu.Unlock()

	return s.lockTeamLineupsForRoundLocked(compID, round, lockedAt, s.directWrite)
}

// lockTeamLineupsForRoundLocked is the lock-free body of
// LockTeamLineupsForRound. Caller MUST already hold the per-comp
// write lock (typically via WithTransaction).
//
// Used by the tx-aware score / decision paths (T156) so the lineup
// freeze runs under the same lock acquire as the score write — without
// this variant the public LockTeamLineupsForRound would deadlock when
// called from inside a WithTransaction closure (sync.RWMutex is
// non-recursive). The write parameter routes the save (T211/T212).
func (s *Store) lockTeamLineupsForRoundLocked(compID string, round int, lockedAt time.Time, write writeFn) error {
	current, err := s.loadTeamLineupsLocked(compID)
	if err != nil {
		return err
	}
	changed := false
	for k, l := range current {
		if l.Round != round {
			continue
		}
		if l.LockedAt != nil {
			continue
		}
		t := lockedAt
		l.LockedAt = &t
		current[k] = l
		changed = true
	}
	if !changed {
		return nil
	}
	return s.saveTeamLineupsLocked(compID, current, write)
}

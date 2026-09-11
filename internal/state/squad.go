// Package state, squad.go owns the on-disk persistence for a team's squad
// (bc-tmid pass 1): the people on a team, each with a stable id and a
// display index (domain.TeamMember), kept OUT of Player.Metadata so the
// participant-roster write paths that overwrite that array (see
// ErrDuplicateTeamMember's doc comment in participants.go for the confirmed
// data-loss sites) can no longer destroy it.
//
// One file per competition lives at
// tournament-data/competitions/<id>/squads.yaml, keyed by the TEAM's
// participant id (never its name -- a team may be renamed, and the id is
// what does not change underneath that). Modelled closely on
// team_lineup.go, the same shape of problem (per-competition YAML keyed by
// an id) that already gets the locking, caching and file-version
// discipline right.
//
// Squad size is unconstrained and independent of the competition's
// TeamSize (operator ruling 2026-09-09): real teams carry reserves and
// replacements, so a squad may be larger than however many fight at once.
//
// A member is added, or renamed; there is NO removal operation (operator
// ruling), so an index once minted is never freed and never reused --
// AddTeamMember mints max(existing index)+1, never a stored counter (see
// domain.TeamMember's own doc comment for why a stored "next index" would
// be derivable state that can disagree with the list it describes).
//
// squads.yaml is deliberately NOT in allowedDrawFiles (competition.go): a
// team's squad and its draw are independent lifecycles, so discarding the
// draw must never touch it.
package state

import (
	"errors"
	"fmt"
	"os"
	"strings"

	"github.com/gitrgoliveira/bracket-creator/internal/domain"
	"github.com/gitrgoliveira/bracket-creator/internal/helper"
	"gopkg.in/yaml.v3"
)

const squadsFilename = "squads.yaml"

// ErrTeamMemberNotFound is returned by RenameTeamMember when (teamID,
// memberID) does not resolve to a stored entry -- either teamID has no
// squad recorded at all, or it does but no member inside it carries
// memberID. Both cases collapse to this one sentinel: from the caller's
// perspective there is nothing to rename either way.
var ErrTeamMemberNotFound = errors.New("team member not found")

// ErrTeamNotFound is returned when a squad write names a team id that no
// participant in this competition carries.
var ErrTeamNotFound = errors.New("no team with that id in this competition")

// squadsFile is the on-disk YAML shape: a single top-level key so the file
// is self-describing and can grow a sibling key later without a format
// break (mirrors teamLineupFile's own reasoning). Marshaling a
// map[string][]domain.TeamMember directly (rather than flattening to a
// slice the way teamLineupFile does) is safe here: gopkg.in/yaml.v3 sorts
// map keys before encoding, so the team-id ordering on disk is
// deterministic without this package doing it by hand.
type squadsFile struct {
	Squads map[string][]domain.TeamMember `yaml:"squads"`
}

// parseSquadsFile reads and parses squads.yaml at path. A missing file is
// "no squad recorded yet" and returns an empty map, matching
// parseTeamLineupsFile's contract for the identical situation.
func parseSquadsFile(path string) (map[string][]domain.TeamMember, error) {
	data, err := os.ReadFile(path) // #nosec G304, compPath enforces containment under the competitions dir.
	if err != nil {
		if os.IsNotExist(err) {
			return map[string][]domain.TeamMember{}, nil
		}
		return nil, err
	}
	return parseSquadsBytes(data)
}

// parseSquadsBytes parses squads.yaml from in-memory bytes. Empty input →
// empty map, matching the "file does not exist" contract.
func parseSquadsBytes(data []byte) (map[string][]domain.TeamMember, error) {
	if len(data) == 0 {
		return map[string][]domain.TeamMember{}, nil
	}
	var file squadsFile
	if err := yaml.Unmarshal(data, &file); err != nil {
		return nil, err
	}
	if file.Squads == nil {
		file.Squads = map[string][]domain.TeamMember{}
	}
	return file.Squads, nil
}

// copySquads deep-copies a squads map so cached data is never aliased to a
// caller (mirrors copyTeamLineups; callers of LoadSquads must be free to
// mutate the result).
func copySquads(in map[string][]domain.TeamMember) map[string][]domain.TeamMember {
	out := make(map[string][]domain.TeamMember, len(in))
	for id, members := range in {
		cp := make([]domain.TeamMember, len(members))
		copy(cp, members)
		out[id] = cp
	}
	return out
}

// LoadSquads returns every team's squad persisted for compID, keyed by the
// team's participant id. A missing file is treated as "no squads recorded
// yet" and returns an empty map (consistent with LoadTeamLineups).
//
// Cache-aware (mtime-keyed via loadCached, same as LoadTeamLineups).
// Returns a deep copy so callers can mutate the map freely.
func (s *Store) LoadSquads(compID string) (map[string][]domain.TeamMember, error) {
	data, err := s.loadCached(compID, squadsFilename, func(path string) (any, error) {
		return parseSquadsFile(path)
	})
	if err != nil {
		return nil, err
	}
	return copySquads(data.(map[string][]domain.TeamMember)), nil
}

// loadSquadsLocked reads squads.yaml directly from disk WITHOUT acquiring
// the per-competition lock. Caller MUST already hold the lock. Bypasses the
// cache: locked callers are about to load-mutate-save and need a fresh
// private map, mirroring loadTeamLineupsLocked.
func (s *Store) loadSquadsLocked(compID string) (map[string][]domain.TeamMember, error) {
	return parseSquadsFile(s.compPath(compID, squadsFilename))
}

// saveSquadsLocked persists the squads map. Caller MUST hold the per-comp
// write lock.
//
// Deliberately does NOT create the competition directory -- see
// saveOverridesLocked's doc comment for the full reasoning (a write
// landing after DeleteCompetition would otherwise rebuild
// competitions/<id>/ around a lone squads.yaml, which ListCompetitions
// keeps reporting and a same-named recreation adopts).
// saveCompetitionChangedLocked is the ONE writer that legitimately creates
// the directory; do not reintroduce os.MkdirAll here.
func (s *Store) saveSquadsLocked(compID string, squads map[string][]domain.TeamMember, write writeFn) error {
	data, err := yaml.Marshal(&squadsFile{Squads: squads})
	if err != nil {
		return err
	}
	path := s.compPath(compID, squadsFilename)
	if err := write(path, data, 0600); err != nil {
		return err
	}

	cache := s.getFileCache(compID, squadsFilename)
	cache.mu.Lock()
	cache.data = copySquads(squads)
	cache.mtime = s.FileMtime(compID, squadsFilename)
	cache.mu.Unlock()

	// Bumped AFTER the bytes land and the cache is refreshed
	// (bumpFileVersion's contract, store.go): a future consumer keying a
	// derived cache on squads.yaml (bout-log/kachinuki member resolution,
	// a later pass of bc-tmid) must see this write without a
	// same-millisecond mtime hiding it from FileVersion.
	s.bumpFileVersion(compID, squadsFilename)
	return nil
}

// requireTeamParticipantLocked refuses a teamID no participant in this
// competition carries. Caller MUST hold the per-competition lock.
//
// The check belongs HERE rather than in the handler above it because
// AddTeamMember is the one door that MINTS a member, and there is no removal
// operation: a member created under an id no team holds could never be
// cleaned up through the app, so a mistyped id would write permanent,
// unreachable data. RenameTeamMember needs no equivalent, since a team with
// no squad already fails its member lookup.
//
// Reads the roster through the no-lock loader for the reason SaveSeeds does
// (seeds.go): LoadParticipants would re-acquire the lock this function's
// caller already holds and deadlock a non-reentrant mutex. WithSeeds is off
// because the seed merge is irrelevant to an id comparison and would read a
// second file.
func (s *Store) requireTeamParticipantLocked(compID, teamID string) error {
	withZekken, _, err := s.withZekkenNameLocked(compID)
	if err != nil {
		return err
	}
	players, err := s.loadParticipantsNoLock(compID, withZekken, LoadParticipantsOpts{WithSeeds: false})
	if err != nil {
		return err
	}
	for i := range players {
		if players[i].ID == teamID {
			return nil
		}
	}
	return fmt.Errorf("%w: %q", ErrTeamNotFound, teamID)
}

// squadDuplicateNameCheck runs helper.DuplicateNamesWithKeys over
// candidateName plus otherNames (every OTHER member already on the team --
// the member being added has no "other" self to compare against, and a
// rename excludes the member being renamed, so renaming to one's own
// current name is never a self-collision) and returns
// ErrDuplicateTeamMember, wrapped with %w so errors.Is matches, when
// candidateName collides with any of them.
//
// Reuses the SAME normaliser checkTeamMemberNameCollisions uses for the
// participant-roster floor (participants.go), rather than a second
// hand-rolled scan, so "Sato"/"sato"/" Sato "/"Satō" are refused here
// exactly as they are there.
func squadDuplicateNameCheck(teamID, candidateName string, otherNames []string) error {
	names := make([]string, 0, len(otherNames)+1)
	names = append(names, otherNames...)
	names = append(names, candidateName)
	if dupes, _ := helper.DuplicateNamesWithKeys(names); len(dupes) > 0 {
		return fmt.Errorf("%w: team %q already has a member named %q", ErrDuplicateTeamMember, teamID, candidateName)
	}
	return nil
}

// AddTeamMember mints a new member's id and the next display index
// (operator ruling 2026-09-09: assigned automatically as members are
// ADDED), appends it to teamID's squad, and persists. There is no cap on
// squad size: real teams carry reserves and replacements, so a squad may
// exceed the competition's TeamSize.
//
// teamID is treated as opaque, exactly like handlers_lineup.go's own
// teamID, and NOTHING validates it against participants.csv -- not this
// function and not the handler above it. A member added under an id no
// team holds is therefore reachable, and because there is no removal
// operation it stays. It is inert (no surface renders a squad for an id
// that is not a participant) and the SPA only ever sends ids it read off
// the roster, so this is a known limit of the opaque-key convention rather
// than an oversight. Validating it is an operator-visible behaviour change
// -- a request that succeeds today would 404 -- so it is a decision, not a
// tidy-up.
func (s *Store) AddTeamMember(compID, teamID, name string) (domain.TeamMember, error) {
	if err := ValidateCompetitionID(compID); err != nil {
		return domain.TeamMember{}, err
	}
	name = strings.TrimSpace(name)

	mu := s.getCompLock(compID)
	mu.Lock()
	defer mu.Unlock()

	if err := s.requireTeamParticipantLocked(compID, teamID); err != nil {
		return domain.TeamMember{}, err
	}

	squads, err := s.loadSquadsLocked(compID)
	if err != nil {
		return domain.TeamMember{}, err
	}
	existing := squads[teamID]

	otherNames := make([]string, 0, len(existing))
	for _, m := range existing {
		otherNames = append(otherNames, m.Name)
	}
	if err := squadDuplicateNameCheck(teamID, name, otherNames); err != nil {
		return domain.TeamMember{}, err
	}

	nextIndex := 1
	for _, m := range existing {
		if m.Index >= nextIndex {
			nextIndex = m.Index + 1
		}
	}
	member := domain.TeamMember{ID: newParticipantID(), Index: nextIndex, Name: name}
	squads[teamID] = append(existing, member)

	if err := s.saveSquadsLocked(compID, squads, s.directWrite); err != nil {
		return domain.TeamMember{}, err
	}
	return member, nil
}

// RenameTeamMember keeps memberID's id and index and replaces its Name.
// Returns ErrTeamMemberNotFound when (teamID, memberID) does not resolve
// (no squad for teamID at all, or no member with that id inside it).
func (s *Store) RenameTeamMember(compID, teamID, memberID, newName string) error {
	if err := ValidateCompetitionID(compID); err != nil {
		return err
	}
	newName = strings.TrimSpace(newName)

	mu := s.getCompLock(compID)
	mu.Lock()
	defer mu.Unlock()

	squads, err := s.loadSquadsLocked(compID)
	if err != nil {
		return err
	}
	existing := squads[teamID]

	target := -1
	otherNames := make([]string, 0, len(existing))
	for i, m := range existing {
		if m.ID == memberID {
			target = i
			continue
		}
		otherNames = append(otherNames, m.Name)
	}
	if target == -1 {
		return ErrTeamMemberNotFound
	}
	if err := squadDuplicateNameCheck(teamID, newName, otherNames); err != nil {
		return err
	}

	existing[target].Name = newName
	squads[teamID] = existing
	return s.saveSquadsLocked(compID, squads, s.directWrite)
}

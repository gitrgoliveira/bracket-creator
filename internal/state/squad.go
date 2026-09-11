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
// A squad's FLOOR, however, IS the competition's TeamSize (bc-pnum ruling:
// "by default teams have x team members, as defined in the competition
// config, and those positions have their numbers"): upgradeSquadsFromMetadataLocked
// (legacy_upgrade.go) seeds every team up to TeamSize on load, minting an
// id and a 1-based index for each slot with Name left blank unless already
// known, and pads (never trims) the squad again if TeamSize is later
// raised.
//
// A member is added, renamed, or CLEARED; there is no ENTRY-removal
// operation (an id/index pair, once minted, is never deleted or reused).
// "Removal" in the operator's own words is ClearTeamMemberName blanking
// Name back to "" -- a bout already fought refers to a position by its
// index, and the label (e.g. "T10.4") must keep meaning what it always
// meant. AddTeamMember mints max(existing index)+1, never a stored counter
// (see domain.TeamMember's own doc comment for why a stored "next index"
// would be derivable state that can disagree with the list it describes).
// ClearTeamMemberName is refused once the competition has started
// (state.CanStart(comp.Status) false), the same precondition
// engine.StartCompetition itself gates on.
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

// ErrTeamMemberNotFound is returned by RenameTeamMember and
// ClearTeamMemberName when (teamID, memberID) does not resolve to a stored
// entry -- either teamID has no squad recorded at all, or it does but no
// member inside it carries memberID. Both cases collapse to this one
// sentinel: from the caller's perspective there is nothing to rename (or
// clear) either way.
var ErrTeamMemberNotFound = errors.New("team member not found")

// ErrTeamNotFound is returned when a squad write names a team id that no
// participant in this competition carries.
var ErrTeamNotFound = errors.New("no team with that id in this competition")

// ErrTeamMemberClearAfterStart is returned by ClearTeamMemberName when the
// competition has already started (state.CanStart(comp.Status) is false --
// past setup and past a generated-but-not-yet-started draw). Distinct from
// ErrCompetitionNotInSetup (participants.go), which gates the roster floor
// on the narrower "still in setup" precondition: a squad clear is allowed
// through draw-ready too, the same window engine.StartCompetition itself
// still accepts a one-click start from, so gating this on CanStart rather
// than requireSetupLocked's stricter check is deliberate, not an oversight.
var ErrTeamMemberClearAfterStart = errors.New("cannot clear a team member's name once the competition has started")

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
//
// Blank names are excluded from BOTH sides of the comparison (bc-pnum): a
// team's squad is seeded with TeamSize members whose Name is blank until
// filled in or after a clear, so a real team routinely holds several blank
// names at once. helper.NormalizeParticipantName("") returns "", so without
// this exclusion every blank slot beyond the first would register as a
// "duplicate" of the one before it -- refusing the team's own default state,
// and any later add/rename alongside it, on every team the moment it has
// TWO blank slots (which is every seeded team, since TeamSize is always
// >= 2 for a team competition). A blank candidateName is itself never a
// collision (there is nothing to name yet), so it short-circuits before
// even excluding blanks from otherNames.
func squadDuplicateNameCheck(teamID, candidateName string, otherNames []string) error {
	if strings.TrimSpace(candidateName) == "" {
		return nil
	}
	names := make([]string, 0, len(otherNames)+1)
	for _, n := range otherNames {
		if strings.TrimSpace(n) == "" {
			continue
		}
		names = append(names, n)
	}
	names = append(names, candidateName)
	if dupes, _ := helper.DuplicateNamesWithKeys(names); len(dupes) > 0 {
		// Deliberately does NOT name the team. Both callers act on ONE team
		// the caller already identified, and this message is shown to the
		// operator verbatim inside the lineup warning, where the only id
		// available here is the team's UUID: a raw identifier dropped into
		// the middle of a sentence about a fighter is noise the operator
		// cannot act on. The surrounding warning already names the position
		// and the person.
		return fmt.Errorf("%w: %q is already on this team", ErrDuplicateTeamMember, candidateName)
	}
	return nil
}

// AddTeamMember mints a new member's id and the next display index
// (operator ruling 2026-09-09: assigned automatically as members are
// ADDED), appends it to teamID's squad, and persists. There is no cap on
// squad size: real teams carry reserves and replacements, so a squad may
// exceed the competition's TeamSize.
//
// teamID IS validated against participants.csv here, which is where this
// function departs from handlers_lineup.go's opaque-teamID convention. The
// reason is the missing removal operation: a member minted under an id no
// team holds could never be reached or deleted through the app, so a typo
// would write permanent junk. A lineup keyed on a bogus id is recoverable
// by overwriting it; a squad member is not, which is why the two diverge.
// See requireTeamParticipantLocked below.
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

// ClearTeamMemberName is the operator's "removal": it blanks memberID's
// Name back to "" and leaves ID and Index untouched, so a bout already
// fought that refers to this position (e.g. "T10.4") keeps meaning what it
// always meant. It never deletes the entry -- there is no ENTRY-removal
// operation, see this file's own package doc comment.
//
// Returns ErrTeamMemberNotFound when (teamID, memberID) does not resolve
// (no squad for teamID at all, or no member with that id inside it),
// matching RenameTeamMember's contract exactly.
//
// Refuses with ErrTeamMemberClearAfterStart once the competition has
// started (state.CanStart(comp.Status) false, checked under this same
// lock so a concurrent POST .../start landing between an operator's outer
// check and this call cannot let a clear slip in after all). A missing
// competition record is treated as "not yet started" (mirrors
// requireSetupLocked's identical treatment a few lines above in this
// package), so a fresh test fixture or an in-flight creation still allows
// a clear rather than refusing on a technicality.
//
// Loads the competition through loadCompetitionLocked (competition.go), the
// no-lock reader, for the same reason requireTeamParticipantLocked above
// reads the roster through loadParticipantsNoLock: this function already
// holds the per-comp lock, and LoadCompetition would re-acquire it and
// deadlock the non-reentrant mutex.
//
// Does NOT run squadDuplicateNameCheck: blanking a name can never collide
// with anything (squadDuplicateNameCheck already treats a blank candidate
// as never a collision), so the check would be a costly no-op here.
func (s *Store) ClearTeamMemberName(compID, teamID, memberID string) error {
	if err := ValidateCompetitionID(compID); err != nil {
		return err
	}

	mu := s.getCompLock(compID)
	mu.Lock()
	defer mu.Unlock()

	comp, err := s.loadCompetitionLocked(compID)
	if err != nil {
		return err
	}
	if comp != nil && !CanStart(comp.Status) {
		return ErrTeamMemberClearAfterStart
	}

	squads, err := s.loadSquadsLocked(compID)
	if err != nil {
		return err
	}
	existing := squads[teamID]

	target := -1
	for i, m := range existing {
		if m.ID == memberID {
			target = i
			break
		}
	}
	if target == -1 {
		return ErrTeamMemberNotFound
	}

	existing[target].Name = ""
	squads[teamID] = existing
	return s.saveSquadsLocked(compID, squads, s.directWrite)
}

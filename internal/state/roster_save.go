package state

import (
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"os"
	"strconv"

	"github.com/gitrgoliveira/bracket-creator/internal/domain"
	"github.com/gitrgoliveira/bracket-creator/internal/helper"
)

// ErrRosterChanged reports a whole-roster write built from a roster that is
// no longer the stored one: another desk added, removed, renamed, moved or
// seeded a competitor since the writer read it. Nothing was written. The
// error is a *RosterChangedError carrying the roster as it stands, so the
// writer can lay its own change on it and send it again.
var ErrRosterChanged = errors.New("roster changed since it was read")

// RosterChangedError is ErrRosterChanged with the stored roster (seeds
// merged, as a competition detail serves it) and its version.
type RosterChangedError struct {
	Players []domain.Player
	Version string
}

func (e *RosterChangedError) Error() string { return ErrRosterChanged.Error() }

// Is lets errors.Is(err, ErrRosterChanged) match.
func (e *RosterChangedError) Is(target error) bool { return target == ErrRosterChanged }

// RosterVersion names a roster as a whole-roster write sets it: each
// competitor's id, name, zekken name, dojo, metadata, registration source
// and seed, in roster order. It is a hash of the data, so it reads the same
// across a restart and on every server, and a competition detail and a write
// computing it from the same roster always agree.
//
// A check-in is NOT part of it: the roster PUT never writes one (only the
// check-in doors do, and SaveRoster keeps every stored check-in), so a
// check-in made while a seed PUT is out cannot be lost to it and need not
// refuse it. Registration desks check competitors in continuously while the
// seeding is typed; counting them would refuse every seed PUT made then.
func RosterVersion(players []domain.Player) string {
	h := sha256.New()
	field := func(s string) {
		h.Write([]byte(strconv.Itoa(len(s))))
		h.Write([]byte{':'})
		h.Write([]byte(s))
	}
	field(strconv.Itoa(len(players)))
	for _, p := range players {
		field(p.ID)
		field(p.Name)
		field(p.DisplayName)
		field(p.Dojo)
		field(strconv.Itoa(len(p.Metadata)))
		for _, m := range p.Metadata {
			field(m)
		}
		field(p.Source)
		field(strconv.Itoa(p.Seed))
	}
	return hex.EncodeToString(h.Sum(nil))[:16]
}

// SaveRoster is the roster PUT's write: the roster and its seeds as ONE step
// under the competition's lock.
//
//   - expected, when not empty, is the RosterVersion of the roster the writer
//     built this one from. When the stored roster is no longer that one the
//     write is refused with a *RosterChangedError carrying the stored roster
//     and its version, and nothing is written. An empty expected (an API
//     caller, an older page) writes unchecked, as before.
//   - Every competitor keeps the check-in stored for them (matched by
//     participant id; a competitor new to the roster is not checked in),
//     whatever the write carries: only the check-in doors check a competitor
//     in or out, so a check-in another desk made while this write was out is
//     never erased by it.
//
// seeds is the seeds.csv content the write sets (derived from the players by
// the caller); a failure to write it is returned as seedsErr after the roster
// itself landed, the contract the roster PUT always had.
func (s *Store) SaveRoster(compID string, players []domain.Player, seeds []domain.SeedAssignment, expected string) (seedsErr error, err error) {
	if err := ValidateCompetitionID(compID); err != nil {
		return nil, err
	}
	mu := s.getCompLock(compID)
	mu.Lock()
	defer mu.Unlock()
	withZekken, comp, err := s.withZekkenNameLocked(compID)
	if err != nil {
		return nil, err
	}
	var hint *bool
	if comp != nil {
		hint = comp.ParticipantIDsHint()
	}
	stored, err := s.loadParticipantsNoLock(compID, withZekken, LoadParticipantsOpts{WithSeeds: true, HasIDs: hint})
	if err != nil {
		return nil, err
	}
	if expected != "" {
		if now := RosterVersion(stored); now != expected {
			return nil, &RosterChangedError{Players: stored, Version: now}
		}
	}
	return s.saveRosterLocked(compID, players, seeds, stored, withZekken, comp)
}

// saveRosterLocked writes players (with each stored competitor's check-in
// kept, keepStoredCheckIns) and seeds, for a caller holding the lock.
func (s *Store) saveRosterLocked(compID string, players []domain.Player, seeds []domain.SeedAssignment, stored []domain.Player, withZekken bool, comp *Competition) (seedsErr error, err error) {
	if err := s.saveParticipantsNoLock(compID, keepStoredCheckIns(players, stored), withZekken, teamNameRule{comp: comp}); err != nil {
		return nil, err
	}
	return s.saveSeedsLocked(compID, seeds), nil
}

// keepStoredCheckIns answers a copy of players whose check-ins are the stored
// ones, and not checked in for a competitor the stored roster does not hold.
// A stored competitor is found by participant id. A stored row that carries
// no id (a roster saved before ids existed, which the first save after it
// gives ids) can be found by nothing else, so a row the write also sends with
// no id is found by its (name, dojo) pair, the identity rows had before ids
// (helper.CompetitorKey with no id). A row the write sends WITH an id is found
// by that id only, never by name: an id the stored roster lacks is a new
// competitor.
func keepStoredCheckIns(players, stored []domain.Player) []domain.Player {
	byID := make(map[string]bool, len(stored))
	byKey := map[string]bool{}
	for _, p := range stored {
		if !helper.ParticipantIDMissing(p.ID) {
			byID[p.ID] = p.CheckedIn
		} else {
			byKey[helper.CompetitorKey("", p.Name, p.Dojo)] = p.CheckedIn
		}
	}
	out := make([]domain.Player, len(players))
	copy(out, players)
	for i := range out {
		// A competitor the write names by an id is found by that id alone (an
		// id the stored roster does not hold is a new competitor); only one
		// the write sends with no id, as the page sends a row saved before ids
		// existed, is found by (name, dojo) among the stored rows with none.
		if helper.ParticipantIDMissing(out[i].ID) {
			out[i].CheckedIn = byKey[helper.CompetitorKey("", out[i].Name, out[i].Dojo)]
		} else {
			out[i].CheckedIn = byID[out[i].ID]
		}
	}
	return out
}

// ReplaceRosterKeepingCheckIns is the batch POST /participants write: the
// roster as given, with every stored check-in kept (keepStoredCheckIns), read
// and written under one lock so a check-in landing between the handler's
// read and this write is not erased. The seed rows it touches are the ones
// prune judges, in the same locked step (see SeedPruner).
func (s *Store) ReplaceRosterKeepingCheckIns(compID string, players []domain.Player, prune SeedPruner) (seedsErr error, err error) {
	if err := ValidateCompetitionID(compID); err != nil {
		return nil, err
	}
	mu := s.getCompLock(compID)
	mu.Lock()
	defer mu.Unlock()
	withZekken, comp, err := s.withZekkenNameLocked(compID)
	if err != nil {
		return nil, err
	}
	var hint *bool
	if comp != nil {
		hint = comp.ParticipantIDsHint()
	}
	stored, err := s.loadParticipantsNoLock(compID, withZekken, LoadParticipantsOpts{HasIDs: hint})
	if err != nil {
		return nil, err
	}
	// The seeds are read and judged inside this same locked step, before
	// anything is written, so a seed another desk set since the caller last
	// read them is judged too: kept, or the replace refused with nothing
	// written, never dropped by a judgement made on a copy read before the
	// lock (stage 6 review 13, m7; review 14, item 2).
	var (
		seeds   []domain.SeedAssignment
		kept    []domain.SeedAssignment
		readErr error
	)
	if prune != nil {
		var rerr error
		seeds, rerr = helper.ReadSeedsFileRaw(s.compPath(compID, "seeds.csv"))
		switch {
		case rerr == nil:
			if kept, err = prune(stored, seeds); err != nil {
				return nil, err
			}
		case !os.IsNotExist(rerr):
			readErr = rerr
		}
	}
	if err := s.saveParticipantsNoLock(compID, keepStoredCheckIns(players, stored), withZekken, teamNameRule{comp: comp}); err != nil {
		return nil, err
	}
	if readErr != nil {
		return readErr, nil
	}
	if prune != nil && len(kept) != len(seeds) {
		return s.saveSeedsLocked(compID, kept), nil
	}
	return nil, nil
}

// SeedPruner judges, under the competition's lock and before the roster is
// written, which of the stored seed rows a roster replace keeps: it is handed
// the roster as it stood before the replace and the seeds stored now. An
// error refuses the replace, and nothing is written.
type SeedPruner func(storedRoster []domain.Player, seeds []domain.SeedAssignment) (kept []domain.SeedAssignment, err error)

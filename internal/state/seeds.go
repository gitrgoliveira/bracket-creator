package state

import (
	"bytes"
	"encoding/csv"
	"errors"
	"fmt"
	"os"
	"sort"
	"strconv"

	"github.com/gitrgoliveira/bracket-creator/internal/domain"
	"github.com/gitrgoliveira/bracket-creator/internal/helper"
)

// ErrSeedsWithoutRoster marks a SaveSeeds call the operator can fix by
// entering participants first: a competitor list must exist to define
// seeds (operator ruling), so a NON-EMPTY seeding against a roster with no
// participants at all has nothing to attach to. An EMPTY seed set is exempt
// -- that is how an operator clears a seeding -- so this only ever fires for
// a set that actually names somebody.
var ErrSeedsWithoutRoster = errors.New("seeds: a competitor list must exist before seeds can be set")

// LoadSeeds and SaveSeeds use the PER-COMPETITION lock (not the store-wide
// `s.mu`) so they serialize against other per-comp readers/writers. In
// particular against the StartCompetition transform held by
// UpdateCompetitionChanged. Pre-fix, SaveSeeds took `s.mu.Lock()`
// (store-wide) and the StartCompetition transform took the per-comp lock,
// so the seeds drift check inside the transform (via FileMtime) had a
// race window: a concurrent SaveSeeds could land AFTER the mtime check
// but BEFORE the status commit, leaving status=Pools on disk with
// seeds.csv reflecting roster the engine never read.
//
// Switching to per-comp locking ALSO improves scalability; concurrent
// seed saves for DIFFERENT comps no longer block each other on the
// global store mutex. Same locking strategy participants.csv and
// pools.csv already use.
// LoadSeeds is LoadSeedsRaw plus the usability check, exactly as
// helper.ParseSeedsFile is helper.ReadSeedsFileRaw plus that check one layer
// down. Reading through the raw loader rather than repeating its body keeps the
// locking, the missing-file answer and any future caching in ONE place: the
// show path is the one a change here is least likely to be carried across, and
// a divergence would let a draw be built from a seeding the operator has not
// finished. Validation runs on the returned copy, outside the lock.
func (s *Store) LoadSeeds(compID string) ([]domain.SeedAssignment, error) {
	result, err := s.LoadSeedsRaw(compID)
	if err != nil {
		return nil, err
	}
	if err := domain.ValidateAssignments(result); err != nil {
		return nil, err
	}
	return result, nil
}

// LoadSeedsRaw returns the stored seed assignments WITHOUT requiring them to be
// a usable seeding, for callers that need to SHOW the operator what is on disk.
//
// LoadSeeds is the right call everywhere seeds are consumed: it refuses an
// unusable set so a draw can never be built from one. But an operator halfway
// through entering seeds has an unusable set by definition, and answering "there
// are no seeds" (or HTTP 500) when they can plainly see the ranks they typed is
// how the tool stops telling them anything. Show it, warn about it, and refuse
// to draw with it.
func (s *Store) LoadSeedsRaw(compID string) ([]domain.SeedAssignment, error) {
	if err := ValidateCompetitionID(compID); err != nil {
		return nil, err
	}
	mu := s.getCompLock(compID)
	mu.RLock()
	defer mu.RUnlock()

	result, err := helper.ReadSeedsFileRaw(s.compPath(compID, "seeds.csv"))
	if err != nil {
		if os.IsNotExist(err) {
			return []domain.SeedAssignment{}, nil
		}
		return nil, err
	}
	return result, nil
}

func (s *Store) SaveSeeds(compID string, assignments []domain.SeedAssignment) error {
	if err := ValidateCompetitionID(compID); err != nil {
		return err
	}
	mu := s.getCompLock(compID)
	mu.Lock()
	defer mu.Unlock()

	if len(assignments) > 0 {
		// Load the roster under the lock already held above: LoadParticipants
		// would try to re-acquire it and deadlock the non-reentrant mutex, so
		// this goes through the no-lock loader every other locked caller in
		// this package uses. WithSeeds:false because the merge this loader
		// would otherwise do (seeds.csv onto players) reads the very file
		// this call is about to overwrite -- irrelevant here and one fewer
		// file this write depends on.
		withZekken, _, err := s.withZekkenNameLocked(compID)
		if err != nil {
			return err
		}
		players, err := s.loadParticipantsNoLock(compID, withZekken, LoadParticipantsOpts{WithSeeds: false})
		if err != nil {
			return err
		}
		if len(players) == 0 {
			return ErrSeedsWithoutRoster
		}
		// Stamp every row with its participant's id, resolved the same way
		// every seed-row matcher resolves one (domain.RosterIndex.LookupSeed:
		// id-first, else the (name, dojo) pair). SaveSeeds is the one door
		// every seeds.csv write goes through (the participant-rename rewrite
		// in updateParticipantNoLock is the sole exception, and it only ever
		// PRESERVES an id it already read, never stamps a new one), so
		// stamping here makes it automatic for every caller: the seeding
		// panel, tournament import, and create-with-players. A row that
		// resolves to nobody -- a ghost the caller's own gate should already
		// have refused before reaching here -- is written with whatever id
		// it arrived carrying, unchanged.
		idx := domain.NewRosterIndex(players)
		for i := range assignments {
			if p, ok := idx.LookupSeed(assignments[i]); ok {
				assignments[i].ID = p.ID
			}
		}
	}

	path := s.compPath(compID, "seeds.csv")

	// Sort by rank for readability
	sort.Slice(assignments, func(i, j int) bool {
		return assignments[i].SeedRank < assignments[j].SeedRank
	})

	data, err := marshalSeedsCSV(assignments)
	if err != nil {
		return err
	}
	return s.atomicWrite(path, data, 0600)
}

// marshalSeedsCSV serialises seed assignments into RFC 4180 CSV bytes. The
// ONE seeds.csv writer, shared by SaveSeeds and the participant-rename rewrite
// in updateParticipantNoLock; those used to be two hand-copied bodies related
// only by a "mirrors" comment, which is how a new column would have reached
// one and silently vanished through the other. Policy stays with the callers
// (SaveSeeds sorts by rank; the rename rewrite preserves file order).
//
// The Dojo column exists because a seed assignment is matched to its
// participant by (name, dojo): names are not unique within a competition
// (only same-name AND same-dojo is rejected), so without the dojo a seed for
// either of two same-named players could not be resolved after a reload.
// ParseSeedsFile has always located columns by header name and read Dojo when
// present, so files written before this column load unchanged (empty dojo,
// with a single-match-by-name fallback) and files written now load in older
// builds, which simply never look the column up.
//
// encoding/csv rather than fmt.Fprintf so names containing commas / quotes
// (e.g. "Smith, John") are properly escaped; hand-formatted rows would emit
// broken CSV that ParseSeedsFile then mis-splits, silently dropping seeds.
//
// ID is APPENDED as a fourth column, not inserted alongside Rank/Name/Dojo:
// ReadSeedsFileRaw locates every column by header name, so an older build
// that has never heard of "ID" simply never looks the column up (same
// backward-compatible story the Dojo column's own doc paragraph above
// describes), and a file this build wrote is still readable by one that
// predates the column.
func marshalSeedsCSV(assignments []domain.SeedAssignment) ([]byte, error) {
	var buf bytes.Buffer
	w := csv.NewWriter(&buf)
	if err := w.Write([]string{"Rank", "Name", "Dojo", "ID"}); err != nil {
		return nil, fmt.Errorf("writing seeds CSV header: %w", err)
	}
	for _, a := range assignments {
		if err := w.Write([]string{strconv.Itoa(a.SeedRank), a.Name, a.Dojo, a.ID}); err != nil {
			return nil, fmt.Errorf("writing seeds CSV record: %w", err)
		}
	}
	w.Flush()
	if err := w.Error(); err != nil {
		return nil, fmt.Errorf("flushing seeds CSV: %w", err)
	}
	return buf.Bytes(), nil
}

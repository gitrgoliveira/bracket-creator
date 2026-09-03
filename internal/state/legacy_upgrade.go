package state

import (
	"log"

	"github.com/gitrgoliveira/bracket-creator/internal/domain"
	"github.com/gitrgoliveira/bracket-creator/internal/helper"
)

// Legacy on-disk shapes convert at a boundary, not in a standing read path
// (operator policy, 2026-08-21). WHICH boundary depends on whether the legacy
// shape can be recognised without guessing:
//
//   - seeds.csv rows without a dojo (pre-Dojo builds) convert ON READ, below.
//     Seeds columns are located by header name, so the legacy shape is
//     unambiguous, and the dojo is half of the seed's identity
//     (domain.SeedKey): a legacy row is completed from the roster only when
//     the name is unique there, exactly the fallback the matchers apply. An
//     unresolvable row (duplicate name) is left alone: inventing a dojo would
//     guess, and AssignSeeds refuses that seeding either way.
//
//   - participants.csv rows without the leading id column convert ON WRITE
//     (marshalParticipantsCSV mints ids for id-less rows on every save), and
//     DELIBERATELY NOT on read. A legacy no-id roster and a roster carrying
//     non-UUID client ids that is awaiting its deferred HasParticipantIDs
//     flip are byte-indistinguishable (the mp-p7n ambiguity; the flag is
//     omitempty, so absent reads as false). A read-side conversion built on
//     that sniff re-saved the shifted mis-parse: "race-p1, Aaron Adams,
//     Team Alpha" came back as a minted UUID with Name "Race-P1" and Dojo
//     "Aaron Adams", destroying the id every id-keyed record pointed at.
//     TestMpP7nRepro_CacheInvalidatedOnHasParticipantIDsFlip is the pin that
//     caught it; do not reintroduce a read-side roster rewrite without an
//     unambiguous discriminator.
//
// EnsureLegacyUpgraded runs BEFORE loadParticipants takes its read lock, once
// per competition per process: the conversion needs the per-comp WRITE lock,
// and writing from under the reader's RLock would race the other readers
// doing the same. Callers that FINGERPRINT files before their first load
// (StartCompetition's drift guard, via ParticipantsFingerprint below) must run
// this first, or the conversion lands between snapshot and re-check and reads
// as operator drift.
//
// Failure policy: a failed conversion is logged and NOT retried until the
// next process start (the once-map is stamped regardless). The file stays in
// its legacy shape, which every reader still parses, so degradation is "the
// fallback keeps working", never a lost read; retrying on every load would
// hammer a broken disk from the hot viewer path.
func (s *Store) EnsureLegacyUpgraded(compID string) {
	if _, done := s.legacyUpgraded.Load(compID); done {
		return
	}
	mu := s.getCompLock(compID)
	mu.Lock()
	defer mu.Unlock()
	if _, done := s.legacyUpgraded.Load(compID); done {
		return // lost the race to a concurrent reader's upgrade
	}
	if err := s.upgradeSeedDojosLocked(compID); err != nil {
		log.Printf("state: legacy seed-dojo upgrade for %s: %v", compID, err)
	}
	s.legacyUpgraded.Store(compID, struct{}{})
}

// ParticipantsFingerprint stats participants.csv and seeds.csv for compID,
// running EnsureLegacyUpgraded first so the two mtimes it returns are the
// POST-upgrade files, never a pre-upgrade snapshot that a later lazy upgrade
// (triggered by the first LoadParticipants call) would then race against and
// misreport as operator drift. This is the one chokepoint a drift guard
// should call to capture a participants/seeds baseline -- folding the
// ordering requirement in here means a future caller cannot forget it the
// way engine.StartCompetition's hand-wired EnsureLegacyUpgraded call could
// have been forgotten or reordered.
func (s *Store) ParticipantsFingerprint(compID string) (participantsMtime, seedsMtime int64) {
	s.EnsureLegacyUpgraded(compID)
	return s.FileMtime(compID, "participants.csv"), s.FileMtime(compID, "seeds.csv")
}

// upgradeSeedDojosLocked completes legacy (name-only) seeds.csv rows with the
// roster dojo where the name is unique. Caller holds the per-comp lock.
func (s *Store) upgradeSeedDojosLocked(compID string) error {
	path := s.compPath(compID, "seeds.csv")
	seeds, err := helper.ReadSeedsFileRaw(path)
	if err != nil || len(seeds) == 0 {
		return nil // missing/unreadable seeds are the consumers' error to report
	}
	needs := false
	for i := range seeds {
		if seeds[i].Dojo == "" {
			needs = true
			break
		}
	}
	if !needs {
		return nil
	}
	// Load the roster under the SAME layout flags a real load uses: with
	// withZekkenName hardcoded false, a zekken competition's rows shift one
	// column and the zekken string reads as the dojo, which this pass would
	// then write into seeds.csv as fact.
	withZekken, comp, err := s.withZekkenNameLocked(compID)
	if err != nil || comp == nil {
		return err
	}
	players, err := s.loadParticipantsNoLock(compID, withZekken, LoadParticipantsOpts{HasIDs: comp.ParticipantIDsHint()})
	if err != nil || len(players) == 0 {
		return err
	}
	// Same shared resolver every other matcher uses (domain.RosterIndex):
	// Lookup(name, "") tries the exact (name, "") key first, then falls back
	// to the unique-bare-name match. Either way the guard below only ever
	// completes a row from a NON-empty roster dojo, so a legacy row that
	// matches a roster entry whose own dojo is also blank is correctly left
	// alone (nothing to backfill).
	roster := domain.NewRosterIndex(players)
	changed := false
	for i := range seeds {
		if seeds[i].Dojo != "" {
			continue
		}
		if p, ok := roster.Lookup(seeds[i].Name, ""); ok && p.Dojo != "" {
			seeds[i].Dojo = p.Dojo
			changed = true
		}
	}
	if !changed {
		return nil
	}
	data, err := marshalSeedsCSV(seeds) // preserves the file's row order
	if err != nil {
		return err
	}
	if err := s.atomicWrite(path, data, 0600); err != nil {
		return err
	}
	// The participant caches fold seeds.csv's mtime into their key; drop them
	// outright so a same-millisecond write cannot serve the pre-upgrade merge.
	s.invalidateParticipantCaches(compID)
	return nil
}

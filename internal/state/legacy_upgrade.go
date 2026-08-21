package state

import (
	"log"

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
// upgradeLegacyOnce runs BEFORE loadParticipants takes its read lock, once
// per competition per process: the conversion needs the per-comp WRITE lock,
// and writing from under the reader's RLock would race the other readers
// doing the same. Callers that FINGERPRINT files before their first load
// (StartCompetition's drift guard) call EnsureLegacyUpgraded first, or the
// conversion lands between snapshot and re-check and reads as operator drift.
//
// Failure policy: a failed conversion is logged and NOT retried until the
// next process start (the once-map is stamped regardless). The file stays in
// its legacy shape, which every reader still parses, so degradation is "the
// fallback keeps working", never a lost read; retrying on every load would
// hammer a broken disk from the hot viewer path.

// EnsureLegacyUpgraded runs the once-per-process legacy conversion for compID
// immediately; see above for who needs to call it explicitly.
func (s *Store) EnsureLegacyUpgraded(compID string) {
	s.upgradeLegacyOnce(compID)
}

func (s *Store) upgradeLegacyOnce(compID string) {
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
	nameCount := make(map[string]int, len(players))
	dojoByName := make(map[string]string, len(players))
	for i := range players {
		nameCount[players[i].Name]++
		dojoByName[players[i].Name] = players[i].Dojo
	}
	changed := false
	for i := range seeds {
		if seeds[i].Dojo != "" {
			continue
		}
		if dojo := dojoByName[seeds[i].Name]; dojo != "" && nameCount[seeds[i].Name] == 1 {
			seeds[i].Dojo = dojo
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

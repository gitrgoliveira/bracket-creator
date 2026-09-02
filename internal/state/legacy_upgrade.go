package state

import (
	"errors"
	"fmt"
	"log"
	"os"

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
//   - config.md's retired "playoffs" format/status values, and the retired
//     playoff_match_duration_seconds key, convert ON READ (or rather, once
//     per process, exactly like the seed-dojo case) via
//     upgradeCompetitionFormatLocked below. Unlike the two shapes above, this
//     one is ALSO swept EAGERLY at store construction (see
//     SweepLegacyUpgrades, called from NewStore) rather than waiting for the
//     competition's first touch: a live tournament must not discover mid-
//     event that its bracket stage silently isn't there. See
//     upgradeCompetitionFormatLocked's own doc comment for why its failure
//     policy also diverges from the log-and-continue default below.
//
// EnsureLegacyUpgraded runs BEFORE loadParticipants takes its read lock, once
// per competition per process: the conversion needs the per-comp WRITE lock,
// and writing from under the reader's RLock would race the other readers
// doing the same. Callers that FINGERPRINT files before their first load
// (StartCompetition's drift guard, via ParticipantsFingerprint below) must run
// this first, or the conversion lands between snapshot and re-check and reads
// as operator drift.
//
// Failure policy: a failed SEED-DOJO conversion is logged and NOT retried
// until the next process start (the once-map is stamped regardless). The
// file stays in its legacy shape, which every reader still parses, so
// degradation is "the fallback keeps working", never a lost read; retrying
// on every load would hammer a broken disk from the hot viewer path. The
// format/status conversion below has a DIFFERENT, louder policy; see its own
// call site comment.
func (s *Store) EnsureLegacyUpgraded(compID string) error {
	if _, done := s.legacyUpgraded.Load(compID); done {
		return nil
	}
	mu := s.getCompLock(compID)
	mu.Lock()
	defer mu.Unlock()
	if _, done := s.legacyUpgraded.Load(compID); done {
		return nil // lost the race to a concurrent reader's upgrade
	}
	if err := s.upgradeSeedDojosLocked(compID); err != nil {
		log.Printf("state: legacy seed-dojo upgrade for %s: %v", compID, err)
	}
	// Format/status/duration conversion (playoffs -> knockout; bc-terminology
	// commit 1). UNLIKE the seed-dojo call above, this error is RETURNED
	// rather than merely logged -- a deliberate divergence from this
	// function's own default policy (operator ruling). The seed-dojo
	// fallback is safe because a legacy (no-dojo) seed row still parses and
	// functions; a legacy "playoffs" Format/Status value also still parses
	// (both are bare strings with no load-time validator) but silently
	// MISBEHAVES instead: IsKnockoutEnabled matches none of its switch arms
	// and returns false, so the competition quietly forgets it has a
	// knockout stage. Silent wrong behaviour is worse than a loud failure.
	//
	// This propagates all the way up in both directions that matter: the
	// startup sweep (SweepLegacyUpgrades, invoked from NewStore) turns it
	// into a hard startup failure -- the real defence, since it runs before
	// the app accepts any traffic -- and the lazy hot-path caller
	// (loadParticipants) also refuses the read rather than silently serving
	// a mis-classified competition. In ordinary operation the sweep already
	// converts every on-disk competition before this lazy path can ever run,
	// so reaching this branch here at all means the sweep did not cover this
	// competition (e.g. it raced a concurrent write). Do not "fix" this back
	// to a bare log.Printf-and-continue to match the seed-dojo policy above;
	// that policy's safety argument does not hold for this conversion.
	if err := s.upgradeCompetitionFormatLocked(compID); err != nil {
		// Do NOT stamp the once-map on this path. Stamping here is exactly the
		// silent mis-classification this function exists to prevent: a later
		// call would see `done`, return nil, and report success while
		// config.md is still unconverted. Leaving it unstamped means every
		// subsequent call retries the conversion and keeps failing loudly
		// until the underlying problem (e.g. a read-only competition
		// directory) is fixed. This is the opposite of the seed-dojo policy
		// above on purpose; see the comment above this call for why.
		return fmt.Errorf("legacy format/status upgrade for %s: %w", compID, err)
	}
	s.legacyUpgraded.Store(compID, struct{}{})
	return nil
}

// SweepLegacyUpgrades converts every known competition's on-disk legacy
// shapes EAGERLY, once, at store construction (see NewStore) rather than
// waiting for each competition's first touch. It reuses EnsureLegacyUpgraded
// -- the same per-competition entry point the lazy read paths call -- over
// every id ListCompetitions reports, rather than a second conversion path.
//
// A live tournament must not discover mid-event that its knockout stage
// silently isn't there (IsKnockoutEnabled misses every switch arm on an
// unconverted "playoffs" value), so this returns a combined error naming
// every competition whose conversion failed instead of stopping at the
// first one: a single startup failure message should tell the operator the
// full extent of the problem, not make them fix-and-restart repeatedly to
// discover it one competition at a time.
func (s *Store) SweepLegacyUpgrades() error {
	ids, err := s.ListCompetitions()
	if err != nil {
		return fmt.Errorf("legacy upgrade sweep: list competitions: %w", err)
	}
	var errs []error
	for _, id := range ids {
		if err := s.EnsureLegacyUpgraded(id); err != nil {
			errs = append(errs, err)
		}
	}
	return errors.Join(errs...)
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
//
// Returns a non-nil error only when the format/status upgrade fails (see
// EnsureLegacyUpgraded); callers must check it rather than fingerprinting a
// competition whose on-disk shape may still be the unconverted legacy one.
func (s *Store) ParticipantsFingerprint(compID string) (participantsMtime, seedsMtime int64, err error) {
	if err := s.EnsureLegacyUpgraded(compID); err != nil {
		return 0, 0, err
	}
	return s.FileMtime(compID, "participants.csv"), s.FileMtime(compID, "seeds.csv"), nil
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

// legacyKnockoutDuration captures the pre-rename playoff_match_duration_seconds
// YAML key on its own, minimal struct. This key predates bc-terminology
// commit 1: the Competition field that used to carry it (PlayoffMatchDurationSeconds)
// is now tagged knockout_match_duration_seconds, so a value written under the
// old key is invisible to the ordinary parseCompetitionFile path once the
// rename lands -- yaml.Unmarshal simply drops an unrecognised key. Without
// this recovery, every competition configured since the seconds field was
// introduced (but before this rename) would silently lose its configured
// knockout match duration. The whole-MINUTE legacy field
// (KnockoutMatchDuration) needs no such recovery: its yaml tag intentionally
// did not change (see models.go), so ApplyCompetitionDefaults' existing fold
// already picks it up on every ordinary load.
type legacyKnockoutDuration struct {
	PlayoffMatchDurationSeconds int `yaml:"playoff_match_duration_seconds"`
}

// upgradeCompetitionFormatLocked rewrites a competition's config.md off the
// retired "playoffs" wire values onto their "knockout" equivalents (owner
// ruling: clean break on the wire, no permanent dual-accept read path).
// Caller holds the per-comp lock (EnsureLegacyUpgraded).
//
// This function, and no other standing reader, is allowed to recognise the
// literal "playoffs" string -- that is what makes this a clean break instead
// of a permanent dual-accept.
//
// Three independent legacy shapes, all folded in one pass so a competition
// converges in a single rewrite rather than three:
//  1. format: playoffs -> knockout
//  2. status: playoffs -> knockout
//  3. the retired playoff_match_duration_seconds key -> knockout_match_duration_seconds
//     (see legacyKnockoutDuration above for why this one needs raw-YAML
//     recovery while the whole-minute legacy field does not).
func (s *Store) upgradeCompetitionFormatLocked(compID string) error {
	path := s.compPath(compID, "config.md")
	raw, err := os.ReadFile(path) // #nosec G304; path built by compPath which calls filepath.Clean
	if err != nil {
		if os.IsNotExist(err) {
			return nil // no config yet; nothing to convert
		}
		return err
	}

	var legacy legacyKnockoutDuration
	if err := parseFrontMatter(raw, &legacy); err != nil {
		return err
	}

	comp, err := s.loadCompetitionLocked(compID)
	if err != nil || comp == nil {
		return err
	}

	const legacyPlayoffsValue = "playoffs"
	changed := false
	if comp.Format == legacyPlayoffsValue {
		comp.Format = CompFormatKnockout
		changed = true
	}
	if comp.Status == CompetitionStatus(legacyPlayoffsValue) {
		comp.Status = CompStatusKnockout
		changed = true
	}
	if comp.KnockoutMatchDurationSeconds == 0 && legacy.PlayoffMatchDurationSeconds > 0 {
		comp.KnockoutMatchDurationSeconds = ClampMatchSeconds(legacy.PlayoffMatchDurationSeconds)
		changed = true
	}
	if !changed {
		return nil
	}
	return s.saveCompetitionLocked(comp, s.directWrite)
}

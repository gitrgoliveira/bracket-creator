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
//   - seeds.csv rows missing a dojo (pre-Dojo builds) or a participant id
//     (pre-id-column builds, bc-sdid) convert ON READ, below, in the SAME
//     pass. Seeds columns are located by header name, so either legacy shape
//     is unambiguous. The dojo is completed from the roster only when the
//     name is unique there, exactly the fallback the matchers apply; an
//     unresolvable row (duplicate name) is left alone -- inventing a dojo
//     would guess, and every seed-row matcher refuses that seeding either
//     way. The id is then stamped from whichever roster entry the row (now
//     dojo-complete, if it needed to be) resolves to: an exact (name, dojo)
//     match when the row carries a dojo, or -- gated EXPLICITLY on
//     domain.RosterIndex.NameCount rather than left to RosterIndex.Lookup's
//     own dojo=="" branch, for the identical reason the pools.csv bullet
//     below gives -- the same unique-bare-name fallback when it does not. A
//     row neither resolves keeps an empty id; the (name, dojo) fallback
//     every matcher shares keeps serving it.
//
//   - pools.csv rows with an empty id (pre-append-column builds) convert ON
//     READ, below (bc-pnum). The id is column 8 of an explicit, positional
//     layout, so a legacy row is unambiguous by width alone. Completed from
//     the roster by an EXACT (name, dojo) match when the row's own dojo is
//     non-empty -- the pair the repo refuses as a duplicate on every roster
//     save (helper.CheckDuplicateEntriesByNameDojo,
//     state.saveParticipantsNoLock's floor), so that match can never be
//     ambiguous. A row whose OWN dojo is blank (tolerated on load, unlike
//     save -- see the participants.csv bullet below) instead takes the SAME
//     unique-bare-name fallback the seeds.csv and pool-matches.csv upgrades
//     below use, gated EXPLICITLY on domain.RosterIndex.NameCount rather
//     than left to RosterIndex.Lookup's own dojo=="" branch -- see the
//     pool-matches.csv bullet below for why that branch alone is not safe: a
//     roster entry whose OWN dojo genuinely IS blank would otherwise score
//     an exact hit on Lookup's first branch (SeedKey(name, "") == "name|")
//     and resolve BEFORE the uniqueness fallback ever runs, silently
//     attributing an unknown-dojo row to a specific blank-dojo competitor by
//     coincidence when a second, non-blank-dojo namesake also exists. It is
//     not literally an exact (name, dojo) pair, so do not describe it as
//     one. A row that still does not match any current roster entry is left
//     alone; helper.PoolsMissingParticipantIDsMessage keeps naming it.
//
//   - pool-matches.csv rows with an empty SideAID/SideBID/WinnerID
//     (pre-append-column builds) convert ON READ, below (bc-pnum). Unlike
//     pools.csv, a match row names a side by BARE name only (no dojo
//     column): "" here is not a recorded value, it is the absence of one, so
//     resolving it needs the unique-bare-name fallback the seeds.csv upgrade
//     above already uses, gated EXPLICITLY on domain.RosterIndex.NameCount
//     rather than left to RosterIndex.Lookup's own dojo=="" branch -- a
//     roster entry whose OWN dojo genuinely IS blank would otherwise score an
//     exact hit on Lookup's first branch (SeedKey(name, "") == "name|") and
//     resolve BEFORE the uniqueness fallback ever runs, silently attributing
//     an unknown-dojo side to a specific blank-dojo competitor by
//     coincidence when a second, non-blank-dojo namesake also exists. Two or
//     more roster entries sharing the exact name (an individual competition,
//     since team names must stay unique) leave that side's id alone -- the
//     SAME conservative miss the seeds.csv upgrade already accepts for a
//     duplicate name, and engine.PoolMatchesMissingSideIDsMessage keeps
//     naming the row. WinnerID is derived from the ROW'S OWN just-resolved
//     side NAMES, routed through domain.AttributeWinnerSide (the one owner
//     of "which side won"; internal/state/legacy_hantei.go already calls it
//     from this package), and ONLY when SideA != SideB: two competitors can
//     legally share a display name from different dojos, and a row where
//     both sides hold that shared name can never be told apart by name
//     alone -- crediting side A regardless would repeat the exact mechanism
//     internal/engine/bc_idfx_test.go already fixed once for the runtime
//     resolver. A winner naming neither side, or a row whose sides are not
//     distinguishable, stays empty.
//
//   - bracket.json rows with an empty SideAID/SideBID/WinnerID convert ON
//     READ, below (bc-brid). Two resolution paths, tried in order, mirroring
//     the two ways a bracket side is written: (a) for a STANDALONE knockout
//     (Bracket.DrawOrder non-empty -- a mixed pool-fed bracket never carries
//     it), Bracket.StampRoundZeroSideIDsFromDrawOrder resolves round-0
//     POSITIONALLY: DrawOrder[k] is the k-th non-empty round-0 leaf
//     (helper.CreateBalancedTree/SlotArray are order-preserving), so this is
//     exact even for a name shared by two round-0 competitors -- it never
//     compares names at all. (b) every side (any round, plus the bronze
//     sibling) that path did not reach -- every round of a mixed bracket, and
//     any round-0 side a corrupted/short DrawOrder missed -- falls back to
//     the SAME unique-bare-name match every other legacy upgrade in this file
//     uses, gated on domain.RosterIndex.NameCount rather than
//     RosterIndex.Lookup's own dojo=="" branch, for the identical reason the
//     pools.csv/pool-matches.csv upgrades above give: a roster entry whose
//     OWN dojo genuinely IS blank would otherwise score an exact hit on
//     Lookup's first branch and resolve before the uniqueness fallback ever
//     runs. A placeholder ("Winner of r1-m3", "Pool A-1st",
//     helper.IsReservedParticipantName) is never a resolution target -- it is
//     not a competitor's name at all. WinnerID is then derived from the
//     row's OWN just-resolved SideA/SideB NAMES via domain.AttributeWinnerSide
//     (the pool-matches upgrade's identical rule; a row is left alone when
//     SideA == SideB and non-empty, the same legally-shared-name guard). A
//     row neither path resolves (two or more current competitors share that
//     exact name) is left alone; the fields stay empty and the identity-
//     critical readers keep their name-based fallback for it.
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
// EnsureLegacyUpgraded runs BEFORE loadParticipants/LoadPools/LoadPoolMatches/
// LoadBracket take their read lock, once per competition per process: the conversion
// needs the per-comp WRITE lock, and writing from under the reader's RLock
// would race the other readers doing the same. Callers that FINGERPRINT
// files before their first load (StartCompetition's drift guard, via
// ParticipantsFingerprint below) must run this first, or the conversion
// lands between snapshot and re-check and reads as operator drift.
//
// The once-map is re-armed by a participants.csv WRITE (saveParticipantsNoLock,
// participants.go), not just cleared by DeleteCompetition: a genuinely legacy
// competition's roster predates the id column too, so the FIRST read here has
// nothing to copy ids FROM and repairs nothing, permanently marking the
// competition done for the rest of the process even though the very next
// roster save mints the ids this repair needs. Re-arming on save means the
// NEXT read after that save retries the repair with a roster that can now
// resolve it, without requiring a restart.
//
// ONLY the five PUBLIC, caller-does-not-already-hold-the-lock entry points
// (loadParticipants, Store.LoadPools, Store.LoadPoolMatches, Store.LoadBracket,
// and ParticipantsFingerprint below, which calls it directly rather than
// through one of the other four) call this. The *Locked siblings
// (loadPoolsLocked, LoadPoolMatchesLocked, loadBracketLocked) and every
// storeTx method (including storeTx.LoadBracket) are called by something
// that ALREADY holds the per-comp lock (typically WithTransaction), so
// calling this from any of them would try to re-acquire a non-reentrant
// mutex and deadlock. Do not add a call here from inside that set.
//
// Failure policy: a failed conversion is logged and NOT retried until the
// next process start OR the next participants.csv write, whichever comes
// first (the once-map is stamped regardless of the log). The file stays in
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
	// One roster load shared by all four steps below (bc-pnum review): built
	// LAZILY on first actual use, so a competition needing no repair at all
	// never parses participants.csv, and loaded/indexed at most ONCE rather
	// than once per step. Safe to share across steps despite the seeds step's
	// own invalidateParticipantCaches call: that invalidates the STORE's
	// file cache (so the NEXT independent Store.LoadParticipants call
	// re-parses), but a seed-dojo backfill never touches participants.csv
	// itself, so the roster this instance already loaded stays accurate for
	// the pools/pool-matches/bracket steps that follow it in the same call.
	roster := &legacyUpgradeRoster{store: s, compID: compID}
	if err := s.upgradeSeedRowsLocked(compID, roster); err != nil {
		log.Printf("state: legacy seed-row upgrade for %s: %v", compID, err)
	}
	if err := s.upgradePoolParticipantIDsLocked(compID, roster); err != nil {
		log.Printf("state: legacy pool-participant-id upgrade for %s: %v", compID, err)
	}
	if err := s.upgradePoolMatchSideIDsLocked(compID, roster); err != nil {
		log.Printf("state: legacy pool-match-side-id upgrade for %s: %v", compID, err)
	}
	if err := s.upgradeBracketSideIDsLocked(compID, roster); err != nil {
		log.Printf("state: legacy bracket-side-id upgrade for %s: %v", compID, err)
	}
	s.legacyUpgraded.Store(compID, struct{}{})
}

// legacyUpgradeRoster lazily loads and indexes compID's participants.csv for
// EnsureLegacyUpgraded's four steps. Caller MUST already hold the per-comp
// write lock (the same requirement every method below carries); this type
// adds no locking of its own because it is never shared beyond a single
// EnsureLegacyUpgraded call.
type legacyUpgradeRoster struct {
	store   *Store
	compID  string
	loaded  bool
	index   *domain.RosterIndex
	loadErr error
}

// get returns the roster index, loading it on the first call and caching
// the result (including a load failure) for every subsequent call in this
// same EnsureLegacyUpgraded invocation. A nil index with a nil error means
// "no roster to resolve against" (missing/empty participants.csv, or no
// competition config), which every caller below treats as "nothing to do",
// exactly as the pre-extraction per-step loads did.
func (r *legacyUpgradeRoster) get() (*domain.RosterIndex, error) {
	if r.loaded {
		return r.index, r.loadErr
	}
	r.loaded = true
	withZekken, comp, err := r.store.withZekkenNameLocked(r.compID)
	if err != nil || comp == nil {
		r.loadErr = err
		return nil, r.loadErr
	}
	players, err := r.store.loadParticipantsNoLock(r.compID, withZekken, LoadParticipantsOpts{HasIDs: comp.ParticipantIDsHint()})
	if err != nil {
		r.loadErr = err
		return nil, r.loadErr
	}
	if len(players) > 0 {
		r.index = domain.NewRosterIndex(players)
	}
	return r.index, nil
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

// upgradeSeedRowsLocked completes legacy seeds.csv rows in ONE pass: a
// missing dojo (name-only, pre-Dojo builds) is backfilled where the name is
// unique in the roster, and a missing participant id (pre-id-column builds,
// bc-sdid) is then stamped from whichever roster entry the row -- now
// dojo-complete, if it needed to be -- resolves to. Caller holds the
// per-comp lock. See the header comment above for the full rationale.
func (s *Store) upgradeSeedRowsLocked(compID string, roster *legacyUpgradeRoster) error {
	path := s.compPath(compID, "seeds.csv")
	seeds, err := helper.ReadSeedsFileRaw(path)
	if err != nil || len(seeds) == 0 {
		return nil // missing/unreadable seeds are the consumers' error to report
	}
	needs := false
	for i := range seeds {
		if seeds[i].Dojo == "" || helper.ParticipantIDMissing(seeds[i].ID) {
			needs = true
			break
		}
	}
	if !needs {
		return nil
	}
	// Loaded under the SAME layout flags a real load uses: with withZekkenName
	// hardcoded false, a zekken competition's rows shift one column and the
	// zekken string reads as the dojo, which this pass would then write into
	// seeds.csv as fact.
	idx, err := roster.get()
	if err != nil || idx == nil {
		return err
	}
	changed := false
	for i := range seeds {
		row := &seeds[i]
		if row.Dojo == "" {
			// NameCount(name) == 1 is checked EXPLICITLY before Lookup ever
			// runs, rather than relying on Lookup's own dojo=="" fallback
			// gate: a roster entry whose OWN dojo genuinely IS blank would
			// otherwise score an exact hit on Lookup's first branch
			// (SeedKey(name, "") == "name|") and resolve BEFORE the
			// uniqueness check runs, silently attributing this row's id to
			// that specific blank-dojo competitor by coincidence even when a
			// second, non-blank-dojo namesake also exists (the same trap the
			// pools.csv/pool-matches.csv/bracket.json upgrades below guard
			// against). A row that fails this check is left alone entirely --
			// dojo AND id both stay as read.
			if idx.NameCount(row.Name) != 1 {
				continue
			}
			p, ok := idx.Lookup(row.Name, "")
			if !ok {
				continue
			}
			if p.Dojo != "" {
				row.Dojo = p.Dojo
				changed = true
			}
			if helper.ParticipantIDMissing(row.ID) && p.ID != "" {
				row.ID = p.ID
				changed = true
			}
			continue
		}
		// The row's own dojo is non-empty: an exact (name, dojo) pair, which
		// the roster refuses to save twice, so this can never be ambiguous.
		if helper.ParticipantIDMissing(row.ID) {
			if p, ok := idx.Lookup(row.Name, row.Dojo); ok && p.ID != "" {
				row.ID = p.ID
				changed = true
			}
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

// poolMemberMissingID reports whether a pools.csv row still needs an id: a
// named player with no id (blank or whitespace-only, helper.ParticipantIDMissing
// -- the ONE predicate every missing-id notice and this repair now share, so
// a whitespace-only id can no longer be named by the notice while never
// being attempted here).
func poolMemberMissingID(p *helper.Player) bool {
	return p.Name != "" && helper.ParticipantIDMissing(p.ID)
}

// poolsNeedIDRepair reports whether any player in pools is missing an id.
// A named function reads better than an inline flag walked to the end with
// a double break (one per nested loop level), and matches the shape of its
// sibling, MatchResult.MissingSideOrWinnerID (models.go), which
// upgradePoolMatchSideIDsLocked's own needs-scan below calls the same way.
func poolsNeedIDRepair(pools []helper.Pool) bool {
	for i := range pools {
		for j := range pools[i].Players {
			if poolMemberMissingID(&pools[i].Players[j]) {
				return true
			}
		}
	}
	return false
}

// upgradePoolParticipantIDsLocked completes legacy (no-id) pools.csv rows by
// resolving each member against the roster on an exact (name, dojo) match,
// or the unique-bare-name fallback when the row's own dojo is blank -- see
// the header comment above for why neither path can resolve ambiguously.
// Caller holds the per-comp lock.
//
// Parses pools.csv directly (parsePoolsFile) rather than going through
// loadPoolsLocked: that wrapper is exactly parsePoolsFile followed by a
// defensive deep copy, and the copy exists to protect a caller that shares
// the result with something else. This repair owns the freshly parsed slice
// exclusively, mutates it in place, and either discards it (nothing to fix)
// or hands it straight to savePoolsLocked, so the copy would be pure
// overhead nobody ever reads.
func (s *Store) upgradePoolParticipantIDsLocked(compID string, roster *legacyUpgradeRoster) error {
	path := s.compPath(compID, "pools.csv")
	parsed, err := parsePoolsFile(path)
	if err != nil {
		return nil // missing/unreadable pools are the consumers' error to report
	}
	pools, _ := parsed.([]helper.Pool)
	if len(pools) == 0 {
		return nil
	}
	if !poolsNeedIDRepair(pools) {
		return nil
	}
	idx, err := roster.get()
	if err != nil || idx == nil {
		return err
	}
	changed := false
	for i := range pools {
		for j := range pools[i].Players {
			p := &pools[i].Players[j]
			if !poolMemberMissingID(p) {
				continue
			}
			// A blank OWN dojo is checked for roster-wide name uniqueness
			// EXPLICITLY, before Lookup ever runs -- the same guard the
			// pool-matches.csv upgrade below applies via NameCount, and for
			// the same reason: Lookup(name, "") tries the exact (name, "")
			// key FIRST, so a roster entry whose own dojo genuinely IS blank
			// would score an exact hit on that first branch and resolve
			// BEFORE the uniqueness fallback ever runs, silently picking
			// that specific blank-dojo competitor by coincidence even when a
			// second, non-blank-dojo namesake of the same name also exists.
			// A non-blank row dojo skips this guard: it pins an exact
			// (name, dojo) pair, and the roster refuses to SAVE two rows
			// sharing one. Note the asymmetry this file keeps running into:
			// loading is deliberately tolerant where saving is not, so a
			// hand-edited participants.csv carrying the pair twice would
			// collapse to whichever row NewRosterIndex saw last. Unreachable
			// through the app, which refuses the save that would mint the
			// second id, so it is left alone rather than guarded.
			if p.Dojo == "" && idx.NameCount(p.Name) != 1 {
				continue
			}
			// Lookup(name, dojo) tries the exact (name, dojo) key first and,
			// only when THIS row's own dojo is blank, falls back to the
			// unique-bare-name match -- the same fallback the
			// seeds.csv/pool-matches.csv upgrades use. The guard above has
			// already ruled out the blank-dojo ambiguity that fallback branch
			// cannot see for itself.
			if rp, ok := idx.Lookup(p.Name, p.Dojo); ok && !helper.ParticipantIDMissing(rp.ID) {
				p.ID = rp.ID
				changed = true
			}
		}
	}
	if !changed {
		return nil
	}
	return s.savePoolsLocked(compID, pools)
}

// upgradePoolMatchSideIDsLocked completes legacy (no-id) pool-matches.csv
// rows: SideAID/SideBID are stamped when the side's name is unique across
// the whole roster, and WinnerID is then derived from the row's OWN
// just-resolved sides. See the header comment above for the full rationale.
// Caller holds the per-comp lock.
//
// Parses pool-matches.csv directly (parsePoolMatchesFile) rather than going
// through LoadPoolMatchesLocked, for the same reason
// upgradePoolParticipantIDsLocked calls parsePoolsFile directly: that
// wrapper is parse-then-deep-copy, and the copy is pure overhead when this
// repair owns the parsed slice exclusively and either discards it or hands
// it straight to savePoolMatchesLocked.
func (s *Store) upgradePoolMatchSideIDsLocked(compID string, roster *legacyUpgradeRoster) error {
	path := s.compPath(compID, "pool-matches.csv")
	parsed, err := parsePoolMatchesFile(path)
	if err != nil {
		return nil // missing/unreadable pool matches are the consumers' error to report
	}
	matches, _ := parsed.([]MatchResult)
	if len(matches) == 0 {
		return nil
	}
	needs := false
	for i := range matches {
		if matches[i].MissingSideOrWinnerID() {
			needs = true
			break
		}
	}
	if !needs {
		return nil
	}
	idx, err := roster.get()
	if err != nil || idx == nil {
		return err
	}
	changed := false
	for i := range matches {
		m := &matches[i]
		// Leave the whole row alone when both sides carry the same non-empty
		// name: even a UNIQUE name (NameCount == 1) resolves both SideAID
		// and SideBID to that one competitor, and unlike before this repair
		// existed, that no longer contributes nothing to standings -- the
		// row is now stamped with a single competitor playing both sides of
		// their own match, double-counting them. A row with two blank sides
		// is not this case (nothing to conflate) and falls through
		// unaffected.
		if m.SideA != "" && m.SideA == m.SideB {
			continue
		}
		// NameCount(name) == 1 is checked EXPLICITLY before Lookup, rather
		// than relying on Lookup's own dojo=="" fallback gate: a
		// pool-matches.csv side has no dojo column at all, so the ""
		// passed here is not a recorded value, it is the absence of one. A
		// roster entry whose OWN dojo genuinely IS blank would otherwise
		// score an exact hit on Lookup's FIRST branch
		// (SeedKey(name, "") == "name|"), bypassing the uniqueness check
		// Lookup's fallback would apply, and resolve this side to that
		// specific blank-dojo competitor by coincidence even when a second,
		// non-blank-dojo namesake also exists on the roster. Blank dojos
		// are refused on save but tolerated on load (deliberately, so a
		// legacy roster can be repaired rather than rejected outright), so
		// a legacy roster reaching this code may carry one.
		if m.SideA != "" && m.SideAID == "" && idx.NameCount(m.SideA) == 1 {
			if p, ok := idx.Lookup(m.SideA, ""); ok && p.ID != "" {
				m.SideAID = p.ID
				changed = true
			}
		}
		if m.SideB != "" && m.SideBID == "" && idx.NameCount(m.SideB) == 1 {
			if p, ok := idx.Lookup(m.SideB, ""); ok && p.ID != "" {
				m.SideBID = p.ID
				changed = true
			}
		}
		// WinnerID comes from the ROW'S OWN just-resolved side NAMES, routed
		// through domain.AttributeWinnerSide (the one owner of "which side
		// won"; internal/state/legacy_hantei.go already calls it from this
		// package) rather than a hand-rolled Winner==SideA/SideB comparison.
		// Runs after the SideA/SideB stamps above so a row resolved in this
		// same pass is picked up too.
		//
		// Gated on m.SideA != m.SideB: two competitors sharing a display
		// name from different dojos is legal data, and a row where BOTH
		// sides hold that shared name can never be told apart by name
		// alone. Comparing Winner against SideA first (as a plain switch,
		// or the old hand-rolled if/else-if, would do) always credits side
		// A regardless of which competitor actually won -- the exact
		// mechanism internal/engine/bc_idfx_test.go already fixed once for
		// the runtime resolver (resolveWinnerSide). Skipping derivation
		// outright when the sides are indistinguishable is the only safe
		// choice here; the row keeps its operator-facing notice instead.
		// The whole-row skip above already sends every m.SideA == m.SideB
		// (non-empty) row past this point without stamping either side id,
		// so this comparison can no longer see a true SideA == SideB case in
		// practice; it stays as a direct, self-contained guard rather than a
		// derived invariant a future edit to the skip above could silently
		// invalidate.
		if m.Winner != "" && m.WinnerID == "" && m.SideA != m.SideB {
			switch domain.AttributeWinnerSide(domain.WinnerAttribution{Winner: m.Winner, SideA: m.SideA, SideB: m.SideB}) {
			case domain.MatchSideA:
				if m.SideAID != "" {
					m.WinnerID = m.SideAID
					changed = true
				}
			case domain.MatchSideB:
				if m.SideBID != "" {
					m.WinnerID = m.SideBID
					changed = true
				}
			}
		}
	}
	if !changed {
		return nil
	}
	return s.savePoolMatchesLocked(compID, matches, s.directWrite)
}

// bracketSideNeedsIDRepair reports whether m still needs a side or winner id:
// a resolved competitor's SideA/SideB (helper.IsReservedParticipantName
// excludes a bye, a "Winner of ..." feeder, and an unseeded pool placeholder,
// none of which are ever id-stamped by design) with no id, or a named Winner
// with no WinnerID. The bracket twin of MatchResult.MissingSideOrWinnerID
// (models.go); BracketMatch has no such method of its own because the
// placeholder exclusion has no pool-matches.csv equivalent (a pool row's
// SideA/SideB are always resolved competitors, never a bracket-style
// forward reference).
func bracketSideNeedsIDRepair(m *BracketMatch) bool {
	return (m.SideA != "" && !helper.IsReservedParticipantName(m.SideA) && m.SideAID == "") ||
		(m.SideB != "" && !helper.IsReservedParticipantName(m.SideB) && m.SideBID == "") ||
		(m.Winner != "" && m.WinnerID == "")
}

// bracketNeedsIDRepair reports whether any match in b (every round, plus the
// bronze sibling) still needs a side or winner id.
func bracketNeedsIDRepair(b *Bracket) bool {
	for i := range b.Rounds {
		for j := range b.Rounds[i] {
			if bracketSideNeedsIDRepair(&b.Rounds[i][j]) {
				return true
			}
		}
	}
	return b.ThirdPlaceMatch != nil && bracketSideNeedsIDRepair(b.ThirdPlaceMatch)
}

// upgradeBracketSideIDsLocked completes a legacy (no-id) bracket.json's
// SideAID/SideBID/WinnerID. See the header comment above for the full
// resolution order (DrawOrder positionally for round 0, then the
// unique-bare-name fallback everywhere else, then WinnerID derived from each
// row's own resolved sides). Caller holds the per-comp lock.
//
// Parses bracket.json directly (parseBracketFile) rather than going through
// loadBracketLocked, for the same reason the pools/pool-matches upgrades
// above parse directly: that wrapper is parse-then-deep-copy
// (state.copyBracket), and the copy is pure overhead when this repair owns
// the parsed value exclusively and either discards it or hands it straight
// to saveBracketLocked.
func (s *Store) upgradeBracketSideIDsLocked(compID string, roster *legacyUpgradeRoster) error {
	path := s.compPath(compID, "bracket.json")
	parsed, err := parseBracketFile(path)
	if err != nil {
		return nil // missing/unreadable bracket is the consumers' error to report
	}
	bracket, _ := parsed.(*Bracket)
	if bracket == nil || len(bracket.Rounds) == 0 {
		return nil
	}
	if !bracketNeedsIDRepair(bracket) {
		return nil
	}

	// (a) Round 0, positionally, from DrawOrder -- exact even for a
	// duplicated name, and the ONLY resolver here that never compares names.
	// No-op (returns false) for a mixed bracket, which carries no DrawOrder.
	changed := bracket.StampRoundZeroSideIDsFromDrawOrder()

	// (b) Everywhere DrawOrder didn't reach: the unique-bare-name fallback,
	// gated on idx.NameCount EXPLICITLY before Lookup for the same reason
	// documented on the pools.csv/pool-matches.csv upgrades above.
	idx, err := roster.get()
	if err != nil {
		return err
	}
	if idx != nil {
		resolveSide := func(m *BracketMatch) {
			if m.SideA != "" && m.SideAID == "" && !helper.IsReservedParticipantName(m.SideA) && idx.NameCount(m.SideA) == 1 {
				if p, ok := idx.Lookup(m.SideA, ""); ok && p.ID != "" {
					m.SideAID = p.ID
					changed = true
				}
			}
			if m.SideB != "" && m.SideBID == "" && !helper.IsReservedParticipantName(m.SideB) && idx.NameCount(m.SideB) == 1 {
				if p, ok := idx.Lookup(m.SideB, ""); ok && p.ID != "" {
					m.SideBID = p.ID
					changed = true
				}
			}
		}
		for i := range bracket.Rounds {
			for j := range bracket.Rounds[i] {
				resolveSide(&bracket.Rounds[i][j])
			}
		}
		if bracket.ThirdPlaceMatch != nil {
			resolveSide(bracket.ThirdPlaceMatch)
		}
	}

	// (c) WinnerID from the row's OWN just-resolved side NAMES
	// (domain.AttributeWinnerSide's name path -- the row carries no
	// WinnerID/SideAID/SideBID triple to compare by id at this point, only
	// the two names), gated on SideA != SideB exactly like the
	// pool-matches.csv upgrade: two competitors can legally share a display
	// name, and a row where both sides hold it can never be told apart by
	// name alone.
	deriveWinner := func(m *BracketMatch) {
		if m.Winner == "" || m.WinnerID != "" {
			return
		}
		if m.SideA != "" && m.SideA == m.SideB {
			return
		}
		switch domain.AttributeWinnerSide(domain.WinnerAttribution{Winner: m.Winner, SideA: m.SideA, SideB: m.SideB}) {
		case domain.MatchSideA:
			if m.SideAID != "" {
				m.WinnerID = m.SideAID
				changed = true
			}
		case domain.MatchSideB:
			if m.SideBID != "" {
				m.WinnerID = m.SideBID
				changed = true
			}
		}
	}
	for i := range bracket.Rounds {
		for j := range bracket.Rounds[i] {
			deriveWinner(&bracket.Rounds[i][j])
		}
	}
	if bracket.ThirdPlaceMatch != nil {
		deriveWinner(bracket.ThirdPlaceMatch)
	}

	if !changed {
		return nil
	}
	return s.saveBracketLocked(compID, bracket, s.directWrite)
}

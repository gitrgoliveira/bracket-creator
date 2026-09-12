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
//   - a team's Player.Metadata (its ordered member-name list, sharing that
//     array ambiguously with an individual's dan grade) converts to
//     squads.yaml ON READ *and* ON WRITE (bc-tmid), below
//     (upgradeSquadsFromMetadataLocked). Unlike every upgrade above, this
//     one is not resolving a foreign id against the roster; a team's own
//     Metadata needs no lookup, only its OWN already-resolved participant
//     id, so it migrates on the FIRST write too (from inside
//     saveParticipantsNoLock, before the incoming payload is marshaled),
//     not only on the next load -- see that call site's own comment for why
//     a load-only trigger would still lose data. A team with no squad
//     entry at all gets one from its current Metadata, in array order; a
//     team that already has a squad entry is left untouched (never
//     re-folded); an individual competitor's Metadata (Kind != "team" and
//     TeamSize == 0) is never read as a member list. A row with no
//     participant id yet is left alone, the same residual miss every
//     upgrade above accepts, resolved once the roster itself gains one.
//
//   - a team's lineups.yaml positions (occupied Positions entries with no
//     MemberIDs counterpart) convert ON READ, below (bc-tmid pass 2),
//     AFTER the squads.yaml migration above so a team migrated in the SAME
//     pass is still resolvable. Unlike every upgrade above this one is not
//     resolving against the roster at all, but against the TEAM'S OWN
//     squad (squads.yaml), and needs none of the NameCount-gated
//     uniqueness dance those upgrades carry: two members of ONE team
//     sharing a name is already impossible (bc-tmdup, enforced on every
//     squad mutation), so an exact name match inside a single team's squad
//     can never be ambiguous. A position whose name matches no squad
//     member -- a genuinely unresolvable row, or a team with no squad
//     recorded at all -- is left alone; every id-aware reader falls back
//     to the name for exactly that slot.
//
//   - pool-matches.csv / bracket.json SUB-BOUT rows (SubMatchResult's
//     SideAMemberID/SideBMemberID/WinnerMemberID) convert in the SAME pass
//     as the match-level upgrades above (bc-tmid pass 2), an EXTENSION of
//     upgradePoolMatchSideIDsLocked / upgradeBracketSideIDsLocked rather
//     than a third read of either file (mirroring how the seeds.csv dojo
//     and id repairs share one pass instead of two). A sub-bout's SideA/
//     SideB are resolved against the TWO TEAMS' OWN squads -- side A
//     against squads[m.SideAID], side B against squads[m.SideBID] -- using
//     the match's OWN already-resolved SideAID/SideBID from the step just
//     above, so this can only repair a row whose parent match id-resolved
//     first (a match that could not be, e.g. an ambiguous shared name,
//     leaves its sub-bouts alone too). WinnerMemberID is then derived from
//     the sub-bout's OWN just-resolved side member ids via
//     SubMatchResult.ResolveMemberWinnerID, the sub-bout twin of the
//     match-level WinnerID derivation two bullets above. The daihyosen
//     sentinel row (Position == DaihyosenSubPosition) is resolved the same
//     uniform way; it is simply never a RETIREMENT input (kachinuki
//     retirement already skips it) so an id here has no observable effect
//     either way.
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
	// One roster load shared by every step below: built
	// LAZILY on first actual use, so a competition needing no repair at all
	// never parses participants.csv, and loaded/indexed at most ONCE rather
	// than once per step. Safe to share across steps despite the seeds step's
	// own invalidateParticipantCaches call: that invalidates the STORE's
	// file cache (so the NEXT independent Store.LoadParticipants call
	// re-parses), but the seed-row backfill never touches participants.csv
	// itself, so the roster this instance already loaded stays accurate for
	// every step that follows it in the same call.
	roster := &legacyUpgradeRoster{store: s, compID: compID}
	if err := s.upgradeSeedRowsLocked(compID, roster); err != nil {
		log.Printf("state: legacy seed-row upgrade for %s: %v", compID, err)
	}
	if err := s.upgradePoolParticipantIDsLocked(compID, roster); err != nil {
		log.Printf("state: legacy pool-participant-id upgrade for %s: %v", compID, err)
	}
	// Squads BEFORE pool-matches/bracket/lineups (bc-tmid pass 2 reorder):
	// those three now also resolve SUB-BOUT / lineup-position member ids
	// against squads.yaml, so a team whose squad is migrated from
	// Player.Metadata in THIS SAME pass must be migrated before anything
	// downstream tries to resolve against it, or the very team this pass
	// just gave a squad to would still see "no squad" and skip repair for
	// a full extra load.
	if err := s.upgradeSquadsFromMetadataLocked(compID, roster, nil); err != nil {
		log.Printf("state: legacy squad upgrade for %s: %v", compID, err)
	}
	if err := s.upgradePoolMatchSideIDsLocked(compID, roster); err != nil {
		log.Printf("state: legacy pool-match-side-id upgrade for %s: %v", compID, err)
	}
	if err := s.upgradeBracketSideIDsLocked(compID, roster); err != nil {
		log.Printf("state: legacy bracket-side-id upgrade for %s: %v", compID, err)
	}
	if err := s.upgradeLineupMemberIDsLocked(compID, roster); err != nil {
		log.Printf("state: legacy lineup-member-id upgrade for %s: %v", compID, err)
	}
	s.legacyUpgraded.Store(compID, struct{}{})
}

// legacyUpgradeRoster lazily loads and indexes compID's participants.csv for
// EnsureLegacyUpgraded's steps. Caller MUST already hold the per-comp
// write lock (the same requirement every method below carries); this type
// adds no locking of its own because it is never shared beyond a single
// EnsureLegacyUpgraded call.
type legacyUpgradeRoster struct {
	store      *Store
	compID     string
	loaded     bool
	index      *domain.RosterIndex
	players    []domain.Player
	comp       *Competition
	compLoaded bool
	compErr    error
	withZekken bool
	loadErr    error

	// squadsLoaded / squadsData / squadsErr back the squads() accessor
	// below (bc-tmid pass 2): the sub-bout and lineup member-id repairs
	// both resolve against squads.yaml, so it is loaded lazily and cached
	// here exactly like the roster fields above, at most once per
	// EnsureLegacyUpgraded call.
	squadsLoaded bool
	squadsData   map[string][]domain.TeamMember
	squadsErr    error
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
	if err := r.loadComp(); err != nil {
		r.loadErr = err
		return nil, r.loadErr
	}
	if r.comp == nil {
		return nil, nil
	}
	players, err := r.store.loadParticipantsNoLock(r.compID, r.withZekken, LoadParticipantsOpts{HasIDs: r.comp.ParticipantIDsHint()})
	if err != nil {
		r.loadErr = err
		return nil, r.loadErr
	}
	r.players = players
	if len(players) > 0 {
		r.index = domain.NewRosterIndex(players)
	}
	return r.index, nil
}

// competition triggers get()'s lazy load (if it has not already run for
// this EnsureLegacyUpgraded call) and returns the competition record it
// fetched -- nil, nil when there is none. Lets a step that needs
// Kind/TeamSize rather than the RosterIndex (upgradeSquadsFromMetadataLocked)
// share the one load the other steps already pay for, instead of
// re-reading config.md itself.
// competition returns the competition record WITHOUT paying for the roster
// load get() performs. The split matters because upgradeSquadsFromMetadataLocked
// gates on Kind/TeamSize before it needs a single participant, and
// saveParticipantsNoLock calls that step on EVERY roster write: charging an
// individual competition a participants.csv parse and a RosterIndex build to
// answer a question its config.md already answers is work thrown away on a
// path the seeding panel hits repeatedly.
func (r *legacyUpgradeRoster) competition() (*Competition, error) {
	if err := r.loadComp(); err != nil {
		return nil, err
	}
	return r.comp, nil
}

// loadComp lazily loads the competition record and the zekken layout flag the
// roster parse below depends on. Separate from get() so a caller that only
// needs the record does not trigger the roster read; get() calls it first.
func (r *legacyUpgradeRoster) loadComp() error {
	if r.compLoaded {
		return r.compErr
	}
	r.compLoaded = true
	withZekken, comp, err := r.store.withZekkenNameLocked(r.compID)
	if err != nil {
		r.compErr = err
		return r.compErr
	}
	if comp == nil {
		// No competition on disk. Leave r.comp nil rather than stamping the
		// layout flag off a record that does not exist; both callers branch
		// on r.comp == nil.
		return nil
	}
	r.withZekken = withZekken
	r.comp = comp
	return nil
}

// rosterPlayers is competition's twin: it returns the raw, as-loaded player
// list get() populated (nil when there is no roster), for a step that needs
// each row's OWN fields (Metadata) rather than a name-keyed index.
func (r *legacyUpgradeRoster) rosterPlayers() ([]domain.Player, error) {
	if _, err := r.get(); err != nil {
		return nil, err
	}
	return r.players, nil
}

// squads returns compID's squads.yaml contents, loading it on the first
// call and caching the result (including a load failure) for every
// subsequent call in this same EnsureLegacyUpgraded invocation -- the
// squads.yaml sibling of get()/rosterPlayers() above (bc-tmid pass 2). A
// nil map with a nil error means "no squads recorded", which every caller
// below treats as "nothing to resolve against".
func (r *legacyUpgradeRoster) squads() (map[string][]domain.TeamMember, error) {
	if r.squadsLoaded {
		return r.squadsData, r.squadsErr
	}
	r.squadsLoaded = true
	r.squadsData, r.squadsErr = r.store.loadSquadsLocked(r.compID)
	return r.squadsData, r.squadsErr
}

// squadMemberIDByName looks up teamID's squad in squads for a member named
// name and returns its id, or "" when the team has no squad, or no member
// of it carries that exact name. Two members of ONE team sharing a name is
// already impossible (bc-tmdup), so this is the tractable, dance-free
// exact match the header comment above promises: unlike every
// participants.csv-facing resolver in this file, there is no NameCount
// uniqueness gate to run first, because within a single team's squad a
// name match is ALWAYS unambiguous.
func squadMemberIDByName(squads map[string][]domain.TeamMember, teamID, name string) string {
	if teamID == "" || name == "" {
		return ""
	}
	for _, m := range squads[teamID] {
		if m.Name == name {
			return m.ID
		}
	}
	return ""
}

// resolveSubMemberIDs fills sub's SideAMemberID/SideBMemberID from squads,
// resolving side A against teamAID's squad and side B against teamBID's
// squad -- the PARENT match's own already-resolved SideAID/SideBID, so this
// can only repair a sub-bout whose parent match id-resolved first.
// WinnerMemberID is then derived via SubMatchResult.ResolveMemberWinnerID
// (models.go), the sub-bout twin of the match-level WinnerID derivation the
// callers of this function (upgradePoolMatchSideIDsLocked,
// upgradeBracketSideIDsLocked) already run for the match itself. Returns
// whether it changed anything, matching those callers' changed-flag
// convention.
func resolveSubMemberIDs(sub *SubMatchResult, squads map[string][]domain.TeamMember, teamAID, teamBID string) bool {
	changed := false
	if sub.SideA != "" && sub.SideAMemberID == "" {
		if id := squadMemberIDByName(squads, teamAID, sub.SideA); id != "" {
			sub.SideAMemberID = id
			changed = true
		}
	}
	if sub.SideB != "" && sub.SideBMemberID == "" {
		if id := squadMemberIDByName(squads, teamBID, sub.SideB); id != "" {
			sub.SideBMemberID = id
			changed = true
		}
	}
	if sub.ResolveMemberWinnerID() {
		changed = true
	}
	return changed
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
// just-resolved sides. Since bc-tmid pass 2 it ALSO resolves each row's
// SUB-BOUT member ids (resolveSubMemberIDs) against the two teams' squads,
// in the SAME pass rather than a second read/write of this file -- see the
// header comment above for both rationales. Caller holds the per-comp lock.
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
		// bc-tmid pass 2: also scan sub-bout rows, so a match whose OWN
		// SideAID/SideBID/WinnerID are already fine (a fresh draw) but
		// whose bout log still lacks member ids still trips the repair.
		for j := range matches[i].SubResults {
			if matches[i].SubResults[j].MissingMemberID() {
				needs = true
				break
			}
		}
		if needs {
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
	// A squads.yaml this pass cannot read must not cost the MATCH-level
	// repair below, which needs no squad at all -- only the sub-bout branch
	// resolves against it. Aborting here left every legacy row's
	// SideAID/SideBID/WinnerID empty forever, and standings resolve BY ID
	// ONLY since bc-pnum, so each of those matches would contribute nothing
	// to anyone's record. Log and carry on with no squad: resolveSubMemberIDs
	// then finds no member to stamp, which is exactly the answer a team that
	// has no squad yet already gets.
	squads, err := roster.squads()
	if err != nil {
		log.Printf("state: legacy pool-match-side-id upgrade for %s: squads unreadable, sub-bout member ids not repaired: %v", compID, err)
		squads = nil
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
		// Sub-bout member ids (bc-tmid pass 2), resolved against the two
		// TEAMS' OWN squads using the SideAID/SideBID just resolved above --
		// see resolveSubMemberIDs' own doc for why an ambiguous row (both
		// SideAID/SideBID still empty via the whole-row skip above) is a
		// safe no-op here rather than a special case to guard against.
		for j := range m.SubResults {
			if resolveSubMemberIDs(&m.SubResults[j], squads, m.SideAID, m.SideBID) {
				changed = true
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
//
// Also true when any SUB-BOUT row is missing a member id (bc-tmid pass 2),
// so a bracket match whose OWN SideAID/SideBID/WinnerID are already fine
// but whose bout log still lacks member ids still trips the repair.
func bracketSideNeedsIDRepair(m *BracketMatch) bool {
	if (m.SideA != "" && !helper.IsReservedParticipantName(m.SideA) && m.SideAID == "") ||
		(m.SideB != "" && !helper.IsReservedParticipantName(m.SideB) && m.SideBID == "") ||
		(m.Winner != "" && m.WinnerID == "") {
		return true
	}
	for i := range m.SubResults {
		if m.SubResults[i].MissingMemberID() {
			return true
		}
	}
	return false
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

	// (d) Sub-bout member ids (bc-tmid pass 2), resolved against the two
	// TEAMS' OWN squads using the SideAID/SideBID (b) and, where applicable,
	// (a) just resolved -- a match neither reached leaves its sub-bouts
	// alone too, the same "can only repair what its parent resolved" rule
	// resolveSubMemberIDs' doc states.
	// A squads.yaml this pass cannot read must not cost the MATCH-level
	// repair below, which needs no squad at all -- only the sub-bout branch
	// resolves against it. Aborting here left every legacy row's
	// SideAID/SideBID/WinnerID empty forever, and standings resolve BY ID
	// ONLY since bc-pnum, so each of those matches would contribute nothing
	// to anyone's record. Log and carry on with no squad: resolveSubMemberIDs
	// then finds no member to stamp, which is exactly the answer a team that
	// has no squad yet already gets.
	squads, err := roster.squads()
	if err != nil {
		log.Printf("state: legacy bracket-side-id upgrade for %s: squads unreadable, sub-bout member ids not repaired: %v", compID, err)
		squads = nil
	}
	resolveSubs := func(m *BracketMatch) {
		for i := range m.SubResults {
			if resolveSubMemberIDs(&m.SubResults[i], squads, m.SideAID, m.SideBID) {
				changed = true
			}
		}
	}
	for i := range bracket.Rounds {
		for j := range bracket.Rounds[i] {
			resolveSubs(&bracket.Rounds[i][j])
		}
	}
	if bracket.ThirdPlaceMatch != nil {
		resolveSubs(bracket.ThirdPlaceMatch)
	}

	if !changed {
		return nil
	}
	return s.saveBracketLocked(compID, bracket, s.directWrite)
}

// upgradeSquadsFromMetadataLocked migrates a team's squad OUT of
// Player.Metadata into squads.yaml (bc-tmid), and SEEDS it up to the
// competition's TeamSize (bc-pnum ruling: "by default teams have x team
// members, as defined in the competition config, and those positions have
// their numbers"). Metadata is the untyped trailing-columns array
// participants.csv shares between two unrelated uses -- a team's ordered
// member-name list (CreatePlayersFromRecords/marshalParticipantsCSV) and an
// individual's dan grade at index 0 (buildPlayerMetadata, the SPA) -- so this
// only ever runs for a team competition (comp.Kind=="team" ||
// comp.TeamSize>0, the same discriminator checkTeamMemberNameCollisions
// already uses); scanning an individual's Metadata as a member list would
// invent members out of a dan-grade string.
//
// Two cases, both keyed on TeamSize rather than "has this team ever been
// migrated":
//
//   - No entry at all for the team's id yet: build members from any Metadata
//     names (indices 1..len(names), preserving the original migration's
//     shape) and then PAD with blank-named members up to TeamSize, so a team
//     with fewer named members than TeamSize (including zero) still ends up
//     with a full set of numbered slots.
//   - An entry already exists (a prior migration, or an operator using
//     AddTeamMember/RenameTeamMember/ClearTeamMemberName): re-folding
//     Metadata into it would duplicate members, so the existing members are
//     left untouched, but if TeamSize has since been RAISED the squad is
//     padded with new blank slots to match. TeamSize being LOWERED never
//     trims: a bout already fought refers to a position by its index, and a
//     smaller roster limit does not un-fight it (existing indices are always
//     contiguous 1..len(existing), because AddTeamMember only ever mints
//     max(existing index)+1 and nothing ever removes an entry, so the next
//     padded index is simply len(existing)+1).
//
// A member's Name is blank unless already known (from Metadata, or already
// stored) -- operator ruling: "a member with a BLANK name is a normal,
// expected state," never absence.
//
// Deliberately does NOT clear or rewrite Player.Metadata (operator ruling:
// "nothing is deleted" -- migrate on load, don't build a separate repair):
// squads.yaml becomes the source of truth going forward and the old array
// is simply left where it is.
//
// Requires the row's OWN participant id: squads.yaml is keyed by the
// team's participant id, and a genuinely legacy (pre-id-column) row has
// none yet -- the same residual miss the four legacy-upgrade steps above
// accept for the identical reason. Such a team is left alone here; the
// very next roster write mints its id (marshalParticipantsCSV), and the
// write after that can migrate it, exactly like the pools.csv/
// pool-matches.csv/bracket.json repairs above wait for the roster itself
// to gain ids before they can resolve against it.
//
// Caller holds the per-comp lock. Called from TWO sites: EnsureLegacyUpgraded
// (below, sharing the roster this function's siblings already loaded) and
// saveParticipantsNoLock (participants.go, given a FRESH roster instance),
// which calls this directly -- never EnsureLegacyUpgraded itself, which
// would re-acquire the same per-comp lock its caller already holds and
// deadlock a non-reentrant mutex -- BEFORE marshaling the incoming write,
// so a save that blanks a team's Metadata (the Apply flow's shape) cannot
// destroy members that were never migrated: see that call site's own
// comment for why a load-only trigger is not enough.
// mintedByCompetitor lets the PRE-WRITE call site (saveParticipantsNoLock)
// migrate a stored row that has no id of its own, under the id that same
// save is about to give it. It is keyed by helper.CompetitorKey's id-less
// form, the (name, dojo) pair that IS a competitor's identity wherever no
// id exists yet -- the documented carve-out, not a weakening of the id
// ruling, since the row being resolved carries no id field to resolve by.
// EnsureLegacyUpgraded passes nil: on a plain load there is no pending
// write to borrow an id from, so an id-less row still migrates to nothing.
func (s *Store) upgradeSquadsFromMetadataLocked(compID string, roster *legacyUpgradeRoster, mintedByCompetitor map[string]string) error {
	comp, err := roster.competition()
	if err != nil || comp == nil {
		return err
	}
	if comp.Kind != "team" && comp.TeamSize == 0 {
		return nil
	}
	players, err := roster.rosterPlayers()
	if err != nil || len(players) == 0 {
		return err
	}

	squads, err := s.loadSquadsLocked(compID)
	if err != nil {
		return err
	}

	changed := false
	for _, p := range players {
		id := p.ID
		if id == "" {
			id = mintedByCompetitor[helper.CompetitorKey("", p.Name, p.Dojo)]
		}
		if id == "" {
			continue // no stable key to migrate under yet; see doc comment above
		}
		existing, alreadyMigrated := squads[id]
		if !alreadyMigrated {
			names := nonBlankMetadata(p.Metadata)
			members := make([]domain.TeamMember, 0, max(len(names), comp.TeamSize))
			for i, name := range names {
				members = append(members, domain.TeamMember{
					ID:    newParticipantID(),
					Index: i + 1,
					Name:  name,
				})
			}
			for i := len(members); i < comp.TeamSize; i++ {
				members = append(members, domain.TeamMember{ID: newParticipantID(), Index: i + 1, Name: ""})
			}
			if len(members) == 0 {
				continue // nothing to migrate and nothing to seed (e.g. TeamSize 0)
			}
			squads[id] = members
			changed = true
			continue
		}
		// Already migrated (or operator-managed): never re-fold Metadata, but
		// pad up to a since-raised TeamSize. Indices are always contiguous
		// 1..len(existing) -- see the doc comment above -- so the next slot's
		// index is simply len(existing)+1.
		if len(existing) < comp.TeamSize {
			for i := len(existing); i < comp.TeamSize; i++ {
				existing = append(existing, domain.TeamMember{ID: newParticipantID(), Index: i + 1, Name: ""})
			}
			squads[id] = existing
			changed = true
		}
	}
	if !changed {
		return nil
	}
	if err := s.saveSquadsLocked(compID, squads, s.directWrite); err != nil {
		return err
	}
	// Drop the shared lazy squad cache: this call just changed squads.yaml,
	// and the sub-bout and lineup repairs that follow resolve against it.
	// Today nothing reads squads before this step, so this is a no-op -- and
	// that is exactly the point. It makes the ordering above a property of
	// the code rather than of the current step list, because the failure it
	// prevents is silent: a stale map reports "no squad" for the very team
	// this step just built one for, and the repair is skipped for a whole
	// extra load with nothing logged. Same safe direction bumpFileVersion
	// takes (store.go) -- an unnecessary invalidation costs one re-parse, a
	// missed one serves stale data.
	roster.squadsLoaded = false
	roster.squadsData = nil
	roster.squadsErr = nil
	return nil
}

// upgradeLineupMemberIDsLocked completes a legacy (or otherwise unrepaired)
// lineups.yaml: an occupied position's MemberIDs entry is filled from the
// team's OWN squad (squads.yaml) whenever the position's Name resolves to
// EXACTLY one member on that team -- always true once bc-tmdup's
// duplicate-name refusal holds (bc-tmid pass 2), so this needs none of the
// NameCount-gated uniqueness dance the participants.csv-facing repairs
// above carry: two members of ONE team sharing a name is already
// impossible, so an exact name match within a single team's squad can
// never be ambiguous. A position whose name matches no squad member -- a
// genuinely unresolvable row, or a team with no squad recorded at all -- is
// left alone; the residue is a Positions entry with no MemberIDs
// counterpart, which every id-aware reader (kachinuki retirement) already
// falls back to the name for.
//
// Runs AFTER upgradeSquadsFromMetadataLocked in EnsureLegacyUpgraded's
// step order (see that function's own comment): a team migrated from
// Player.Metadata in the SAME pass must already have its squad before this
// step can resolve against it.
//
// Reads/writes lineups.yaml directly via loadTeamLineupsLocked/
// saveTeamLineupsLocked rather than a copy-then-mutate wrapper: there is
// none to bypass here (loadTeamLineupsLocked is already the direct,
// no-cache loader), so this repair owns the parsed map exclusively like
// its siblings above. Caller holds the per-comp lock.
func (s *Store) upgradeLineupMemberIDsLocked(compID string, roster *legacyUpgradeRoster) error {
	lineups, err := s.loadTeamLineupsLocked(compID)
	if err != nil {
		// Propagate rather than swallow, like every sibling step: a missing
		// lineups.yaml already reads as an empty map (parseTeamLineupsFile),
		// so an error here means the file exists and could not be parsed --
		// the one condition EnsureLegacyUpgraded's log-and-continue policy
		// exists to report. Swallowing it left a corrupt lineup file as the
		// only repair failure in this pass with nothing logged anywhere,
		// while every position silently fell back to name matching.
		return err
	}
	if len(lineups) == 0 {
		return nil
	}
	needs := false
	for _, l := range lineups {
		for pos, name := range l.Positions {
			if name != "" && l.MemberIDs[pos] == "" {
				needs = true
				break
			}
		}
		if needs {
			break
		}
	}
	if !needs {
		return nil
	}
	squads, err := roster.squads()
	if err != nil || len(squads) == 0 {
		return err
	}
	changed := false
	for key, l := range lineups {
		members := squads[l.TeamID]
		if len(members) == 0 {
			continue
		}
		lineupChanged := false
		for pos, name := range l.Positions {
			if name == "" || l.MemberIDs[pos] != "" {
				continue
			}
			if id := squadMemberIDByName(squads, l.TeamID, name); id != "" {
				if l.MemberIDs == nil {
					l.MemberIDs = map[domain.Position]string{}
				}
				l.MemberIDs[pos] = id
				lineupChanged = true
			}
		}
		if lineupChanged {
			lineups[key] = l
			changed = true
		}
	}
	if !changed {
		return nil
	}
	return s.saveTeamLineupsLocked(compID, lineups, s.directWrite)
}

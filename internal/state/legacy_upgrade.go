package state

import (
	"errors"
	"fmt"
	"log"
	"maps"
	"os"
	"slices"
	"strings"

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
//   - bracket.json DisplayRound/MatchNumber convert ON READ, below (bc-tmfn),
//     in the same parse and the same write as the side-id repair above
//     (upgradeBracketLocked). Any stored bracket whose rounds or numbers
//     differ from the rule generation applies is recomputed: each real
//     bout's round is its distance from the final along the bracket's OWN
//     stored Feeders, and the bouts are renumbered by the one numbering
//     rule (Bracket.RestampRoundsFromFeeders, sharing
//     Bracket.StampRoundsFromFeeders with generation). In practice that
//     means brackets drawn by v2.0.0 or v2.1.0, which put a pair drawn beside
//     an empty pair one round early and numbered by those rounds, and older
//     brackets with byes: their rounds were already right (Feeders are
//     stored since v0.17.0, mp-7f2w, and every release before v2.0.0
//     derived the rounds from them),
//     but before v2.0.0 a tie inside a round was broken by the bout's
//     position in its own pow2 round, not its first-round slot, so where one
//     round draws bouts from two pow2 rounds they are renumbered. Either
//     way the app then agrees with the Excel export, which numbers the
//     rebuilt tree and lays scores and courts onto the printed numbers.
//     Recognising one needs no guess, and a bracket that is already right
//     compares equal and is not written. Times too: every release before
//     match-number scheduling gave a court its times in storage order, so
//     on a bracket where no real bout has been started, scored or decided, a
//     court whose times still rise in storage order has its OWN times handed
//     out again in match-number order and the court queue runs Match 1
//     first. A touched bracket keeps its times, and so does a court whose
//     times do not rise in storage order (this release's scheduling, or a
//     time moved by hand). Pairings, results, courts and the bronze are
//     never touched. A bracket with no Feeders at all predates the
//     feeder metadata and is left alone silently; one whose Feeders do not
//     form the generated tree is left alone and logged, once per process.
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
//     team-members.yaml ON READ *and* ON WRITE (bc-tmid), below
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
//     AFTER the team-members.yaml migration above so a team migrated in the SAME
//     pass is still resolvable. Unlike every upgrade above this one is not
//     resolving against the roster at all, but against the TEAM'S OWN
//     squad (team-members.yaml), and needs none of the NameCount-gated
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
//   - config.md's retired format/status literal ("playoffs") and duration
//     keys (playoff_match_duration, playoff_match_duration_seconds) convert
//     on WRITE, via upgradeCompetitionFormatLocked below (bc-terminology
//     commit 1 follow-up), called from EnsureLegacyUpgraded like every step
//     above AND from a one-time startup sweep (sweepLegacyUpgrades, called
//     from NewStore) so the whole folder converges without waiting for each
//     competition to be individually read. Unlike every upgrade above, this
//     one resolves nothing against the roster -- parseCompetitionFile
//     (competition.go) already folds every retired value in memory on EVERY
//     read, unconditionally, so this is a best-effort convenience that lets
//     the on-disk bytes catch up, never a correctness requirement. It
//     refuses to write when the loaded record's own id: does not match the
//     directory it was loaded from -- the guard a deleted, buggier version
//     of this migration lacked, which let it save a converted competition's
//     bytes into a DIFFERENT competition's directory, under that OTHER
//     competition's lock, because saveCompetitionLocked paths off the
//     record's own id: rather than the directory it was read from. A blank
//     or missing id: is simply "" failing to match a non-empty directory
//     name, so it is caught by that same guard rather than needing one of
//     its own.
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
// ONLY the five PUBLIC, caller-does-not-already-hold-the-lock entry points on
// the READ path (loadParticipants, Store.LoadPools, Store.LoadPoolMatches,
// Store.LoadBracket, and ParticipantsFingerprint below, which calls it
// directly rather than through one of the other four), PLUS ONE caller on the
// startup path (sweepLegacyUpgrades below, called once from NewStore, which
// loops every id ListCompetitions finds so the whole data folder converges
// without waiting for each competition's files to be individually read) call
// this. All six are safe for the identical reason: none of them already hold
// compID's per-comp lock at the point they call in. The *Locked siblings
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
	// config.md's retired format/status/duration shapes convert independently
	// of the participant roster the steps below share: it touches a different
	// file and needs no lookup against anything. Best-effort, log-and-continue,
	// exactly like every step below -- parseCompetitionFile's in-memory fold
	// (competition.go) is what actually guarantees a correct read regardless
	// of whether this succeeds.
	if err := s.upgradeCompetitionFormatLocked(compID); err != nil {
		log.Printf("state: legacy competition-format upgrade for %s: %v", compID, err)
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
	// FIRST: every step below resolves against the roster's participant ids,
	// so a roster that has none makes all of them no-ops.
	if err := s.upgradeParticipantIDsLocked(compID, roster); err != nil {
		log.Printf("state: legacy participant-id upgrade for %s: %v", compID, err)
	}
	if err := s.upgradeSeedRowsLocked(compID, roster); err != nil {
		log.Printf("state: legacy seed-row upgrade for %s: %v", compID, err)
	}
	if err := s.upgradePoolParticipantIDsLocked(compID, roster); err != nil {
		log.Printf("state: legacy pool-participant-id upgrade for %s: %v", compID, err)
	}
	// Squads BEFORE pool-matches/bracket/lineups (bc-tmid pass 2 reorder):
	// those three now also resolve SUB-BOUT / lineup-position member ids
	// against team-members.yaml, so a team whose squad is migrated from
	// Player.Metadata in THIS SAME pass must be migrated before anything
	// downstream tries to resolve against it, or the very team this pass
	// just gave a squad to would still see "no squad" and skip repair for
	// a full extra load.
	// The v2.0.0 filename migration runs INSIDE this step (see its own comment)
	// so both of its callers get it, and so a failure aborts before anything
	// mints blank members over the file it has not adopted yet. It therefore
	// also runs before the three id repairs below, which all resolve against
	// team-members.yaml.
	//
	// A failure here is logged and the competition is STAMPED ANYWAY, like
	// every other step. Not stamping was tried and reverted: this function is
	// on the viewer's hot path (LoadPools, LoadPoolMatches, LoadBracket and
	// loadParticipants each call it, and one viewer payload calls three of
	// them), so a competition whose squads.yaml is permanently unreadable
	// made EVERY poll take the exclusive per-competition lock and re-parse
	// four files for the life of the process, which is exactly what the
	// failure policy above exists to prevent.
	//
	// Stamping costs nothing here, because the stamp gates only THIS path.
	// The write that could actually destroy data is the blank-member seeding
	// inside the step below, and that step aborts before minting whenever the
	// adoption fails -- on every call, stamp or no stamp. Its other caller,
	// saveParticipantsNoLock, is not stamp-gated at all, and loadSquadsLocked
	// re-attempts the adoption for all three squad mutators (squad.go). So a
	// fault that clears is still picked up; what stops is only the re-sweep
	// of seven unrelated steps from a read.
	if err := s.upgradeSquadsFromMetadataLocked(compID, roster, nil); err != nil {
		log.Printf("state: legacy squad upgrade for %s: %v", compID, err)
	}
	if err := s.upgradePoolMatchSideIDsLocked(compID, roster); err != nil {
		log.Printf("state: legacy pool-match-side-id upgrade for %s: %v", compID, err)
	}
	// bracket.json's side ids, then its rounds, match numbers and times, over
	// one parse and at most one write (upgradeBracketLocked).
	if err := s.upgradeBracketLocked(compID, roster); err != nil {
		log.Printf("state: legacy bracket upgrade for %s: %v", compID, err)
	}
	if err := s.upgradeLineupMemberIDsLocked(compID, roster); err != nil {
		log.Printf("state: legacy lineup-member-id upgrade for %s: %v", compID, err)
	}
	if err := s.upgradeTeamDefaultWinBoutPaddingLocked(compID, roster); err != nil {
		log.Printf("state: legacy team-default-win-bout-padding upgrade for %s: %v", compID, err)
	}
	s.legacyUpgraded.Store(compID, struct{}{})
}

// upgradeCompetitionFormatLocked converges compID's config.md onto the
// canonical bc-terminology values (the retired "playoffs" format/status
// literal, and the matching retired duration keys) by loading and re-saving
// it. Caller holds compID's per-competition write lock.
//
// This is NOT a safety mechanism: parseCompetitionFile (competition.go)
// already folds every retired format/status/duration shape in memory on
// EVERY read, unconditionally, regardless of whether this function ever
// runs or runs and fails. This is a best-effort WRITE-side convergence pass
// only, so an operator's config.md eventually stops saying "playoffs"
// instead of saying so forever.
//
// A prior attempt at this exact migration shipped three data-loss bugs and
// was deleted; the guards below are the fix for each, not incidental
// caution -- do not remove or "simplify" any of them.
//
//   - BUG 1 (wrong-competition write): the prior code loaded by compID (the
//     directory) but saved via saveCompetitionLocked, which builds its path
//     from c.ID (the "id:" front-matter field) and documents that the
//     CALLER must already hold THAT id's lock. A directory whose id:
//     disagrees with its own directory name -- an operator copying
//     competitions/foo to competitions/foo-backup, say -- got its converted
//     bytes written INTO the other competition's directory, outside that
//     competition's lock, destroying it. Guarded below by refusing to save
//     whenever comp.ID != compID. Do not "fix" a mismatch by assigning
//     comp.ID = compID instead: that silently rewrites the record's
//     identity, and nothing here can tell whether the directory name or the
//     id: field is the mistake. Skipping is correct either way: the
//     in-memory fold still serves this competition correctly on every read,
//     so nothing is lost by declining to converge its on-disk bytes.
//
//   - BUG 2 (blank id: made a competition permanently unreadable):
//     saveCompetitionLocked's first line is ValidateCompetitionID(c.ID),
//     which rejects "". The prior code let that error propagate out of the
//     migration and, from there, out of every subsequent load, so a
//     config.md with no id: (or a blank one) -- which loaded fine before
//     this migration existed -- became unloadable forever. The bug-1 guard
//     above already covers this: comp.ID == "" can never equal a non-empty
//     compID (every id ListCompetitions/EnsureLegacyUpgraded hands in is a
//     real directory name), so a blank id: is skipped for the same reason a
//     mismatched one is, and saveCompetitionLocked's validation is never
//     reached with an empty ID from this function.
//
//   - BUG 3 (duration guard ran too late): the prior code re-derived
//     KnockoutMatchDurationSeconds itself, checking == 0 AFTER
//     ApplyCompetitionDefaults had already back-filled it from a
//     whole-minute key, so a file carrying both the precise retired seconds
//     key and a whole-minute key resolved to the coarser, rounded value, and
//     the rewrite then deleted the precise value permanently. Fixed
//     structurally, not here: this function does no duration arithmetic of
//     its own. loadCompetitionLocked already runs the fold, in the right
//     order (the retired seconds key beats the whole-minute keys -- see
//     ApplyCompetitionDefaults), before this function ever sees the struct,
//     and saveCompetitionLocked normalizes again (idempotently) on the way
//     out. Reintroducing any duration handling here would risk the exact
//     same ordering mistake; don't.
//
// Reuses loadCompetitionLocked/saveCompetitionLocked rather than a bespoke
// duration recovery (the deleted attempt's bug 3, not coming back): this is
// deliberately just "load, guard, save", with no duration arithmetic of its
// own anywhere in this function.
//
// It DOES pre-check the raw bytes for a retired token before paying for that
// load+save, and this is NOT the deleted attempt's "second front-matter
// read" returning: it is a plain byte scan, never a YAML unmarshal, and it
// exists for a reason a pure "load, guard, save" cannot avoid on its own.
// saveCompetitionChangedLocked's existing no-op guard only compares the
// reserialized bytes against what's on disk, and that catches more than
// "this file carried a retired shape": a config.md written by an OLDER
// release, missing some field a LATER release added without an omitempty
// tag, reserializes with that field now spelled out at its zero value --
// a real byte difference, and a real write, for a competition with nothing
// retired in it at all. A fleet's worth of already-canonical competitions
// would each take that spurious write on every single process start. The
// byte scan below keeps this function's effect scoped to files that
// actually need converging, at the cost of one extra cheap read only on
// the files that do.
func (s *Store) upgradeCompetitionFormatLocked(compID string) error {
	path := s.compPath(compID, "config.md")
	raw, err := os.ReadFile(path) // #nosec G304; path built by compPath, which enforces containment under the competitions dir
	if err != nil {
		if os.IsNotExist(err) {
			return nil // no config.md on disk for this id; nothing to converge
		}
		return err
	}
	text := string(raw)
	if !strings.Contains(text, "playoffs") && !strings.Contains(text, "playoff_match_duration") {
		return nil // already canonical; see the doc comment above for why this matters
	}

	comp, err := s.loadCompetitionLocked(compID)
	if err != nil {
		return err
	}
	if comp == nil {
		return nil // config.md vanished between the scan above and this load
	}
	if comp.ID != compID {
		// BUG 1 / BUG 2 guard: saveCompetitionLocked paths and locks off
		// comp.ID, not compID (the directory this call was handed). Writing
		// here -- for a mismatched id: OR a blank one -- would either land in
		// a different competition's directory outside that competition's
		// lock, or fail ValidateCompetitionID outright. Skip; the in-memory
		// fold keeps serving this competition correctly regardless.
		log.Printf("state: legacy competition-format upgrade for %s: config.md id %q does not match its directory; left unconverted", compID, comp.ID)
		return nil
	}
	return s.saveCompetitionLocked(comp, s.directWrite)
}

// sweepLegacyUpgrades runs EnsureLegacyUpgraded for every competition
// ListCompetitions finds, so the whole data folder converges once at startup
// rather than waiting for each competition's participants/pools/pool-matches/
// bracket/config to be individually read. Called from NewStore AFTER
// s.init() has released s.mu: ListCompetitions takes its own RLock, and
// calling it while init still held s.mu's write lock would deadlock the
// non-reentrant mutex.
//
// Best-effort at both levels it touches. EnsureLegacyUpgraded already logs
// and continues per upgrade step for a single competition; a failure to even
// LIST the competitions directory (unusual -- init() just created it) is
// logged here and swallowed rather than returned, so neither a single broken
// competition nor a transient listing failure can stop NewStore from
// succeeding and starting mobile-app or print.
func (s *Store) sweepLegacyUpgrades() {
	ids, err := s.ListCompetitions()
	if err != nil {
		log.Printf("state: startup legacy-upgrade sweep: could not list competitions: %v", err)
		return
	}
	for _, id := range ids {
		s.EnsureLegacyUpgraded(id)
	}
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
	// both resolve against team-members.yaml, so it is loaded lazily and cached
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

// squads returns compID's team-members.yaml contents, loading it on the first
// call and caching the result (including a load failure) for every
// subsequent call in this same EnsureLegacyUpgraded invocation -- the
// team-members.yaml sibling of get()/rosterPlayers() above (bc-tmid pass 2). A
// nil map with a nil error means "no squads recorded", which every caller
// below treats as "nothing to resolve against".
// reset drops what this pass has already read off disk, so the steps below
// re-read it. Used after the participant-id repair changes how
// participants.csv PARSES (the has_participant_ids flip): everything after
// that point resolves against the roster, and a cached pre-flip parse is the
// column-shifted one.
func (r *legacyUpgradeRoster) reset() {
	r.loaded = false
	r.loadErr = nil
	r.index = nil
	r.players = nil
	r.compLoaded = false
	r.comp = nil
	r.compErr = nil
}

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
	// A team-members.yaml this pass cannot read must not cost the MATCH-level
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

// upgradeBracketLocked runs the two bracket.json repairs, the side ids
// (upgradeBracketSideIDsLocked) and then the rounds, match numbers and times
// (upgradeBracketRoundsLocked), over ONE parse of the file, and saves it once
// when either changed it. The rounds pass reads one thing the side-id pass
// writes, WinnerID, in its "has this bracket been played" check, and the
// side-id pass only derives a WinnerID on a row whose Winner already answers
// yes, so their order does not change the outcome. Caller holds the per-comp
// lock.
//
// Parses bracket.json directly (parseBracketFile) rather than going through
// loadBracketLocked, for the same reason the pools/pool-matches upgrades
// above parse directly: that wrapper is parse-then-deep-copy
// (state.copyBracket), and the copy is pure overhead when this repair owns
// the parsed value exclusively and either discards it or hands it straight
// to saveBracketLocked, which refreshes the bracket cache and bumps its file
// version as every bracket write does.
//
// A side-id repair that fails part way (an unreadable roster) is logged and
// does not stop the rounds: what it had already stamped is the positional
// round-0 repair from DrawOrder, which needs no roster and is exact, so it is
// saved along with the rounds rather than thrown away.
func (s *Store) upgradeBracketLocked(compID string, roster *legacyUpgradeRoster) error {
	path := s.compPath(compID, "bracket.json")
	parsed, err := parseBracketFile(path)
	if err != nil {
		return nil // missing/unreadable bracket is the consumers' error to report
	}
	bracket, _ := parsed.(*Bracket)
	if bracket == nil || len(bracket.Rounds) == 0 {
		return nil
	}
	idsChanged, err := s.upgradeBracketSideIDsLocked(compID, bracket, roster)
	if err != nil {
		log.Printf("state: legacy bracket-side-id upgrade for %s: %v", compID, err)
	}
	settledBefore := bracket.TimesSettled
	changes := s.upgradeBracketRoundsLocked(compID, bracket)
	// A bracket that only became TimesSettled moved no match but must still
	// be saved, or its times are examined again on every load.
	if !idsChanged && len(changes) == 0 && bracket.TimesSettled == settledBefore {
		return nil
	}
	if err := s.saveBracketLocked(compID, bracket, s.directWrite); err != nil {
		return err
	}
	if len(changes) > 0 {
		moved := make([]string, 0, len(changes))
		for _, c := range changes {
			moved = append(moved, fmt.Sprintf("%s round %d->%d match %d->%d at %q->%q",
				c.ID, c.OldRound, c.NewRound, c.OldNumber, c.NewNumber, c.OldScheduledAt, c.NewScheduledAt))
		}
		log.Printf("state: bracket rounds for %s recomputed from its feeders: %s", compID, strings.Join(moved, "; "))
	}
	return nil
}

// upgradeBracketSideIDsLocked completes a legacy (no-id) bracket's
// SideAID/SideBID/WinnerID in place and reports whether it changed anything.
// See the header comment above for the full resolution order (DrawOrder
// positionally for round 0, then the unique-bare-name fallback everywhere
// else, then WinnerID derived from each row's own resolved sides). Called by
// upgradeBracketLocked, which owns the parse and the save.
func (s *Store) upgradeBracketSideIDsLocked(compID string, bracket *Bracket, roster *legacyUpgradeRoster) (bool, error) {
	if !bracketNeedsIDRepair(bracket) {
		return false, nil
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
		return changed, err
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
	// A team-members.yaml this pass cannot read must not cost the MATCH-level
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
	return changed, nil
}

// upgradeBracketRoundsLocked brings a parsed bracket's DisplayRound and
// MatchNumber (and, on an untouched bracket it renumbers, its times) up to
// the rule generation applies (bc-tmfn), in place, through
// Bracket.RestampRoundsFromFeeders, and returns what moved. See the header
// comment above for which brackets it changes and which it leaves alone.
// Called by upgradeBracketLocked, which owns the parse and the save.
//
// A bracket whose Feeders cannot be walked is left as stored and logged, but
// only the FIRST time this process meets it: saveParticipantsNoLock re-arms
// EnsureLegacyUpgraded after every roster write (bc-pnum), and a bracket that
// cannot be walked on one pass cannot be on the next, so logging each pass
// would repeat the same line after every check-in.
func (s *Store) upgradeBracketRoundsLocked(compID string, bracket *Bracket) []BracketRoundChange {
	changes, err := bracket.RestampRoundsFromFeeders()
	if errors.Is(err, ErrBracketNoFeeders) {
		return nil // predates the feeder metadata: nothing to walk
	}
	if err != nil {
		if _, logged := s.bracketRoundsRefusalLogged.LoadOrStore(compID, struct{}{}); !logged {
			log.Printf("state: legacy bracket-rounds upgrade for %s: rounds and match numbers left as stored: %v", compID, err)
		}
		return nil
	}
	return changes
}

// upgradeTeamDefaultWinBoutPaddingLocked pads a stored COMPLETED team match
// that a default-win ruling (any kiken, fusenpai, or fusensho:
// domain.IsDefaultWinDecisionStr) closed with fewer SubResults rows than the
// competition's TeamSize -- e.g. a kiken recorded before the match's first
// bout was ever scored, on a file saved before this padding existed -- with
// an empty SubMatchResult row (Position only) for every missing numbered
// position, via PadDefaultWinBoutPositions, the SAME function
// engine.RecordMatchResultWithIneligibilityTx now applies on write. Without
// this, a match recorded before that write-time padding existed keeps
// contributing nothing to IV/PW forever: DefaultWinCreditSide's readers
// (TeamResultFrom, engine.accrueTeamSubResults, the Excel export) can only
// credit a Position actually present in SubResults.
//
// Independent of the roster/id repairs above (it touches Position/SubResults
// shape only, never a name or id), so ordering relative to them does not
// matter; it runs last purely because it is the newest, unrelated addition.
// Caller holds the per-comp lock (EnsureLegacyUpgraded).
func (s *Store) upgradeTeamDefaultWinBoutPaddingLocked(compID string, roster *legacyUpgradeRoster) error {
	comp, err := roster.competition()
	if err != nil || comp == nil || comp.TeamSize < 2 || comp.IsKachinuki() {
		return err
	}
	if err := s.padTeamDefaultWinBoutsInPoolMatchesLocked(compID, comp.TeamSize); err != nil {
		return err
	}
	return s.padTeamDefaultWinBoutsInBracketLocked(compID, comp.TeamSize)
}

// padTeamDefaultWinBoutsInPoolMatchesLocked is
// upgradeTeamDefaultWinBoutPaddingLocked's pool-matches.csv half. Caller
// holds the per-comp lock.
func (s *Store) padTeamDefaultWinBoutsInPoolMatchesLocked(compID string, teamSize int) error {
	path := s.compPath(compID, "pool-matches.csv")
	parsed, err := parsePoolMatchesFile(path)
	if err != nil {
		return nil // missing/unreadable pool matches are the consumers' error to report
	}
	matches, _ := parsed.([]MatchResult)
	if len(matches) == 0 {
		return nil
	}
	changed := false
	for i := range matches {
		m := &matches[i]
		if !NeedsDefaultWinBoutPadding(m.Status, m.Decision, m.SideA, m.SideB, m.ID, m.SubResults, m.SubResultsUnreadable, teamSize) {
			continue
		}
		m.SubResults = PadDefaultWinBoutPositions(m.SubResults, teamSize)
		changed = true
	}
	if !changed {
		return nil
	}
	return s.savePoolMatchesLocked(compID, matches, s.directWrite)
}

// padTeamDefaultWinBoutsInBracketLocked is
// upgradeTeamDefaultWinBoutPaddingLocked's bracket.json half. Caller holds
// the per-comp lock. A bracket match id never carries the pool DH/TB suffix,
// so IsPoolDaihyosenMatchID/IsTiebreakerMatchID are harmless (always false)
// here -- checked anyway inside NeedsDefaultWinBoutPadding so the one
// predicate serves both halves rather than one gated copy and one ungated.
func (s *Store) padTeamDefaultWinBoutsInBracketLocked(compID string, teamSize int) error {
	path := s.compPath(compID, "bracket.json")
	parsed, err := parseBracketFile(path)
	if err != nil {
		return nil // missing/unreadable bracket is the consumers' error to report
	}
	bracket, _ := parsed.(*Bracket)
	if bracket == nil || len(bracket.Rounds) == 0 {
		return nil
	}
	changed := false
	pad := func(m *BracketMatch) {
		// bracket.json parses as one atomic JSON document, so there is no
		// per-match "unreadable SubResults cell" shape here the way a
		// pool-matches.csv row has (a parse failure fails the WHOLE file,
		// caught by parseBracketFile above, and this loop never runs).
		// false is therefore always correct, not a stand-in for a missing
		// field.
		if !NeedsDefaultWinBoutPadding(m.Status, m.Decision, m.SideA, m.SideB, m.ID, m.SubResults, false, teamSize) {
			return
		}
		m.SubResults = PadDefaultWinBoutPositions(m.SubResults, teamSize)
		changed = true
	}
	for i := range bracket.Rounds {
		for j := range bracket.Rounds[i] {
			pad(&bracket.Rounds[i][j])
		}
	}
	if bracket.ThirdPlaceMatch != nil {
		pad(bracket.ThirdPlaceMatch)
	}
	if !changed {
		return nil
	}
	return s.saveBracketLocked(compID, bracket, s.directWrite)
}

// upgradeSquadsFromMetadataLocked migrates a team's squad OUT of
// Player.Metadata into team-members.yaml (bc-tmid), and SEEDS it up to
// squadFloor(comp.TeamSize) -- the competition's TeamSize plus two reserve
// slots (bc-pnum ruling: "by default teams have x team members, as defined
// in the competition config, and those positions have their numbers";
// extended by operator ruling 2026-09-15, bc-dnst, to reserve two further
// numbered slots beyond TeamSize so the score sheet can offer every number
// the team can field). Metadata is the untyped trailing-columns array
// participants.csv shares between two unrelated uses -- a team's ordered
// member-name list (CreatePlayersFromRecords/marshalParticipantsCSV) and an
// individual's dan grade at index 0 (buildPlayerMetadata, the SPA) -- so this
// only ever runs for a team competition (comp.Kind=="team" ||
// comp.TeamSize>0, the same discriminator checkTeamMemberNameCollisions
// already uses); scanning an individual's Metadata as a member list would
// invent members out of a dan-grade string.
//
// Two cases, both keyed on the floor rather than "has this team ever been
// migrated":
//
//   - No entry at all for the team's id yet: build members from any Metadata
//     names (indices 1..len(names), preserving the original migration's
//     shape) and then PAD with blank-named members up to the floor, so a
//     team with fewer named members than the floor (including zero) still
//     ends up with a full set of numbered slots.
//   - An entry already exists (a prior migration, or an operator using
//     AddTeamMember/RenameTeamMember/ClearTeamMemberName): re-folding
//     Metadata into it would duplicate members, so the existing members are
//     left untouched, but if the floor has since RISEN (TeamSize raised) the
//     squad is padded with new blank slots to match. The floor falling
//     never trims: a bout already fought refers to a position by its index,
//     and a smaller roster limit does not un-fight it (existing indices are
//     always contiguous 1..len(existing), because AddTeamMember only ever
//     mints max(existing index)+1 and nothing ever removes an entry, so the
//     next padded index is simply len(existing)+1).
//
// A member's Name is blank unless already known (from Metadata, or already
// stored) -- operator ruling: "a member with a BLANK name is a normal,
// expected state," never absence.
//
// Deliberately does NOT clear or rewrite Player.Metadata (operator ruling:
// "nothing is deleted" -- migrate on load, don't build a separate repair):
// team-members.yaml becomes the source of truth going forward and the old array
// is simply left where it is.
//
// Requires the row's OWN participant id: team-members.yaml is keyed by the
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
// rosterFirstColumn is what column 0 of participants.csv actually holds.
// The whole participant-id repair turns on this one question, because a
// legacy roster with no id column and a roster carrying non-UUID ids whose
// has_participant_ids flag never landed are BYTE-IDENTICAL on disk: both
// parse as "no ids", and the second one parses SHIFTED (id read as the name,
// name read as the dojo). Minting over that shift rewrites it permanently
// and destroys the id every other record points at, which is why this file's
// header forbids a read-side roster rewrite without an unambiguous
// discriminator. This type IS that discriminator, or says it has none.
type rosterFirstColumn int

const (
	// rosterFirstColumnUnknown: nothing on disk can say. The roster is left
	// exactly as it is -- the one honest answer, and the reason a
	// competition still in setup with non-UUID ids keeps its file untouched.
	rosterFirstColumnUnknown rosterFirstColumn = iota
	rosterFirstColumnID
	rosterFirstColumnName
)

// referencedParticipantKeys returns every participant id and every competitor
// NAME that some OTHER file of this competition records, read through the
// same locked readers the repairs below use (never the public Load*, which
// would re-enter EnsureLegacyUpgraded and deadlock on the lock this caller
// already holds).
//
// Both halves are evidence, and the name half is the stronger one: an id
// reference proves column 0 is an id only when it matches, whereas a name
// reference decides by WHICH column it lands in -- names in column 1 mean
// column 0 is the id, names in column 0 mean column 0 is the name. That is
// what lets this tell the two byte-identical files apart.
//
// Bracket placeholder sides ("Pool A-1st", "Winner of M1", byes) simply match
// no roster row and fall out harmlessly.
func (s *Store) referencedParticipantKeys(compID string) (ids, names map[string]bool) {
	ids, names = map[string]bool{}, map[string]bool{}
	add := func(set map[string]bool, v string) {
		if v = strings.TrimSpace(v); v != "" {
			set[v] = true
		}
	}
	if pools, err := s.loadPoolsLocked(compID); err == nil {
		for _, pool := range pools {
			for _, p := range pool.Players {
				add(ids, p.ID)
				add(names, p.Name)
			}
		}
	}
	if matches, err := s.LoadPoolMatchesLocked(compID); err == nil {
		for _, m := range matches {
			add(ids, m.SideAID)
			add(ids, m.SideBID)
			add(ids, m.WinnerID)
			add(names, m.SideA)
			add(names, m.SideB)
		}
	}
	if bracket, err := s.loadBracketLocked(compID); err == nil && bracket != nil {
		for _, round := range bracket.Rounds {
			for _, m := range round {
				add(ids, m.SideAID)
				add(ids, m.SideBID)
				add(ids, m.WinnerID)
				add(names, m.SideA)
				add(names, m.SideB)
			}
		}
		for _, id := range bracket.DrawOrder {
			add(ids, id)
		}
	}
	return ids, names
}

// classifyRosterFirstColumn answers the question rosterFirstColumn poses,
// for a competition whose has_participant_ids flag is false.
//
// Tier 1 is shape: every non-empty column-0 value being a UUID is the SAME
// discriminator the loader's own auto-detect already trusts, so agreeing
// with it adds no new guess.
//
// Tier 2 is evidence from the competition's other files, and it is what
// makes this repair safe rather than a coin flip. Anything else returns
// Unknown and the caller leaves the file alone.
func (s *Store) classifyRosterFirstColumn(compID string, rows [][]string) rosterFirstColumn {
	col0, col1 := map[string]bool{}, map[string]bool{}
	allUUID, any := true, false
	for _, rec := range rows {
		if len(rec) == 0 {
			continue
		}
		v := strings.TrimSpace(rec[0])
		if v == "" {
			continue
		}
		any = true
		col0[v] = true
		if !uuidRE(v) {
			allUUID = false
		}
		if len(rec) > 1 {
			if n := strings.TrimSpace(rec[1]); n != "" {
				col1[n] = true
			}
		}
	}
	if !any {
		return rosterFirstColumnUnknown
	}
	if allUUID {
		return rosterFirstColumnID
	}

	refIDs, refNames := s.referencedParticipantKeys(compID)
	if intersects(refIDs, col0) {
		return rosterFirstColumnID
	}
	inCol0, inCol1 := intersects(refNames, col0), intersects(refNames, col1)
	switch {
	case inCol1 && !inCol0:
		return rosterFirstColumnID
	case inCol0 && !inCol1:
		return rosterFirstColumnName
	}
	return rosterFirstColumnUnknown
}

func intersects(a, b map[string]bool) bool {
	for k := range a {
		if b[k] {
			return true
		}
	}
	return false
}

// evidenceDojoByName is the dojo the DRAW records for each competitor name.
// Only pools.csv carries a dojo; match rows and bracket sides name a side and
// nothing else.
func (s *Store) evidenceDojoByName(compID string) map[string]string {
	out := map[string]string{}
	pools, err := s.loadPoolsLocked(compID)
	if err != nil {
		return out
	}
	for _, pool := range pools {
		for _, p := range pool.Players {
			if n := strings.TrimSpace(p.Name); n != "" && strings.TrimSpace(p.Dojo) != "" {
				out[helper.NormalizeParticipantName(n)] = p.Dojo
			}
		}
	}
	return out
}

// rosterParseCorroborated guards the MINT path against a second legacy
// ambiguity, one that column 0 says nothing about: the ROW SHAPE.
//
// The original writer emitted "Name, DisplayName, Dojo" whenever the display
// name differed from the name -- which SanitizeName makes true for almost
// every competitor ("Rin Sato" -> "R. SATO") -- and "Name, Dojo" otherwise.
// Both shapes in one file. A NON-zekken competition's parser reads three
// fields as [Name, Dojo, Metadata...], so such a row loads TODAY with the
// display name as the dojo and the real dojo pushed into metadata.
//
// Reading it wrong is survivable: the bytes still hold the truth. REWRITING
// it from that parse is not -- the file then encodes Dojo="R. SATO" and the
// mistake stops being recoverable. Measured on exactly this fixture before
// the check existed.
//
// So a wide non-zekken row is minted only when the draw corroborates the
// dojo the parse produced. No corroboration, no rewrite; the roster keeps
// its operator notice instead, which is the same answer the column-0
// question gives when nothing can prove it.
func (s *Store) rosterParseCorroborated(compID string, withZekken bool, rows [][]string, players []domain.Player) bool {
	if withZekken {
		return true // this parser reads both legacy shapes correctly
	}
	wide := false
	for _, rec := range rows {
		if len(rec) >= 3 {
			wide = true
			break
		}
	}
	if !wide {
		return true // [Name, Dojo] only: no shape to mistake
	}
	dojoByName := s.evidenceDojoByName(compID)
	if len(dojoByName) == 0 {
		return false
	}
	for _, p := range players {
		want, ok := dojoByName[helper.NormalizeParticipantName(p.Name)]
		if !ok {
			return false // a row the draw cannot vouch for
		}
		if helper.NormalizeParticipantName(want) != helper.NormalizeParticipantName(p.Dojo) {
			return false // the parse disagrees with the draw: wrong shape
		}
	}
	return true
}

// upgradeParticipantIDsLocked is the participants.csv storage-format repair,
// and it runs FIRST in EnsureLegacyUpgraded because every step after it
// resolves against the roster's ids: pools.csv, the pool-match and bracket
// side ids, the squad keys, the lineup member ids. While the roster has none,
// all of them copy an empty string and the competition's matches count for
// nobody, since competitors are resolved BY ID.
//
// Two repairs, one question (see rosterFirstColumn):
//
//   - column 0 IS an id: the file was always right and only the flag is
//     wrong -- the deferred has_participant_ids flip that failed after the
//     roster save succeeded. Flip it. Nothing is minted and no id is
//     replaced, which is the whole point of preferring this over a rewrite.
//
//   - column 0 is a NAME: a genuine pre-id roster. Mint an id per row and
//     rewrite, using the roster as it PARSED, which for this case is the
//     correct parse.
//
//   - neither provable: leave the file untouched and log it. The operator
//     notice (helper.MissingParticipantIDsMessage) keeps naming the roster.
func (s *Store) upgradeParticipantIDsLocked(compID string, roster *legacyUpgradeRoster) error {
	comp, err := roster.competition()
	if err != nil || comp == nil {
		return err
	}
	if comp.HasParticipantIDs {
		return nil // the flag is already authoritative; the loader strips column 0
	}
	rows, err := helper.ReadCSVFile(s.compPath(compID, "participants.csv"))
	if err != nil {
		if os.IsNotExist(err) {
			return nil
		}
		return err
	}
	if len(rows) == 0 {
		return nil
	}

	switch s.classifyRosterFirstColumn(compID, rows) {
	case rosterFirstColumnID:
		return s.repairParticipantIDFlagLocked(compID, comp, roster)
	case rosterFirstColumnName:
		return s.mintRosterParticipantIDsLocked(compID, comp, roster, rows)
	default:
		log.Printf("state: %s: participants.csv carries no id column and nothing on disk can say whether column 0 is an id or a name; left untouched", compID)
		return nil
	}
}

// repairParticipantIDFlagLocked writes has_participant_ids=true for a roster
// whose first column is proven to be an id. The bytes of participants.csv are
// not touched: they were never wrong.
func (s *Store) repairParticipantIDFlagLocked(compID string, comp *Competition, roster *legacyUpgradeRoster) error {
	updated := *comp
	updated.HasParticipantIDs = true
	if _, err := s.saveCompetitionChangedLocked(&updated, s.directWrite); err != nil {
		return fmt.Errorf("participant-id flag not repaired: %w", err)
	}
	// saveCompetitionChangedLocked invalidates the participant caches itself
	// (the loader derives its id-strip decision from this flag), and the
	// roster this pass already read is the PRE-flip, column-shifted parse.
	roster.reset()
	log.Printf("state: %s: participants.csv has an id column its has_participant_ids flag did not record; flag repaired, no id replaced", compID)
	return nil
}

// mintRosterParticipantIDsLocked gives every row an id and rewrites the file.
//
// Writes DIRECTLY (marshalParticipantsCSV + atomicWrite) rather than through
// SaveParticipants, because loading is deliberately tolerant where saving is
// strict: ErrBlankDojo refuses every roster save precisely so a legacy roster
// can be LOADED and repaired. Routing a load through the strict path would
// refuse this for exactly the rosters that need it, and take the load with it.
//
// The column layout comes from the roster's own withZekken, never guessed.
func (s *Store) mintRosterParticipantIDsLocked(compID string, comp *Competition, roster *legacyUpgradeRoster, rows [][]string) error {
	players, err := roster.rosterPlayers()
	if err != nil || len(players) == 0 {
		return err
	}
	if !s.rosterParseCorroborated(compID, roster.withZekken, rows, players) {
		log.Printf("state: %s: participants.csv has rows whose shape the draw does not corroborate (the original writer's \"Name, DisplayName, Dojo\" form); left untouched rather than rewritten from a parse that may put the display name in the dojo", compID)
		return nil
	}
	stamped := make([]domain.Player, len(players))
	copy(stamped, players)
	minted := 0
	for i := range stamped {
		if helper.ParticipantIDMissing(stamped[i].ID) {
			stamped[i].ID = newParticipantID()
			minted++
		}
	}
	if minted == 0 {
		return nil
	}
	data, err := marshalParticipantsCSV(stamped, roster.withZekken)
	if err != nil {
		return err
	}
	if err := s.atomicWrite(s.compPath(compID, "participants.csv"), data, 0600); err != nil {
		return err
	}
	s.invalidateParticipantCaches(compID)
	roster.adoptStampedPlayers(stamped)

	updated := *comp
	updated.HasParticipantIDs = true
	if _, err := s.saveCompetitionChangedLocked(&updated, s.directWrite); err != nil {
		// The ids are on disk and every one is a UUID, so the loader's own
		// auto-detect still reads them: the narrow window ParticipantIDsHint
		// documents. Report it; the repair itself landed.
		return fmt.Errorf("participant ids written but the has_participant_ids flag was not: %w", err)
	}
	comp.HasParticipantIDs = true
	log.Printf("state: %s: assigned participant ids to %d roster row(s) written before the id column existed", compID, minted)
	return nil
}

// adoptStampedPlayers replaces the roster this pass is working from after the
// mint rewrites participants.csv. The instance is shared by every step below
// and loaded at most once, so without this the five steps that resolve
// AGAINST the roster would keep matching on the id-less copy read before the
// stamp -- resolving nothing, for a whole extra load, with nothing logged.
func (r *legacyUpgradeRoster) adoptStampedPlayers(players []domain.Player) {
	r.loaded = true
	r.loadErr = nil
	r.players = players
	r.index = nil
	if len(players) > 0 {
		r.index = domain.NewRosterIndex(players)
	}
}

// mintedByCompetitor lets the PRE-WRITE call site (saveParticipantsNoLock)
// migrate a stored row that has no id of its own, under the id that same
// save is about to give it. It is keyed by helper.CompetitorKey's id-less
// form, the (name, dojo) pair that IS a competitor's identity wherever no
// id exists yet -- the documented carve-out, not a weakening of the id
// ruling, since the row being resolved carries no id field to resolve by.
// EnsureLegacyUpgraded passes nil: on a plain load there is no pending
// write to borrow an id from, so an id-less row still migrates to nothing.
func (s *Store) upgradeSquadsFromMetadataLocked(compID string, roster *legacyUpgradeRoster, mintedByCompetitor map[string]string) error {
	// FIRST, and inside this function rather than beside one of its callers.
	// This step MINTS blank members and writes team-members.yaml, which
	// permanently arms the filename migration's refuse-to-overwrite guard: seed
	// over an unadopted v2.0.0 squads.yaml and the operator's real members are
	// stranded under a name nothing looks for, with fresh ids that orphan every
	// lineup position and fought bout referencing the old ones.
	//
	// Both callers reach that damage through here -- EnsureLegacyUpgraded's
	// load hook AND saveParticipantsNoLock's pre-write call, which exists
	// precisely because the load hook misses a save that lands before anything
	// reads the competition -- so the migration belongs HERE, not at a call
	// site. Registering it beside one caller left the other open.
	//
	// A failed migration ABORTS rather than logging on: minting is the
	// irreversible half, and a transient fault (a permission blip, a
	// half-mounted volume) must cost a retry, not the members.
	// AFTER the kind gate below, not before it: this function's second caller is
	// saveParticipantsNoLock, the chokepoint EVERY roster write funnels through,
	// and an individual competition can never reach the seeding that needs the
	// migration. Probing the filesystem above that gate put two uncached
	// syscalls on every participant add, edit and check-in of a competition
	// that will never have a team member at all.
	comp, err := roster.competition()
	if err != nil || comp == nil {
		return err
	}
	if comp.Kind != "team" && comp.TeamSize == 0 {
		return nil
	}
	if err := s.upgradeTeamMembersFilenameLocked(compID, s.directWrite); err != nil {
		return err
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
		floor := squadFloor(comp.TeamSize)
		existing, alreadyMigrated := squads[id]
		if !alreadyMigrated {
			names := nonBlankMetadata(p.Metadata)
			members := make([]domain.TeamMember, 0, max(len(names), floor))
			for i, name := range names {
				members = append(members, domain.TeamMember{
					ID:    newParticipantID(),
					Index: i + 1,
					Name:  name,
				})
			}
			for i := len(members); i < floor; i++ {
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
		// pad up to a since-raised floor. Indices are always contiguous
		// 1..len(existing) -- see the doc comment above -- so the next slot's
		// index is simply len(existing)+1.
		if len(existing) < floor {
			for i := len(existing); i < floor; i++ {
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
	// Drop the shared lazy squad cache: this call just changed team-members.yaml,
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
// team's OWN squad (team-members.yaml) whenever the position's Name resolves to
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
		// A lineup already holding ONE member at two positions needs this pass
		// too, even with every position id-stamped, so the scan cannot stop at
		// "is an id missing". Such a row is refused by ValidatePositions on
		// every future write, so it is repaired here rather than blamed on the
		// next unrelated operator edit.
		seenIDs := make(map[string]struct{}, len(l.MemberIDs))
		for _, id := range l.MemberIDs {
			if id == "" {
				continue
			}
			if _, dup := seenIDs[id]; dup {
				needs = true
				break
			}
			seenIDs[id] = struct{}{}
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
		// The ids this lineup ALREADY fields, so no member ends up at two
		// positions -- neither one carried over from disk, nor one this pass
		// would otherwise create.
		//
		// The creation half is the sharp one. The backfill below resolves by
		// NAME, so a lineup naming one person at two positions used to stamp
		// the SAME id onto both. A lineup may not hold one member twice
		// (ValidatePositions refuses it), so this pass manufactured a row the
		// server then rejected -- on the operator's next unrelated edit, for
		// damage the operator never made, which is precisely the "a write
		// answers for what it introduces, not for what it inherited" rule.
		//
		// The carry-over half repairs what older releases left: the duplicate
		// is cleared HERE, on load, so the write-time guard only ever sees what
		// a write introduced. The position keeps its NAME, so nothing changes
		// on screen and the operator's next save re-resolves it.
		//
		// Sorted, because which of the two positions keeps the id must not
		// depend on Go's randomised map order: the same file would otherwise
		// repair differently on two loads.
		used := make(map[string]struct{}, len(l.MemberIDs))
		for _, pos := range slices.Sorted(maps.Keys(l.MemberIDs)) {
			id := l.MemberIDs[pos]
			if id == "" {
				continue
			}
			if _, dup := used[id]; dup {
				delete(l.MemberIDs, pos)
				lineupChanged = true
				log.Printf("state: lineup repair for %s: member %s was at two positions; cleared the id at %q, its name is kept", compID, id, pos)
				continue
			}
			used[id] = struct{}{}
		}
		for _, pos := range slices.Sorted(maps.Keys(l.Positions)) {
			name := l.Positions[pos]
			if name == "" || l.MemberIDs[pos] != "" {
				continue
			}
			id := squadMemberIDByName(squads, l.TeamID, name)
			if id == "" {
				continue
			}
			if _, dup := used[id]; dup {
				continue // already fielded elsewhere; leave this row id-less
			}
			if l.MemberIDs == nil {
				l.MemberIDs = map[domain.Position]string{}
			}
			l.MemberIDs[pos] = id
			used[id] = struct{}{}
			lineupChanged = true
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

// upgradeTeamMembersFilenameLocked moves a competition recorded by v2.0.0 from
// squads.yaml onto team-members.yaml, re-keying the document's root from
// `squads` to `members`. Caller holds compID's per-competition write lock.
//
// bc-dnst renamed both the file and its key. Without this, a tournament
// written by the last release opens with every team showing numbered slots and
// no names: the members are on disk but under a name nothing looks for. That
// is data loss on upgrade, which is what the operator's rule ("a storage
// change carries a migration path on load from the last two releases") is for.
// v1.1.0 and earlier had no team-member storage at all, so v2.0.0's shape is
// the entire history to carry.
//
// Deliberately NOT a dual-read at the load path. A one-time convergence keeps
// exactly one shape live afterwards, so no reader has to know two names
// forever, and it matches how every other step in this file works.
//
// Takes the writer rather than reaching for atomicWriteFile, the same seam
// saveSquadsLocked already has: the ordering below (write, THEN remove) is
// the crash-safety story, and a test can only prove it by making the write
// fail while the remove would have succeeded.
//
// Refuses to overwrite: if team-members.yaml already exists, this competition
// has been migrated (or was written new) and the stale squads.yaml is left
// alone rather than allowed to win. The old file is REMOVED only after the new
// one is safely written, so a crash between the two leaves the original intact
// and the next load simply retries.
func (s *Store) upgradeTeamMembersFilenameLocked(compID string, write writeFn) error {
	newPath := s.compPath(compID, teamMembersFilename)
	if _, err := os.Stat(newPath); err == nil {
		return nil // already on the current name
	} else if !os.IsNotExist(err) {
		return err
	}
	oldPath := s.compPath(compID, legacySquadsFilename)
	data, err := os.ReadFile(oldPath) // #nosec G304, compPath enforces containment under the competitions dir.
	if err != nil {
		if os.IsNotExist(err) {
			return nil // nothing recorded by an older release either
		}
		return err
	}
	members, err := parseLegacySquadsBytes(data)
	if err != nil {
		// Named, and naming the remedy, because this error REFUSES every squad
		// read and write for the competition: loadSquadsLocked hard-fails on
		// it, so the three team-member endpoints answer 500 until it clears,
		// and a corrupt file never clears on its own. That is the safe
		// direction rather than an oversight. Reading past it reports the team
		// as having no members, and the next whole-file write then strands the
		// real ones under a name nothing looks for, with ids that orphan every
		// lineup position and fought bout. A refusal is recoverable; that
		// write is not. The remedy is on disk, so the message says which file.
		return fmt.Errorf("competition %s: %s cannot be parsed, so its team members cannot be adopted onto %s; repair or remove that file: %w",
			compID, legacySquadsFilename, teamMembersFilename, err)
	}
	if members == nil {
		// Parsed, but carries no `squads` key: not v2.0.0's shape. Leave both
		// files alone rather than writing an empty member list over nothing.
		return nil
	}
	if err := s.saveSquadsLocked(compID, members, write); err != nil {
		return err
	}
	if err := os.Remove(oldPath); err != nil && !os.IsNotExist(err) {
		// The members are safe on the new name; a leftover old file is
		// cosmetic and must not fail the load.
		log.Printf("state: team-members migration for %s left %s in place: %v", compID, legacySquadsFilename, err)
	}
	return nil
}

package engine

import (
	"fmt"
	"log"
	"strings"

	"github.com/gitrgoliveira/bracket-creator/internal/domain"
	"github.com/gitrgoliveira/bracket-creator/internal/helper"
	"github.com/gitrgoliveira/bracket-creator/internal/state"
)

// NumberPlayoffsOnlyParticipants is the ONE derivation of an effective-
// playoffs competition's numbers (bc-pnum A8, tightened by [review] and
// again in the bc-pnum review): participant order under NumberPrefix, through
// helper.AssignPlayerNumbers (R1), mutating players in place. No-op when
// the competition has no prefix, so a caller does not have to special-case
// that itself.
//
// Package-level, not a method: its body never reads the receiver, and
// mobileapp threaded a whole consumer-boundary interface
// (PlayoffsNumberingEngine, deps.go) plus an extra `eng` parameter through
// six signatures (mergePoolNumbersIntoPlayersSlice and five callers) solely
// to reach this one call. A plain function call is the same "one shared
// derivation, not two independent call sites" guarantee the interface
// existed for, with none of the plumbing: mobileapp already imports engine
// directly.
//
// Both real callers reach it through this SAME function rather than each
// calling helper.AssignPlayerNumbers independently: the public
// viewer/display merge (mobileapp.mergePoolNumbersIntoPlayersSlice's
// playoffs-only branch) and NumberedParticipantsFor below (the
// blank-template export's caller, which has no pools.csv to read a Number
// from for a playoffs-only competition, see RenumberCompetitors's own doc
// comment on why "no pools file" happens for playoffs). One function, not
// two independent call sites invoking the shared primitive, is what
// actually prevents the two surfaces from silently disagreeing -- merely
// routing both through the same low-level helper.AssignPlayerNumbers call
// left room for one of the two call sites to drift (a different load
// order, a different options struct) without either side inherently
// noticing.
func NumberPlayoffsOnlyParticipants(comp *state.Competition, players []domain.Player) {
	if prefix := comp.EffectiveNumberPrefix(); prefix != "" {
		helper.AssignPlayerNumbers(players, prefix, 1)
	}
}

// NumberedParticipantsFor returns comp's roster, loaded fresh, with Number
// composed by NumberPlayoffsOnlyParticipants above. Used by the
// blank-template export, which (unlike the viewer/display merge) has no
// already-loaded roster to mutate in place.
func (e *Engine) NumberedParticipantsFor(comp *state.Competition) ([]domain.Player, error) {
	players, err := e.store.LoadParticipantsOpt(comp.ID, comp.EffectiveWithZekkenName(), state.LoadParticipantsOpts{WithSeeds: false, HasIDs: comp.ParticipantIDsHint()})
	if err != nil {
		return nil, err
	}
	NumberPlayoffsOnlyParticipants(comp, players)
	return players, nil
}

// PlayoffsNamesToPrint is the ONE derivation of RenderCompetitionWorkbook's
// namesToPrintPlayers argument (see that parameter's own doc comment in
// workbook.go): a playoffs-only competition never has a pools.csv, so
// nothing else populates the shared pipeline's Data / Names-to-Print sheets
// for it, and a prefix is what tells the pipeline there is a numbered
// roster worth printing at all.
//
// Shared by both workbook builders -- Engine.ExportCompetitionXlsx
// (blank-template) and export.BuildResultsWorkbook (results archive) -- so
// the two exports of one competition agree on whether the Names to Print
// sheet exists. Before this was extracted, only the blank-template export
// derived it and the results export always passed nil, so a knockout-only
// competition's results workbook was silently missing the sheet the
// blank-template export had -- every such competition carries a prefix
// (comp.NumberPrefix is never left blank once a competition is created), so
// the gap was not a rare edge case but the ordinary shape.
//
// pools is the caller's own already-loaded roster, not re-read here: both
// callers load it earlier for their own needs (rendering the pool sheets),
// and passing it in keeps this function pure over its inputs rather than
// hitting the store a second time.
//
// Returns (nil, nil) -- not an error -- when the competition is not this
// shape, so a caller can feed the result straight into
// RenderCompetitionWorkbook without a branch of its own.
func (e *Engine) PlayoffsNamesToPrint(comp *state.Competition, pools []helper.Pool) ([]helper.Player, error) {
	if comp.EffectiveFormat() != state.CompFormatPlayoffs || len(pools) != 0 || comp.EffectiveNumberPrefix() == "" {
		return nil, nil
	}
	return e.NumberedParticipantsFor(comp)
}

// RenumberCompetitors rewrites every competitor's Number from the competition's
// CURRENT NumberPrefix, so changing that prefix in Settings does not require
// discarding and regenerating the draw. Pool membership and draw order are
// untouched: this only restates each competitor's label.
//
// It is idempotent and unconditional: callers run it after every successful
// settings save, whether or not that save touched NumberPrefix. It snapshots
// the stored Number of every competitor, runs helper.NumberPools over the
// pools in place (the same primitive that numbers a fresh draw), and compares
// the two -- writing pools.csv back only when at least one Number actually
// differs. A save that left the prefix untouched is therefore a cheap
// read-compare-discard, a save that moved it rewrites the file, and a retry
// after a prior renumber failed (or a legacy pools.csv whose Number column
// was never populated, see G7) is healed by the very next save with no extra
// code.
//
// pools.csv column 7 is the only persisted home of Player.Number (BracketMatch
// has no such field and participants.csv does not persist it), so rewriting the
// pools file renumbers every surface. "No pools file" is not the same thing as
// "playoffs-only": a playoffs competition never has one (its numbers are
// composed from participant order on read, see
// mobileapp.mergePoolNumbersIntoPlayers); a mixed or league competition that
// has not been drawn YET has none either, because SavePools has not run; and a
// Swiss competition NEVER has one (its draw writes rounds to pool-matches.csv
// and never calls SavePools), so Swiss competitors carry no number on any
// surface and this call is a permanent no-op for them, a pre-existing gap this
// bead names rather than closes. In every case there is nothing on disk to
// rewrite, so this returns (false, nil).
//
// The load and the save share one WithTransaction, so a concurrent score write
// cannot interleave between reading the pools and writing them back.
//
// Returns whether pools.csv was actually rewritten, so a caller that only
// broadcasts a change notification when something changed (the settings PUT
// handler) does not have to re-derive that from its own before/after
// comparison -- this function already computed it to decide whether to save.
func (e *Engine) RenumberCompetitors(compID string) (bool, error) {
	var changed bool
	err := e.store.WithTransaction(compID, func(tx state.StoreTx) error {
		comp, err := tx.LoadCompetition(compID)
		if err != nil {
			return fmt.Errorf("renumber %s: load competition: %w", compID, err)
		}
		if comp == nil {
			return notFoundErrorf("competition %s not found", compID)
		}

		pools, err := tx.LoadPools(compID)
		if err != nil {
			return fmt.Errorf("renumber %s: load pools: %w", compID, err)
		}
		if len(pools) == 0 {
			return nil
		}

		// bc-pnum A3: a blank prefix must never reach helper.NumberPools, which
		// documents (and relies on) the app never handing it an empty one --
		// NumberPools/AssignPlayerNumbers would happily compose bare "1","2","3"
		// (no letters at all) into pools.csv. Every caller of this function is
		// supposed to have already assigned a default (G2), but this refuses
		// outright rather than trusting that invariant silently: a bug state
		// upstream (a stored blank prefix an inherit step missed) must never
		// WRITE, it must surface.
		// A blank or whitespace-only prefix must never reach the composition
		// below; EffectiveNumberPrefix is the one trimmed value the guard and
		// the call both bind, so they cannot disagree.
		prefix := comp.EffectiveNumberPrefix()
		if prefix == "" {
			return fmt.Errorf("renumber %s: competition has no number prefix", compID)
		}

		total := 0
		for _, pool := range pools {
			total += len(pool.Players)
		}
		before := make([]string, 0, total)
		for _, pool := range pools {
			for _, p := range pool.Players {
				before = append(before, p.Number)
			}
		}

		helper.NumberPools(pools, prefix)

		i := 0
		for _, pool := range pools {
			for _, p := range pool.Players {
				if p.Number != before[i] {
					changed = true
				}
				i++
			}
		}
		if !changed {
			return nil
		}

		if err := tx.SavePools(compID, pools); err != nil {
			return fmt.Errorf("renumber %s: save pools: %w", compID, err)
		}
		return nil
	})
	return changed, err
}

// siblingCompetitions loads every competition except excludeID, tolerant of
// an unreadable one (log-and-skip) when tolerateUnreadable is true, or
// returning the first load error otherwise. The ONE sibling walk shared by
// takenNumberPrefixes, CheckUniqueCompFields and EnsureNumberPrefix (PR #416
// finding 1, moved from mobileapp's checkUniqueCompFieldsSiblingPolicy): a
// caller that needs both the taken-prefix set and the uniqueness check loads
// the sibling set once and feeds both from the same slice, instead of
// listing and loading every sibling twice.
//
// skipped carries the ids of any sibling this call could not read (only
// reachable when tolerateUnreadable is true), so a caller correlating an
// assignment with the exact siblings invisible to it (DefaultNumberPrefixFor,
// EnsureNumberPrefix) can log one warning naming both facts together instead
// of leaving them as two independent, uncorrelated log lines.
func (e *Engine) siblingCompetitions(excludeID string, tolerateUnreadable bool) (siblings []*state.Competition, skipped []string, err error) {
	ids, err := e.store.ListCompetitions()
	if err != nil {
		return nil, nil, fmt.Errorf("list competitions: %w", err)
	}
	siblings = make([]*state.Competition, 0, len(ids))
	for _, id := range ids {
		if id == excludeID {
			continue
		}
		comp, err := e.store.LoadCompetition(id)
		if err != nil {
			if !tolerateUnreadable {
				return siblings, skipped, fmt.Errorf("load competition %s: %w", id, err)
			}
			log.Printf("engine: siblingCompetitions: skipping unreadable competition %s: %v", id, err)
			skipped = append(skipped, id)
			continue
		}
		if comp != nil {
			siblings = append(siblings, comp)
		}
	}
	return siblings, skipped, nil
}

// takenNumberPrefixes returns the number prefix of every competition except
// excludeID, the set helper.DefaultNumberPrefix avoids. Unexported (bc-pnum
// C6): its only caller is DefaultNumberPrefixFor in this same file, the ONE
// exported door onto this derivation (R6) -- callers that must not race a
// concurrent create hold Store.WithCompetitionRenameLock around THAT call,
// as the mobileapp handlers do, so there is no reason for this helper to be
// reachable on its own. (MigrateNumberPrefixes does not call it either: it
// derives over a taken set it builds itself, because that set must also grow
// with the prefixes it assigns during its own pass.)
//
// An unreadable sibling is tolerated (bc-pnum A5(d)): this feeds
// DefaultNumberPrefix, a best-effort SUGGESTION (see its own doc comment),
// never the uniqueness guarantee -- that is CheckUniqueCompFields's job, and
// it still refuses a genuine collision. Propagating the first sibling's load
// error here would turn ONE unrelated competition's bad file into a 500 for
// every OTHER competition trying to derive a prefix, including the
// start/generate-draw pre-flight, which cannot defer the way create/import
// can. GET /competitions and MigrateNumberPrefixes already apply the same
// "one bad cell cannot stop a tournament" rule; this matches it.
func (e *Engine) takenNumberPrefixes(excludeID string) (taken []string, skipped []string, err error) {
	siblings, skipped, err := e.siblingCompetitions(excludeID, true)
	if err != nil {
		return nil, nil, err
	}
	taken = make([]string, 0, len(siblings))
	for _, comp := range siblings {
		taken = append(taken, comp.NumberPrefix)
	}
	return taken, skipped, nil
}

// DefaultNumberPrefixFor is the ONE server-side derivation of a competition's
// default number prefix (R6): helper.DefaultNumberPrefix over the prefixes
// every other competition holds. Create, settings, import, the
// start/generate-draw pre-flight and the preview endpoint all resolve through
// here, so what any of them assigns is what the others would have; the
// load-time migration applies the same helper over its own in-pass set (see
// MigrateNumberPrefixes).
//
// The tolerance for an unreadable sibling (log-and-skip, never refuse -- see
// takenNumberPrefixes) is unchanged; when it actually happened for THIS
// derivation, the returned prefix is a best-effort suggestion made blind to
// that sibling's own prefix, so it is logged as ONE correlated warning
// naming the assignment and the skipped ids together, rather than leaving
// the two facts to land as unconnected log lines an operator has to
// manually cross-reference (the bc-pnum review). The exported signature is
// unchanged: every caller (mobileapp's create/settings/import/preview
// handlers) needs no adaptation.
func (e *Engine) DefaultNumberPrefixFor(name, excludeID string) (string, error) {
	taken, skipped, err := e.takenNumberPrefixes(excludeID)
	if err != nil {
		return "", err
	}
	prefix := helper.DefaultNumberPrefix(name, taken)
	if len(skipped) > 0 {
		log.Printf("engine: DefaultNumberPrefixFor: assigned prefix %q to %q while siblings %v were unreadable; check for a collision once they load",
			prefix, name, skipped)
	}
	return prefix, nil
}

// checkPrefixAgainstSiblings is the pure name/prefix collision check over an
// already-loaded sibling list: the walk moved from mobileapp's
// checkUniqueCompFieldsSiblingPolicy (PR #416 finding 1), split out so a
// caller that already has the sibling set loaded for another reason
// (EnsureNumberPrefix's own derivation) can validate against it without a
// second store pass. Either name or prefix may be empty to exempt that field.
// "K2" is not EQUAL to "K", but AssignPlayerNumbers's prefix+counter
// concatenation makes them ambiguous ("K"'s 21st entrant and "K2"'s 1st
// entrant both print "K21"), so that is refused with the same shape as an
// exact match.
func checkPrefixAgainstSiblings(siblings []*state.Competition, name, prefix string) error {
	prefix = strings.TrimSpace(prefix)
	for _, existing := range siblings {
		if name != "" && strings.EqualFold(existing.Name, name) {
			return validationErrorf("competition name %q already exists", name)
		}
		if prefix == "" || existing.NumberPrefix == "" {
			continue
		}
		existingPrefix := strings.TrimSpace(existing.NumberPrefix)
		if strings.EqualFold(existingPrefix, prefix) {
			return validationErrorf("number prefix %q already used by competition %q", prefix, existing.Name)
		}
		if helper.NumberPrefixesAmbiguous(existingPrefix, prefix) {
			return validationErrorf("number prefix %q is ambiguous with %q, already used by competition %q", prefix, existingPrefix, existing.Name)
		}
	}
	return nil
}

// CheckUniqueCompFields verifies that name and prefix are both unique across
// every OTHER competition (excludeID excluded). Moved from mobileapp's
// checkUniqueCompFieldsSiblingPolicy (PR #416 finding 1); mobileapp's
// checkUniqueCompFields / checkUniqueCompFieldsTolerant are now thin wrappers
// over this.
//
// Both fields may be empty to exempt them from the check; when BOTH are
// empty nothing is validated and the sibling set is never even loaded, so a
// caller that validates only what it moved and moved neither field pays no
// sibling-load cost.
//
// tolerateUnreadableSibling selects siblingCompetitions' policy: STRICT
// (false) is for create/import, which can be retried by the operator, so
// silently skipping a sibling and letting a genuine collision through would
// be the wrong trade; TOLERANT (true) is for a caller that cannot defer the
// way create/import can (the start/generate-draw pre-flight).
//
// Returns the ids of any sibling this call could not read (only possible
// under the tolerant policy) and a single error: a collision is a
// *ValidationError, an infrastructure fault (the list/load itself failing
// under the strict policy) is a plain error.
func (e *Engine) CheckUniqueCompFields(name, prefix, excludeID string, tolerateUnreadableSibling bool) (skipped []string, err error) {
	prefix = strings.TrimSpace(prefix)
	if name == "" && prefix == "" {
		return nil, nil
	}
	siblings, skipped, err := e.siblingCompetitions(excludeID, tolerateUnreadableSibling)
	if err != nil {
		return skipped, err
	}
	return skipped, checkPrefixAgainstSiblings(siblings, name, prefix)
}

// EnsureNumberPrefix is the ONE engine-level implementation of the derive ->
// validate -> persist -> renumber sequence that guarantees a competition
// never reaches a draw without a number prefix (G2). Both the mobileapp
// start/generate-draw pre-flight (ensureNumberPrefix, handlers_competition.go)
// and runDrawPipeline's own backstop for non-HTTP callers route through this
// single function, so the two cannot drift (PR #416 finding 1).
//
// allowed gates the action exactly the way the caller's own subsequent
// engine call will (CanStart/CanGenerateDraw): a competition in any other
// status is left completely untouched -- no assignment, no renumber -- and
// (false, nil) is returned so the caller's own action reports the refusal on
// its own terms.
//
// A stored, non-blank prefix skips the assignment step; the renumber below
// still always runs (RenumberCompetitors is a cheap read-compare-discard
// when nothing differs), so a retry after an earlier renumber failure heals
// itself with no extra code, whether or not THIS call assigned anything.
//
// tolerateUnreadableSiblings selects siblingCompetitions' policy for BOTH the
// derivation's taken-prefix set and the post-derivation uniqueness check,
// loaded ONCE and shared between them rather than listing and loading every
// sibling twice: neither caller of this function can defer the way
// create/import can, so an unrelated sibling's unreadable config.md is
// logged and skipped rather than refused.
//
// Returns (assigned, err): assigned is true only once a NEW prefix has
// actually been SAVED this call, so a caller whose subsequent action then
// fails still knows config.md/pools.csv may have already changed.
func (e *Engine) EnsureNumberPrefix(compID string, allowed func(state.CompetitionStatus) bool, tolerateUnreadableSiblings bool) (assigned bool, err error) {
	var assignedPrefix string
	var skipRenumber bool
	err = e.store.WithCompetitionRenameLock(func() error {
		_, updateErr := e.store.UpdateCompetitionChanged(compID, func(current *state.Competition) (*state.Competition, error) {
			if current == nil || !allowed(current.Status) {
				skipRenumber = true
				return nil, nil
			}
			if strings.TrimSpace(current.NumberPrefix) != "" {
				return nil, nil
			}
			siblings, skipped, serr := e.siblingCompetitions(compID, tolerateUnreadableSiblings)
			if serr != nil {
				return nil, serr
			}
			taken := make([]string, 0, len(siblings))
			for _, sib := range siblings {
				taken = append(taken, sib.NumberPrefix)
			}
			prefix := helper.DefaultNumberPrefix(current.Name, taken)
			if verr := checkPrefixAgainstSiblings(siblings, "", prefix); verr != nil {
				return nil, verr
			}
			if len(skipped) > 0 {
				log.Printf("engine: EnsureNumberPrefix %s: assigned prefix %q while skipping unreadable sibling(s) %v from the uniqueness check; verify no collision once they load", compID, prefix, skipped)
			}
			current.NumberPrefix = prefix
			assigned = true
			assignedPrefix = prefix
			return current, nil
		})
		if updateErr != nil {
			return updateErr
		}
		if skipRenumber {
			return nil
		}
		if _, err := e.RenumberCompetitors(compID); err != nil {
			if assigned {
				// This write's own damage: the assignment just above is what
				// made this call reachable, so a renumber failure right after
				// it is reported, not swallowed.
				return fmt.Errorf("competition %s: prefix %q assigned but competitors could not be numbered: %w", compID, assignedPrefix, err)
			}
			// Inherited damage: a pools.csv already broken (or a renumber
			// that failed on an earlier call, before the prefix was saved) is
			// not something THIS call introduced. Log and proceed so the
			// caller is not blocked over pre-existing pools.csv damage; the
			// very next settings save heals it.
			log.Printf("engine: EnsureNumberPrefix %s: competitors not numbered (inherited damage, not this call's own): %v", compID, err)
		}
		return nil
	})
	return assigned, err
}

// MigrateNumberPrefixes brings a tournament-data folder written before the
// never-empty-prefix rule up to it at LOAD time: every competition whose
// stored prefix is empty is given the derived default (unique against the
// rest, including the ones assigned earlier in this same pass), and every
// prefixed competition's pools.csv is numbered under its prefix. The
// mobile-app command runs it once at startup, after the store's own WAL
// replay and before serving, so no request ever meets a legacy competition
// (operator ruling 2026-09-03: migrate on load, not on the next save).
// Returns the ids whose prefix it assigned, in the store's listing order; a
// second run assigns none.
//
// It can never stop the app from starting. One competition that will not
// load (a stray folder under competitions/, an unparseable config.md) or one
// pools.csv that will not parse is that competition's problem, logged with
// the reason, and the pass carries on with the rest: the same availability
// rule the competition list and the viewer payload apply per competition
// ("one bad cell cannot stop a tournament"), at the point where it matters
// most, a restart mid-event. It is also resumable: the numbering step runs
// for EVERY prefixed competition, not only the ones assigned in this pass,
// and RenumberCompetitors is a read-compare-skip when nothing differs, so a
// competition whose numbering failed on an earlier start (prefix saved,
// pools.csv still blank) is picked up again on the next one. A settings save
// heals it in between (G4).
//
// Holds the competition-rename lock for the whole pass for the same reason
// the create and settings handlers hold it around their derivation: the taken
// set must not move under it. At startup nothing else is running, but the
// lock costs nothing and keeps the invariant local to this function. Only a
// failure to LIST the competitions is returned, since then nothing at all can
// be migrated.
func (e *Engine) MigrateNumberPrefixes() ([]string, error) {
	var migrated []string
	err := e.store.WithCompetitionRenameLock(func() error {
		ids, err := e.store.ListCompetitions()
		if err != nil {
			return fmt.Errorf("list competitions: %w", err)
		}
		var taken []string
		var pending []*state.Competition
		var prefixed []string
		// PR #416 finding 9: prefixByID lets the final renumbering loop name
		// WHICH prefix a rewrite happened under, so the log correlates the
		// assignment with its effect instead of reporting the two facts with
		// nothing tying them together.
		prefixByID := make(map[string]string)
		for _, id := range ids {
			comp, err := e.store.LoadCompetition(id)
			if err != nil {
				log.Printf("engine: number-prefix migration: skipping %s: %v", id, err)
				continue
			}
			if comp == nil {
				continue
			}
			if strings.TrimSpace(comp.NumberPrefix) == "" {
				pending = append(pending, comp)
				continue
			}
			taken = append(taken, comp.NumberPrefix)
			prefixed = append(prefixed, id)
			prefixByID[id] = comp.NumberPrefix
		}
		for _, comp := range pending {
			prefix := helper.DefaultNumberPrefix(comp.Name, taken)
			taken = append(taken, prefix)
			// the bc-pnum review: LoadCompetition (in the scan loop above) +
			// a later whole-struct SaveCompetition left a load/save race
			// window a concurrent writer could land a DIFFERENT field change
			// into, which this save would then clobber with the stale
			// snapshot. UpdateCompetitionChanged re-reads under the
			// per-competition lock and writes back the SAME loaded value it
			// mutated, closing that window; the no-op guard (current==nil or
			// already prefixed) mirrors the "pending" gate the caller already
			// applied, in case that raced too.
			changed, err := e.store.UpdateCompetitionChanged(comp.ID, func(current *state.Competition) (*state.Competition, error) {
				if current == nil || strings.TrimSpace(current.NumberPrefix) != "" {
					return nil, nil
				}
				current.NumberPrefix = prefix
				return current, nil
			})
			if err != nil {
				log.Printf("engine: number-prefix migration: %s: prefix %q not saved: %v (retried on the next start)", comp.ID, prefix, err)
				continue
			}
			if !changed {
				continue
			}
			migrated = append(migrated, comp.ID)
			prefixed = append(prefixed, comp.ID)
			prefixByID[comp.ID] = prefix
		}
		for _, id := range prefixed {
			renumbered, err := e.RenumberCompetitors(id)
			if err != nil {
				log.Printf("engine: number-prefix migration: %s: competitors not numbered: %v (retried on the next start; a settings save heals it too)", id, err)
			} else if renumbered {
				log.Printf("engine: number-prefix migration: %s: competitor numbers rewritten under prefix %q", id, prefixByID[id])
			}
		}
		return nil
	})
	return migrated, err
}

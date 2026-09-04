package engine

import (
	"fmt"
	"log"
	"strings"

	"github.com/gitrgoliveira/bracket-creator/internal/domain"
	"github.com/gitrgoliveira/bracket-creator/internal/helper"
	"github.com/gitrgoliveira/bracket-creator/internal/state"
)

// NumberedParticipantsFor returns comp's roster with Number composed exactly
// the way mobileapp.mergePoolNumbersIntoPlayersSlice's playoffs-only branch
// derives it for the public viewer (bc-pnum A8, G8): participant order under
// NumberPrefix, through helper.AssignPlayerNumbers, the ONE composition both
// surfaces route through (R1). It exists so a second caller (the blank-
// template export, which has no pools.csv to read a Number from for a
// playoffs-only competition, see RenumberCompetitors's own doc comment on why
// "no pools file" happens for playoffs) cannot silently derive the number a
// different way and disagree with what the app already shows.
//
// Returns the roster with Number left as loaded (empty, since
// participants.csv never persists it) when the competition has no prefix,
// so a caller does not have to special-case that itself.
func (e *Engine) NumberedParticipantsFor(comp *state.Competition) ([]domain.Player, error) {
	players, err := e.store.LoadParticipantsOpt(comp.ID, comp.EffectiveWithZekkenName(), state.LoadParticipantsOpts{WithSeeds: false, HasIDs: comp.ParticipantIDsHint()})
	if err != nil {
		return nil, err
	}
	if comp.NumberPrefix != "" {
		helper.AssignPlayerNumbers(players, comp.NumberPrefix, 1)
	}
	return players, nil
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
		if strings.TrimSpace(comp.NumberPrefix) == "" {
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

		helper.NumberPools(pools, comp.NumberPrefix)

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
// An unreadable sibling (a stray folder, an unparseable config.md) is logged
// and SKIPPED rather than returned as an error (bc-pnum A5(d)): this feeds
// DefaultNumberPrefix, a best-effort SUGGESTION (see its own doc comment),
// never the uniqueness guarantee -- that is checkUniqueCompFields's job, and
// it still refuses a genuine collision. Propagating the first sibling's load
// error here used to turn ONE unrelated competition's bad file into a 500 for
// every OTHER competition trying to derive a prefix, including the
// start/generate-draw pre-flight, which cannot defer the way create/import
// can. GET /competitions and MigrateNumberPrefixes already apply the same
// "one bad cell cannot stop a tournament" rule; this matches it.
func (e *Engine) takenNumberPrefixes(excludeID string) ([]string, error) {
	ids, err := e.store.ListCompetitions()
	if err != nil {
		return nil, fmt.Errorf("list competitions: %w", err)
	}
	taken := make([]string, 0, len(ids))
	for _, id := range ids {
		if id == excludeID {
			continue
		}
		comp, err := e.store.LoadCompetition(id)
		if err != nil {
			log.Printf("engine: takenNumberPrefixes: skipping unreadable competition %s: %v", id, err)
			continue
		}
		if comp != nil {
			taken = append(taken, comp.NumberPrefix)
		}
	}
	return taken, nil
}

// DefaultNumberPrefixFor is the ONE server-side derivation of a competition's
// default number prefix (R6): helper.DefaultNumberPrefix over the prefixes
// every other competition holds. Create, settings, import, the
// start/generate-draw pre-flight and the preview endpoint all resolve through
// here, so what any of them assigns is what the others would have; the
// load-time migration applies the same helper over its own in-pass set (see
// MigrateNumberPrefixes).
func (e *Engine) DefaultNumberPrefixFor(name, excludeID string) (string, error) {
	taken, err := e.takenNumberPrefixes(excludeID)
	if err != nil {
		return "", err
	}
	return helper.DefaultNumberPrefix(name, taken), nil
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
		}
		for _, comp := range pending {
			comp.NumberPrefix = helper.DefaultNumberPrefix(comp.Name, taken)
			taken = append(taken, comp.NumberPrefix)
			if err := e.store.SaveCompetition(comp); err != nil {
				log.Printf("engine: number-prefix migration: %s: prefix %q not saved: %v (retried on the next start)", comp.ID, comp.NumberPrefix, err)
				continue
			}
			migrated = append(migrated, comp.ID)
			prefixed = append(prefixed, comp.ID)
		}
		for _, id := range prefixed {
			if _, err := e.RenumberCompetitors(id); err != nil {
				log.Printf("engine: number-prefix migration: %s: competitors not numbered: %v (retried on the next start; a settings save heals it too)", id, err)
			}
		}
		return nil
	})
	return migrated, err
}

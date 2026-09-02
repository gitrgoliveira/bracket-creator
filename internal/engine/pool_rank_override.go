package engine

import "github.com/gitrgoliveira/bracket-creator/internal/helper"

// lookupPoolRankOverride resolves a manual pool-rank override
// (state.Overrides.PoolRanks[poolName]) against a single competitor,
// identified by (id, name, dojo) exactly as a standings row carries them.
//
// It is the single read-side implementation of the override-identity scheme
// (bc-cse): overrides used to be keyed by bare player name
// (state.Overrides.PoolRanks[poolID][playerName]), so two same-name,
// different-dojo competitors in one pool shared a single override entry --
// a chusen (drawing-lots) result recorded for one was silently applied to
// the other. Writes now key on helper.CompetitorKey(id, name, dojo) (see
// mobileapp's PUT .../override-rank handler), so this function tries that
// key FIRST.
//
// Backward compatibility is deliberate and READ-ONLY: a legacy
// overrides.json written before this fix has no identity keys at all, only
// bare player names. Falling back to overrides[name] when the identity key
// misses means those files keep applying without any migration step, and a
// live tournament's in-flight overrides are never silently dropped by an
// upgrade landing mid-event. Legacy entries are NEVER rewritten in place: a
// fresh override for one member of a same-name pair only ever adds the new
// identity-keyed entry, it does not touch (or delete) whatever stale
// bare-name entry may already exist. Consequence, stated rather than hidden:
// if a legacy bare-name override already exists for a name shared by two
// competitors and only ONE of them later receives a fresh, identity-keyed
// override, the OTHER one still resolves via the untouched legacy name key --
// exactly the pre-fix ambiguity, now scoped to just that one remaining
// namesake instead of both. Closing that residual gap would require deleting
// or rewriting the legacy entry on write, which risks removing an override
// the operator still relies on for a different, unrelated pool; read-only
// compatibility was chosen as the simpler, non-destructive half of the
// "migrate on write vs read-only" choice CLAUDE.md's task brief calls out.
func lookupPoolRankOverride(overrides map[string]int, id, name, dojo string) (int, bool) {
	if len(overrides) == 0 {
		return 0, false
	}
	if rank, ok := overrides[helper.CompetitorKey(id, name, dojo)]; ok {
		return rank, true
	}
	rank, ok := overrides[name]
	return rank, ok
}

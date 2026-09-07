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
// the other. Writes key on helper.CompetitorKey(id, name, dojo) (see
// mobileapp's PUT .../override-rank handler, which now REQUIRES playerId,
// bc-pnum), so this function tries only that key.
//
// The legacy bare-name overrides[name] fallback (a pre-bc-cse overrides.json
// written before identity keys existed at all) has been removed (operator
// ruling bc-pnum): a record that carries an id field is resolved by id only,
// and a stale bare-name entry from before that scheme existed is not read
// back. An operator carrying such a file forward re-records the override
// through the current chusen/override-rank flow, which always writes the
// identity-keyed form.
func lookupPoolRankOverride(overrides map[string]int, id, name, dojo string) (int, bool) {
	if len(overrides) == 0 {
		return 0, false
	}
	rank, ok := overrides[helper.CompetitorKey(id, name, dojo)]
	return rank, ok
}

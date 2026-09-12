package engine

import "github.com/gitrgoliveira/bracket-creator/internal/helper"

// lookupPoolRankOverride resolves a manual pool-rank override
// (state.Overrides.PoolRanks[poolName]) against a single competitor,
// identified by participant id ONLY, exactly as SaveRankOverrideChanged
// writes it (helper.CompetitorKey(id, "", ""), which resolves to "id:"+id).
// A row without an id resolves to nothing (operator ruling bc-pnum): a
// legacy bare-name-keyed entry, from an overrides.json written before
// identity keys existed at all, is not read back by anyone. An operator
// carrying such a file forward re-records the override through the current
// chusen/override-rank flow, which always writes the identity-keyed form.
func lookupPoolRankOverride(overrides map[string]int, id string) (int, bool) {
	if len(overrides) == 0 || id == "" {
		return 0, false
	}
	rank, ok := overrides[helper.CompetitorKey(id, "", "")]
	return rank, ok
}

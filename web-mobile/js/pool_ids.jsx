// pool_ids.jsx: canonical parser for pool-match ids. LEAF module with NO
// imports, so both display_helpers.jsx and admin_pools.jsx can import it
// without pulling a transitive import chain into either: admin_pools.jsx
// otherwise relies on window globals rather than ESM imports, so a leaf with
// no dependencies keeps its module graph trivial. Single source of truth for
// the pool-id parse rule used across the display and admin surfaces.
//
// Backend id formats: "PoolName-N", "PoolName-DH-N" (daihyosen), "PoolName-TB-N"
// (tiebreaker). The non-greedy capture leaves hyphenated pool names intact
// ("Pool A-East-0" → "Pool A-East"); ids without a recognisable suffix yield "".
// CAVEAT: this matches ANY id ending in "-<digits>" (with an optional DH-/TB-),
// so non-pool ids are NOT rejected: e.g. a Swiss id "Swiss-R1-0" yields
// "Swiss-R1". Call sites that must distinguish a real pool from Swiss/other
// phases should gate on the competition format or a "Pool " prefix, not on a
// truthy poolNameOf() result alone (see findNextPoolOnCourt's "mixed" gate).
// This regex constant is intentionally module-private: callers use the
// exported poolNameOf() wrapper below, never the raw pattern.
// DAIHYOSEN_POSITION is the sentinel `position` value marking a sub-bout as
// the daihyosen (representative bout) rather than a numbered roster bout
// (real bouts use a non-negative position: fixed-format 0-based, kachinuki
// 1-based). Negative so it never collides with a real bout index.
// Mirrors state.DaihyosenSubPosition on the Go side. Use this instead of -1.
export const DAIHYOSEN_POSITION = -1;

const POOL_MATCH_ID_RE = /^(.*?)-(?:DH-|TB-)?\d+$/;

// poolNameOf: derive the pool name from a pool-match id (incl. DH/TB
// supplementary bouts). Returns "" when the id isn't pool-shaped.
export function poolNameOf(id) {
    if (typeof id !== "string") return "";
    return id.match(POOL_MATCH_ID_RE)?.[1] ?? "";
}

// This regex constant is intentionally module-private: callers use the
// exported isSupplementaryBout() wrapper below, never the raw pattern.
const SUPPLEMENTARY_BOUT_RE = /-(?:DH|TB)-\d+$/;

// isSupplementaryBout: true for a pool daihyosen ("…-DH-N") or tiebreaker
// ("…-TB-N") rep bout (a single individual ippon-shobu even in a team comp).
export function isSupplementaryBout(id) {
    return typeof id === "string" && SUPPLEMENTARY_BOUT_RE.test(id);
}

// This regex constant is intentionally module-private: callers use the
// exported isPoolDaihyosenBout() wrapper below, never the raw pattern.
const DAIHYOSEN_BOUT_RE = /-DH-\d+$/;

// isPoolDaihyosenBout: true ONLY for a pool daihyosen ("…-DH-N") rep bout, NOT a
// tiebreaker ("…-TB-N"). Use this for DAIHYOSEN-specific labels/badges. For
// ROUTING a bout to the individual editor use isSupplementaryBout instead (both
// DH and TB are single ippon-shobu rep bouts); only the "DH" label is
// daihyosen-specific, since a tiebreaker is not a daihyosen.
export function isPoolDaihyosenBout(id) {
    return typeof id === "string" && DAIHYOSEN_BOUT_RE.test(id);
}

// teamMatchTypeFor: the competition-level team match format ("kachinuki",
// "fixed", or "" when unset) as stamped onto enriched match objects and read
// by the display surfaces. "" means the format is unspecified: an individual
// comp, or a team comp whose teamMatchType was omitted or is a legacy empty
// value. Callers must treat "" as "not kachinuki" (fixed-order is the
// default behaviour), never as specifically "individual". Supplementary rep
// bouts (-DH-/-TB-) are fought as individual ippon-shobu even in a team comp;
// callers suppress the format for those at the stamping site
// (`isRepBout ? "" : teamMatchTypeFor(c)`). Reads both the flat viewer competition shape
// (c.teamMatchType) and the admin detail shape where the value nests under
// c.config. Lives here so every consumer (viewer, admin, display, overlay)
// shares one definition without adding import edges: this file is the leaf
// module they all already import.
export function teamMatchTypeFor(comp) {
    if (!comp) return "";
    return comp.teamMatchType || (comp.config && comp.config.teamMatchType) || "";
}

// teamMatchTypeHint: the one-line explanatory hint rendered under the "Team
// match format" selector on the create (admin_setup) and settings
// (admin_competition_settings) forms. Shared so the two admin surfaces cannot
// drift. isKachinuki picks the winner-stays copy; otherwise the fixed-order copy.
export function teamMatchTypeHint(isKachinuki) {
    return isKachinuki
        ? "The winner of each bout stays on to face the next opponent. Bouts are scored one at a time."
        : "All bouts are scheduled up-front by lineup position: each fighter faces the opponent in the same position.";
}

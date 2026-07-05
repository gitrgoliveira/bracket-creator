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

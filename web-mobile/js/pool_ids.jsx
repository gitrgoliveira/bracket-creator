// pool_ids.jsx — canonical parser for pool-match ids. LEAF module with NO
// imports, so both display_helpers.jsx and admin_pools.jsx can import it
// without pulling a transitive import chain into either — admin_pools.jsx
// otherwise relies on window globals rather than ESM imports, so a leaf with
// no dependencies keeps its module graph trivial. Single source of truth for
// the pool-id parse rule used across the display and admin surfaces.
//
// Backend id formats: "PoolName-N", "PoolName-DH-N" (daihyosen), "PoolName-TB-N"
// (tiebreaker). The non-greedy capture leaves hyphenated pool names intact
// ("Pool A-East-0" → "Pool A-East"); ids without a recognisable suffix yield "".
export const POOL_MATCH_ID_RE = /^(.*?)-(?:DH-|TB-)?\d+$/;

// poolNameOf — derive the pool name from a pool-match id (incl. DH/TB
// supplementary bouts). Returns "" when the id isn't pool-shaped.
export function poolNameOf(id) {
    if (typeof id !== "string") return "";
    return id.match(POOL_MATCH_ID_RE)?.[1] ?? "";
}

// isSupplementaryBout — true for a pool daihyosen ("…-DH-N") or tiebreaker
// ("…-TB-N") rep bout (a single individual ippon-shobu even in a team comp).
export const SUPPLEMENTARY_BOUT_RE = /-(?:DH|TB)-\d+$/;
export function isSupplementaryBout(id) {
    return typeof id === "string" && SUPPLEMENTARY_BOUT_RE.test(id);
}

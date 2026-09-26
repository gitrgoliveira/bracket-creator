// pool_ids.jsx: canonical parser for pool-match ids. A leaf that imports only
// write_result.jsx (itself a leaf with no imports, for scoreRowMatchLabel's
// matchLabel below), so both display_helpers.jsx and admin_pools.jsx can
// import it without pulling a real transitive import chain into either:
// admin_pools.jsx otherwise relies on window globals rather than ESM
// imports, so a leaf this close to dependency-free keeps its module graph
// trivial. Single source of truth for the pool-id parse rule used across the
// display and admin surfaces.
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
import { matchLabel } from './write_result.jsx';

// DAIHYOSEN_POSITION is the sentinel `position` value marking a sub-bout as
// the daihyosen (representative bout) rather than a numbered roster bout
// (real bouts are numbered from 1 in BOTH formats: the team editor sends
// idx+1, quick-score counts up from 1, and kachinuki appends
// LastBout.Position+1). Negative so it never collides with a real bout index.
// An earlier revision of this comment claimed fixed-format was 0-based; it
// is not, and no writer has ever emitted a 0 (the Excel export places a bout
// at subStartExcelRow+(Position-1), so a real 0 would land one row above the
// sub-match grid and the first bout would go missing from the sheet).
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
// exported poolMatchNumberOf() wrapper below, never the raw pattern.
const POOL_MATCH_ORDINAL_RE = /-(\d+)$/;

// poolMatchNumberOf: the bout's match number WITHIN its pool, 1-based, or 0
// when the id carries no such ordinal.
//
// A pool bout is numbered inside its own pool and the numbering RESTARTS for
// each pool (operator ruling 2026-09-19), so "Pool B · Match 1" and
// "Pool A · Match 1" are different bouts and a render site must name the pool
// alongside the number. This is a separate numbering from the knockout's
// `BracketMatch.MatchNumber`, which runs once across the whole tree.
//
// The number comes from the id's own numeric suffix (0-based on disk, so +1
// here), NOT from the bout's position in whatever list a caller holds: the
// export treats that suffix as the bout's stable identity (see
// `poolOrdinals` in internal/export/builder.go, where a skipped unresolvable
// match must not shift the ones after it), and a list-position count would
// renumber every later bout the moment one is filtered out.
//
// Supplementary bouts ("…-DH-N", "…-TB-N") get 0: they are rep bouts appended
// after the round-robin, not one of its numbered bouts, and their own DH/TB
// suffix is a separate sequence. The same caveat as poolNameOf applies -- any
// id ending in "-<digits>" parses -- so gate on the phase, not on a non-zero
// result alone.
export function poolMatchNumberOf(id) {
    if (typeof id !== "string" || isSupplementaryBout(id)) return 0;
    const n = id.match(POOL_MATCH_ORDINAL_RE)?.[1];
    return n === undefined ? 0 : Number(n) + 1;
}

// scoreRowMatchLabel: how the scores list names a match to the operator
// (bc-cse: relocated from admin_schedule_score_editor.jsx, its original
// home and still its re-exporter, so this leaf can also serve callers like
// admin_scoring_shared.jsx that cannot import that file without a cycle --
// admin_schedule_score_editor.jsx -> admin_schedule_lineup.jsx ->
// admin_scoring_shared.jsx already exists).
//
// EVERY row carries an identity, because this list is where an operator lands
// after a dialog names a match ("Match 15 was reopened"), and a row identified
// only by its time and its two competitors cannot be found that way.
//
// The two phases number independently, so the label says which numbering it
// is quoting. A knockout match owns a number across the whole tree, the same
// one the printed tree and the correction dialogs use, so it reads bare:
// "Match 15". A pool bout is numbered inside its own pool and the numbering
// restarts per pool (operator ruling 2026-09-19), so its pool is named with
// it: "Pool A · Match 2". Without that prefix the two would collide, since
// every pool has a Match 1 and so does the bracket.
//
// The pool's own name comes from window.poolLabel (viewer_utils.jsx), the one
// owner of the pool-vs-league-vs-Swiss heading, so a Swiss round reads
// "Round 3 · Match 2" rather than the synthetic "Swiss-R3" id.
//
// A knockout row is named by matchLabel (write_result.jsx), the same owner the
// correction dialogs use, so the row an operator is sent to looking for
// "Match 15" -- or for "the 3rd-place match", the one match named rather than
// numbered -- carries the words they were given. A dialog adds the round the
// server names it with ("Match 15 (Semifinals)"); the row keeps the number,
// which is what the dialog's label leads with.
//
// Returns "" when the match carries no number at all: a bracket match drawn
// before numbering existed, and a pool supplementary bout (daihyosen or
// tiebreaker), which is an appended rep bout rather than one of the pool's
// numbered round-robin bouts.
export function scoreRowMatchLabel(m) {
    if (m.phase === "bracket") {
        const label = matchLabel({ number: m.matchNumber, id: m.id });
        // matchLabel falls back to the raw id, which names nothing on screen.
        return label === m.id ? "" : label;
    }
    const n = poolMatchNumberOf(m.id || "");
    if (!n) return "";
    const pool = (window.poolLabel ? window.poolLabel(m) : m.poolName) || "";
    return pool ? `${pool} · Match ${n}` : `Match ${n}`;
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

// swissRoundLabel: "Swiss-R3" (the synthetic engine pool name, see
// engine/swiss.go swissPoolName) → "Round 3"; any other shape (a real pool
// name, "", null) is returned unchanged.
//
// SINGLE owner of the "Swiss-R<N>" parse: nothing else may restate it (mp-dej2).
// Two layers call in, and a new render site should use the layer above rather
// than this function directly:
//   • viewer_utils.jsx  leagueAwareLabel / poolLabel — the pool-phase label for
//     the viewer AND admin match rows. This is the one to reach for.
//   • display_helpers.jsx  phaseLabel — the TV board and lobby, which read off
//     the raw payload and so must derive the name from the match id.
// admin_shiaijo.jsx's queue grouping is the one site that calls this directly,
// because its own fallback ("Pool") differs from leagueAwareLabel's.
//
// A render site that reaches past both layers for a raw m.poolName prints the
// synthetic id: that is what admin_scoring_shared.jsx's withdrawal panel
// (since removed) did until it was moved onto poolLabel. If you add one, use poolLabel.
export function swissRoundLabel(poolName) {
    const m = /^Swiss-R(\d+)$/.exec(poolName || "");
    return m ? `Round ${m[1]}` : (poolName || "");
}

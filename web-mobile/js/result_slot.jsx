// result_slot.jsx: WHERE a result mark that names ONE competitor sits within
// that competitor's two ippon slots.
//
// This is the third of a three-part display rule whose other two parts already
// live in bracket.jsx: `sideMarks` answers WHICH mark, `placeMarks` answers
// WHICH SIDE, and this answers WHICH SLOT.
//
// `hanteiSlot` and `hanteiWinnerKey` live here too, for the reason this file
// exists. They were briefly defined in admin_scoring_team.jsx, which meant the
// INDIVIDUAL editor imported its Ht chip and attribution rule from the 2500-line
// team modal - dragging in that modal's state machine and network calls for two
// pure functions, and leaving any future surface (the viewer card, the overlay)
// the same bad choice. A shared rule belongs in the leaf, not in one consumer.
//
// WHY A SEPARATE LEAF, AND NOT bracket.jsx (the ONE statement of this — the
// other sites point here; two earlier rationales were wrong, so verify against
// the build before writing a third): Makefile `esbuild-jsx` runs esbuild with
// --outdir and NO --bundle, a per-file JSX transform whose imports stay as
// runtime ESM. And bracket.jsx is ALREADY in every page's module graph — the
// single index.html unconditionally script-tags /dist/admin_scoring_modal.js,
// which imports admin_scoring_team.jsx, which imports ./bracket.jsx — so an
// import from match_scoreboard.jsx would resolve to that cached module at
// roughly zero cost. The leaf is NOT a fetch saving. It is dependency hygiene:
// match_scoreboard is a shared display module whose imports are deliberately
// small leaves (pool_ids.jsx, lineup_resolver.jsx), it reaches bracket's
// display primitives through window globals, and bracket's module identity is
// currently fragile anyway (index.html ALSO script-tags /dist/bracket.js?v=6,
// a second module key, so the file evaluates twice and the window.* assignments
// race — a pre-existing issue this leaf stays clear of).
//
// THE RULE: a result mark behaves like a point. It fills the next FREE slot in
// the same outside-to-inside order a point would, so a 0-0 bout puts it in the
// outer (name-side) slot and a 1-1 bout in the inner one, giving
// `[K][ ] vs [Ht][M]`. It is NEVER written to the shared centre cell, which
// carries only the middle mark (vs / X / (E) / (DH)) — that centre is shared
// between the competitors, and a mark naming one of them does not belong there.
//
// `loose` reports that both slots were already full. What a caller does with
// that is ITS policy, and the three consumers deliberately differ:
//   - the read-only scoreboard (resultCells, match_scoreboard.jsx) renders the
//     mark beside the slots — dropping it would leave the verdict visible
//     nowhere on that surface;
//   - the team editor (hanteiSlot, admin_scoring_team.jsx) drops it, because
//     its armed hantei row is a second always-mounted channel for the verdict;
//   - the individual editor (admin_scoring_individual.jsx) drops it too — its
//     hantei panel highlights the RECORDED side's button (btn--primary, like
//     the team panel), and its slots are locked with a "hantei already
//     recorded" title.
// Change the contract and all three call sites must be re-checked (CLAUDE.md).
// The loose case is IMPOSSIBLE under the rules (sanbon-shobu ends at 2, so a
// 2-2 bout cannot occur), and it is closed on both sides: the editors' ippon
// entry stops each side at 2, and validateIpponCounts (internal/mobileapp/
// validation.go) caps each side at 2 AND rejects 2-2 outright on every sub-bout
// and on the bulk path. The branch exists solely so a hand-edited file can
// never overwrite a recorded point.
export function resultSlot(cells) {
  const pair = cells || [];
  const slot = [pair[0] || "", pair[1] || ""].findIndex(v => !v);
  return { slot, loose: slot === -1 };
}

// realIppons: what counts as a RECORDED ippon (drops empties and the "\u2022"
// placeholder). Exported so the surfaces that gate on a COUNT all count the
// same way; the display pair, the hantei tie gates and the editors' totals must
// never read different totals from one array.
//
// SCOPE, stated accurately because an earlier version of this comment was not
// (it claimed only two callers remained; there are roughly a dozen): this leaf
// owns the filter for the surfaces that gate a MARK on a count - the
// scoreboard's hantei tie test, both editors' totals, and the slot picks. It is
// NOT the only definition in the codebase. Inline copies of the same predicate
// still live in bracket.jsx, display_scoreboard.jsx, streaming_overlay.jsx,
// viewer_standings.jsx, admin_competition_bracket.jsx and api_serializers.jsx.
// Migrating them is a separate sweep; until it happens, changing what counts as
// a recorded ippon means grepping that literal, not just editing this function.
// (Go's equivalent is domain.CountScoringIppons, which both the engine and the
// wire validator now call — that pair no longer needs a keep-in-sync comment.)
export const realIppons = (arr) => (arr || []).filter(x => x && x !== "\u2022");

// hanteiTied: the ONE JS statement of "hantei applies only to a tied
// scoreline" (FIK 7-5 / 29-6, mirroring validation.go's equal-counts gate),
// counted through realIppons.
export const hanteiTied = (ipponsA, ipponsB) => realIppons(ipponsA).length === realIppons(ipponsB).length;

// nameOf: a side may arrive as an object or a bare string; this is the unwrap
// for callers that must compare or display a side name. Same scope caveat as
// realIppons above — the scoreboard and both editors call it, but one-line
// copies remain in bracket.jsx, viewer_standings.jsx, admin_pools.jsx,
// streaming_overlay.jsx and admin_scoring_engi.jsx.
export const nameOf = (v) => (v && typeof v === "object" ? v.name : v) || "";

// hanteiSlot: the EDITOR variant of resultSlot — "is this the side that won the
// hantei" over the shared placement rule above, so an editor and the read-only
// scoreboard cannot put the mark in different cells.
//
// It DELIBERATELY discards resultSlot's `loose` (both slots full). Per the
// header's enumeration, each editor keeps a second always-mounted channel for
// the verdict — its hantei row, armed and seeded from the stored decision with
// the winning side's button primary — so a dropped mark is not a lost result.
// Rendering a third slot-shaped chip would claim an ippon that does not exist.
export function hanteiSlot(isWinner, pts) {
  if (!isWinner) return -1;
  return resultSlot(pts).slot;
}

// hanteiWinnerKey: which side ("a"/"b"/"") a recorded hantei verdict names.
// Id-first (the server's authoritative identity), name fallback only when the
// name distinguishes the sides - a same-name pair with no usable ids returns
// "" so callers do not guess, and neither does the team editor's daihyosen
// seed, which then opens armed with no side primary. Shared by the individual
// editor's Ht chip and that seed, so the exclusive-attribution rule has one
// owner. Exported for tests.
export function hanteiWinnerKey(m) {
  if (!m?.winner) return "";
  const wId = m.winner?.id || "";
  const aId = m.sideA?.id || "";
  const bId = m.sideB?.id || "";
  if (wId && aId && wId === aId && wId !== bId) return "a";
  if (wId && bId && wId === bId && wId !== aId) return "b";
  // Only when BOTH sides carry ids is the id-space authoritative enough to
  // call a non-matching winner id unattributable; with one id-less side the
  // name fallback below can still resolve it (a replaced participant leaves
  // one side id-less while the winner keeps its stamped uuid).
  if (wId && aId && bId) return "";
  const wn = nameOf(m.winner), an = nameOf(m.sideA), bn = nameOf(m.sideB);
  if (wn && wn === an && wn !== bn) return "a";
  if (wn && wn === bn && wn !== an) return "b";
  return "";
}

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
// other sites point here; THREE earlier rationales have been wrong now, so
// verify against the build before writing a fourth): Makefile `esbuild-jsx`
// runs esbuild with --outdir and NO --bundle, a per-file JSX transform whose
// imports stay as runtime ESM. And bracket.jsx is ALREADY in every page's
// module graph — the single index.html unconditionally script-tags
// /dist/admin_scoring_modal.js, which imports admin_scoring_team.jsx, which
// imports ./bracket.jsx — so an import from match_scoreboard.jsx would resolve
// to that cached module at roughly zero cost. The leaf is NOT a fetch saving.
//
// It is dependency hygiene, and that is the WHOLE of it: match_scoreboard is a
// shared display module whose imports are deliberately small leaves
// (pool_ids.jsx, lineup_resolver.jsx), and it reaches bracket's display
// primitives through window globals rather than importing that 3000-line module
// for two pure functions.
//
// The third rationale, deleted here, was "bracket's module identity is fragile
// anyway — index.html ALSO script-tags /dist/bracket.js?v=6, so the file
// evaluates twice". That was true when written and is not any more: the tag now
// reads /dist/bracket.jsx, matching the "./bracket.jsx" import specifier, and
// check-imports.mjs Phase 2 fails the build if a tag and an import ever
// disagree again. Weigh a fold-back on the hygiene argument alone.
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
// entry stops each side at 2, and validateIppons (internal/mobileapp/
// validation.go) caps each side at 2 AND rejects 2-2 outright on every sub-bout
// and on the bulk path. The branch exists solely so a hand-edited file can
// never overwrite a recorded point.
export function resultSlot(cells) {
  const pair = cells || [];
  const slot = [pair[0] || "", pair[1] || ""].findIndex(v => !v);
  return { slot, loose: slot === -1 };
}

// sideSlotOrder: the VISUAL half of the same outside-to-inside rule resultSlot
// answers logically. Slot 0 is always a side's OUTER (name-side) cell, but the
// two sides mirror across the centre, so Aka must render its pair reversed for
// slot 0 to land nearest the Aka name on the right (FIK Table 2, p.16).
//
// Returns the render order of the two slot indices: [0,1] for Shiro (left),
// [1,0] for Aka (right).
//
// It lives beside resultSlot because they are one contract seen from two
// angles: resultSlot says WHICH slot a mark takes, this says WHERE that slot
// appears. Splitting them is how a mark ends up logically outer and visually
// inner. All THREE JS pair-builders derive from here: the read-only scoreboard
// (slotCells), the team editor's live slots (ptSlots), and its read-only
// done-bout rows (renderReadOnlyBout) — previously spelled `cells.toReversed()`
// and `[1, 0]` twice. An earlier version of this comment said "both", having
// missed the third; a `grep -n "\[1, 0\]"` in web-mobile/js is the check.
//
// A THIRD expression of this rule exists and is deliberately left alone: the
// individual score editor mirrors its Aka slots in CSS (`flex-direction:
// row-reverse`, styles.css). That surface renders its cells in DOM order and
// flips them in the layout layer, so there is no index to derive; converting it
// would be a visual refactor of an operator-critical surface for no behavioural
// gain. If you change the direction here, change that declaration too.
export function sideSlotOrder(side) {
  return side === "aka" ? [1, 0] : [0, 1];
}

// realIppons: what counts as a RECORDED ippon (drops empties, the "\u2022"
// placeholder, and the "Ht" judges'-decision mark — a hantei records who the
// referees chose, not that anyone struck). Mirrors domain.IsScoringIppon. Exported so the surfaces that gate on a COUNT all count the
// same way; the display pair, the hantei tie gates and the editors' totals must
// never read different totals from one array.
//
// SCOPE: this is now the ONLY definition in web-mobile/js. The sweep is done —
// bracket.jsx, display_scoreboard.jsx, streaming_overlay.jsx, viewer_standings.jsx,
// admin_competition_bracket.jsx, api_serializers.jsx, admin_shiaijo.jsx and both
// editors all call it, so changing what counts as a recorded ippon is a
// one-line edit here rather than a grep for the literal. A `grep -rn '!== "•"'
// --include='*.jsx'` outside this file returning ANY production hit means a new
// copy has appeared. (Two earlier versions of this comment were wrong in
// opposite directions: the first claimed two callers when a dozen existed, the
// second still listed files that had already been migrated.)
//
// It also unified two spellings that had drifted apart: read paths dropped
// empties AND the placeholder (`x && x !== "•"`), while some write paths dropped
// only the placeholder, so one file could hold three different answers to "how
// many ippons is this". An empty cell is not a scored ippon on any path.
// (Go's equivalent is domain.CountScoringIppons, which both the engine and the
// wire validator now call — that pair no longer needs a keep-in-sync comment.)
export const realIppons = (arr) => (arr || []).filter(x => x && x !== "\u2022" && x !== "Ht");

// hanteiTied: the ONE JS statement of "hantei applies only to a tied
// scoreline" (FIK 7-5 / 29-6, mirroring validation.go's equal-counts gate),
// counted through realIppons.
export const hanteiTied = (ipponsA, ipponsB) => realIppons(ipponsA).length === realIppons(ipponsB).length;

// nameOf: a side may arrive as an object or a bare string; this is the unwrap
// for callers that must compare or display a side NAME.
//
// SCOPE: no plain name-unwrap copies remain. What is left in the codebase is a
// different shape and deliberately not migrated — identity-pair extractors that
// pull the id AND the name together to compare two sides (winnerSideLR in
// bracket.jsx, the winner-side check in api_serializers.jsx, viewer_awards.jsx,
// viewer_watchlist_core.jsx). Taking only their name half from here would split
// one identity rule across two modules, which is worse than a local pair.
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

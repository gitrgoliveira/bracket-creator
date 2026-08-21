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
// `containsHt`, `hanteiDecided`, `placeHt`, `placeHtForWinner`, `stripHt`
// and `attributeWinnerSide` are the wire-serializer's slice of the same rule:
// api_serializers.jsx's toBackendMatchResult and normalizeMatch are the
// consumers. `hanteiDecided` answers "does this match/sub carry the mark"
// (mirrors Go's HanteiDecided()); `attributeWinnerSide` answers "which side
// does a winner name" - id-first when a winnerId/sideAId/sideBId triple is
// available, name fallback (sideA-first) otherwise - mirroring Go's
// internal/domain.AttributeWinnerSide exactly; `placeHtForWinner` answers
// "which side does an armed verdict's mark go on" by delegating to that
// helper and then placing the mark in the free slot — this replaced literal
// `containsHt(x.ipponsA) || containsHt(x.ipponsB)` / winner-name if-chains
// duplicated across that file's two write paths. IPPON_PLACEHOLDER
// ("•", U+2022 BULLET) and HANTEI_MARK ("Ht") are this file's two literal
// tokens, spelled once each and mirrored byte-for-byte by Go's
// internal/domain/ippon.go (IpponPlaceholder / HanteiMark). Both languages'
// values are drawn from one shared fixture, internal/domain/testdata/
// ippon_marks.json (the same pattern encho_labels.json uses for enchoLabel):
// __tests__/result_slot_constants.test.jsx reads it for the JS side and
// internal/domain/ippon_test.go's TestIpponMarks_GoldenFixture reads it for
// the Go side, so a divergence between the two languages — not just a drift
// within one of them — fails a test.
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

// IPPON_PLACEHOLDER / HANTEI_MARK: the two literal tokens this file's rules
// are built from, spelled ONCE each and used everywhere below instead of
// raw string literals. Mirrors Go's internal/domain/ippon.go exactly —
// IpponPlaceholder ("•", U+2022 BULLET) and HanteiMark ("Ht") — so a
// divergence between the two languages fails a test
// (result_slot_constants.test.jsx) rather than silently drifting apart.
// Exported so that test can assert on the literal values directly, rather
// than inferring them indirectly through behaviour.
export const IPPON_PLACEHOLDER = "•";
export const HANTEI_MARK = "Ht";

// isFreeSlot: a slot is free when it holds no letter — an empty string OR
// the IPPON_PLACEHOLDER no-strike placeholder. Mirrors Go's
// domain.AppendHantei (both treat "" and the placeholder as free). Shared
// by resultSlot (which slot a mark takes in a 2-cell pair) and placeHt
// below (which slot a mark takes in a growable ippon array) so there is
// exactly one definition of "free" in this file — previously resultSlot's
// own inline predicate treated the placeholder as OCCUPIED, a paper
// divergence from placeHt's that never fired in practice (every real input
// reaching resultSlot is pre-stripped of the placeholder by realIppons
// before display), but still left two answers to the same question in one
// dependency chain.
const isFreeSlot = (v) => !v || v === IPPON_PLACEHOLDER;

export function resultSlot(cells) {
  const pair = cells || [];
  const slot = [pair[0] || "", pair[1] || ""].findIndex(isFreeSlot);
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

// realIppons: what counts as a RECORDED ippon (drops empties, the
// IPPON_PLACEHOLDER, and the HANTEI_MARK judges'-decision mark — a hantei
// records who the referees chose, not that anyone struck). Mirrors
// domain.IsScoringIppon. Exported so the surfaces that gate on a COUNT all count the
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
export const realIppons = (arr) => (arr || []).filter(x => x && x !== IPPON_PLACEHOLDER && x !== HANTEI_MARK);

// containsHt / placeHt / stripHt: the wire-serializer's half of the same
// Ht-as-a-real-ippon-slice-entry contract realIppons reads. Moved here from
// api_serializers.jsx: that made the serializer a fourth consumer of this
// file's Ht rule (alongside the read-only scoreboard and both editors listed
// above), each with its own copy of "what counts as a free slot" or "what
// counts as the mark". Consolidating means placeHt's free-slot search shares
// isFreeSlot with resultSlot (defined above), and containsHt/stripHt share
// HANTEI_MARK with realIppons, so there is exactly one definition of
// each, not four. api_serializers.jsx already imports realIppons from here
// via a static ES import, so importing these three follows the identical,
// already-established route.
//
// containsHt: the read predicate - does this ippon array already carry the
// mark. The wire never sends a separate flag, so this is the ONLY way to
// detect a stored verdict on an array.
export const containsHt = (arr) => Array.isArray(arr) && arr.includes(HANTEI_MARK);

// hanteiDecided: whether a match/sub-shaped object (anything with an
// ipponsA/ipponsB pair) carries the mark on either side. Mirrors Go's
// HanteiDecided() on MatchResult and SubMatchResult (internal/state/models.go)
// — one predicate, not `containsHt(x.ipponsA) || containsHt(x.ipponsB)`
// spelled out at each call site (api_serializers.jsx's normalizeMatch used to
// spell it three times: once for the match itself, once in a .some() gate,
// once again inside the following .map()).
export const hanteiDecided = (obj) => containsHt(obj?.ipponsA) || containsHt(obj?.ipponsB);

// placeHt: the write placement - fill a free placeholder slot before
// growing, mirroring domain.AppendHantei so the mark lands in the winner's
// next free slot (0-0 -> outer, 1-1 -> inner), never overwriting a struck
// point. A no-op if the array already carries the mark (never double-place):
// checked BEFORE copying, so the common re-save-of-an-already-marked-match
// path returns the input array unchanged instead of allocating a throwaway
// copy just to discover there was nothing to place.
export function placeHt(arr) {
  if ((arr || []).includes(HANTEI_MARK)) return arr || [];
  const out = [...(arr || [])];
  const free = out.findIndex(isFreeSlot);
  if (free >= 0) { out[free] = HANTEI_MARK; return out; }
  out.push(HANTEI_MARK);
  return out;
}

// stripHt: drop a stored HANTEI_MARK entry from an ippon array without
// touching any other letter. Used where a caller must re-derive placement
// (e.g. re-adding the mark to a different, still-attributable side) rather
// than simply reading past it the way realIppons does for display/counting.
export const stripHt = (arr) => (arr || []).filter((v) => v !== HANTEI_MARK);

// attributeWinnerSide: the ONE statement of "which side does a winner name",
// id-first. Mirrors Go's internal/domain.AttributeWinnerSide exactly (that
// function is this one's twin - a divergence between the two is a bug, not a
// style choice):
//   - when winnerId, sideAId AND sideBId are ALL non-empty, ids are
//     AUTHORITATIVE and win over names when they disagree: winnerId ===
//     sideAId -> "a", winnerId === sideBId -> "b", matches neither -> null
//     (unattributable - do NOT fall back to names in this branch: a
//     same-name/different-dojo pair is exactly the case ids exist to
//     disambiguate, so a name fallback here would silently reintroduce the
//     bug this function fixes).
//   - otherwise (any id missing - legacy data, id-less payloads, sub-bout
//     rows that carry no ids at all) fall back to the name comparison this
//     file has always used: an empty winner name is unattributable, then
//     sideA-first when the winner name matches BOTH sides (CLAUDE.md's
//     documented defensive AKA/sideA-first convention), so id-less data
//     stays byte-identical to before this function existed.
// The id branch is checked FIRST and unconditionally - it does not require a
// non-empty winner NAME, only a non-empty winnerId matching all three ids'
// presence. This mirrors Go's ordering exactly (AttributeWinnerSide checks
// the id triple before its `winner == ""` guard, which belongs to the name
// fallback only); do not hoist an empty-winner-name guard above the id
// branch, which would silently disagree with the Go twin whenever a caller
// has an id but no name.
// Exported so a caller that only needs "which side", not "place the mark",
// can use it directly (e.g. a future consumer that isn't ippon-shaped).
export function attributeWinnerSide({ winnerId, sideAId, sideBId, winner, sideA, sideB } = {}) {
  if (winnerId && sideAId && sideBId) {
    if (winnerId === sideAId) return "a";
    if (winnerId === sideBId) return "b";
    return null;
  }
  if (!winner) return null;
  if (winner === sideA) return "a";
  if (winner === sideB) return "b";
  return null;
}

// placeHtForWinner: the ONE statement of "place the mark on the side a winner
// names" - delegates the WHICH SIDE question to attributeWinnerSide above and
// only decides WHICH SLOT (via placeHt). api_serializers.jsx's
// toBackendMatchResult had this same three-way switch written out twice (once
// for the top-level match, once per sub-bout), which is exactly the kind of
// copy this file exists to prevent for the rest of the Ht rules. It takes the
// already-prepared ipponsA/ipponsB (callers decide separately whether to
// stripHt first: the top-level caller's arrays already arrive pre-stripped
// via realIppons, the sub-bout caller strips explicitly) and returns [newA,
// newB], unchanged on whichever side it did not touch.
//
// winnerId/sideAId/sideBId are OPTIONAL trailing args: the sub-bout call site
// (api_serializers.jsx, sub rows carry no ids at all) omits them, so
// attributeWinnerSide's id branch never fires there and that path stays on
// the name fallback exactly as before this function existed. The top-level
// match call site threads the ids it already has in scope.
export function placeHtForWinner(winner, sideA, sideB, ipponsA, ipponsB, winnerId, sideAId, sideBId) {
  const side = attributeWinnerSide({ winnerId, sideAId, sideBId, winner, sideA, sideB });
  if (side === "a") return [placeHt(ipponsA), ipponsB];
  if (side === "b") return [ipponsA, placeHt(ipponsB)];
  return [ipponsA, ipponsB];
}

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

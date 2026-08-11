// result_slot.jsx: WHERE a result mark that names ONE competitor sits within
// that competitor's two ippon slots.
//
// This is the third of a three-part display rule whose other two parts already
// live in bracket.jsx: `sideMarks` answers WHICH mark, `placeMarks` answers
// WHICH SIDE, and this answers WHICH SLOT.
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
// that is ITS policy, and the two consumers deliberately differ: the read-only
// scoreboard renders the mark beside the slots (dropping it would leave the
// verdict visible nowhere), while the team editor drops it because its armed
// hantei row is a second always-mounted channel for the same verdict. This is
// reachable, not theoretical: a daihyosen may be taken to hantei from any tied
// scoreline, 2-2 included.
export function resultSlot(cells) {
  const pair = cells || [];
  const slot = [pair[0] || "", pair[1] || ""].findIndex(v => !v);
  return { slot, loose: slot === -1 };
}

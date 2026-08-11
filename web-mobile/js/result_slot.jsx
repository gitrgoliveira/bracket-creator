// result_slot.jsx: WHERE a result mark that names ONE competitor sits within
// that competitor's two ippon slots.
//
// This is the third of a three-part display rule whose other two parts already
// live in bracket.jsx: `sideMarks` answers WHICH mark, `placeMarks` answers
// WHICH SIDE, and this answers WHICH SLOT. It sits in its own leaf rather than
// beside them because match_scoreboard.jsx must NOT import bracket.jsx. The
// build (Makefile `esbuild-jsx`) runs esbuild with --outdir and NO --bundle, so
// it is a per-file JSX transform: imports stay as runtime ESM the browser
// resolves. Importing bracket.jsx would therefore not inline it, but it WOULD
// add a 32 KB module fetch and evaluation to every display surface that loads
// match_scoreboard (display.js, viewer.js, streaming_overlay.js). This leaf is
// 173 bytes. Small leaves imported by both admin and display surfaces
// (pool_ids.jsx, lineup_resolver.jsx) are the established shape for exactly this.
//
// THE RULE: a result mark behaves like a point. It fills the next FREE slot in
// the same outside-to-inside order a point would, so a 0-0 bout puts it in the
// outer (name-side) slot and a 1-1 bout in the inner one, giving
// `[K][ ] vs [Ht][M]`. It is NEVER written to the shared centre cell, which
// carries only the middle mark (vs / X / (E) / (DH)) — that centre is shared
// between the competitors, and a mark naming one of them does not belong there.
//
// `loose` reports that both slots were already full, so the caller must render
// the mark BESIDE the slots rather than drop it ("a result is never silently
// dropped"). This is reachable, not theoretical: a daihyosen may be taken to
// hantei from any tied scoreline, 2-2 included.
export function resultSlot(cells) {
  const pair = cells || [];
  const slot = [pair[0] || "", pair[1] || ""].findIndex(v => !v);
  return { slot, loose: slot === -1 };
}

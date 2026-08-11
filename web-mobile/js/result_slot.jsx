// result_slot.jsx: WHERE a result mark that names ONE competitor sits within
// that competitor's two ippon slots.
//
// This is the third of a three-part display rule whose other two parts already
// live in bracket.jsx: `sideMarks` answers WHICH mark, `placeMarks` answers
// WHICH SIDE, and this answers WHICH SLOT. It sits in its own leaf rather than
// beside them because match_scoreboard.jsx must NOT import bracket.jsx: esbuild
// inlines imported modules per entry, so that would pull the whole bracket tree
// into display.js, viewer.js and streaming_overlay.js. Small leaves imported by
// both admin and display surfaces (pool_ids.jsx, lineup_resolver.jsx) are the
// established shape for exactly this.
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
//
// Callers own the "is this side the one being marked" test, because they
// resolve the winner differently (the scoreboard walks a sub → team-alias →
// match-level fallback chain; the editor reads the armed hantei verdict).
export function resultSlot(cells) {
  const pair = cells || [];
  const slot = [pair[0] || "", pair[1] || ""].findIndex(v => !v);
  return { slot, loose: slot === -1 };
}

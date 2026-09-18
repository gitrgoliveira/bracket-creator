// numbered_name.jsx: the ONE renderer of a competitor's name with its number
// chip on the OUTER side of the name (operator ruling 2026-09-14, bc-dnst).
// Shiro is always the left column, so its number sits BEFORE the name; Aka is
// always the right column, so its number sits AFTER it. The two chips then
// frame the pairing from the outside, [K1 Tanaka] vs [Yamada K2], the way the
// team bout rows already read. That outer-side pairing only applies where the
// two sides sit LEFT/RIGHT. `side` is optional, and every caller whose sides
// do not sit left/right omits it: a sideless list (e.g. a standings table
// with one name per row), and a surface whose two sides STACK vertically
// instead (the bracket card, the admin and public schedule rows), where
// omitting `side` makes the number sit before the name on both, so the
// numbers align in one column. Either way the number renders before the
// name, same as Shiro.
//
// This is a leaf with no imports and no window.* dependency, so every
// Shiro/Aka layout that shows a numbered side ES-imports it directly instead
// of restating the before/after ternary pair inline, per this repo's rule
// that a display contract lives in one primitive. withNumber in
// match_scoreboard.jsx is the plain-string twin of this rule, for the
// contexts that genuinely need a string; keep the two in step. Both read
// the same numberedParts, so they cannot disagree about a side's name and
// number.
//
// Which form a surface takes is decided by ONE question: does the cell CLIP?
// A cell with text-overflow:ellipsis truncates the END of its run, which on
// Aka is the number, so the string form silently loses it there while Shiro's
// leading number survives. The TV board and the OBS lower third are therefore
// MIXED, not string-only as an earlier revision of this comment said: their
// clipping cells pass `clip` here, and their non-clipping rows keep the
// string. display_helpers.jsx's sideLabelParts carries that decision and the
// measurement behind it (bc-rvfx).
//
// The wrapper span (.numbered-name) is layout-transparent (display: contents)
// by default, so it never affects a host's flex/grid layout. The name text is
// further wrapped in its own span (.numbered-name__text) so a host that needs
// to ellipsise the name without ever clipping the chip passes `clip`: that
// adds .numbered-name--clip, an inline-flex row whose ellipsis rules live on
// that class next to .num-prefix in styles.css. A host that merely wraps
// needs neither the prop nor any CSS of its own.
//
// `number` may be empty (pre-draw, or a competitor excluded from the draw):
// then only the name renders. `name` is rendered as given; the caller owns
// any "TBD"/"-" fallback, since surfaces differ on it.
// numberFollowsName: THE operator ruling, as one predicate. Aka sits in the
// right column, so its number goes AFTER the name; everything else (Shiro, and
// every sideless or vertically-stacked surface) puts it before.
//
// It exists because the ruling used to be written twice -- `side === "aka"`
// here and `color === "aka" ? ... : ...` in withNumber -- which is two places
// for one rule to drift, and drift between the string form and the component
// form is precisely what this module's header says must not happen. Both now
// ask this. Exported from the leaf so withNumber can import it without
// numbered_name.jsx taking on any import of its own.
export function numberFollowsName(side) {
  return side === "aka";
}

export function NumberedName({ side, name, number, clip }) {
  const after = numberFollowsName(side);
  return (
    <span className={clip ? "numbered-name numbered-name--clip" : "numbered-name"}>
      {!after && number ? <span className="num-prefix">{number}</span> : null}
      <span className="numbered-name__text">{name}</span>
      {after && number ? <span className="num-prefix num-prefix--after">{number}</span> : null}
    </span>
  );
}

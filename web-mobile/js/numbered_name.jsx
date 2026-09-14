// numbered_name.jsx: the ONE renderer of a competitor's name with its number
// chip on the OUTER side of the name (operator ruling 2026-09-14, bc-dnst).
// Shiro is always the left column, so its number sits BEFORE the name; Aka is
// always the right column, so its number sits AFTER it. The two chips then
// frame the pairing from the outside, [K1 Tanaka] vs [Yamada K2], the way the
// team bout rows already read. `side` is optional: a sideless list (e.g. a
// standings table with one name per row, no Shiro/Aka pairing) omits it, and
// the number renders before the name, same as Shiro.
//
// This is a leaf with no imports and no window.* dependency, so every
// Shiro/Aka layout that shows a numbered side ES-imports it directly instead
// of restating the before/after ternary pair inline, per this repo's rule
// that a display contract lives in one primitive. withNumber in
// match_scoreboard.jsx is the plain-string twin of this rule, for the string
// contexts (the TV board, the OBS lower third, the viewer match card, the
// public schedule list); keep the two in step.
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
export function NumberedName({ side, name, number, clip }) {
  return (
    <span className={clip ? "numbered-name numbered-name--clip" : "numbered-name"}>
      {side !== "aka" && number ? <span className="num-prefix">{number}</span> : null}
      <span className="numbered-name__text">{name}</span>
      {side === "aka" && number ? <span className="num-prefix num-prefix--after">{number}</span> : null}
    </span>
  );
}

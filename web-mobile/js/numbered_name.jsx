// numbered_name.jsx: the ONE renderer of a competitor's name with its number
// chip on the OUTER side of the name (operator ruling 2026-09-14, bc-dnst).
// Shiro is always the left column, so its number sits BEFORE the name; Aka is
// always the right column, so its number sits AFTER it. The two chips then
// frame the pairing from the outside, [K1 Tanaka] vs [Yamada K2], the way the
// team bout rows already read.
//
// This is a leaf with no imports and no window.* dependency, so every
// Shiro/Aka layout that shows a numbered side ES-imports it directly instead
// of restating the before/after ternary pair inline, per this repo's rule
// that a display contract lives in one primitive. The plain-text sibling for
// string contexts (the TV board, the OBS lower third, the schedule list) is
// withNumber in match_scoreboard.jsx, which still prepends on both sides;
// whether the outer-side rule extends to those spectator surfaces is an open
// operator decision recorded on bc-dnst, so do not fold the two together
// until it is taken.
//
// The name text is wrapped in its own span (.numbered-name__text) so a
// nowrap + ellipsis container can be told to clip the NAME and never the chip:
// an ellipsised block drops its LAST inline content first, which for Aka is
// the number. The containers that do that (.shiaijo-qrow__name,
// .shiaijo-sides__side .name, .score-edit-row__side .name) carry the flex +
// ellipsis rules in styles.css next to .num-prefix; a wrapping container
// needs nothing and the span stays an ordinary inline.
//
// `side` is the competitor's colour, "shiro" or "aka". `number` may be empty
// (pre-draw, or a competitor excluded from the draw): then only the name
// renders. `name` is rendered as given; the caller owns any "TBD"/"-"
// fallback, since surfaces differ on it.
export function NumberedName({ side, name, number }) {
  return (
    <React.Fragment>
      {side === "shiro" && number ? <span className="num-prefix">{number}</span> : null}
      <span className="numbered-name__text">{name}</span>
      {side === "aka" && number ? <span className="num-prefix num-prefix--after">{number}</span> : null}
    </React.Fragment>
  );
}

// side_cell.jsx: the ONE owner of "this cell is Shiro" / "this cell is Aka".
//
// A tinted side cell used to state that fact two or three times INDEPENDENTLY:
// a hand-typed `--shiro` class, a hand-typed `sr-only` label, and
// NumberedName's own `side` prop. Ten surfaces did it, and the literals had
// already drifted -- `"Shiro: "` with a trailing space on the surfaces bc-sccl
// converted, `"Shiro:"` without one on viewer_match.jsx.
//
// The drift is the small cost. The real one is that the label was OPTIONAL by
// construction: nothing stopped a surface shipping the tint without the text
// channel DESIGN.md §4 requires, and that is exactly what two of them did --
// the court console and the watchlist hero both went out with the side
// announced to nobody, because each had reached for an `aria-label` that the
// element's role does not permit. One `side` value now produces the fill class
// AND the label together, so the tint cannot arrive without its text.
//
// A leaf: no imports and no window.* reads, like numbered_name.jsx beside it,
// which owns the other half of a side cell (the name and its number chip).

// The side's name as UI text. admin_scoring_shared.jsx's sideColorName
// delegates here rather than keeping a second copy; it stays exported under
// its own name because its callers read as "the name of this COLOUR key".
export function sideWord(side) {
  return side === "aka" ? "Aka" : "Shiro";
}

// The screen-reader text channel for a tinted cell.
//
// NOT an aria-label. On a bare <div> the implicit role is `generic`, where
// ARIA PROHIBITS author naming, so a label there is announced by nothing; on
// an interactive wrapper role=button has presentational children, so a label
// REPLACES the side word, the numbers and the dojos inside it instead of
// supplementing them. Both mistakes shipped in bc-sccl and both are why this
// is a span. (An aria-label on a cell that has a naming-permitted role is
// still fine -- CLAUDE.md's "aria-labels and titles may always say the side".)
export function SideLabel({ side }) {
  return <span className="sr-only">{sideWord(side)}: </span>;
}

// A tinted side cell.
//
//   side       "shiro" | "aka". The one place the surface states it.
//   className  the surface's own geometry/layout classes; kept verbatim.
//   density    the hatch pitch: "" (3/6px, the dense-row default), "mid"
//              (6/7px, schedule rows and the bracket card) or "loose" (7/8px,
//              the score editors and the engi card). Three pitches exist on
//              purpose; see the .side-fill--* block in styles.css.
//   fill       pass false where the surface PAINTS ITS OWN tint under its own
//              class (the compact schedule rows, the engi card). They predate
//              the shared pair and keep their fill; stacking side-fill--* on
//              top would repaint them at a different pitch. They still come
//              here for the label, which is the part that was going missing.
//   label      pass false ONLY where the side is already named in VISIBLE
//              text, so a second label would be announced twice. NO CALLER
//              passes it today: the one surface that names the side visibly
//              (the watchlist hero, CLAUDE.md's exception, granted by size)
//              does not use SideCell at all -- it would be built with the
//              only thing this guarantees turned off, and wrapping its row in
//              a component hides it from the panel suite's shim. The flag
//              exists for the next such surface; until one arrives it is
//              unexercised, so treat it as untested.
//   as         element to render; "div" by default.
export function SideCell({ side, className = "", density = "", fill = true, label = true, as: Tag = "div", children, ...rest }) {
  const cls = [
    className,
    fill ? `side-fill--${side}` : "",
    fill && density ? `side-fill--${density}` : "",
  ]
    .filter(Boolean)
    .join(" ");
  return (
    <Tag className={cls} {...rest}>
      {label ? <SideLabel side={side} /> : null}
      {children}
    </Tag>
  );
}

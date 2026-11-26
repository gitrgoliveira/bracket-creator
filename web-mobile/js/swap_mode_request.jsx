// swap_mode_request.jsx: the Draw review card's "Swap competitors on Pools"
// (or "on the Bracket") button opens the competition's preview tab with the
// Swap competitors mode already on (bc-drev). The admin console's tabs are
// view state, not URLs, so the ask is held here for one competition and taken
// once, by useSwapCompetitors (admin_competition_bracket.jsx) as the tab
// mounts, on the render the button's tap causes (the section change is plain
// view state). Taking it clears it, so a later visit to the tab, or a reload,
// starts with the mode off.
//
// A leaf with no imports: both sides ES-import it directly.

let pending = null;

// askSwapMode: the next preview tab of compId to mount starts in the mode.
export function askSwapMode(compId) {
  pending = compId;
}

// takeSwapMode: whether the preview tab of compId mounting now was asked to
// start in the mode. Taking it clears the ask, whichever competition it named.
export function takeSwapMode(compId) {
  const ask = pending;
  pending = null;
  return !!compId && ask === compId;
}

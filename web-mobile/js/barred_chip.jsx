// barred_chip.jsx: the "Withdrawn" chip beside a barred competitor's name on
// a still-scheduled match.
//
// A scheduled match stays visible in every public schedule and pool list
// even when it cannot be fought (every entry stays fixable, nothing is
// hidden): ineligible_match.jsx is the one reader of the server's
// `ineligibleSides` stamp, and this file is the one renderer of the chip it
// names (BARRED_CHIP), so every list row places it the same way.
//
// Placement mirrors teamNameMark (match_scoreboard.jsx), the existing
// Kiken/Fus. result-mark composer these same rows already call: the chip
// rides on the competitor's INNER side (toward the match's centre "vs"),
// via the same numberFollowsName predicate that decides where a competitor
// number sits. Not reused directly: teamNameMark wraps in `.sb-result-mark`
// with a `team-summary-mark-*` testid, both specific to the completed-match
// team mark, and only ever fires for a completed match, so a scheduled
// barred row never fires it — the two never collide on one name.
//
// Placed at each of the four call sites where a name renders inline with a
// "vs" centre (bracket.jsx's MatchCard has no such centre -- the two sides
// stack vertically -- so it renders BarredChip directly instead of going
// through barredNameMark; see its own comment there). The a/b <-> aka/shiro
// mapping barredNameMark needs to read barredSides() is NOT spelled here:
// sideKeyForDecisionBy (ineligible_match.jsx) is the one owner of it (bc-cse).

import { BARRED_CHIP, barredSides, sideKeyForDecisionBy } from './ineligible_match.jsx';
import { numberFollowsName } from './numbered_name.jsx';

// The visible chip itself: plain readable text, same as "Kiken"/"Fus." are
// already announced by teamNameMark -- no aria-label (the cells this rides
// in are bare inline spans, role generic, where ARIA prohibits naming via
// aria-label) and no aria-hidden (the text IS the label).
export function BarredChip() {
  return <span className="barred-chip" data-testid="barred-chip">{BARRED_CHIP}</span>;
}

// barredNameMark: wraps `nameEl` (a string or a JSX element) with BarredChip
// on `side`'s inner edge when the server has barred that side of `m`. `side`
// is "aka" | "shiro" -- the same value every caller already passes to
// NumberedName / SideLabel / teamNameMark. Returns `nameEl` unchanged when
// that side isn't barred, so nesting this around an existing teamNameMark
// call is free: the two marks can never both be non-empty for one match
// (teamMatchMarks requires `status === "completed"`; barredSides requires
// `status === "scheduled"`).
export function barredNameMark(m, side, nameEl) {
  const key = sideKeyForDecisionBy(side) || null;
  const isBarred = !!(key && barredSides(m)[key]);
  if (!isBarred) return nameEl;
  const chip = <BarredChip />;
  return numberFollowsName(side) ? <>{chip}{" "}{nameEl}</> : <>{nameEl}{" "}{chip}</>;
}

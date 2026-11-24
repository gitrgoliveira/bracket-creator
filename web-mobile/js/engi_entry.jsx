// engi_entry.jsx: the one owner of "does this name fit a side of this engi
// competition". An engi side is a PAIR (both member names combined in the one
// name field, "Name 1 - Name 2") or, when the competition picks "Single
// competitors" under Competitors per side (engiSolo), ONE competitor. Every door that takes a
// typed engi name asks this module (the Participants page's paste box, add and
// replace forms, the registration desk, the public self-registration form),
// so the rule is never re-derived beside it.

// The sentence a pair competition answers for a name that does not carry both
// members around " - ".
export const ENGI_PAIR_PROBLEM = 'Enter both names of the pair as "Name 1 - Name 2".';

// The sentence a solo competition answers for a name typed as a pair. It says
// what to do, since the operator who typed a pair most likely meant a pair
// competition.
export const ENGI_SOLO_PROBLEM = 'This competition takes single competitors. To enter pairs, pick "Two competitors (pairs)" in Settings.';

// The sentence a solo competition answers for a pair typed once its setting can
// no longer change (a draw exists), and on the public form, whose participant
// cannot reach Settings: the advice to change the setting could not be
// followed, so it says what to do instead.
export const ENGI_SOLO_PROBLEM_LOCKED = "This competition takes single competitors. Enter one name.";

// The sentence a solo competition answers for a blank name.
export const ENGI_SOLO_BLANK = "Enter the competitor's name.";

// engiShape names the shape of an engi competition's side: "pair", "solo",
// or null when the competition is not engi (no engi rule applies). engiSolo
// means something only with engi, so it is read only then.
export function engiShape(c) {
  if (!c || !c.engi) return null;
  return c.engiSolo ? "solo" : "pair";
}

// engiEntryProblem returns the sentence that refuses `name` for an engi side
// of the given engiShape ("pair" or "solo"), or null when the name fits or the
// shape is null (not an engi competition, no engi rule applies). The name is
// trimmed first, as the paste box and the forms trim, so "Alice - " is judged
// as "Alice -". `settingLocked` (engiSettingLocked, or true on the public form)
// words a solo refusal without the advice to change the setting.
//   pair: both halves around the FIRST " - " are non-blank (the same split as
//         window.engiPairParts), so "Alice -", "- Bob", "-" and " - Bob" are
//         refused and "Mary-Jane Smith - Ren Fujita" passes.
//   solo: refused when the name holds " - " or begins or ends with "-" (a
//         pair typed into a solo competition); a hyphenated name with no
//         spaces around the hyphen ("Jean-Paul Martin") passes.
export function engiEntryProblem(name, shape, settingLocked = false) {
  if (!shape) return null;
  const s = String(name ?? "").trim();
  if (shape === "solo") {
    if (s === "") return ENGI_SOLO_BLANK;
    if (s.includes(" - ") || s.startsWith("-") || s.endsWith("-")) {
      return settingLocked ? ENGI_SOLO_PROBLEM_LOCKED : ENGI_SOLO_PROBLEM;
    }
    return null;
  }
  const i = s.indexOf(" - ");
  if (i < 0) return ENGI_PAIR_PROBLEM;
  if (s.slice(0, i).trim() === "" || s.slice(i + 3).trim() === "") return ENGI_PAIR_PROBLEM;
  return null;
}

// engiSettingLocked is true once the competition's Competitors per side
// setting can no longer be changed: from draw-ready onward, the same lock the
// Settings pills carry (admin_competition_settings.jsx, lockedAfterDraw).
export function engiSettingLocked(c) {
  return !!c && !!c.status && c.status !== "setup";
}

// engiNameFieldLabel is the label of a form's name field for a competition of
// the given engiShape: the pair format for a pair competition, and the plain
// "Name" every other competition (solo engi included) shows.
export function engiNameFieldLabel(shape) {
  return shape === "pair" ? "Pair (Name 1 - Name 2)" : "Name";
}

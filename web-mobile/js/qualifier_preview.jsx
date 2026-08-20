// qualifier_preview.jsx: client-side mirror of the "knockout qualifiers"
// pool/qualifier COUNT arithmetic (bead bc-qual, phase LP-5a), used to drive
// the live preview line under the "Knockout qualifiers" radio on the
// competition SETTINGS page (admin_competition_settings.jsx), the one surface
// that renders the radio next to a known roster. The competition CREATE form
// (admin_setup.jsx) renders the same radio from the copy + coupling helpers at
// the bottom of this file but shows a static placeholder instead of a preview:
// it has no roster to compute one from. Pure functions, no window/React
// dependency, so this stays safe to import from anywhere including tests.
//
// Deliberately NOT a port of the actual pool-BUILDING code
// (internal/helper.BuildPoolPhase / CreatePools / dojo-conflict avoidance):
// this only reproduces the pool/qualifier COUNTS, mirroring three small
// pure functions on the Go side --
//
//   - internal/helper.PoolCount(n, minSize, isMax=false) == floor(n / minSize)
//     (minimum-players-per-pool sizing; the maximum/isMax=true branch is out
//     of scope here, the "Knockout qualifiers" radio only renders under
//     minimum sizing).
//   - internal/state.Competition.QualifiersForPool's oversized test for
//     ExtraQualifiersLargerPools: a pool is OVERSIZED when it holds more than
//     the configured minimum size, and an oversized pool sends one extra
//     qualifier. At pool-formation time (PoolCount's floor + remainder),
//     the number of oversized pools is exactly `n - minSize*pools`.
//   - internal/helper.FillBracketPoolCount(n, minSize, seededPools): WKC's
//     own formation rule (see that function's doc comment for the sheet
//     derivation) -- pools of size minSize/minSize+1 that use every entrant,
//     the SMALLEST reachable power-of-two bracket, and within it the largest
//     SUPPLIED even-draft pool count, then the largest supplied odd-draft
//     one. Supply is seeded pools first (drafted 2nds come from the seeded
//     pools, oversized ones as fallback), which is why this mirror needs the
//     roster's seeded count and the preview can genuinely CHANGE when the
//     operator seeds another pool.
//   - internal/helper.NextPow2.
//
// See ValidateExtraQualifiers (internal/state/models.go) for the coupling
// rule this preview does not itself enforce: both non-standard modes are
// only VALID at poolWinners == 1. Both screens force poolWinners to 1 (via
// winnersForExtraQualifiersChange below) the moment a non-standard option is
// selected, so by the time a non-standard preview is ever shown,
// `poolWinners` passed in here is already 1 -- this module has no opinion on
// that, it just multiplies whatever poolWinners value it is given.

// EXTRA_QUALIFIERS_* mirror state.ExtraQualifiersNone / ...LargerPools /
// ...FillBracket (internal/state/models.go) byte-for-byte (the wire values,
// also the YAML extra_qualifiers / JSON extraQualifiers values).
export const EXTRA_QUALIFIERS_STANDARD = "";
export const EXTRA_QUALIFIERS_LARGER_POOLS = "larger-pools";
export const EXTRA_QUALIFIERS_FILL_BRACKET = "fill-bracket";

// nextPow2 mirrors internal/helper.NextPow2: the smallest power of two >= n
// (1 for n <= 1).
export function nextPow2(n) {
  if (!Number.isFinite(n) || n <= 1) return 1;
  let p = 1;
  while (p < n) p <<= 1;
  return p;
}

// fillBracketPoolCount mirrors internal/helper.FillBracketPoolCount(n,
// minSize, seededPools): returns { pools, drafts } under the WKC formation
// rule, or null when no pool count exists (invalid n/minSize, or no count
// whose drafts the seeded and oversized pools can supply) -- mirrors the Go
// function's error return, just without the message text, since this is
// preview-only and never shown as a submit-time rejection.
//
// The rule, kept in lockstep with the Go original (which carries the sheet
// evidence): P in [ceil(n/(minSize+1)), floor(n/minSize)]; the bracket is the
// smallest power of two that range reaches (nextPow2 of the range's bottom);
// within it, the largest SUPPLIED even-draft P, then the largest supplied
// odd-draft P. A P is supplied when its draft count fits
// max(min(seededPools, P), n - minSize*P) -- the max, never the sum, because
// a seeded pool can also be the oversized one.
export function fillBracketPoolCount(n, minSize, seededPools) {
  if (!Number.isInteger(minSize) || minSize <= 0) return null;
  if (!Number.isInteger(n) || n < minSize) return null;
  const seeds = Number.isInteger(seededPools) && seededPools > 0 ? seededPools : 0;
  const maxP = Math.floor(n / minSize);
  const minP = Math.ceil(n / (minSize + 1));
  if (minP > maxP || minP < 1) return null;
  const bracket = nextPow2(minP);
  const top = Math.min(maxP, bracket);
  const supplied = (p) => {
    const supply = Math.max(Math.min(seeds, p), n - minSize * p);
    return bracket - p <= supply;
  };
  for (const wantOdd of [0, 1]) {
    for (let p = top; p >= minP; p--) {
      if ((bracket - p) % 2 === wantOdd && supplied(p)) {
        return { pools: p, drafts: bracket - p };
      }
    }
  }
  return null;
}

// bracketShape derives the knockout bracket size/byes/rounds for a given
// qualifier count. rounds is log2(bracketSize); bracketSize is always a
// power of two by construction (nextPow2), so Math.log2 always lands on an
// integer here (Math.round only guards float noise).
function bracketShape(qualifiers) {
  const bracketSize = nextPow2(qualifiers);
  return {
    bracketSize,
    byes: bracketSize - qualifiers,
    rounds: qualifiers > 0 ? Math.round(Math.log2(bracketSize)) : 0,
  };
}

// computeQualifierPreview computes the pool/qualifier/bracket arithmetic for
// all three "Knockout qualifiers" modes at once, given the current roster
// count n, the minimum-players-per-pool size minSize, the pool-winners
// count poolWinners (the EFFECTIVE value -- callers that have not resolved
// an unset/<=0 poolWinners to the default of 2 should do that before
// calling, same contract as state.Competition.EffectivePoolWinners()), and
// seededCount, the number of participants carrying a seed rank -- which
// feeds the fill-bracket supply rule ONLY (drafted 2nds come from seeded
// pools first), so the standard and larger-pools shapes never depend on it.
// The settings page derives it from the same c.players it derives n from;
// omitting it (undefined) previews the unseeded regime.
//
// Returns { standard, largerPools, fillBracket }, each either:
//   - null, when that mode has no defined shape for these inputs (n < minSize,
//     minSize invalid, or -- fillBracket only -- no pool count fits within the
//     search range), or
//   - { pools, qualifiers, bracketSize, byes, rounds, ... } with mode-specific
//     extras: largerPools additionally carries `oversized` (how many pools
//     send the extra qualifier); fillBracket additionally carries `drafts`
//     (how many pools' 2nd place is drafted).
//
// Guards n <= 0 / non-finite / minSize <= 0 by returning all-null: the
// settings page renders this radio from the moment a mixed competition
// exists, and a competition created a moment ago has an empty roster.
// Callers render a neutral "add participants to preview" state rather than a
// misleading 0-pool line. The create form does not call this at all -- it has
// no roster by construction, so every result would be null; it renders the
// placeholder directly.
export function computeQualifierPreview(n, minSize, poolWinners, seededCount) {
  const roster = Number.isFinite(n) ? Math.trunc(n) : 0;
  const winners = Number.isInteger(poolWinners) && poolWinners > 0 ? poolWinners : 1;

  if (!Number.isInteger(minSize) || minSize <= 0 || roster <= 0 || roster < minSize) {
    return { standard: null, largerPools: null, fillBracket: null };
  }

  const pools = Math.floor(roster / minSize);
  // Count of OVERSIZED POOLS, not remainder players: min-mode spreads the
  // remainder one player per pool, so the two are equal whenever
  // remainder <= pools -- but a degenerate roster (e.g. n=5 at min 3: one
  // pool of 5) packs several extras into fewer pools, and Go's
  // QualifiersForPool grants +1 per oversized POOL regardless of how far
  // over the minimum it is. Clamp to match.
  const oversized = Math.min(roster - minSize * pools, pools);

  const standardQualifiers = pools * winners;
  const standard = { pools, qualifiers: standardQualifiers, ...bracketShape(standardQualifiers) };

  const largerQualifiers = standardQualifiers + oversized;
  const largerPools = { pools, oversized, qualifiers: largerQualifiers, ...bracketShape(largerQualifiers) };

  const fill = fillBracketPoolCount(roster, minSize, seededCount);
  const fillBracket = fill
    ? { pools: fill.pools, drafts: fill.drafts, qualifiers: fill.pools * winners + fill.drafts, ...bracketShape(fill.pools * winners + fill.drafts) }
    : null;

  return { standard, largerPools, fillBracket };
}

// formatQualifierPreviewLine renders one mode's computed shape (as returned
// inside computeQualifierPreview's result) into the operator-facing preview
// sentence, e.g. "34 pools -> 36 qualifiers -> 64-slot knockout (28 byes)".
// Returns null when shape is null (see computeQualifierPreview's n<=0 guard)
// so the caller can fall back to a neutral placeholder.
export function formatQualifierPreviewLine(shape) {
  if (!shape) return null;
  const byesText = shape.byes === 0 ? "no byes" : `${shape.byes} bye${shape.byes === 1 ? "" : "s"}`;
  return `${shape.pools} pool${shape.pools === 1 ? "" : "s"} -> ${shape.qualifiers} qualifier${shape.qualifiers === 1 ? "" : "s"} -> ${shape.bracketSize}-slot knockout (${byesText})`;
}

// --- Knockout qualifiers form coupling (bc-qual LP-5a) ---
//
// Pure coupling rules between the "Pool size is a" (poolMode/poolSizeMode)
// radio, the "Knockout qualifiers" (extraQualifiers) radio, and the
// "Winners per pool" (winners/poolWinners) field. Shared by BOTH screens
// that render the pool-size radios: the competition CREATE form
// (admin_setup.jsx AdminCreateCompetition) and the competition SETTINGS
// page (admin_competition_settings.jsx AdminSettings) -- the settings page
// shows the same "Pool size is a" selection the create form does, so per
// the operator ruling ("the UI shows the 3 options whenever minimum-per-pool
// is selected") it must show the same three options too. Living here
// (rather than in either screen's own file) is what lets both import the
// SAME functions instead of two copies drifting.
//
// state.ValidateExtraQualifiers (internal/state/models.go) is the
// server-side owner of the actual RULE (non-standard modes require
// minimum-players-per-pool sizing AND poolWinners == 1); these helpers exist
// so neither UI can construct the request that rule would reject in the
// first place ("Never rely on the server 400 as the primary UX" -- CLAUDE.md
// Testing & Verification). They do not restate the rule, only the FORM
// mechanics that keep the operator from reaching an invalid combination.

// extraQualifiersRadioVisible: the "Knockout qualifiers" radio is only
// meaningful under minimum-players-per-pool sizing (poolMode === "min") on a
// pools-then-knockout ("mixed") competition. Mirrors the poolMode === "max"
// half of state.ValidateExtraQualifiers's gate; the format === "mixed" half
// is already the condition the sibling pool-size fields render under on
// both screens.
export function extraQualifiersRadioVisible(format, poolMode) {
  return format === "mixed" && poolMode === "min";
}

// resetExtraQualifiersOnPoolModeChange: when the operator switches pool
// sizing AWAY FROM minimum-players-per-pool (i.e. to "max"), the "Knockout
// qualifiers" radio is about to be hidden. A non-standard selection hidden
// behind a mode the value is no longer valid under must not silently persist
// as hidden state waiting to be resubmitted -- so a change TO "max" resets
// the stored value to standard. A change TO "min" (or any other poolMode
// value) leaves the current selection alone: nothing about becoming visible
// again invalidates what was already selected (and standard, the only value
// reachable while hidden started in "max", needs no reset).
export function resetExtraQualifiersOnPoolModeChange(newPoolMode, currentExtraQualifiers) {
  // Normalizes a falsy-but-not-"" currentExtraQualifiers (undefined/null,
  // e.g. from a never-normalized/legacy competition record) to the explicit
  // standard sentinel, same reasoning as winnersInputDisabled above.
  if (newPoolMode === "min") return currentExtraQualifiers || EXTRA_QUALIFIERS_STANDARD;
  return EXTRA_QUALIFIERS_STANDARD;
}

// winnersForExtraQualifiersChange: selecting a non-standard "Knockout
// qualifiers" option forces "Winners per pool" to 1 (state.ValidateExtraQualifiers
// rejects larger-pools/fill-bracket at poolWinners >= 2; the draw builders have
// no defined crossing behaviour for a pool sending 2+ home qualifiers plus a
// crossed/drafted extra). Switching back to standard does NOT restore
// whatever winners value was set before: the field simply becomes editable
// again (see winnersInputDisabled below), starting from 1.
export function winnersForExtraQualifiersChange(newExtraQualifiers, currentWinners) {
  if (newExtraQualifiers === EXTRA_QUALIFIERS_STANDARD) return currentWinners;
  return 1;
}

// winnersInputDisabled: the "Winners per pool" input is disabled whenever a
// non-standard "Knockout qualifiers" option is active, since
// winnersForExtraQualifiersChange has already pinned it to 1 and any further
// edit would immediately violate state.ValidateExtraQualifiers's poolWinners
// == 1 requirement. The settings page additionally disables it (and every
// sibling pool-config field) while draw-ready; that OR is applied at the
// call site, not here, since only the settings page has an isDrawReady
// concept.
//
// Falsy-but-not-"" (undefined/null) normalizes to "not disabled", same as an
// explicit EXTRA_QUALIFIERS_STANDARD: the create form's extraQualifiers
// state is always initialised to "", but the settings page seeds `local`
// straight off the competition prop, and a never-normalized/legacy record
// (or a raw object in a test) can carry `extraQualifiers: undefined` rather
// than "". A bare `!== EXTRA_QUALIFIERS_STANDARD` would then read undefined
// as non-standard and wrongly disable/lock the field for every competition
// predating this feature.
export function winnersInputDisabled(extraQualifiers) {
  return (extraQualifiers || EXTRA_QUALIFIERS_STANDARD) !== EXTRA_QUALIFIERS_STANDARD;
}

// extraQualifiersLabel / extraQualifiersHint are the ONE home of the radio's
// operator-facing copy (operator wording, 2026-08-19), imported by both the
// create form (admin_setup.jsx) and the settings page
// (admin_competition_settings.jsx) so the two surfaces cannot drift.
// The oversized hint derives its pool size from the configured minimum
// ("4-person" at the default minimum of 3), so it stays truthful when an
// operator raises the minimum.
export function extraQualifiersLabel(extraQualifiers) {
  if (extraQualifiers === EXTRA_QUALIFIERS_LARGER_POOLS) return "Oversized send +1";
  if (extraQualifiers === EXTRA_QUALIFIERS_FILL_BRACKET) return "Fit the knockout";
  return "Standard";
}

export function extraQualifiersHint(extraQualifiers, minSize) {
  if (extraQualifiers === EXTRA_QUALIFIERS_LARGER_POOLS) {
    const over = Number.isInteger(minSize) && minSize > 0 ? minSize + 1 : 4;
    return `Same pools; ${over}-person pools send their top 2.`;
  }
  if (extraQualifiers === EXTRA_QUALIFIERS_FILL_BRACKET) {
    return "Fewer, fatter pools; the bracket fills exactly, no byes.";
  }
  return "Every pool sends the same top-N; the bracket is padded with byes.";
}

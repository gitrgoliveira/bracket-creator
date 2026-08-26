// competition_shape.jsx: shared shape of a competition's CONFIGURATION --
// the option lists, field labels/hints, and visibility/coupling rules
// rendered by the two screens that edit it, so they cannot drift (bead
// bc-symm, operator ruling: the two screens must offer the same controls
// with the same copy). Pure leaf, same shape as qualifier_preview.jsx: no
// window, no React, no imports, ES-exported, safe to import from a test
// with no setup.
//
// The two consumers:
//   - the competition CREATE form (AdminCreateCompetition, admin_setup.jsx)
//     authors a brand-new competition with no roster and no draw yet, so
//     every control here is unconditionally editable;
//   - the competition SETTINGS page (AdminSettings,
//     admin_competition_settings.jsx) edits an EXISTING competition, so on
//     top of what lives here it separately locks output-affecting fields
//     once a draw exists (isDrawReady) or the competition has started
//     (isStarted). Those locks are a settings-only concept (there is
//     nothing to lock on a form that hasn't created anything yet) and stay
//     out of this module; the OR of "is this field meaningful for this
//     config" (owned here) with "is it currently editable on THIS screen"
//     (owned by the caller) is applied at the call site, same split as
//     qualifier_preview.jsx's winnersInputDisabled comment describes for
//     the draw-ready half.
//
// Split from qualifier_preview.jsx on purpose, not merged into it: that
// module already owns the "Knockout qualifiers" radio + "Winners per pool"
// coupling and the live preview arithmetic, which is its own well-bounded
// problem (bc-qual LP-5a). This module owns the REST of the competition
// config form -- kind, format, pool shape, Swiss rounds, league
// tie-breaking, per-phase durations -- so a reader hunting for one piece of
// shared copy has exactly two places to check, not a growing pile in one
// file. teamMatchTypeHint (the "Team match format" hint) is shared from a
// THIRD place, pool_ids.jsx, because it is also read by non-admin display
// surfaces that must not pull in a competition-config-editing module just
// to read one hint string; it is not re-exported or duplicated here.
//
// Wire values below mirror internal/state/models.go's Competition fields
// byte-for-byte (Kind, Format, PoolFormat's JSON/YAML values); the Go side
// has no named constants for these, only string-literal comparisons, so
// there is nothing further to mirror than the literals themselves.

export const KIND_INDIVIDUAL = "individual";
export const KIND_TEAM = "team";

export const FORMAT_PLAYOFFS = "playoffs";
export const FORMAT_MIXED = "mixed";
export const FORMAT_LEAGUE = "league";
export const FORMAT_SWISS = "swiss";

export const POOL_FORMAT_FULL = "full";
export const POOL_FORMAT_PARTIAL = "partial";

// --- Option lists -----------------------------------------------------
//
// Each entry is { value, label, hint? }. `value` is the wire value; `label`
// is the pill/option text; `hint`, where present, is the explanatory line
// shown under the control for that selection (formatHint below resolves
// FORMAT_OPTIONS' hint by value, the same shape extraQualifiersHint uses in
// qualifier_preview.jsx).

// KIND_OPTIONS / LABEL_KIND: verbatim from admin_setup.jsx's "Competition
// type" pills. admin_competition_settings.jsx renders the identical pills
// (disabled once isDrawReady || isStarted || kindChangeBlockedReason(...)
// is truthy, since an existing roster can't be reshaped in place -- see
// kindChangeBlockedReason below), which is exactly the parity this module
// exists to hold: both screens import KIND_OPTIONS/LABEL_KIND from here
// rather than each keeping a second transcription of the pills' copy.
export const LABEL_KIND = "Competition type";
export const KIND_OPTIONS = [
  { value: KIND_INDIVIDUAL, label: "Individual" },
  { value: KIND_TEAM, label: "Team" },
];

// FORMAT_OPTIONS / LABEL_FORMAT / formatHint: verbatim from admin_setup.jsx's
// "Format" pills + the per-format hint line under them.
// admin_competition_settings.jsx renders the same pills (disabled once
// isDrawReady || isStarted, since format is output-affecting the moment a
// draw exists), so both screens import FORMAT_OPTIONS/LABEL_FORMAT/
// formatHint from here instead of each carrying a second transcription of
// the four format names and their hints.
export const LABEL_FORMAT = "Format";
export const FORMAT_OPTIONS = [
  { value: FORMAT_PLAYOFFS, label: "Knockout only", hint: "Direct single-elimination knockout." },
  { value: FORMAT_MIXED, label: "Pools + Knockout", hint: "Round-robin pools first, then top finishers advance to a knockout bracket." },
  { value: FORMAT_LEAGUE, label: "League", hint: "Single round-robin across all participants; final standings determine the winner (no knockout)." },
  { value: FORMAT_SWISS, label: "Swiss", hint: "Swiss-system: fixed number of rounds, pairing players with equal win counts; cumulative standings decide the winner." },
];

// formatHint: the hint line shown under the Format pills for the given
// format. "" for an unrecognised/empty format, matching the create form's
// chained `{format === "x" && "..."}` rendering (nothing shown when no
// branch matches).
export function formatHint(format) {
  return FORMAT_OPTIONS.find((o) => o.value === format)?.hint ?? "";
}

// POOL_FORMAT_OPTIONS / LABEL_POOL_FORMAT: verbatim from admin_setup.jsx's
// "Round-robin shape" pills (full round-robin vs partial/neighbour-only).
export const LABEL_POOL_FORMAT = "Round-robin shape";
export const POOL_FORMAT_OPTIONS = [
  { value: POOL_FORMAT_FULL, label: "Full round-robin", hint: "Every participant plays every other participant in their pool." },
  { value: POOL_FORMAT_PARTIAL, label: "Partial / neighbour-only", hint: "Each participant plays a neighbourhood subset: useful when a full round-robin would not fit in the day's schedule." },
];

// poolFormatVisible: true wherever a competition builds a pool phase at all
// (bc-symm Gap 1). internal/engine/pools.go:149's `switch comp.PoolFormat`
// dispatches on this field for ANY pool-bearing competition -- the switch
// sits before and independent of any format check, and generatePools is
// called for both state.CompFormatMixed and state.CompFormatLeague
// (internal/engine/competition.go:904-907) -- so "mixed" and "league" are
// exactly the two formats where PoolFormat is meaningful. Previously this
// returned true for "league" only, mirroring the create form's historical
// gate rather than where the engine actually reads the field: create()
// already attached `c.poolFormat` to the payload for "mixed" too
// (admin_setup.jsx), and the import manifest has no poolFormat field at
// all, so a MIXED competition's pool shape could only ever be set to
// "partial" by hand-editing config.md. Fixed by widening this predicate;
// see also roundRobinVisible below, whose own gate depends on this one
// having gone live for mixed first.
export function poolFormatVisible(format) {
  return format === FORMAT_MIXED || format === FORMAT_LEAGUE;
}

// --- Swiss rounds -------------------------------------------------------
//
// COPY DRIFT (resolved): admin_setup.jsx labels this field "Number of
// rounds"; admin_competition_settings.jsx labels the identical field
// "Number of Swiss rounds". Picked the settings wording as the single
// label: it is self-contained. Both surfaces already gate this field on
// format === "swiss" so "Number of rounds" is unambiguous IN CONTEXT, but
// the label text itself is read out of that context too -- a screen
// reader announces the <label> alone, and an operator scanning a filled
// form (or a later summary/review surface built from this same copy)
// sees the field's name without the surrounding pill state. "Number of
// Swiss rounds" carries its own context; "Number of rounds" does not.
export const LABEL_SWISS_ROUNDS = "Number of Swiss rounds";
// Hint text is identical on both surfaces already; no drift to resolve.
export const HINT_SWISS_ROUNDS = "Typical: 4 rounds for 16 players, 5 for 32, 6 for 64 (≈ log₂ of field size).";

// swissRoundsVisible: both surfaces render the Swiss-rounds field only when
// format === "swiss".
export function swissRoundsVisible(format) {
  return format === FORMAT_SWISS;
}

// --- Round-robin-in-pools checkbox --------------------------------------
//
// Present on both surfaces (admin_setup.jsx, admin_competition_
// settings.jsx). Both used to render this checkbox unconditionally, but it
// is only ever READ in one place: internal/engine/pools.go:157 checks
// `comp.RoundRobin` solely inside the PoolFormat switch's `default` branch
// (PoolFormatFull, or an unset/unrecognized value) -- the
// PoolFormatPartial case at pools.go:150-152 calls
// helper.CreatePartialPoolMatches(pools) and never looks at RoundRobin at
// all, so unchecking the box does nothing once poolFormat is "partial".
// And internal/engine/competition.go:896 unconditionally overwrites
// `comp.RoundRobin = true` for CompFormatLeague before generatePools runs,
// regardless of what the operator stored, so the checkbox is equally inert
// for a league. That leaves exactly one format where toggling it changes
// anything: "mixed" with poolFormat !== "partial" (see roundRobinVisible).
// Elsewhere the control was a lie -- unchecking it left the UI showing
// "off" while the draw still built a full round-robin (league) or ran
// CreatePartialPoolMatches regardless (partial).
export const LABEL_ROUND_ROBIN = "Round-robin in pools";

// roundRobinVisible: gates the checkbox on the one combination where it is
// live -- see the field comment above for the pools.go/competition.go
// evidence. `poolFormat` is read loosely (only an exact "partial" hides
// the control), matching POOL_FORMAT_OPTIONS' own comment that a stored/
// legacy "" or any other value resolves to "full" for display purposes:
// the engine's switch reaches the same `default` branch for "", "full", or
// an unrecognized value, so the checkbox should be visible for all three.
export function roundRobinVisible(format, poolFormat) {
  return format === FORMAT_MIXED && poolFormat !== POOL_FORMAT_PARTIAL;
}

// --- League tie-break band ("Break ties for top") -----------------------
//
// Settings-only today (admin_competition_settings.jsx ~1090-1106); the
// create form has no equivalent control at all -- leagueTiebreakTopN is
// absent from admin_setup.jsx entirely.
export const LABEL_LEAGUE_TIEBREAK = "Break ties for top";
export const HINT_LEAGUE_TIEBREAK = "Tied teams within this finishing band require an operator-run tie-breaker before standings are finalised.";
// LEAGUE_TIEBREAK_OPTIONS: the stored value is an int (3 or 4), not a
// wire-string sentinel like the other option lists here, because
// state.Competition.LeagueTiebreakTopN is int on both sides of the wire.
// A stored/legacy 0 (unset) reads as "Top 3" is active -- see
// state.Competition's own doc comment ("Zero is treated as the default
// (3)...") and admin_competition_settings.jsx:1094's
// `(local.leagueTiebreakTopN || 0) === 0 || local.leagueTiebreakTopN === 3`
// active-pill check -- but 0 is not a selectable OPTION, only the default
// resolution of "not yet chosen", so it is not listed as a value here.
export const LEAGUE_TIEBREAK_OPTIONS = [
  { value: 3, label: "Top 3" },
  { value: 4, label: "Top 4" },
];

// leagueTiebreakVisible: mirrors admin_competition_settings.jsx's gate,
// `local.format === "league" && (local.teamSize > 0 || local.kind ===
// "team")`, AS CLOSELY AS A TWO-ARGUMENT (format, kind) PREDICATE CAN.
//
// The teamSize term is NOT redundant with kind. `kind !== "team"` paired
// with `teamSize > 0` is real, if inconsistent, stored data that
// models.go:885-886 calls out by name ("kind=\"individual\" with
// teamSize=3"), and the settings page shows the tie-break control for
// exactly those records today. Dropping the term would hide a control an
// operator can currently see, on the records least able to afford a
// surprise, so the predicate takes teamSize rather than narrowing to kind.
export function leagueTiebreakVisible(format, kind, teamSize) {
  const isTeam = kind === KIND_TEAM || (Number.isFinite(teamSize) && teamSize > 0);
  return format === FORMAT_LEAGUE && isTeam;
}

// --- Per-phase match duration ------------------------------------------
//
// Settings-only today (admin_competition_settings.jsx ~972-989); the
// create form has no duration inputs at all -- a newly created competition
// always starts on the scheduler's built-in default duration, set only
// after creation via Settings.
//
// The pool/round duration field's LABEL and HINT are format-dependent (the
// Swiss variant calls it a "round", not a "pool"); LABEL_POOL_DURATION /
// HINT_POOL_DURATION below are the base (mixed/league) copy the required
// export names refer to, and poolDurationLabel/poolDurationHint resolve
// the format-conditional text exactly as
// admin_competition_settings.jsx:976-978 does today, so a caller never
// re-implements that ternary.
export const LABEL_POOL_DURATION = "Pool match duration";
const LABEL_ROUND_DURATION = "Round match duration";
export const HINT_POOL_DURATION = "Estimated time per pool match, as m:ss (e.g. 2:30).";
const HINT_ROUND_DURATION = "Estimated time per Swiss-round match, as m:ss (e.g. 2:30).";

export function poolDurationLabel(format) {
  return format === FORMAT_SWISS ? LABEL_ROUND_DURATION : LABEL_POOL_DURATION;
}

export function poolDurationHint(format) {
  return format === FORMAT_SWISS ? HINT_ROUND_DURATION : HINT_POOL_DURATION;
}

export const LABEL_PLAYOFF_DURATION = "Playoff match duration";
export const HINT_PLAYOFF_DURATION = "Estimated time per playoff/knockout match, as m:ss (e.g. 2:30).";

// poolDurationVisible / playoffDurationVisible: mirror
// admin_competition_settings.jsx:972-985's two independent format gates
// (the row itself renders for mixed/league/playoffs/swiss; within it, the
// pool/round field renders for mixed/league/swiss and the playoff field
// for playoffs/mixed -- "mixed" runs both phases and shows both fields).
export function poolDurationVisible(format) {
  return format === FORMAT_MIXED || format === FORMAT_LEAGUE || format === FORMAT_SWISS;
}

export function playoffDurationVisible(format) {
  return format === FORMAT_PLAYOFFS || format === FORMAT_MIXED;
}

// --- League joint-3rd-place convention -----------------------------------
//
// Present on BOTH surfaces (admin_setup.jsx:1081, admin_competition_
// settings.jsx:1111) with IDENTICAL copy already -- checked verbatim,
// nothing to unify.
export const LABEL_TWO_THIRD_PLACES = "Award two joint 3rd places";
export const HINT_TWO_THIRD_PLACES = "When enabled, competitors genuinely tied for 3rd share bronze (standard kendo convention). Leave off for naginata, which awards a single 3rd place.";

// twoThirdPlacesVisible: both surfaces gate this checkbox on format ===
// "league" alone (not kind: an individual league can still award joint
// bronze under the kendo convention).
export function twoThirdPlacesVisible(format) {
  return format === FORMAT_LEAGUE;
}

// --- Kind-gated fields (team size, team match format, zekken, engi) -----
//
// LABEL_TEAM_SIZE / LABEL_TEAM_MATCH_TYPE / TEAM_MATCH_TYPE_OPTIONS:
// verbatim from admin_setup.jsx's "Team size" input and "Team match format"
// pills; admin_competition_settings.jsx renders the identical field and
// pills under the identical teamFieldsVisible gate immediately below.
// TEAM_MATCH_TYPE_OPTIONS' value strings mirror state.TeamMatchTypeFixed /
// ...Kachinuki (internal/state/models.go) byte-for-byte, same as this
// file's other wire-value option lists; "fixed" is listed first because
// that is the order both screens' pills have always rendered in (Regular,
// then Kachinuki).
export const LABEL_TEAM_SIZE = "Team size";
export const LABEL_TEAM_MATCH_TYPE = "Team match format";
export const TEAM_MATCH_TYPE_OPTIONS = [
  { value: "fixed", label: "Regular" },
  { value: "kachinuki", label: "Kachinuki (winner stays on)" },
];

// teamFieldsVisible: both surfaces gate "Team size" and "Team match
// format" identically on kind === "team" (admin_setup.jsx:1370,1391;
// admin_competition_settings.jsx:858,884).
export function teamFieldsVisible(kind) {
  return kind === KIND_TEAM;
}

// LABEL_ZEKKEN / LABEL_ENGI: verbatim from admin_setup.jsx's "Use Zekken
// display name" and "Engi (kata competition)" checkbox copy. Both screens
// render the same two checkboxes -- see zekkenApplies/engiApplies just
// below for the show-vs-disable split they DELIBERATELY keep local rather
// than folding into these constants.
export const LABEL_ZEKKEN = "Use Zekken display name";
export const LABEL_ENGI = "Engi (kata competition)";

// zekkenApplies / engiApplies: the RULE (zekken display names and engi
// pairs are individual-only concepts), NOT a presentation instruction.
// Both surfaces agree on the rule and DELIBERATELY differ on how they show
// it, so the rule is what lives here and the presentation stays local:
//
//   create   (admin_setup.jsx:1417,1429) HIDES both controls for a team
//            competition. Correct there: kind is freely switchable on an
//            empty form, so a hidden control comes straight back.
//   settings (admin_competition_settings.jsx:1240,1248) RENDERS both and
//            disables them, with the hint "(Only applicable for individual
//            competitions)". Correct there for the opposite reason: once a
//            roster exists the kind toggle is itself locked
//            (kindChangeBlockedReason below), so hiding these would leave
//            an operator unable to see why a setting they remember is
//            gone, with no way to reveal it.
//
// Naming them *Applies rather than *Visible is the point: a caller reads
// "does this concept apply to this kind" and then applies its own
// show-vs-disable policy, the same way it already layers isDrawReady /
// isStarted on top.
export function zekkenApplies(kind) {
  return kind === KIND_INDIVIDUAL;
}

export function engiApplies(kind) {
  return kind === KIND_INDIVIDUAL;
}

// --- Pool sizing field labels (mixed format only) -----------------------
//
// LABEL_POOL_SIZE / LABEL_POOL_WINNERS / LABEL_EXTRA_QUALIFIERS: verbatim
// from admin_setup.jsx's "Players per pool" / "Winners per pool" /
// "Knockout qualifiers" <label> text, rendered by both screens under the
// identical FORMAT_MIXED gate. These are the FIELD's own label, not the
// "Knockout qualifiers" radio's per-option copy ("Standard" / "Oversized
// send +1" / "Fit the knockout") -- that already lives in
// qualifier_preview.jsx's extraQualifiersLabel/extraQualifiersHint (see
// this file's header for why the split), so LABEL_EXTRA_QUALIFIERS only
// carries the <label> above the radio, not what is on the pills inside it.
export const LABEL_POOL_SIZE = "Players per pool";
export const LABEL_POOL_WINNERS = "Winners per pool";
export const LABEL_EXTRA_QUALIFIERS = "Knockout qualifiers";

// --- poolSettingsError ----------------------------------------------------
//
// The ONE owner of the mixed-format pool-settings rule (poolSize >= 3,
// poolWinners >= 1, both whole numbers), taken verbatim from the create
// form's admin_setup.jsx `validatePoolSettings` so the two thresholds
// cannot drift apart. Knockout ("playoffs") has no pools and league runs a
// single round-robin without a user-configured size, so both are exempt;
// non-"mixed" always returns null.
//
// Returns `string | null` -- the first problem found, or null when the
// combination is fine -- matching this codebase's other error helpers
// (e.g. shiaijoPickerError) rather than the `{ok, error}` shape the create
// form's caller wants. admin_setup.jsx's validatePoolSettings is now a thin
// adapter over this function that reshapes the return for its own callers
// and __tests__/admin_setup.test.jsx, which depend on that shape.
//
// Why settings needs this too, and not just create: normalizePoolConfig
// (internal/mobileapp/handlers_competition.go) zeroes poolSize/poolWinners
// on every stored league/playoffs competition, so ANY such competition sits
// on disk with poolSize: 0. normalizeConfigForFormat above only clears
// those fields on the way OUT of "mixed" -- flipping back INTO "mixed" is a
// no-op for them -- so an operator who switches a stored playoffs
// competition to "mixed" on the Settings screen lands here with poolSize
// still 0 and poolWinners still 0, staged nowhere else. Without this check
// the "Players per pool" field renders "0", Save stays enabled, and the PUT
// takes a 400 whose raw server string ("mixed format requires a pool size
// of at least 1") reaches the operator verbatim -- while the create form
// already blocks the identical combination client-side with the friendlier
// copy below. This function closes that parity gap.
//
// The poolSize >= 3 floor here is DELIBERATELY stricter than the server's:
// validateMixedPoolSize (internal/mobileapp/handlers_competition.go)
// rejects only poolSize <= 0, and the engine's own bound is looser still
// (internal/engine/pools.go: "pool size must be at least 1"). The gap is
// intentional, not a drift to close -- 3 is a PRODUCT floor (the smallest
// pool a round-robin is worth running), while the server stays at its
// actual functional minimum so a hand-edited or imported config.md is not
// retroactively made invalid by a UI-only rule it never agreed to. A
// maintainer tightening either bound should re-read the other side's
// comment first: they are meant to stay apart, not converge.
export function poolSettingsError(format, poolSize, winners) {
  if (format !== FORMAT_MIXED) return null;
  if (!Number.isInteger(poolSize) || poolSize < 3) {
    return "Players per pool must be a whole number ≥ 3.";
  }
  if (!Number.isInteger(winners) || winners < 1) {
    return "Winners per pool must be a whole number ≥ 1.";
  }
  return null;
}

// --- normalizeConfigForFormat --------------------------------------------
//
// Client mirror of the server's normalizePoolConfig +
// normalizeExtraQualifiers (internal/mobileapp/handlers_competition.go:
// 223-230, 272-277), so a format flip staged on EITHER screen leaves the
// same coherent config the server would have produced from the same
// change, instead of showing the operator a poolSize/poolWinners/
// extraQualifiers value that the next save silently discards.
//
// Pure: returns a NEW object, never mutates `cfg`. Field names are the
// canonical wire/competition-record names (state.Competition's JSON tags:
// poolSize, poolWinners, extraQualifiers), the same names
// admin_competition_settings.jsx's `local` object already uses directly
// (data.jsx's buildCompetition is what renames the create form's local
// `poolMode`/`winnersPerPool` state vars to poolSizeMode/poolWinners on
// construction -- by the time either screen holds a competition-shaped
// object, the field is `poolWinners`).
//
// CALL THIS ONLY AT THE PAYLOAD BOUNDARY (AdminSettings.saveNow's
// `shaped`), NEVER from a Format pill's onClick. It used to run from the
// pill tap itself (the old `updateFormat` handler staged its result
// straight into `local`), which quietly destroyed operator data:
// reproduced on a stored mixed competition (poolSize: 4, poolWinners: 2)
// by tapping "Knockout only" (stages poolSize/poolWinners: 0) and then
// tapping "Pools + Knockout" to go straight back -- a NO-OP for those
// fields going back INTO mixed, per this function's own "league / playoffs"
// bullet below, which only clears them on the way OUT -- so two taps that
// cancelled out on `format` left poolSize/poolWinners at 0 with no control
// on screen able to recover them, and Save blocked by poolSettingsError.
// If a future change is tempted to re-normalize on the control's onChange
// again, re-read this paragraph first: pendingConfigClears exists
// specifically so normalization can stay at the boundary and still warn
// the operator ahead of Save, without needing to run early.
//
// - league / playoffs: PoolSize and PoolWinners are zeroed (no pool phase
//   to size; league's single implicit pool and playoffs' bare bracket both
//   ignore these at runtime, per normalizePoolConfig's comment).
// - league / playoffs / swiss: ExtraQualifiers is forced to the standard
//   sentinel ("") -- the "Knockout qualifiers" radio only ever means
//   something on a pools-then-knockout ("mixed") competition.
export function normalizeConfigForFormat(cfg) {
  const next = { ...cfg };
  if (next.format === FORMAT_LEAGUE || next.format === FORMAT_PLAYOFFS) {
    next.poolSize = 0;
    next.poolWinners = 0;
  }
  if (next.format === FORMAT_LEAGUE || next.format === FORMAT_PLAYOFFS || next.format === FORMAT_SWISS) {
    // Inline "" rather than importing qualifier_preview.jsx's
    // EXTRA_QUALIFIERS_STANDARD: that constant IS "", this module has no
    // other reason to depend on qualifier_preview.jsx, and the "no
    // imports from other project modules" leaf constraint (see header)
    // rules out taking one just for a shared literal.
    next.extraQualifiers = "";
  }
  return next;
}

// --- normalizeConfigForKind ------------------------------------------------
//
// The kind half of normalizeConfigForFormat, and it exists for a sharper
// reason than tidiness: the three fields below are HIDDEN or DISABLED by
// the kind they belong to, so an operator who flips kind cannot reach them
// to fix what the flip invalidated -- but the surfaces still SEND their
// stale values, and the server rejects every one of those pairings:
//
// CALL THIS ONLY AT THE PAYLOAD BOUNDARY (AdminSettings.saveNow's
// `shaped`), NEVER from a Kind pill's onClick -- same rule as
// normalizeConfigForFormat above, for the identical failure shape. The old
// `updateKind` handler used to re-stage teamSize/teamMatchType/engi/
// withZekkenName via this function on every tap, which meant a team ->
// individual -> team round trip could destroy the operator's real team
// size the same way the format flip destroyed pool sizing (see
// normalizeConfigForFormat's comment for the reproduced case).
// Normalization now runs once, at the PUT payload boundary;
// pendingConfigClears tells the operator what that save is about to clear
// before they commit to it. Do not re-add a call from either screen's
// Kind-pill onClick.
//
//   teamSize       ValidateCompetitionTeamSize (state/models.go:891) rejects
//                  team+<2 AND non-team+>0. Flipping team->individual leaves
//                  teamSize 5 and 400s, with the Team size input now hidden.
//   teamMatchType  ValidateTeamMatchType (models.go:868) rejects kachinuki
//                  below teamSize 2. Note we set "fixed" and NOT "" going
//                  individual: both are valid to the server, but the
//                  settings PUT body builds this field as
//                  `effective.teamMatchType || latestC.teamMatchType || ""`,
//                  so an empty local value is re-filled from the STORED
//                  "kachinuki" and the 400 comes straight back.
//   engi           handlers_competition.go rejects engi on a team
//                  competition; the checkbox is disabled for team, so the
//                  operator cannot clear it themselves.
//
//   withZekkenName it survives no server rejection, which is why this was
//                  first left alone -- but that reasoning was wrong twice
//                  over. The create form already forces it false for a team
//                  (admin_setup.jsx: `kind === "individual" ? withZekken :
//                  false`), so preserving it here made the two surfaces
//                  disagree about what a team competition IS, which is the
//                  exact divergence this module exists to remove. And it is
//                  not inert: EffectiveWithZekkenName (state/models.go) has
//                  no kind term, so a team competition carrying it parses
//                  participants.csv with the 4-column zekken layout
//                  (state/participants.go) -- a roster shape create can
//                  never produce. The roster lock means no existing rows are
//                  re-read, but the next paste would use it.
//
// The create form encodes the same teamSize rule inline at data.jsx:215
// (`teamSize || (kind === "team" ? 5 : 0)`) because it builds a payload from
// scratch rather than editing one; that call site is equivalent, not a
// second opinion.
export const DEFAULT_TEAM_SIZE = 5;

// MIN_TEAM_SIZE: the legal floor for a team competition's team size, and
// the ONE place that number is written down. state.ValidateCompetitionTeamSize
// (internal/state/models.go:891-903) rejects teamSize == 1 outright ("use 0
// for individual competitions or >= 2 for teams") -- 1 is neither a valid
// individual value (which must be exactly 0) nor a valid team value (which
// must be >= 2), so it 400s unconditionally. Both surfaces' Team size
// number input had `min="1"` and floored a typed/stepped value at 1 (bc-symm
// Gap 3), so the UI could construct a request the server always refuses.
// Both call sites (admin_setup.jsx's onChange, admin_competition_
// settings.jsx's updateNumber("teamSize", ...)) and normalizeConfigForKind's
// own floor check below read MIN_TEAM_SIZE rather than repeating the
// literal, the same sharing pattern MAX_TEAM_SIZE already uses
// (admin_helpers.jsx) -- see that constant's own comment for the
// mirrored-cap rationale.
export const MIN_TEAM_SIZE = 2;

export function normalizeConfigForKind(cfg) {
  const next = { ...cfg };
  if (next.kind === KIND_TEAM) {
    if (!(Number.isFinite(next.teamSize) && next.teamSize >= MIN_TEAM_SIZE)) {
      next.teamSize = DEFAULT_TEAM_SIZE;
    }
    if (!next.teamMatchType) next.teamMatchType = "fixed";
    next.engi = false;
    next.withZekkenName = false;
  } else {
    next.teamSize = 0;
    next.teamMatchType = "fixed";
  }
  return next;
}

// --- kindChangeBlockedReason ----------------------------------------------
//
// Operator ruling: changing a competition's kind (individual <-> team)
// once a roster exists is DISABLED, not warn-and-clear. Individual and
// team rosters do not translate -- a team competition's name-uniqueness is
// enforced only on write (not by any client-side structure), and
// lineups.yaml is never revalidated against a roster that changed shape
// out from under it. admin_competition_settings.jsx wires this in as
// `kindLockReason`, OR'd with isDrawReady/isStarted to disable the Kind
// pills (create has no roster yet to conflict with, so it never calls
// this at all -- kind is freely switchable up to the moment of creation).
//
// Returns "" (free to change) when playerCount <= 0, or an operator-facing
// reason naming the way out (clear the roster) otherwise. Any non-finite/
// negative input is treated as "no roster", the same permissive-default
// stance decideNumericUpdate and friends take elsewhere in this codebase
// for a value that cannot represent a real blocking count.
export function kindChangeBlockedReason(playerCount) {
  const n = Number.isFinite(playerCount) ? playerCount : 0;
  if (n <= 0) return "";
  return `Competition type can't be changed with ${n} participant${n === 1 ? "" : "s"} loaded: individual and team rosters aren't compatible. Clear the participant list first.`;
}

// --- pendingConfigClears ----------------------------------------------------
//
// Operator ruling: a config change must never quietly overwrite or delete
// the operator's data. It must SURFACE what will happen and let the
// operator decide.
//
// The reproduced failure this exists to prevent: a stored mixed
// competition (poolSize: 4, poolWinners: 2) had its Format pill tapped to
// "Knockout only". Settings USED TO stage normalizeConfigForFormat's
// result straight into `local` on the tap itself, zeroing poolSize/
// poolWinners immediately. Tapping "Pools + Knockout" to go straight back
// is a no-op for those fields going back INTO mixed (normalizeConfigForFormat
// only clears them on the way OUT of "mixed" -- see its own comment), so two
// taps that cancelled out on `format` left poolSize/poolWinners at 0 with no
// operator action able to recover them, and Save blocked by
// poolSettingsError. Normalization now runs only at the PUT-payload boundary
// (AdminSettings.saveNow's `shaped`), never on the pill tap, so `local`
// never again holds a value the operator didn't type. This function is the
// other half of that fix: the clearing itself still genuinely happens on
// Save whenever it's warranted (a team -> individual flip really does need
// to drop teamSize, or the server 400s), so the operator needs to be told
// BEFORE they click Save, not find out via a rejected request or a field
// that silently reads empty afterward.
//
// Pure, like the rest of this module: computes what Save WOULD clear
// without staging anything into any state.
//
// Returns [] unless a format or kind change is actually STAGED, i.e.
// staged.format !== stored.format || staged.kind !== stored.kind. An
// untouched form must never show the notice, even if the record on disk is
// itself already inconsistent (e.g. hand-edited config.md) -- that is
// poolSettingsError/courtsErr's job to flag, not this one's; this function
// only speaks to what THIS edit is about to do.
//
// Otherwise runs the same two normalizers saveNow's `shaped` value does,
// and reports every key where the normalized result differs from what the
// operator currently has staged -- but only when the staged value is
// "meaningful" (isMeaningfulValue below): the point is to report DATA LOSS,
// so a field that was already 0/""/false is not worth naming -- it had
// nothing in it to lose.
//
// Iterates Object.keys(normalized) rather than a hardcoded field list, so a
// field a future normalizer starts touching is covered automatically,
// without a second edit here.
//
// Returns { key, from } pairs -- KEYS only, not labels: this module owns
// the RULE (what gets cleared, and what it held), the screen owns the
// RENDER (what to call each key on screen, how to phrase the sentence).
// That split is also why this can't reach qualifier_preview.jsx for the
// "Knockout qualifiers" label -- this is a leaf with no project imports,
// same as the rest of this module (see header).

// isMeaningfulValue: a staged value worth reporting as "about to be lost".
// 0 / "" / false / null / undefined / NaN are all how an unset or
// already-cleared field looks in this codebase (see safeInt/safeNonNegInt
// in admin_competition_settings.jsx for the NaN-as-"cleared" convention),
// so normalizing one of THOSE to the same resting value is not data loss.
function isMeaningfulValue(v) {
  if (v === 0 || v === "" || v === false || v === null || v === undefined) return false;
  if (typeof v === "number" && Number.isNaN(v)) return false;
  return true;
}

export function pendingConfigClears(stored, staged) {
  if (staged.format === stored.format && staged.kind === stored.kind) return [];
  const normalized = normalizeConfigForKind(normalizeConfigForFormat(staged));
  const clears = [];
  Object.keys(normalized).forEach((key) => {
    if (normalized[key] !== staged[key] && isMeaningfulValue(staged[key])) {
      clears.push({ key, from: staged[key] });
    }
  });
  return clears;
}

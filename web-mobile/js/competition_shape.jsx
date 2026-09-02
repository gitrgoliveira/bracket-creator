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

export const FORMAT_KNOCKOUT = "knockout";
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
  { value: FORMAT_KNOCKOUT, label: "Knockout only", hint: "Direct single-elimination knockout." },
  { value: FORMAT_MIXED, label: "Pools + Knockout", hint: "Round-robin pools first, then top finishers advance to a knockout bracket." },
  { value: FORMAT_LEAGUE, label: "League", hint: "Single round-robin across all participants; final standings determine the winner (no knockout)." },
  { value: FORMAT_SWISS, label: "Swiss", hint: "Swiss-system: fixed number of rounds, pairing players with equal win counts; cumulative standings decide the winner." },
];

// optionLabel: resolve a wire value through one of this module's option
// lists to its operator-facing copy. `field` picks which copy ("label" by
// default, "hint" for the lines under a pill group). Returns "" for an
// unrecognised or empty value, so a caller can render the result directly
// without a fallback -- an unknown value shows nothing rather than the raw
// wire string. Every option-list lookup in this codebase goes through here
// so the "?? \"\"" and the `.find` shape are not respelled per call site.
export function optionLabel(options, value, field = "label") {
  return options.find((o) => o.value === value)?.[field] ?? "";
}

// formatHint: the hint line shown under the Format pills for the given
// format. "" for an unrecognised/empty format, matching the create form's
// chained `{format === "x" && "..."}` rendering (nothing shown when no
// branch matches).
export function formatHint(format) {
  return optionLabel(FORMAT_OPTIONS, format, "hint");
}

// POOL_FORMAT_OPTIONS / LABEL_POOL_FORMAT: verbatim from admin_setup.jsx's
// "Round-robin shape" pills (full round-robin vs partial/neighbour-only).
export const LABEL_POOL_FORMAT = "Round-robin shape";
export const POOL_FORMAT_OPTIONS = [
  { value: POOL_FORMAT_FULL, label: "Full round-robin", hint: "Every participant plays every other participant in their pool." },
  { value: POOL_FORMAT_PARTIAL, label: "Partial / neighbour-only", hint: "Each participant plays a neighbourhood subset: useful when a full round-robin would not fit in the day's schedule." },
];

// resolvePoolFormat: a stored/legacy "" means "full" -- the engine's own
// unset default (internal/engine/pools.go's `switch comp.PoolFormat`
// falls through to the full round-robin arm). Both screens need this to
// pick the active pill AND the hint, and before this existed they
// disagreed: settings resolved `local.poolFormat || POOL_FORMAT_FULL`
// while create used the bare value, so an empty poolFormat lit no pill
// and showed no hint on the create form.
export function resolvePoolFormat(poolFormat) {
  return poolFormat || POOL_FORMAT_FULL;
}

// poolFormatHint: the hint under the "Round-robin shape" pills, the twin
// of formatHint above. Resolves the legacy-empty value first so the hint
// matches whichever pill is lit.
export function poolFormatHint(poolFormat) {
  return optionLabel(POOL_FORMAT_OPTIONS, resolvePoolFormat(poolFormat), "hint");
}

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

// MIN_SWISS_ROUNDS: the floor validateSwissConfig
// (internal/mobileapp/handlers_competition.go) enforces -- "swiss format
// requires swissRounds >= 1" -- written down once, next to the client rule
// that keeps a request below it off the wire. Same pattern as
// MIN_POOL_SIZE / MIN_TEAM_SIZE below.
export const MIN_SWISS_ROUNDS = 1;

// swissSettingsError: the ONE owner of the Swiss-rounds rule, moved here
// from admin_setup.jsx's validateSwissSettings (which is now a thin
// { ok, error } adapter over it, exactly as validatePoolSettings became one
// over poolSettingsError). Returns `string | null`, like every other error
// helper in this module.
//
// It is here rather than on the create form because settings needs it too,
// and by the same route poolSettingsError did: the Format editor this
// change added to the settings screen makes "swiss" reachable for a stored
// competition that has never had a round count, and swissRounds sits on
// disk as 0 for every non-Swiss competition (`omitempty` on the wire, so an
// untouched record simply has no value). Tapping the new "Swiss" pill
// therefore renders "Number of Swiss rounds" showing 0, with nothing
// blocking Save -- and the PUT takes a 400 whose raw server string reaches
// the operator verbatim, which is precisely the gap poolSettingsError
// exists to close on the sibling field.
export function swissSettingsError(format, swissRounds) {
  if (format !== FORMAT_SWISS) return null;
  if (!Number.isInteger(swissRounds) || swissRounds < MIN_SWISS_ROUNDS) {
    return `${LABEL_SWISS_ROUNDS} must be a whole number ≥ ${MIN_SWISS_ROUNDS}.`;
  }
  return null;
}

// --- Round-robin-in-pools checkbox --------------------------------------
//
// Present on both surfaces (admin_setup.jsx, admin_competition_
// settings.jsx). Both used to render this checkbox unconditionally, but
// only ONE read of the flag decides anything the operator can see, and it
// is internal/engine/pools.go:157: `comp.RoundRobin` is checked solely
// inside the PoolFormat switch's `default` branch (PoolFormatFull, or an
// unset/unrecognized value) -- the PoolFormatPartial case at
// pools.go:150-152 calls helper.CreatePartialPoolMatches(pools) and never
// looks at RoundRobin at all, so unchecking the box does nothing once
// poolFormat is "partial".
// And internal/engine/competition.go:896 unconditionally overwrites
// `comp.RoundRobin = true` for CompFormatLeague before generatePools runs,
// regardless of what the operator stored, so the checkbox is equally inert
// for a league. That leaves exactly one format where toggling it changes
// anything: "mixed" with poolFormat !== "partial" (see roundRobinVisible).
// Elsewhere the control was a lie -- unchecking it left the UI showing
// "off" while the draw still built a full round-robin (league) or ran
// CreatePartialPoolMatches regardless (partial).
//
// The flag IS read in three other places, none of which widens that rule
// -- they are listed here because "only read in one place" was this
// comment's original wording and it was simply false, which is a bad
// footing for the sole justification given for removing a control
// operators used to have:
//
//   internal/engine/estimate_schedule.go:47 copies it into
//     helper.EstimateMatchCountsInput, where poolMatchesPerPool
//     (helper/estimate.go:189) branches on it -- but only down the
//     estimator's `case "mixed"` path. `case "league"`
//     (helper/estimate.go:87-89) computes N*(N-1)/2 directly and never
//     consults the flag, matching competition.go:896's forced true. So the
//     estimator agrees with the draw on every format, and agrees with this
//     rule about which format the operator can move.
//   internal/engine/competition.go:1022 compares it against the value
//     loaded at the top of StartCompetition, to abort a draw whose config
//     changed underneath it. A drift check, not a behaviour switch.
//   internal/mobileapp/handlers_competition.go:1344 puts it in the
//     output-affecting set that forces a redraw while draw-ready -- which
//     is why the checkbox carries the same isDrawReady lock as poolSize.
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
export const LEAGUE_TIEBREAK_DEFAULT = 3;
export const LEAGUE_TIEBREAK_OPTIONS = [
  { value: LEAGUE_TIEBREAK_DEFAULT, label: "Top 3" },
  { value: 4, label: "Top 4" },
];

// leagueTiebreakActive: is `option` the lit pill for the stored value?
// The "a stored/legacy 0 (unset) reads as Top 3" rule described above was
// previously stated in THIS COMMENT ONLY, while both screens carried the
// same nested ternary
//   (o.value === 3 ? ((v || 0) === 0 || v === 3) : v === o.value)
// with the 3 hardcoded a second and third time. That is precisely the
// per-surface chain this module exists to replace, so the rule is now
// executable here and the screens ask instead of re-deriving.
export function leagueTiebreakActive(option, stored) {
  return option === (stored || LEAGUE_TIEBREAK_DEFAULT);
}

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

export const LABEL_KNOCKOUT_DURATION = "Knockout match duration";
export const HINT_KNOCKOUT_DURATION = "Estimated time per knockout match, as m:ss (e.g. 2:30).";

// poolDurationVisible / knockoutDurationVisible: mirror
// admin_competition_settings.jsx:972-985's two independent format gates
// (the row itself renders for mixed/league/knockout/swiss; within it, the
// pool/round field renders for mixed/league/swiss and the knockout field
// for knockout/mixed -- "mixed" runs both phases and shows both fields).
export function poolDurationVisible(format) {
  return format === FORMAT_MIXED || format === FORMAT_LEAGUE || format === FORMAT_SWISS;
}

export function knockoutDurationVisible(format) {
  return format === FORMAT_KNOCKOUT || format === FORMAT_MIXED;
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
export const TEAM_MATCH_TYPE_FIXED = "fixed";
export const TEAM_MATCH_TYPE_KACHINUKI = "kachinuki";
export const TEAM_MATCH_TYPE_OPTIONS = [
  { value: TEAM_MATCH_TYPE_FIXED, label: "Regular" },
  { value: TEAM_MATCH_TYPE_KACHINUKI, label: "Kachinuki (winner stays on)" },
];

// teamMatchTypeActive: which "Team match format" pill is lit for a stored
// value. NOT plain equality, and the asymmetry is the rule: a competition
// can hold a legacy/stored value that is neither exactly "fixed" nor
// exactly "kachinuki" (the empty sentinel, see normalizeConfigForKind), and
// ValidateTeamMatchType (internal/state/models.go) reads "" as fixed. So
// only "kachinuki" is matched exactly and everything else lights Regular.
//
// Both screens now read this. The settings screen already did it inline; the
// create form used plain equality, with a comment arguing it was safe there
// because its local state can only ever hold one of the two canonical
// values. That was true and beside the point -- the same question answered
// two ways in two files is the shape every drift in this module's history
// started as.
export function teamMatchTypeActive(option, stored) {
  return option === TEAM_MATCH_TYPE_KACHINUKI
    ? stored === TEAM_MATCH_TYPE_KACHINUKI
    : stored !== TEAM_MATCH_TYPE_KACHINUKI;
}


// resolveKind: what a stored `kind` MEANS, as opposed to what it says.
//
// Every other loose field in this module has one of these
// (resolvePoolFormat, resolvePoolSizeMode, resolveTeamSize) and kind needs
// it for the same reason: "" is a first-class member of the set, not a
// straggler. state.ValidateCompetitionKind accepts `""`, `"individual"`
// and `"team"`, and its doc comment spells out that "" MEANS individual --
// an import manifest with no `kind:` key decodes to "", and
// state.Competition's Go zero value stores the same "", so both the legacy
// import path and any hand-seeded record depend on that reading.
//
// So the predicates below must ask "is this team?" and not "is this
// exactly the string 'individual'?". Written the second way, a legacy
// record whose kind is "" got Zekken and Engi rendered DISABLED under the
// hint "(Only applicable for individual competitions)" -- on a competition
// that is individual. Anything not exactly "team" resolves to individual
// here, matching how the engine already behaves: Kind is checked as
// `== "team"` at every site that branches on it, so an unrecognised value
// has always run as an individual competition (which is the hole
// ValidateCompetitionKind was added to close at the write doors, leaving
// this function to agree with the engine about everything already stored).
export function resolveKind(kind) {
  return kind === KIND_TEAM ? KIND_TEAM : KIND_INDIVIDUAL;
}

// teamFieldsVisible: both surfaces gate "Team size" and "Team match
// format" identically on kind === "team" (admin_setup.jsx:1370,1391;
// admin_competition_settings.jsx:858,884).
export function teamFieldsVisible(kind) {
  return resolveKind(kind) === KIND_TEAM;
}

// LABEL_ZEKKEN / LABEL_ENGI: verbatim from admin_setup.jsx's "Use Zekken
// display name" and "Engi (kata competition)" checkbox copy. Both screens
// render the same two checkboxes -- see zekkenApplies/engiApplies just
// below for the show-vs-disable split they DELIBERATELY keep local rather
// than folding into these constants.
export const LABEL_ZEKKEN = "Use Zekken display name";
export const LABEL_ENGI = "Engi (kata competition)";
export const HINT_ZEKKEN = "When enabled, participant CSV uses three columns: Name, Zekken, Dojo.";
export const HINT_ENGI = "Flag-count scoring for Engi-Kyogi pairs. Enter each pair as one participant: Name 1 - Name 2, Dojo.";

// --- The remaining shared checkboxes / text fields ----------------------
//
// Naginata, Check-in and the player-number prefix are rendered by BOTH
// screens and were the last shared controls whose copy lived nowhere.
//
// The check-in HINT had ALREADY DRIFTED by the time it was hoisted: create
// said "Show check-in column and counter for this competition." while
// settings said "Show check-in column and counter. Disable for
// competitions that don't need attendance tracking." The settings wording
// is kept -- it is the one that says what turning the control OFF is for,
// which is the only reason an operator touches it. That drift is the exact
// failure mode a half-converted field invites: hoisting the LABEL alone
// makes the label undriftable while leaving the sentence under it free,
// and the parity guard only sees strings this module owns. So a hint is
// hoisted WITH its label, never after it.
export const LABEL_NAGINATA = "Naginata competition";
export const HINT_NAGINATA = "Adds the Sune (S) ippon button to the score editor. Use for Naginata divisions.";
export const LABEL_CHECK_IN = "Check-in tracking";
export const HINT_CHECK_IN = "Show check-in column and counter. Disable for competitions that don't need attendance tracking.";
export const LABEL_NUMBER_PREFIX = "Player number prefix";
export const HINT_NUMBER_PREFIX = "Single letter prefix for participant numbers (A1, B1…). Keeps numbers unique across competitions.";
// HINT_KIND_ONLY_INDIVIDUAL: settings shows this in place of the zekken /
// engi hint when the competition is a team one, standing in for the hint
// rather than sitting beside it (see zekkenApplies above for why settings
// disables where create hides).
export const HINT_KIND_ONLY_INDIVIDUAL = "(Only applicable for individual competitions)";

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
// Both read through resolveKind (above) rather than comparing against
// KIND_INDIVIDUAL directly: a stored "" MEANS individual, and asking the
// question the other way round disabled both controls on a legacy record
// under a hint saying they are for individual competitions.
export function zekkenApplies(kind) {
  return resolveKind(kind) === KIND_INDIVIDUAL;
}

export function engiApplies(kind) {
  return resolveKind(kind) === KIND_INDIVIDUAL;
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

// --- "Pool size is a" (poolSizeMode) --------------------------------------
//
// POOL_SIZE_MODE_MAX / _MIN are the two values the pills write.
// resolvePoolSizeMode is what a STORED value means, and the distinction
// matters because the stored field has a third state the pills do not: the
// empty string.
//
// Nothing on the server defaults it. POST /api/competitions trims
// PoolSizeMode and validates the rest of the record around it, but never
// fills it in (handlers_competition.go); only the tournament-import path
// does (handlers_import.go: `if comp.PoolSizeMode == "" { ... = "max" }`)
// and only the SPA's own buildCompetition supplies one (data.jsx:
// `poolMode || "max"`). So a competition authored by any other client sits
// on disk with "", and the engine reads that as MINIMUM sizing -- every
// consumer spells the test `isMax := PoolSizeMode == "max"`, so "" falls
// to the min branch.
//
// Before the Format editor landed on the settings screen those records
// could not reach the "Pool size is a" control at all, because it renders
// only for "mixed" and a knockout-only competition could not be switched
// to "mixed" from that screen. Now it can, and a bare `=== "max"` /
// `=== "min"` equality lights NEITHER pill for a stored "" while the draw
// quietly runs minimum sizing -- the identical failure resolvePoolFormat
// (above) exists to fix on the sibling field, where an empty poolFormat lit
// no pill and showed no hint.
//
// Also passed to extraQualifiersRadioVisible (qualifier_preview.jsx), whose
// gate is a strict `poolMode === "min"`: unresolved, it hides the
// "Knockout qualifiers" radio on exactly the records the SERVER would
// accept a non-standard qualifier setting for. The predicate keeps its
// strict contract and the call sites hand it a resolved value, rather than
// this rule being respelled inside it.
// LABEL_COURTS: the shiaijo picker's caption. A bare literal on BOTH screens
// until now -- the one shared control label that never made it into this
// module, because the picker itself (window.courtPillOptions) is not shared
// and the label came along with it.
export const LABEL_COURTS = "Assigned shiaijo (courts)";

// HINT_TEAM_SIZE: the five kendo positions. The create form has always shown
// this under "Team size"; the settings screen showed the same input with NO
// hint at all, so an operator editing an existing competition got less help
// than one creating it. Now both render it.
export const HINT_TEAM_SIZE = "Standard kendo team is 5 (Senpou, Jihou, Chuken, Fukushou, Taishou).";

// HINT_POOL_WINNERS_LOCKED: shown under "Winners per pool" while the knockout
// qualifiers setting has pinned it to 1. Verbatim in both files before this.
export const HINT_POOL_WINNERS_LOCKED = "Set to 1 by the knockout qualifiers setting below.";

export const LABEL_POOL_SIZE_MODE = "Pool size is a";
export const POOL_SIZE_MODE_MAX = "max";
export const POOL_SIZE_MODE_MIN = "min";
// The two pills' own copy. Both screens inlined this array verbatim, which
// left the label text and the wire values paired up in two places; the
// option list is the pairing, so it belongs with the values.
export const POOL_SIZE_MODE_OPTIONS = [
  { value: POOL_SIZE_MODE_MAX, label: "maximum" },
  { value: POOL_SIZE_MODE_MIN, label: "minimum" },
];

export function resolvePoolSizeMode(poolSizeMode) {
  return poolSizeMode === POOL_SIZE_MODE_MAX ? POOL_SIZE_MODE_MAX : POOL_SIZE_MODE_MIN;
}

// --- poolSettingsError ----------------------------------------------------
//
// The ONE owner of the mixed-format pool-settings rule (poolSize >= 3,
// poolWinners >= 1, both whole numbers), taken verbatim from the create
// form's admin_setup.jsx `validatePoolSettings` so the two thresholds
// cannot drift apart. Knockout has no pools and league runs a
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
// on every stored league/knockout competition, so ANY such competition sits
// on disk with poolSize: 0. normalizeConfigForFormat above only clears
// those fields on the way OUT of "mixed" -- flipping back INTO "mixed" is a
// no-op for them -- so an operator who switches a stored knockout
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
// MIN_POOL_SIZE / MIN_POOL_WINNERS: the same two floors the check below
// enforces, exported so the `min=` attributes and decideNumericUpdate
// clamps on both screens read them instead of repeating a bare 3 and 1.
// Before this, both screens' comments claimed "poolSettingsError owns both
// thresholds" -- true of the MESSAGE and not of the input floors, which
// were literals four call sites over from the constant they had to match.
// Same pattern as MIN_TEAM_SIZE below.
export const MIN_POOL_SIZE = 3;
export const MIN_POOL_WINNERS = 1;

export function poolSettingsError(format, poolSize, winners) {
  if (format !== FORMAT_MIXED) return null;
  // Interpolate the field's own caption rather than restating it, the way
  // swissSettingsError already does with LABEL_SWISS_ROUNDS: spelled as a
  // literal, renaming the "Players per pool" control changed the caption and
  // left the error message naming the old one.
  if (!Number.isInteger(poolSize) || poolSize < MIN_POOL_SIZE) {
    return `${LABEL_POOL_SIZE} must be a whole number ≥ ${MIN_POOL_SIZE}.`;
  }
  if (!Number.isInteger(winners) || winners < MIN_POOL_WINNERS) {
    return `${LABEL_POOL_WINNERS} must be a whole number ≥ ${MIN_POOL_WINNERS}.`;
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
// fields going back INTO mixed, per this function's own "league / knockout"
// bullet below, which only clears them on the way OUT -- so two taps that
// cancelled out on `format` left poolSize/poolWinners at 0 with no control
// on screen able to recover them, and Save blocked by poolSettingsError.
// If a future change is tempted to re-normalize on the control's onChange
// again, re-read this paragraph first: pendingConfigClears exists
// specifically so normalization can stay at the boundary and still warn
// the operator ahead of Save, without needing to run early.
//
// - league / knockout: PoolSize and PoolWinners are zeroed (no pool phase
//   to size; league's single implicit pool and knockout' bare bracket both
//   ignore these at runtime, per normalizePoolConfig's comment).
// - league / knockout / swiss: ExtraQualifiers is forced to the standard
//   sentinel ("") -- the "Knockout qualifiers" radio only ever means
//   something on a pools-then-knockout ("mixed") competition.
//
// PoolFormat is DELIBERATELY not in that list, and the omission has been
// re-raised in review, so here is why it stays out. This function is a
// mirror of the server's normalizePoolConfig, and the server does not
// touch PoolFormat for any format -- so clearing it here would make the
// client destroy a field the server preserves, which is a new divergence
// rather than a closed one. It would also be the very failure the
// paragraph above documents for poolSize: PoolFormat is meaningful for
// BOTH "mixed" and "league" (poolFormatVisible), so a league operator who
// taps "Knockout only" and taps straight back would lose the
// neighbour-only shape they chose, with the round trip cancelling out on
// `format` and silently not on this field. Carrying it across a format
// the field has no meaning for is what PRESERVING it looks like; a value
// that reappears when the control that owns it comes back on screen is
// the control working.
export function normalizeConfigForFormat(cfg) {
  const next = { ...cfg };
  if (next.format === FORMAT_LEAGUE || next.format === FORMAT_KNOCKOUT) {
    next.poolSize = 0;
    next.poolWinners = 0;
  }
  if (next.format === FORMAT_LEAGUE || next.format === FORMAT_KNOCKOUT || next.format === FORMAT_SWISS) {
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

// resolveTeamSize: what a STAGED team size actually means, resolved against
// the last-saved one. An unusable staged value -- cleared to NaN, or typed
// below MIN_TEAM_SIZE (the number input's own `min`, so the browser already
// marks it invalid) -- says "the operator has not supplied a usable team
// size", not "the operator wants 1", so it reads as the stored value.
//
// This has to run BEFORE normalizeConfigForKind, and that ordering is the
// whole point. normalizeConfigForKind rewrites teamSize on BOTH branches (0
// going individual, DEFAULT_TEAM_SIZE for a team whose value is under the
// floor), so its output is ALWAYS a finite integer -- which means a
// "fall back to the stored value if this isn't usable" guard applied to its
// output can never fire. admin_competition_settings.jsx had exactly that
// guard, reading `safeNonNegInt(shaped.teamSize, latestC.teamSize)`, and it
// was dead: clearing the Team size input on a stored 3 silently saved 5,
// while the input's own comment promised a fall back to the last-saved
// value. Resolving first keeps that promise and still lets the normalizer
// stage its deliberate 0 on a team -> individual flip, because that 0 comes
// from the KIND branch and never from this function.
//
// Both of the settings screen's readers call it -- the payload boundary and
// the pendingConfigClears notice -- so the warning can't name a value the
// save will not actually send.
export function resolveTeamSize(staged, stored) {
  if (Number.isInteger(staged) && staged >= MIN_TEAM_SIZE) return staged;
  return Number.isInteger(stored) && stored >= 0 ? stored : 0;
}

// teamSizeError: the same shape as poolSettingsError / swissSettingsError,
// and it exists because Team size was the one bounded numeric field with a
// silent failure instead of a visible one.
//
// The create form has always refused an out-of-range team size at submit
// (two inline branches, "must be a whole number" / "must be between N and
// M"). The settings screen had NO equivalent: resolveTeamSize above
// deliberately falls back to the stored value rather than sending a
// clobbering 0, which is right for the PAYLOAD and wrong as the operator's
// only feedback -- typing 1 into Team size and clicking Save stored the
// OLD value and reported "✓ Saved", so the screen claimed to have saved
// something it had discarded. That is the divergence this module exists to
// close, so the rule moves here and both screens read it.
//
// `maxTeamSize` is a PARAMETER rather than a constant of this module: the
// ceiling is not a config rule, it is how many bout positions the scoring
// UI can render (MAX_TEAM_SIZE in admin_helpers.jsx, which TEAM_POSITIONS
// in admin_scoring_modal.jsx is built from). This module is a leaf with no
// project imports, and inventing a second copy of that number here is
// exactly the drift it is meant to prevent. Callers pass their own
// MAX_TEAM_SIZE; omitting it checks the floor alone.
//
// Returns null when the field does not apply (individual competitions send
// teamSize 0 by rule), so a caller can gate on the error without first
// repeating the kind test.
export function teamSizeError(kind, teamSize, maxTeamSize) {
  if (!teamFieldsVisible(kind)) return null;
  if (!Number.isInteger(teamSize) || teamSize < MIN_TEAM_SIZE) {
    return `${LABEL_TEAM_SIZE} must be a whole number ≥ ${MIN_TEAM_SIZE}.`;
  }
  if (Number.isInteger(maxTeamSize) && teamSize > maxTeamSize) {
    return `${LABEL_TEAM_SIZE} must be between ${MIN_TEAM_SIZE} and ${maxTeamSize}.`;
  }
  return null;
}

export function normalizeConfigForKind(cfg) {
  const next = { ...cfg };
  if (next.kind === KIND_TEAM) {
    if (!(Number.isFinite(next.teamSize) && next.teamSize >= MIN_TEAM_SIZE)) {
      next.teamSize = DEFAULT_TEAM_SIZE;
    }
    if (!next.teamMatchType) next.teamMatchType = TEAM_MATCH_TYPE_FIXED;
    next.engi = false;
    next.withZekkenName = false;
  } else {
    next.teamSize = 0;
    next.teamMatchType = TEAM_MATCH_TYPE_FIXED;
  }
  return next;
}

// twoThirdPlacesForNaginata: ticking "Naginata competition" clears "Award
// two joint 3rd places", and unticking it restores them. Naginata awards a
// single 3rd place where kendo awards two -- the two hints say exactly that,
// and state.Competition's LeagueTwoThirdPlaces comment says it a third time.
//
// The create form has always applied this coupling, inline in the naginata
// checkbox's onChange. The settings screen did not, so ticking Naginata
// there left the kendo two-joint-3rds convention in place on a naginata
// competition, and an operator who set the discipline on the screen built
// for editing got a different config from one who set it at creation.
// Same shape as resetExtraQualifiersOnPoolModeChange (qualifier_preview.jsx):
// a coupling between two controls, owned once, called from both screens'
// onChange.
export function twoThirdPlacesForNaginata(naginataOn) {
  return !naginataOn;
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
// Reports every key where the config the save will actually SEND
// (shapeConfigForSave, immediately above -- the very function the settings
// payload boundary calls) differs from what the operator has staged, and
// only when the staged value is
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

// --- configShapeChangeStaged / shapeConfigForSave --------------------------
//
// configShapeChangeStaged: does THIS edit stage a change to one of the two
// fields the normalizers key on? shapeConfigForSave: the config a save
// should actually send, which is the staged config UNTOUCHED unless such a
// change is staged.
//
// The scoping is the point, and it is the same scoping the server applies a
// few lines apart in handlers_competition.go, for the same reason its own
// comment gives there -- "change-scoped to avoid locking an operator out of
// an already-stored illegal value".
//
// Running the normalizers on EVERY save instead was a real lockout. A
// competition can be stored as kind "team" with withZekkenName true: no
// server guard rejects that pairing (POST validates kind, team size, match
// type and qualifiers around it but never that pair, and the import path
// carries `with_zekken_name` and `kind` as independent keys), and only the
// create form's own `kind === "individual" ? withZekken : false` keeps the
// SPA from producing it. Open Settings on such a record, change nothing but
// the start time, and an unscoped normalization forces withZekkenName
// false. At status draw-ready `comp.WithZekkenName != current.WithZekkenName`
// is in the PUT's output-affecting set, so the save the operator DID ask for
// dies on a 409 about a change they did not -- on every attempt, with no
// control on the screen able to undo it, because the zekken checkbox is
// disabled at that status. Below draw-ready it is quieter and no better:
// participants.csv is thereafter read with the 3-column layout.
//
// Scoping to a staged flip returns those saves to exactly what they did
// before the normalizers were introduced, so it cannot regress a record
// this change did not already touch, while a flip -- which is only
// reachable at setup, since both pill groups carry the draw-ready lock --
// still gets the legal payload the normalizers exist to produce.
//
// pendingConfigClears below is defined in terms of this function rather
// than re-running the normalizers itself. That is what makes the notice and
// the payload agree BY CONSTRUCTION: the notice reports the diff between
// what the operator staged and what this function will send, so there is no
// arrangement in which a save clears a field the notice did not name.
export function configShapeChangeStaged(stored, staged) {
  return staged.format !== stored.format || staged.kind !== stored.kind;
}

// The two normalizers are gated INDEPENDENTLY, and that is the whole
// point of the scoping rather than a refinement of it.
//
// Running both whenever EITHER field moved re-creates the lockout the
// paragraph above describes, just through the other door: a stored
// `{kind: "team", withZekkenName: true}` (reachable -- no server guard
// rejects that pairing, and the import manifest carries `with_zekken_name`
// and `kind` as independent keys) meets an operator who changes nothing
// but the Format. The kind normalizer would run anyway and force
// withZekkenName and engi to false, on a kind that did not move, with the
// zekken checkbox disabled behind teamFieldsVisible so nothing on screen
// can put it back. participants.csv is thereafter read with the 3-column
// layout. The pendingConfigClears notice compounds it rather than saving
// the operator: it derives from this function, so it would faithfully
// announce "Saving will clear these settings, which do not apply to
// League: Use Zekken display name (on)" -- attributing a kind-driven clear
// to a format change with nothing to do with it.
//
// A normalizer answers for the field it is named after. Gate it on that
// field.
export function shapeConfigForSave(stored, staged) {
  let next = { ...staged };
  if (staged.format !== stored.format) next = normalizeConfigForFormat(next);
  if (staged.kind !== stored.kind) next = normalizeConfigForKind(next);
  return next;
}

// isMeaningfulValue: a staged value worth reporting as "about to be lost".
// 0 / "" / false / null / undefined / NaN are all how an unset or
// already-cleared field looks in this codebase (see safeInt/safeNonNegInt
// in admin_competition_settings.jsx for the NaN-as-"cleared" convention),
// so normalizing one of THOSE to the same resting value is not data loss.
// Those six cases (0, "", false, null, undefined, NaN) are exactly JS's
// falsy set, so this is Boolean(v) spelled out. Written as `!!v` rather
// than the enumeration: a reader had to check all six against the falsy
// table to conclude "truthy", and the trailing NaN branch read as though
// it were catching something the line above had missed.
function isMeaningfulValue(v) {
  return !!v;
}

export function pendingConfigClears(stored, staged) {
  const normalized = shapeConfigForSave(stored, staged);
  const clears = [];
  Object.keys(normalized).forEach((key) => {
    if (normalized[key] !== staged[key] && isMeaningfulValue(staged[key])) {
      clears.push({ key, from: staged[key] });
    }
  });
  return clears;
}

// CLEARED_FIELD_LABELS: wire key -> the operator-facing name of the field,
// for the notice that lists what a staged format/kind change is about to
// clear. It lives HERE, next to the two normalizers, rather than with the
// renderer: pendingConfigClears above deliberately walks
// Object.keys(normalized) "so a field a future normalizer starts touching
// is covered automatically", and a label map kept on the far side of that
// generality is how such a field reaches the operator as a raw wire key
// (the renderer falls back to `|| key`). Keeping the map beside the
// normalizers means the one edit that adds a cleared field has the label
// table already in front of it. Every value is one of this module's own
// LABEL_* exports, so each string still has exactly one home.
export const CLEARED_FIELD_LABELS = {
  poolSize: LABEL_POOL_SIZE,
  poolWinners: LABEL_POOL_WINNERS,
  extraQualifiers: LABEL_EXTRA_QUALIFIERS,
  teamSize: LABEL_TEAM_SIZE,
  teamMatchType: LABEL_TEAM_MATCH_TYPE,
  engi: LABEL_ENGI,
  withZekkenName: LABEL_ZEKKEN,
};

// --- COMPETITION_DEFAULTS -------------------------------------------------
//
// Every value a competition starts life with, in ONE object.
//
// They were in three places: the create form's ~20 `useStateA(...)` seeds,
// data.jsx's buildEmptyCompetition fallbacks (`poolMode || "max"`,
// `roundRobin ?? true`, `teamSize || (kind === "team" ? 5 : 0)`, ...), and
// Go's zero values for anything the client never sent. Three copies of one
// answer, and they had already drifted: the create form seeded poolSize to 3
// while buildEmptyCompetition also said 3 but poolWinners 2 against the
// form's 2 -- agreeing today by inspection rather than by construction.
//
// The rule this exists to serve: if two places have to hold the same value,
// they must READ it, not restate it. A test asserting two copies agree is
// still two copies; it just fails louder when they part.
//
// Keys are the wire/competition-record names (state.Competition's JSON tags),
// so a caller can spread this straight into a payload and a reader can look a
// field up by the name it has everywhere else. Values reference the module's
// own named constants wherever one exists, so the default and the thing it
// defaults TO cannot disagree either.
export const COMPETITION_DEFAULTS = {
  kind: KIND_INDIVIDUAL,
  format: FORMAT_KNOCKOUT,
  poolFormat: POOL_FORMAT_FULL,
  roundRobin: true,
  poolSizeMode: POOL_SIZE_MODE_MAX,
  poolSize: 3,
  poolWinners: 2,
  swissRounds: 4,
  teamSize: DEFAULT_TEAM_SIZE,
  teamMatchType: TEAM_MATCH_TYPE_FIXED,
  withZekkenName: false,
  naginata: false,
  engi: false,
  checkInEnabled: false,
  // Two joint 3rd places is the standard kendo convention; naginata is the
  // exception and turns it off (twoThirdPlacesForNaginata above). This
  // default being TRUE while Go's zero value is false is precisely why the
  // field must reach the wire explicitly on every format, rather than only
  // when the league controls are on screen. The Go tag keeps its
  // `omitempty` and always could: a bool with omitempty is value-lossless
  // (false marshals to an absent key, an absent key unmarshals back to
  // false), so the conditional send was the entire bug. See
  // state.Competition's own comment on this field.
  leagueTwoThirdPlaces: true,
  // 0, not LEAGUE_TIEBREAK_DEFAULT: the stored zero IS "not yet chosen" and
  // leagueTiebreakActive resolves it to Top 3 for display. Seeding the
  // resolved value instead would persist a choice the operator never made.
  leagueTiebreakTopN: 0,
  numberPrefix: "",
  // 0 means "unset, use the scheduler default" for both durations (T047).
  poolMatchDurationSeconds: 0,
  knockoutMatchDurationSeconds: 0,
  mirror: true,
  startTime: "09:00",
};

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
// type" pills (the only surface that renders a kind selector today --
// admin_competition_settings.jsx reads local.kind to gate other fields but
// never lets the operator change it; kindChangeBlockedReason below exists
// for a later phase to add that control there).
export const LABEL_KIND = "Competition type";
export const KIND_OPTIONS = [
  { value: KIND_INDIVIDUAL, label: "Individual" },
  { value: KIND_TEAM, label: "Team" },
];

// FORMAT_OPTIONS / LABEL_FORMAT / formatHint: verbatim from admin_setup.jsx's
// "Format" pills + the per-format hint line under them. Settings has no
// format selector at all -- local.format is read everywhere on that screen
// and staged nowhere (see that file's courtsErr comment) -- so these are
// exercised by the create form only today; they are still exported from
// here rather than left as literals because a later phase adding
// kindChangeBlockedReason-style format-change support to Settings needs the
// identical copy, not a second transcription of it.
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
//
// FINDING (reported, not fixed here -- this module only extracts what
// exists): the create form renders this control ONLY when format ===
// "league" (poolFormatVisible below mirrors that exactly), yet create()
// attaches `c.poolFormat` to the payload whenever format is "mixed" OR
// "league". A "mixed" (pools + knockout) competition therefore always
// submits poolFormat as "full" (the state's unreachable default) from
// EITHER screen: the create form never shows the pills for mixed, and
// admin_competition_settings.jsx never renders this control at all (it
// only READS local.poolFormat, at admin_competition_settings.jsx:840, to
// pick the courts-suggestion hint's wording). A mixed competition's pool
// shape can never be set to "partial" through either UI today.
export const LABEL_POOL_FORMAT = "Round-robin shape";
export const POOL_FORMAT_OPTIONS = [
  { value: POOL_FORMAT_FULL, label: "Full round-robin", hint: "Every participant plays every other participant in their pool." },
  { value: POOL_FORMAT_PARTIAL, label: "Partial / neighbour-only", hint: "Each participant plays a neighbourhood subset: useful when a full round-robin would not fit in the day's schedule." },
];

// poolFormatVisible: mirrors admin_setup.jsx's actual gate on the
// "Round-robin shape" pills (format === "league") -- see the FINDING above
// for why this is narrower than where the field is actually meaningful.
export function poolFormatVisible(format) {
  return format === FORMAT_LEAGUE;
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
// Settings-only today (admin_competition_settings.jsx:1066); the create
// form has no equivalent control. No visibility predicate is exported for
// it because the settings page renders it unconditionally (not gated on
// format), so there is nothing to derive.
export const LABEL_ROUND_ROBIN = "Round-robin in pools";

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
// teamFieldsVisible: both surfaces gate "Team size" and "Team match
// format" identically on kind === "team" (admin_setup.jsx:1221,1242;
// admin_competition_settings.jsx:753,774).
export function teamFieldsVisible(kind) {
  return kind === KIND_TEAM;
}

// zekkenApplies / engiApplies: the RULE (zekken display names and engi
// pairs are individual-only concepts), NOT a presentation instruction.
// Both surfaces agree on the rule and DELIBERATELY differ on how they show
// it, so the rule is what lives here and the presentation stays local:
//
//   create   (admin_setup.jsx:1255,1267) HIDES both controls for a team
//            competition. Correct there: kind is freely switchable on an
//            empty form, so a hidden control comes straight back.
//   settings (admin_competition_settings.jsx:1068,1076) RENDERS both and
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
// withZekkenName is deliberately NOT reset. Nothing on the server rejects
// it for a team competition, so clearing it would be silent loss of a
// setting rather than the repair of an unsubmittable one.
//
// The create form encodes the same teamSize rule inline at data.jsx:215
// (`teamSize || (kind === "team" ? 5 : 0)`) because it builds a payload from
// scratch rather than editing one; that call site is equivalent, not a
// second opinion.
export const DEFAULT_TEAM_SIZE = 5;

export function normalizeConfigForKind(cfg) {
  const next = { ...cfg };
  if (next.kind === KIND_TEAM) {
    if (!(Number.isFinite(next.teamSize) && next.teamSize >= 2)) {
      next.teamSize = DEFAULT_TEAM_SIZE;
    }
    if (!next.teamMatchType) next.teamMatchType = "fixed";
    next.engi = false;
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
// out from under it. Neither surface has a kind-change control wired to a
// roster today (create has no roster to conflict with; settings never
// exposes kind as editable at all), so this predicate exists ahead of that
// control for a later phase, per the task's required export surface.
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

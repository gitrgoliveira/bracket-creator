// Pure helpers shared across the admin layer.
// No JSX, no React deps. See web-mobile/admin_split_plan.md.

// sideA/sideB can be a string (raw backend shape), an object with .name
// (normalizeMatch output, which substitutes {id:"",name:""} for missing sides),
// or null. Return the participant's display name, or "" when no real side is
// present. Used by compMatchStats and AdminTopbar's running-strip filter, so the
// two stay in lockstep about what "has a real side" means.
function sideName(side) {
  if (!side) return "";
  if (typeof side === "string") return side;
  return side.name || "";
}

// True when a match has both sides resolved to a real participant (not a
// bye, not a TBD bracket placeholder, not an unresolved "Winner of rX-mY"
// reference). The naïve `m.sideA && m.sideB` test is almost always wrong
// post-normalizeMatch: that function substitutes {id:"",name:""} for
// missing sides, which is truthy. Use this helper in filter predicates
// / rendering guards instead.
//
// Bracket-side caveat: future-round matches carry placeholder side
// names like `"Winner of r0-m1"` until the source match resolves. Those
// are non-empty strings: sideName() returns them as-is: so the
// underlying `sideName(...)` check ALONE isn't enough. We reject the
// EXACT placeholder shape `Winner of r<n>-m<n>` (the literal format
// emitted by internal/engine/bracket.go at lines 65 and 73), NOT every
// name that happens to start with "Winner of ": a legitimate
// participant named "Winner of the 2025 Cup" should still pass.
// (See web-mobile/js/viewer.jsx for the consumer.)
//
// Mixed-comp pool-origin caveat: before a pool finishes seeding, knockout
// bracket leaves carry pool-origin placeholder labels like "Pool A-1st",
// "Pool B-2nd" (format produced by helper.BuildKnockoutDraw / engine/knockout.go).
// These look like real strings to sideName() but are not real participants.
// Mirrors the Go regex poolFinalistPlaceholderRE = `^Pool .+-\d+(st|nd|rd|th)$`.
const BRACKET_PLACEHOLDER_RE = /^Winner of r\d+-m\d+$/;
const POOL_ORIGIN_PLACEHOLDER_RE = /^Pool .+-\d+(st|nd|rd|th)$/;
function hasBothSides(m) {
  if (!m) return false;
  const a = sideName(m.sideA);
  const b = sideName(m.sideB);
  if (!a || !b) return false;
  if (BRACKET_PLACEHOLDER_RE.test(a) || BRACKET_PLACEHOLDER_RE.test(b)) return false;
  if (POOL_ORIGIN_PLACEHOLDER_RE.test(a) || POOL_ORIGIN_PLACEHOLDER_RE.test(b)) return false;
  return true;
}

// hasPoolOriginPlaceholder reports whether a bracket match still has a pool-origin
// "Pool A-1st" side (a mixed comp whose feeder pool hasn't finished). Unlike
// !hasBothSides, this is TRUE only for pool placeholders: NOT for normal
// "Winner of rX-mY" feeders or structural byes: so the "Knockout filling in"
// banner shows ONLY for an incomplete mixed knockout, not standalone playoffs or
// bye-containing brackets.
function hasPoolOriginPlaceholder(m) {
  if (!m) return false;
  const a = sideName(m.sideA);
  const b = sideName(m.sideB);
  return POOL_ORIGIN_PLACEHOLDER_RE.test(a) || POOL_ORIGIN_PLACEHOLDER_RE.test(b);
}

// isPendingBracketMatch reports whether a match is a scheduled knockout bout
// still waiting on a "Winner of rX-mY" feeder (mp-y3nk). Such matches are
// excluded from the actionable shiaijo queue by hasBothSides (you can't call
// "Winner of r2-m0" to the court), which leaves a court whose only remaining
// bout is a downstream final showing a falsely-empty Upcoming list. The shiaijo
// queue uses this to surface them as clearly non-actionable "later" rows.
//
// Deliberately narrow, and intentionally NOT the inverse of hasBothSides:
//   - only status "scheduled" (running/completed are handled by their own paths);
//   - at least one side is a bracket feeder placeholder ("Winner of rX-mY");
//   - NEVER pool-origin ("Pool A-1st") placeholders: those belong to the
//     mixed-comp knockout-seeding flow and are surfaced by the separate
//     "Knockout filling in" banner, not the queue.
function isPendingBracketMatch(m) {
  if (!m || m.status !== "scheduled") return false;
  if (hasBothSides(m)) return false; // resolved → normal actionable row
  if (hasPoolOriginPlaceholder(m)) return false; // mixed-comp seeding path, not ours
  const a = sideName(m.sideA);
  const b = sideName(m.sideB);
  return BRACKET_PLACEHOLDER_RE.test(a) || BRACKET_PLACEHOLDER_RE.test(b);
}

// Returns { total, done, running } match counts for a single competition object.
// Accepts either:
//   - flat `poolMatches` array from GET /api/viewer/competitions (list endpoint)
//   - structured `pools[].matches` from GET /api/viewer/competitions/:id (detail endpoint)
// The admin-side GET /api/competitions/:id returns only config; use the viewer
// endpoints when match counts are needed.
function compMatchStats(c) {
  let total = 0, done = 0, running = 0;
  // Use hasBothSides(): the canonical cross-file predicate: so admin
  // dashboard / overview / running-strip stats can't drift from viewer-side
  // filtering. Inline `sideName(m.sideA) && sideName(m.sideB)` was almost
  // right (skips byes / normalizeMatch's empty-side substitute) but missed
  // bracket placeholders like "Winner of r0-m1": those have truthy
  // sideName() values, so future-round matches were counted as real before
  // their source resolves. hasBothSides also rejects that exact shape.
  const count = (m) => {
    if (!hasBothSides(m)) return;
    total++;
    if (m.status === "completed") done++;
    if (m.status === "running") running++;
  };
  if (Array.isArray(c.poolMatches)) {
    c.poolMatches.forEach(count);
  } else if (c.pools) {
    c.pools.forEach((p) => (p.matches || []).forEach(count));
  }
  if (c.bracket && c.bracket.rounds) {
    c.bracket.rounds.forEach((r) => (r || []).forEach(count));
  }
  return { total, done, running };
}

// True when a bracket exists and every real match in it -- including
// bracket.thirdPlaceMatch, the naginata bronze match, which is a SIBLING
// field of bracket.rounds, not a row inside it -- is completed. Deliberately
// does NOT reuse compMatchStats's total/done counters: those never walk
// thirdPlaceMatch, so a completed bronze match would be silently ignored and
// a naginata bracket could read as "fully done" one match early.
//
// Gates the "Complete competition" action (admin_competition.jsx): League
// and pure-pools formats auto-complete server-side once every pool match is
// in (MaybeAutoCompletePools) and never produce a `bracket`, so this
// predicate naturally returns false for them without a format check. Mixed
// competitions only reach here once their knockout bracket is seeded (before
// that, `bracket` is absent/empty).
function bracketFullyComplete(bracket) {
  if (!bracket || !bracket.rounds || !bracket.rounds.length) return false;
  const matches = [];
  bracket.rounds.forEach((r) => (r || []).forEach((m) => { if (m) matches.push(m); }));
  if (bracket.thirdPlaceMatch) matches.push(bracket.thirdPlaceMatch);
  // Use isRequiredBracketMatch, NOT hasBothSides, for the completion gate.
  // hasBothSides excludes "Winner of rX-mY" / "Pool A-1st" PLACEHOLDER sides
  // (correct for progress stats), but an SSE match_updated only carries the one
  // scored match, so a downstream final/bronze can transiently still show a
  // placeholder side until the next refetch. Filtering those out would let this
  // return true and enable "Complete competition" before the final/bronze is
  // actually played. A placeholder-sided match is required-and-incomplete;
  // byes (one empty side) and hidden phantoms are still excluded (Copilot #326).
  const required = matches.filter(isRequiredBracketMatch);
  if (!required.length) return false;
  return required.every((m) => m.status === "completed");
}

// A bracket match that must eventually be played before the competition is
// complete: both sides are NAMED -- including a "Winner of rX-mY" or "Pool
// A-1st" placeholder that propagation/refetch hasn't resolved yet -- and it is
// not a structural bye (one empty side) or a hidden empty-vs-empty phantom
// (state.BracketMatch.Hidden). Unlike hasBothSides this KEEPS placeholder-sided
// matches, so the completion gate can't fire while a downstream match is still
// waiting on its feeder.
function isRequiredBracketMatch(m) {
  if (!m || m.hidden) return false;
  return !!sideName(m.sideA) && !!sideName(m.sideB);
}

// Canonical numeric bounds. The year range is shared by every date
// validator (admin_helpers.jsx validateAndNormalizeDate, admin_competition.jsx
// saveNow inline). MAX_TEAM_SIZE is the canonical team-size cap; the
// scoring modal's TEAM_POSITIONS array is built from it (see
// admin_scoring_modal.jsx), and the team-size inputs in admin_competition
// + admin_setup use it as their HTML `max` attribute. Bumping any of these
// here flows to every consumer mechanically.
//
// MIN_YEAR / MAX_YEAR mirror helper.MinDateYear / helper.MaxDateYear
// (internal/helper/constants.go): the API's validateDateDMY rejects
// out-of-range years to keep the wire contract symmetric with the UI.
// MAX_COURTS mirrors helper.MaxCourts (same Go file): anchored to the
// A–Z labelling cap. MAX_RANK mirrors helper.MaxRankOverride: overflow
// guard for the override-rank handler; the real semantic constraint is
// pool size, enforced server-side.
//
// Pin tests on BOTH sides assert the literal values (this file's vitest
// suite + internal/helper/constants_test.go) so cross-language drift
// fails CI rather than waiting for a downstream UX bug.
const MIN_YEAR = 1900;
const MAX_YEAR = 2100;
const MAX_TEAM_SIZE = 9;
const MAX_COURTS = 26;
const MAX_RANK = 1000;

// Canonical date error messages. Referenced by validateAndNormalizeDate
// AND by AdminSettings.saveNow's inline asymmetric validation, so the
// user-facing UX stays consistent across all four date-validation sites
// regardless of where the error is generated. Exported on window + ES.
// The year-range message is a template so changing MIN_YEAR/MAX_YEAR
// above auto-updates the user-facing text.
const DATE_ERR_INVALID_FORMAT = "Invalid date. Please pick a valid day.";
const DATE_ERR_YEAR_RANGE = `Year must be between ${MIN_YEAR} and ${MAX_YEAR}.`;

// Combined date validation + normalization. Returns:
//   - { norm: "DD-MM-YYYY", error: null }  on success
//   - { norm: null, error: "<message>" }   on failure
//
// Canonical predicate for date inputs across the admin UI. Save paths
// (AdminEditTournament.handleSave, AdminCreateCompetition.create,
// AdminSettings.saveNow) use the `error` for user-facing messaging AND
// `norm` for the value to save. Pure boolean callers use `isValidDate`
// below.
function validateAndNormalizeDate(date) {
  const norm = normalizeDate(date);
  if (!norm || !/^\d{2}-\d{2}-\d{4}$/.test(norm)) {
    return { norm: null, error: DATE_ERR_INVALID_FORMAT };
  }
  const year = parseInt(norm.substring(6, 10));
  if (year < MIN_YEAR || year > MAX_YEAR) {
    return { norm: null, error: DATE_ERR_YEAR_RANGE };
  }
  return { norm, error: null };
}

// Boolean predicate: is `date` a valid DD-MM-YYYY day in the supported
// year range (1900–2100)? Used by AdminCompetition's "Start competition"
// button gate: anywhere a boolean result is enough. For save flows that
// need both the boolean AND the normalized value, use
// validateAndNormalizeDate above.
function isValidDate(date) {
  return validateAndNormalizeDate(date).error === null;
}

// Pure decision logic for "user edited a <input type='number'> bound to a
// debounce-saved field" (e.g. AdminSettings.teamSize/poolSize/poolWinners).
//
// The naïve `onChange={e => update(k, +e.target.value)}` has two failure
// modes from one JS coercion:
//   - `+""` → 0   (cleared input collapses to a displayed "0" instead of
//                  staying empty; backend then receives 0 and likely rejects)
//   - `+"abc"` → NaN  (React warns "Received NaN for the value attribute")
//
// Returns:
//   { value, shouldSave }
//
// - `value` is what to store in local state. For empty input we return NaN
//   so the render side can do `value={Number.isFinite(v) ? v: ""}` and
//   keep the cleared display empty (same shape as the duration fields in
//   admin_competition_settings.jsx).
// - `shouldSave` is true only when the parsed value is a positive integer
//   ≥ min. Callers MUST still issue a saveLater on false: the debounceRef
//   is single-slot and covers all fields, so an earlier scheduled save
//   captured the OLD valid value for THIS field and will commit it over
//   the wire if not replaced. Use saveLater(next-with-NaN) so the
//   commit-side safeInt fallback resolves the field to the on-disk
//   c.<field>, while cross-field edits in `next` (e.g. Name typed
//   concurrently) still propagate. `shouldSave` is therefore informational
//   only: callers no longer branch on it.
//
// Exported for vitest at __tests__/admin_helpers.test.jsx.
function decideNumericUpdate(raw, min = 1) {
  if (raw === "" || raw == null) return { value: NaN, shouldSave: false };
  const parsed = +raw;
  if (!Number.isFinite(parsed) || !Number.isInteger(parsed) || parsed < min) {
    return { value: parsed, shouldSave: false };
  }
  return { value: parsed, shouldSave: true };
}

// Normalize a date string to the canonical DD-MM-YYYY format. Accepts
// DD-MM-YYYY (no-op normalization) and ISO YYYY-MM-DD (converted to DMY,
// for paths still handing over the HTML `<input type="date">` raw value).
// Returns null for malformed shape or semantically invalid days (Feb 31 etc.).
function normalizeDate(d) {
  if (!d) return d;
  let day, m, y;
  if (/^\d{2}-\d{2}-\d{4}$/.test(d)) {
    [day, m, y] = d.split('-').map(Number);
  } else if (/^\d{4}-\d{2}-\d{2}$/.test(d)) {
    [y, m, day] = d.split('-').map(Number);
  } else {
    // Match the older permissive parser shape (D-M-YYYY, D/M/YYYY) for
    // user-pasted text via admin import. Canonical output is still
    // zero-padded DD-MM-YYYY.
    const match = d.match(/^(\d{1,2})[-/](\d{1,2})[-/](\d{4})$/);
    if (!match) return null;
    day = Number(match[1]);
    m = Number(match[2]);
    y = Number(match[3]);
  }
  // Reject semantically invalid dates like "32-13-2026" or "31-02-2026".
  // JS's Date constructor silently rolls invalid components over (Feb 31 →
  // Mar 3), so round-trip the parts through UTC and require an exact match.
  const dt = new Date(Date.UTC(y, m - 1, day));
  if (
    isNaN(dt.getTime()) ||
    dt.getUTCFullYear() !== y ||
    dt.getUTCMonth() + 1 !== m ||
    dt.getUTCDate() !== day
  ) {
    return null;
  }
  return `${String(day).padStart(2, '0')}-${String(m).padStart(2, '0')}-${y}`;
}

// HTML <input type="date"> uses ISO YYYY-MM-DD for value/min/max attributes.
// These converters bridge the input boundary; everywhere else uses DMY.
//
// dmyToIso accepts an ISO YYYY-MM-DD pass-through as a transition convenience:
// `normalizeDate` and `formatDate` also accept ISO as input, and any record
// saved by a pre-canonicalization build still has an ISO date in state until
// the next save round-trips it. Without the pass-through, an ISO value would
// produce an empty <input type="date"> value, blanking the picker in the UI.
function dmyToIso(dmy) {
  if (!dmy) return "";
  if (/^\d{4}-\d{2}-\d{2}$/.test(dmy)) return dmy;
  if (!/^\d{2}-\d{2}-\d{4}$/.test(dmy)) return "";
  const [dd, mm, yyyy] = dmy.split('-');
  return `${yyyy}-${mm}-${dd}`;
}
// isoToDmy accepts a DMY DD-MM-YYYY pass-through symmetrically: most callers
// feed it the raw `e.target.value` from <input type="date">, which is ISO,
// but defense-in-depth costs nothing here.
function isoToDmy(iso) {
  if (!iso) return "";
  if (/^\d{2}-\d{2}-\d{4}$/.test(iso)) return iso;
  if (!/^\d{4}-\d{2}-\d{2}$/.test(iso)) return "";
  const [yyyy, mm, dd] = iso.split('-');
  return `${dd}-${mm}-${yyyy}`;
}

// Chronological comparator for DD-MM-YYYY date strings. JS's default
// `Array.sort()` does lexical compare, which works for ISO YYYY-MM-DD
// (lex == chronological) but produces wrong order for DMY: "01-06-2026"
// (June 1) sorts before "12-05-2026" (May 12) lexically. This helper
// converts each value to an ISO sort key so lex compare matches
// chronological order. Non-DMY inputs (e.g. "") fall back to string
// compare so a mix of valid + empty dates still sorts deterministically.
function compareDmy(a, b) {
  const toKey = (d) => {
    if (!d) return "";
    const m = /^(\d{2})-(\d{2})-(\d{4})$/.exec(d);
    return m ? `${m[3]}-${m[2]}-${m[1]}` : d;
  };
  return toKey(a).localeCompare(toKey(b));
}

function getScoreBtnClass(status) {
  return `score-btn ${status === "completed" ? "score-btn--correct" : "score-btn--active"}`;
}

// MAX_TOURNAMENT_DURATION_DAYS mirrors MaxTournamentDurationDays in
// internal/mobileapp/validation.go. Keep both in sync.
const MAX_TOURNAMENT_DURATION_DAYS = 30;

// deriveTournamentDays returns an ordered array of DD-MM-YYYY strings
// covering the tournament, mirroring Tournament.Days() on the Go side.
//
//   deriveTournamentDays("05-06-2026", 3) → ["05-06-2026", "06-06-2026", "07-06-2026"]
//
// Returns [] (empty) when:
//   - startDate is empty / unparseable
//   - durationDays < 1
//
// Exported for JSX components and vitest.
function deriveTournamentDays(startDate, durationDays) {
  if (!startDate || !Number.isInteger(durationDays) || durationDays < 1) return [];
  const norm = normalizeDate(startDate);
  if (!norm) return [];
  // Parse from DD-MM-YYYY
  const [dd, mm, yyyy] = norm.split('-').map(Number);
  const base = new Date(Date.UTC(yyyy, mm - 1, dd));
  if (isNaN(base.getTime())) return [];
  const days = [];
  for (let i = 0; i < durationDays; i++) {
    const d = new Date(base);
    d.setUTCDate(base.getUTCDate() + i);
    const day = String(d.getUTCDate()).padStart(2, '0');
    const month = String(d.getUTCMonth() + 1).padStart(2, '0');
    const year = d.getUTCFullYear();
    days.push(`${day}-${month}-${year}`);
  }
  return days;
}

// Normalizes a courts array. Fallback to ["A"] if missing or empty,
// preventing crashes and ensuring a consistent default court selection UI.
function normalizeCourts(courts) {
  return (Array.isArray(courts) && courts.length > 0) ? courts : ["A"];
}

// Returns the count of courts, safely falling back to the normalized minimum of 1.
// Used for displaying counts in the UI where "0 courts" is semantically invalid.
function courtCount(courts) {
  return normalizeCourts(courts).length;
}

// --- Shiaijo-count rule (spec 007 R9) --------------------------------------
//
// A competition's shiaijo allocation must be a POWER OF TWO. The valid
// counts are derived from MAX_COURTS rather than written out, so the A–Z
// label cap and this list can never disagree: 32 shiaijo are unlabelled and
// therefore unreachable, which is why 16 is the practical ceiling.
const VALID_SHIAIJO_COUNTS = (() => {
  const out = [];
  for (let p = 1; p <= MAX_COURTS; p *= 2) out.push(p);
  return out;
})();

// The canonical reason, authored ONCE and reused by every surface that
// states the rule (the rejection message, the standing field hint, the
// stored-allocation banner). Kept as a bare clause so it can be embedded
// after a colon; REASON_SENTENCE is the standalone-sentence form.
const SHIAIJO_RULE_REASON = "the knockout draw gives each shiaijo its own block of the bracket and the blocks merge in pairs, so the count has to halve cleanly";
const SHIAIJO_RULE_REASON_SENTENCE = `${SHIAIJO_RULE_REASON[0].toUpperCase()}${SHIAIJO_RULE_REASON.slice(1)}.`;

// The organiser's real question, answered ONCE. Every message about this rule
// leads with a verdict about a number, which an organiser whose venue has
// exactly 3 shiaijo reads as a verdict about their venue. It is not: the venue
// may hold any number, and only a single competition's slice of it is
// constrained. Kept verbatim from the wording the operator guide settled on.
const SHIAIJO_RULE_IS_PER_COMPETITION = "This is a rule about each competition, never about your venue.";

// One list joiner behind every enumeration this module renders: Oxford-comma-
// free, ", " between all but the last, the conjunction before it. The separator
// and the singleton handling are console microcopy, so they are decided once
// here rather than restated per message.
function joinList(list, conjunction, empty) {
  if (!list.length) return empty;
  if (list.length === 1) return String(list[0] ?? "");
  return `${list.slice(0, -1).join(", ")} ${conjunction} ${list[list.length - 1]}`;
}

// "1, 2 or 4" from [1, 2, 4].
function joinCounts(list) {
  return joinList(list, "or", "");
}

// The counts a competition may take on a venue of `venueCourtCount` shiaijo,
// plus whether the venue actually narrows the full list. ONE primitive behind
// the standing field hint, the venue-aware rejection and the import preview,
// so no surface can offer a count the venue cannot supply while another does.
//
// venue 0 / NaN / undefined means "not loaded yet", NOT "the venue has none":
// it falls back to the full list rather than inventing a constraint.
function allowedShiaijoCounts(venueCourtCount) {
  const venue = Number.isFinite(venueCourtCount) && venueCourtCount > 0 ? Math.floor(venueCourtCount) : 0;
  // `1` always survives the filter for any venue >= 1, so nothing derived
  // from this can ever read as "at least 2 shiaijo".
  const allowed = venue ? VALID_SHIAIJO_COUNTS.filter((p) => p <= venue) : VALID_SHIAIJO_COUNTS;
  return { venue, allowed, constrained: venue > 0 && allowed.length < VALID_SHIAIJO_COUNTS.length };
}

// Is this a count a single competition may run its bracket on? The yes/no
// half of the rule, owned here so no call site derives it a second way.
//
// shiaijoCountError is predicate-and-label in one call, and is how the screens
// that REFUSE something ask. Use isLegalShiaijoCount only where the yes/no is
// itself the output: a hint deciding whether to reassure, a split example
// deciding whether there is a puzzle to answer. Building a two-sentence
// operator message and discarding it to read its truthiness is how a second,
// drifting copy of the rule gets in.
function isLegalShiaijoCount(n) {
  return VALID_SHIAIJO_COUNTS.includes(n);
}

// The concrete answer to "so one of my three shiaijo just sits idle?" - no:
// two competitions run side by side and cover the venue exactly. Returns the
// sentence, or null when no exact two-way split exists.
//
// Only an EXACT split is offered. On a 7-shiaijo venue the best pair is 4 + 2
// and one shiaijo really is left over, so claiming otherwise would be false
// advice; the caller keeps the "rule about each competition" clause and drops
// the example. A venue that is itself a legal count (1, 2, 4, 8, 16) has no
// puzzle to answer and also returns null.
function shiaijoVenueSplitExample(venueCourtCount) {
  const { venue } = allowedShiaijoCounts(venueCourtCount);
  if (!venue || isLegalShiaijoCount(venue)) return null;
  for (let i = VALID_SHIAIJO_COUNTS.length - 1; i >= 0; i--) {
    const first = VALID_SHIAIJO_COUNTS[i];
    const rest = venue - first;
    if (first < venue && VALID_SHIAIJO_COUNTS.includes(rest)) {
      return `With ${venue} shiaijo you can run one competition on ${first} and another on the remaining ${rest} at the same time, so all ${venue} stay busy.`;
    }
  }
  return null;
}

// Shiaijo-count rule for ONE competition, mirrored from
// helper.ValidateShiaijoCount (internal/helper/shiaijo_count.go): a
// competition whose draw builds a knockout bracket runs on 1, 2, 4, 8 or 16
// shiaijo. Anything else (3, 5, 6, 10, ...) is invalid, because the draw
// gives each shiaijo its own block of the bracket and merges those blocks in
// PAIRS: the count therefore has to halve cleanly all the way down, which
// only a power of two does. 6 halves to 3 and stops.
//
// 1 shiaijo is explicitly VALID (its single block splits into two halves
// that merge like any other pair), so the message always offers 1 and must
// never read as "at least 2 shiaijo".
//
// The rule is per COMPETITION, never per venue: a 3-shiaijo tournament is
// perfectly legal and simply runs each competition on 1 or 2 of its shiaijo.
// Nothing here validates the tournament's own court list, and a venue must
// never be pushed to a power of two to satisfy this.
//
// Returns null when the count is valid, or the operator-facing message when
// it is not, so call sites can use it as both predicate and label. The
// message names the nearest valid counts either side (capped at 16, since
// the next power of two exceeds the A–Z label cap) and always offers 1. The
// Go side and this string are pinned against each other by
// web-mobile/js/__tests__/shiaijo_count.test.jsx and
// internal/helper/shiaijo_count_test.go.
//
// `venueCourtCount` is OPTIONAL and changes only which counts are OFFERED.
// Omit it (the two forms) and the message is the venue-agnostic mirror of the
// Go one: those screens render shiaijoCountHint directly beneath, which
// supplies the venue view, and stating it twice buries the part that changes.
// Pass it wherever the message appears ALONE - the dashboard card, the
// "Start all" picker, the competition header, the overview checklist - or a
// 3-shiaijo venue is told to "use 2 or 4" when it has no 4 to give. Those
// surfaces all reach it through competitionDrawBlockedReason, which already
// receives the tournament's courts for the orphan check.
function shiaijoCountError(n, venueCourtCount) {
  if (!Number.isFinite(n) || n <= 1) return null;
  if (isLegalShiaijoCount(n)) return null;
  const { venue, allowed, constrained } = allowedShiaijoCounts(venueCourtCount);
  let remedy;
  if (constrained) {
    // Naming the venue's own size first is deliberate: it says the app is not
    // asking the operator to change their hall, only this competition's slice
    // of it.
    remedy = `This tournament has ${venue}, so this competition can use ${joinCounts(allowed)}`;
  } else {
    const below = VALID_SHIAIJO_COUNTS.filter((p) => p < n).pop();
    const above = VALID_SHIAIJO_COUNTS.find((p) => p > n);
    // `above` is undefined past the ceiling (17+ shiaijo): there is no higher
    // valid count to offer, so the message names only the one below. `below`
    // is always at least 2 here, because n > 1 and every n <= 2 is valid.
    remedy = above ? `Use ${below} or ${above}, or 1` : `Use ${below}, or 1`;
  }
  // One sentence frame, two remedies. Only the middle clause differs between
  // the venue-aware and venue-agnostic forms, and the opening and the reason
  // are pinned against the Go message, so they are written once.
  return `${n} shiaijo cannot be paired down to a single bracket. ${remedy}: ${SHIAIJO_RULE_REASON}.`;
}

const SHIAIJO_NONE_SELECTED = "At least one shiaijo (court) must be selected.";

// THE answer for a shiaijo picker: what is wrong with the selection currently on
// screen, or null. Both pickers (the create form and competition Settings) call
// only this.
//
// Two rules, in the order an operator can act on them: name a shiaijo at all,
// then name a legal number of them. The ORDER is itself the rule, so it lives
// here rather than at each picker -- the same reason helper.ValidateDrawCourtCount
// owns the equivalent pair on the Go side. Otherwise a third picker calls the
// count half alone and silently drops the other.
//
// `authored` says this list is the operator's own current selection rather than
// a value that arrived off disk, and it is what an EMPTY list means:
//
//   off disk   "inherit the tournament's shiaijo" -- a legal record the server
//              materialises, which is why shiaijoCountError answers null for 0.
//              Demanding "select at least one" of a competition that merely
//              ARRIVED that way contradicts both the silent banner beside it
//              (inheriting a legal count is fine) and its live Save button.
//   authored   the operator turned every pill off: an unfinished form. Left
//              unsaid, the create form silently substituted the venue's first
//              court, so deselecting everything on a 4-shiaijo venue produced a
//              1-shiaijo competition with nothing on screen saying so.
//
// The emptiness half is NOT scoped by format the way shiaijoCountErrorFor is: a
// league has to run somewhere too, even though its shiaijo count is free.
//
// Venue-agnostic on purpose: both pickers render this directly above the
// venue-aware standing hint, so passing the venue would state that clause twice.
// resolvedShiaijoCountError is the venue-aware twin, for surfaces rendering a
// verdict alone.
function shiaijoPickerError(format, courts, authored) {
  const list = Array.isArray(courts) ? courts : [];
  if (!list.length) return authored ? SHIAIJO_NONE_SELECTED : null;
  return shiaijoCountErrorFor(format, list.length);
}

// shiaijoCountError with the FORMAT scope applied: null for a league or Swiss
// competition, whose shiaijo run in parallel with no bracket blocks to merge.
//
// The scope is half the rule, so it belongs beside the other half rather than
// at each screen that asks. Every staged-allocation surface (create, settings,
// import preview) used to spell `formatDrawsBracket(f) ? shiaijoCountError(n)
// : null` for itself; the next surface to forget the gate would reject a
// league on 3 shiaijo, which is the count the app's own hint recommends
// (floor(players/2)-1). Mirrors engine.ValidateCompetitionShiaijoCount.
function shiaijoCountErrorFor(format, n, venueCourtCount) {
  if (!formatDrawsBracket(format)) return null;
  return shiaijoCountError(n, venueCourtCount);
}

// The STANDING hint for the shiaijo field: what the operator may pick, and
// why, shown whether or not the current selection is valid. The rule used to
// surface only as a rejection AFTER a bad pick, which reads as the app
// changing its mind; this teaches it in place.
//
// VENUE-AWARE on purpose. A 3-shiaijo tournament is legal and its
// competitions run on 1 or 2, so the hint names 1 or 2 and says the venue has
// 3. That answers "why can't I pick all three of my shiaijo" at the field
// instead of by refusal. A venue big enough for every valid count (16+) gets
// no venue clause, because there is nothing to explain.
//
// includeReason=false drops the mechanism sentence for the case where
// shiaijoCountError is already on screen directly above and would state it a
// second time.
//
// Also carries the REASSURANCE, not just the verdict. A hint that only names
// the counts still reads, to an organiser with exactly 3 shiaijo, as the app
// disapproving of their hall. It does not: the venue is fine, and the two
// clauses added here say so and show the split that keeps every shiaijo busy.
// The mechanism sentence is what the operator needs LAST, so it stays last.
// The reassurance survives includeReason=false, because the error above
// states the mechanism and never the reassurance.
function shiaijoCountHint(venueCourtCount, includeReason = true) {
  const { venue, allowed, constrained } = allowedShiaijoCounts(venueCourtCount);
  const head = `This competition can use ${joinCounts(allowed)} shiaijo${constrained ? ` (this tournament has ${venue})` : ""}.`;
  const parts = [head];
  // Only an organiser whose venue count is not itself a legal allocation is
  // reading the rule as being about their venue. A 4-shiaijo hall never meets
  // a refusal and needs no reassurance.
  if (venue && !isLegalShiaijoCount(venue)) {
    parts.push(SHIAIJO_RULE_IS_PER_COMPETITION);
    const split = shiaijoVenueSplitExample(venue);
    if (split) parts.push(split);
  }
  if (includeReason) parts.push(SHIAIJO_RULE_REASON_SENTENCE);
  return parts.join(" ");
}

// shiaijoCountHint with the same FORMAT scope shiaijoCountErrorFor applies, so
// a league's court field is not taught a rule that does not bind it. The two
// travel together on every screen that renders them, so they are gated the
// same way in the same place.
function shiaijoCountHintFor(format, venueCourtCount, includeReason = true) {
  if (!formatDrawsBracket(format)) return null;
  return shiaijoCountHint(venueCourtCount, includeReason);
}

// The hint for the TOURNAMENT-level "Number of Shiaijo (courts)" field: the
// first place an organiser types a shiaijo count, and until now the only one
// that said nothing about the rule. They typed 3, met the refusal two screens
// later, and read it as a verdict on their hall.
//
// So this states the rule's SCOPE before the rule can ever refuse anything,
// and shows the split that keeps the odd shiaijo busy. `venueCourtCount` is
// whatever is currently in the field, so the example is about their number,
// not a worked one. A blank/NaN field falls back to the full list.
function shiaijoVenueHint(venueCourtCount) {
  const { allowed } = allowedShiaijoCounts(venueCourtCount);
  const parts = [
    "Pick the number your venue actually has: any number is fine.",
    `Each competition then runs on ${joinCounts(allowed)} of them.`,
    SHIAIJO_RULE_IS_PER_COMPETITION,
  ];
  const split = shiaijoVenueSplitExample(venueCourtCount);
  if (split) parts.push(split);
  return parts.join(" ");
}

// Scope of the shiaijo-count rule above: true when the competition's draw
// builds a knockout bracket. Mirrors engine.CompetitionDrawsBracket
// (internal/engine/court_validation.go), which in turn mirrors the format
// switch in the engine's draw pipeline: league and Swiss produce pools or
// rounds and never a bracket, while mixed, playoffs and a legacy record
// with no format at all all end up building one.
//
// League and Swiss courts run in parallel with no bracket blocks to merge,
// so shiaijoCountError must not be applied to them. The league court hint
// recommends floor(players/2)-1 courts, which is rarely a power of two, so a
// format-blind rule would reject counts the app itself suggests.
function formatDrawsBracket(format) {
  return format !== "league" && format !== "swiss";
}

// Returns the competition's assigned shiaijo that the TOURNAMENT does not
// have, in the competition's own order. Mirrors
// engine.CourtsOutsideTournament (internal/engine/court_validation.go).
//
// Reducing the tournament's court count leaves every competition's own list
// alone, so a competition allocated A–D keeps D after the venue shrinks to
// A–C. D is then a shiaijo with no operator view, and the draw and schedule
// would still use it. The server refuses the reduction while a live
// competition depends on the court and refuses to draw onto one; this helper
// is how the settings screen SHOWS the leftover rather than quietly hiding it.
//
// An empty tournament list returns nothing: it means "not loaded yet", not
// "the venue has no courts". Duplicates are reported once.
//
// Argument order is the Go function's, competition first: it is the only helper
// here that claims to be a mirror, so its call shape has to be copyable in both
// directions. Its neighbours (courtPillOptions, orphanedShiaijoError) take the
// venue first because they render the venue's pills and have no Go counterpart
// to agree with -- that difference is deliberate, not drift.
function courtsOutsideTournament(compCourts, tournamentCourts) {
  const sel = Array.isArray(compCourts) ? compCourts : [];
  const tourn = Array.isArray(tournamentCourts) ? tournamentCourts : [];
  if (!tourn.length || !sel.length) return [];
  const seen = new Set();
  return sel.filter((cc) => {
    if (tourn.includes(cc) || seen.has(cc)) return false;
    seen.add(cc);
    return true;
  });
}

// The shiaijo pills a competition-settings screen must render, so that what
// is SHOWN is exactly what would be SAVED.
//
// Rendering `tournament.courts` alone (the old behaviour) silently dropped
// any court the competition still holds but the tournament no longer has: a
// competition storing [A B C D] under a 3-shiaijo tournament drew three
// selected pills, so the operator saw an odd-looking 3-court selection while
// 4 were on disk, no rule fired on the shown count, and saving an unrelated
// field kept all four. Emitting the leftovers as extra, flagged pills keeps
// `pills.filter(selected)` equal to `local.courts` at all times, and gives the
// operator the one action that fixes it: deselect and save.
//
// Same shape as the "(outside tournament days)" option the date <select> on
// that screen renders for an out-of-range date, and for the same reason.
//
// Returns [{ court, selected, inTournament }] with the tournament's courts
// first, in tournament order, then the leftovers in the competition's order.
function courtPillOptions(tournamentCourts, selectedCourts) {
  const tourn = Array.isArray(tournamentCourts) ? tournamentCourts : [];
  const sel = Array.isArray(selectedCourts) ? selectedCourts : [];
  const seen = new Set();
  const out = [];
  for (const cc of tourn) {
    if (seen.has(cc)) continue;
    seen.add(cc);
    out.push({ court: cc, selected: sel.includes(cc), inTournament: true });
  }
  // courtsOutsideTournament has already dropped everything `tourn` holds and
  // deduped what it returns, so nothing here can collide with `seen`.
  for (const cc of courtsOutsideTournament(sel, tourn)) {
    out.push({ court: cc, selected: true, inTournament: false });
  }
  return out;
}

// The shiaijo allocation a competition's draw would ACTUALLY run on. Mirror of
// engine.InheritedDrawCourts (internal/engine/court_validation.go): a
// competition's own list wins whenever it has one, and an empty list means
// "inherit the tournament's", never "no shiaijo".
//
// Every console surface that judges an allocation has to resolve first, because
// the resolved value is the one the server persists and validates. Skipping it
// is how a 3-shiaijo venue's commonest competition of all - one with no shiaijo
// of its own - gets shown as fine and then refused on generate.
function inheritedDrawCourts(ownCourts, tournamentCourts) {
  const own = Array.isArray(ownCourts) ? ownCourts : [];
  const venue = Array.isArray(tournamentCourts) ? tournamentCourts : [];
  return own.length ? own : venue;
}

// Operator-facing message for the flagged pills above. Null when every
// assigned shiaijo still exists. Sibling of shiaijoCountError: predicate and
// label in one call, so no call site restates the rule.
function orphanedShiaijoError(tournamentCourts, selectedCourts) {
  const missing = courtsOutsideTournament(selectedCourts, tournamentCourts);
  if (!missing.length) return null;
  const plural = missing.length > 1;
  return `Shiaijo ${missing.join(", ")} ${plural ? "are" : "is"} no longer part of this tournament. Deselect ${plural ? "them" : "it"} and save: matches cannot run on a shiaijo the tournament does not have, and the draw will be refused until you do.`;
}

// THE single derived answer to "can this competition draw its bracket, or is
// its shiaijo allocation in the way?". Null when nothing blocks it; the
// operator-facing reason when something does.
//
// Lifted here (spec 007 R9) because the same question was being asked on
// three screens and answered on ONE: the competition header derived it, while
// the dashboard card's "Start competition →" and the tournament-level "Start
// all" picker did not, so both offered a live button for a start the server
// refuses with a 400 whose only trace is an 8s toast. A predicate that decides
// whether an operator may act belongs in one place, not copied per surface.
//
// Combines both court rules the engine's draw pipeline applies, in the order
// the operator can act on them. Scope notes:
//   - the shiaijo-COUNT rule is limited to bracket-drawing formats
//     (formatDrawsBracket); league and Swiss shiaijo run in parallel.
//   - the orphan rule applies to EVERY format: a match on a shiaijo the
//     tournament no longer has is invisible whatever the format.
//   - reads the SAVED allocation, because that is what the server will draw
//     with, INCLUDING the empty one. An empty list means "inherit the
//     tournament's shiaijo", and the engine validates the inherited count, so
//     on a venue whose own count is illegal an empty allocation is a refusal
//     waiting to happen (verified live: POST generate-draw answers 400 "It has
//     no shiaijo of its own, so the draw would run on all 3 of the
//     tournament's"). shiaijoCountError alone cannot see this: it is handed 0
//     and correctly stays silent, because 0 is not a competition's count.
//
// Call it only where a draw would actually be generated (a competition still
// in setup). A draw-ready competition has already cleared these rules and its
// start is a status flip, so gating that would block a start the server
// accepts.
function competitionDrawBlockedReason(competition, tournamentCourts) {
  if (!competition) return null;
  const courts = competition.courts || [];
  return resolvedShiaijoCountError(competition.format, courts, tournamentCourts)
    || orphanedShiaijoError(tournamentCourts, courts);
}

// The shiaijo-COUNT problem with an allocation, judged on the RESOLVED list and
// framed so an inherited count says where it came from. Null when the count is
// fine, or when the format has no bracket to split.
//
// Every surface that judges an allocation has to resolve it first: an empty
// list MEANS "inherit the tournament's shiaijo" and is what the server stores,
// so judging the raw list answers a question about a value that is never
// persisted. shiaijoCountError(0) is null, which is how a competition with no
// shiaijo of its own on a 3-shiaijo venue read as fine on the Settings screen
// while the dashboard refused its draw and sent the operator to that very
// screen to fix it.
//
// The venue count is forwarded to the count message, not just to the orphan
// check: every surface that renders this reason renders it ALONE, with no
// venue-aware hint beneath to correct it, so the unqualified message told a
// 3-shiaijo venue to "use 2 or 4" - one of which it cannot supply. A surface
// that DOES print the venue-aware hint alongside (the staged error under the
// Settings pills) calls shiaijoCountErrorFor directly instead, so the venue
// clause is not stated twice.
//
// Takes format and courts rather than a competition so a screen can ask about a
// STAGED allocation, which is on no competition object yet.
function resolvedShiaijoCountError(format, ownCourts, tournamentCourts) {
  const own = Array.isArray(ownCourts) ? ownCourts : [];
  const venue = (tournamentCourts || []).length;
  // Inheriting is only a problem when the venue's own count is not a legal
  // allocation; inheriting 2 of 2 is fine and the server accepts it. venue 0 is
  // "tournament not loaded", never "the venue has none".
  const effective = inheritedDrawCourts(own, tournamentCourts);
  const countErr = shiaijoCountErrorFor(format, effective.length, venue);
  if (!countErr) return null;
  // Name the inheritance when that is where the count came from, exactly as the
  // engine's refusal does, or the operator is handed a count they never chose.
  if (own.length || !effective.length) return countErr;
  return `This competition has no shiaijo of its own, so the draw would run on all ${venue} of the tournament's. ${countErr}`;
}

// The remedy sentences. Each has to read correctly on all four surfaces that
// render a blocker - the competition header, the overview checklist, the
// dashboard card and the "Start all" picker - so they name the tab rather than
// assuming the operator is already inside the competition.
const SHIAIJO_FIX = "Reassign shiaijo in Settings.";
const SEEDING_FIX = "Set the missing ranks or clear the seeds in Participants & seeds.";
// A duplicate is not a gap, so it does not get the gap's remedy: nothing is
// missing, and telling the operator to "set the missing ranks" sends them
// looking for a rank that is already on the roster twice.
const SEEDING_DUPLICATE_FIX = "Give each seed rank to one competitor, or clear the seeds in Participants & seeds.";

// rankList renders seed ranks as "3", "3 and 4" or "3, 4 and 5". Mirror of
// helper.RankList (internal/helper/seed_warnings.go); the two exist so the
// server's refusal and this console's block name the same ranks the same way.
function rankList(ranks) {
  return joinList(ranks, "and", "none");
}

// seedGapDiagnosis names the seed ranks still to be typed, for a set whose
// ranks are not contiguous from 1. Takes RANKS, not players, so it mirrors the
// Go implementation exactly.
//
// Mirror of helper.SeedGapDiagnosis. Both are pinned to the shared golden table
// internal/helper/testdata/seed_gap_messages.json (Go half:
// TestSeedGapDiagnosis_GoldenTable; JS half: __tests__/seed_gap.test.jsx).
//
// Returns "" for a contiguous set, an empty one, AND for the faults that are
// not gaps (a duplicate rank, a rank of 0). Those are refused by rules that
// describe themselves precisely, and must never be reported as a gap; the
// caller supplies their wording.
function seedGapDiagnosis(ranks) {
  const present = new Set();
  let highest = 0;
  for (const rank of ranks) {
    if (!Number.isInteger(rank) || rank <= 0) return "";
    if (present.has(rank)) return "";
    present.add(rank);
    if (rank > highest) highest = rank;
  }
  const missing = [];
  for (let r = 1; r < highest; r++) {
    if (!present.has(r)) missing.push(r);
  }
  if (!missing.length) return "";
  const plural = missing.length === 1 ? "" : "s";
  const verb = missing.length === 1 ? "has" : "have";
  return `Seeding is incomplete: seed rank${plural} ${rankList(missing)} ${verb} not been set, but rank ${highest} has.`;
}

// seededRanks pulls the ranks an operator has actually typed off a roster.
// Anything non-positive is UNSEEDED here rather than invalid: the seeding
// panel's own updateSeed maps a cleared or <= 0 input to null, so a rank of 0
// is not a state this roster can be in.
function seededRanks(players) {
  return (players || [])
    .map((p) => (p && Number.isInteger(p.seed) ? p.seed : 0))
    .filter((rank) => rank > 0);
}

// competitionSeedingBlocker is the seeding half of competitionDrawBlocker: the
// draw refuses a seeding whose ranks are not 1..N, so the console must refuse
// it first rather than let the operator fire a request that comes back 400.
//
// Covers the two ways a roster gets there. A GAP is the one an operator reaches
// without doing anything unusual, since the panel saves each rank the moment it
// is typed and entering seed 4 before seeds 1 to 3 leaves {4} on disk. A
// DUPLICATE takes two rows carrying the same number, which the panel also
// allows; it is refused by the same server-side validator, so it blocks here
// too and keeps its own wording (it is not a gap and naming a "missing" rank
// for it would be a lie).
function competitionSeedingBlocker(competition) {
  const ranks = seededRanks(competition.players);
  const diagnosis = seedGapDiagnosis(ranks);
  if (diagnosis) return { reason: diagnosis, fix: SEEDING_FIX, section: "participants", cta: "Fix seeding →" };
  const seen = new Set();
  const duplicates = [];
  ranks.forEach((rank) => {
    if (seen.has(rank) && !duplicates.includes(rank)) duplicates.push(rank);
    seen.add(rank);
  });
  if (duplicates.length) {
    const plural = duplicates.length === 1 ? "" : "s";
    return {
      reason: `Seeding is invalid: seed rank${plural} ${rankList(duplicates.sort((a, b) => a - b))} ${duplicates.length === 1 ? "is" : "are"} used more than once.`,
      fix: SEEDING_DUPLICATE_FIX,
      section: "participants",
      cta: "Fix seeding →",
    };
  }
  return null;
}

// competitionDrawBlocker is THE predicate for "may this competition be drawn
// yet", returning { reason, fix, section, cta } or null.
//
// It returns an object rather than a bare string because every surface renders
// the reason together with what to do about it, and those surfaces used to
// hard-code "Reassign shiaijo in Settings" as that tail. That was true while a
// blocker could only ever be a court rule; the moment a second kind of blocker
// exists, a hard-coded remedy sends the operator to the wrong screen. The fix
// and the destination now travel WITH the reason.
//
// Order is the order an operator can act on: the shiaijo allocation is a
// structural property of the competition, while the seeding is a list they may
// still be halfway through typing.
function competitionDrawBlocker(competition, tournamentCourts) {
  if (!competition) return null;
  const courtsReason = competitionDrawBlockedReason(competition, tournamentCourts);
  if (courtsReason) return { reason: courtsReason, fix: SHIAIJO_FIX, section: "settings", cta: "Reassign shiaijo →" };
  // NOTE: only sees a seeding when the caller's competition carries one.
  // GET /api/viewer/competitions/:id hydrates seeds (WithSeeds: true) so the
  // competition header and its overview block correctly; the LIST payload the
  // dashboard reads does not (WithSeeds: false), so the dashboard card and
  // "Start all" fall back to the server's refusal, which names the missing
  // ranks in its 400. Hydrating seeds into a public list read by every viewer
  // to answer an admin question is the wrong trade.
  return competitionSeedingBlocker(competition);
}

// What the dashboard's "Start all" may actually start, and what it must not
// offer. Returns { startable: [competition], blocked: [{ comp, reason }] }.
//
// Eligibility (still in setup, at least 2 participants) and the court rules
// are answered together so the picker cannot offer a competition the server
// refuses: batching a blocked competition in produced a guaranteed entry in
// the modal's "failed" list carrying a raw API message. Blocked competitions
// are RETURNED rather than dropped so the modal can name them; "start all"
// must never quietly mean "start most".
function partitionStartableCompetitions(competitions, tournamentCourts) {
  const startable = [];
  const blocked = [];
  (competitions || []).forEach((c) => {
    if (!c || c.status !== "setup" || (c.players || []).length < 2) return;
    // reason carries its own remedy: the modal lists several competitions at
    // once and a single hard-coded tail cannot be right for all of them.
    const blocker = competitionDrawBlocker(c, tournamentCourts);
    if (blocker) blocked.push({ comp: c, reason: `${blocker.reason} ${blocker.fix}` });
    else startable.push(c);
  });
  return { startable, blocked };
}

// Resolves the 0-based round index from a match object. Bracket matches
// carry m.roundIndex (stamped by compMatches/viewer.jsx); fall back to a
// non-negative numeric m.round for any older shapes.
// Returns 0 for pool matches (no per-round lineup).
function resolveRoundIndex(match) {
  if (typeof match.roundIndex === "number" && match.roundIndex >= 0) return match.roundIndex;
  if (typeof match.round === "number" && match.round >= 0) return match.round;
  return 0;
}

// Guard window assignments so this file stays safely importable in
// non-browser test environments (matches the pattern in data.jsx / ui.jsx).
if (typeof window !== "undefined") {
  window.resolveRoundIndex = resolveRoundIndex;
  window.sideName = sideName;
  window.hasBothSides = hasBothSides;
  window.hasPoolOriginPlaceholder = hasPoolOriginPlaceholder;
  window.isPendingBracketMatch = isPendingBracketMatch;
  window.compMatchStats = compMatchStats;
  window.bracketFullyComplete = bracketFullyComplete;
  window.normalizeDate = normalizeDate;
  window.dmyToIso = dmyToIso;
  window.isoToDmy = isoToDmy;
  window.compareDmy = compareDmy;
  window.isValidDate = isValidDate;
  window.validateAndNormalizeDate = validateAndNormalizeDate;
  window.decideNumericUpdate = decideNumericUpdate;
  window.getScoreBtnClass = getScoreBtnClass;
  window.DATE_ERR_INVALID_FORMAT = DATE_ERR_INVALID_FORMAT;
  window.DATE_ERR_YEAR_RANGE = DATE_ERR_YEAR_RANGE;
  window.MIN_YEAR = MIN_YEAR;
  window.MAX_YEAR = MAX_YEAR;
  window.MAX_TEAM_SIZE = MAX_TEAM_SIZE;
  window.MAX_COURTS = MAX_COURTS;
  window.MAX_RANK = MAX_RANK;
  window.MAX_TOURNAMENT_DURATION_DAYS = MAX_TOURNAMENT_DURATION_DAYS;
  window.deriveTournamentDays = deriveTournamentDays;
  window.setCachedAuthConfig = setCachedAuthConfig;
  window.getCachedAuthConfig = getCachedAuthConfig;
  window.promptAdminPassword = promptAdminPassword;
  window.normalizeCourts = normalizeCourts;
  window.courtCount = courtCount;
  window.shiaijoCountHint = shiaijoCountHint;
  window.shiaijoPickerError = shiaijoPickerError;
  window.SHIAIJO_NONE_SELECTED = SHIAIJO_NONE_SELECTED;
  window.shiaijoCountErrorFor = shiaijoCountErrorFor;
  window.shiaijoCountHintFor = shiaijoCountHintFor;
  window.inheritedDrawCourts = inheritedDrawCourts;
  window.shiaijoVenueHint = shiaijoVenueHint;
  window.courtPillOptions = courtPillOptions;
  window.orphanedShiaijoError = orphanedShiaijoError;
  window.competitionDrawBlocker = competitionDrawBlocker;
  window.competitionSeedingBlocker = competitionSeedingBlocker;
  window.resolvedShiaijoCountError = resolvedShiaijoCountError;
  window.partitionStartableCompetitions = partitionStartableCompetitions;
}

// --- Elevated (destructive-ops) password prompt (spec 004 / mp-e21) ---
//
// authConfig is held in app.jsx state and threaded to only a few components.
// Rather than prop-drill it to every destructive call site, app.jsx pushes
// the latest value here whenever it resolves /api/auth-config, and the
// destructive handlers read it via promptAdminPassword(). Single writer
// (app.jsx), many readers: no React context needed for an imperative prompt.
//
// IMPORTANT: the cache lives on `window`, NOT a module-level variable.
// esbuild bundles this file into BOTH the app and admin entry chunks, so a
// module-scoped `let` would be two independent instances: app.jsx's writes
// would never be visible to the admin components' reads. A single window slot
// is shared across both bundles. Guarded for non-browser (vitest) use.
const _authConfigSlot = () => (typeof window !== "undefined" ? window : globalThis);

// setCachedAuthConfig is called by app.jsx after every fetchAuthConfig().
function setCachedAuthConfig(cfg) {
  _authConfigSlot().__bcAuthConfig = cfg || null;
}

function getCachedAuthConfig() {
  return _authConfigSlot().__bcAuthConfig || null;
}

// promptAdminPassword returns the elevated password to send with a
// destructive action, following the "re-prompt every time" model:
//
//   - gate inactive (file mode, no admin pw set) → returns "" so the caller
//     proceeds; the server ignores the (omitted) X-Admin-Password header.
//   - gate active but not configured (locked mode, env var unset) → alerts
//     the operator and returns null so the caller ABORTS (the server would
//     503 anyway).
//   - gate active and configured → window.prompt for the password. Returns
//     the typed value, or null if the operator cancels or submits empty
//     (the caller aborts).
//
// Caller contract (now async): `const a = await promptAdminPassword(); if (a === null) return;`
// then pass `a` as the trailing adminPassword arg to the API method. A wrong
// password surfaces as the API's 401 error (caught/toasted by the caller);
// the operator simply retries the action, which re-prompts.
//
// Returns a Promise so the password is collected via the app's themed,
// accessible promptDialog (masked input) instead of window.prompt, which
// renders the password in plaintext in some browsers and can't be styled.
async function promptAdminPassword(message) {
  const cfg = getCachedAuthConfig();
  if (!cfg || !cfg.elevatedRequired) return "";
  if (cfg.elevatedConfigured === false) {
    window.alert(
      "This action requires an admin password, but none is configured on this server. " +
      "In locked mode set TOURNAMENT_ADMIN_PASSWORD_HASH; in file mode set one in Settings."
    );
    return null;
  }
  const pw = await window.promptDialog({
    title: "Admin password required",
    message: message || "This action requires the admin (destructive-ops) password:",
    password: true,
    confirmLabel: "Confirm",
  });
  return pw ? pw : null;
}

export {
  setCachedAuthConfig,
  getCachedAuthConfig,
  promptAdminPassword,
  sideName,
  hasBothSides,
  hasPoolOriginPlaceholder,
  isPendingBracketMatch,
  compMatchStats,
  bracketFullyComplete,
  normalizeDate,
  dmyToIso,
  isoToDmy,
  compareDmy,
  isValidDate,
  validateAndNormalizeDate,
  decideNumericUpdate,
  getScoreBtnClass,
  DATE_ERR_INVALID_FORMAT,
  DATE_ERR_YEAR_RANGE,
  MIN_YEAR,
  MAX_YEAR,
  MAX_TEAM_SIZE,
  MAX_COURTS,
  MAX_RANK,
  MAX_TOURNAMENT_DURATION_DAYS,
  deriveTournamentDays,
  normalizeCourts,
  courtCount,
  shiaijoCountError,
  shiaijoCountErrorFor,
  shiaijoCountHint,
  shiaijoCountHintFor,
  shiaijoPickerError,
  SHIAIJO_NONE_SELECTED,
  allowedShiaijoCounts,
  shiaijoVenueSplitExample,
  shiaijoVenueHint,
  VALID_SHIAIJO_COUNTS,
  formatDrawsBracket,
  courtsOutsideTournament,
  inheritedDrawCourts,
  courtPillOptions,
  orphanedShiaijoError,
  competitionDrawBlockedReason,
  resolvedShiaijoCountError,
  competitionDrawBlocker,
  competitionSeedingBlocker,
  seedGapDiagnosis,
  seededRanks,
  partitionStartableCompetitions,
  resolveRoundIndex,
};

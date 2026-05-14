// Pure helpers shared across the admin layer.
// No JSX, no React deps. See web-mobile/admin_split_plan.md.

// sideA/sideB can be a string (raw backend shape), an object with .name
// (normalizeMatch output, which substitutes {id:"",name:""} for missing sides),
// or null. Return the participant's display name, or "" when no real side is
// present. Used by compMatchStats and AdminTopbar's live-strip filter, so the
// two stay in lockstep about what "has a real side" means.
function sideName(side) {
  if (!side) return "";
  if (typeof side === "string") return side;
  return side.name || "";
}

// True when a match has both sides resolved to a real participant (not a
// bye, not a TBD bracket placeholder, not an unresolved "Winner of rX-mY"
// reference). The naïve `m.sideA && m.sideB` test is almost always wrong
// post-normalizeMatch — that function substitutes {id:"",name:""} for
// missing sides, which is truthy. Use this helper in filter predicates
// / rendering guards instead.
//
// Bracket-side caveat: future-round matches carry placeholder side
// names like `"Winner of r0-m1"` until the source match resolves. Those
// are non-empty strings — sideName() returns them as-is — so the
// underlying `sideName(...)` check ALONE isn't enough. We reject the
// EXACT placeholder shape `Winner of r<n>-m<n>` (the literal format
// emitted by internal/engine/bracket.go at lines 65 and 73), NOT every
// name that happens to start with "Winner of " — a legitimate
// participant named "Winner of the 2025 Cup" should still pass.
// (See web-mobile/js/viewer.jsx for the consumer.)
const BRACKET_PLACEHOLDER_RE = /^Winner of r\d+-m\d+$/;
function hasBothSides(m) {
  if (!m) return false;
  const a = sideName(m.sideA);
  const b = sideName(m.sideB);
  if (!a || !b) return false;
  if (BRACKET_PLACEHOLDER_RE.test(a) || BRACKET_PLACEHOLDER_RE.test(b)) return false;
  return true;
}

// Returns { total, done, live } match counts for a single competition object.
// Accepts either:
//   - flat `poolMatches` array from GET /api/viewer/competitions (list endpoint)
//   - structured `pools[].matches` from GET /api/viewer/competitions/:id (detail endpoint)
// The admin-side GET /api/competitions/:id returns only config; use the viewer
// endpoints when match counts are needed.
function compMatchStats(c) {
  let total = 0, done = 0, live = 0;
  // Truthy-checking the side itself isn't enough — normalizeMatch substitutes
  // {id:"",name:""} for missing sides, which is truthy. sideName() returns
  // "" for those, so byes and unresolved bracket slots stay uncounted.
  const count = (m) => {
    if (!m || !sideName(m.sideA) || !sideName(m.sideB)) return;
    total++;
    if (m.status === "completed") done++;
    if (m.status === "running") live++;
  };
  if (Array.isArray(c.poolMatches)) {
    c.poolMatches.forEach(count);
  } else if (c.pools) {
    c.pools.forEach((p) => (p.matches || []).forEach(count));
  }
  if (c.bracket && c.bracket.rounds) {
    c.bracket.rounds.forEach((r) => (r || []).forEach(count));
  }
  return { total, done, live };
}

// Canonical numeric bounds. The year range is shared by every date
// validator (admin_helpers.jsx validateAndNormalizeDate, admin_competition.jsx
// saveNow inline). MAX_TEAM_SIZE is the canonical team-size cap; the
// scoring modal's TEAM_POSITIONS array is built from it (see
// admin_scoring_modal.jsx), and the team-size inputs in admin_competition
// + admin_setup use it as their HTML `max` attribute. Bumping any of these
// here flows to every consumer mechanically.
const MIN_YEAR = 1900;
const MAX_YEAR = 2100;
const MAX_TEAM_SIZE = 9;

// Canonical date error messages. Referenced by validateAndNormalizeDate
// AND by AdminSettings.saveNow's inline asymmetric validation, so the
// user-facing UX stays consistent across all four date-validation sites
// regardless of where the error is generated. Exported on window + ES.
// The year-range message is a template so changing MIN_YEAR/MAX_YEAR
// above auto-updates the user-facing text.
const DATE_ERR_INVALID_FORMAT = "Invalid date. Please pick a valid day.";
const DATE_ERR_YEAR_RANGE = `Year must be between ${MIN_YEAR} and ${MAX_YEAR}.`;

// Combined date validation + normalization. Returns:
//   - { norm: "YYYY-MM-DD", error: null }  on success
//   - { norm: null, error: "<message>" }   on failure
//
// Canonical predicate for date inputs across the admin UI. Save paths
// (AdminEditTournament.handleSave, AdminCreateCompetition.create) use the
// `error` for user-facing messaging AND `norm` for the value to save.
// Pure boolean callers use `isValidISODate` below.
//
// AdminSettings.saveNow has an intentional asymmetry (shape-invalid +
// unchanged → allow save, preserving legacy data; year-invalid → always
// block) so it doesn't use this helper directly — see comment there.
// It does use DATE_ERR_* constants above so error UX stays in lockstep.
function validateAndNormalizeDate(date) {
  const norm = normalizeDate(date);
  if (!norm || !/^\d{4}-\d{2}-\d{2}$/.test(norm)) {
    return { norm: null, error: DATE_ERR_INVALID_FORMAT };
  }
  const year = parseInt(norm.substring(0, 4));
  if (year < MIN_YEAR || year > MAX_YEAR) {
    return { norm: null, error: DATE_ERR_YEAR_RANGE };
  }
  return { norm, error: null };
}

// Boolean predicate: is `date` a valid ISO-format day in the supported
// year range (1900–2100)? Used by AdminCompetition's "Start competition"
// button gate — anywhere a boolean result is enough. For save flows that
// need both the boolean AND the normalized value, use
// validateAndNormalizeDate above.
function isValidISODate(date) {
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
//   so the render side can do `value={Number.isFinite(v) ? v : ""}` and
//   keep the cleared display empty (matches the matchDuration pattern at
//   admin_schedule.jsx).
// - `shouldSave` is true only when the parsed value is a positive integer
//   ≥ min. Callers should skip saveLater on false AND cancel any pending
//   save (otherwise an earlier in-flight debounced save with the old good
//   value would land on the server while the user sees an empty input,
//   producing a state mismatch that only resolves on next SSE refresh).
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

function normalizeDate(d) {
  if (!d) return d;
  let out;
  if (/^\d{4}-\d{2}-\d{2}$/.test(d)) {
    out = d;
  } else {
    const match = d.match(/^(\d{1,2})[-/](\d{1,2})[-/](\d{4})$/);
    if (!match) return d;
    out = `${match[3]}-${match[2].padStart(2, '0')}-${match[1].padStart(2, '0')}`;
  }
  // Reject semantically invalid dates like "2026-13-32" or "31-02-2026".
  // JS's Date constructor silently rolls invalid components over (Feb 31 →
  // Mar 3), so round-trip the parts through UTC and require an exact match.
  const [y, m, day] = out.split('-').map(Number);
  const dt = new Date(Date.UTC(y, m - 1, day));
  if (
    isNaN(dt.getTime()) ||
    dt.getUTCFullYear() !== y ||
    dt.getUTCMonth() + 1 !== m ||
    dt.getUTCDate() !== day
  ) {
    return null;
  }
  return out;
}

// Guard window assignments so this file stays safely importable in
// non-browser test environments (matches the pattern in data.jsx / ui.jsx).
if (typeof window !== "undefined") {
  window.sideName = sideName;
  window.hasBothSides = hasBothSides;
  window.compMatchStats = compMatchStats;
  window.normalizeDate = normalizeDate;
  window.isValidISODate = isValidISODate;
  window.validateAndNormalizeDate = validateAndNormalizeDate;
  window.decideNumericUpdate = decideNumericUpdate;
  window.DATE_ERR_INVALID_FORMAT = DATE_ERR_INVALID_FORMAT;
  window.DATE_ERR_YEAR_RANGE = DATE_ERR_YEAR_RANGE;
  window.MIN_YEAR = MIN_YEAR;
  window.MAX_YEAR = MAX_YEAR;
  window.MAX_TEAM_SIZE = MAX_TEAM_SIZE;
}

// Also exported so the vitest suite under web-mobile/js/__tests__/ can
// import these directly without going through window globals.
export {
  sideName,
  hasBothSides,
  compMatchStats,
  normalizeDate,
  isValidISODate,
  validateAndNormalizeDate,
  decideNumericUpdate,
  DATE_ERR_INVALID_FORMAT,
  DATE_ERR_YEAR_RANGE,
  MIN_YEAR,
  MAX_YEAR,
  MAX_TEAM_SIZE,
};

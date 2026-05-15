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
  // Use hasBothSides() — the canonical cross-file predicate — so admin
  // dashboard / overview / live-strip stats can't drift from viewer-side
  // filtering. Inline `sideName(m.sideA) && sideName(m.sideB)` was almost
  // right (skips byes / normalizeMatch's empty-side substitute) but missed
  // bracket placeholders like "Winner of r0-m1" — those have truthy
  // sideName() values, so future-round matches were counted as real before
  // their source resolves. hasBothSides also rejects that exact shape.
  const count = (m) => {
    if (!hasBothSides(m)) return;
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
// button gate — anywhere a boolean result is enough. For save flows that
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
//   so the render side can do `value={Number.isFinite(v) ? v : ""}` and
//   keep the cleared display empty (matches the matchDuration pattern at
//   admin_schedule.jsx).
// - `shouldSave` is true only when the parsed value is a positive integer
//   ≥ min. Callers MUST still issue a saveLater on false — the debounceRef
//   is single-slot and covers all fields, so an earlier scheduled save
//   captured the OLD valid value for THIS field and will commit it over
//   the wire if not replaced. Use saveLater(next-with-NaN) so the
//   commit-side safeInt fallback resolves the field to the on-disk
//   c.<field>, while cross-field edits in `next` (e.g. Name typed
//   concurrently) still propagate. `shouldSave` is therefore informational
//   only — callers no longer branch on it.
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
// isoToDmy accepts a DMY DD-MM-YYYY pass-through symmetrically — most callers
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

// Guard window assignments so this file stays safely importable in
// non-browser test environments (matches the pattern in data.jsx / ui.jsx).
if (typeof window !== "undefined") {
  window.sideName = sideName;
  window.hasBothSides = hasBothSides;
  window.compMatchStats = compMatchStats;
  window.normalizeDate = normalizeDate;
  window.dmyToIso = dmyToIso;
  window.isoToDmy = isoToDmy;
  window.compareDmy = compareDmy;
  window.isValidDate = isValidDate;
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
  dmyToIso,
  isoToDmy,
  compareDmy,
  isValidDate,
  validateAndNormalizeDate,
  decideNumericUpdate,
  DATE_ERR_INVALID_FORMAT,
  DATE_ERR_YEAR_RANGE,
  MIN_YEAR,
  MAX_YEAR,
  MAX_TEAM_SIZE,
};

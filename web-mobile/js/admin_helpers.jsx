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
// bye, not a TBD bracket placeholder). The naïve `m.sideA && m.sideB` test
// is almost always wrong post-normalizeMatch — that function substitutes
// {id:"",name:""} for missing sides, which is truthy. Use this helper in
// filter predicates / rendering guards instead.
function hasBothSides(m) {
  return !!(m && sideName(m.sideA) && sideName(m.sideB));
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

// Canonical date error messages. Referenced by validateAndNormalizeDate
// AND by AdminSettings.saveNow's inline asymmetric validation, so the
// user-facing UX stays consistent across all four date-validation sites
// regardless of where the error is generated. Exported on window + ES.
const DATE_ERR_INVALID_FORMAT = "Invalid date. Please pick a valid day.";
const DATE_ERR_YEAR_RANGE = "Year must be between 1900 and 2100.";

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
  if (year < 1900 || year > 2100) {
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
  window.DATE_ERR_INVALID_FORMAT = DATE_ERR_INVALID_FORMAT;
  window.DATE_ERR_YEAR_RANGE = DATE_ERR_YEAR_RANGE;
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
  DATE_ERR_INVALID_FORMAT,
  DATE_ERR_YEAR_RANGE,
};

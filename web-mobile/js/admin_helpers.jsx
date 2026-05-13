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

function normalizeDate(d) {
  if (!d) return d;
  if (/^\d{4}-\d{2}-\d{2}$/.test(d)) return d;
  const match = d.match(/^(\d{1,2})[-/](\d{1,2})[-/](\d{4})$/);
  if (match) {
    return `${match[3]}-${match[2].padStart(2, '0')}-${match[1].padStart(2, '0')}`;
  }
  return d;
}

window.sideName = sideName;
window.compMatchStats = compMatchStats;
window.normalizeDate = normalizeDate;

// Also exported so the vitest suite under web-mobile/js/__tests__/ can
// import these directly without going through window globals.
export { sideName, compMatchStats, normalizeDate };

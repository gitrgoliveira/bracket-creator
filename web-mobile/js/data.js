// Sample tournament data.
//
// Tournament: a multi-day event held at a venue.
//   - has its own list of `courts` (Shiaijo) — these are the physical concurrency unit.
//     Different competitions can share courts; concurrency depends on the courts assigned to each.
//   - holds many Competitions.
//
// Competition: one event within a tournament (e.g., "Men's Individual", "Women's Teams").
//   - kind: "individual" | "team"  (only two kinds — gender/age tags are metadata)
//   - format: "playoffs" (knockout only) | "pools" (pools + knockout) | "pools_only"
//   - has its own list of competitors (players or teams).
//   - has pools (optional) AND a bracket (optional). Both can coexist.
//   - assigned to a subset of tournament courts.
//
// Match: an atomic fight, owned by a pool or a bracket round. Stores court, scheduledAt,
// status, score. Score can be edited at any time by an admin.

const DOJOS = [
  "Mumeishi", "Sanshukai", "Nenriki", "Kenshinkan", "Wakaba",
  "Hizen", "Shobukan", "Edoya", "Tora", "Ryokushinkai",
  "Hokushinkan", "Senshinkan",
];
const FIRST_M = ["Akira", "Hiroshi", "Kenji", "Takashi", "Sho", "Daichi", "Ren", "Marcus", "Liam", "Owen", "Felix", "Jonas", "Pierre", "Diego", "Arjun", "Tomas", "Kai", "Leon", "Noah", "Mateo", "Hugo", "Theo", "Ezra", "Kenta", "Yuto"];
const FIRST_F = ["Sakura", "Aiko", "Mei", "Yui", "Hana", "Naomi", "Rika", "Kana", "Elena", "Sofia", "Anna", "Clara", "Maya", "Lin", "Iris", "Nora", "Yoko", "Mariko", "Emi", "Akari"];
const LAST = ["Tanaka", "Sato", "Suzuki", "Watanabe", "Yamamoto", "Nakamura", "Kobayashi", "Ito", "Saito", "Kato", "Yoshida", "Mori", "Hayashi", "Shimizu", "Dupont", "Martin", "Schmidt", "Müller", "Rossi", "Garcia", "Silva", "Chen", "Park", "Kim", "Singh", "Patel", "Novak"];

function makePlayer(i, gender, prefix, seed) {
  const first = (gender === "F" ? FIRST_F : FIRST_M);
  const fn = first[i % first.length];
  const ln = LAST[(i * 7 + (gender === "F" ? 3 : 0)) % LAST.length];
  return { id: `${prefix}-p${i + 1}`, name: `${fn} ${ln}`, dojo: DOJOS[(i * 3) % DOJOS.length], seed: seed || null };
}
function makeTeam(i, prefix, seed) {
  const dojo = DOJOS[i % DOJOS.length];
  return { id: `${prefix}-t${i + 1}`, name: `${dojo} ${["A","B","C","Red","Blue"][i % 5]}`, dojo, seed: seed || null };
}
// kind: "individual" | "team". gender hint: "M" | "F" | "X" (for naming variety only).
function makeCompetitors(count, kind, prefix, seedCount = 0, gender = "M") {
  const out = [];
  for (let i = 0; i < count; i++) {
    const seed = i < seedCount ? i + 1 : null;
    if (kind === "team") out.push(makeTeam(i, prefix, seed));
    else out.push(makePlayer(i, gender, prefix, seed));
  }
  return out;
}

function standardSeedOrder(n) { let order = [1]; while (order.length < n) { const next = []; const total = order.length * 2 + 1; for (const s of order) { next.push(s); next.push(total - s); } order = next; } return order; }
function nextPow2(n) { let p = 1; while (p < n) p *= 2; return p; }

let _matchSeq = 0;
function newMatchId(prefix) { _matchSeq++; return `${prefix}-m${_matchSeq}`; }

function buildBracket(players, courtsAssigned) {
  if (!players || players.length < 2) return null;
  const courts = courtsAssigned && courtsAssigned.length ? courtsAssigned : ["A"];
  const size = nextPow2(players.length);
  const order = standardSeedOrder(size);
  const slots = order.map((rank) => players[rank - 1] || null);
  const rounds = [];
  let r1 = [];
  for (let i = 0; i < slots.length; i += 2) {
    r1.push({ id: `m-r1-${i / 2}-${_matchSeq++}`, sideA: slots[i], sideB: slots[i + 1], winner: null, court: courts[Math.floor(i / 2) % courts.length], scheduledAt: null, status: "scheduled", score: null });
  }
  rounds.push(r1);
  let prev = r1;
  while (prev.length > 1) {
    const next = [];
    for (let i = 0; i < prev.length; i += 2) next.push({ id: `m-r${rounds.length + 1}-${i / 2}-${_matchSeq++}`, sideA: null, sideB: null, winner: null, court: courts[Math.floor(i / 2) % courts.length], scheduledAt: null, status: "pending", score: null });
    rounds.push(next); prev = next;
  }
  return rounds;
}

function advanceByes(rounds) {
  if (!rounds) return rounds;
  const r1 = rounds[0];
  r1.forEach((m) => { if (m.sideA && !m.sideB) { m.winner = m.sideA; m.status = "complete"; m.score = { type: "bye" }; } else if (!m.sideA && m.sideB) { m.winner = m.sideB; m.status = "complete"; m.score = { type: "bye" }; } });
  if (rounds.length > 1) rounds[1].forEach((m, i) => { const a = r1[i * 2]; const b = r1[i * 2 + 1]; if (a.winner) m.sideA = a.winner; if (b.winner) m.sideB = b.winner; if (m.sideA && m.sideB) m.status = "scheduled"; });
  return rounds;
}

function pickIppons(n) { const pool = ["M", "K", "D", "T"]; const out = []; for (let i = 0; i < n; i++) out.push(pool[Math.floor(Math.random() * pool.length)]); return out; }

function simulateRounds(rounds, completedRounds, includePartial = false) {
  for (let r = 0; r < completedRounds && r < rounds.length; r++) {
    rounds[r].forEach((m) => {
      if (m.status === "complete") return;
      if (!m.sideA || !m.sideB) { m.winner = m.sideA || m.sideB; m.status = m.winner ? "complete" : "pending"; if (m.winner) m.score = { type: "bye" }; return; }
      const seedA = m.sideA.seed || 99; const seedB = m.sideB.seed || 99;
      const aWins = seedA < seedB ? Math.random() > 0.25 : Math.random() > 0.65;
      m.winner = aWins ? m.sideA : m.sideB; m.status = "complete";
      const winPts = Math.random() > 0.4 ? 2 : 1;
      m.score = { type: "ippon", winnerPts: winPts, loserPts: winPts === 2 && Math.random() > 0.5 ? 1 : 0, ippons: pickIppons(winPts) };
    });
    if (r + 1 < rounds.length) rounds[r + 1].forEach((m, i) => { const a = rounds[r][i * 2]; const b = rounds[r][i * 2 + 1]; if (a.winner) m.sideA = a.winner; if (b.winner) m.sideB = b.winner; if (m.sideA && m.sideB && m.status === "pending") m.status = "scheduled"; });
  }
  if (includePartial && completedRounds < rounds.length) { const r = rounds[completedRounds]; if (r && r[0] && r[0].sideA && r[0].sideB) { r[0].status = "in_progress"; r[0].score = { type: "ippon", winnerPts: 1, loserPts: 0, ippons: ["M"], live: true }; } }
  return rounds;
}

// Schedule matches sequentially across the assigned courts, starting at startTime.
function scheduleRound(matches, startTime, perMatchMin, courtNames) {
  if (!courtNames || !courtNames.length) courtNames = ["A"];
  const perCourt = courtNames.map(() => 0);
  matches.forEach((m, i) => {
    const courtIdx = i % courtNames.length;
    m.court = courtNames[courtIdx];
    const slot = perCourt[courtIdx]++;
    m.scheduledAt = addMinutes(startTime, slot * perMatchMin);
  });
  return matches;
}
function addMinutes(t, mins) { const [h, m] = t.split(":").map(Number); const total = h * 60 + m + mins; const hh = Math.floor(total / 60) % 24; const mm = total % 60; return `${String(hh).padStart(2, "0")}:${String(mm).padStart(2, "0")}`; }

// ---------- Pools ----------
// poolMode: "max" => poolSize is a maximum (never more than N per pool — flex pool count to fit)
//           "min" => poolSize is a minimum (try to keep at least N per pool — fewer, larger pools)
function buildPools(players, opts = {}) {
  const { poolMode = "max", poolSize = 4, winnersPerPool = 2, courts = ["A"] } = opts;
  if (!players || players.length < 2) return null;
  let numPools;
  if (poolMode === "min") {
    numPools = Math.max(1, Math.floor(players.length / poolSize));
  } else { // "max"
    numPools = Math.max(1, Math.ceil(players.length / poolSize));
  }
  const pools = [];
  for (let i = 0; i < numPools; i++) pools.push({ id: `pool-${String.fromCharCode(65 + i)}-${_matchSeq++}`, name: `Pool ${String.fromCharCode(65 + i)}`, court: courts[i % courts.length], players: [], matches: [], standings: [], winnersPerPool });
  // snake distribute by seed so each pool gets a balanced spread
  const sorted = [...players].sort((a, b) => (a.seed || 99) - (b.seed || 99));
  sorted.forEach((p, idx) => {
    const row = Math.floor(idx / numPools);
    const col = row % 2 === 0 ? idx % numPools : (numPools - 1) - (idx % numPools);
    pools[col].players.push(p);
  });
  pools.forEach((pool) => {
    const ps = pool.players; let mi = 0;
    for (let i = 0; i < ps.length; i++) for (let j = i + 1; j < ps.length; j++) pool.matches.push({ id: `${pool.id}-m${mi++}`, sideA: ps[i], sideB: ps[j], winner: null, status: "scheduled", score: null, court: pool.court });
  });
  return pools;
}

function simulatePools(pools, completedPct = 1) {
  pools.forEach((pool) => {
    const total = pool.matches.length; const toComplete = Math.floor(total * completedPct);
    pool.matches.forEach((m, i) => {
      if (i >= toComplete) return;
      const seedA = m.sideA.seed || 99; const seedB = m.sideB.seed || 99;
      const aWins = seedA < seedB ? Math.random() > 0.3 : Math.random() > 0.6;
      m.winner = aWins ? m.sideA : m.sideB; m.status = "complete";
      const winPts = Math.random() > 0.5 ? 2 : 1;
      m.score = { type: "ippon", winnerPts: winPts, loserPts: winPts === 2 && Math.random() > 0.5 ? 1 : 0, ippons: pickIppons(winPts) };
    });
    if (toComplete < total) { const next = pool.matches[toComplete]; if (next) { next.status = "in_progress"; next.score = { type: "ippon", winnerPts: 1, loserPts: 0, ippons: ["K"], live: true }; } }
    pool.standings = computeStandings(pool);
  });
  return pools;
}
function computeStandings(pool) {
  const stats = {};
  pool.players.forEach((p) => { stats[p.id] = { player: p, wins: 0, losses: 0, ippons: 0, given: 0, played: 0 }; });
  pool.matches.forEach((m) => {
    if (m.status !== "complete" || !m.winner) return;
    const winId = m.winner.id; const loseId = m.sideA.id === winId ? m.sideB.id : m.sideA.id;
    stats[winId].wins++; stats[winId].played++; stats[loseId].losses++; stats[loseId].played++;
    if (m.score) { stats[winId].ippons += m.score.winnerPts || 0; stats[loseId].ippons += m.score.loserPts || 0; stats[winId].given += m.score.loserPts || 0; stats[loseId].given += m.score.winnerPts || 0; }
  });
  return Object.values(stats).sort((a, b) => { if (b.wins !== a.wins) return b.wins - a.wins; const ad = a.ippons - a.given; const bd = b.ippons - b.given; if (bd !== ad) return bd - ad; return b.ippons - a.ippons; });
}

function poolWinners(pools) {
  const out = []; pools.forEach((p) => p.standings.slice(0, p.winnersPerPool).forEach((s) => out.push(s.player))); return out;
}

// ---------- Competition ----------
// kind: "individual" | "team"
// format: "playoffs" | "pools"  (pools format always implies pools followed by a bracket)
function buildCompetition({ id, name, kind, gender = "X", format, sampleRoster = "medium", courts, seedCount, status, startTime, teamSize, poolMode, poolSize, winnersPerPool }) {
  const count = sampleRoster ? ({ small: 8, medium: 16, large: 32 }[sampleRoster] || 16) : 0;
  const players = count > 0 ? makeCompetitors(count, kind, id, seedCount, gender) : [];
  const c = {
    id, name, kind, gender, format, status,
    teamSize: teamSize || (kind === "team" ? 5 : 0),
    courts: courts || ["A", "B"],
    players,
    pools: null, bracket: null,
    settings: {
      poolMode: poolMode || "max",
      poolSize: poolSize || 4,
      winnersPerPool: winnersPerPool || 2,
      roundRobin: true, mirror: true, withZekken: false,
    },
    startTime: startTime || "09:00",
  };
  if (status === "setup") return c;

  if (format === "pools") {
    c.pools = buildPools(players, { poolMode: c.settings.poolMode, poolSize: c.settings.poolSize, winnersPerPool: c.settings.winnersPerPool, courts: c.courts });
    if (status === "pools") {
      simulatePools(c.pools, 0.6);
      // ALSO build empty bracket scaffold so the playoffs tab is visible/in-progress alongside pools
      // (some federations seed playoffs early; others wait — we show it as TBD).
      const placeholder = c.pools.map((_, i) => ({ id: `tbd-${i}`, name: `TBD`, dojo: "", seed: null }));
      c.bracket = buildBracket(placeholder.slice(0, Math.min(placeholder.length, 8)), c.courts);
    } else if (status === "playoffs") {
      simulatePools(c.pools, 1);
      c.bracket = buildBracket(poolWinners(c.pools), c.courts); advanceByes(c.bracket);
      simulateRounds(c.bracket, 1, true);
    } else if (status === "completed") {
      simulatePools(c.pools, 1);
      c.bracket = buildBracket(poolWinners(c.pools), c.courts); advanceByes(c.bracket);
      simulateRounds(c.bracket, c.bracket.length);
    }
  } else {
    c.bracket = buildBracket(players, c.courts); advanceByes(c.bracket);
    if (status === "playoffs") simulateRounds(c.bracket, Math.max(1, Math.floor(c.bracket.length / 2)), true);
    if (status === "completed") simulateRounds(c.bracket, c.bracket.length);
  }
  if (c.bracket && c.bracket[0]) scheduleRound(c.bracket[0], c.startTime, 5, c.courts);
  return c;
}

// ---------- Tournament ----------
function buildTournament({ id, name, date, venue, status, courts, competitions }) {
  return { id, name, date, venue, status, courts: courts || ["A", "B"], competitions };
}

function competitionStatus(comps) {
  if (!comps.length) return "setup";
  if (comps.every((c) => c.status === "completed")) return "completed";
  if (comps.some((c) => c.status === "pools")) return "pools";
  if (comps.some((c) => c.status === "playoffs")) return "playoffs";
  if (comps.every((c) => c.status === "setup")) return "setup";
  return "playoffs";
}

const SAMPLE_TOURNAMENTS = [
  (() => {
    const courts = ["A", "B", "C"];
    const comps = [
      buildCompetition({ id: "lc26-mi", name: "Men's Individual", kind: "individual", gender: "M", format: "playoffs", sampleRoster: "medium", seedCount: 4, status: "playoffs", startTime: "09:00", courts: ["A","B"] }),
      buildCompetition({ id: "lc26-wi", name: "Women's Individual", kind: "individual", gender: "F", format: "playoffs", sampleRoster: "small", seedCount: 2, status: "playoffs", startTime: "09:00", courts: ["C"] }),
      buildCompetition({ id: "lc26-mt", name: "Men's Teams", kind: "team", format: "pools", sampleRoster: "small", seedCount: 0, status: "setup", startTime: "14:00", teamSize: 5, courts: ["A","B"] }),
    ];
    return buildTournament({ id: "lc2026", name: "London Cup 2026", date: "2026-05-12", venue: "Crystal Palace Sports Centre", courts, status: competitionStatus(comps), competitions: comps });
  })(),
  (() => {
    const courts = ["A", "B", "C", "D"];
    const comps = [
      buildCompetition({ id: "ko-mi", name: "Men's Individual", kind: "individual", gender: "M", format: "pools", sampleRoster: "large", seedCount: 4, status: "pools", startTime: "09:00", courts: ["A","B"] }),
      buildCompetition({ id: "ko-wi", name: "Women's Individual", kind: "individual", gender: "F", format: "pools", sampleRoster: "medium", seedCount: 4, status: "pools", startTime: "09:00", courts: ["C","D"] }),
      buildCompetition({ id: "ko-mt", name: "Men's Teams", kind: "team", format: "pools", sampleRoster: "medium", seedCount: 0, status: "setup", startTime: "14:00", teamSize: 5, courts: ["A","B","C","D"] }),
      buildCompetition({ id: "ko-wt", name: "Women's Teams", kind: "team", format: "pools", sampleRoster: "small", seedCount: 0, status: "setup", startTime: "16:00", teamSize: 5, courts: ["A","B"] }),
    ];
    return buildTournament({ id: "kanto-open", name: "Kanto Open Spring Shiai", date: "2026-04-28", venue: "Tokyo Budokan", courts, status: competitionStatus(comps), competitions: comps });
  })(),
  (() => {
    const courts = ["A"];
    const comps = [
      buildCompetition({ id: "wk-yi", name: "Youth Individual", kind: "individual", gender: "M", format: "playoffs", sampleRoster: "small", seedCount: 0, status: "setup", courts: ["A"] }),
      buildCompetition({ id: "wk-bi", name: "Beginners Individual", kind: "individual", gender: "M", format: "playoffs", sampleRoster: "small", seedCount: 0, status: "setup", courts: ["A"] }),
    ];
    return buildTournament({ id: "wakaba-cup", name: "Wakaba Cup — Beginners", date: "2026-06-04", venue: "Sanshukai Dojo", courts, status: "setup", competitions: comps });
  })(),
  (() => {
    const courts = ["A", "B", "C"];
    const comps = [
      buildCompetition({ id: "ek25-mi", name: "Men's Individual", kind: "individual", gender: "M", format: "pools", sampleRoster: "medium", seedCount: 4, status: "completed", courts: ["A","B"] }),
      buildCompetition({ id: "ek25-wi", name: "Women's Individual", kind: "individual", gender: "F", format: "pools", sampleRoster: "medium", seedCount: 4, status: "completed", courts: ["A","B"] }),
      buildCompetition({ id: "ek25-mt", name: "Men's Teams", kind: "team", format: "pools", sampleRoster: "small", seedCount: 0, status: "completed", teamSize: 5, courts: ["A","B","C"] }),
    ];
    return buildTournament({ id: "european-2025", name: "European Kendo Championships 2025", date: "2025-11-08", venue: "Paris Bercy", courts, status: "completed", competitions: comps });
  })(),
];

window.SAMPLE_TOURNAMENTS = SAMPLE_TOURNAMENTS;
window.buildTournament = buildTournament;
window.buildCompetition = buildCompetition;
window.competitionStatus = competitionStatus;
window.buildBracket = buildBracket; window.advanceByes = advanceByes;
window.simulateRounds = simulateRounds; window.scheduleRound = scheduleRound;
window.buildPools = buildPools; window.simulatePools = simulatePools;
window.computeStandings = computeStandings; window.makeCompetitors = makeCompetitors;
window.standardSeedOrder = standardSeedOrder; window.nextPow2 = nextPow2;
window.poolWinners = poolWinners;

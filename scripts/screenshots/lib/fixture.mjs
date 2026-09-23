// Read-back checks: refuse to capture a fixture that lacks what the shot shows.
//
// Driving the UI is supposed to make these fields arrive by construction, but a
// flow that half-completes (a click that missed, a save that 409'd) leaves a
// plausible-looking page whose stored record is empty. That is precisely how a
// team screenshot shipped with no competitor-number chips and PW/PL of zero:
// the numbers belong to member IDs, and a record without them renders bare.
// Checking the stored record, not the DOM, is the point - the DOM can look
// right while the write never landed.
import fs from 'node:fs';
import path from 'node:path';

const compDir = (dataDir, compId) => path.join(dataDir, 'competitions', compId);

function read(dataDir, compId, file) {
  const p = path.join(compDir(dataDir, compId), file);
  return fs.existsSync(p) ? fs.readFileSync(p, 'utf8') : null;
}

// Every filled lineup position must carry a member id, not just a name: the
// number chip is resolved by id, so a name-only position renders without one.
//
// Checked lineup by lineup, through the server, which serves each team's
// round-0 lineup already parsed (domain.TeamLineup: `positions` and the
// `omitempty` `memberIds`, two parallel maps). This used to hand-parse
// lineups.yaml by indentation and compare FILE-WIDE totals, so one lineup's
// spare id could hide another's missing one. A team with no lineup yet is
// skipped (the server answers 404 by design); no lineup at all is a failure.
// Returns how many positions carry an id, for a recipe that checks the count.
export async function assertLineupIds(api, compId) {
  let ids = 0;
  let lineups = 0;
  for (const team of await api.get(`/api/competitions/${compId}/participants`)) {
    const lineup = await api.find(`/api/competitions/${compId}/teams/${team.id}/lineups/0`);
    if (!lineup) continue;
    lineups += 1;
    const memberIds = lineup.memberIds || {};
    const filled = Object.keys(lineup.positions || {}).filter((pos) => lineup.positions[pos] || memberIds[pos]);
    const bare = filled.filter((pos) => !memberIds[pos]);
    if (bare.length) {
      throw new Error(`${compId}: ${team.name}'s lineup has no member id at ${bare.join(', ')}, ` +
        `so ${bare.length} row(s) will render without a competitor-number chip`);
    }
    ids += filled.length;
  }
  if (!lineups) throw new Error(`${compId}: no team has a lineup - the lineup never saved`);
  return { ids };
}

// Minimal RFC4180 reader: the SubResults column is JSON embedded in a CSV
// field, so its quotes are doubled and a naive split destroys it. Every column
// is also full of UUIDs, which makes a pattern like "does a digit appear" true
// of an untouched file - so the checks below parse rather than grep.
//
// Exported because the recipes' own read-back gates need the same reader; it
// lived twice, and a reader that disagrees with itself is how a gate passes on
// exactly the fixture it exists to reject.
export function csvRows(text) {
  const rows = [];
  let row = [];
  let field = '';
  let quoted = false;
  for (let i = 0; i < text.length; i += 1) {
    const c = text[i];
    if (quoted) {
      if (c === '"') {
        if (text[i + 1] === '"') { field += '"'; i += 1; } else { quoted = false; }
      } else field += c;
    } else if (c === '"') quoted = true;
    else if (c === ',') { row.push(field); field = ''; }
    else if (c === '\n') { row.push(field); rows.push(row); row = []; field = ''; }
    else if (c !== '\r') field += c;
  }
  // `field !== ''` rather than a truthiness test: a trailing field of exactly
  // "0" is a value, and dropping it would shorten the last row by one column.
  if (field !== '' || row.length) { row.push(field); rows.push(row); }
  return rows;
}

// A DECIDED sub-bout must carry ippon arrays. Quick-scoring over HTTP records
// ipponsA/B as null, which shows as PW/PL of zero on every standings surface.
//
// This PARSES rather than pattern-matches, because matching kept being subtly
// wrong: a first version missed the CSV's doubled quotes entirely and passed on
// the very fixture it rejects, and its replacement could not see past a nested
// object (a bout that also went to encho carries one), so it silently skipped
// those rows too.
//
// Scope, stated because it is narrower than the name suggests: this reads the
// SubResults column, which only a TEAM competition fills. An individual
// competition keeps its ippons in top-level CSV columns and, thanks to
// `omitempty` on IpponsA/B (state/models.go), never writes a null for them
// anywhere - so there is nothing here for this check to catch. Guard an
// individual fixture at the recipe with its own check.
//
// Null ippons alone are not the fault either: kachinuki appends the next
// pairing as play proceeds, so a bout nobody has fought yet legitimately has
// none. What cannot come from the editor is a bout with a WINNER and no points.
export function assertBoutPoints(dataDir, compId) {
  const csv = read(dataDir, compId, 'pool-matches.csv');
  if (!csv) throw new Error(`${compId}: no pool-matches.csv to check`);

  const rows = csvRows(csv);
  const header = rows.shift() || [];
  const col = header.indexOf('SubResults');
  if (col === -1) throw new Error(`${compId}: pool-matches.csv has no SubResults column`);

  let decidedWithoutPoints = 0;
  let subBouts = 0;
  for (const row of rows) {
    const cell = (row[col] || '').trim();
    if (!cell) continue;
    let subs;
    try {
      subs = JSON.parse(cell);
    } catch {
      throw new Error(`${compId}: SubResults is not valid JSON, so this check cannot run`);
    }
    for (const sub of subs || []) {
      subBouts += 1;
      const decided = String(sub.winner || '').trim() !== '';
      const noPoints = sub.ipponsA == null && sub.ipponsB == null;
      if (decided && noPoints) decidedWithoutPoints += 1;
    }
  }
  if (decidedWithoutPoints) {
    throw new Error(`${compId}: ${decidedWithoutPoints} of ${subBouts} sub-bout(s) name a ` +
      'winner but carry null ippons - they were quick-scored rather than entered through ' +
      'the editor, so points will render as zero');
  }
  return { subBouts };
}

// The individual-competition counterpart of assertBoutPoints, which cannot help
// there: an individual match has no SubResults, it keeps its points in the
// top-level IpponsA/IpponsB columns as pipe-joined waza letters (state/pools.go
// writes them with domain.IpponFieldSeparator, and an empty cell means none).
//
// Same shape of fault, same tell: a row naming a WINNER with no points on
// either side did not come from the editor. Every legitimate way to win records
// something - a struck ippon its letter, a default win its maru (per FIK Art.
// 32, domain.DefaultWinIppons), a judges' decision its Ht mark - and the losing
// side keeps whatever it had already struck. So "winner, but both cells empty"
// is unreachable from the UI and means the result was written over the API.
export function assertIndividualBoutPoints(dataDir, compId) {
  const csv = read(dataDir, compId, 'pool-matches.csv');
  if (!csv) throw new Error(`${compId}: no pool-matches.csv to check`);

  const rows = csvRows(csv);
  const header = rows.shift() || [];
  const need = ['Winner', 'IpponsA', 'IpponsB'];
  const at = {};
  for (const name of need) {
    at[name] = header.indexOf(name);
    if (at[name] === -1) throw new Error(`${compId}: pool-matches.csv has no ${name} column`);
  }

  let decidedWithoutPoints = 0;
  let decided = 0;
  for (const row of rows) {
    if (!String(row[at.Winner] || '').trim()) continue;
    decided += 1;
    const points = String(row[at.IpponsA] || '').trim() || String(row[at.IpponsB] || '').trim();
    if (!points) decidedWithoutPoints += 1;
  }
  if (decidedWithoutPoints) {
    throw new Error(`${compId}: ${decidedWithoutPoints} of ${decided} decided match(es) name a ` +
      'winner but carry no ippons on either side - they were written over the API rather than ' +
      'entered through the editor, so scores will render blank');
  }
  return { decided };
}

// A hantei (judges' decision) verdict is recorded as an ippon mark, "Ht", in
// the WINNER's ippon list - not as a separate flag and never as a centre mark
// (domain.AppendHantei; the centre is a closed set of vs/X/(E)/(DH)). So the
// stored tell is the literal token in IpponsA or IpponsB.
//
// Worth asserting because a capture whose whole subject is the Ht mark looks
// entirely healthy without one: the page renders, the scores render, and only
// a reader comparing it against the caption would notice the mark is missing.
export function assertHanteiRecorded(dataDir, compId) {
  const csv = read(dataDir, compId, 'pool-matches.csv');
  if (!csv) throw new Error(`${compId}: no pool-matches.csv to check`);

  const rows = csvRows(csv);
  const header = rows.shift() || [];
  const cols = ['IpponsA', 'IpponsB'].map((name) => {
    const at = header.indexOf(name);
    if (at === -1) throw new Error(`${compId}: pool-matches.csv has no ${name} column`);
    return at;
  });

  // Ippons are pipe-joined, so compare tokens rather than substring-matching:
  // a bare "contains Ht" would also accept a name or a waza letter pair.
  const marked = rows.some((row) => cols.some((at) => String(row[at] || '')
    .split('|')
    .some((token) => token.trim() === 'Ht')));
  if (!marked) {
    throw new Error(`${compId}: no match carries an Ht mark, so the capture would show no ` +
      'hantei result - the editor never recorded one');
  }
}

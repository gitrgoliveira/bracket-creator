// Audit rows: what a journey observed of each action x clumsy variant, the raw
// material for the review's audit table. Nothing here judges a row: judgement
// is formed by eye, from the screenshots.
//
// Every row is printed as an `AUDIT {json}` line, so the run log carries the
// table (people grep run logs for it), and kept for flush(), which writes the
// rows to <journey folder>/audit.json beside the journey's screenshots
// (fixtures/shots.mjs owns the folder). Call flush() in afterAll, so a journey
// that fails part way still leaves the rows it recorded.
import fs from 'node:fs';
import path from 'node:path';
import { fileURLToPath } from 'node:url';
import { journeyDir } from './shots.mjs';

// spec file -> the audits it made, so the console policy (fixtures/test.mjs)
// can add its warning rows to the audit of the test's own file. The file is
// read off the call stack: auditLog is always called while a spec file loads.
const bySpec = new Map();
function callingSpec() {
  const m = (new Error().stack || '').match(/((?:file:\/\/)?\/[^\s():]+\.spec\.mjs)/);
  if (!m) return null;
  return m[1].startsWith('file://') ? fileURLToPath(m[1]) : m[1];
}
export const auditsFor = (specFile) => bySpec.get(specFile) || [];

export function auditLog(journey) {
  const file = path.join(journeyDir(journey), 'audit.json');
  const rows = [];
  const record = (row) => {
    rows.push(row);
    console.log(`AUDIT ${JSON.stringify(row)}`);
  };
  const flush = () => {
    let prior = [];
    try { prior = JSON.parse(fs.readFileSync(file, 'utf8')); } catch { prior = []; }
    // A journey re-run replaces its own earlier rows (by their `journey`
    // field) and keeps the others: one folder can hold several journeys' rows.
    const journeys = new Set(rows.map((r) => r.journey));
    const kept = prior.filter((r) => !journeys.has(r.journey));
    fs.mkdirSync(path.dirname(file), { recursive: true });
    fs.writeFileSync(file, JSON.stringify([...kept, ...rows], null, 2));
  };
  const audit = { record, flush, rows, file };
  const spec = callingSpec();
  if (spec) bySpec.set(spec, [...auditsFor(spec), audit]);
  return audit;
}

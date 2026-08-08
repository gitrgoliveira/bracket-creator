#!/usr/bin/env node
/*
 * record-kachinuki-demo.cjs — regenerate docs/videos/kachinuki-demo.webm.
 *
 * The clip is a ~40s walkthrough of the kachinuki score editor, recorded from
 * a real browser. Re-run it whenever the score editor's look changes so the
 * doc video stays current (it has gone stale before — dash-vs-vs middles, the
 * overtime pill moving, etc.).
 *
 * WHAT IT SHOWS (chapters — keep these in sync with the numbered list under the
 * <video> in docs/user-guide/organisers/team-tournaments.md; the timestamps
 * shift a little between runs, so re-read the printed CHAPTERS and update the
 * prose if they move):
 *   1  ~0:00  Winner stays on ............ every fought bout reads "vs" centre
 *   2  ~0:08  Knockout tie -> Encho ...... tie holds End back; Encho, marked "(E)"
 *   3  ~0:15  League draw ................ same tie ends as a draw, marked "X"
 *   4  ~0:22  Reopen ..................... completed encounter reopens, then ends
 *                                          again via the audit-reason prompt
 *
 * PREREQUISITES
 *   1. A mobile-app server on a FRESH data dir (so the seed below is clean):
 *        make go/build
 *        TOURNAMENT_DATA_DIR=$(mktemp -d) PORT=8099 ./bin/bracket-creator mobile-app &
 *   2. Playwright + a chromium build. From the repo root, once:
 *        npm i -D playwright-core && npx playwright-core install chromium
 *      (or `playwright` instead of `playwright-core`). If Playwright lives
 *      elsewhere, point PLAYWRIGHT_CORE at its package dir.
 *
 * RUN
 *   node scripts/record-kachinuki-demo.cjs
 *   # overrides: BASE (server url), PW (admin password), OUT (output webm)
 *
 * The output overwrites docs/videos/kachinuki-demo.webm. Then run
 * `make docs/build` and, if the chapter times moved, edit the prose timestamps.
 *
 * WHY THE ODD BITS
 *   - It scores with the KEYBOARD (Shift+M = an Aka ippon): the editor's global
 *     key handler is the most robust way to score without hunting ippon buttons.
 *   - It injects CSS to un-cap the modal height (kachinuki is always the
 *     "compact", internally-scrolled layout) so the whole editor is on screen.
 *   - Ending a REOPENED match requires a reason, so endMatch() also confirms the
 *     reopen-reason prompt when it appears.
 */
'use strict';
const fs = require('fs');
const path = require('path');
const os = require('os');

const BASE = process.env.BASE || 'http://localhost:8099';
const PW = process.env.PW || 'pw';
const OUT = process.env.OUT || path.resolve(__dirname, '..', 'docs', 'videos', 'kachinuki-demo.webm');
const VW = 820, VH = 1120;
const EXPAND = `.modal-backdrop{align-items:flex-start!important;padding:8px 0!important}` +
  `.editor-modal--compact{max-height:none!important;height:auto!important}` +
  `.team-bouts-scroll{max-height:none!important;overflow:visible!important}`;

function loadChromium() {
  for (const m of ['playwright', 'playwright-core', process.env.PLAYWRIGHT_CORE].filter(Boolean)) {
    try { return require(m).chromium; } catch (_) { /* try next */ }
  }
  console.error('Playwright not found. From the repo root:\n' +
    '  npm i -D playwright-core && npx playwright-core install chromium\n' +
    'or set PLAYWRIGHT_CORE=/path/to/playwright-core');
  process.exit(1);
}

async function api(method, p, body, auth = true) {
  const res = await fetch(BASE + p, {
    method,
    headers: { 'Content-Type': 'application/json', ...(auth ? { 'X-Tournament-Password': PW } : {}) },
    body: body != null ? JSON.stringify(body) : undefined,
  });
  const text = await res.text();
  if (!res.ok) throw new Error(`${method} ${p} -> ${res.status} ${text.slice(0, 120)}`);
  return text.trim() ? JSON.parse(text) : null;
}

// Seed one 2-team kachinuki competition, started, ready to score.
async function seedComp(name, format, teamA, teamB) {
  const comp = await api('POST', '/api/competitions', {
    name, kind: 'team', format, teamSize: 5, teamMatchType: 'kachinuki',
    courts: ['A'], roundRobin: true,
  });
  const cid = comp.id;
  await api('POST', `/api/competitions/${cid}/participants`, {
    players: [{ name: teamA, dojo: 'North' }, { name: teamB, dojo: 'South' }],
  });
  const parts = await api('GET', `/api/competitions/${cid}/participants`);
  const [a, b] = [parts[0].id, parts[1].id];
  const lineup = (pre) => ({ positions: {
    senpo: `${pre}-S`, jiho: `${pre}-J`, chuken: `${pre}-C`, fukusho: `${pre}-F`, taisho: `${pre}-T`,
  } });
  await api('PUT', `/api/competitions/${cid}/teams/${a}/lineups/0`, lineup(teamA));
  await api('PUT', `/api/competitions/${cid}/teams/${b}/lineups/0`, lineup(teamB));
  await api('POST', `/api/competitions/${cid}/generate-draw`);
  await api('POST', `/api/competitions/${cid}/start`);
  return cid;
}

async function seed() {
  try {
    await api('POST', '/api/tournament',
      { name: 'Kachinuki Demo', date: '01-01-2026', courts: ['A'], password: PW }, false);
  } catch (e) { console.warn('tournament create skipped:', e.message, '(is the data dir fresh?)'); }
  const ko = await seedComp('KO Demo', 'playoffs', 'Aka', 'Shiro');
  const lg = await seedComp('League Demo', 'league', 'Kita', 'Minami');
  return { ko, lg };
}

(async () => {
  const chromium = loadChromium();
  const { ko, lg } = await seed();

  const dir = fs.mkdtempSync(path.join(os.tmpdir(), 'kachi-vid-'));
  const browser = await chromium.launch({ args: ['--no-sandbox'] });
  const ctx = await browser.newContext({
    viewport: { width: VW, height: VH },
    recordVideo: { dir, size: { width: VW, height: VH } },
  });
  const page = await ctx.newPage();
  await page.addInitScript(() => {
    try { localStorage.setItem('bc_authed', 'true'); localStorage.setItem('bc_password', 'pw'); } catch (_) {}
  });

  const marks = []; const t0 = Date.now();
  const mark = (l) => marks.push([((Date.now() - t0) / 1000).toFixed(1), l]);
  const inject = () => page.addStyleTag({ content: EXPAND }).catch(() => {});
  const openScore = async (cid) => {
    await page.goto(`${BASE}/admin/competition/${cid}/scores`, { waitUntil: 'domcontentloaded' });
    await page.waitForTimeout(900);
    await page.locator('button.score-btn:has-text("Score"), button:has-text("Correct")').first().click();
    await page.waitForTimeout(750); await inject();
  };
  const startIfNeeded = async () => {
    const sm = page.locator('button:has-text("Start match")');
    if (await sm.count()) { await sm.first().click().catch(() => {}); await page.waitForTimeout(900); }
  };
  const focus = () => page.locator('.sb-match, .team-summary').first().click({ timeout: 2000 }).catch(() => {});
  const aka = async (n = 1) => { await focus(); for (let i = 0; i < n; i++) { await page.keyboard.press('Shift+M'); await page.waitForTimeout(600); } };
  const click = async (sel, ms = 1100) => { await page.locator(sel).first().click({ timeout: 5000 }); await page.waitForTimeout(ms); };
  const clickT = async (sel, ms = 1100) => { await page.locator(sel).first().click({ timeout: 5000 }).catch(() => {}); await page.waitForTimeout(ms); };
  const endMatch = async () => {
    const b = page.locator('[data-testid="kachinuki-end-match-button"]');
    await b.click({ timeout: 5000 }).catch(() => {}); await page.waitForTimeout(650);
    await b.click({ timeout: 5000 }).catch(() => {}); await page.waitForTimeout(900);
    const conf = page.locator('button:has-text("Confirm")'); // reopen-reason prompt
    if (await conf.count()) { await conf.first().click().catch(() => {}); await page.waitForTimeout(1300); }
    else { await page.waitForTimeout(600); }
  };

  try {
    // 1: winner stays on (KO) — score, Record, repeat; "vs" middles appear
    await openScore(ko); mark('P1 winner-stays');
    await aka(2); await click('button:has-text("Record bout")');
    await aka(2); await click('button:has-text("Record bout")');
    await page.waitForTimeout(1300);
    // 2: knockout tie -> Encho (bout 3 is fresh/0-0)
    mark('P2 knockout tie -> encho');
    await click('[data-testid="scoring-modal-tie-button"]', 1500);
    await clickT('button:has-text("Encho")', 1300);
    await aka(1); await page.waitForTimeout(800);
    await endMatch();
    // 3: drawn encounter in a league
    mark('P3 league draw');
    await openScore(lg); await startIfNeeded();
    await clickT('[data-testid="scoring-modal-tie-button"]', 1400);
    await endMatch();
    // 4: reopen a completed encounter, end again (asks for a reason)
    mark('P4 reopen');
    await openScore(ko); await page.waitForTimeout(1200);
    await clickT('[data-testid="kachinuki-reopen-button"]', 1600); // reopen closes the modal
    await openScore(ko); await page.waitForTimeout(900);           // re-open the now-running match
    await endMatch();
    mark('end'); await page.waitForTimeout(700);
  } catch (e) { console.error('FLOW ERROR:', e.message); }

  const src = await page.video().path();
  await ctx.close(); await browser.close(); // finalises the webm
  fs.mkdirSync(path.dirname(OUT), { recursive: true });
  fs.copyFileSync(src, OUT);
  fs.rmSync(dir, { recursive: true, force: true });
  console.log('WROTE', OUT);
  console.log('CHAPTERS', JSON.stringify(marks));
  console.log('If the chapter times moved, update the numbered list under the <video> in',
    'docs/user-guide/organisers/team-tournaments.md');
})().catch((e) => { console.error('FATAL', e.message); process.exit(1); });

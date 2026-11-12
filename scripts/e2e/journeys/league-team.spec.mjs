// TEAM LEAGUE journeys: the desk organiser and the court operator running a
// league (everyone fights everyone, one table, the podium from the table).
// Everything is seeded through the interface (fixtures/league.mjs seedLeague).
//
// Not serial: each test signs in (or creates the tournament when this file's
// server has none), seeds a competition of its own on a shiaijo of its own, and
// keeps its own folder of screenshots and audit rows, so one failing test or
// one test.fixme hides nothing else here.
//
// What is asserted is what the seeding path and the docs promise; how the rest
// reads is recorded as AUDIT rows and judged from the screenshots.
//
// A defect step follows one shape: the steps before it run against today's
// app and RECORD what it does (never asserting the defect), then a
// `test.fixme(true, '<finding>: ...')` in the body stops the test there, and
// what follows asserts the FIXED behaviour the league review's design names
// (the fixture's TEXT holds its wording). The flows before the fixme are
// written so they run on the fixed app as well (a warning the fix adds is
// answered when it appears), so the fix only removes the fixme line.
//
// The scripted tie (`twoWayTie`): four teams of three, A and B each beat C and
// D by two bouts to none with one hikiwake, A v B is three hikiwake, C beats D
// the same way. A and B end level on every criterion (W2 L0 T1 IV4 IL0 IT5
// PW4 PL0) for 1st and 2nd: a consequential tie the organiser decides (Run
// tie-breaker or Accept shared ranks). The ring (`ringTie`): three teams beat
// each other in a ring and all beat the fourth by the same scoreline, a tie
// for 1st to 3rd. Expected figures are tallied from the scripts, never read
// back from the app.
import crypto from 'node:crypto';
import fs from 'node:fs';
import os from 'node:os';
import path from 'node:path';
import zlib from 'node:zlib';
import { test, expect } from '../fixtures/test.mjs';
import { OPERATOR_DEVICE, PUBLIC_DEVICE } from '../fixtures/devices.mjs';
import { journeyShots } from '../fixtures/shots.mjs';
import { auditLog } from '../fixtures/audit.mjs';
import { enterTournament } from '../fixtures/setup.mjs';
import { createCompetition } from '../fixtures/wizard.mjs';
import { generateDraw, pasteRoster, startCompetition } from '../fixtures/competition.mjs';
import { EDITOR, awardIppon, clearMarks, finishButton, finishMatch, running, settled, startMatch } from '../fixtures/scoring.mjs';
import { boutIppon, boutRow, ensureTie, finishBtn, finishTeam, rowMiddle, rowSlots, rowTie, sheetState, startUpNextTeam, teamTotals } from '../fixtures/team.mjs';
import { completedRowFor, courtDone, editorIdentity, inlineEditor, openShiaijo, sides, startUpNext, upNextCard } from '../fixtures/shiaijo.mjs';
import {
  awardBoutIppon, closeLineupPanel, endMatch, endMatchButton, openLineupFromUpNext, reopenButton, startUpNextKachinuki, typeLineup,
} from '../fixtures/kachinuki.mjs';
import {
  TEXT, acceptButton, acceptSharedRanks, acceptedLines, answerConfirm, chooseTiebreakTop, confirmDialog, drawnBoutNotices,
  leagueMatrix, leagueRowFigures, leagueRows, openLeague, openLeagueSettings, openMatrixCell, openNumberedRow,
  openViewerAwards, openViewerLeague, readBanner, readChusen, readChusenRecorded, readConfirm, readLeagueSettings,
  readLeagueTable, readNumberedRows, readPodium, requestAcceptSharedRanks, runTiebreaker, saveSettings, seedLeague,
  undoControl, undoSharedRanks, playEncounter, escape, squash,
} from '../fixtures/league.mjs';
import { chooseBackup, confirmWithPassword, openBackupPage, planRow, readZip, restoreButton } from '../fixtures/organiser.mjs';

// Enough shiaijo for every test here to have one of its own (A to J).
const LEAGUE_CUP = { name: 'League Cup', courts: 10 };
const LONG = 900_000;
// The tournament password enterTournament signs in with (fixtures/setup.mjs).
const PASSWORD = 'e2e-operator';

// Four teams of three, four dojos: six encounters.
const FOUR_TEAMS = (tag) => [
  [`${tag} Hayabusa`, 'Kita Dojo'], [`${tag} Inoshishi`, 'Minami Dojo'],
  [`${tag} Kitsune`, 'Higashi Dojo'], [`${tag} Tanuki`, 'Nishi Dojo'],
];
// The same four as roles: A B C D.
const rolesOf = (tag) => {
  const [A, B, C, D] = FOUR_TEAMS(tag).map(([t]) => t);
  return { A, B, C, D };
};

// A recorder per test, flushed by afterAll to the audit.json in the test's own
// folder (the one its screenshots go to).
const audits = [];
function recorder(journey, folder) {
  const log = auditLog(folder);
  audits.push(log);
  return { record: (row) => log.record({ journey, ...row }) };
}

const pairKey = (a, b) => [a, b].sort().join(' | ');
const samePair = (row, a, b) => pairKey(row.shiro, row.aka) === pairKey(a, b);

// ------------------------------------------------------------ the scripts --

const zero = () => ({ W: 0, L: 0, T: 0, IV: 0, IL: 0, IT: 0, PW: 0, PL: 0 });

// A league's scripted encounters: `entries` are [x, y, bouts], where `bouts`
// names each bout's winner (a team name, one men) or null (hikiwake). The
// script answers which bouts to tap for a pairing as the draw sided it, and
// tallies the figures the table must show (one point per won bout; an
// encounter is won on bouts, then points, else drawn).
function leagueScript(entries) {
  const plan = new Map(entries.map(([x, y, bouts]) => [pairKey(x, y), { x, y, bouts }]));
  const get = (a, b) => {
    const e = plan.get(pairKey(a, b));
    if (!e) throw new Error(`no scripted encounter for ${a} v ${b}`);
    return e;
  };
  return {
    pairs: () => [...plan.values()].map(({ x, y }) => [x, y]),
    // The bouts as taps for a row sided { shiro, aka }: 'shiro' | 'aka' | 'tie'.
    boutsFor: ({ shiro, aka }) => get(shiro, aka).bouts.map((w) => (w === null ? 'tie' : w === shiro ? 'shiro' : 'aka')),
    isLevel: (a, b) => get(a, b).bouts.every((w) => w === null),
    // The scripted bouts of a pairing, by winner (a copy).
    bouts: (a, b) => [...get(a, b).bouts],
    // A correction: replace one encounter's bouts.
    set: (a, b, bouts) => { get(a, b).bouts = bouts; },
    tally() {
      const t = {};
      for (const { x, y, bouts } of plan.values()) {
        t[x] ||= zero();
        t[y] ||= zero();
        const iv = { [x]: 0, [y]: 0 };
        for (const w of bouts) {
          if (w === null) { t[x].IT += 1; t[y].IT += 1; continue; }
          const l = w === x ? y : x;
          iv[w] += 1;
          t[w].IV += 1; t[l].IL += 1; t[w].PW += 1; t[l].PL += 1;
        }
        if (iv[x] === iv[y]) { t[x].T += 1; t[y].T += 1; } else {
          const [w, l] = iv[x] > iv[y] ? [x, y] : [y, x];
          t[w].W += 1; t[l].L += 1;
        }
      }
      return t;
    },
  };
}

// A and B level for 1st and 2nd; C 3rd, D 4th.
const twoWayTie = ({ A, B, C, D }) => leagueScript([
  [A, B, [null, null, null]],
  [A, C, [A, A, null]], [A, D, [A, A, null]],
  [B, C, [B, B, null]], [B, D, [B, B, null]],
  [C, D, [C, C, null]],
]);

// A beats B, B beats C, C beats A, all beat D: A, B, C level for 1st to 3rd.
const ringTie = ({ A, B, C, D }) => leagueScript([
  [A, B, [A, A, null]], [B, C, [B, B, null]], [C, A, [C, C, null]],
  [A, D, [A, A, null]], [B, D, [B, B, null]], [C, D, [C, C, null]],
]);

// A first, B second, C and D level for 3rd and 4th (W0 L2 T1 IV0 IL4 IT5 PW0
// PL4): A beats B by three bouts, A and B each beat C and D by two with one
// hikiwake, C v D is three hikiwake.
const sharedThird = ({ A, B, C, D }) => leagueScript([
  [A, B, [A, A, A]],
  [A, C, [A, A, null]], [A, D, [A, A, null]],
  [B, C, [B, B, null]], [B, D, [B, B, null]],
  [C, D, [null, null, null]],
]);

// The four corrections that take C and D from a shared 3rd to a shared 1st,
// in order. The scorelines differ so that no table between them holds a tie
// (tallied by hand, re-tallied by the script at each step):
//   1. A v C won by C 2-0: A W2 L1 IV5, B W2 L1 IV4, C W1, D W0.
//   2. A v D won by D 1-0: B W2, C W1 L1 IV2, D W1 L1 IV1, A W1 L2.
//   3. B v C won by C 1-0: C W2, D W1 L1, A W1 L2 IV3, B W1 L2 IV2.
//   4. B v D won by D 2-0: C and D level (W2 L0 T1 IV3 IL0 IT6) for 1st, A 3rd, B 4th.
const sharedThirdToFirst = ({ A, B, C, D }) => [
  [A, C, [C, C, null]],
  [A, D, [D, null, null]],
  [B, C, [C, null, null]],
  [B, D, [D, D, null]],
];

// ----------------------------------------------------------- spec helpers --

// Score the bouts of a TEAM encounter on the sheet `ed` (overlay or inline),
// starting it when the overlay still offers Start match. Does not Finish.
async function scoreBouts(ed, bouts) {
  const page = ed.page();
  await expect(boutRow(ed, 1)).toBeVisible();
  const start = ed.locator('button').filter({ hasText: /^Start match$/ }).first();
  if (await start.isVisible()) await startMatch(page);
  await running(page, ed);
  for (const [i, b] of bouts.entries()) {
    if (b === 'tie') await ensureTie(ed, i + 1);
    else await boutIppon(ed, i + 1, b, 'M');
  }
}

// What the sheet says around a LEVEL encounter (F5: a league has no
// mid-encounter daihyosen, and the line D5 decides). Read before Finish.
async function readLevelSheet(ed) {
  return {
    totals: await teamTotals(ed),
    sheet: await sheetState(ed, 3),
    daihyosenOffer: await ed.getByTestId('scoring-modal-daihyosen-button').count(),
    daihyosenControls: await ed.locator('.daihyosen-controls').count(),
    mentionsDaihyosen: /daihyosen|representative/i.test(await ed.innerText()),
  };
}

// The regular (numbered 1..6) row of x v y that has not been played yet.
async function regularRow(page, x, y) {
  const rows = await readNumberedRows(page);
  const row = rows.find((r) => r.num <= 6 && samePair(r, x, y));
  expect(row, `a numbered row for ${x} v ${y}`).toBeTruthy();
  return row;
}

// Play the encounter x v y from its numbered row on Pools as scripted, and
// return once the table counts it. `onLevel(ed)` runs before Finish when the
// encounter is level (F5). Returns the Finish label the overlay offered.
async function playFromRow(page, script, [x, y], playedAfter, { onLevel } = {}) {
  const row = await regularRow(page, x, y);
  const ed = await openNumberedRow(page, { num: row.num });
  await scoreBouts(ed, script.boutsFor(row));
  if (onLevel && script.isLevel(x, y)) await onLevel(ed);
  const label = squash(await finishBtn(ed).innerText());
  await finishTeam(ed);
  await expect.poll(async () => (await readLeagueTable(page)).played, { message: `${x} v ${y} counted` }).toBe(playedAfter);
  return { pair: { shiro: row.shiro, aka: row.aka }, finishLabel: label };
}

// Play every scripted encounter not in `done` from the numbered rows.
async function playRestFromRows(page, script, done = new Set(), opts) {
  const out = [];
  for (const [x, y] of script.pairs()) {
    if (done.has(pairKey(x, y))) continue;
    out.push(await playFromRow(page, script, [x, y], done.size + 1, opts));
    done.add(pairKey(x, y));
  }
  return out;
}

// The row of the tie-breaker bout(s) between `teams` still to be fought: the
// rows numbered past the regular ones whose two sides are both in `teams`.
async function tiebreakRows(page, regular, teams) {
  const set = new Set(teams);
  return (await readNumberedRows(page)).filter((r) => r.num > regular && set.has(r.shiro) && set.has(r.aka));
}

// The expected figures of `team`, as the row reader names them.
const figuresOf = (tally, team) => tally[team];

// Every row's name -> rank, as shown.
const ranksShown = async (page) => Object.fromEntries((await leagueRows(page)).map((r) => [r.name, r.rank]));

// The answer to the next POST .../league-tiebreak/finalize, armed before the
// tap that sends it.
function finalizeAnswer(page) {
  const answered = page.waitForResponse((r) => r.request().method() === 'POST'
    && /^\/api\/competitions\/[^/]+\/league-tiebreak\/finalize$/.test(new URL(r.url()).pathname))
    .then(async (r) => ({ status: r.status(), ok: r.ok(), body: await r.json().catch(() => null) }));
  answered.catch(() => {});
  return answered;
}

// Close an overlay score editor left open on a finished match.
async function closeEditor(page) {
  const ed = page.locator(EDITOR);
  await page.keyboard.press('Escape');
  await expect(ed).toHaveCount(0);
}

// What an editor opened on a FINISHED match offers: its head pill, its
// primary button, and whether hantei or the draw toggle are there.
async function readFinishedEditor(ed) {
  return {
    pill: squash(((await ed.locator('.editor-head-pill').allInnerTexts())[0]) || ''),
    primary: squash(((await ed.locator('.score-nav__actions button.btn--primary').allInnerTexts())[0]) || ''),
    hanteiRow: await ed.getByTestId('scoring-modal-hantei-row').count(),
    drawToggle: (await ed.getByTestId('scoring-modal-mark-draw').count())
      ? { text: squash(await ed.getByTestId('scoring-modal-mark-draw').innerText()), disabled: await ed.getByTestId('scoring-modal-mark-draw').isDisabled() }
      : null,
  };
}

// Save the correction an editor `ed` holds (Save correction, then the reason
// prompt's Confirm), answering the finished-league warning the fix adds when
// it appears. Returns { warned, warning, statuses } once a /score write of the
// correction has been answered with a success status.
async function saveCorrection(page, ed) {
  const statuses = [];
  const onRes = (r) => {
    if (r.request().method() === 'PUT' && /\/matches\/[^/]+\/score$/.test(new URL(r.url()).pathname)) statuses.push(r.status());
  };
  page.on('response', onRes);
  try {
    await ed.getByRole('button', { name: 'Save correction', exact: true }).tap();
    const reason = page.locator('.reason-prompt').last();
    await expect(reason).toBeVisible();
    await reason.getByRole('button', { name: 'Confirm', exact: true }).tap();
    const warned = await confirmDialog(page).waitFor({ state: 'visible', timeout: 5_000 }).then(() => true, () => false);
    let warning = null;
    if (warned) {
      warning = await readConfirm(page);
      await answerConfirm(page, TEXT.changeAnyway);
    }
    await expect.poll(() => statuses.some((s) => s >= 200 && s < 300), { message: 'the correction was saved' }).toBe(true);
    await settled(page);
    return { warned, warning, statuses: [...statuses] };
  } finally {
    page.off('response', onRes);
  }
}

// Correct the played encounter x v y from its numbered row on Pools so its
// bouts read `to` (winners by name, null for hikiwake), saving through
// saveCorrection, and keep the script in step. Returns saveCorrection's answer
// with the pair as the draw sided it.
async function correctFromRow(page, script, [x, y], to) {
  const row = await regularRow(page, x, y);
  const ed = await openNumberedRow(page, { num: row.num });
  const sideOf = (team) => (team === row.shiro ? 'shiro' : 'aka');
  const from = script.bouts(x, y);
  for (const [i, want] of to.entries()) {
    if (from[i] === want) continue;
    const bout = boutRow(ed, i + 1);
    if (from[i] === null) {
      if ((await rowMiddle(bout).innerText()).trim() === 'X') await rowTie(bout).tap();
      await expect(rowMiddle(bout)).not.toHaveText('X');
    } else {
      await clearMarks(rowSlots(bout, sideOf(from[i])));
    }
    if (want === null) await ensureTie(ed, i + 1);
    else await boutIppon(ed, i + 1, sideOf(want), 'M');
  }
  const saved = await saveCorrection(page, ed);
  script.set(x, y, to);
  return { pair: { shiro: row.shiro, aka: row.aka }, ...saved };
}

// A second operator device, signed in.
async function secondOperator(browser, baseURL) {
  const ctx = await browser.newContext({ ...OPERATOR_DEVICE, baseURL });
  const page = await ctx.newPage();
  await enterTournament(page, LEAGUE_CUP);
  return { ctx, page };
}

// The public Awards tab of `id`, read on a phone.
async function publicPodium(browser, baseURL, id, shot, name) {
  const ctx = await browser.newContext({ ...PUBLIC_DEVICE, baseURL });
  try {
    const phone = await ctx.newPage();
    await openViewerAwards(phone, id);
    const podium = await readPodium(phone);
    return { podium, screenshot: await shot(phone, name, { fullPage: true }) };
  } finally {
    await ctx.close();
  }
}

// Seed a team league of `teams` on `court` and play its six encounters from
// the numbered rows; returns its id once the tie-break banner is up.
async function seedAndPlay(page, { name, court, teams, script, twoThirdPlaces }) {
  const id = await seedLeague(page, {
    name, kind: 'team', entrants: teams, teamSize: 3, teamMatchType: 'fixed', courts: [court], twoThirdPlaces,
  });
  await openLeague(page, id);
  await playRestFromRows(page, script);
  await expect.poll(async () => (await readBanner(page)).shown, { message: 'the tie-break banner' }).toBe(true);
  return id;
}

const expectTwoWayBanner = async (page, A, B) => {
  const banner = await readBanner(page);
  expect(banner.groups).toHaveLength(1);
  expect([...banner.groups[0].teams].sort()).toEqual([A, B].sort());
  expect(banner.groups[0]).toMatchObject({ min: 1, max: 2 });
  return banner;
};

test.describe('league-team', () => {
  test.afterAll(() => audits.forEach((a) => a.flush()));

  test('smoke: a team league is seeded through the wizard and its table reads on Pools and the public page', async ({ page, browser, baseURL }) => {
    test.setTimeout(240_000);
    const shot = journeyShots('league-team/smoke');
    const { record } = recorder('smoke', 'league-team/smoke');
    const teams = FOUR_TEAMS('S');
    let id;

    await test.step('create a team league in the wizard and read its league settings', async () => {
      await enterTournament(page, LEAGUE_CUP);
      id = await createCompetition(page, {
        name: 'Smoke League', kind: 'team', format: 'league', teamSize: 3, teamMatchType: 'fixed', courts: ['A'], numberPrefix: 'S',
      });
      await openLeagueSettings(page, id);
      const before = await readLeagueSettings(page);
      record({ step: 'settings', action: 'read before the draw', ...before });
      await shot(page, 'settings-before-draw', { fullPage: true });
      expect(before.tiebreakTop.shown).toBe(true);
      expect(before.tiebreakTop.options.map((o) => o.label)).toEqual(['Top 3', 'Top 4']);
      expect(before.tiebreakTop.active).toBe('Top 3');
      expect(before.tiebreakTop.locked).toBe(false);
      expect(before.twoThirdPlaces.shown).toBe(true);
      expect(before.twoThirdPlaces.locked).toBe(false);

      // A choice is kept across a reload, and put back.
      await chooseTiebreakTop(page, 4);
      await saveSettings(page, id);
      await openLeagueSettings(page, id);
      expect((await readLeagueSettings(page)).tiebreakTop.active).toBe('Top 4');
      await chooseTiebreakTop(page, 3);
      await saveSettings(page, id);
    });

    await test.step('paste four teams, draw and start', async () => {
      await pasteRoster(page, id, teams);
      await generateDraw(page, id);
      await openLeagueSettings(page, id);
      const locked = await readLeagueSettings(page);
      record({ step: 'settings', action: 'read once drawn', ...locked });
      expect(locked.tiebreakTop.locked).toBe(true);
      expect(locked.twoThirdPlaces.locked).toBe(true);
      await startCompetition(page, id);
    });

    let rows;
    await test.step('Pools: the league table, its numbered rows, no banner', async () => {
      await openLeague(page, id);
      const table = await readLeagueTable(page);
      rows = await readNumberedRows(page);
      record({
        step: 'pools', action: 'read before any result', table, numberedRows: rows,
        matrixShown: await leagueMatrix(page).count(), banner: await readBanner(page),
      });
      await shot(page, 'pools-before-results', { fullPage: true });
      expect(table.heading).toBe('Standings');
      expect(table).toMatchObject({ played: 0, total: 6 });
      expect(rows).toHaveLength(6);
      const named = new Set(rows.flatMap((r) => [r.shiro, r.aka]));
      expect([...named].sort()).toEqual(teams.map(([t]) => t).sort());
      expect((await readBanner(page)).shown).toBe(false);
    });

    let first;
    await test.step('score the first encounter from its numbered row', async () => {
      first = rows[0];
      const ed = await openNumberedRow(page, { shiro: first.shiro, aka: first.aka });
      await shot(page, 'encounter-1-opened');
      const totals = await playEncounter(ed, ['shiro', 'shiro', 'tie']);
      record({ step: 'pools', action: 'encounter 1 from the numbered row', pair: first, totals });
      await expect.poll(async () => (await readLeagueTable(page)).played).toBe(1);
      const table = await readLeagueTable(page);
      record({ step: 'pools', action: 'read after encounter 1', table, numberedRows: await readNumberedRows(page) });
      await shot(page, 'pools-after-encounter-1', { fullPage: true });
      expect(table.rows).toHaveLength(4);
      expect(await leagueRowFigures(page, first.shiro)).toMatchObject({ rank: 1, W: 1, L: 0, T: 0, IV: 2, IL: 0, IT: 1, PW: 2, PL: 0 });
      expect(await leagueRowFigures(page, first.aka)).toMatchObject({ W: 0, L: 1, T: 0, IV: 0, IL: 2, IT: 1, PW: 0, PL: 2 });
    });

    await test.step('a spectator phone reads the same table on the League tab', async () => {
      const spectator = await browser.newContext({ ...PUBLIC_DEVICE, baseURL });
      try {
        const phone = await spectator.newPage();
        await openViewerLeague(phone, id);
        await expect.poll(async () => (await readLeagueTable(phone)).played).toBe(1);
        const table = await readLeagueTable(phone);
        record({ step: 'public', action: 'League tab after encounter 1', table });
        await shot(phone, 'public-league-tab', { fullPage: true });
        expect(await leagueRowFigures(phone, first.shiro)).toMatchObject({ rank: 1, W: 1, L: 0 });
        expect(table.rows.map((r) => r.name).sort()).toEqual(teams.map(([t]) => t).sort());
      } finally {
        await spectator.close();
      }
    });
  });

  // The court operator plays the first encounters on the console (Finish +
  // Start Next is theirs), the desk organiser the rest from the numbered rows
  // and the matrix cell where one is drawn (Finish alone: the automatic advance
  // belongs to the court console). The level encounter's sheet is captured for
  // D5 (F5). Run tie-breaker; the court operator meets the tie-breaker bout on
  // the console (F12: does anything say why it is there) and decides it; the
  // table, the podium, and the editor opened on a finished league's match.
  test('run tie-breaker: console and desk scoring, the daihyosen on the court console, the table and the podium', async ({ page, browser, baseURL }) => {
    test.setTimeout(LONG);
    const shot = journeyShots('league-team/run-tiebreaker');
    const { record } = recorder('run-tiebreaker', 'league-team/run-tiebreaker');
    const teams = FOUR_TEAMS('R');
    const { A, B, C, D } = rolesOf('R');
    const script = twoWayTie({ A, B, C, D });
    const court = 'B';
    const done = new Set();
    let id;
    // What the court console says about the daihyosen (F12), read in the
    // console step and judged at the end, so a fixme there skips nothing else.
    let f12 = null;

    const captureLevel = (where) => async (ed) => {
      const level = await readLevelSheet(ed);
      record({ step: 'F5', action: `level encounter A v B before Finish (${where})`, ...level,
        screenshot: await shot(ed.page(), 'f5-level-encounter-sheet', { fullPage: true }) });
      expect(level.totals.shiro).toBe(level.totals.aka);
    };

    await test.step('seed the league on its own shiaijo', async () => {
      await enterTournament(page, LEAGUE_CUP);
      id = await seedLeague(page, { name: 'Run League', kind: 'team', entrants: teams, teamSize: 3, teamMatchType: 'fixed', courts: [court] });
    });

    await test.step('court console: two encounters, each finished with Finish + Start Next', async () => {
      await openShiaijo(page, court);
      let pair = await sides(upNextCard(page));
      const ed = await startUpNextTeam(page);
      for (let k = 0; k < 2; k += 1) {
        await scoreBouts(ed, script.boutsFor(pair));
        if (script.isLevel(pair.shiro, pair.aka)) await captureLevel('court console')(ed);
        const label = squash(await finishBtn(ed).innerText());
        const next = await sides(upNextCard(page));
        record({ step: 'console', action: `encounter ${k + 1} before Finish`, pair, finishLabel: label, nextUp: next,
          screenshot: await shot(page, `console-encounter-${k + 1}-before-finish`) });
        // The ruling: the console, and only the console, advances by itself.
        expect(label).toMatch(/^Finish \+ Start Next/);
        await finishTeam(ed);
        done.add(pairKey(pair.shiro, pair.aka));
        pair = next;
      }
      // The third encounter is now running on the console; the desk takes it.
    });

    await test.step('Pools: the matrix cell where one is drawn, then the numbered rows; Finish alone', async () => {
      await openLeague(page, id);
      const matrix = await leagueMatrix(page).count();
      record({ step: 'pools', action: 'is a matrix drawn on a league', matrixShown: matrix,
        screenshot: await shot(page, 'pools-two-played', { fullPage: true }) });
      if (matrix > 0) {
        const [x, y] = script.pairs().find(([p, q]) => !done.has(pairKey(p, q)));
        const ed = await openMatrixCell(page, x, y);
        const row = await regularRow(page, x, y);
        await scoreBouts(ed, script.boutsFor(row));
        if (script.isLevel(x, y)) await captureLevel('matrix cell')(ed);
        const label = squash(await finishBtn(ed).innerText());
        record({ step: 'pools', action: 'encounter from the matrix cell', pair: { x, y }, finishLabel: label });
        expect(label).toBe('Finish');
        await finishTeam(ed);
        done.add(pairKey(x, y));
        await expect.poll(async () => (await readLeagueTable(page)).played).toBe(done.size);
      }
      const played = await playRestFromRows(page, script, done, { onLevel: captureLevel('numbered row') });
      record({ step: 'pools', action: 'encounters from the numbered rows', played });
      // The ruling: no Finish + Start Next on an admin view (operator ruling 2026-09-26).
      for (const p of played) expect(p.finishLabel, `${p.pair.shiro} v ${p.pair.aka}`).toBe('Finish');
    });

    await test.step('the table level for 1st and 2nd, and the banner', async () => {
      await expect.poll(async () => (await readBanner(page)).shown, { message: 'the tie-break banner' }).toBe(true);
      const tally = script.tally();
      const table = await readLeagueTable(page);
      const banner = await readBanner(page);
      record({ step: 'banner', action: 'all six played', table, banner,
        screenshot: await shot(page, 'banner-two-way-tie', { fullPage: true }) });
      expect(table).toMatchObject({ heading: 'Standings', played: 6, total: 6 });
      for (const team of [A, B, C, D]) expect(await leagueRowFigures(page, team)).toMatchObject(figuresOf(tally, team));
      await expectTwoWayBanner(page, A, B);
      expect(banner.groups[0].run).toEqual({ offered: true, enabled: true });
      expect(banner.acceptOffered).toBe(true);
      // F4: today's wording of the Accept confirm, read and cancelled.
      const asked = await requestAcceptSharedRanks(page);
      record({ step: 'F4', action: 'Accept confirm, read then Cancel', asked, description: banner.description, acceptLabel: banner.acceptLabel });
      await shot(page, 'accept-confirm-today');
      await answerConfirm(page, TEXT.cancel);
      expect((await readBanner(page)).shown).toBe(true);
    });

    let dh;
    await test.step('Run tie-breaker: one daihyosen between A and B joins the rows', async () => {
      const res = await runTiebreaker(page, [A, B]);
      await expect.poll(async () => (await tiebreakRows(page, 6, [A, B])).length).toBe(1);
      [dh] = await tiebreakRows(page, 6, [A, B]);
      const table = await readLeagueTable(page);
      record({ step: 'run', action: 'Run tie-breaker', answer: res, dhRow: dh, table, banner: await readBanner(page),
        screenshot: await shot(page, 'after-run-tiebreaker', { fullPage: true }) });
      expect(table).toMatchObject({ played: 6, total: 7 });
      expect(dh.score).toBe('vs');
    });

    await test.step('court console: the daihyosen in the queue (F12), decided by one men for A', async () => {
      await openShiaijo(page, court);
      const card = upNextCard(page);
      const onConsole = await card.count();
      if (!onConsole) {
        record({ step: 'console', action: 'the daihyosen is not on the court console', screenshot: await shot(page, 'console-no-daihyosen') });
        const ed = await openLeague(page, id).then(() => openNumberedRow(page, { num: dh.num }));
        await playTiebreakBoutOn(ed, dh.shiro === A ? 'shiro' : 'aka');
        return;
      }
      const cardText = squash(await card.innerText());
      const pair = await sides(card);
      await shot(page, 'console-daihyosen-up-next');
      await startUpNext(page);
      const identity = await editorIdentity(page);
      const editorText = squash(await inlineEditor(page).first().innerText());
      const why = /tie-?breaker|daihyosen|representative|play-?off|podium|decid/i;
      f12 = { cardText, eyebrow: identity.eyebrow || '', title: identity.title || '' };
      record({ step: 'F12', action: 'the daihyosen on the court console', ...f12,
        cardSaysWhy: why.test(cardText), editorSaysWhy: why.test(identity.eyebrow) || /tie-?breaker|podium/i.test(editorText),
        screenshot: await shot(page, 'console-daihyosen-running') });
      expect([pair.shiro, pair.aka].sort()).toEqual([A, B].sort());
      await awardIppon(page, pair.shiro === A ? 'shiro' : 'aka', 'M');
      const label = squash(await finishButton(page).innerText());
      record({ step: 'console', action: 'daihyosen before Finish', finishLabel: label });
      await finishMatch(page);
      record({ step: 'console', action: 'after the daihyosen', courtDone: await courtDone(page).count(),
        doneText: (await courtDone(page).count()) ? squash(await courtDone(page).innerText()) : '',
        screenshot: await shot(page, 'console-after-daihyosen') });
    });

    await test.step('Pools: the final table, A first with the DH badge', async () => {
      await openLeague(page, id);
      await expect.poll(async () => (await readLeagueTable(page)).heading, { message: 'the league finished' }).toBe('Final standings');
      const table = await readLeagueTable(page);
      record({ step: 'final', action: 'after the daihyosen', table, banner: await readBanner(page),
        screenshot: await shot(page, 'final-standings', { fullPage: true }) });
      expect(table).toMatchObject({ played: 7, total: 7, winner: A });
      expect(await ranksShown(page)).toEqual({ [A]: 1, [B]: 2, [C]: 3, [D]: 4 });
      expect((await leagueRowFigures(page, A)).dh).toBe(true);
      expect((await readBanner(page)).shown).toBe(false);
    });

    await test.step('the public podium', async () => {
      const { podium, screenshot } = await publicPodium(browser, baseURL, id, shot, 'public-awards');
      record({ step: 'public', action: 'Awards after the daihyosen', podium, screenshot });
      expect(podium.places.slice(0, 3).map((p) => p.name)).toEqual([A, B, C]);
    });

    await test.step('the score editor on a finished league\'s match opens as a correction', async () => {
      await openLeague(page, id);
      const row = (await readNumberedRows(page)).find((r) => r.num <= 6 && samePair(r, A, C));
      const ed = await openNumberedRow(page, { num: row.num });
      const read = await readFinishedEditor(ed);
      record({ step: 'finished league', action: 'tap a played encounter', pair: row, ...read,
        screenshot: await shot(page, 'finished-league-editor') });
      expect(read.pill).toBe('CORRECTION');
      expect(read.primary).toBe('Save correction');
      await closeEditor(page);
    });

    // Last in this test, so it skips only its own check. The design names no
    // wording (P2.6: "a line on the console's tie-breaker row naming the place
    // it decides"), so the check is the shape: the row says it is a
    // tie-breaker and names a place.
    test.fixme(true, 'F12 (P2.6, only if P1.4 confirms; delete this step if dropped): the court console says nothing about why a tie-breaker bout appeared or which place it decides');

    await test.step('fixed: the console row of the daihyosen says it is a tie-breaker and names the place it decides (F12)', async () => {
      expect(f12, 'the daihyosen reached the court console').not.toBeNull();
      const said = `${f12.cardText} ${f12.eyebrow}`;
      expect(said).toMatch(/tie-?breaker|daihyosen/i);
      expect(said).toMatch(/\b(1st|2nd|3rd|4th)\b|positions?\s*\d|place/i);
    });
  });

  // Accept shared ranks: the table and the podium once accepted, read on a
  // second operator tab too (F10). Then Undo (F1): the control, its confirm,
  // and the banner back on both tabs.
  test('accept shared ranks: the final table and podium, then Undo on both tabs (F1, F10)', async ({ page, browser, baseURL }) => {
    test.setTimeout(LONG);
    const shot = journeyShots('league-team/accept');
    const { record } = recorder('accept', 'league-team/accept');
    const teams = FOUR_TEAMS('P');
    const { A, B, C, D } = rolesOf('P');
    const script = twoWayTie({ A, B, C, D });
    const court = 'C';
    let id;

    await test.step('seed and play the two-way tie', async () => {
      await enterTournament(page, LEAGUE_CUP);
      id = await seedAndPlay(page, { name: 'Accept League', court, teams, script });
      await expectTwoWayBanner(page, A, B);
    });

    const second = await secondOperator(browser, baseURL);
    try {
      const tb = second.page;
      await test.step('a second operator tab shows the same banner', async () => {
        await openLeague(tb, id);
        await expect.poll(async () => (await readBanner(tb)).shown).toBe(true);
        await shot(tb, 'second-tab-banner', { fullPage: true });
      });

      let accepted;
      await test.step('Accept shared ranks: the confirm, then the competition finishes', async () => {
        accepted = await acceptSharedRanks(page);
        record({ step: 'accept', action: 'Accept shared ranks', asked: accepted.asked, answer: { status: accepted.status, body: accepted.body } });
        expect(accepted.asked.buttons).toEqual(expect.arrayContaining([TEXT.acceptConfirm, TEXT.cancel]));
        await expect.poll(async () => (await readLeagueTable(page)).heading, { message: 'the league finished' }).toBe('Final standings');
        const tally = script.tally();
        const table = await readLeagueTable(page);
        const ranks = await ranksShown(page);
        record({ step: 'accept', action: 'table after Accept', table, ranks,
          sharedRankReadsShared: ranks[A] === ranks[B], banner: await readBanner(page),
          undoControl: await undoControl(page).count(), acceptedLines: await acceptedLines(page),
          screenshot: await shot(page, 'after-accept', { fullPage: true }) });
        expect(table).toMatchObject({ played: 6, total: 6 });
        expect(await leagueRowFigures(page, A)).toMatchObject(figuresOf(tally, A));
        expect(await leagueRowFigures(page, B)).toMatchObject(figuresOf(tally, B));
        expect([ranks[A], ranks[B]].sort()).toEqual(expect.arrayContaining([1]));
        expect(ranks[C]).toBe(3);
        expect(ranks[D]).toBe(4);
        expect((await readBanner(page)).shown).toBe(false);
      });

      await test.step('the second tab after Accept (F10)', async () => {
        const gone = await expect.poll(async () => (await readBanner(tb)).shown, { timeout: 15_000 }).toBe(false)
          .then(() => true, () => false);
        record({ step: 'F10', action: 'second tab after Accept', bannerGone: gone, banner: await readBanner(tb),
          heading: (await readLeagueTable(tb)).heading, undoControl: await undoControl(tb).count(),
          screenshot: await shot(tb, 'second-tab-after-accept', { fullPage: true }) });
      });

      await test.step('the public podium after Accept', async () => {
        const { podium, screenshot } = await publicPodium(browser, baseURL, id, shot, 'public-awards-after-accept');
        record({ step: 'public', action: 'Awards after Accept', podium, screenshot });
        expect(podium.places.slice(0, 2).map((p) => p.name).sort()).toEqual([A, B].sort());
        expect(podium.places[2] && podium.places[2].name).toBe(C);
      });

      test.fixme(true, 'F1: accepted shared ranks cannot be undone, so a wrong tap on Accept has no way back');

      await test.step('fixed: both tabs offer Undo shared ranks beside the accepted tie (F1, F10)', async () => {
        await expect(undoControl(page)).toBeVisible();
        const lines = await acceptedLines(page);
        expect(lines).toHaveLength(1);
        expect(lines[0]).toMatch(new RegExp(`^Shared ranks accepted: .*(${escape(A)}, ${escape(B)}|${escape(B)}, ${escape(A)})\\.$`));
        await expect(undoControl(tb)).toBeVisible();
      });

      await test.step('fixed: Undo shared ranks takes the league back to match play on both tabs', async () => {
        const undone = await undoSharedRanks(page);
        record({ step: 'F1', action: 'Undo shared ranks', asked: undone.asked, answer: { status: undone.status, body: undone.body },
          screenshot: await shot(page, 'after-undo', { fullPage: true }) });
        expect(undone.asked.message).toBe(TEXT.undoQuestion);
        expect(undone.asked.buttons).toEqual(expect.arrayContaining([TEXT.undoConfirm, TEXT.cancel]));
        await expect.poll(async () => (await readLeagueTable(page)).heading).toBe('Standings');
        await expect.poll(async () => (await readBanner(page)).shown).toBe(true);
        const banner = await expectTwoWayBanner(page, A, B);
        expect(banner.groups[0].run.offered).toBe(true);
        expect(banner.acceptOffered).toBe(true);
        await expect(undoControl(page)).toHaveCount(0);
        await expect.poll(async () => (await readBanner(tb)).shown).toBe(true);
      });
    } finally {
      await second.ctx.close();
    }
  });

  // F3: Accept while the tie-breaker is scheduled but not fought.
  test('accept with a scheduled daihyosen: the banner stays, the confirm names the bout, accepting removes it (F3)', async ({ page }) => {
    test.setTimeout(LONG);
    const shot = journeyShots('league-team/accept-scheduled-dh');
    const { record } = recorder('accept-scheduled-dh', 'league-team/accept-scheduled-dh');
    const teams = FOUR_TEAMS('Q');
    const { A, B, C, D } = rolesOf('Q');
    const script = twoWayTie({ A, B, C, D });
    let asked;

    await test.step('seed, play, Run tie-breaker', async () => {
      await enterTournament(page, LEAGUE_CUP);
      await seedAndPlay(page, { name: 'Scheduled DH League', court: 'D', teams, script });
      await runTiebreaker(page, [A, B]);
      await expect.poll(async () => (await tiebreakRows(page, 6, [A, B])).length).toBe(1);
    });

    await test.step('the banner stays while the daihyosen waits, with Remove and Accept', async () => {
      const banner = await readBanner(page);
      record({ step: 'F3', action: 'banner with a scheduled daihyosen', banner,
        screenshot: await shot(page, 'banner-dh-scheduled', { fullPage: true }) });
      expect(banner.shown).toBe(true);
      expect(banner.groups[0].remove).toEqual({ offered: true, enabled: true });
      expect(banner.acceptOffered).toBe(true);
    });

    let after;
    await test.step('Accept shared ranks with the daihyosen not fought', async () => {
      const res = await acceptSharedRanks(page, { allowRefusal: true });
      asked = res.asked;
      after = {
        table: await readLeagueTable(page), banner: await readBanner(page),
        tiebreakRows: await tiebreakRows(page, 6, [A, B]), undoControl: await undoControl(page).count(),
      };
      record({ step: 'F3', action: 'Accept shared ranks with the daihyosen scheduled', asked, answer: { status: res.status, body: res.body },
        ...after, screenshot: await shot(page, 'after-accept-dh-scheduled', { fullPage: true }) });
    });

    test.fixme(true, 'F3: Accept with a tie-breaker scheduled empties the banner and leaves the league waiting on a bout the organiser declined');

    await test.step('fixed: the confirm named the bout, and accepting removed it and finished the league', async () => {
      expect(asked.message).toContain('Accept shared ranks?');
      expect(asked.message).toContain('The competition finishes now. Undo shared ranks brings the question back.');
      expect(asked.message).toMatch(new RegExp(`${escape(TEXT.acceptRemovesBout(A, B))}|${escape(TEXT.acceptRemovesBout(B, A))}`));
      expect(asked.message).not.toContain('cannot be undone');
      expect(asked.buttons).toEqual(expect.arrayContaining([TEXT.acceptConfirm, TEXT.cancel]));
      await expect.poll(async () => (await tiebreakRows(page, 6, [A, B])).length).toBe(0);
      await expect.poll(async () => (await readLeagueTable(page)).heading).toBe('Final standings');
      expect((await readLeagueTable(page))).toMatchObject({ played: 6, total: 6 });
      expect((await readBanner(page)).shown).toBe(false);
      await expect(undoControl(page)).toBeVisible();
    });
  });

  // F3b: the daihyosen saved as a draw, which today's editor allows. What the
  // organiser sees, whether the bout opens again from Pools; then, per D6 (b),
  // the editor refuses the draw and a hantei winner finishes the league.
  test('a drawn daihyosen: what the organiser sees today, then a tie-breaker that needs a winner (F3b)', async ({ page }) => {
    test.setTimeout(LONG);
    const shot = journeyShots('league-team/drawn-dh');
    const { record } = recorder('drawn-dh', 'league-team/drawn-dh');
    const teams = FOUR_TEAMS('W');
    const { A, B, C, D } = rolesOf('W');
    const script = twoWayTie({ A, B, C, D });
    let dh;
    let draw;

    await test.step('seed, play, Run tie-breaker', async () => {
      await enterTournament(page, LEAGUE_CUP);
      await seedAndPlay(page, { name: 'Drawn DH League', court: 'E', teams, script });
      await runTiebreaker(page, [A, B]);
      await expect.poll(async () => (await tiebreakRows(page, 6, [A, B])).length).toBe(1);
      [dh] = await tiebreakRows(page, 6, [A, B]);
    });

    await test.step('open the daihyosen and try to mark it drawn', async () => {
      const ed = await openNumberedRow(page, { num: dh.num });
      const start = ed.locator('button').filter({ hasText: /^Start match$/ }).first();
      if (await start.isVisible()) await startMatch(page);
      await running(page, EDITOR);
      const toggle = ed.getByTestId('scoring-modal-mark-draw');
      draw = {
        toggleDisabled: await toggle.isDisabled(),
        toggleTitle: (await toggle.getAttribute('title')) || '',
        finishLabel: squash(await finishButton(page, EDITOR).innerText().catch(() => '')),
      };
      await shot(page, 'dh-before-draw');
      draw.saved = false;
      if (!draw.toggleDisabled) {
        await toggle.tap();
        await expect(toggle).toHaveText('Cancel draw');
        draw.finishLabelDrawn = squash(await finishButton(page, EDITOR).innerText().catch(() => ''));
        const blocked = /^Needs a winner/.test(draw.finishLabelDrawn);
        if (!blocked) {
          await finishMatch(page, EDITOR);
          draw.saved = true;
        }
      }
      record({ step: 'F3b', action: 'mark the daihyosen drawn', ...draw, screenshot: await shot(page, 'dh-draw-attempt') });
    });

    await test.step('what Pools shows once a drawn daihyosen is saved', async () => {
      if (!draw.saved) {
        record({ step: 'F3b', action: 'no draw saved: the editor refused it' });
        return;
      }
      await expect.poll(async () => (await readLeagueTable(page)).played).toBe(7);
      const seen = {
        table: await readLeagueTable(page), banner: await readBanner(page),
        chusen: await readChusen(page), chusenRecorded: await readChusenRecorded(page),
        drawnNotices: await drawnBoutNotices(page),
      };
      record({ step: 'F3b', action: 'Pools after the drawn daihyosen', ...seen,
        screenshot: await shot(page, 'pools-after-drawn-dh', { fullPage: true }) });
      // Can the saved draw be opened again from Pools and given a winner?
      const ed = await openNumberedRow(page, { num: dh.num });
      const reopened = await readFinishedEditor(ed);
      const arm = ed.getByTestId('scoring-modal-hantei-arm');
      reopened.hanteiArm = (await arm.count()) ? { disabled: await arm.isDisabled(), title: (await arm.getAttribute('title')) || '' } : null;
      record({ step: 'F3b', action: 'tap the drawn daihyosen row', ...reopened, screenshot: await shot(page, 'drawn-dh-reopened') });
      await closeEditor(page);
    });

    test.fixme(true, 'F3b: a league tie-breaker saved as a draw strands the league; per D6 (b) it needs a winner');

    await test.step('fixed: the editor refused the draw and pointed to encho and hantei', async () => {
      expect(draw.saved).toBe(false);
      expect(draw.toggleDisabled).toBe(true);
      expect(draw.toggleTitle).toMatch(/tie-breaker needs a winner/i);
      expect(draw.finishLabel).toMatch(/^Needs a winner/);
    });

    await test.step('fixed: a hantei for Shiro decides the daihyosen and finishes the league', async () => {
      const ed = page.locator(EDITOR);
      await expect(ed).toBeVisible();
      await ed.getByTestId('scoring-modal-hantei-arm').tap();
      await ed.getByTestId('scoring-modal-hantei-shiro').tap();
      await finishMatch(page, EDITOR);
      await expect.poll(async () => (await readLeagueTable(page)).heading).toBe('Final standings');
      const ranks = await ranksShown(page);
      record({ step: 'F3b', action: 'hantei decided the daihyosen', ranks, screenshot: await shot(page, 'dh-hantei-final', { fullPage: true }) });
      expect(ranks[dh.shiro]).toBe(1);
      expect(ranks[dh.aka]).toBe(2);
      expect(await drawnBoutNotices(page)).toEqual([]);
    });
  });

  // A fought daihyosen cycle (each of three teams beats one other): the chusen
  // panel only once every bout is won, then Accept on that cycle settles it
  // (design item 4: today Accept leaves the league uncompletable).
  test('a daihyosen cycle: chusen only once every bout is won, then Accept finishes the league', async ({ page }) => {
    test.setTimeout(LONG);
    const shot = journeyShots('league-team/dh-cycle');
    const { record } = recorder('dh-cycle', 'league-team/dh-cycle');
    const teams = FOUR_TEAMS('Y');
    const { A, B, C, D } = rolesOf('Y');
    const script = ringTie({ A, B, C, D });
    const beats = new Map([[A, B], [B, C], [C, A]]); // winner -> loser
    let offered;
    let res;

    await test.step('seed, play the ring tie, Run tie-breaker on the three', async () => {
      await enterTournament(page, LEAGUE_CUP);
      await seedAndPlay(page, { name: 'Cycle League', court: 'F', teams, script });
      const banner = await readBanner(page);
      record({ step: 'cycle', action: 'banner of the ring tie', banner, screenshot: await shot(page, 'banner-ring', { fullPage: true }) });
      expect(banner.groups).toHaveLength(1);
      expect([...banner.groups[0].teams].sort()).toEqual([A, B, C].sort());
      await runTiebreaker(page, [A, B, C]);
      await expect.poll(async () => (await tiebreakRows(page, 6, [A, B, C])).length).toBeGreaterThan(0);
      record({ step: 'cycle', action: 'Run tie-breaker on three', tiebreakRows: await tiebreakRows(page, 6, [A, B, C]),
        screenshot: await shot(page, 'after-run-ring', { fullPage: true }) });
    });

    await test.step('win each daihyosen in a cycle; chusen only after the last', async () => {
      const rows = await tiebreakRows(page, 6, [A, B, C]);
      expect(rows).toHaveLength(3);
      for (const [i, row] of rows.entries()) {
        const winner = beats.get(row.shiro) === row.aka ? row.shiro : row.aka;
        const ed = await openNumberedRow(page, { num: row.num });
        await playTiebreakBoutOn(ed, winner === row.shiro ? 'shiro' : 'aka');
        await expect.poll(async () => (await readLeagueTable(page)).played).toBe(7 + i);
        const chusen = await readChusen(page);
        record({ step: 'cycle', action: `daihyosen ${i + 1} won by ${winner}`, chusen, banner: await readBanner(page),
          screenshot: await shot(page, `cycle-after-dh-${i + 1}`, { fullPage: true }) });
        if (i < rows.length - 1) expect(chusen, 'no chusen while a daihyosen of the cycle is unfought').toEqual([]);
      }
      await expect.poll(async () => (await readChusen(page)).length, { message: 'the chusen panel for the cycle' }).toBe(1);
      expect([...(await readChusen(page))[0].teams].sort()).toEqual([A, B, C].sort());
      offered = (await acceptButton(page).count()) > 0;
      record({ step: 'cycle', action: 'after the cycle', acceptOffered: offered, table: await readLeagueTable(page),
        screenshot: await shot(page, 'cycle-chusen', { fullPage: true }) });
      if (offered) {
        res = await acceptSharedRanks(page, { allowRefusal: true });
        record({ step: 'cycle', action: 'Accept shared ranks on the fought cycle', asked: res.asked, answer: { status: res.status, body: res.body },
          table: await readLeagueTable(page), chusen: await readChusen(page),
          screenshot: await shot(page, 'cycle-after-accept', { fullPage: true }) });
      }
    });

    test.fixme(true, 'F3b: Accept on a fought daihyosen cycle leaves the league uncompletable (design item 4)');

    await test.step('fixed: Accept is offered on the cycle, names lots as the other way, and finishes the league', async () => {
      expect(offered).toBe(true);
      expect(res.ok).toBe(true);
      expect(res.asked.message).toContain(TEXT.acceptCycleLots);
      await expect.poll(async () => (await readLeagueTable(page)).heading).toBe('Final standings');
      await expect(undoControl(page)).toBeVisible();
    });
  });

  // F2: the stale tab. Tab A shows the banner and opens Accept's confirm; tab
  // B takes the last encounter back to match play (a kachinuki league: its
  // Reopen match is the one door that does that); tab A then confirms. Per
  // D7 (a) the server says no with a sentence and the league is not finished.
  test('a stale tab accepts while an encounter is back in play: refused with a sentence (F2)', async ({ page, browser, baseURL }) => {
    test.setTimeout(LONG);
    const shot = journeyShots('league-team/stale-accept');
    const { record } = recorder('stale-accept', 'league-team/stale-accept');
    const [A, B, C] = ['K Hayabusa', 'K Inoshishi', 'K Kitsune'];
    const entrants = [[A, 'Kita Dojo'], [B, 'Minami Dojo'], [C, 'Higashi Dojo']];
    const MEMBERS = { [A]: ['Abe', 'Baba', 'Chiba'], [B]: ['Doi', 'Endo', 'Fujii'], [C]: ['Goto', 'Hara', 'Ishii'] };
    const beats = new Map([[A, B], [B, C], [C, A]]); // winner -> loser
    const court = 'G';
    let id;
    let last;

    await test.step('seed a kachinuki team league of three on its own shiaijo', async () => {
      await enterTournament(page, LEAGUE_CUP);
      id = await seedLeague(page, { name: 'Stale League', kind: 'team', entrants, teamSize: 3, teamMatchType: 'kachinuki', courts: [court] });
    });

    await test.step('court console: name every lineup and win each encounter in a ring', async () => {
      await openShiaijo(page, court);
      const named = new Set();
      for (let k = 0; k < 3; k += 1) {
        const pair = await sides(upNextCard(page));
        const needing = [pair.shiro, pair.aka].filter((t) => !named.has(t));
        if (needing.length) {
          await openLineupFromUpNext(page);
          for (const team of needing) {
            await typeLineup(page, team, MEMBERS[team]);
            named.add(team);
          }
          await closeLineupPanel(page);
        }
        await startUpNextKachinuki(page);
        const winner = beats.get(pair.shiro) === pair.aka ? pair.shiro : pair.aka;
        const side = winner === pair.shiro ? 'shiro' : 'aka';
        await awardBoutIppon(page, side, 'M');
        await awardBoutIppon(page, side, 'K');
        await endMatch(page);
        await expect(completedRowFor(page, pair)).toBeVisible();
        last = pair;
      }
    });

    await test.step('tab A: the ring tie on the banner', async () => {
      await openLeague(page, id);
      await expect.poll(async () => (await readBanner(page)).shown, { message: 'the tie-break banner' }).toBe(true);
      const banner = await readBanner(page);
      record({ step: 'F2', action: 'tab A banner', banner, screenshot: await shot(page, 'tab-a-banner', { fullPage: true }) });
      expect([...banner.groups[0].teams].sort()).toEqual([A, B, C].sort());
    });

    const second = await secondOperator(browser, baseURL);
    let answer;
    let sentenceShown;
    let afterEnd;
    try {
      const tb = second.page;
      await test.step('tab A opens Accept; tab B reopens the last encounter meanwhile', async () => {
        const asked = await requestAcceptSharedRanks(page);
        record({ step: 'F2', action: 'tab A: Accept confirm open', asked });
        await openLeague(tb, id);
        const row = (await readNumberedRows(tb)).find((r) => samePair(r, last.shiro, last.aka));
        await openNumberedRow(tb, { num: row.num });
        await reopenButton(tb).tap();
        await expect(tb.locator(EDITOR).getByRole('button', { name: 'Record bout' })).toBeVisible();
        record({ step: 'F2', action: 'tab B: Reopen match on the last encounter', pair: last,
          screenshot: await shot(tb, 'tab-b-reopened') });
        await tb.keyboard.press('Escape');
      });

      await test.step('tab A confirms Accept on the table that can still change', async () => {
        const answered = finalizeAnswer(page);
        await answerConfirm(page, TEXT.acceptConfirm);
        answer = await answered;
        sentenceShown = await page.getByText(TEXT.notSettled).first().waitFor({ state: 'visible', timeout: 5_000 })
          .then(() => true, () => false);
        record({ step: 'F2', action: 'tab A: Accept confirmed', answer, sentenceShown, banner: await readBanner(page),
          table: await readLeagueTable(page), screenshot: await shot(page, 'tab-a-after-stale-accept', { fullPage: true }) });
      });

      await test.step('tab B ends the reopened encounter at the court', async () => {
        await openShiaijo(tb, court);
        await expect(endMatchButton(tb)).toBeVisible();
        await endMatch(tb);
        await expect(completedRowFor(tb, last)).toBeVisible();
        await openLeague(page, id);
        await expect.poll(async () => (await readLeagueTable(page)).played).toBe(3);
        afterEnd = { table: await readLeagueTable(page), banner: await readBanner(page) };
        record({ step: 'F2', action: 'tab A after the encounter ended again', ...afterEnd,
          screenshot: await shot(page, 'tab-a-after-end', { fullPage: true }) });
      });
    } finally {
      await second.ctx.close();
    }

    test.fixme(true, 'F2: a stale tab can accept shared ranks before the table is final, and the league then finishes silently');

    await test.step('fixed: the server said no with the D7 (a) sentence, and the tie is asked again', async () => {
      expect(answer.status).toBe(409);
      expect(sentenceShown).toBe(true);
      expect(afterEnd.table.heading).toBe('Standings');
      expect(afterEnd.banner.shown).toBe(true);
      expect([...afterEnd.banner.groups[0].teams].sort()).toEqual([A, B, C].sort());
    });
  });

  // F2b: a correction on a FINISHED league, from an admin host (Pools) and
  // from the court console. Warned first (the fix); a correction that leaves
  // no new tie keeps the league finished, one that makes a new tie at a podium
  // place takes it back to match play with the banner.
  test('corrections on a finished league: warned, then kept finished or reopened (F2b)', async ({ page }) => {
    test.setTimeout(LONG);
    const shot = journeyShots('league-team/finished-corrections');
    const { record } = recorder('finished-corrections', 'league-team/finished-corrections');
    const teams = FOUR_TEAMS('F');
    const { A, B, C, D } = rolesOf('F');
    const script = twoWayTie({ A, B, C, D });
    const court = 'H';
    let id;
    let first;
    let secondFix;

    await test.step('seed, play, decide the daihyosen for A: the league finishes', async () => {
      await enterTournament(page, LEAGUE_CUP);
      // Two joint 3rd places off, so a tie at 3rd is a consequential one.
      id = await seedAndPlay(page, { name: 'Corrections League', court, teams, script, twoThirdPlaces: false });
      await runTiebreaker(page, [A, B]);
      await expect.poll(async () => (await tiebreakRows(page, 6, [A, B])).length).toBe(1);
      const [dh] = await tiebreakRows(page, 6, [A, B]);
      const ed = await openNumberedRow(page, { num: dh.num });
      await playTiebreakBoutOn(ed, dh.shiro === A ? 'shiro' : 'aka');
      await expect.poll(async () => (await readLeagueTable(page)).heading).toBe('Final standings');
      await shot(page, 'finished', { fullPage: true });
    });

    await test.step('Pools (admin host): C v D bout 3 corrected from hikiwake to a men for D', async () => {
      const row = await regularRow(page, C, D);
      const ed = await openNumberedRow(page, { num: row.num });
      const dSide = row.shiro === D ? 'shiro' : 'aka';
      const bout = boutRow(ed, 3);
      if ((await rowMiddle(bout).innerText()).trim() === 'X') await rowTie(bout).tap();
      await expect(rowMiddle(bout)).not.toHaveText('X');
      await boutIppon(ed, 3, dSide, 'M');
      first = await saveCorrection(page, ed);
      script.set(C, D, [C, C, D]);
      const tally = script.tally();
      // The table on screen when the overlay closes, then after a reload: a
      // correction that keeps the encounter's winner changes no match's
      // status or winner, which is all the table's refetch keys on.
      await page.waitForTimeout(2_000);
      first.liveFigures = { C: await leagueRowFigures(page, C), D: await leagueRowFigures(page, D) };
      first.liveTableFollowed = first.liveFigures.D.IV === tally[D].IV;
      await openLeague(page, id);
      await expect.poll(async () => (await leagueRowFigures(page, D)).IV, { message: 'the table follows the correction' }).toBe(tally[D].IV);
      const table = await readLeagueTable(page);
      first.table = table;
      record({ step: 'F2b', action: 'correction from Pools that leaves no new tie', ...first,
        banner: await readBanner(page), screenshot: await shot(page, 'after-correction-1', { fullPage: true }) });
      expect(await leagueRowFigures(page, C)).toMatchObject(figuresOf(tally, C));
      expect(await leagueRowFigures(page, D)).toMatchObject(figuresOf(tally, D));
    });

    await test.step('court console: C v D bout 1 corrected to hikiwake, a new tie at 3rd', async () => {
      await openShiaijo(page, court);
      const pair = { shiro: '', aka: '' };
      const rowOnConsole = completedRowFor(page, { shiro: C, aka: D });
      const onConsole = await rowOnConsole.count();
      record({ step: 'F2b', action: 'console of a finished league', completedRowShown: onConsole,
        courtDone: (await courtDone(page).count()) ? squash(await courtDone(page).innerText()) : '',
        screenshot: await shot(page, 'console-finished-league', { fullPage: true }) });
      let ed;
      if (onConsole) {
        Object.assign(pair, await sides(rowOnConsole));
        await rowOnConsole.getByRole('button', { name: 'Correct' }).tap();
        ed = inlineEditor(page).first();
        await expect(ed.locator('.editor-head-pill')).toHaveText('CORRECTION');
      } else {
        // No door on the console: the correction is made from Pools instead.
        await openLeague(page, id);
        const row = await regularRow(page, C, D);
        Object.assign(pair, { shiro: row.shiro, aka: row.aka });
        ed = await openNumberedRow(page, { num: row.num });
      }
      const cSide = pair.shiro === C ? 'shiro' : 'aka';
      await clearMarks(rowSlots(boutRow(ed, 1), cSide));
      await ensureTie(ed, 1);
      secondFix = await saveCorrection(page, ed);
      script.set(C, D, [null, C, D]);
      await openLeague(page, id);
      const tally = script.tally();
      await expect.poll(async () => (await leagueRowFigures(page, C)).T).toBe(tally[C].T);
      secondFix.table = await readLeagueTable(page);
      secondFix.banner = await readBanner(page);
      secondFix.onConsole = !!onConsole;
      record({ step: 'F2b', action: 'correction from the court console that makes a tie at 3rd', ...secondFix,
        screenshot: await shot(page, 'after-correction-2', { fullPage: true }) });
      expect(await leagueRowFigures(page, C)).toMatchObject(figuresOf(tally, C));
      expect(await leagueRowFigures(page, D)).toMatchObject(figuresOf(tally, D));
    });

    test.fixme(true, 'F2b: a correction on a finished league moves the table and podium with no warning, and a new tie is never asked');

    await test.step('fixed: both corrections were warned first; the first kept the league finished', async () => {
      expect(first.warned).toBe(true);
      expect(first.warning.message).toContain(TEXT.finishedLeague);
      expect(first.warning.buttons).toEqual(expect.arrayContaining([TEXT.changeAnyway, TEXT.cancel]));
      expect(first.table.heading).toBe('Final standings');
      // Recorded, not asserted here (first.liveTableFollowed): the table on
      // screen kept the old figures until a reload, because the correction
      // left the encounter's winner as it was. A finding of its own, not F2b's.
      expect(secondFix.onConsole).toBe(true);
      expect(secondFix.warned).toBe(true);
      expect(secondFix.warning.message).toContain(TEXT.finishedLeague);
    });

    await test.step('fixed: the new tie at 3rd took the league back to match play with the banner', async () => {
      expect(secondFix.table.heading).toBe('Standings');
      expect(secondFix.banner.shown).toBe(true);
      const group = secondFix.banner.groups.find((g) => [...g.teams].sort().join('|') === [C, D].sort().join('|'));
      expect(group).toMatchObject({ min: 3, max: 4 });
    });
  });

  // F2b, the accepted group moved: C and D accepted as a shared 3rd, then four
  // corrections from Pools (each warned on the fixed app) move them, still
  // level, to a shared 1st. An accepted group is kept by its place AND its
  // teams (design item 4), so the same two teams at another place are a new
  // tie and asked again. No table between the corrections holds a tie, so
  // the league stays finished until the last one.
  test('an accepted shared 3rd that corrections move to a shared 1st is asked again (F2b)', async ({ page }) => {
    test.setTimeout(LONG);
    const shot = journeyShots('league-team/shared-third-moved');
    const { record } = recorder('shared-third-moved', 'league-team/shared-third-moved');
    const teams = FOUR_TEAMS('T');
    const roles = rolesOf('T');
    const { A, B, C, D } = roles;
    const script = sharedThird(roles);
    const court = 'I';
    const after = [];
    let id;

    await test.step('seed and play: C and D level for 3rd on the banner', async () => {
      await enterTournament(page, LEAGUE_CUP);
      // Two joint 3rd places off, so a tie at 3rd is a consequential one.
      id = await seedAndPlay(page, { name: 'Moved Third League', court, teams, script, twoThirdPlaces: false });
      const banner = await readBanner(page);
      const tally = script.tally();
      record({ step: 'F2b', action: 'the tie at 3rd', banner, table: await readLeagueTable(page),
        screenshot: await shot(page, 'banner-tie-at-3rd', { fullPage: true }) });
      for (const team of [A, B, C, D]) expect(await leagueRowFigures(page, team)).toMatchObject(figuresOf(tally, team));
      expect(banner.groups).toHaveLength(1);
      expect([...banner.groups[0].teams].sort()).toEqual([C, D].sort());
      expect(banner.groups[0]).toMatchObject({ min: 3, max: 4 });
    });

    await test.step('Accept shared ranks: C and D share 3rd and the league finishes', async () => {
      const accepted = await acceptSharedRanks(page);
      await expect.poll(async () => (await readLeagueTable(page)).heading, { message: 'the league finished' }).toBe('Final standings');
      const ranks = await ranksShown(page);
      record({ step: 'F2b', action: 'Accept shared ranks at 3rd', asked: accepted.asked, answer: { status: accepted.status, body: accepted.body },
        ranks, acceptedLines: await acceptedLines(page), screenshot: await shot(page, 'accepted-shared-3rd', { fullPage: true }) });
      expect(ranks[A]).toBe(1);
      expect(ranks[B]).toBe(2);
    });

    for (const [k, [x, y, to]] of sharedThirdToFirst(roles).entries()) {
      await test.step(`correction ${k + 1} from Pools: ${x} v ${y} won by ${to.find((w) => w !== null)}`, async () => {
        const res = await correctFromRow(page, script, [x, y], to);
        const winner = to.find((w) => w !== null);
        const tally = script.tally();
        await openLeague(page, id);
        await expect.poll(async () => leagueRowFigures(page, winner), { message: 'the table follows the correction' })
          .toMatchObject(figuresOf(tally, winner));
        const seen = { ...res, table: await readLeagueTable(page), banner: await readBanner(page), ranks: await ranksShown(page) };
        after.push(seen);
        record({ step: 'F2b', action: `correction ${k + 1}: ${x} v ${y}`, ...seen,
          screenshot: await shot(page, `after-correction-${k + 1}`, { fullPage: true }) });
        for (const team of [A, B, C, D]) expect(await leagueRowFigures(page, team)).toMatchObject(figuresOf(tally, team));
      });
    }

    test.fixme(true, 'F2b: an accepted shared 3rd that corrections move to a shared 1st is never asked again');

    await test.step('fixed: every correction was warned; the league stayed finished until the last', async () => {
      for (const [k, seen] of after.entries()) {
        expect(seen.warned, `correction ${k + 1} warned`).toBe(true);
        expect(seen.warning.message).toContain(TEXT.finishedLeague);
      }
      for (const seen of after.slice(0, -1)) {
        expect(seen.table.heading).toBe('Final standings');
        expect(seen.banner.shown).toBe(false);
      }
    });

    await test.step('fixed: C and D level for 1st took the league back to match play with the banner', async () => {
      const last = after[after.length - 1];
      expect(last.table.heading).toBe('Standings');
      expect(last.banner.shown).toBe(true);
      expect(last.banner.groups).toHaveLength(1);
      expect([...last.banner.groups[0].teams].sort()).toEqual([C, D].sort());
      expect(last.banner.groups[0]).toMatchObject({ min: 1, max: 2 });
      expect(last.ranks[A]).toBe(3);
      expect(last.ranks[B]).toBe(4);
    });
  });

  // F3b, a drawn daihyosen saved before the fix (D6 (b)): no draw is rewritten
  // in storage, so Pools names the saved draw, for any league kind, and opens
  // it to be decided by encho or hantei. The record is restored from a backup
  // (see forgeDrawnDaihyosen), as a copy beside the league it came from.
  test('a daihyosen saved as a draw before the fix: Pools names it and opens it (F3b)', async ({ page }) => {
    test.setTimeout(LONG);
    const shot = journeyShots('league-team/saved-drawn-dh');
    const { record } = recorder('saved-drawn-dh', 'league-team/saved-drawn-dh');
    const teams = FOUR_TEAMS('N');
    const { A, B, C, D } = rolesOf('N');
    const script = twoWayTie({ A, B, C, D });
    const name = 'Notice League';
    const copyName = 'Notice League Old';
    const work = fs.mkdtempSync(path.join(os.tmpdir(), 'league-drawn-dh-'));
    let copyId;
    let dh;
    let seen;

    try {
      await test.step('seed, play, Run tie-breaker: one daihyosen scheduled', async () => {
        await enterTournament(page, LEAGUE_CUP);
        await seedAndPlay(page, { name, court: 'J', teams, script });
        await runTiebreaker(page, [A, B]);
        await expect.poll(async () => (await tiebreakRows(page, 6, [A, B])).length).toBe(1);
      });

      let forged;
      await test.step('back the league up, and save its daihyosen as a draw in the archive', async () => {
        await openBackupPage(page);
        const [download] = await Promise.all([
          page.waitForEvent('download'),
          page.getByRole('button', { name: `Back up "${name}"`, exact: true }).tap(),
        ]);
        const file = path.join(work, download.suggestedFilename());
        await download.saveAs(file);
        forged = path.join(work, 'drawn-daihyosen.zip');
        const row = forgeDrawnDaihyosen(file, forged);
        record({ step: 'F3b', action: 'the daihyosen row as restored', backup: path.basename(file), row });
        expect(row).toMatchObject({ Status: 'completed', Winner: '', Decision: 'hikiwake' });
      });

      await test.step('restore it as a copy on Backup and restore', async () => {
        await chooseBackup(page, forged);
        const row = planRow(page, name);
        await row.getByRole('radio', { name: /Restore as a copy named/ }).check();
        await row.getByRole('textbox', { name: `Name for the copy of "${name}"` }).fill(copyName);
        await expect(restoreButton(page)).toHaveText('Restore');
        const answered = page.waitForResponse((r) => r.request().method() === 'POST'
          && new URL(r.url()).pathname === '/api/tournament/restore');
        await restoreButton(page).tap();
        await confirmWithPassword(page, 'Confirm restore', 'Restore', PASSWORD);
        const res = await answered;
        expect(res.ok(), `restore answered ${res.status()}`).toBe(true);
        const body = await res.json();
        await expect(page.getByRole('heading', { level: 2, name: 'Restore completed' })).toBeVisible();
        copyId = ((body.plan && body.plan.competitions) || []).find((c) => c.newId)?.newId;
        record({ step: 'F3b', action: 'restored as a copy', copyId, screenshot: await shot(page, 'restored-copy') });
        expect(copyId, 'the copy has an id').toBeTruthy();
      });

      await test.step('the copy on Pools today: what the organiser sees, and the drawn row tapped', async () => {
        await openLeague(page, copyId);
        await expect.poll(async () => (await readLeagueTable(page)).played).toBe(7);
        [dh] = await tiebreakRows(page, 6, [A, B]);
        seen = {
          dhRow: dh, table: await readLeagueTable(page), banner: await readBanner(page),
          chusen: await readChusen(page), drawnNotices: await drawnBoutNotices(page),
        };
        record({ step: 'F3b', action: 'Pools of a league holding a saved drawn daihyosen', ...seen,
          screenshot: await shot(page, 'pools-saved-drawn-dh', { fullPage: true }) });
        expect(dh, 'the daihyosen row').toBeTruthy();
        const ed = await openNumberedRow(page, { num: dh.num });
        const opened = await readFinishedEditor(ed);
        record({ step: 'F3b', action: 'tap the saved drawn daihyosen row', ...opened, screenshot: await shot(page, 'saved-drawn-dh-opened') });
        // The record reads as the editor's own saved draw does.
        expect(opened.pill).toBe('CORRECTION');
        expect(opened.drawToggle && opened.drawToggle.text).toMatch(/^cancel draw$/i);
        await closeEditor(page);
      });
    } finally {
      fs.rmSync(work, { recursive: true, force: true });
    }

    test.fixme(true, 'F3b: nothing on Pools names a tie-breaker saved as a draw or opens it to be given a winner (D6 (b))');

    await test.step('fixed: one notice names the drawn daihyosen', async () => {
      expect(seen.drawnNotices).toHaveLength(1);
      expect([TEXT.drawnBout(dh.shiro, dh.aka), TEXT.drawnBout(dh.aka, dh.shiro)]).toContain(seen.drawnNotices[0]);
    });

    await test.step('fixed: Open bout, a hantei for Shiro, and the league finishes', async () => {
      await openLeague(page, copyId);
      const ed = await openDrawnBout(page);
      // The saved draw is taken off first: it must stay cancellable, or the
      // notice's "open it and decide" is a dead end.
      const toggle = ed.getByTestId('scoring-modal-mark-draw');
      if ((await toggle.count()) && /cancel draw/i.test(await toggle.innerText())) await toggle.tap();
      await ed.getByTestId('scoring-modal-hantei-arm').tap();
      await ed.getByTestId('scoring-modal-hantei-shiro').tap();
      const saved = await saveCorrection(page, ed);
      await expect.poll(async () => (await readLeagueTable(page)).heading).toBe('Final standings');
      const ranks = await ranksShown(page);
      record({ step: 'F3b', action: 'hantei decided the saved drawn daihyosen', saved, ranks,
        screenshot: await shot(page, 'saved-drawn-dh-hantei', { fullPage: true }) });
      expect(ranks[dh.shiro]).toBe(1);
      expect(ranks[dh.aka]).toBe(2);
      expect(await drawnBoutNotices(page)).toEqual([]);
    });
  });
});

// ------------------------------------------- a drawn tie-breaker, as data --
//
// Once a league tie-breaker needs a winner (D6 (b)) no screen can save one
// drawn, so the only drawn tie-breaker the notice can meet is one saved
// before the fix. The journey makes that record the way such a tournament
// reaches the app: a backup archive, restored through Backup and restore. The
// archive is the app's own backup of the league with its scheduled daihyosen
// rewritten as a finished bout with no winner; the manifest's size and SHA-256
// for that file are rewritten with it (the restore checks both).

// RFC 4180 rows of `text` (quoted fields may hold commas, quotes, newlines).
function parseCsv(text) {
  const rows = [];
  let row = [];
  let field = '';
  let quoted = false;
  for (let i = 0; i < text.length; i += 1) {
    const ch = text[i];
    if (quoted) {
      if (ch === '"' && text[i + 1] === '"') { field += '"'; i += 1; } else if (ch === '"') quoted = false;
      else field += ch;
    } else if (ch === '"') quoted = true;
    else if (ch === ',') { row.push(field); field = ''; } else if (ch === '\n' || ch === '\r') {
      if (ch === '\r' && text[i + 1] === '\n') i += 1;
      row.push(field); rows.push(row); row = []; field = '';
    } else field += ch;
  }
  if (field !== '' || row.length) { row.push(field); rows.push(row); }
  return rows;
}

// The rows back as CSV, quoting a field only where it needs it.
const formatCsv = (rows) => rows.map((r) => r.map((f) => (/[",\r\n]|^\s/.test(f) ? `"${f.replace(/"/g, '""')}"` : f)).join(',')).join('\n') + '\n';

// A zip of `entries` (name -> Buffer), every entry stored.
function writeZip(entries) {
  const locals = [];
  const centrals = [];
  let offset = 0;
  for (const [name, data] of entries) {
    const nameBuf = Buffer.from(name, 'utf8');
    const crc = zlib.crc32(data);
    const local = Buffer.alloc(30);
    local.writeUInt32LE(0x04034b50, 0);
    local.writeUInt16LE(20, 4);
    local.writeUInt16LE(0x0800, 6); // UTF-8 names
    local.writeUInt16LE(0, 8); // stored
    local.writeUInt16LE(0, 10);
    local.writeUInt16LE(0x21, 12); // 1980-01-01
    local.writeUInt32LE(crc, 14);
    local.writeUInt32LE(data.length, 18);
    local.writeUInt32LE(data.length, 22);
    local.writeUInt16LE(nameBuf.length, 26);
    local.writeUInt16LE(0, 28);
    const central = Buffer.alloc(46);
    central.writeUInt32LE(0x02014b50, 0);
    central.writeUInt16LE(20, 4);
    central.writeUInt16LE(20, 6);
    central.writeUInt16LE(0x0800, 8);
    central.writeUInt16LE(0, 10);
    central.writeUInt16LE(0, 12);
    central.writeUInt16LE(0x21, 14);
    central.writeUInt32LE(crc, 16);
    central.writeUInt32LE(data.length, 20);
    central.writeUInt32LE(data.length, 24);
    central.writeUInt16LE(nameBuf.length, 28);
    central.writeUInt32LE(offset, 42);
    locals.push(local, nameBuf, data);
    centrals.push(central, nameBuf);
    offset += 30 + nameBuf.length + data.length;
  }
  const dir = Buffer.concat(centrals);
  const end = Buffer.alloc(22);
  end.writeUInt32LE(0x06054b50, 0);
  end.writeUInt16LE(entries.size, 8);
  end.writeUInt16LE(entries.size, 10);
  end.writeUInt32LE(dir.length, 12);
  end.writeUInt32LE(offset, 16);
  return Buffer.concat([...locals, dir, end]);
}

// Write to `out` the backup `file` with its one daihyosen row (MatchIdx
// "DH-<n>") saved as a finished hikiwake. Returns the row as rewritten.
function forgeDrawnDaihyosen(file, out) {
  const entries = readZip(fs.readFileSync(file));
  const csvName = [...entries.keys()].find((n) => n.endsWith('/pool-matches.csv'));
  if (!csvName) throw new Error(`no pool-matches.csv in the backup: ${[...entries.keys()].join(', ')}`);
  const rows = parseCsv(entries.get(csvName).toString('utf8'));
  const [head] = rows;
  const col = (name) => {
    const i = head.indexOf(name);
    if (i < 0) throw new Error(`pool-matches.csv has no ${name} column: ${head.join(', ')}`);
    return i;
  };
  const dhRows = rows.slice(1).filter((r) => /^DH-/.test(r[col('MatchIdx')]));
  if (dhRows.length !== 1) throw new Error(`expected one daihyosen row, found ${dhRows.length}`);
  const [dh] = dhRows;
  // As today's editor saves a drawn bout: finished, no winner, hikiwake.
  dh[col('Status')] = 'completed';
  dh[col('Winner')] = '';
  dh[col('Decision')] = 'hikiwake';
  const csv = Buffer.from(formatCsv(rows), 'utf8');
  entries.set(csvName, csv);
  const manifest = JSON.parse(entries.get('manifest.json').toString('utf8'));
  const listed = manifest.files.find((f) => f.path === csvName);
  listed.size = csv.length;
  listed.sha256 = crypto.createHash('sha256').update(csv).digest('hex');
  entries.set('manifest.json', Buffer.from(JSON.stringify(manifest, null, 2), 'utf8'));
  fs.writeFileSync(out, writeZip(entries));
  return Object.fromEntries(head.slice(0, 11).map((h, i) => [h, dh[i]]));
}

// Score a tie-breaker bout on the overlay editor `ed`: one men for `side`,
// then Finish.
async function playTiebreakBoutOn(ed, side) {
  const page = ed.page();
  await expect(ed).toBeVisible();
  const start = ed.locator('button').filter({ hasText: /^Start match$/ }).first();
  if (await start.isVisible()) await startMatch(page);
  await running(page, EDITOR);
  await awardIppon(page, side, 'M', EDITOR);
  return finishMatch(page, EDITOR);
}

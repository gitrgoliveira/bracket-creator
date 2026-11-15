// INDIVIDUAL LEAGUE journeys: the desk organiser and the court operator running
// an individual league (everyone fights everyone, one table, the podium from
// the table). A tie at a place that matters is not the operator's decision in
// an individual league: the engine adds ippon-shobu bouts (tie-breakers, ids
// "Pool <x>-TB-<n>") by itself once every regular bout is finished
// (internal/engine/tiebreaker.go InjectTiebreakerMatches, documented in
// docs/user-guide/organisers/run-tournament.md). These journeys record what the
// two operators see when that happens, and walk the shapes around it:
//
//   cycle        a three-way tie at 1st-3rd: three ippon-shobu bouts are added;
//                what tells the desk and the court operator why (F12); the
//                round is then won in a cycle (A beats B, B beats C, C beats A):
//                the league stays open and the Pools notice offers another
//                round or lots (F5); another round adds a fresh three bouts,
//                won in a cycle again, and the desk enters the lots
//   F3b / D6 (b) a two-way tie at 1st: the editor refuses a drawn league
//                tie-breaker and encho, then hantei, decides it ("ippon-shobu"
//                shots on the court console and the desk)
//   joint 3rd    a tie at 3rd with "Award two joint 3rd places" on (no bout:
//                both share 3rd) and off (an ippon-shobu decides 3rd)
//   tie at 4th   a tie at 4th-5th: a tie at 4th or lower always shares its
//                place, so no bout is added and the two share 4th
//   F6           a league an earlier release saved with "Top 4" (a backup
//                restored as a copy): the same tie at 4th-5th, no bout, the
//                two sharing 4th
//   both barred  a tie at 1st between two competitors who both withdrew: the
//                court console lists their ippon-shobu but never as Up next,
//                records it drawn (neither can fight), and the desk draws lots
//   F12          the court console says why an ippon-shobu is on the court
//
// Court console evidence is named by the flow: <flow>-console-up-next,
// -running, -before-finish and -after on the shiaijo route, and
// <flow>-desk-table and <flow>-public-awards on the desk and the phone.
//
// Everything is seeded through the interface (fixtures/league.mjs seedLeague).
// The bouts are scripted by ROLE (a competitor's place in the roster), never by
// the draw's side: a league pairs everyone, so the draw only decides who stands
// Shiro, and a script keyed by the sorted pair holds whichever side the draw
// gives it. Expected figures are tallied from the scripts, never read back.
//
// Not serial: each test signs in (or creates the tournament when this file's
// server has none), seeds a competition of its own on a shiaijo of its own, and
// keeps its own folder of screenshots and audit rows, so one failing test
// hides nothing else here.
import fs from 'node:fs';
import os from 'node:os';
import path from 'node:path';
import { test, expect } from '../fixtures/test.mjs';
import { EXPECT_TIMEOUT_MS, PUBLIC_DEVICE } from '../fixtures/devices.mjs';
import { journeyShots } from '../fixtures/shots.mjs';
import { flushAudits, journeyRecorder } from '../fixtures/audit.mjs';
import { enterTournament } from '../fixtures/setup.mjs';
import { completedRowFor, openShiaijo, sides, upNextCard, waitingRows } from '../fixtures/shiaijo.mjs';
import {
  EDITOR, INLINE_EDITOR, awardIppon, finishMatch, markDraw, nextTapWrite, running, startMatch,
  writeLanded, writtenMatchId,
} from '../fixtures/scoring.mjs';
import { editorSides, ensureRunning, inlineRunning, recordDecision, rosterOf } from '../fixtures/individual.mjs';
import {
  TEXT, awardsTabOffered, backUpCompetition, circleNotice, drawnBoutNotices, leagueRanks, openLeague,
  openLeagueSettings, openNumberedRow, openViewerAwards, pairKey, readBanner, readChusen, readChusenRecorded,
  readLeagueSettings, readLeagueTable, readNumberedRows, readPodium, recordLots, restoreAsCopy, rewriteBackup,
  seedLeague, withFrontMatterLine,
} from '../fixtures/league.mjs';
import { openViewer } from '../fixtures/public.mjs';

// Enough shiaijo for every test here to have one of its own.
const CUP = { name: 'Individual League Cup', courts: 10 };
// The tournament password enterTournament signs in with (fixtures/setup.mjs).
const PASSWORD = 'e2e-operator';

// A tie-breaker bout (ippon-shobu) id, as the score write names it.
const TB_ID = /^Pool [^-]+-TB-\d+$/;
// A regular league bout reads "Match n of m"; a tie-breaker is the bare pairing.
const MATCH_N_OF_M = /Match \d+ of \d+/i;
// Words that would tell an operator why an extra bout appeared (F12).
const SAYS_TIEBREAK = /tie-?break|ippon[- ]shobu|decides? (the )?\d|place/i;

// A recorder per test: audit rows and screenshots in the journey's own folder.
function recorder(journey) {
  const folder = `league-individual/${journey}`;
  return { ...journeyRecorder(journey, folder), shot: journeyShots(folder) };
}

// ------------------------------------------------------------------ scripts

const zero = () => ({ W: 0, L: 0, D: 0, PW: 0, PL: 0 });
// The individual league ranking (CLAUDE.md, Key Algorithms): wins, then fewer
// losses, then more hikiwake, then more points scored, then fewer points lost.
// 0 means level on every criterion.
const order = (a, b) => (b.W - a.W) || (a.L - b.L) || (b.D - a.D) || (b.PW - a.PW) || (a.PL - b.PL);

// A script of bouts and the table it must make: `beat(w, l)` (one men to w),
// `draw(a, b)` (Mark draw, no points). `planFor({ shiro, aka })` turns a pairing
// the app shows into what to record on each side. `tally` is the table the
// script makes, before any tie-breaker (a tie-breaker does not count in it).
function scripted(names) {
  const plan = new Map();
  const tally = Object.fromEntries(names.map((n) => [n, zero()]));
  const beat = (w, l) => {
    plan.set(pairKey(w, l), { winner: w });
    tally[w].W += 1; tally[w].PW += 1;
    tally[l].L += 1; tally[l].PL += 1;
  };
  const draw = (a, b) => {
    plan.set(pairKey(a, b), { draw: true });
    tally[a].D += 1; tally[b].D += 1;
  };
  const planFor = ({ shiro, aka }) => {
    const entry = plan.get(pairKey(shiro, aka));
    if (!entry) throw new Error(`no scripted result for ${shiro} v ${aka}`);
    if (entry.draw) return { draw: true };
    return { side: entry.winner === shiro ? 'shiro' : 'aka' };
  };
  const ranked = () => [...names].sort((a, b) => order(tally[a], tally[b]));
  return { beat, draw, planFor, tally, ranked, size: () => plan.size };
}

// The script for a round of tie-breakers: `beat(w, l)` only.
function tiebreakScript() {
  const plan = new Map();
  return {
    beat: (w, l) => plan.set(pairKey(w, l), w),
    planFor: ({ shiro, aka }) => {
      const w = plan.get(pairKey(shiro, aka));
      if (!w) throw new Error(`no scripted tie-breaker for ${shiro} v ${aka}`);
      return { side: w === shiro ? 'shiro' : 'aka' };
    },
  };
}

// --------------------------------------------------------------- playing

// Record one scripted result on an editor already holding a running bout.
async function recordPlan(page, plan, root) {
  if (plan.draw) await markDraw(page, root);
  else await awardIppon(page, plan.side, 'M', root);
}

// The desk organiser: score every listed bout from the league's numbered rows
// on admin Pools, through the overlay editor each row opens. `nums` are the
// row numbers to play; returns what was played.
async function playOnPools(page, nums, planFor) {
  const played = [];
  for (const num of nums) {
    await openNumberedRow(page, { num });
    const pair = await editorSides(page, EDITOR);
    const start = page.locator(EDITOR).locator('button').filter({ hasText: /^Start match$/ }).first();
    if (await start.isVisible()) await startMatch(page);
    await running(page, EDITOR);
    const plan = planFor(pair);
    await recordPlan(page, plan, EDITOR);
    await finishMatch(page, EDITOR);
    played.push({ num, ...pair, plan });
  }
  return played;
}

// The court operator: wait until the console holds a bout between two of
// `among` (Up next, or already running after a Finish + Start Next), and
// return its pairing.
async function boutOnCourt(page, among) {
  let pair = null;
  await expect.poll(async () => {
    pair = (await inlineRunning(page))
      ? await editorSides(page).catch(() => null)
      : await sides(upNextCard(page)).catch(() => null);
    return !!pair && among.includes(pair.shiro) && among.includes(pair.aka);
  }, { message: `the court holds a bout between two of ${among.join(', ')}`, timeout: EXPECT_TIMEOUT_MS }).toBe(true);
  return pair;
}

// Play `count` scripted bouts on the court console, each between two of
// `among`, and return them, each with the match id its first write named.
async function playOnCourt(page, count, among, planFor) {
  const played = [];
  for (let n = 0; n < count; n += 1) {
    await boutOnCourt(page, among);
    const pair = await ensureRunning(page);
    if (!pair) throw new Error(`playOnCourt: nothing to start for bout ${n + 1} of ${count}`);
    const plan = planFor(pair);
    let id = null;
    if (plan.draw) {
      await markDraw(page);
    } else {
      const wrote = nextTapWrite(page);
      await awardIppon(page, plan.side, 'M');
      id = writtenMatchId(await writeLanded(wrote, `${pair.shiro} v ${pair.aka}'s first mark`));
    }
    await finishMatch(page);
    await expect(completedRowFor(page, pair)).toBeVisible();
    played.push({ ...pair, plan, id });
  }
  return played;
}

// ---------------------------------------------------------------- reading

// What the desk reads on admin Pools: the table, the numbered rows, the
// team-only tie-break banner and chusen panel (expected absent here), the
// drawn-bout notice the fix adds, and any words on the page that name a
// tie-breaker.
async function readDesk(page) {
  const table = await readLeagueTable(page);
  return {
    table,
    finished: table.heading === 'Final standings',
    numberedRows: await readNumberedRows(page),
    banner: await readBanner(page),
    chusen: await readChusen(page),
    drawnNotices: await drawnBoutNotices(page),
    // The app draws no <main>, so the words are read from the whole body.
    tiebreakWords: (await page.locator('body').getByText(/tie-?break|ippon[- ]shobu|chusen|drawing lots/i).allInnerTexts())
      .map((t) => t.replace(/\s+/g, ' ').trim()),
  };
}

// What the court operator reads: the Up next card, the Upcoming rows, and the
// running bout's eyebrow when one runs.
async function readCourt(page) {
  const card = upNextCard(page);
  const runningNow = await inlineRunning(page);
  return {
    upNext: (await card.count()) ? (await card.first().innerText()).replace(/\s+/g, ' ').trim() : '',
    waiting: (await waitingRows(page).allInnerTexts()).map((t) => t.replace(/\s+/g, ' ').trim()),
    running: runningNow,
    eyebrow: runningNow ? ((await page.locator(INLINE_EDITOR).first().locator('.editor-modal__eyebrow').allInnerTexts())[0] || '').trim() : '',
  };
}

// Wait until the desk's league table reads the competition as finished (or
// not), and return the desk read.
async function deskOnceFinished(page, finished = true) {
  await expect.poll(async () => (await readLeagueTable(page)).heading, {
    message: finished ? 'the league table reads Final standings' : 'the league table reads Standings',
    timeout: EXPECT_TIMEOUT_MS,
  }).toBe(finished ? 'Final standings' : 'Standings');
  return readDesk(page);
}

// The numbered rows of the tie-breakers (the rows after the regular ones).
const tiebreakRows = (rows, regular) => rows.filter((r) => r.num > regular);

// Reopen the league until `arrived(desk)` holds and return that read. When it
// never does inside `windowMs` the last read comes back instead: the outcome
// is then an absence (nothing was added, nothing finished), which has no state
// to wait for, so the window's end is the evidence (Deliberate: a negative).
// The default window is the suite's wait for a state to arrive, so a slow
// engine is not recorded as an outcome; a caller whose outcome IS the absence
// passes a short one. The reopens are spaced so the loop does not hammer the
// server.
async function deskWhen(page, id, arrived, windowMs = EXPECT_TIMEOUT_MS) {
  const deadline = Date.now() + windowMs;
  for (;;) {
    await openLeague(page, id);
    const desk = await readDesk(page);
    if (arrived(desk) || Date.now() >= deadline) return desk;
    await page.waitForTimeout(500);
  }
}

// The public Awards tab of the competition, read on a spectator's phone; with
// `shot` and `step`, the Awards tab is also captured as `step`.
async function publicPodiumIfOffered(browser, baseURL, id, { shot, step } = {}) {
  const spectator = await browser.newContext({ ...PUBLIC_DEVICE, baseURL });
  try {
    const phone = await spectator.newPage();
    await openViewer(phone, id);
    if (!(await awardsTabOffered(phone))) return { offered: false, podium: null };
    await openViewerAwards(phone, id);
    const podium = await readPodium(phone);
    return { offered: true, podium, ...(shot ? { screenshot: await shot(phone, step, { fullPage: true }) } : {}) };
  } finally {
    await spectator.close();
  }
}

// Wait a moment for a tie-breaker that must NOT come: the table must read the
// league finished with no row past the regular ones, held for a few seconds so
// a late injection is not missed.
async function expectNoTiebreak(page, id, regular) {
  await expect.poll(async () => (await readLeagueTable(page)).heading, { timeout: EXPECT_TIMEOUT_MS }).toBe('Final standings');
  // Deliberate: a NEGATIVE. Nothing is to be added, so there is no state to
  // wait for and the time is the evidence.
  await page.waitForTimeout(2_000);
  await openLeague(page, id);
  expect(tiebreakRows(await readNumberedRows(page), regular), 'no tie-breaker bout was added').toEqual([]);
}

test.describe('league-individual', () => {
  test.afterAll(() => flushAudits(test.info().file));

  // ------------------------------------------------------------- 1. cycle
  // Four entrants: A beats B, B beats C, C beats A, and all three beat D, so
  // A, B and C end level on every criterion (W2 L1 D0 PW2 PL1) at 1st-3rd.
  test('a three-way tie at 1st: ippon-shobu bouts are added by themselves (F12), won in a cycle twice, then settled by lots', async ({ page, browser, baseURL }) => {
    test.setTimeout(600_000);
    const { record, shot } = recorder('cycle');
    const names = rosterOf('Cy', 4).map(([n]) => n);
    const [A, B, C, D] = names;
    const s = scripted(names);
    s.beat(A, B); s.beat(B, C); s.beat(C, A);
    s.beat(A, D); s.beat(B, D); s.beat(C, D);
    expect(order(s.tally[A], s.tally[B]) || order(s.tally[B], s.tally[C]), 'A, B and C are level on every criterion').toBe(0);
    let id;

    await test.step('seed: an individual league of four on shiaijo A', async () => {
      await enterTournament(page, CUP);
      id = await seedLeague(page, { name: 'Cycle League', kind: 'individual', entrants: rosterOf('Cy', 4), courts: ['A'] });
      await openLeague(page, id);
      const desk = await readDesk(page);
      record({ step: 'pools', action: 'read before any result', ...desk });
      expect(desk.numberedRows).toHaveLength(6);
      expect(desk.banner.shown, 'an individual league has no tie-break banner').toBe(false);
    });

    await test.step('desk: score the six regular bouts from the numbered rows', async () => {
      const played = await playOnPools(page, [1, 2, 3, 4, 5, 6], s.planFor);
      record({ step: 'pools', action: 'six regular bouts from the numbered rows', played });
    });

    let desk;
    await test.step('desk: three ippon-shobu bouts appear, between A, B and C', async () => {
      await expect.poll(async () => (await readNumberedRows(page)).length, {
        message: 'three tie-breaker bouts are added once the last regular bout is finished', timeout: EXPECT_TIMEOUT_MS,
      }).toBe(9);
      desk = await readDesk(page);
      // Whether the table shows the three as level (one shared rank) while the
      // tie-breakers wait, or numbers them apart: recorded for the findings.
      const levelRanks = [A, B, C].map((n) => desk.table.rows.find((r) => r.name === n).rank);
      record({ step: 'pools', action: 'read once the tie-breakers are added', levelRanks, ...desk,
        shot: await shot(page, 'pools-tiebreakers-added', { fullPage: true }) });
      const tbs = tiebreakRows(desk.numberedRows, 6);
      expect(tbs.map((r) => pairKey(r.shiro, r.aka)).sort()).toEqual([pairKey(A, B), pairKey(A, C), pairKey(B, C)].sort());
      expect(desk.finished).toBe(false);
      expect(desk.banner.shown).toBe(false);
      for (const n of names) {
        expect(desk.table.rows.find((r) => r.name === n), `${n}'s figures`).toMatchObject(s.tally[n]);
      }
    });

    let court;
    await test.step('court: the console queues the ippon-shobu; does anything say why (F12)?', async () => {
      await openShiaijo(page, 'A');
      const pair = await boutOnCourt(page, [A, B, C]);
      court = await readCourt(page);
      const explains = SAYS_TIEBREAK.test(`${court.upNext} ${court.waiting.join(' ')}`);
      record({ step: 'court', action: 'the console with the tie-breakers queued', finding: 'F12', pair, ...court, explainsWhy: explains,
        shot: await shot(page, 'court-tiebreaker-queued', { fullPage: true }) });
      expect(court.upNext, 'a tie-breaker carries no "Match n of m"').not.toMatch(MATCH_N_OF_M);
    });

    let tbs;
    await test.step('court: win the round in a cycle (A beats B, B beats C, C beats A)', async () => {
      const t = tiebreakScript();
      t.beat(A, B); t.beat(B, C); t.beat(C, A);
      tbs = await playOnCourt(page, 3, [A, B, C], t.planFor);
      record({ step: 'court', action: 'the three tie-breakers, won in a cycle', played: tbs, court: await readCourt(page),
        shot: await shot(page, 'court-after-cycle', { fullPage: true }) });
      for (const b of tbs) expect(b.id, `${b.shiro} v ${b.aka} is a tie-breaker`).toMatch(TB_ID);
    });

    // F5: the cycle leaves the league open and nothing is added by itself; the
    // Pools notice names the three and offers another round (the main step) or
    // lots (the operator's other choice).
    const notice = () => circleNotice(page);
    await test.step('desk: the Pools notice says the round left a tie and offers another round or lots', async () => {
      await openLeague(page, id);
      await expect(notice(), 'the notice names the tie the round left').toBeVisible({ timeout: EXPECT_TIMEOUT_MS });
      const after = await readDesk(page);
      const group = notice().locator('.league-tiebreak__group');
      await expect(group.locator('.league-tiebreak__teams')).toHaveText([A, B, C].join(' · '), { useInnerText: true });
      await expect(notice().getByRole('button', { name: 'Run another tie-breaker round' })).toBeVisible();
      await expect(notice().getByRole('button', { name: 'Draw lots instead' })).toBeVisible();
      expect(after.finished, 'the cycle leaves the league open').toBe(false);
      expect(tiebreakRows(after.numberedRows, 9), 'no further round is added by itself').toEqual([]);
      const pub = await publicPodiumIfOffered(browser, baseURL, id);
      record({ step: 'pools', action: 'the notice after the cycle', ranks: await leagueRanks(page), ...after, public: pub,
        shot: await shot(page, 'pools-after-cycle', { fullPage: true }) });
      expect(pub.offered, 'no podium while the cycle is unbroken').toBe(false);
    });

    await test.step('desk: Draw lots instead opens the lots entry, still offering another round', async () => {
      await notice().getByRole('button', { name: 'Draw lots instead' }).tap();
      await expect(notice().getByRole('button', { name: 'Draw lots instead' })).toHaveCount(0);
      await expect(notice().getByRole('button', { name: 'Run another tie-breaker round' })).toBeVisible();
      record({ step: 'pools', action: 'the lots entry opened from the notice', shot: await shot(page, 'pools-circle-lots-open', { fullPage: true }) });
    });

    await test.step('desk: Run another tie-breaker round adds a fresh round among the three', async () => {
      await notice().getByRole('button', { name: 'Run another tie-breaker round' }).tap();
      await expect.poll(async () => (await readNumberedRows(page)).length, {
        message: 'three more tie-breaker bouts are added', timeout: EXPECT_TIMEOUT_MS,
      }).toBe(12);
      const rows = await readNumberedRows(page);
      expect(tiebreakRows(rows, 9).map((r) => pairKey(r.shiro, r.aka)).sort()).toEqual([pairKey(A, B), pairKey(A, C), pairKey(B, C)].sort());
      record({ step: 'pools', action: 'a fresh round added among the three', numberedRows: rows,
        shot: await shot(page, 'pools-another-round-added', { fullPage: true }) });
      await expect(notice(), 'the notice is gone while the new round is unfought').toHaveCount(0);
    });

    await test.step('court: the second round is won in a cycle too', async () => {
      await openShiaijo(page, 'A');
      const t = tiebreakScript();
      t.beat(A, B); t.beat(B, C); t.beat(C, A);
      const again = await playOnCourt(page, 3, [A, B, C], t.planFor);
      record({ step: 'court', action: 'the second round, won in a cycle', played: again,
        shot: await shot(page, 'cycle-console-after', { fullPage: true }) });
      for (const b of again) expect(b.id, `${b.shiro} v ${b.aka} is a tie-breaker`).toMatch(TB_ID);
    });

    // The lots, in an order that is not the name order, so the table shows them.
    const lots = [C, A, B];
    await test.step('desk: Draw lots instead, the order entered, and the league finishes', async () => {
      await openLeague(page, id);
      await expect(notice(), 'the notice names the tie the second round left').toBeVisible({ timeout: EXPECT_TIMEOUT_MS });
      await notice().getByRole('button', { name: TEXT.drawLotsInstead }).tap();
      record({ step: 'pools', action: 'the lots entry after the second cycle',
        shot: await shot(page, 'cycle-desk-lots', { fullPage: true }) });
      const res = await recordLots(page, lots);
      const desk = await deskOnceFinished(page);
      const ranks = await leagueRanks(page);
      const recorded = await readChusenRecorded(page);
      record({ step: 'pools', action: 'lots recorded', answer: { status: res.status }, ranks, recorded, ...desk,
        shot: await shot(page, 'cycle-desk-table', { fullPage: true }) });
      expect(ranks).toEqual({ [C]: 1, [A]: 2, [B]: 3, [D]: 4 });
      expect(desk.table.winner).toBe(C);
      for (const n of lots) expect(desk.table.rows.find((r) => r.name === n).overridden, `${n}'s place came from the lots`).toBe(true);
      expect(recorded.map((r) => r.order)).toEqual([`1st ${C}, 2nd ${A}, 3rd ${B}`]);
      const pub = await publicPodiumIfOffered(browser, baseURL, id, { shot, step: 'cycle-public-awards' });
      record({ step: 'public', action: 'Awards after the lots', ...pub });
      expect(pub.offered).toBe(true);
      expect(pub.podium.places.slice(0, 3).map((pl) => pl.name)).toEqual(lots);
    });
  });

  // ------------------------------------------------------- 2. F3b / D6 (b)
  // Three entrants: A and B draw, both beat C, so A and B end level at 1st.
  // The editor treats a league tie-breaker as a knockout bout: no draw, encho,
  // then hantei.
  test('F3b: a league ippon-shobu cannot be saved drawn; encho, then hantei, decides it and the league finishes', async ({ page, browser, baseURL }) => {
    test.setTimeout(360_000);
    const { record, shot } = recorder('tiebreaker-needs-a-winner');
    const names = rosterOf('Ht', 3).map(([n]) => n);
    const [A, B, C] = names;
    const s = scripted(names);
    s.draw(A, B); s.beat(A, C); s.beat(B, C);
    let id;

    await test.step('seed and play the three regular bouts on shiaijo C', async () => {
      await enterTournament(page, CUP);
      id = await seedLeague(page, { name: 'Hantei League', kind: 'individual', entrants: rosterOf('Ht', 3), courts: ['C'] });
      await openShiaijo(page, 'C');
      await playOnCourt(page, 3, names, s.planFor);
    });

    const flow = 'ippon-shobu';
    const shots = {};
    await test.step('the tie-breaker\'s editor offers no draw, and the x key marks none', async () => {
      await boutOnCourt(page, [A, B]);
      const court = await readCourt(page);
      shots.upNext = await shot(page, `${flow}-console-up-next`, { fullPage: true });
      record({ step: 'court', action: 'the ippon-shobu Up next', ...court, screenshot: shots.upNext });
      expect(court.upNext).toMatch(/tie-?break/i);
      await ensureRunning(page);
      shots.running = await shot(page, `${flow}-console-running`, { fullPage: true });
      const ed = page.locator(INLINE_EDITOR).first();
      const toggle = ed.getByTestId('scoring-modal-mark-draw');
      record({ step: 'court', action: 'the tie-breaker\'s Mark draw', disabled: await toggle.isDisabled(), title: await toggle.getAttribute('title'),
        shot: await shot(page, 'tiebreaker-no-draw') });
      await expect(toggle).toBeDisabled();
      await expect(toggle).toHaveAttribute('title', /tie-breaker needs a winner/i);
      await page.keyboard.press('x');
      await expect(toggle).toHaveText('Mark draw');
    });

    await test.step('level at one men each, Finish asks for a winner', async () => {
      await awardIppon(page, 'shiro', 'M');
      await awardIppon(page, 'aka', 'M');
      const blocked = page.locator(INLINE_EDITOR).first().locator('button').filter({ hasText: 'Needs a winner' }).first();
      record({ step: 'court', action: 'Finish on a level tie-breaker', label: (await blocked.innerText().catch(() => '')).trim(),
        title: await blocked.getAttribute('title').catch(() => null), shot: await shot(page, 'tiebreaker-level') });
      await expect(blocked).toBeDisabled();
    });

    await test.step('encho, then hantei for A: the league finishes with A first', async () => {
      const ed = page.locator(INLINE_EDITOR).first();
      await ed.getByTestId('scoring-modal-encho-pill').tap();
      await ed.getByTestId('scoring-modal-encho-checkbox').check();
      await ed.getByTestId('scoring-modal-hantei-arm').tap();
      const pair = await editorSides(page);
      await ed.getByTestId(pair.shiro === A ? 'scoring-modal-hantei-shiro' : 'scoring-modal-hantei-aka').tap();
      shots.beforeFinish = await shot(page, `${flow}-console-before-finish`, { fullPage: true });
      await finishMatch(page);
      await expect(completedRowFor(page, pair)).toBeVisible();
      shots.after = await shot(page, `${flow}-console-after`, { fullPage: true });
      await openLeague(page, id);
      const desk = await deskOnceFinished(page);
      const pub = await publicPodiumIfOffered(browser, baseURL, id, { shot, step: `${flow}-public-awards` });
      record({ step: 'pools', action: 'after the hantei', ...desk, public: pub, shots,
        shot: await shot(page, `${flow}-desk-table`, { fullPage: true }) });
      expect(desk.table.winner).toBe(A);
      expect(await leagueRanks(page)).toEqual({ [A]: 1, [B]: 2, [C]: 3 });
      expect(pub.offered).toBe(true);
      expect(pub.podium.places.slice(0, 3).map((pl) => pl.name)).toEqual([A, B, C]);
    });
  });

  // ------------------------------------------------------ 3/4. joint 3rd
  // Four entrants: A beats everyone, B beats C and D, C and D draw, so C and D
  // end level (W0 L2 D1 PW0 PL2) at 3rd-4th.
  const jointThird = (prefix) => {
    const names = rosterOf(prefix, 4).map(([n]) => n);
    const [A, B, C, D] = names;
    const s = scripted(names);
    s.beat(A, B); s.beat(A, C); s.beat(A, D);
    s.beat(B, C); s.beat(B, D);
    s.draw(C, D);
    return { names, A, B, C, D, s };
  };

  test('a tie at 3rd with two joint 3rd places on: no ippon-shobu, both share 3rd', async ({ page, browser, baseURL }) => {
    test.setTimeout(420_000);
    const { record, shot } = recorder('joint-third-on');
    const { names, A, B, C, D, s } = jointThird('Jn');
    let id;

    await test.step('seed with "Award two joint 3rd places" on and play the six bouts on shiaijo D', async () => {
      await enterTournament(page, CUP);
      id = await seedLeague(page, { name: 'Joint Third League', kind: 'individual', entrants: rosterOf('Jn', 4), courts: ['D'], twoThirdPlaces: true });
      await openShiaijo(page, 'D');
      record({ step: 'court', action: 'six regular bouts', played: await playOnCourt(page, 6, names, s.planFor) });
    });

    await test.step('the league finishes with no tie-breaker; C and D share 3rd', async () => {
      await openLeague(page, id);
      await expectNoTiebreak(page, id, 6);
      const desk = await readDesk(page);
      const ranks = await leagueRanks(page);
      const pub = await publicPodiumIfOffered(browser, baseURL, id);
      record({ step: 'pools', action: 'read once finished', ranks, ...desk, public: pub, shot: await shot(page, 'pools-joint-third-on', { fullPage: true }) });
      expect(desk.table.winner).toBe(A);
      expect(ranks[A]).toBe(1);
      expect(ranks[B]).toBe(2);
      expect([ranks[C], ranks[D]], 'C and D share 3rd').toEqual([3, 3]);
    });
  });

  test('a tie at 3rd with two joint 3rd places off: an ippon-shobu decides 3rd', async ({ page, browser, baseURL }) => {
    test.setTimeout(420_000);
    const { record, shot } = recorder('joint-third-off');
    const { names, A, B, C, D, s } = jointThird('Jf');
    let id;

    await test.step('seed with "Award two joint 3rd places" off and play the six bouts on shiaijo E', async () => {
      await enterTournament(page, CUP);
      id = await seedLeague(page, { name: 'Bronze Bout League', kind: 'individual', entrants: rosterOf('Jf', 4), courts: ['E'], twoThirdPlaces: false });
      await openShiaijo(page, 'E');
      record({ step: 'court', action: 'six regular bouts', played: await playOnCourt(page, 6, names, s.planFor) });
    });

    await test.step('an ippon-shobu between C and D is queued; C wins it and takes 3rd', async () => {
      const queued = await boutOnCourt(page, [C, D]);
      record({ step: 'court', action: 'the tie-breaker for 3rd queued', pair: queued, ...(await readCourt(page)),
        shot: await shot(page, 'court-third-place-tiebreaker', { fullPage: true }) });
      const t = tiebreakScript();
      t.beat(C, D);
      const [tb] = await playOnCourt(page, 1, [C, D], t.planFor);
      expect(tb.id).toMatch(TB_ID);
      await openLeague(page, id);
      const desk = await deskOnceFinished(page);
      const ranks = await leagueRanks(page);
      const pub = await publicPodiumIfOffered(browser, baseURL, id);
      record({ step: 'pools', action: 'read once finished', ranks, ...desk, public: pub, shot: await shot(page, 'pools-joint-third-off', { fullPage: true }) });
      expect(desk.table.winner).toBe(A);
      expect(ranks).toMatchObject({ [A]: 1, [B]: 2, [C]: 3, [D]: 4 });
    });
  });

  // ------------------------------------------------------- 5. tie at 4th
  // Five entrants: A beats everyone, B beats C, D and E, C beats D and E, D
  // and E draw, so D and E end level (W0 L3 D1 PW0 PL3) at 4th-5th: a tie at
  // 4th or lower always shares its place. Two joint 3rd places is off, so the
  // 3rd-place rule plays no part in the outcome.
  const belowBand = (prefix) => {
    const names = rosterOf(prefix, 5).map(([n]) => n);
    const [A, B, C, D, E] = names;
    const s = scripted(names);
    s.beat(A, B); s.beat(A, C); s.beat(A, D); s.beat(A, E);
    s.beat(B, C); s.beat(B, D); s.beat(B, E);
    s.beat(C, D); s.beat(C, E);
    s.draw(D, E);
    return { names, A, B, C, D, E, s };
  };

  test('a tie at 4th-5th: no ippon-shobu, the two share 4th and the league finishes', async ({ page }) => {
    test.setTimeout(600_000);
    const { record, shot } = recorder('below-band');
    const { names, A, B, C, D, E, s } = belowBand('Bb');
    let id;

    await test.step('seed and play the ten bouts on shiaijo F', async () => {
      await enterTournament(page, CUP);
      id = await seedLeague(page, { name: 'Below Band League', kind: 'individual', entrants: rosterOf('Bb', 5), courts: ['F'], twoThirdPlaces: false });
      await openShiaijo(page, 'F');
      record({ step: 'court', action: 'ten regular bouts', played: await playOnCourt(page, 10, names, s.planFor) });
    });

    await test.step('no tie-breaker is added for 4th-5th; the league finishes', async () => {
      await openLeague(page, id);
      await expectNoTiebreak(page, id, 10);
      const desk = await readDesk(page);
      const ranks = await leagueRanks(page);
      record({ step: 'pools', action: 'read once finished', ranks, ...desk,
        shot: await shot(page, 'pools-below-band', { fullPage: true }) });
      // A tie at 4th is a shared place: D and E both read their better
      // place, 4th (N2b).
      expect(ranks).toMatchObject({ [A]: 1, [B]: 2, [C]: 3, [D]: 4, [E]: 4 });
      for (const n of [D, E]) expect(desk.table.rows.find((r) => r.name === n), `${n}'s figures`).toMatchObject(s.tally[n]);
    });
  });

  // --------------------------------------------------------------- 6. F6
  // A league an earlier release saved with "Break ties for top: Top 4" reaches
  // the app as that tournament would: its backup, the old key in config.md,
  // restored as a copy. No band is offered any more, so the copy breaks ties at
  // 1st to 3rd like any league, and the level pair at 4th-5th shares 4th. The
  // copy is played from the desk's numbered rows: the league it was copied
  // from is on the same shiaijo with the same names, so the court console
  // could start either.
  test('F6: a league saved with Top 4 reads as any other: a tie at 4th-5th adds no ippon-shobu and the two share 4th', async ({ page }) => {
    test.setTimeout(600_000);
    const { record, shot } = recorder('f6-saved-top4');
    const { A, B, C, D, E, s } = belowBand('Fb');
    const name = 'Saved Top 4 League';
    const work = fs.mkdtempSync(path.join(os.tmpdir(), 'league-individual-top4-'));
    let id;

    try {
      await test.step('seed on shiaijo H, back it up with "league_tiebreak_top_n: 4" and restore it as a copy', async () => {
        await enterTournament(page, CUP);
        await seedLeague(page, { name, kind: 'individual', entrants: rosterOf('Fb', 5), courts: ['H'], twoThirdPlaces: false });
        const file = await backUpCompetition(page, name, work);
        const forged = path.join(work, 'saved-top4.zip');
        const rewritten = rewriteBackup(file, forged, { '/config.md': (text) => withFrontMatterLine(text, 'league_tiebreak_top_n: 4') });
        id = await restoreAsCopy(page, { archive: forged, name, copyName: `${name} Copy`, password: PASSWORD });
        await openLeagueSettings(page, id);
        const settings = await readLeagueSettings(page);
        record({ step: 'settings', action: 'the restored copy', rewritten, copyId: id, ...settings,
          shot: await shot(page, 'f6-copy-settings', { fullPage: true }) });
        expect(settings.retired, 'no band is offered').toEqual({ tiebreakTop: 0, sharedRanks: 0 });
      });

      await test.step('desk: score the ten bouts of the copy from its numbered rows', async () => {
        await openLeague(page, id);
        const played = await playOnPools(page, Array.from({ length: 10 }, (_, k) => k + 1), s.planFor);
        record({ step: 'pools', action: 'ten regular bouts', played });
      });

      await test.step('no ippon-shobu is added for 4th-5th; the league finishes with D and E sharing 4th', async () => {
        await expectNoTiebreak(page, id, 10);
        const desk = await readDesk(page);
        const ranks = await leagueRanks(page);
        record({ step: 'pools', action: 'read once finished', ranks, ...desk,
          shot: await shot(page, 'f6-desk-table', { fullPage: true }) });
        expect(ranks).toMatchObject({ [A]: 1, [B]: 2, [C]: 3, [D]: 4, [E]: 4 });
        expect(desk.table.winner).toBe(A);
      });
    } finally {
      fs.rmSync(work, { recursive: true, force: true });
    }
  });

  // ------------------------------------------------------- 7. both barred
  // Five entrants. A and B draw, both beat C and D; C and D draw, both beat E;
  // then, LAST, A and B each withdraw (kiken) in their bout against E, so the
  // bar closes nothing else. A and B end level at 1st (W2 L1 D1, the same
  // points), E 3rd (W2 L2), C and D level at 4th (shared, nothing asked). The
  // engine adds the A v B ippon-shobu; neither can fight it, so the court
  // console lists it but never as Up next, the operator records it drawn, and
  // the desk draws lots for 1st.
  test('both sides barred at 1st: the console lists the ippon-shobu but never as Up next, records it drawn, and lots decide 1st', async ({ page, browser, baseURL }) => {
    test.setTimeout(600_000);
    const { record, shot } = recorder('both-barred');
    const flow = 'both-barred';
    const roster = rosterOf('Bw', 5);
    const names = roster.map(([n]) => n);
    const [A, B, C, D, E] = names;
    const s = scripted(names);
    s.draw(A, B); s.beat(A, C); s.beat(A, D); s.beat(B, C); s.beat(B, D);
    s.draw(C, D); s.beat(C, E); s.beat(D, E);
    const lots = [B, A];
    const court = 'J';
    let id;

    await test.step('seed on shiaijo J; the desk scores the eight bouts that are not against E for A or B', async () => {
      await enterTournament(page, CUP);
      id = await seedLeague(page, { name: 'Both Barred League', kind: 'individual', entrants: roster, courts: [court] });
      await openLeague(page, id);
      const rows = await readNumberedRows(page);
      expect(rows).toHaveLength(10);
      const withdrawals = new Set([pairKey(A, E), pairKey(B, E)]);
      const nums = rows.filter((r) => !withdrawals.has(pairKey(r.shiro, r.aka))).map((r) => r.num);
      record({ step: 'pools', action: 'eight regular bouts', played: await playOnPools(page, nums, s.planFor) });
    });

    await test.step('desk: A, then B, withdraws (kiken) in the bout against E', async () => {
      for (const [who, playedAfter] of [[A, 9], [B, 10]]) {
        const row = (await readNumberedRows(page)).find((r) => pairKey(r.shiro, r.aka) === pairKey(who, E));
        await openNumberedRow(page, { num: row.num });
        const pair = await editorSides(page, EDITOR);
        const start = page.locator(EDITOR).locator('button').filter({ hasText: /^Start match$/ }).first();
        if (await start.isVisible()) await startMatch(page);
        await running(page, EDITOR);
        const kiken = await recordDecision(page, 'kiken-voluntary', { side: pair.shiro === who ? 'shiro' : 'aka', root: EDITOR });
        const close = page.locator(EDITOR).getByRole('button', { name: /✕ Close/ });
        if (await close.isVisible().catch(() => false)) await close.tap();
        await expect.poll(async () => (await readLeagueTable(page)).played, { message: `${who} v ${E} is counted` }).toBe(playedAfter);
        record({ step: 'pools', action: `${who} withdraws against ${E}`, pair, ...kiken });
      }
    });

    let desk;
    await test.step('desk: the table ties A and B at 1st and the engine adds their ippon-shobu', async () => {
      desk = await deskWhen(page, id, (d) => tiebreakRows(d.numberedRows, 10).length > 0);
      const rowOf = (n) => desk.table.rows.find((r) => r.name === n);
      record({ step: 'pools', action: 'once every regular bout is recorded', ranks: await leagueRanks(page), ...desk,
        shot: await shot(page, `${flow}-desk-before`, { fullPage: true }) });
      // The two withdrawals are A's and B's losses: A and B are level on
      // every figure, whatever points a kiken gives.
      for (const k of ['W', 'L', 'D', 'PW', 'PL']) expect(rowOf(A)[k], `A and B level on ${k}`).toBe(rowOf(B)[k]);
      expect(rowOf(A)).toMatchObject({ W: 2, L: 1, D: 1 });
      const tbs = tiebreakRows(desk.numberedRows, 10);
      expect(tbs.map((r) => pairKey(r.shiro, r.aka))).toEqual([pairKey(A, B)]);
      expect(desk.finished).toBe(false);
    });

    await test.step('court: the ippon-shobu is listed with the barred notice and is never Up next', async () => {
      await openShiaijo(page, court);
      const row = page.locator('.shiaijo-qrow').filter({ has: page.getByTestId('barred-match-notice') });
      await expect(row, 'the ippon-shobu is listed with why it cannot start').toHaveCount(1);
      const notice = row.getByTestId('barred-match-notice');
      await expect(notice).toContainText(TEXT.bothBarred);
      const court1 = await readCourt(page);
      record({ step: 'court', action: 'the console with the barred ippon-shobu', ...court1,
        notice: (await notice.innerText()).replace(/\s+/g, ' ').trim(),
        screenshot: await shot(page, `${flow}-console-up-next`, { fullPage: true }) });
      expect(await upNextCard(page).count(), 'a bout neither side can fight is never Up next').toBe(0);
      expect(court1.running).toBe(false);
      await expect(notice.getByRole('button', { name: TEXT.recordDrawn })).toBeVisible();
      // This bout never runs (neither side can fight it), so the flow has no
      // "-running" shot: the operator goes from the queue to Record as drawn.
      // The shot before that tap is the viewport on the barred row with the
      // button in view, not the full-page queue the up-next shot already holds.
      await notice.scrollIntoViewIfNeeded();
      record({ step: 'court', action: 'before Record as drawn (the bout never runs)',
        screenshot: await shot(page, `${flow}-console-before-finish`) });
      await notice.getByRole('button', { name: TEXT.recordDrawn }).tap();
      await expect(completedRowFor(page, { shiro: desk.numberedRows.at(-1).shiro, aka: desk.numberedRows.at(-1).aka })).toBeVisible();
      record({ step: 'court', action: 'after Record as drawn', ...(await readCourt(page)),
        screenshot: await shot(page, `${flow}-console-after`, { fullPage: true }) });
    });

    await test.step('desk: the lots entry is drawn at once, the order entered, and the league finishes', async () => {
      await openLeague(page, id);
      await expect(circleNotice(page), 'the notice for the tie the drawn bout left').toBeVisible({ timeout: EXPECT_TIMEOUT_MS });
      const offersAnotherRound = await circleNotice(page).getByRole('button', { name: TEXT.anotherRound }).count();
      record({ step: 'pools', action: 'the notice after the drawn ippon-shobu', offersAnotherRound,
        screenshot: await shot(page, `${flow}-desk-banner`, { fullPage: true }) });
      expect(offersAnotherRound, 'neither can fight another round').toBe(0);
      const res = await recordLots(page, lots);
      const after = await deskOnceFinished(page);
      const ranks = await leagueRanks(page);
      record({ step: 'pools', action: 'lots recorded', answer: { status: res.status }, ranks, recorded: await readChusenRecorded(page), ...after,
        shot: await shot(page, `${flow}-desk-table`, { fullPage: true }) });
      expect(ranks).toMatchObject({ [B]: 1, [A]: 2, [E]: 3, [C]: 4, [D]: 4 });
      expect(after.table.winner).toBe(B);
    });

    await test.step('public: the Awards follow the lots', async () => {
      const pub = await publicPodiumIfOffered(browser, baseURL, id, { shot, step: `${flow}-public-awards` });
      record({ step: 'public', action: 'Awards after the lots', ...pub });
      expect(pub.offered).toBe(true);
      expect(pub.podium.places.slice(0, 3).map((pl) => pl.name)).toEqual([B, A, E]);
    });
  });

  // -------------------------------------------------------------- F12
  test('F12: the court console says the ippon-shobu decides 1st place', async ({ page }) => {
    test.setTimeout(360_000);
    const { record, shot } = recorder('f12-fixed');
    const names = rosterOf('Wy', 3).map(([n]) => n);
    const [A, B, C] = names;
    const s = scripted(names);
    s.draw(A, B); s.beat(A, C); s.beat(B, C);

    await test.step('seed and play the three regular bouts on shiaijo I', async () => {
      await enterTournament(page, CUP);
      await seedLeague(page, { name: 'Why League', kind: 'individual', entrants: rosterOf('Wy', 3), courts: ['I'] });
      await openShiaijo(page, 'I');
      await playOnCourt(page, 3, names, s.planFor);
    });

    await test.step('the tie-breaker\'s row names it a tie-breaker and the place it decides', async () => {
      await boutOnCourt(page, [A, B]);
      const court = await readCourt(page);
      record({ step: 'court', action: 'the tie-breaker queued', ...court, shot: await shot(page, 'court-why-tiebreaker', { fullPage: true }) });
      expect(court.upNext).toMatch(/tie-?break/i);
      expect(court.upNext).toMatch(/\b1st\b/);
    });
  });
});

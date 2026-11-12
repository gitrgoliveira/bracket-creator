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
//                does a further round come, does the league strand, or does it
//                finish? Recorded, not asserted: the outcome is a question for
//                the boundary (chusen is team-only, internal/engine/chusen.go)
//   drawn today  a two-way tie at 1st, its ippon-shobu saved as a draw (today's
//                editor allows it): what the operator sees, then whether the
//                bout can be reopened from Pools and given a winner (F3b)
//   F3b / D6 (b) fixme: the editor refuses a drawn league tie-breaker and
//                encho, then hantei, decides it
//   joint 3rd    a tie at 3rd with "Award two joint 3rd places" on (no bout:
//                both share 3rd) and off (an ippon-shobu decides 3rd)
//   below band   a tie at 4th-5th: below the band of 3, so no bout is added
//   F6           a team league saved with "Top 4", switched to individual
//                before the roster: today's hidden band of 4 (recorded), and
//                the fixed band of 3 (fixme)
//   F12 fixme    the court console says why an ippon-shobu is on the court
//
// Everything is seeded through the interface (fixtures/league.mjs seedLeague).
// The bouts are scripted by ROLE (a competitor's place in the roster), never by
// the draw's side: a league pairs everyone, so the draw only decides who stands
// Shiro, and a script keyed by the sorted pair holds whichever side the draw
// gives it. Expected figures are tallied from the scripts, never read back.
//
// Not serial: each test signs in (or creates the tournament when this file's
// server has none), seeds a competition of its own on a shiaijo of its own, and
// keeps its own folder of screenshots and audit rows, so one failing test or
// one test.fixme hides nothing else here.
import { test, expect } from '../fixtures/test.mjs';
import { EXPECT_TIMEOUT_MS, PUBLIC_DEVICE } from '../fixtures/devices.mjs';
import { journeyShots } from '../fixtures/shots.mjs';
import { auditLog } from '../fixtures/audit.mjs';
import { enterTournament } from '../fixtures/setup.mjs';
import { createCompetition } from '../fixtures/wizard.mjs';
import { generateDraw, pasteRoster, startCompetition } from '../fixtures/competition.mjs';
import { completedRowFor, openShiaijo, sides, upNextCard, waitingRows } from '../fixtures/shiaijo.mjs';
import {
  EDITOR, INLINE_EDITOR, awardIppon, finishMatch, ipponButton, markDraw, nextTapWrite, running, startMatch,
  writeLanded, writtenMatchId,
} from '../fixtures/scoring.mjs';
import { editorSides, ensureRunning, inlineRunning, rosterOf } from '../fixtures/individual.mjs';
import {
  awardsTabOffered, chooseTiebreakTop, drawnBoutNotices, leagueRanks, openLeague,
  openLeagueSettings, openNumberedRow, openViewerAwards, readBanner, readChusen, readLeagueSettings,
  readLeagueTable, readNumberedRows, readPodium, saveSettings, seedLeague,
} from '../fixtures/league.mjs';
import { openViewer } from '../fixtures/public.mjs';

// Enough shiaijo for every test here to have one of its own.
const CUP = { name: 'Individual League Cup', courts: 10 };

// A tie-breaker bout (ippon-shobu) id, as the score write names it.
const TB_ID = /^Pool [^-]+-TB-\d+$/;
// A regular league bout reads "Match n of m"; a tie-breaker is the bare pairing.
const MATCH_N_OF_M = /Match \d+ of \d+/i;
// Words that would tell an operator why an extra bout appeared (F12).
const SAYS_TIEBREAK = /tie-?break|ippon[- ]shobu|decides? (the )?\d|place/i;

// A recorder per test, flushed by afterAll to the audit.json in the test's own
// folder (the one its screenshots go to).
const audits = [];
function recorder(journey) {
  const folder = `league-individual/${journey}`;
  const log = auditLog(folder);
  audits.push(log);
  return { record: (row) => log.record({ journey, ...row }), shot: journeyShots(folder) };
}

// ------------------------------------------------------------------ scripts

const zero = () => ({ W: 0, L: 0, D: 0, PW: 0, PL: 0 });
// The individual league ranking (CLAUDE.md, Key Algorithms): wins, then fewer
// losses, then more hikiwake, then more points scored, then fewer points lost.
// 0 means level on every criterion.
const order = (a, b) => (b.W - a.W) || (a.L - b.L) || (b.D - a.D) || (b.PW - a.PW) || (a.PL - b.PL);
const pairKey = (a, b) => [a, b].sort().join(' | ');

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
    tiebreakWords: (await page.locator('main').getByText(/tie-?break|ippon[- ]shobu|chusen|drawing lots/i).allInnerTexts())
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

// The public Awards tab of the competition, read on a spectator's phone.
async function publicPodium(browser, baseURL, id) {
  const spectator = await browser.newContext({ ...PUBLIC_DEVICE, baseURL });
  try {
    const phone = await spectator.newPage();
    await openViewer(phone, id);
    if (!(await awardsTabOffered(phone))) return { offered: false, podium: null };
    await openViewerAwards(phone, id);
    return { offered: true, podium: await readPodium(phone) };
  } finally {
    await spectator.close();
  }
}

// Wait a moment for a tie-breaker that must NOT come: the table must read the
// league finished with no row past the regular ones, held for a few seconds so
// a late injection is not missed.
async function expectNoTiebreak(page, id, regular) {
  await expect.poll(async () => (await readLeagueTable(page)).heading, { timeout: EXPECT_TIMEOUT_MS }).toBe('Final standings');
  await page.waitForTimeout(2_000);
  await openLeague(page, id);
  expect(tiebreakRows(await readNumberedRows(page), regular), 'no tie-breaker bout was added').toEqual([]);
}

test.describe('league-individual', () => {
  test.afterAll(() => audits.forEach((a) => a.flush()));

  // ------------------------------------------------------------- 1. cycle
  // Four entrants: A beats B, B beats C, C beats A, and all three beat D, so
  // A, B and C end level on every criterion (W2 L1 D0 PW2 PL1) at 1st-3rd.
  test('a three-way tie at 1st: ippon-shobu bouts are added by themselves (F12), then won in a cycle', async ({ page, browser, baseURL }) => {
    test.setTimeout(480_000);
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

    await test.step('record what follows the cycle: a further round, a stranded league, or a finish', async () => {
      await openLeague(page, id);
      // Give the engine time to answer the last write (a further round would
      // be added, or the league would finish) before the read is judged.
      await page.waitForTimeout(3_000);
      await openLeague(page, id);
      const after = await readDesk(page);
      const further = tiebreakRows(after.numberedRows, 9);
      const outcome = after.finished ? 'finished with the cycle unbroken' : (further.length ? 'a further round was added' : 'stranded: not finished, nothing to play');
      const pub = await publicPodium(browser, baseURL, id);
      record({ step: 'pools', action: 'read after the cycle', outcome, ranks: await leagueRanks(page), furtherRound: further, ...after, public: pub,
        shot: await shot(page, 'pools-after-cycle', { fullPage: true }) });
      test.info().annotations.push({ type: 'finding', description: `individual ippon-shobu cycle at 1st-3rd: ${outcome}` });
      expect(after.table.rows.map((r) => r.name).sort()).toEqual([...names].sort());
    });
  });

  // ------------------------------------------------------ 2. drawn today
  // Three entrants: A and B draw, both beat C, so A and B end level at 1st.
  test('a drawn ippon-shobu today: what the desk and the court see, and whether it can be reopened and given a winner (F3b)', async ({ page, browser, baseURL }) => {
    test.setTimeout(360_000);
    const { record, shot } = recorder('drawn-today');
    const names = rosterOf('Dr', 3).map(([n]) => n);
    const [A, B, C] = names;
    const s = scripted(names);
    s.draw(A, B); s.beat(A, C); s.beat(B, C);
    let id;

    await test.step('seed and play the three regular bouts on shiaijo B', async () => {
      await enterTournament(page, CUP);
      id = await seedLeague(page, { name: 'Drawn League', kind: 'individual', entrants: rosterOf('Dr', 3), courts: ['B'] });
      await openShiaijo(page, 'B');
      const played = await playOnCourt(page, 3, names, s.planFor);
      record({ step: 'court', action: 'three regular bouts', played });
    });

    let pair;
    await test.step('court: the ippon-shobu between A and B is queued; save it as a draw', async () => {
      pair = await boutOnCourt(page, [A, B]);
      await ensureRunning(page);
      const toggle = page.locator(INLINE_EDITOR).first().getByTestId('scoring-modal-mark-draw');
      const offered = { disabled: await toggle.isDisabled(), title: await toggle.getAttribute('title') };
      record({ step: 'court', action: 'Mark draw on the tie-breaker', pair, offered, shot: await shot(page, 'court-tiebreaker-running') });
      // Once the fix is in (D6 (b)) a tie-breaker cannot be saved drawn, and
      // there is nothing left of today's shape to record.
      test.skip(offered.disabled, 'the editor refuses a drawn league tie-breaker (D6 (b) is in): today\'s draw cannot be made');
      await markDraw(page);
      await finishMatch(page);
      await expect(completedRowFor(page, pair)).toBeVisible();
      record({ step: 'court', action: 'the console once the tie-breaker is saved drawn', court: await readCourt(page),
        shot: await shot(page, 'court-after-drawn-tiebreaker', { fullPage: true }) });
    });

    await test.step('desk: the league is not finished; what Pools says about the drawn bout', async () => {
      await openLeague(page, id);
      await page.waitForTimeout(2_000);
      await openLeague(page, id);
      const desk = await readDesk(page);
      const pub = await publicPodium(browser, baseURL, id);
      record({ step: 'pools', action: 'read with the tie-breaker saved drawn', finding: 'F3b', ...desk, public: pub,
        shot: await shot(page, 'pools-drawn-tiebreaker', { fullPage: true }) });
      expect(desk.finished, 'a league whose tie-breaker has no winner is not finished').toBe(false);
      expect(desk.table.winner).toBe('');
      expect(tiebreakRows(desk.numberedRows, 3)).toHaveLength(1);
    });

    await test.step('desk: reopen the drawn bout from its numbered row and give it a winner', async () => {
      const ed = await openNumberedRow(page, { num: 4 });
      const pill = ed.locator('.editor-head-pill');
      const opened = {
        pill: (await pill.count()) ? (await pill.first().innerText()).trim() : '',
        sides: await editorSides(page, EDITOR),
        drawToggle: (await ed.getByTestId('scoring-modal-mark-draw').innerText().catch(() => '')).trim(),
        buttons: (await ed.locator('button:visible').allInnerTexts()).map((t) => t.replace(/\s+/g, ' ').trim()).filter(Boolean),
      };
      record({ step: 'pools', action: 'tap the drawn tie-breaker\'s numbered row', ...opened, shot: await shot(page, 'pools-drawn-tiebreaker-opened') });
      // Today the row opens the bout as a correction: cancel the draw, give A
      // one men, Save correction (and its reason prompt).
      // The toggle is upper-cased by CSS, so its state is read from its
      // accessible name, never its rendered text.
      const drawToggle = ed.getByTestId('scoring-modal-mark-draw');
      const side = opened.sides.shiro === A ? 'shiro' : 'aka';
      const drawOn = (await drawToggle.getAttribute('aria-label')) === 'Cancel draw (hikiwake)';
      record({ step: 'pools', action: 'the reopened drawn bout\'s board', drawOn,
        ipponOfferedWhileDrawOn: drawOn ? await ipponButton(page, side, 'M', EDITOR).isEnabled() : null });
      if (drawOn) {
        await drawToggle.tap();
        await expect(drawToggle).toHaveAttribute('aria-label', 'Mark as draw (hikiwake)');
      }
      await awardIppon(page, side, 'M', EDITOR);
      const save = ed.getByRole('button', { name: 'Save correction' });
      const saveOffered = (await save.count()) > 0;
      if (saveOffered) {
        await save.tap();
        const reason = ed.locator('.reason-prompt');
        if (await reason.waitFor({ state: 'visible', timeout: 4_000 }).then(() => true, () => false)) {
          await reason.locator('button.btn--primary').tap();
        }
      } else {
        await finishMatch(page, EDITOR);
      }
      await expect(page.locator(EDITOR)).toHaveCount(0, { timeout: EXPECT_TIMEOUT_MS }).catch(() => {});
      const after = await deskOnceFinished(page).catch(async () => readDesk(page));
      const pub = await publicPodium(browser, baseURL, id);
      record({ step: 'pools', action: 'the drawn tie-breaker corrected to a win for A', saveOffered, ranks: await leagueRanks(page), ...after, public: pub,
        shot: await shot(page, 'pools-tiebreaker-corrected', { fullPage: true }) });
      expect(after.finished, 'once the tie-breaker has a winner the league finishes').toBe(true);
      expect(after.table.winner).toBe(A);
      const ranks = await leagueRanks(page);
      expect(ranks[A]).toBeLessThan(ranks[B]);
    });
  });

  // ------------------------------------------------- 3. F3b / D6 (b) fixme
  // The same shape as the drawn journey; the fixed editor treats a league
  // tie-breaker as a knockout bout: no draw, encho, then hantei.
  test.fixme('F3b: a league ippon-shobu cannot be saved drawn; encho, then hantei, decides it and the league finishes', async ({ page, browser, baseURL }) => {
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

    await test.step('the tie-breaker\'s editor offers no draw, and the x key marks none', async () => {
      await boutOnCourt(page, [A, B]);
      await ensureRunning(page);
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
      await finishMatch(page);
      await expect(completedRowFor(page, pair)).toBeVisible();
      await openLeague(page, id);
      const desk = await deskOnceFinished(page);
      record({ step: 'pools', action: 'after the hantei', ...desk, public: await publicPodium(browser, baseURL, id),
        shot: await shot(page, 'pools-after-hantei', { fullPage: true }) });
      expect(desk.table.winner).toBe(A);
    });
  });

  // ------------------------------------------------------ 4/5. joint 3rd
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
      const pub = await publicPodium(browser, baseURL, id);
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
      const pub = await publicPodium(browser, baseURL, id);
      record({ step: 'pools', action: 'read once finished', ranks, ...desk, public: pub, shot: await shot(page, 'pools-joint-third-off', { fullPage: true }) });
      expect(desk.table.winner).toBe(A);
      expect(ranks).toMatchObject({ [A]: 1, [B]: 2, [C]: 3, [D]: 4 });
    });
  });

  // ------------------------------------------------------- 6. below band
  // Five entrants: A beats everyone, B beats C, D and E, C beats D and E, D
  // and E draw, so D and E end level (W0 L3 D1 PW0 PL3) at 4th-5th: below the
  // band of 3 an individual league breaks ties for. Two joint 3rd places is
  // off, so the 3rd-place exemption plays no part in the outcome.
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

  test('a tie below the band (4th-5th): no ippon-shobu, the league finishes', async ({ page }) => {
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
      // A tie nobody breaks is a shared place, yet the table numbers the two
      // rows apart: recorded for the findings, not asserted (no ruling says
      // how a shared place below the band reads).
      const shared = ranks[D] === ranks[E];
      record({ step: 'pools', action: 'read once finished', ranks, sharedPlaceShown: shared, ...desk,
        shot: await shot(page, 'pools-below-band', { fullPage: true }) });
      test.info().annotations.push({ type: 'finding', description: `tie at 4th-5th below the band: ranks read ${ranks[D]} and ${ranks[E]}` });
      expect(ranks).toMatchObject({ [A]: 1, [B]: 2, [C]: 3 });
      for (const n of [D, E]) expect(desk.table.rows.find((r) => r.name === n), `${n}'s figures`).toMatchObject(s.tally[n]);
    });
  });

  // --------------------------------------------------------------- 7. F6
  // A team league saved with "Top 4", then switched to individual before the
  // roster: the settings page hides the pills for an individual competition,
  // but the stored band may still be 4.
  async function seedTeamThenIndividual(page, { name, court, record, shot }) {
    const id = await createCompetition(page, {
      name, kind: 'team', format: 'league', teamSize: 3, teamMatchType: 'fixed', courts: [court], twoThirdPlaces: false,
    });
    await openLeagueSettings(page, id);
    await chooseTiebreakTop(page, 4);
    await saveSettings(page, id);
    await openLeagueSettings(page, id);
    const asTeam = await readLeagueSettings(page);
    const kind = page.getByRole('group', { name: /^Competition type/ });
    await kind.getByRole('button', { name: 'Individual', exact: true }).tap();
    await expect(kind.getByRole('button', { name: 'Individual', exact: true })).toHaveAttribute('aria-pressed', 'true');
    const clears = page.getByTestId('config-clears-notice');
    const clearsNotice = (await clears.count()) ? (await clears.innerText()).replace(/\s+/g, ' ').trim() : '';
    record({ step: 'settings', action: 'Top 4 saved, then Individual chosen', asTeam: asTeam.tiebreakTop, clearsNotice,
      shot: await shot(page, 'settings-switched-to-individual', { fullPage: true }) });
    await saveSettings(page, id);
    await openLeagueSettings(page, id);
    const asIndividual = await readLeagueSettings(page);
    record({ step: 'settings', action: 'read once saved as individual', ...asIndividual });
    expect(asTeam.tiebreakTop.active).toBe('Top 4');
    expect(asIndividual.tiebreakTop.shown, 'the band is not offered for an individual league').toBe(false);
    return { id, clearsNotice };
  }

  test('F6 today: a team league saved with Top 4 and switched to individual, with a tie at 4th-5th', async ({ page }) => {
    test.setTimeout(600_000);
    const { record, shot } = recorder('f6-today');
    const { names, D, E, s } = belowBand('Fa');
    let id;

    await test.step('seed: team league, Top 4, switched to individual, five entrants on shiaijo G', async () => {
      await enterTournament(page, CUP);
      ({ id } = await seedTeamThenIndividual(page, { name: 'Switched League', court: 'G', record, shot }));
      await pasteRoster(page, id, rosterOf('Fa', 5));
      await generateDraw(page, id);
      await startCompetition(page, id);
      await openShiaijo(page, 'G');
      record({ step: 'court', action: 'ten regular bouts', played: await playOnCourt(page, 10, names, s.planFor) });
    });

    await test.step('record whether the hidden band of 4 adds an ippon-shobu for 4th-5th', async () => {
      await openLeague(page, id);
      await page.waitForTimeout(3_000);
      await openLeague(page, id);
      const desk = await readDesk(page);
      const added = tiebreakRows(desk.numberedRows, 10);
      const reproduced = added.length > 0;
      record({ step: 'pools', action: 'read after the ten bouts', finding: 'F6', reproduced, tiebreakers: added, ranks: await leagueRanks(page), ...desk,
        shot: await shot(page, 'pools-f6-today', { fullPage: true }) });
      test.info().annotations.push({ type: 'finding', description: `F6 ${reproduced ? 'reproduced' : 'not reproduced'}: ${added.length} tie-breaker(s) added at 4th-5th` });
      if (reproduced) {
        expect(added.map((r) => pairKey(r.shiro, r.aka))).toEqual([pairKey(D, E)]);
        expect(desk.finished).toBe(false);
      } else {
        expect(desk.finished).toBe(true);
      }
    });
  });

  test.fixme('F6: a league switched from team to individual breaks ties for the top 3 only, whatever band it had as a team', async ({ page }) => {
    test.setTimeout(600_000);
    const { record, shot } = recorder('f6-fixed');
    const { names, D, E, s } = belowBand('Fb');
    let id;

    await test.step('seed: team league, Top 4, switched to individual, five entrants on shiaijo H', async () => {
      await enterTournament(page, CUP);
      ({ id } = await seedTeamThenIndividual(page, { name: 'Switched League Two', court: 'H', record, shot }));
      await pasteRoster(page, id, rosterOf('Fb', 5));
      await generateDraw(page, id);
      await startCompetition(page, id);
      await openShiaijo(page, 'H');
      await playOnCourt(page, 10, names, s.planFor);
    });

    await test.step('no tie-breaker is added for 4th-5th; the league finishes with D and E sharing 4th', async () => {
      await openLeague(page, id);
      await expectNoTiebreak(page, id, 10);
      const ranks = await leagueRanks(page);
      record({ step: 'pools', action: 'read once finished', ranks, shot: await shot(page, 'pools-f6-fixed', { fullPage: true }) });
      expect(ranks[D]).toBe(ranks[E]);
    });
  });

  // -------------------------------------------------------- F12 fixme
  test.fixme('F12: the court console says the ippon-shobu decides 1st place', async ({ page }) => {
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
